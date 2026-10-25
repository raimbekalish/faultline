// Package telemetry instruments only Faultline's fixed sample HTTP topology.
// It uses explicit providers, bounded asynchronous export, and no global SDK state.
package telemetry

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/raimbekalish/faultline/internal/localhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

const ShutdownTimeout = 2 * time.Second
const QueueSize = 256
const runKey = "faultline.run_id"
const phaseKey = "faultline.phase"
const observationKey = "faultline.observation"

var runPattern = regexp.MustCompile(`^faultline-[a-f0-9]{32}$`)
var propagator = propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})

type Metadata struct {
	RunID       string
	Phase       string
	Observation int
}

func (m Metadata) valid() bool {
	return runPattern.MatchString(m.RunID) && (m.Phase == "readiness" || m.Phase == "baseline" || m.Phase == "fault" || m.Phase == "recovery") && m.Observation >= 0 && m.Observation < 100
}

func (m Metadata) attrs() []attribute.KeyValue {
	if !m.valid() {
		return nil
	}
	return []attribute.KeyValue{attribute.String(runKey, m.RunID), attribute.String(phaseKey, m.Phase), attribute.Int(observationKey, m.Observation)}
}

func withMetadata(ctx context.Context, m Metadata) context.Context {
	var members []baggage.Member
	if m.valid() {
		for k, v := range map[string]string{runKey: m.RunID, phaseKey: m.Phase, observationKey: strconv.Itoa(m.Observation)} {
			member, _ := baggage.NewMember(k, v)
			members = append(members, member)
		}
	}
	b, _ := baggage.New(members...)
	return baggage.ContextWithBaggage(ctx, b)
}

func metadata(ctx context.Context) Metadata {
	b := baggage.FromContext(ctx)
	n, err := strconv.Atoi(b.Member(observationKey).Value())
	if err != nil {
		return Metadata{}
	}
	m := Metadata{b.Member(runKey).Value(), b.Member(phaseKey).Value(), n}
	if !m.valid() {
		return Metadata{}
	}
	return m
}

type Stats struct {
	Enabled        bool   `json:"enabled"`
	ExportedSpans  int64  `json:"exported_spans"`
	ExportFailures int64  `json:"export_failures"`
	ShutdownError  string `json:"shutdown_error,omitempty"`
}

type countingExporter struct {
	sdktrace.SpanExporter
	exported atomic.Int64
	failures atomic.Int64
}

func (e *countingExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	err := e.SpanExporter.ExportSpans(ctx, spans)
	if err != nil {
		e.failures.Add(1)
	} else {
		e.exported.Add(int64(len(spans)))
	}
	return err
}

type Runtime struct {
	provider *sdktrace.TracerProvider
	tracer   trace.Tracer
	exporter *countingExporter
	client   *http.Client
}

// NewFromMode intentionally supports only off or the fixed local Jaeger service.
// Generic OTEL endpoint/header/resource environment variables are not a target API.
func NewFromMode(ctx context.Context, service, mode string) (*Runtime, error) {
	if mode == "" || mode == "off" {
		return nil, nil
	}
	if mode != "jaeger" {
		return nil, fmt.Errorf("tracing mode must be off or jaeger")
	}
	client := localhttp.New(300 * time.Millisecond)
	e, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpointURL("http://jaeger:4318/v1/traces"),
		otlptracehttp.WithHeaders(map[string]string{}),
		otlptracehttp.WithCompression(otlptracehttp.NoCompression),
		otlptracehttp.WithTimeout(300*time.Millisecond),
		otlptracehttp.WithRetry(otlptracehttp.RetryConfig{Enabled: false}),
		otlptracehttp.WithHTTPClient(client),
	)
	if err != nil {
		client.CloseIdleConnections()
		return nil, err
	}
	t := newRuntime(service, e)
	t.client = client
	return t, nil
}

func newRuntime(service string, e sdktrace.SpanExporter) *Runtime {
	counted := &countingExporter{SpanExporter: e}
	p := sdktrace.NewTracerProvider(
		sdktrace.WithResource(resource.NewSchemaless(attribute.String("service.name", service))),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.AlwaysSample())),
		sdktrace.WithSpanLimits(sdktrace.SpanLimits{AttributeCountLimit: 16, AttributeValueLengthLimit: 128, EventCountLimit: 4, AttributePerEventCountLimit: 2, LinkCountLimit: 0}),
		sdktrace.WithBatcher(counted, sdktrace.WithMaxQueueSize(QueueSize), sdktrace.WithMaxExportBatchSize(32), sdktrace.WithBatchTimeout(100*time.Millisecond), sdktrace.WithExportTimeout(500*time.Millisecond)),
	)
	return &Runtime{provider: p, tracer: p.Tracer("faultline/http"), exporter: counted}
}

// Shutdown must be called after fault cleanup, with an independent bounded context.
// Export failure is telemetry loss, never permission to skip fault cleanup.
func (t *Runtime) Shutdown(ctx context.Context) *Stats {
	if t == nil {
		return nil
	}
	err := t.provider.Shutdown(ctx)
	if t.client != nil {
		t.client.CloseIdleConnections()
	}
	s := &Stats{Enabled: true, ExportedSpans: t.exporter.exported.Load(), ExportFailures: t.exporter.failures.Load()}
	if err != nil {
		s.ShutdownError = "telemetry shutdown failed"
	}
	return s
}

func errorType(err error) string {
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	var n net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &n) && n.Timeout()) {
		return "timeout"
	}
	return "request_error"
}

func finish(span trace.Span, status int, err error, server bool) {
	if span == nil {
		return
	}
	defer span.End()
	if status != 0 {
		span.SetAttributes(attribute.Int("http.response.status_code", status))
	}
	var kind string
	if err != nil {
		kind = errorType(err)
	} else if status >= 500 || (!server && status >= 400) {
		kind = "HTTP_" + strconv.Itoa(status)
	}
	if kind != "" {
		span.SetAttributes(attribute.String("error.type", kind))
		span.SetStatus(codes.Error, kind)
		if err != nil {
			span.RecordError(errors.New(kind))
		}
	}
}

type Result struct {
	Body    []byte
	Status  int
	Err     error
	TraceID string
	SpanID  string
}

// Do ends the client span only after the bounded response-body read, including
// read failures and deadline errors. It preserves the caller's http.Client timeout.
func (t *Runtime) Do(client *http.Client, req *http.Request, route string, m Metadata) (result Result) {
	var span trace.Span
	if t != nil {
		ctx := req.Context()
		if m.valid() {
			ctx = withMetadata(ctx, m)
		} else {
			ctx = withMetadata(ctx, metadata(ctx))
		}
		ctx, span = t.tracer.Start(ctx, req.Method+" "+route, trace.WithSpanKind(trace.SpanKindClient), trace.WithAttributes(append(metadata(ctx).attrs(), attribute.String("http.request.method", req.Method))...))
		req = req.Clone(ctx)
		propagator.Inject(ctx, propagation.HeaderCarrier(req.Header))
		sc := span.SpanContext()
		result.TraceID, result.SpanID = sc.TraceID().String(), sc.SpanID().String()
	}
	defer func() { finish(span, result.Status, result.Err, false) }()
	resp, err := client.Do(req)
	result.Err = err
	if resp != nil {
		result.Status = resp.StatusCode
		result.Body, err = localhttp.ReadBody(resp.Body)
		_ = resp.Body.Close()
		if result.Err == nil {
			result.Err = err
		}
	}
	return result
}

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (w *responseWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
		w.ResponseWriter.WriteHeader(status)
	}
}
func (w *responseWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	return w.ResponseWriter.Write(b)
}

// Handler is only applied to the two known application routes, not health checks.
// Headers, URLs, query strings, request bodies and arbitrary baggage are not span attributes.
func (t *Runtime) Handler(route string, next http.Handler) http.Handler {
	if t == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := propagator.Extract(r.Context(), propagation.HeaderCarrier(r.Header))
		ctx = withMetadata(ctx, metadata(ctx)) // Strip every non-allowlisted baggage member.
		ctx, span := t.tracer.Start(ctx, r.Method+" "+route, trace.WithSpanKind(trace.SpanKindServer), trace.WithAttributes(append(metadata(ctx).attrs(), attribute.String("http.request.method", r.Method), attribute.String("http.route", route))...))
		rw := &responseWriter{ResponseWriter: w}
		defer func() {
			if rw.status == 0 {
				rw.status = 200
			}
			finish(span, rw.status, ctx.Err(), true)
		}()
		next.ServeHTTP(rw, r.WithContext(ctx))
	})
}
