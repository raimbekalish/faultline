package bundlecli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/raimbekalish/faultline/internal/bundleevaluate"
)

const evaluationPolicy = `{"schema_version":1,"require_complete":true,"require_cleanup":true,"phases":{"baseline":{"max_mean_ms":3},"fault":{"max_failure_rate":0},"recovery":{"min_samples":1,"max_p95_ms":3}}}`

func TestEvaluateCLIStatusesAndNoSideEffects(t *testing.T) {
	dir := t.TempDir()
	good, slow, partial := filepath.Join(dir, "good.flb"), filepath.Join(dir, "slow.flb"), filepath.Join(dir, "canceled.flb")
	p := filepath.Join(dir, "policy.json")
	writeEvidence(t, good, cliEvidence(1, "complete"))
	writeEvidence(t, slow, cliEvidence(5, "complete"))
	writeEvidence(t, partial, cliEvidence(3, "canceled"))
	if err := os.WriteFile(p, []byte(evaluationPolicy), 0400); err != nil {
		t.Fatal(err)
	}
	hashes := map[string][32]byte{}
	for _, path := range []string{good, slow, partial, p} {
		b, _ := os.ReadFile(path)
		hashes[path] = sha256.Sum256(b)
	}
	for _, tc := range []struct {
		path   string
		code   int
		status bundleevaluate.Status
	}{{good, 0, bundleevaluate.Pass}, {slow, 3, bundleevaluate.Fail}, {partial, 4, bundleevaluate.Indeterminate}} {
		var out, stderr bytes.Buffer
		args := []string{"evaluate", tc.path, "--policy", p}
		if code := Execute(context.Background(), args, &out, &stderr); code != tc.code || stderr.Len() != 0 {
			t.Fatal(code, out.String(), stderr.String())
		}
		var r bundleevaluate.Result
		if err := json.Unmarshal(out.Bytes(), &r); err != nil || r.Status != tc.status || r.SchemaVersion != 1 {
			t.Fatal(err, r)
		}
		if strings.Contains(out.String(), dir) || !bytes.HasSuffix(out.Bytes(), []byte("\n")) {
			t.Fatal("paths leaked or JSON framing changed")
		}
		first := append([]byte(nil), out.Bytes()...)
		out.Reset()
		if code := Execute(context.Background(), args, &out, &stderr); code != tc.code || !bytes.Equal(first, out.Bytes()) {
			t.Fatal("nondeterministic CLI output", code)
		}
	}
	for path, hash := range hashes {
		b, _ := os.ReadFile(path)
		if sha256.Sum256(b) != hash {
			t.Fatal("input mutated", path)
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != len(hashes) {
		t.Fatal("evaluation created filesystem state", entries)
	}
}

func TestEvaluateCLIUsageAndInputErrors(t *testing.T) {
	dir := t.TempDir()
	good, p := filepath.Join(dir, "run.flb"), filepath.Join(dir, "policy.json")
	writeEvidence(t, good, cliEvidence(1, "complete"))
	if err := os.WriteFile(p, []byte(evaluationPolicy), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"evaluate"}, {"evaluate", good}, {"evaluate", good, "--policy"},
		{"evaluate", good, "--policy", p, "extra"}, {"evaluate", "--policy", p, good},
		{"evaluate", good, "--policy=" + p}, {"evaluate", good, "--other", p}, {"evaluate", "", "--policy", p},
	} {
		var out, stderr bytes.Buffer
		if code := Execute(context.Background(), args, &out, &stderr); code != 2 || out.Len() != 0 || !strings.Contains(stderr.String(), "evaluate FILE.flb") {
			t.Fatal(args, code, out.String(), stderr.String())
		}
	}
	for _, raw := range []string{"{}", strings.Replace(evaluationPolicy, `"schema_version":1`, `"schema_version":2`, 1), strings.Replace(evaluationPolicy, `"max_mean_ms":3`, `"max_mean_ms":3,"MAX_MEAN_MS":2`, 1), strings.Repeat(" ", bundleevaluate.MaxPolicyBytes+1)} {
		if err := os.WriteFile(p, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		var out, stderr bytes.Buffer
		if code := Execute(context.Background(), []string{"evaluate", good, "--policy", p}, &out, &stderr); code != 1 || out.Len() != 0 || !strings.Contains(stderr.String(), "invalid policy:") {
			t.Fatal(code, out.String(), stderr.String())
		}
	}
	if err := os.WriteFile(p, []byte(evaluationPolicy), 0600); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(dir, "bad.flb")
	b, _ := os.ReadFile(good)
	// Corrupt an actual ZIP payload, not a mocked validation result.
	i := bytes.Index(b, []byte(`"format_version":1`))
	b[i+17] = '2'
	if err := os.WriteFile(bad, b, 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"evaluate", bad, "--policy", p}, {"evaluate", good, "--policy", filepath.Join(dir, "missing")}, {"evaluate", filepath.Join(dir, "missing"), "--policy", p}} {
		var out, stderr bytes.Buffer
		if code := Execute(context.Background(), args, &out, &stderr); code != 1 || out.Len() != 0 || stderr.Len() == 0 {
			t.Fatal(code, out.String(), stderr.String())
		}
	}
	var out, stderr bytes.Buffer
	if code := Execute(canceledContext(), []string{"evaluate", good, "--policy", p}, &out, &stderr); code != 1 || out.Len() != 0 {
		t.Fatal(code, out.String(), stderr.String())
	}
}

type shortOutput struct{}

func (shortOutput) Write(p []byte) (int, error) { return len(p) / 2, nil }

func TestEvaluateOutputFailuresOverridePolicyExitCode(t *testing.T) {
	dir := t.TempDir()
	good, p := filepath.Join(dir, "run.flb"), filepath.Join(dir, "policy.json")
	writeEvidence(t, good, cliEvidence(5, "complete"))
	if err := os.WriteFile(p, []byte(evaluationPolicy), 0600); err != nil {
		t.Fatal(err)
	}
	for _, out := range []io.Writer{brokenOutput{}, shortOutput{}} {
		var stderr bytes.Buffer
		if code := Execute(context.Background(), []string{"evaluate", good, "--policy", p}, out, &stderr); code != 1 || !strings.Contains(stderr.String(), "write evidence result:") {
			t.Fatal(code, stderr.String())
		}
	}
}
