package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/raimbekalish/faultline/internal/bundle"
	"github.com/raimbekalish/faultline/internal/bundlecli"
	"github.com/raimbekalish/faultline/internal/experiment"
	"github.com/raimbekalish/faultline/internal/history"
	"github.com/raimbekalish/faultline/internal/spec"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBundleCLIRealSQLiteAndOfflineInspection(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db := filepath.Join(dir, "history.sqlite")
	s, err := history.Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{}
	sp := spec.FromConfig(experiment.Config{Samples: 1, LatencyMS: 80, RequestTimeoutMS: 1000})
	for i, status := range []string{"complete", "failed", "canceled", "running"} {
		id, err := s.Start(ctx, sp)
		if err != nil {
			t.Fatal(err)
		}
		ids[status] = id
		if status == "running" {
			continue
		}
		phase := experiment.Phase{Samples: []experiment.Sample{{DurationMS: 2, Status: 200}}, Summary: experiment.Summary{Requests: 1, MinMS: 2, MeanMS: 2, P50MS: 2, P95MS: 2, P99MS: 2, MaxMS: 2}}
		r := experiment.Report{SchemaVersion: 1, RunID: fmt.Sprintf("faultline-%032x", i), Status: status, Config: sp.Config(), Target: experiment.Target, StartedAt: time.Now().Add(-time.Second), FinishedAt: time.Now(), CleanupComplete: true, Baseline: phase, Fault: phase, Recovery: phase}
		if status != "complete" {
			r.Error = "interrupted"
			r.Recovery = experiment.Phase{}
		}
		if err = s.Finish(ctx, id, r); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()
	for _, status := range []string{"complete", "failed", "canceled"} {
		t.Run(status, func(t *testing.T) {
			path := filepath.Join(dir, status+".flb")
			var out, stderr bytes.Buffer
			if code := execute(ctx, []string{"bundle", "export", "-db", db, "-out", path, ids[status]}, &out, &stderr); code != 0 {
				t.Fatal(code, stderr.String())
			}
			e, err := bundle.ReadFile(ctx, path)
			if err != nil || e.History.Status != status {
				t.Fatal(e, err)
			}
			// Once exported, inspection works while the database path does not exist.
			if err = os.Rename(db, db+".away"); err != nil {
				t.Fatal(err)
			}
			out.Reset()
			stderr.Reset()
			code := bundlecli.Execute(ctx, []string{"inspect", path}, &out, &stderr)
			_ = os.Rename(db+".away", db)
			if code != 0 || !json.Valid(out.Bytes()) || !strings.Contains(out.String(), "Environment provenance was not recorded") {
				t.Fatal(code, out.String(), stderr.String())
			}
			before, _ := os.ReadFile(path)
			out.Reset()
			stderr.Reset()
			if code = executeBundle(ctx, []string{"export", "-db", db, "-out", path, ids[status]}, &out, &stderr); code != 1 {
				t.Fatal("overwritten", code)
			}
			after, _ := os.ReadFile(path)
			if !bytes.Equal(before, after) {
				t.Fatal("existing bundle changed")
			}
		})
	}
	var out, stderr bytes.Buffer
	path := filepath.Join(dir, "unfinished.flb")
	if code := executeBundle(ctx, []string{"export", "-db", db, "-out", path, ids["running"]}, &out, &stderr); code != 1 || !strings.Contains(stderr.String(), "no finalized report") {
		t.Fatal(code, stderr.String())
	}
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("unfinished report fabricated")
	}
	path = filepath.Join(dir, "output-failure.flb")
	stderr.Reset()
	if code := executeBundle(ctx, []string{"export", "-db", db, "-out", path, ids["complete"]}, brokenWriter{}, &stderr); code != 1 {
		t.Fatal(code)
	}
	if _, err = bundle.ReadFile(ctx, path); err != nil {
		t.Fatal("output failure removed published evidence", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	path = filepath.Join(dir, "canceled-operation.flb")
	if code := executeBundle(canceled, []string{"export", "-db", db, "-out", path, ids["complete"]}, &out, &stderr); code != 1 {
		t.Fatal(code)
	}
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("canceled export published")
	}
}
