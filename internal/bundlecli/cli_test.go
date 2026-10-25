package bundlecli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/raimbekalish/faultline/internal/bundle"
	"github.com/raimbekalish/faultline/internal/bundlecompare"
	"github.com/raimbekalish/faultline/internal/experiment"
	"github.com/raimbekalish/faultline/internal/spec"
)

func cliEvidence(n int, status string) bundle.Evidence {
	sp := spec.FromConfig(experiment.Config{Samples: 1, LatencyMS: 80, RequestTimeoutMS: 1000})
	at := time.Date(2026, 10, 25, 1, 0, 0, 0, time.UTC)
	p := experiment.Phase{Samples: []experiment.Sample{{DurationMS: float64(n), Status: 200}}}
	p.Summary = experiment.Summarize(p.Samples)
	r := experiment.Report{SchemaVersion: 1, RunID: fmt.Sprintf("faultline-%032x", n), Status: status, StartedAt: at, FinishedAt: at.Add(time.Second), Config: sp.Config(), Target: experiment.Target, CleanupComplete: true, Baseline: p, Fault: p, Recovery: p}
	if status != "complete" {
		r.Error = "canceled"
		r.Recovery = experiment.Phase{}
	}
	hash, _ := sp.Hash()
	b, _ := json.Marshal(r)
	return bundle.Evidence{History: bundle.Metadata{ID: fmt.Sprintf("history-%032x", n), RunID: r.RunID, SpecHash: hash, CreatedAt: at, FinishedAt: at.Add(2 * time.Second), Status: status}, Spec: sp, Report: b}
}
func writeEvidence(t *testing.T, path string, e bundle.Evidence) {
	t.Helper()
	b, err := bundle.Build(context.Background(), e)
	if err != nil {
		t.Fatal(err)
	}
	if err = bundle.Save(context.Background(), path, b); err != nil {
		t.Fatal(err)
	}
}
func TestCompareValidFilesAndNonComparableOutput(t *testing.T) {
	dir := t.TempDir()
	left, right, partial := filepath.Join(dir, "left.flb"), filepath.Join(dir, "right.flb"), filepath.Join(dir, "canceled.flb")
	writeEvidence(t, left, cliEvidence(1, "complete"))
	writeEvidence(t, right, cliEvidence(5, "complete"))
	writeEvidence(t, partial, cliEvidence(3, "canceled"))
	var out, stderr bytes.Buffer
	if code := Execute(context.Background(), []string{"compare", left, right}, &out, &stderr); code != 0 {
		t.Fatal(code, stderr.String())
	}
	first := append([]byte(nil), out.Bytes()...)
	var result bundlecompare.Result
	if err := json.Unmarshal(first, &result); err != nil {
		t.Fatal(err)
	}
	if result.SchemaVersion != 1 || result.Phases[1].Changes.Mean.Absolute != 4 || result.Assessment.Classification != "descriptive_only" {
		t.Fatal(result)
	}
	out.Reset()
	if code := Execute(context.Background(), []string{"compare", left, right}, &out, &stderr); code != 0 || !bytes.Equal(out.Bytes(), first) {
		t.Fatal("unstable output", code)
	}
	if strings.Contains(out.String(), dir) {
		t.Fatal("input paths leaked into comparison")
	}
	out.Reset()
	if code := Execute(context.Background(), []string{"compare", left, partial}, &out, &stderr); code != 0 {
		t.Fatal(code, stderr.String())
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Assessment.Classification != "not_comparable" || result.Phases[2].Right.Summary != nil {
		t.Fatal(result)
	}
}
func TestBothInputsValidatedAndFailuresNeverProduceResult(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.flb")
	bad := filepath.Join(dir, "bad.flb")
	writeEvidence(t, good, cliEvidence(1, "complete"))
	b, _ := os.ReadFile(good)
	for _, kind := range []string{"corrupt", "unsupported", "malformed_metadata", "oversized"} {
		t.Run(kind, func(t *testing.T) {
			content := append([]byte(nil), b...)
			switch kind {
			case "corrupt":
				i := bytes.Index(content, []byte(`"mean_ms":1`))
				content[i+10] = '2'
			case "unsupported":
				content = bytes.Replace(content, []byte(`"format_version":1`), []byte(`"format_version":2`), 1)
			case "malformed_metadata":
				content = bytes.Replace(content, []byte(`"status":"complete"`), []byte(`"status":"impostor"`), 1)
			case "oversized":
				content = bytes.Repeat([]byte("x"), bundle.MaxArchive+1)
			}
			if err := os.WriteFile(bad, content, 0600); err != nil {
				t.Fatal(err)
			}
			for _, pair := range [][]string{{bad, good}, {good, bad}} {
				var out, stderr bytes.Buffer
				if code := Execute(context.Background(), append([]string{"compare"}, pair...), &out, &stderr); code != 1 || out.Len() != 0 || !strings.Contains(stderr.String(), "invalid ") {
					t.Fatal(code, out.String(), stderr.String())
				}
			}
		})
	}
	var out, stderr bytes.Buffer
	for _, args := range [][]string{{}, {"compare", good}, {"compare", good, good, good}, {"compare", good, filepath.Join(dir, "missing")}} {
		code := Execute(context.Background(), args, &out, &stderr)
		if code == 0 || out.Len() != 0 {
			t.Fatal(args, code)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if code := Execute(ctx, []string{"compare", good, good}, &out, &stderr); code != 1 || out.Len() != 0 {
		t.Fatal(code)
	}
	files, _ := os.ReadDir(dir)
	if len(files) != 2 {
		t.Fatal("comparison created output files", files)
	}
}

type brokenOutput struct{}

func (brokenOutput) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
func TestCompareOutputFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "run.flb")
	writeEvidence(t, path, cliEvidence(1, "complete"))
	var stderr bytes.Buffer
	if code := Execute(context.Background(), []string{"compare", path, path}, brokenOutput{}, &stderr); code != 1 || !strings.Contains(stderr.String(), io.ErrClosedPipe.Error()) {
		t.Fatal(code, stderr.String())
	}
}
func TestLegacyVerifyAndInspectRemainCompatible(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "run.flb")
	writeEvidence(t, path, cliEvidence(1, "complete"))
	for _, cmd := range []string{"verify", "inspect"} {
		var out, stderr bytes.Buffer
		if code := Execute(context.Background(), []string{cmd, path}, &out, &stderr); code != 0 || !json.Valid(out.Bytes()) {
			t.Fatal(code, stderr.String())
		}
	}
	if _, err := bundle.ReadFile(canceledContext(), path); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func canceledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}
