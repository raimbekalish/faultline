package contracts

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/raimbekalish/faultline/internal/bundle"
	"github.com/raimbekalish/faultline/internal/bundlecli"
)

func TestEvaluationV1Contracts(t *testing.T) {
	for _, tc := range []struct {
		evidence, policy, result string
		code                     int
	}{
		{"complete", "pass", "complete.pass", 0},
		{"complete", "fail", "complete.fail", 3},
		{"canceled", "pass", "canceled.indeterminate", 4},
		{"legacy", "pass", "legacy.pass", 0},
	} {
		t.Run(tc.result, func(t *testing.T) {
			raw, err := os.ReadFile("testdata/v1/" + tc.evidence + ".json")
			if err != nil {
				t.Fatal(err)
			}
			var e bundle.Evidence
			if err := json.Unmarshal(raw, &e); err != nil {
				t.Fatal(err)
			}
			b, err := bundle.Build(context.Background(), e)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "synthetic.flb")
			if err := os.WriteFile(path, b, 0600); err != nil {
				t.Fatal(err)
			}
			var out, stderr bytes.Buffer
			code := bundlecli.Execute(context.Background(), []string{"evaluate", path, "--policy", "testdata/evaluation-v1/" + tc.policy + ".policy.json"}, &out, &stderr)
			if code != tc.code || stderr.Len() != 0 {
				t.Fatal(code, out.String(), stderr.String())
			}
			want, err := os.ReadFile("testdata/evaluation-v1/" + tc.result + ".json")
			if err != nil {
				t.Fatal(err)
			}
			var a, bjson bytes.Buffer
			if err := json.Compact(&a, out.Bytes()); err != nil {
				t.Fatal(err)
			}
			if err := json.Compact(&bjson, want); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(a.Bytes(), bjson.Bytes()) {
				t.Fatalf("evaluation schema/semantics changed; review explicit expectations\ngot %s", out.Bytes())
			}
		})
	}
}
