package labworker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/raimbekalish/faultline/internal/spec"
)

type localLab struct {
	latency      atomic.Int64
	requests     atomic.Int32
	dependencies atomic.Int32
	connections  atomic.Int32
	frontend     *httptest.Server
}

func newLocalLab(t *testing.T) *localLab {
	t.Helper()
	l := &localLab{}
	dependency := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		l.dependencies.Add(1)
		timer := time.NewTimer(time.Duration(l.latency.Load()) * time.Millisecond)
		defer timer.Stop()
		select {
		case <-r.Context().Done():
			return
		case <-timer.C:
		}
		_, _ = io.WriteString(w, `{"value":42}`)
	}))
	t.Cleanup(dependency.Close)
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, DisableCompression: true, MaxConnsPerHost: 1, MaxResponseHeaderBytes: 8192}
	t.Cleanup(transport.CloseIdleConnections)
	client := &http.Client{Transport: transport, Timeout: 250 * time.Millisecond, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	l.frontend = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		l.requests.Add(1)
		if r.Method != http.MethodGet || r.URL.Path != "/work" || r.Host != "frontend:8080" {
			w.WriteHeader(400)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 250*time.Millisecond)
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, dependency.URL, nil)
		response, e := client.Do(req)
		if e != nil {
			if ctx.Err() == context.DeadlineExceeded || classify(context.Background(), e) == "request_timeout" {
				w.WriteHeader(504)
			} else {
				w.WriteHeader(502)
			}
			return
		}
		defer response.Body.Close()
		_, e = io.Copy(io.Discard, io.LimitReader(response.Body, 4097))
		if e != nil {
			w.WriteHeader(502)
			return
		}
		_, _ = io.WriteString(w, `{"price_units":43}`)
	}))
	l.frontend.Config.ConnState = func(_ net.Conn, s http.ConnState) {
		if s == http.StateNew {
			l.connections.Add(1)
		}
	}
	l.frontend.Start()
	t.Cleanup(l.frontend.Close)
	return l
}
func clientFor(t *testing.T, s *httptest.Server, ms int) *contractClient {
	t.Helper()
	addr, e := netip.ParseAddrPort(s.Listener.Addr().String())
	if e != nil {
		t.Fatal(e)
	}
	c, e := newHarnessClient(addr, time.Duration(ms)*time.Millisecond)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(c.close)
	return c
}
func configuration(latency, samples, timeout int) Configuration {
	s := spec.Spec{SchemaVersion: 1, Samples: samples, LatencyMS: latency, RequestTimeoutMS: timeout}
	h, _ := s.Hash()
	rev := strings.Repeat("1", 40)
	if actual := os.Getenv("FAULTLINE_WORKER_TEST_REVISION"); actual != "" {
		rev = actual
	}
	return Configuration{"faultline-lab-" + strings.Repeat("2", 32), "faultline-" + strings.Repeat("3", 32), s, h, rev}
}

// A deterministic, run-scoped recording fake. Its delay is in a local HTTP test
// fixture handler, NOT a network/Toxiproxy toxic or Docker isolation claim.
type fakeFault struct {
	mu                                   sync.Mutex
	expected                             LatencyIntent
	lab                                  *localLab
	calls                                []string
	applyError, removeError, verifyError bool
	keepFault, unknownApply              bool
	verification                         Presence
	applied                              chan struct{}
	afterApply                           func()
	cleanContext                         bool
}

func fakeFor(c Configuration, l *localLab) *fakeFault {
	return &fakeFault{expected: intent(c), lab: l, verification: Absent, applied: make(chan struct{}), cleanContext: true}
}
func (f *fakeFault) ApplyLatency(ctx context.Context, i LatencyIntent) (ApplicationState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "apply")
	if ctx.Err() != nil || i != f.expected {
		return ApplicationUnknown, errors.New("rejected")
	}
	f.lab.latency.Store(int64(i.LatencyMS))
	close(f.applied)
	if f.afterApply != nil {
		f.afterApply()
	}
	if f.applyError {
		return ApplicationUnknown, errors.New("private fixture diagnostic")
	}
	if f.unknownApply {
		return ApplicationUnknown, nil
	}
	return Applied, nil
}
func (f *fakeFault) RemoveLatency(ctx context.Context, i LatencyIntent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "remove")
	deadline, ok := ctx.Deadline()
	f.cleanContext = ctx.Err() == nil && ok && time.Until(deadline) > 0 && time.Until(deadline) <= cleanupTimeout
	if i != f.expected {
		return errors.New("foreign run")
	}
	if !f.keepFault {
		f.lab.latency.Store(0)
	}
	if f.removeError {
		return errors.New("private removal diagnostic")
	}
	return ctx.Err()
}
func (f *fakeFault) VerifyRemoved(ctx context.Context, i LatencyIntent) (Presence, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "verify")
	if i != f.expected || ctx.Err() != nil || f.verifyError {
		return Unknown, errors.New("unconfirmed")
	}
	if f.lab.latency.Load() != 0 {
		return Present, nil
	}
	return f.verification, nil
}
func validReport(t *testing.T, r Report) {
	t.Helper()
	if e := r.Validate(); e != nil {
		t.Fatalf("invalid report: %v %#v", e, r)
	}
	b, e := r.Encode()
	if e != nil {
		t.Fatal(e)
	}
	decoded, e := Decode(context.Background(), bytes.NewReader(b))
	if e != nil {
		t.Fatalf("decode: %v\n%s", e, b)
	}
	again, e := decoded.Encode()
	if e != nil || !bytes.Equal(b, again) {
		t.Fatal("non-deterministic roundtrip")
	}
}
func saveEvidence(t *testing.T, latency int, r Report) {
	t.Helper()
	directory := os.Getenv("FAULTLINE_WORKER_EVIDENCE_DIR")
	if directory == "" {
		return
	}
	if os.Getenv("FAULTLINE_WORKER_TEST_REVISION") == "" {
		t.Fatal("evidence requires committed source revision")
	}
	info, e := os.Lstat(directory)
	if e != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		t.Fatal("private existing evidence directory required")
	}
	b, e := r.Encode()
	if e != nil {
		t.Fatal(e)
	}
	f, e := os.OpenFile(filepath.Join(directory, fmt.Sprintf("latency-%d.json", latency)), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		t.Fatal(e)
	}
	_, e = f.Write(b)
	closeErr := f.Close()
	if e != nil || closeErr != nil {
		t.Fatal("evidence write failed")
	}
}
func TestRealHTTPBaselineFaultRecovery(t *testing.T) {
	for _, latency := range []int{80, 400} {
		t.Run(fmt.Sprint(latency), func(t *testing.T) {
			l := newLocalLab(t)
			c := configuration(latency, 3, 1000)
			f := fakeFor(c, l)
			r, e := run(context.Background(), c, clientFor(t, l.frontend, 1000), f)
			if e != nil || r.Status != "complete" || r.FaultCleanup.Status != "confirmed" {
				t.Fatalf("%v %#v", e, r)
			}
			validReport(t, r)
			if r.Baseline.Summary.Failures != 0 || r.Recovery.Summary.Failures != 0 || r.Fault.Summary.Requests != 3 {
				t.Fatal("phase integrity")
			}
			wantStatus := 200
			if latency == 400 {
				wantStatus = 504
			}
			for _, s := range r.Fault.Samples {
				if s.Status != wantStatus {
					t.Fatalf("HTTP status %d, expected %d", s.Status, wantStatus)
				}
			}
			if latency == 80 && (r.Fault.Summary.MeanMS < 60 || r.Fault.Summary.MeanMS > 1000) {
				t.Fatal("80ms timing outside broad test bounds", r.Fault.Summary.MeanMS)
			}
			if latency == 400 && (r.Fault.Summary.MeanMS < 200 || r.Fault.Summary.MeanMS > 1000 || r.Fault.Summary.FailureRate != 1) {
				t.Fatal("250ms dependency timeout not observed", r.Fault.Summary)
			}
			// One readiness call + exactly three serial requests in each phase; fresh
			// frontend TCP connections prohibit reused-connection automatic retries.
			if l.requests.Load() != 10 || l.dependencies.Load() != 10 || l.connections.Load() != 10 {
				t.Fatal("request/retry mismatch", l.requests.Load(), l.dependencies.Load(), l.connections.Load())
			}
			if strings.Join(f.calls, ",") != "apply,remove,verify" || !f.cleanContext {
				t.Fatal("fault lifecycle", f.calls)
			}
			t.Logf("actual local HTTP: latency=%d baseline mean=%.3f fault mean=%.3f recovery mean=%.3f fault failures=%d/%d", latency, r.Baseline.Summary.MeanMS, r.Fault.Summary.MeanMS, r.Recovery.Summary.MeanMS, r.Fault.Summary.Failures, r.Fault.Summary.Requests)
			saveEvidence(t, latency, r)
		})
	}
}
func TestFaultApplicationAndCleanupFailures(t *testing.T) {
	for _, mode := range []string{"apply-error", "apply-unknown", "remove-error", "still-present", "verify-error", "verify-unknown"} {
		t.Run(mode, func(t *testing.T) {
			l := newLocalLab(t)
			c := configuration(1, 1, 1000)
			f := fakeFor(c, l)
			switch mode {
			case "apply-error":
				f.applyError = true
			case "apply-unknown":
				f.unknownApply = true
			case "remove-error":
				f.removeError = true
			case "still-present":
				f.keepFault = true
			case "verify-error":
				f.verifyError = true
			case "verify-unknown":
				f.verification = Unknown
			}
			r, e := run(context.Background(), c, clientFor(t, l.frontend, 1000), f)
			if !errors.Is(e, ErrExecution) || r.Status != "incomplete" {
				t.Fatal(e, r.Status)
			}
			validReport(t, r)
			if strings.Join(f.calls, ",") != "apply,remove,verify" || !f.cleanContext {
				t.Fatal("cleanup skipped", f.calls)
			}
			if r.Recovery.Summary != nil {
				t.Fatal("recovery fabricated")
			}
			if strings.HasPrefix(mode, "apply") {
				if r.AppliedFault.Application != "unknown" || r.Fault.Summary != nil || r.FaultCleanup.Status != "confirmed" {
					t.Fatal(r)
				}
			} else if r.FaultCleanup.Status != "unconfirmed" {
				t.Fatal(r.FaultCleanup)
			}
			b, _ := r.Encode()
			if bytes.Contains(b, []byte("private")) {
				t.Fatal("raw diagnostic leaked")
			}
		})
	}
}
func TestCanceledRunAndPartialCleanup(t *testing.T) {
	for _, mode := range []string{"before", "after-apply", "in-flight", "whole-deadline"} {
		t.Run(mode, func(t *testing.T) {
			l := newLocalLab(t)
			c := configuration(400, 4, 1000)
			f := fakeFor(c, l)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			budget := runTimeout
			switch mode {
			case "before":
				cancel()
			case "after-apply":
				f.afterApply = cancel
			case "in-flight":
				go func() {
					select {
					case <-f.applied:
						time.Sleep(30 * time.Millisecond)
						cancel()
					case <-ctx.Done():
					}
				}()
			case "whole-deadline":
				budget = 100 * time.Millisecond
			}
			r, e := runWithBudget(ctx, c, clientFor(t, l.frontend, 1000), f, budget)
			if e == nil || r.Status != "canceled" {
				t.Fatal(e, r.Status)
			}
			validReport(t, r)
			if mode != "before" {
				if r.FaultCleanup.Status != "confirmed" || !f.cleanContext || l.latency.Load() != 0 {
					t.Fatal("canceled cleanup", r.FaultCleanup)
				}
			} else if len(f.calls) != 0 {
				t.Fatal("fault touched before readiness")
			}
			if mode == "in-flight" && (len(r.Fault.Samples) != 1 || r.Fault.Samples[0].Error != "request_canceled") {
				t.Fatal("lost canceled observation", r.Fault)
			}
		})
	}
}
func TestHTTPOutcomeClassificationAndBounds(t *testing.T) {
	for _, mode := range []string{"unexpected", "redirect", "oversized", "slow-body", "slow-header", "truncated", "connection"} {
		t.Run(mode, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				switch mode {
				case "unexpected":
					w.WriteHeader(418)
				case "redirect":
					w.Header().Set("Location", "http://public.example.invalid/")
					w.WriteHeader(302)
				case "oversized":
					_, _ = w.Write(make([]byte, maxBody+2))
				case "slow-body":
					w.WriteHeader(200)
					w.(http.Flusher).Flush()
					<-r.Context().Done()
				case "slow-header":
					<-r.Context().Done()
				case "truncated":
					w.Header().Set("Content-Length", "20")
					_, _ = io.WriteString(w, "short")
				default:
					w.WriteHeader(200)
				}
			}))
			defer server.Close()
			client := clientFor(t, server, 30)
			if mode == "connection" {
				server.Close()
			}
			s := client.observe(context.Background())
			expected := map[string]string{"unexpected": "http_status", "redirect": "http_status", "oversized": "body_too_large", "slow-body": "request_timeout", "slow-header": "request_timeout", "truncated": "body_read_error", "connection": "connection_error"}[mode]
			if s.Error != expected || s.DurationMS < 0 || s.DurationMS > 1000 {
				t.Fatal(mode, s)
			}
			count := int32(1)
			if mode == "connection" {
				count = 0
			}
			if requests.Load() != count {
				t.Fatal("retry or redirect", requests.Load())
			}
			if mode == "slow-body" && s.Status != 200 {
				t.Fatal("lost received status")
			}
		})
	}
}
func TestReadinessAndBaselineRefuseInvalidComparison(t *testing.T) {
	for _, mode := range []string{"readiness", "baseline"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := calls.Add(1)
				if mode == "baseline" && n == 1 {
					w.WriteHeader(200)
				} else {
					w.WriteHeader(503)
				}
			}))
			defer server.Close()
			c := configuration(1, 2, 1000)
			f := fakeFor(c, &localLab{})
			r, e := run(context.Background(), c, clientFor(t, server, 1000), f)
			if e == nil || r.Status != "incomplete" || len(f.calls) != 0 {
				t.Fatal(e, r)
			}
			validReport(t, r)
			if mode == "readiness" && calls.Load() != readinessRequests {
				t.Fatal("readiness count", calls.Load())
			}
			if mode == "baseline" && (calls.Load() != 3 || r.Baseline.Summary.Failures != 2) {
				t.Fatal("baseline count", calls.Load())
			}
		})
	}
}
