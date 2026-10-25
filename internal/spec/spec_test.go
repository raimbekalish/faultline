package spec

import (
	"strings"
	"testing"
)

const good = `{"schema_version":1,"samples_per_phase":5,"latency_ms":80,"request_timeout_ms":1000}`

func TestStrictSpec(t *testing.T) {
	for _, bad := range []string{
		`null`, `[]`, `{}`, good + ` {}`, good + ` junk`, strings.Replace(good, `"schema_version":1`, `"schema_version":2`, 1),
		strings.Replace(good, `"schema_version":1`, `"schema_version":1,"schema_version":1`, 1),
		strings.Replace(good, `"schema_version"`, `"Schema_version"`, 1),
		strings.Replace(good, `"schema_version":1,`, ``, 1),
		strings.Replace(good, `"latency_ms":80`, `"latency_ms":null`, 1),
		strings.Replace(good, `"latency_ms":80`, `"latency_ms":"80"`, 1),
		strings.Replace(good, `"latency_ms":80`, `"latency_ms":80.5`, 1),
		strings.Replace(good, `"latency_ms":80`, `"latency_ms":0`, 1),
		strings.Replace(good, `"samples_per_phase":5`, `"samples_per_phase":101`, 1),
		strings.Replace(good, `"request_timeout_ms":1000`, `"request_timeout_ms":9`, 1),
		strings.Replace(good, `}`, `,"target":"https://production.invalid"}`, 1), strings.Repeat(" ", MaxBytes+1),
	} {
		if _, err := Decode(strings.NewReader(bad)); err == nil {
			t.Fatalf("accepted invalid input: %s", bad[:min(len(bad), 150)])
		}
	}
}

func TestDeterministicSnapshot(t *testing.T) {
	a, err := Decode(strings.NewReader(good))
	if err != nil {
		t.Fatal(err)
	}
	b, err := Decode(strings.NewReader(`{ "request_timeout_ms":1000,"latency_ms":80,"samples_per_phase":5,"schema_version":1 }`))
	if err != nil {
		t.Fatal(err)
	}
	ca, _ := a.Canonical()
	cb, _ := b.Canonical()
	ha, _ := a.Hash()
	hb, _ := b.Hash()
	if string(ca) != good || string(ca) != string(cb) || ha != hb || a.Config() != b.Config() {
		t.Fatalf("non-deterministic snapshots: %s %s", ca, cb)
	}
}
