package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/raimbekalish/faultline/internal/experiment"
	"github.com/raimbekalish/faultline/internal/history"
	"github.com/raimbekalish/faultline/internal/spec"
)

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, errors.New("write fixture failure") }

func TestOutputFailureRetainsExecutionError(t *testing.T) {
	t.Setenv("FAULTLINE_TRACING", "off")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var stderr bytes.Buffer
	code := execute(ctx, []string{"run"}, brokenWriter{}, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "write report") || !strings.Contains(stderr.String(), "context canceled") {
		t.Fatalf("lost execution/cleanup error: code=%d %s", code, stderr.String())
	}
}

func TestSpecificationAndHistoryUsageValidation(t *testing.T) {
	t.Setenv("FAULTLINE_TRACING", "off")
	path := filepath.Join(t.TempDir(), "spec.json")
	if err := os.WriteFile(path, []byte(`{"schema_version":1,"samples_per_phase":1,"latency_ms":80,"request_timeout_ms":1000}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"run", "-spec", path, "-samples", "1"},
		{"history"}, {"history", "unknown"}, {"history", "list", "-limit", "101"}, {"history", "show"}, {"history", "compare", "only-one"},
	} {
		var out, stderr bytes.Buffer
		if code := execute(context.Background(), args, &out, &stderr); code != 2 || out.Len() != 0 {
			t.Fatalf("args=%v code=%d out=%s err=%s", args, code, out.String(), stderr.String())
		}
	}
	missing := filepath.Join(t.TempDir(), "missing.sqlite")
	var out, stderr bytes.Buffer
	if code := execute(context.Background(), []string{"history", "list", "-db", missing}, &out, &stderr); code != 1 {
		t.Fatalf("missing history code %d", code)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("query created a missing database")
	}
}

func TestHistoryCLIRejectsInconsistentStoredStatistics(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "history.sqlite")
	s, err := history.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	sp := spec.FromConfig(experiment.Config{Samples: 2, LatencyMS: 80, RequestTimeoutMS: 1000})
	phase := experiment.Phase{Samples: []experiment.Sample{{DurationMS: 1, Status: 200}, {DurationMS: 5, Status: 200}}, Summary: experiment.Summary{Requests: 2, MinMS: 1, MeanMS: 3, P50MS: 1, P95MS: 5, P99MS: 5, MaxMS: 5}}
	r := experiment.Report{SchemaVersion: 1, RunID: "faultline-0123456789abcdef0123456789abcdef", Status: "complete", Config: sp.Config(), Target: experiment.Target, StartedAt: time.Now().Add(-time.Second), FinishedAt: time.Now(), CleanupComplete: true, Baseline: phase, Fault: phase, Recovery: phase}
	id, err := s.Start(ctx, sp)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Finish(ctx, id, r); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("UPDATE runs SET report_json=json_set(report_json,'$.fault.summary.mean_ms',2) WHERE id=?", id); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"history", "list", "-db", path}, {"history", "show", "-db", path, id}, {"history", "compare", "-db", path, id, id}} {
		var out, stderr bytes.Buffer
		if code := execute(ctx, args, &out, &stderr); code != 1 || out.Len() != 0 || !strings.Contains(stderr.String(), history.ErrCorrupt.Error()) {
			t.Fatalf("accepted corrupt history: args=%v code=%d stdout=%s stderr=%s", args, code, out.String(), stderr.String())
		}
	}
}
