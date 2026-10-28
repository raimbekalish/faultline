package labprotocol

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/raimbekalish/faultline/internal/spec"
	"io"
	"strings"
	"testing"
)

func fixture(t *testing.T) []byte {
	t.Helper()
	measurement, e := spec.Decode(strings.NewReader(`{"schema_version":1,"samples_per_phase":5,"latency_ms":80,"request_timeout_ms":1000}`))
	if e != nil {
		t.Fatal(e)
	}
	hash, _ := measurement.Hash()
	b, _ := json.Marshal(Request{1, "faultline-worker-preflight", "faultline-lab-" + strings.Repeat("a", 32), "faultline-" + strings.Repeat("b", 32), hash, measurement, "off"})
	return b
}
func TestStrictPreflightAndNoFabricatedMeasurements(t *testing.T) {
	b := fixture(t)
	var out bytes.Buffer
	if e := Preflight(context.Background(), bytes.NewReader(b), &out, strings.Repeat("c", 40)); e != nil {
		t.Fatal(e)
	}
	var response Response
	if json.Unmarshal(out.Bytes(), &response) != nil || response.ExecutionAvailable || response.Measurements != nil || response.Status != "validated_preflight_only" || len(response.Capabilities) != 1 {
		t.Fatal(out.String())
	}
	again := bytes.Buffer{}
	_ = Preflight(context.Background(), bytes.NewReader(b), &again, strings.Repeat("c", 40))
	if !bytes.Equal(out.Bytes(), again.Bytes()) {
		t.Fatal("non deterministic")
	}
	for _, bad := range [][]byte{[]byte(`{}`), []byte(`null`), append(b, []byte(`{}`)...), bytes.Replace(b, []byte(`"schema_version":1`), []byte(`"schema_version":2`), 1), bytes.Replace(b, []byte(`"kind":`), []byte(`"Kind":"ignored","kind":`), 1), bytes.Replace(b, []byte(`"kind":`), []byte(`"\u006bind":"ignored","kind":`), 1), bytes.Replace(b, []byte(`"samples_per_phase":5`), []byte(`"samples_per_phase":null`), 1), bytes.Replace(b, []byte(`"samples_per_phase":5`), []byte(`"samples_per_phase":5.0`), 1), bytes.Replace(b, []byte(`"off"`), []byte(`"jaeger"`), 1), bytes.Repeat([]byte("x"), MaxInput+1), []byte(`{"x":"\ud800"}`), []byte("\xff")} {
		if _, e := Decode(context.Background(), bytes.NewReader(bad)); e == nil {
			t.Fatal("invalid accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := Decode(ctx, bytes.NewReader(b)); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	if e := Preflight(context.Background(), bytes.NewReader(b), shortWriter{}, "development"); !errors.Is(e, io.ErrShortWrite) {
		t.Fatal(e)
	}
}

type shortWriter struct{}

func (shortWriter) Write(b []byte) (int, error) { return len(b) / 2, nil }
