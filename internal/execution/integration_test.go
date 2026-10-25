//go:build integration

package execution

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/raimbekalish/faultline/internal/experiment"
	"github.com/raimbekalish/faultline/internal/history"
	"github.com/raimbekalish/faultline/internal/localhttp"
	"github.com/raimbekalish/faultline/internal/spec"
	"github.com/raimbekalish/faultline/internal/telemetry"
	"github.com/raimbekalish/faultline/internal/toxiproxy"
)

func TestRealRepeatedPersistentExperiment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.sqlite")
	s, err := history.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	sp := spec.FromConfig(experiment.Config{Samples: 3, LatencyMS: 80, RequestTimeoutMS: 1000})
	var outcomes []Outcome
	for range 2 {
		rt, err := telemetry.NewFromMode(context.Background(), "faultline-runner", os.Getenv("FAULTLINE_TRACING"))
		if err != nil {
			t.Fatal(err)
		}
		out, err := Run(context.Background(), sp, "integration", s, rt)
		if err != nil || out.Report.Status != "complete" || !out.Report.CleanupComplete {
			t.Fatalf("persistent experiment: %+v %v", out, err)
		}
		if out.Report.Comparison.MeanDeltaMS < 50 {
			t.Fatalf("real fault effect absent: %+v", out.Report.Comparison)
		}
		outcomes = append(outcomes, out)
	}
	s.Close()
	s, err = history.OpenExisting(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if outcomes[0].HistoryID == outcomes[1].HistoryID || outcomes[0].Report.RunID == outcomes[1].Report.RunID {
		t.Fatal("run IDs reused")
	}
	list, err := s.List(context.Background(), 20)
	if err != nil || len(list) != 2 {
		t.Fatalf("history missing: %+v %v", list, err)
	}
	for _, out := range outcomes {
		e, err := s.Get(context.Background(), out.Report.RunID)
		if err != nil || e.ID != out.HistoryID || e.Status != "complete" {
			t.Fatalf("alias/durable record: %+v %v", e, err)
		}
		var report experiment.Report
		if err := json.Unmarshal(e.Report, &report); err != nil {
			t.Fatal(err)
		}
		if report.Fault.Samples[0].TraceID != out.Report.Fault.Samples[0].TraceID || report.Revision != "integration" {
			t.Fatal("report provenance lost")
		}
		if os.Getenv("FAULTLINE_TRACING") == "jaeger" && report.Fault.Samples[0].TraceID == "" {
			t.Fatal("trace reference missing")
		}
	}
	c, err := s.Compare(context.Background(), outcomes[0].HistoryID, outcomes[1].HistoryID)
	if err != nil || !c.SameSpec {
		t.Fatalf("comparison: %+v %v", c, err)
	}
	t.Logf("two real repeated runs persisted; fault means %.2f/%.2fms; IDs %s %s", outcomes[0].Report.Fault.Summary.MeanMS, outcomes[1].Report.Fault.Summary.MeanMS, outcomes[0].HistoryID, outcomes[1].HistoryID)
}

func TestRealCanceledExperimentIsPersistedAfterCleanup(t *testing.T) {
	s, err := history.Open(context.Background(), filepath.Join(t.TempDir(), "history.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rt, err := telemetry.NewFromMode(ctx, "faultline-runner", os.Getenv("FAULTLINE_TRACING"))
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		out Outcome
		err error
	}
	done := make(chan result, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		out, err := Run(ctx, spec.FromConfig(experiment.Config{Samples: 100, LatencyMS: 80, RequestTimeoutMS: 1000}), "integration", s, rt)
		done <- result{out, err}
	}()
	defer func() {
		cancel()
		select {
		case <-finished:
		case <-time.After(12 * time.Second):
			t.Error("canceled run did not finish")
		}
	}()
	client := localhttp.New(time.Second)
	defer client.CloseIdleConnections()
	deadline := time.After(10 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case r := <-done:
			t.Fatalf("ended before active fault: %+v", r)
		case <-deadline:
			t.Fatal("no active fault observed")
		case <-ticker.C:
			resp, err := client.Get(toxiproxy.Endpoint + "/proxies")
			if err != nil {
				t.Fatal(err)
			}
			var proxies map[string]struct {
				Toxics []json.RawMessage `json:"toxics"`
			}
			err = json.NewDecoder(resp.Body).Decode(&proxies)
			resp.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			active := false
			for _, p := range proxies {
				if len(p.Toxics) > 0 {
					active = true
				}
			}
			if !active {
				continue
			}
			cancel()
			select {
			case r := <-done:
				if r.err == nil || r.out.Report.Status != "canceled" || !r.out.Report.CleanupComplete {
					t.Fatalf("cancellation: %+v", r)
				}
				e, err := s.Get(context.Background(), r.out.HistoryID)
				if err != nil || e.Status != "canceled" || len(e.Report) == 0 {
					t.Fatalf("cancellation not durable: %+v %v", e, err)
				}
				resp, err := client.Get(toxiproxy.Endpoint + "/proxies/" + r.out.Report.RunID)
				if err != nil {
					t.Fatal(err)
				}
				resp.Body.Close()
				if resp.StatusCode != 404 {
					t.Fatal("owned proxy still exists after persistence")
				}
				return
			case <-time.After(12 * time.Second):
				t.Fatal("cancellation/persistence exceeded budgets")
			}
		}
	}
}
