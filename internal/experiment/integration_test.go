//go:build integration

package experiment

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/raimbekalish/faultline/internal/localhttp"
	"github.com/raimbekalish/faultline/internal/telemetry"
	"github.com/raimbekalish/faultline/internal/toxiproxy"
)

type proxyState struct {
	Toxics []json.RawMessage `json:"toxics"`
}

func proxyStates(t *testing.T) map[string]proxyState {
	t.Helper()
	c := localhttp.New(time.Second)
	defer c.CloseIdleConnections()
	r, err := c.Get(toxiproxy.Endpoint + "/proxies")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		t.Fatalf("list proxies: %s", r.Status)
	}
	var states map[string]proxyState
	if err := json.NewDecoder(r.Body).Decode(&states); err != nil {
		t.Fatal(err)
	}
	return states
}

// This proxy represents another owner's resource. Faultline must leave it alone.
func sentinel(t *testing.T) {
	t.Helper()
	c := localhttp.New(time.Second)
	body := bytes.NewBufferString(`{"name":"other-owner","listen":"0.0.0.0:8667","upstream":"dependency:8080","enabled":true}`)
	r, err := c.Post(toxiproxy.Endpoint+"/proxies", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Body.Close()
	if r.StatusCode != 201 {
		t.Fatalf("create sentinel: %s", r.Status)
	}
	t.Cleanup(func() {
		defer c.CloseIdleConnections()
		req, _ := http.NewRequest(http.MethodDelete, toxiproxy.Endpoint+"/proxies/other-owner", nil)
		r, err := c.Do(req)
		if err != nil {
			t.Error(err)
			return
		}
		_ = r.Body.Close()
		if r.StatusCode != 204 {
			t.Errorf("delete sentinel: %s", r.Status)
		}
	})
}

func assertOnlySentinel(t *testing.T) {
	t.Helper()
	states := proxyStates(t)
	if _, ok := states["other-owner"]; !ok || len(states) != 1 {
		t.Fatalf("leaked proxy or modified unrelated resource: %+v", states)
	}
}

func TestRealLatencyAndRecovery(t *testing.T) {
	sentinel(t)
	for _, tc := range []struct {
		latency  int
		failures int
	}{{80, 0}, {400, 5}} {
		t.Run(fmt.Sprintf("latency_%dms", tc.latency), func(t *testing.T) {
			cfg := Config{Samples: 5, LatencyMS: tc.latency, RequestTimeoutMS: 1000}
			r, err := Run(context.Background(), cfg, "integration-test")
			if err != nil {
				t.Fatalf("%v: %+v", err, r)
			}
			if r.Status != "complete" || !r.CleanupComplete {
				t.Fatalf("incomplete: %+v", r)
			}
			for _, phase := range []Phase{r.Baseline, r.Fault, r.Recovery} {
				for _, sample := range phase.Samples {
					if sample.TraceID != "" || sample.SpanID != "" {
						t.Fatal("disabled engine emitted trace IDs")
					}
				}
			}
			if r.Baseline.Summary.Failures != 0 || r.Recovery.Summary.Failures != 0 || r.Fault.Summary.Failures != tc.failures {
				t.Fatalf("unexpected outcomes: %+v", r)
			}
			if r.Comparison.MeanDeltaMS < 50 {
				t.Fatalf("real fault had insufficient effect: %+v", r.Comparison)
			}
			if r.Fault.Summary.MeanMS-r.Recovery.Summary.MeanMS < 50 {
				t.Fatalf("latency did not recover: %+v", r)
			}
			if tc.failures > 0 {
				for _, s := range r.Fault.Samples {
					if s.Status != 504 {
						t.Fatalf("expected dependency timeout, got %+v", s)
					}
				}
			}
			t.Logf("baseline %.2fms; fault %.2fms (%d failures); recovery %.2fms", r.Baseline.Summary.MeanMS, r.Fault.Summary.MeanMS, r.Fault.Summary.Failures, r.Recovery.Summary.MeanMS)
			assertOnlySentinel(t)
		})
	}
}

func TestRealCancellationCleansActiveFault(t *testing.T) {
	sentinel(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type result struct {
		report Report
		err    error
	}
	done := make(chan result, 1)
	go func() {
		rt, initErr := telemetry.NewFromMode(ctx, "faultline-runner", os.Getenv("FAULTLINE_TRACING"))
		if initErr != nil {
			done <- result{err: initErr}
			return
		}
		r, err := RunWithTelemetry(ctx, Config{Samples: 100, LatencyMS: 100, RequestTimeoutMS: 1000}, "integration-test", rt)
		shutdown, cancel := context.WithTimeout(context.Background(), telemetry.ShutdownTimeout)
		r.Telemetry = rt.Shutdown(shutdown)
		cancel()
		done <- result{r, err}
	}()
	deadline := time.After(10 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case r := <-done:
			t.Fatalf("experiment ended before cancellation: %+v", r)
		case <-deadline:
			t.Fatal("fault never became active")
		case <-ticker.C:
			for name, state := range proxyStates(t) {
				if name != "other-owner" && len(state.Toxics) > 0 {
					cancel()
					select {
					case r := <-done:
						if r.err == nil || r.report.Status != "canceled" || !r.report.CleanupComplete {
							t.Fatalf("cancel result: %+v", r)
						}
					case <-time.After(CleanupTimeout + time.Second):
						t.Fatal("cancellation did not finish within cleanup budget")
					}
					assertOnlySentinel(t)
					return
				}
			}
		}
	}
}
