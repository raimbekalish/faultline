//go:build integration

package experiment

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/raimbekalish/faultline/internal/telemetry"
	"github.com/raimbekalish/faultline/internal/tracequery"
)

func TestRealTraceExportAndCorrelation(t *testing.T) {
	if os.Getenv("FAULTLINE_TRACING") != "jaeger" {
		t.Skip("optional Jaeger overlay is disabled")
	}
	sentinel(t)
	query := tracequery.New()
	defer query.Close()
	for _, latency := range []int{80, 400} {
		t.Run(fmt.Sprintf("latency_%dms", latency), func(t *testing.T) {
			rt, err := telemetry.NewFromMode(context.Background(), "faultline-runner", "jaeger")
			if err != nil {
				t.Fatal(err)
			}
			r, err := RunWithTelemetry(context.Background(), Config{Samples: 3, LatencyMS: latency, RequestTimeoutMS: 1000}, "integration-test", rt)
			shutdown, cancel := context.WithTimeout(context.Background(), telemetry.ShutdownTimeout)
			stats := rt.Shutdown(shutdown)
			cancel()
			if err != nil || !r.CleanupComplete || stats.ShutdownError != "" || stats.ExportFailures != 0 {
				t.Fatalf("run/export: %+v %+v %v", r, stats, err)
			}
			var ids []string
			for _, phase := range []Phase{r.Baseline, r.Fault, r.Recovery} {
				for _, s := range phase.Samples {
					ids = append(ids, s.TraceID)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			traces, err := query.Collect(ctx, ids)
			cancel()
			if err != nil {
				t.Fatal(err)
			}
			index := 0
			for _, phase := range []struct {
				name string
				data Phase
			}{{"baseline", r.Baseline}, {"fault", r.Fault}, {"recovery", r.Recovery}} {
				for observation, sample := range phase.data.Samples {
					record := traces[index]
					index++
					if !record.Complete || len(record.Spans) != 4 {
						t.Fatalf("missing actual spans: %+v", record.Spans)
					}
					byID := map[string]tracequery.Span{}
					var dependencyClient, dependencyServer, frontendServer tracequery.Span
					for _, span := range record.Spans {
						if span.TraceID != sample.TraceID || span.RunID != r.RunID || span.Phase != phase.name || span.Observation != strconv.Itoa(observation) {
							t.Fatalf("broken correlation: %+v sample=%+v", span, sample)
						}
						byID[span.SpanID] = span
						if span.Service == "faultline-frontend" && span.Kind == "client" {
							dependencyClient = span
						}
						if span.Service == "faultline-frontend" && span.Kind == "server" {
							frontendServer = span
						}
						if span.Service == "faultline-dependency" && span.Kind == "server" {
							dependencyServer = span
						}
					}
					root, ok := byID[sample.SpanID]
					if !ok || root.ParentSpanID != "" || root.Service != "faultline-runner" || root.Kind != "client" {
						t.Fatalf("root correlation: %+v", record.Spans)
					}
					if frontendServer.ParentSpanID != root.SpanID || dependencyClient.ParentSpanID != frontendServer.SpanID || dependencyServer.ParentSpanID != dependencyClient.SpanID {
						t.Fatalf("broken parent chain: %+v", record.Spans)
					}
					if phase.name == "fault" {
						if latency == 400 {
							if sample.Status != 504 || root.HTTPStatus != 504 || frontendServer.HTTPStatus != 504 || frontendServer.OTelStatus != "ERROR" || dependencyClient.ErrorType != "timeout" || dependencyClient.OTelStatus != "ERROR" || dependencyClient.HTTPStatus != 0 {
								t.Fatalf("incorrect timeout semantics: %+v", record.Spans)
							}
						} else if sample.Status != 200 || dependencyClient.ErrorType != "" {
							t.Fatalf("latency became error: %+v", record.Spans)
						}
						if dependencyClient.DurationMS-dependencyServer.DurationMS < 50 {
							t.Fatalf("client delay absent: %+v", record.Spans)
						}
					}
					if dependencyServer.HTTPStatus != 200 || dependencyServer.ErrorType != "" {
						t.Fatalf("invented dependency error: %+v", record.Spans)
					}
					if observation == 0 {
						t.Logf("latency=%d phase=%s trace=%s observation=%.2fms dependency_client=%.2fms dependency_server=%.2fms status=%d error=%s", latency, phase.name, record.TraceID, sample.DurationMS, dependencyClient.DurationMS, dependencyServer.DurationMS, sample.Status, dependencyClient.ErrorType)
					}
				}
			}
			assertOnlySentinel(t)
		})
	}
}

func TestRealExportFailureCannotHoldFault(t *testing.T) {
	if os.Getenv("FAULTLINE_TRACING") == "jaeger" {
		t.Skip("requires the no-collector topology")
	}
	sentinel(t)
	rt, err := telemetry.NewFromMode(context.Background(), "faultline-runner", "jaeger")
	if err != nil {
		t.Fatal(err)
	}
	r, err := RunWithTelemetry(context.Background(), Config{Samples: 5, LatencyMS: 80, RequestTimeoutMS: 1000}, "integration-test", rt)
	// Check actual proxy absence before any telemetry shutdown is attempted.
	assertOnlySentinel(t)
	ctx, cancel := context.WithTimeout(context.Background(), telemetry.ShutdownTimeout)
	stats := rt.Shutdown(ctx)
	cancel()
	if err != nil || r.Status != "complete" || !r.CleanupComplete || stats.ExportFailures == 0 {
		t.Fatalf("export failure affected experiment: %+v %+v %v", r, stats, err)
	}
}
