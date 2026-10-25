package tracequery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const fixtureID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func sampleTrace() rawTrace {
	r := rawTrace{TraceID: fixtureID, Processes: map[string]rawProcess{
		"runner": {Service: "faultline-runner"}, "frontend": {Service: "faultline-frontend"}, "dependency": {Service: "faultline-dependency"},
	}}
	for i, role := range []struct{ process, kind, operation string }{
		{"runner", "client", "GET /work"}, {"frontend", "server", "GET /work"}, {"frontend", "client", "GET /value"}, {"dependency", "server", "GET /value"},
	} {
		s := rawSpan{TraceID: fixtureID, SpanID: fmt.Sprintf("%016x", i+1), ProcessID: role.process, Operation: role.operation, Duration: 1000, Tags: []rawTag{{Key: "span.kind", Value: role.kind}}}
		if i > 0 {
			s.References = []rawReference{{Type: "CHILD_OF", TraceID: fixtureID, SpanID: r.Spans[i-1].SpanID}}
		}
		r.Spans = append(r.Spans, s)
	}
	return r
}

func traceResponse(t *testing.T, traces ...rawTrace) []byte {
	t.Helper()
	b, err := json.Marshal(struct {
		Data []rawTrace `json:"data"`
	}{traces})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestDecodeSampleTopology(t *testing.T) {
	for _, tc := range []struct {
		name        string
		change      func(*rawTrace)
		complete    bool
		decodeError bool
	}{
		{"valid chain", func(*rawTrace) {}, true, false},
		{"unordered export", func(r *rawTrace) { r.Spans = []rawSpan{r.Spans[3], r.Spans[1], r.Spans[0], r.Spans[2]} }, true, false},
		{"timeout is still complete", func(r *rawTrace) {
			r.Spans[2].Tags = append(r.Spans[2].Tags, rawTag{Key: "error.type", Value: "timeout"})
			r.Spans[1].Tags = append(r.Spans[1].Tags, rawTag{Key: "http.status_code", Value: 504})
		}, true, false},
		{"duplicate IDs across roles", func(r *rawTrace) { r.Spans[3].SpanID = r.Spans[2].SpanID }, false, false},
		{"duplicate span", func(r *rawTrace) { r.Spans[3] = r.Spans[0] }, false, false},
		{"extra span", func(r *rawTrace) { r.Spans = append(r.Spans, r.Spans[3]) }, false, false},
		{"empty trace", func(r *rawTrace) { r.Spans = nil }, false, false},
		{"empty span ID", func(r *rawTrace) { r.Spans[0].SpanID = "" }, false, false},
		{"short span ID", func(r *rawTrace) { r.Spans[0].SpanID = "1234" }, false, false},
		{"nonhex span ID", func(r *rawTrace) { r.Spans[0].SpanID = "xxxxxxxxxxxxxxxx" }, false, false},
		{"zero span ID", func(r *rawTrace) { r.Spans[0].SpanID = "0000000000000000" }, false, false},
		{"zero parent ID", func(r *rawTrace) { r.Spans[1].References[0].SpanID = "0000000000000000" }, false, false},
		{"malformed parent ID", func(r *rawTrace) { r.Spans[1].References[0].SpanID = "bad-parent" }, false, false},
		{"unexpected service", func(r *rawTrace) { r.Processes["dependency"] = rawProcess{Service: "unrelated-service"} }, false, false},
		{"unresolved process", func(r *rawTrace) { delete(r.Processes, "dependency") }, false, false},
		{"unexpected kind", func(r *rawTrace) { r.Spans[3].Tags[0].Value = "client" }, false, false},
		{"missing kind", func(r *rawTrace) { r.Spans[3].Tags = nil }, false, false},
		{"duplicate role", func(r *rawTrace) { r.Spans[3].ProcessID = "frontend"; r.Spans[3].Operation = "GET /work" }, false, false},
		{"unexpected operation", func(r *rawTrace) { r.Spans[3].Operation = "GET /healthz" }, false, false},
		{"multiple roots", func(r *rawTrace) { r.Spans[3].References = nil }, false, false},
		{"no root", func(r *rawTrace) {
			r.Spans[0].References = []rawReference{{Type: "CHILD_OF", TraceID: fixtureID, SpanID: r.Spans[3].SpanID}}
		}, false, false},
		{"wrong root", func(r *rawTrace) { r.Spans[0].References = r.Spans[2].References; r.Spans[1].References = nil }, false, false},
		{"skipped parent", func(r *rawTrace) { r.Spans[2].References[0].SpanID = r.Spans[0].SpanID }, false, false},
		{"dangling parent", func(r *rawTrace) { r.Spans[2].References[0].SpanID = "0000000000000099" }, false, false},
		{"self parent", func(r *rawTrace) { r.Spans[2].References[0].SpanID = r.Spans[2].SpanID }, false, false},
		{"disconnected cycle", func(r *rawTrace) { r.Spans[2].References[0].SpanID = r.Spans[3].SpanID }, false, false},
		{"foreign span", func(r *rawTrace) { r.Spans[3].TraceID = strings.Repeat("b", 32) }, false, true},
		{"foreign root reference", func(r *rawTrace) {
			r.Spans[0].References = []rawReference{{Type: "CHILD_OF", TraceID: strings.Repeat("b", 32), SpanID: r.Spans[3].SpanID}}
		}, false, false},
		{"unsupported root reference", func(r *rawTrace) {
			r.Spans[0].References = []rawReference{{Type: "FOLLOWS_FROM", TraceID: fixtureID, SpanID: r.Spans[3].SpanID}}
		}, false, false},
		{"multiple parents", func(r *rawTrace) {
			r.Spans[1].References = append(r.Spans[1].References, rawReference{Type: "CHILD_OF", TraceID: fixtureID, SpanID: r.Spans[3].SpanID})
		}, false, false},
		{"duplicate parent references", func(r *rawTrace) { r.Spans[1].References = append(r.Spans[1].References, r.Spans[1].References[0]) }, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := sampleTrace()
			tc.change(&raw)
			r, err := decode(fixtureID, traceResponse(t, raw))
			if (err != nil) != tc.decodeError || r.Complete != tc.complete {
				t.Fatalf("complete=%v error=%v spans=%+v", r.Complete, err, r.Spans)
			}
			if !tc.decodeError && len(r.Spans) != len(raw.Spans) {
				t.Fatal("discarded or invented observed spans")
			}
			if !tc.decodeError {
				original, err := json.Marshal(raw)
				if err != nil || !bytes.Equal(r.Jaeger, original) {
					t.Fatalf("raw evidence changed: %v", err)
				}
			}
		})
	}
	for missing := range 4 {
		t.Run(fmt.Sprintf("missing role %d", missing), func(t *testing.T) {
			raw := sampleTrace()
			raw.Spans = append(raw.Spans[:missing], raw.Spans[missing+1:]...)
			r, err := decode(fixtureID, traceResponse(t, raw))
			if err != nil || r.Complete || len(r.Spans) != 3 {
				t.Fatalf("partial trace: %+v %v", r.Spans, err)
			}
		})
	}
}

func TestDecodeDoesNotJoinTraceObjects(t *testing.T) {
	first, second := sampleTrace(), sampleTrace()
	first.Spans = first.Spans[:2]
	second.Spans = second.Spans[2:]
	r, err := decode(fixtureID, traceResponse(t, first, second))
	if err == nil || r.Complete || len(r.Spans) != 0 {
		t.Fatalf("joined separate snapshots: %+v %v", r.Spans, err)
	}
}

func TestCollectAcceptsCorrectedTopology(t *testing.T) {
	for _, extra := range []bool{false, true} {
		t.Run(fmt.Sprintf("extra span %v", extra), func(t *testing.T) {
			invalid := sampleTrace()
			if extra {
				invalid.Spans = append(invalid.Spans, invalid.Spans[3])
			} else {
				invalid.Spans[2].References[0].SpanID = invalid.Spans[0].SpanID
			}
			first, corrected := traceResponse(t, invalid), traceResponse(t, sampleTrace())
			var calls atomic.Int32
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if calls.Add(1) == 1 {
					_, _ = w.Write(first)
				} else {
					_, _ = w.Write(corrected)
				}
			}))
			defer s.Close()
			c := New()
			defer c.Close()
			c.base = s.URL
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			records, err := c.Collect(ctx, []string{fixtureID})
			if err != nil || !records[0].Complete || len(records[0].Spans) != 4 || calls.Load() != 2 {
				t.Fatalf("corrected response rejected: %+v %v calls=%d", records[0].Spans, err, calls.Load())
			}
		})
	}
}

func TestCollectKeepsIncompleteEvidence(t *testing.T) {
	for _, tc := range []struct {
		name         string
		secondStatus int
		split        bool
	}{
		{"malformed graph", 200, false}, {"partial then query failure", 500, false}, {"separate partial snapshots", 200, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := sampleTrace()
			if tc.name == "malformed graph" {
				raw.Spans[2].References[0].SpanID = raw.Spans[0].SpanID
			} else {
				raw.Spans = raw.Spans[:2]
			}
			first := traceResponse(t, raw)
			second := first
			if tc.split {
				other := sampleTrace()
				other.Spans = other.Spans[2:]
				second = traceResponse(t, other)
			}
			var calls atomic.Int32
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := calls.Add(1)
				if n == 3 {
					cancel()
					return
				}
				if n == 1 {
					_, _ = w.Write(first)
					return
				}
				w.WriteHeader(tc.secondStatus)
				if tc.secondStatus == 200 {
					_, _ = w.Write(second)
				}
			}))
			defer s.Close()
			c := New()
			defer c.Close()
			c.base = s.URL
			records, err := c.Collect(ctx, []string{fixtureID})
			if err != nil || records[0].Complete || records[0].RetrievalError == "" || len(records[0].Spans) != len(raw.Spans) || calls.Load() != 3 {
				t.Fatalf("partial evidence: %+v %v calls=%d", records[0].Spans, err, calls.Load())
			}
			if tc.split && records[0].Spans[0].SpanID == raw.Spans[0].SpanID {
				t.Fatal("did not retain the actual latest snapshot")
			}
		})
	}
}

func TestCollectCanceledBeforeQueryAndInvalidIDs(t *testing.T) {
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer s.Close()
	c := New()
	defer c.Close()
	c.base = s.URL
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r, err := c.Collect(ctx, []string{fixtureID})
	if err != nil || r[0].Complete || r[0].RetrievalError == "" || calls.Load() != 0 {
		t.Fatalf("canceled collection: %+v %v", r, err)
	}
	if _, err := c.Collect(context.Background(), []string{strings.Repeat("0", 32)}); err == nil {
		t.Fatal("accepted zero trace ID")
	}
}
