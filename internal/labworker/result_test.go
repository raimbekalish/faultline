package labworker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"go/parser"
	"go/token"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func realSmallReport(t *testing.T) Report {
	t.Helper()
	l := newLocalLab(t)
	c := configuration(1, 2, 1000)
	r, e := run(context.Background(), c, clientFor(t, l.frontend, 1000), fakeFor(c, l))
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func copyReport(r Report) Report {
	b, _ := json.Marshal(r)
	var out Report
	_ = json.Unmarshal(b, &out)
	return out
}
func TestStrictResultSemanticsAndSerialization(t *testing.T) {
	r := realSmallReport(t)
	validReport(t, r)
	for name, alter := range map[string]func(*Report){
		"schema": func(r *Report) { r.SchemaVersion = 2 }, "kind": func(r *Report) { r.Kind = "sample-report" }, "qualified": func(r *Report) { r.RuntimeQualified = true },
		"mean": func(r *Report) { r.Baseline.Summary.MeanMS += .01 }, "p95": func(r *Report) { r.Fault.Summary.P95MS += .01 }, "counts": func(r *Report) { r.Fault.Summary.Requests++ }, "failures": func(r *Report) { r.Fault.Summary.Failures++ }, "rate": func(r *Report) { r.Fault.Summary.FailureRate = .5 },
		"missing-sample": func(r *Report) { r.Recovery.Samples = r.Recovery.Samples[:1] }, "missing-phase": func(r *Report) { r.Fault = emptyPhase() }, "missing-summary": func(r *Report) { r.Baseline.Summary = nil },
		"cleanup": func(r *Report) { r.FaultCleanup.Status = "unconfirmed" }, "unknown-apply": func(r *Report) { r.AppliedFault.Application = "unknown" }, "foreign-owner": func(r *Report) { r.AppliedFault.Intent.RunID = "foreign" },
		"nan": func(r *Report) { r.Baseline.Samples[0].DurationMS = math.NaN() }, "infinite": func(r *Report) { r.Baseline.Samples[0].DurationMS = math.Inf(1) }, "negative": func(r *Report) { r.Baseline.Samples[0].DurationMS = -1 }, "raw-error": func(r *Report) { r.Baseline.Samples[0].Error = "private URL" },
		"canceled-no-reason": func(r *Report) { r.Status = "canceled"; r.Issues = []string{"cleanup_unconfirmed"} }, "fake-whole-cleanup": func(r *Report) { r.WholeLabCleanup = "confirmed" },
	} {
		t.Run(name, func(t *testing.T) {
			bad := copyReport(r)
			alter(&bad)
			if bad.Validate() == nil {
				t.Fatal("inconsistent report accepted")
			}
			if _, e := bad.Encode(); e == nil {
				t.Fatal("inconsistent report encoded")
			}
		})
	}
	b, _ := r.Encode()
	again, _ := r.Encode()
	if !bytes.Equal(b, again) {
		t.Fatal("changed identical data")
	}
	malformed := [][]byte{[]byte(`{}`), []byte(`null`), []byte(`{"x":"\ud800"}`), []byte{255}, append(append([]byte{}, b...), '{'), bytes.Repeat([]byte(" "), MaxResultBytes+1)}
	malformed = append(malformed, bytes.Replace(b, []byte(`"schema_version":1`), []byte(`"schema_version":1,"Schema_Version":1`), 1), bytes.Replace(b, []byte(`"schema_version":1`), []byte(`"\u0073chema_version":1,"schema_version":1`), 1), bytes.Replace(b, []byte(`"kind":`), []byte(`"Kind":`), 1), bytes.Replace(b, []byte(`"runtime_qualified":false`), []byte(`"runtime_qualified":null`), 1), bytes.Replace(b, []byte(`"mean_ms":`), []byte(`"unknown":0,"mean_ms":`), 1))
	for _, bad := range malformed {
		if _, e := Decode(context.Background(), bytes.NewReader(bad)); e == nil {
			t.Fatal("malformed JSON accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := Decode(ctx, bytes.NewReader(b)); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	if _, e := Decode(context.Background(), failingReader{}); e == nil {
		t.Fatal("IO failure")
	}
	partial := initial(configuration(1, 2, 1000))
	partial.Issues = []string{"readiness_failed"}
	partial.Uncertainty = []string{"measurements_incomplete"}
	validReport(t, partial)
	if partial.Baseline.Summary != nil {
		t.Fatal("unavailable was zero")
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func TestConfigurationAndEndpointAreClosed(t *testing.T) {
	c := configuration(80, 1, 1000)
	for _, alter := range []func(*Configuration){func(c *Configuration) { c.Specification.SchemaVersion = 2 }, func(c *Configuration) { c.Specification.Samples = 0 }, func(c *Configuration) { c.Specification.Samples = 101 }, func(c *Configuration) { c.Specification.LatencyMS = 5001 }, func(c *Configuration) { c.Specification.RequestTimeoutMS = 9 }, func(c *Configuration) { c.SourceRevision = "development" }, func(c *Configuration) { c.RunID = "bad" }, func(c *Configuration) { c.SpecificationSHA256 = "bad" }} {
		bad := c
		alter(&bad)
		if bad.validate() == nil {
			t.Fatal("bad config")
		}
		if _, e := run(context.Background(), bad, nil, nil); !errors.Is(e, ErrConfiguration) {
			t.Fatal(e)
		}
	}
	for _, address := range []string{"192.0.2.1:1234", "127.0.0.1:0", "[::]:8080"} {
		addr, _ := netip.ParseAddrPort(address)
		if _, e := newHarnessClient(addr, time.Second); e == nil {
			t.Fatal("non-loopback or zero port accepted")
		}
	}
	for _, ms := range []int{9, 5001} {
		addr := netip.MustParseAddrPort("127.0.0.1:12345")
		if _, e := newHarnessClient(addr, time.Duration(ms)*time.Millisecond); e == nil {
			t.Fatal("bad timeout")
		}
	}
	t.Setenv("HTTP_PROXY", "http://invalid.example.invalid:1")
	t.Setenv("ALL_PROXY", "http://invalid.example.invalid:1")
	l := newLocalLab(t)
	client := clientFor(t, l.frontend, 1000)
	if s := client.observe(context.Background()); s.Status != 200 || s.Error != "" {
		t.Fatal("ambient proxy used", s)
	}
	if client.transport.Proxy != nil || !client.transport.DisableKeepAlives || !client.transport.DisableCompression {
		t.Fatal("client policy")
	}
	if _, e := runWithBudget(context.Background(), c, client, fakeFor(c, l), runTimeout+time.Nanosecond); !errors.Is(e, ErrConfiguration) {
		t.Fatal(e)
	}
	if _, e := run(context.Background(), configuration(80, 1, 50), client, fakeFor(c, l)); !errors.Is(e, ErrConfiguration) {
		t.Fatal("client deadline differs from spec")
	}
}
func TestRecoveryFailuresRemainIncomplete(t *testing.T) {
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) > 5 {
			w.WriteHeader(503)
		} else {
			w.WriteHeader(200)
		}
	}))
	defer s.Close()
	c := configuration(1, 2, 1000)
	f := fakeFor(c, &localLab{})
	r, e := run(context.Background(), c, clientFor(t, s, 1000), f)
	if e == nil || r.Status != "incomplete" || r.Recovery.Status != "complete" || r.Recovery.Summary.Failures != 2 || r.FaultCleanup.Status != "confirmed" || calls.Load() != 7 {
		t.Fatal(e, r)
	}
	validReport(t, r)
}
func TestMeasurementEngineIsDisconnectedFromProduction(t *testing.T) {
	files, e := filepath.Glob(filepath.Join("..", "..", "cmd", "*", "*.go"))
	if e != nil {
		t.Fatal(e)
	}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, e := parser.ParseFile(token.NewFileSet(), name, nil, parser.ImportsOnly)
		if e != nil {
			t.Fatal(e)
		}
		for _, imp := range file.Imports {
			if strings.Contains(imp.Path.Value, "/internal/labworker") {
				t.Fatal("production activated worker", name)
			}
		}
	}
	// Exercise the unchanged preflight contract in its own package suite too;
	// this check ensures there is no package import of mutation/subprocess code.
	files, _ = filepath.Glob("*.go")
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, _ := parser.ParseFile(token.NewFileSet(), name, nil, parser.ImportsOnly)
		for _, imp := range file.Imports {
			if oneOf(imp.Path.Value, `"os/exec"`, `"github.com/raimbekalish/faultline/internal/lab"`, `"github.com/raimbekalish/faultline/internal/toxiproxy"`) {
				t.Fatal("real control boundary", name)
			}
		}
	}
}

func TestPerRequestTimeoutDoesNotCancelWholeRun(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(200)
			return
		}
		<-r.Context().Done()
	}))
	defer server.Close()
	c := configuration(1, 2, 30)
	f := fakeFor(c, &localLab{})
	r, e := run(context.Background(), c, clientFor(t, server, 30), f)
	if !errors.Is(e, ErrExecution) || r.Status != "incomplete" || r.Baseline.Summary.Requests != 2 || r.Baseline.Summary.Failures != 2 || len(f.calls) != 0 {
		t.Fatal(e, r)
	}
	for _, s := range r.Baseline.Samples {
		if s.Error != "request_timeout" {
			t.Fatal(s)
		}
	}
	validReport(t, r)
}
