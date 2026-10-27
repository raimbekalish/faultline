package bundleevaluate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validPolicy = `{"schema_version":1,"require_complete":true,"require_cleanup":true,"phases":{"baseline":{"max_failure_rate":0,"max_p95_ms":50},"fault":{"min_failure_rate":0.5},"recovery":{"max_failure_rate":0,"max_mean_ms":50}}}`

func policy(t *testing.T, input string) Policy {
	t.Helper()
	p, err := Decode(context.Background(), strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPolicyStrictDecoding(t *testing.T) {
	for name, input := range map[string]string{
		"empty": ``, "null": `null`, "array": `[]`, "empty_object": `{}`,
		"flags_only":             `{"schema_version":1,"require_complete":true,"require_cleanup":true,"phases":{}}`,
		"unsupported":            strings.Replace(validPolicy, `"schema_version":1`, `"schema_version":2`, 1),
		"fraction_version":       strings.Replace(validPolicy, `"schema_version":1`, `"schema_version":1.0`, 1),
		"missing_complete":       strings.Replace(validPolicy, `"require_complete":true,`, ``, 1),
		"disabled_cleanup":       strings.Replace(validPolicy, `"require_cleanup":true`, `"require_cleanup":false`, 1),
		"null_flag":              strings.Replace(validPolicy, `"require_cleanup":true`, `"require_cleanup":null`, 1),
		"null_phases":            `{"schema_version":1,"require_complete":true,"require_cleanup":true,"phases":null}`,
		"unknown":                strings.Replace(validPolicy, `"schema_version":1`, `"schema_version":1,"target":"https://example.invalid"`, 1),
		"case_alias":             strings.Replace(validPolicy, `"schema_version"`, `"SCHEMA_VERSION"`, 1),
		"phase_alias":            strings.Replace(validPolicy, `"baseline"`, `"BASELINE"`, 1),
		"check_alias":            strings.Replace(validPolicy, `"max_p95_ms"`, `"MAX_P95_MS"`, 1),
		"unknown_phase":          strings.Replace(validPolicy, `"baseline"`, `"other"`, 1),
		"unknown_check":          strings.Replace(validPolicy, `"max_p95_ms"`, `"max_p99_ms"`, 1),
		"empty_phase":            strings.Replace(validPolicy, `"min_failure_rate":0.5`, ``, 1),
		"null_check":             strings.Replace(validPolicy, `"max_p95_ms":50`, `"max_p95_ms":null`, 1),
		"null_phase":             strings.Replace(validPolicy, `{"min_failure_rate":0.5}`, `null`, 1),
		"negative_latency":       strings.Replace(validPolicy, `"max_p95_ms":50`, `"max_p95_ms":-1`, 1),
		"nan":                    strings.Replace(validPolicy, `"max_p95_ms":50`, `"max_p95_ms":NaN`, 1),
		"infinity":               strings.Replace(validPolicy, `"max_p95_ms":50`, `"max_p95_ms":Infinity`, 1),
		"overflow":               strings.Replace(validPolicy, `"max_p95_ms":50`, `"max_p95_ms":1e999`, 1),
		"underflow":              strings.Replace(validPolicy, `"min_failure_rate":0.5`, `"min_failure_rate":1e-999`, 1),
		"string_number":          strings.Replace(validPolicy, `"min_failure_rate":0.5`, `"min_failure_rate":"0.5"`, 1),
		"rate_above_one":         strings.Replace(validPolicy, `"min_failure_rate":0.5`, `"min_failure_rate":1.01`, 1),
		"negative_rate":          strings.Replace(validPolicy, `"min_failure_rate":0.5`, `"min_failure_rate":-0.1`, 1),
		"conflict":               strings.Replace(validPolicy, `"min_failure_rate":0.5`, `"min_failure_rate":0.5,"max_failure_rate":0.4`, 1),
		"zero_samples":           strings.Replace(validPolicy, `"min_failure_rate":0.5`, `"min_samples":0`, 1),
		"too_many_samples":       strings.Replace(validPolicy, `"min_failure_rate":0.5`, `"min_samples":101`, 1),
		"overflow_samples":       strings.Replace(validPolicy, `"min_failure_rate":0.5`, `"min_samples":18446744073709551616`, 1),
		"fraction_samples":       strings.Replace(validPolicy, `"min_failure_rate":0.5`, `"min_samples":1.5`, 1),
		"exponent_samples":       strings.Replace(validPolicy, `"min_failure_rate":0.5`, `"min_samples":1e0`, 1),
		"duplicate":              strings.Replace(validPolicy, `"schema_version":1`, `"schema_version":1,"schema_version":1`, 1),
		"escaped_duplicate":      strings.Replace(validPolicy, `"schema_version":1`, `"schema_version":1,"\u0073chema_version":1`, 1),
		"case_duplicate":         strings.Replace(validPolicy, `"schema_version":1`, `"schema_version":1,"SCHEMA_VERSION":1`, 1),
		"unicode_fold_duplicate": strings.Replace(validPolicy, `"phases":`, `"phase\u017f":{},"phases":`, 1),
		"nested_duplicate":       strings.Replace(validPolicy, `"min_failure_rate":0.5`, `"min_failure_rate":0.5,"MIN_FAILURE_RATE":0.5`, 1),
		"surrogate":              strings.Replace(validPolicy, `"schema_version"`, `"\ud800"`, 1),
		"bad_utf8":               strings.Replace(validPolicy, `"schema_version"`, "\"\xff\"", 1),
		"trailing":               validPolicy + `{}`,
		"excessive_nesting":      strings.Repeat("[", 33) + `0` + strings.Repeat("]", 33),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Decode(context.Background(), strings.NewReader(input)); err == nil {
				t.Fatal("invalid policy accepted", input)
			}
		})
	}
	for _, raw := range []string{validPolicy,
		strings.Replace(validPolicy, `"max_p95_ms":50`, `"max_p95_ms":1.7976931348623157e308`, 1),
		strings.Replace(validPolicy, `"max_p95_ms":50`, `"max_p95_ms":5e-324`, 1),
		strings.Replace(validPolicy, `"min_failure_rate":0.5`, `"min_failure_rate":0,"max_failure_rate":1,"min_samples":100`, 1),
		strings.Replace(validPolicy, `"max_p95_ms":50`, `"max_p95_ms":-0`, 1),
		strings.Replace(validPolicy, `"schema_version"`, `"\u0073chema_version"`, 1),
	} {
		p := policy(t, raw)
		b, err := json.Marshal(p)
		if err != nil || !json.Valid(b) {
			t.Fatal(err)
		}
		policy(t, string(b))
	}
}

func TestPolicyInMemoryValidation(t *testing.T) {
	for _, bad := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), -1} {
		p := policy(t, validPolicy)
		p.Phases.Baseline.MaxMeanMS = &bad
		if err := p.Validate(); err == nil {
			t.Fatal("invalid constructed policy accepted", bad)
		}
	}
}

type countingReader struct{ n int }

func (r *countingReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = ' '
	}
	r.n += len(p)
	return len(p), nil
}

type cancelReader struct{ cancel context.CancelFunc }

func (r cancelReader) Read(p []byte) (int, error) {
	r.cancel()
	return copy(p, validPolicy), nil
}

type failedReader struct{}

func (failedReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestPolicyReadBoundsAndCancellation(t *testing.T) {
	padding := strings.Repeat(" ", MaxPolicyBytes-len(validPolicy))
	policy(t, validPolicy+padding)
	if _, err := Decode(context.Background(), strings.NewReader(validPolicy+padding+" ")); err == nil {
		t.Fatal("oversized policy accepted")
	}
	r := &countingReader{}
	if _, err := Decode(context.Background(), r); err == nil || r.n != MaxPolicyBytes+1 {
		t.Fatal(err, r.n)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Decode(ctx, strings.NewReader(validPolicy)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	if _, err := Decode(ctx, cancelReader{cancel}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := Decode(context.Background(), failedReader{}); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatal(err)
	}
}

func TestPolicyRegularFilesOnlyAndNoWrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.json")
	if err := os.WriteFile(path, []byte(validPolicy), 0400); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", dir, link, filepath.Join(dir, "missing"), "https://example.invalid/policy.json"} {
		if _, err := ReadFile(context.Background(), bad); err == nil {
			t.Fatal("unsafe file accepted", bad)
		}
	}
	if _, err := ReadFile(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ReadFile(ctx, path); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	entries, _ := os.ReadDir(dir)
	if !bytes.Equal(got, []byte(validPolicy)) || len(entries) != 2 {
		t.Fatal("policy read changed the filesystem")
	}
	large := filepath.Join(dir, "large.json")
	if err := os.WriteFile(large, bytes.Repeat([]byte(" "), MaxPolicyBytes+1), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFile(context.Background(), large); err == nil {
		t.Fatal("oversized file accepted")
	}
}

func FuzzDecode(f *testing.F) {
	for _, seed := range []string{validPolicy, `{}`, `null`, `{"schema_version":1,"SCHEMA_VERSION":1}`, `{"x":"\ud800"}`} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		p, err := Decode(context.Background(), bytes.NewReader(raw))
		if err == nil {
			if p.Validate() != nil || len(raw) > MaxPolicyBytes {
				t.Fatal("invalid success")
			}
		}
	})
}
