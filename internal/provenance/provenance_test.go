package provenance

import (
	"bytes"
	"encoding/json"
	"runtime"
	"strings"
	"testing"
)

func TestCaptureAllowlistedFacts(t *testing.T) {
	r := Capture(strings.Repeat("a", 64), "development", runtime.Version(), false, nil)
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	if r.RunnerOS != runtime.GOOS || r.RunnerArch != runtime.GOARCH || r.Launch != nil {
		t.Fatal(r)
	}
	b, _ := json.Marshal(r)
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(b, &fields)
	allowed := map[string]bool{"schema_version": true, "spec_hash": true, "faultline_revision": true, "go_version": true, "runner_os": true, "runner_arch": true, "measurement_methodology": true, "tracing_enabled": true}
	for key := range fields {
		if !allowed[key] {
			t.Fatalf("unexpected provenance field %q", key)
		}
	}
	r.Methodology = "unknown"
	if r.Validate() == nil {
		t.Fatal("unknown methodology accepted")
	}
}
func TestLaunchStrictBoundsAndMissingData(t *testing.T) {
	for _, s := range []string{`{"schema_version":2}`, `{"schema_version":1,"SCHEMA_VERSION":1}`, `{"schema_version":1,"docker_server_version":"29.0.0","images":[],"hostname":"secret"}`, strings.Repeat("x", 8193)} {
		if _, err := DecodeLaunch(strings.NewReader(s)); err == nil {
			t.Fatal("invalid launch accepted")
		}
	}
	l := &Launch{1, "29.0.0", []Image{{Component: "sample", ImageID: "sha256:" + strings.Repeat("a", 64)}, {Component: "toxiproxy", ImageID: "sha256:" + strings.Repeat("b", 64), Reference: "ghcr.io/shopify/toxiproxy:2.12.0@sha256:" + strings.Repeat("c", 64)}}}
	if l.Validate() != nil {
		t.Fatal(l.Validate())
	}
	b, _ := json.Marshal(l)
	if _, err := DecodeLaunch(strings.NewReader(string(b))); err != nil {
		t.Fatal(err)
	}
	r := Capture(strings.Repeat("d", 64), "development", runtime.Version(), false, l)
	l.Images[0].ImageID = "mutated"
	if r.Validate() != nil {
		t.Fatal("capture did not snapshot launch")
	}
}

func TestMissingTracingFactAndUnsupportedFieldsAreRejected(t *testing.T) {
	r := Capture(strings.Repeat("a", 64), "development", runtime.Version(), false, nil)
	b, _ := json.Marshal(r)
	for _, bad := range [][]byte{bytes.Replace(b, []byte(`,"tracing_enabled":false`), nil, 1), bytes.Replace(b, []byte(`"tracing_enabled":false`), []byte(`"tracing_enabled":null`), 1), bytes.Replace(b, []byte(`"schema_version":1`), []byte(`"schema_version":2`), 1), bytes.Replace(b, []byte(`"schema_version":1`), []byte(`"schema_version":1,"hostname":"private"`), 1)} {
		var got Record
		if err := json.Unmarshal(bad, &got); err == nil {
			t.Fatal("accepted incomplete/unsupported provenance", string(bad))
		}
	}
}
