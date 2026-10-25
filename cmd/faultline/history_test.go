package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
