//go:build integration

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/raimbekalish/faultline/internal/experiment"
	"github.com/raimbekalish/faultline/internal/history"
)

type failingOutput struct{}

func (failingOutput) Write([]byte) (int, error) { return 0, errors.New("output fixture failure") }

func TestRealCLIHistoryRoundTripAndOutputFailure(t *testing.T) {
	dir := t.TempDir()
	specPath := filepath.Join(dir, "spec.json")
	dbPath := filepath.Join(dir, "history.sqlite")
	if err := os.WriteFile(specPath, []byte(`{"schema_version":1,"samples_per_phase":2,"latency_ms":400,"request_timeout_ms":1000}`), 0600); err != nil {
		t.Fatal(err)
	}
	var output, stderr bytes.Buffer
	if code := execute(context.Background(), []string{"run", "-spec", specPath, "-db", dbPath}, &output, &stderr); code != 0 {
		t.Fatalf("run code %d: %s", code, stderr.String())
	}
	var report experiment.Report
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.SchemaVersion != 1 || report.Status != "complete" || !report.CleanupComplete || report.Fault.Summary.Failures != 2 {
		t.Fatalf("legacy report changed: %+v", report)
	}
	output.Reset()
	stderr.Reset()
	if code := execute(context.Background(), []string{"history", "list", "-db", dbPath}, &output, &stderr); code != 0 {
		t.Fatal(stderr.String())
	}
	var entries []history.Entry
	if err := json.Unmarshal(output.Bytes(), &entries); err != nil || len(entries) != 1 {
		t.Fatalf("list: %s %v", output.String(), err)
	}
	for _, args := range [][]string{{"history", "show", "-db", dbPath, report.RunID}, {"history", "compare", "-db", dbPath, entries[0].ID, report.RunID}} {
		output.Reset()
		stderr.Reset()
		if code := execute(context.Background(), args, &output, &stderr); code != 0 || !json.Valid(output.Bytes()) {
			t.Fatalf("query %v: %s", args, stderr.String())
		}
	}
	stderr.Reset()
	if code := execute(context.Background(), []string{"run", "-spec", specPath, "-db", dbPath}, failingOutput{}, &stderr); code != 1 {
		t.Fatalf("output failure hidden: %d", code)
	}
	store, err := history.OpenExisting(context.Background(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	entries, err = store.List(context.Background(), 20)
	if err != nil || len(entries) != 2 || entries[0].Status != "complete" {
		t.Fatalf("output failure lost committed history: %+v %v", entries, err)
	}
}
