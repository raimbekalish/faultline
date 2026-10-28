package lab

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

const goodDescriptor = `{"schema_version":1,"adapter":"http-work-v1","image_id":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","platform":"linux/arm64","experiment":{"schema_version":1,"samples_per_phase":5,"latency_ms":80,"request_timeout_ms":1000},"tracing":"off"}`

func descriptor(t *testing.T) Descriptor {
	t.Helper()
	d, e := DecodeDescriptor(context.Background(), strings.NewReader(goodDescriptor))
	if e != nil {
		t.Fatal(e)
	}
	return d
}

func TestDescriptorStrictGrammar(t *testing.T) {
	for name, input := range map[string]string{
		"empty": "", "null": "null", "array": "[]", "object": "{}", "trailing": goodDescriptor + "{}",
		"version":           strings.Replace(goodDescriptor, `"schema_version":1`, `"schema_version":2`, 1),
		"version_float":     strings.Replace(goodDescriptor, `"schema_version":1`, `"schema_version":1.0`, 1),
		"case":              strings.Replace(goodDescriptor, `"adapter"`, `"Adapter"`, 1),
		"duplicate":         strings.Replace(goodDescriptor, `"adapter":`, `"adapter":"http-work-v1","adapter":`, 1),
		"escaped_duplicate": strings.Replace(goodDescriptor, `"adapter":`, `"\u0061dapter":"http-work-v1","adapter":`, 1),
		"fold_duplicate":    strings.Replace(goodDescriptor, `"schema_version":1`, `"schema_version":1,"\u017fchema_version":1`, 1),
		"null_tracing":      strings.Replace(goodDescriptor, `"off"`, `null`, 1),
		"missing":           strings.Replace(goodDescriptor, `,"tracing":"off"`, "", 1),
		"trace_on":          strings.Replace(goodDescriptor, `"off"`, `"jaeger"`, 1),
		"platform":          strings.Replace(goodDescriptor, "linux/arm64", "linux/amd64", 1),
		"adapter":           strings.Replace(goodDescriptor, "http-work-v1", "plugin-v1", 1),
		"mutable_tag":       strings.Replace(goodDescriptor, "sha256:"+strings.Repeat("a", 64), "private/app:latest", 1),
		"uppercase_id":      strings.Replace(goodDescriptor, "sha256:"+strings.Repeat("a", 64), "sha256:"+strings.Repeat("A", 64), 1),
		"bad_utf8":          strings.Replace(goodDescriptor, "http-work-v1", "\xff", 1),
		"surrogate":         strings.Replace(goodDescriptor, `"off"`, `"\ud800"`, 1),
		"nested_duplicate":  strings.Replace(goodDescriptor, `"latency_ms":80`, `"latency_ms":80,"LATENCY_MS":80`, 1),
		"nested_unknown":    strings.Replace(goodDescriptor, `"latency_ms":80`, `"latency_ms":80,"target":"http://example.invalid"`, 1),
		"nested_null":       strings.Replace(goodDescriptor, `"latency_ms":80`, `"latency_ms":null`, 1),
		"nested_missing":    strings.Replace(goodDescriptor, `"latency_ms":80,`, "", 1),
		"float":             strings.Replace(goodDescriptor, `"latency_ms":80`, `"latency_ms":80.5`, 1),
		"overflow":          strings.Replace(goodDescriptor, `"latency_ms":80`, `"latency_ms":1e999`, 1),
		"nan":               strings.Replace(goodDescriptor, `"latency_ms":80`, `"latency_ms":NaN`, 1),
		"negative":          strings.Replace(goodDescriptor, `"latency_ms":80`, `"latency_ms":-1`, 1),
		"samples_zero":      strings.Replace(goodDescriptor, `"samples_per_phase":5`, `"samples_per_phase":0`, 1),
		"samples_limit":     strings.Replace(goodDescriptor, `"samples_per_phase":5`, `"samples_per_phase":101`, 1),
		"timeout_limit":     strings.Replace(goodDescriptor, `"request_timeout_ms":1000`, `"request_timeout_ms":5001`, 1),
		"nesting":           strings.Repeat("[", 33) + "0" + strings.Repeat("]", 33),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeDescriptor(context.Background(), strings.NewReader(input)); err == nil {
				t.Fatal("accepted invalid descriptor")
			}
		})
	}
	for _, field := range []string{"target", "url", "ports", "command", "environment", "mounts", "compose_file", "plugins", "docker_host"} {
		b := strings.Replace(goodDescriptor, `"schema_version":1`, `"`+field+`":"SECRET","schema_version":1`, 1)
		if _, err := DecodeDescriptor(context.Background(), strings.NewReader(b)); err == nil {
			t.Fatal("unsafe field accepted", field)
		}
	}
}

type endlessReader struct{ n int }

func (r *endlessReader) Read(b []byte) (int, error) {
	for i := range b {
		b[i] = ' '
	}
	r.n += len(b)
	return len(b), nil
}

type badReader struct{}

func (badReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

type cancelInput struct{ cancel context.CancelFunc }

func (r cancelInput) Read(b []byte) (int, error) { r.cancel(); return copy(b, goodDescriptor), nil }

func TestDescriptorBoundsCancellationAndCanonicalHash(t *testing.T) {
	padding := strings.Repeat(" ", MaxDescriptorBytes-len(goodDescriptor))
	if _, err := DecodeDescriptor(context.Background(), strings.NewReader(goodDescriptor+padding)); err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeDescriptor(context.Background(), strings.NewReader(goodDescriptor+padding+" ")); err == nil {
		t.Fatal("size cap")
	}
	r := &endlessReader{}
	if _, err := DecodeDescriptor(context.Background(), r); err == nil || r.n != MaxDescriptorBytes+1 {
		t.Fatal(err, r.n)
	}
	if _, err := DecodeDescriptor(context.Background(), badReader{}); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := DecodeDescriptor(ctx, strings.NewReader(goodDescriptor)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	if _, err := DecodeDescriptor(ctx, cancelInput{cancel}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	d := descriptor(t)
	before, _ := d.Hash()
	var fields map[string]json.RawMessage
	_ = json.Unmarshal([]byte(goodDescriptor), &fields)
	reordered, _ := json.Marshal(fields)
	again, err := DecodeDescriptor(context.Background(), bytes.NewReader(reordered))
	if err != nil {
		t.Fatal(err)
	}
	after, _ := again.Hash()
	if before != after {
		t.Fatal("field order changed hash")
	}
	d.Experiment.LatencyMS++
	different, _ := d.Hash()
	if before == different {
		t.Fatal("config not bound")
	}
	d.Tracing = "jaeger"
	if _, err := d.Hash(); err == nil {
		t.Fatal("unvalidated constructed descriptor")
	}
	a := Catalog()
	a.Roles[0].Entrypoint[0] = "/unsafe"
	if Catalog().Roles[0].Entrypoint[0] != "/app/service" {
		t.Fatal("shared mutable catalog")
	}
}

func FuzzDescriptor(f *testing.F) {
	for _, s := range []string{goodDescriptor, "{}", "null", `{"schema_version":1,"SCHEMA_VERSION":1}`} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		d, err := DecodeDescriptor(context.Background(), bytes.NewReader(b))
		if err == nil && (d.Validate() != nil || len(b) > MaxDescriptorBytes) {
			t.Fatal("invalid success")
		}
	})
}
