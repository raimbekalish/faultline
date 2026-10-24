package telemetry

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/raimbekalish/faultline/internal/localhttp"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func recorder(t *testing.T, name string) (*Runtime, *tracetest.InMemoryExporter) {
	t.Helper()
	e := tracetest.NewInMemoryExporter()
	r := newRuntime(name, e)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		r.Shutdown(ctx)
	})
	return r, e
}

func TestW3CChainAndAllowlistedMetadata(t *testing.T) {
	runner, re := recorder(t, "runner")
	frontend, fe := recorder(t, "frontend")
	dependency, de := recorder(t, "dependency")
	client := localhttp.New(time.Second)
	defer client.CloseIdleConnections()
	backend := httptest.NewServer(dependency.Handler("/value", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if baggage.FromContext(r.Context()).Member("secret").Value() != "" {
			t.Error("forwarded untrusted baggage")
		}
		_, _ = w.Write([]byte("42"))
	})))
	defer backend.Close()
	app := httptest.NewServer(frontend.Handler("/work", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := http.NewRequestWithContext(r.Context(), "GET", backend.URL, nil)
		result := frontend.Do(client, req, "/value", Metadata{})
		if result.Err != nil {
			t.Error(result.Err)
		}
		_, _ = w.Write(result.Body)
	})))
	defer app.Close()
	m := Metadata{"faultline-0123456789abcdef0123456789abcdef", "fault", 2}
	req, _ := http.NewRequest("GET", app.URL+"?token=do-not-export", nil)
	req.Header.Set("Authorization", "Bearer do-not-export")
	req.Header.Set("Baggage", "secret=do-not-export")
	result := runner.Do(client, req, "/work", m)
	if result.Err != nil || result.Status != 200 {
		t.Fatalf("%+v", result)
	}
	for _, rt := range []*Runtime{runner, frontend, dependency} {
		if err := rt.provider.ForceFlush(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	spans := append(append(re.GetSpans(), fe.GetSpans()...), de.GetSpans()...)
	if len(spans) != 4 {
		t.Fatalf("got %d spans", len(spans))
	}
	byID := map[string]tracetest.SpanStub{}
	for _, s := range spans {
		if s.SpanContext.TraceID().String() != result.TraceID {
			t.Fatal("broken trace propagation")
		}
		attrs := map[string]string{}
		for _, a := range s.Attributes {
			attrs[string(a.Key)] = a.Value.Emit()
			if strings.Contains(a.Value.Emit(), "do-not-export") {
				t.Fatal("sensitive data exported")
			}
		}
		if attrs[runKey] != m.RunID || attrs[phaseKey] != "fault" || attrs[observationKey] != "2" {
			t.Fatalf("missing metadata: %+v", attrs)
		}
		byID[s.SpanContext.SpanID().String()] = s
	}
	root := byID[result.SpanID]
	if root.Parent.IsValid() || root.SpanKind != trace.SpanKindClient {
		t.Fatal("observation is not a client root")
	}
	for _, s := range spans {
		if s.SpanContext.SpanID().String() == result.SpanID {
			continue
		}
		parent, ok := byID[s.Parent.SpanID().String()]
		if !ok || parent.SpanKind == s.SpanKind {
			t.Fatalf("bad parent: %+v", s)
		}
		if s.SpanKind == trace.SpanKindServer && !s.Parent.IsRemote() {
			t.Fatal("server did not extract remote parent")
		}
	}
}

func TestInvalidMetadataIsStripped(t *testing.T) {
	rt, e := recorder(t, "server")
	h := rt.Handler("/work", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if baggage.FromContext(r.Context()).Len() != 0 {
			t.Error("invalid baggage retained")
		}
	}))
	req := httptest.NewRequest("GET", "http://frontend/work", nil)
	req.Header.Set("baggage", "faultline.run_id=production,faultline.phase=fault,faultline.observation=1,secret=credential")
	h.ServeHTTP(httptest.NewRecorder(), req)
	_ = rt.provider.ForceFlush(context.Background())
	for _, a := range e.GetSpans()[0].Attributes {
		if strings.HasPrefix(string(a.Key), "faultline.") {
			t.Fatalf("exported invalid metadata: %v", a)
		}
	}
}

func TestClientDeadlineIncludesBodyRead(t *testing.T) {
	for _, bodyStarted := range []bool{false, true} {
		t.Run(map[bool]string{false: "headers", true: "body"}[bodyStarted], func(t *testing.T) {
			rt, e := recorder(t, "client")
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if bodyStarted {
					w.WriteHeader(200)
					w.(http.Flusher).Flush()
				}
				<-r.Context().Done()
			}))
			defer s.Close()
			client := localhttp.New(30 * time.Millisecond)
			defer client.CloseIdleConnections()
			req, _ := http.NewRequest("GET", s.URL, nil)
			start := time.Now()
			result := rt.Do(client, req, "/work", Metadata{})
			if result.Err == nil || time.Since(start) > time.Second {
				t.Fatalf("unbounded or missing timeout: %+v", result)
			}
			_ = rt.provider.ForceFlush(context.Background())
			spans := e.GetSpans()
			if len(spans) != 1 || spans[0].Status.Code != codes.Error || spans[0].Status.Description != "timeout" || len(spans[0].Events) != 1 {
				t.Fatalf("timeout span: %+v", spans)
			}
			if spans[0].EndTime.Sub(spans[0].StartTime) < 20*time.Millisecond {
				t.Fatal("span ended before body/deadline")
			}
		})
	}
}

type stalledExporter struct{}

func (stalledExporter) ExportSpans(ctx context.Context, _ []sdktrace.ReadOnlySpan) error {
	<-ctx.Done()
	return ctx.Err()
}
func (stalledExporter) Shutdown(context.Context) error { return nil }

func TestQueueDoesNotBlockRequestsAndShutdownIsBounded(t *testing.T) {
	rt := newRuntime("test", stalledExporter{})
	done := make(chan struct{})
	go func() {
		for range 2000 {
			_, span := rt.tracer.Start(context.Background(), "test")
			span.End()
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("full trace queue blocked producer")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	s := rt.Shutdown(ctx)
	if s == nil || s.ShutdownError == "" {
		t.Fatalf("stalled export not reported: %+v", s)
	}
}

func TestDisabledModeAndUnexpectedMode(t *testing.T) {
	for _, mode := range []string{"", "off"} {
		rt, err := NewFromMode(context.Background(), "test", mode)
		if err != nil || rt != nil {
			t.Fatalf("disabled: %v %v", rt, err)
		}
	}
	_, err := NewFromMode(context.Background(), "test", "http://unrelated.invalid")
	if err == nil {
		t.Fatal("accepted arbitrary endpoint")
	}
	if errorType(errors.New("sensitive-error")) != "request_error" {
		t.Fatal("raw error exposed")
	}
}
