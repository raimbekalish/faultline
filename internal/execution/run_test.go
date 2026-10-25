package execution

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/raimbekalish/faultline/internal/experiment"
	"github.com/raimbekalish/faultline/internal/history"
	"github.com/raimbekalish/faultline/internal/spec"
	"github.com/raimbekalish/faultline/internal/telemetry"
)

func fixture(status string) experiment.Report {
	r := experiment.Report{SchemaVersion: 1, RunID: "faultline-0123456789abcdef0123456789abcdef", Status: status, Config: experiment.DefaultConfig(), Target: experiment.Target, StartedAt: time.Now().Add(-time.Second), FinishedAt: time.Now(), CleanupComplete: true, Error: "engine fixture failure"}
	return r
}

func TestCancellationFinalizesWithIndependentContext(t *testing.T) {
	s, err := history.Open(context.Background(), filepath.Join(t.TempDir(), "history.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sp := spec.FromConfig(experiment.DefaultConfig())
	r, err := run(ctx, sp, "test", s, nil, func(ctx context.Context, _ experiment.Config, _ string, _ *telemetry.Runtime) (experiment.Report, error) {
		entries, err := s.List(ctx, 10)
		if err != nil || len(entries) != 1 || entries[0].Status != "running" {
			t.Fatalf("engine started before durable intent: %+v %v", entries, err)
		}
		cancel()
		return fixture("canceled"), context.Canceled
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	e, err := s.Get(context.Background(), r.HistoryID)
	if err != nil || e.Status != "canceled" || len(e.Report) == 0 {
		t.Fatalf("canceled report lost: %+v %v", e, err)
	}
}

func TestIntentFailurePreventsEngine(t *testing.T) {
	s, err := history.Open(context.Background(), filepath.Join(t.TempDir(), "history.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	called := false
	_, err = run(context.Background(), spec.FromConfig(experiment.DefaultConfig()), "test", s, nil, func(context.Context, experiment.Config, string, *telemetry.Runtime) (experiment.Report, error) {
		called = true
		return fixture("failed"), nil
	})
	if err == nil || called {
		t.Fatalf("engine ran after persistence failure: %v", err)
	}
}

func TestFinalPersistenceFailurePreservesEngineErrorAndIntent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.sqlite")
	s, err := history.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("CREATE TRIGGER reject_report BEFORE UPDATE ON runs BEGIN SELECT RAISE(ABORT,'injected write failure'); END"); err != nil {
		t.Fatal(err)
	}
	engineErr := errors.New("engine error")
	out, err := run(context.Background(), spec.FromConfig(experiment.DefaultConfig()), "test", s, nil, func(context.Context, experiment.Config, string, *telemetry.Runtime) (experiment.Report, error) {
		return fixture("failed"), engineErr
	})
	if !errors.Is(err, engineErr) || out.Report.Status != "failed" || !out.Report.CleanupComplete {
		t.Fatalf("engine result changed: %+v %v", out, err)
	}
	e, err := s.Get(context.Background(), out.HistoryID)
	if err != nil || e.Status != "running" || e.Report != nil {
		t.Fatalf("persistence error hid durable intent: %+v %v", e, err)
	}
}

func TestRecordedEnvironmentPersistsWithoutInventingDockerFacts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.sqlite")
	s, err := history.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	sp := spec.FromConfig(experiment.DefaultConfig())
	out, err := run(context.Background(), sp, "development", s, nil, func(context.Context, experiment.Config, string, *telemetry.Runtime) (experiment.Report, error) {
		r := fixture("canceled")
		r.GoVersion = runtime.Version()
		r.Revision = "development"
		return r, context.Canceled
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	e, err := s.Get(context.Background(), out.HistoryID)
	if err != nil {
		t.Fatal(err)
	}
	var r experiment.Report
	if err = json.Unmarshal(e.Report, &r); err != nil {
		t.Fatal(err)
	}
	hash, _ := sp.Hash()
	if r.Environment == nil || r.Environment.SpecHash != hash || r.Environment.Launch != nil || r.Environment.TracingEnabled || r.Environment.RunnerOS != runtime.GOOS {
		t.Fatalf("incorrect recorded facts: %+v", r.Environment)
	}
}
