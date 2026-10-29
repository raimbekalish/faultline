package labworker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"
)

// Protocol-compatible test server, NOT Toxiproxy: POST changes a dependency
// handler delay. It never proxies packets, creates Docker resources or adopts
// another proxy. Its call log also proves the adapter's closed operation set.
type controlLab struct {
	mu            sync.Mutex
	configuration Configuration
	lab           *localLab
	toxics        []latencyToxic
	calls         []string
	mode          string
	server        *httptest.Server
	afterPost     func()
	gets          int
}

func newControlLab(t *testing.T, c Configuration, l *localLab, mode string) *controlLab {
	t.Helper()
	s := &controlLab{configuration: c, lab: l, toxics: []latencyToxic{}, mode: mode}
	s.server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.server.Close)
	return s
}
func serverAddress(t *testing.T, s *httptest.Server) netip.AddrPort {
	t.Helper()
	addr, e := netip.ParseAddrPort(s.Listener.Addr().String())
	if e != nil {
		t.Fatal(e)
	}
	return addr
}
func expectedToxic(c Configuration) latencyToxic {
	return latencyToxic{toxicName, "latency", "downstream", 1, latencyAttributes{c.Specification.LatencyMS, 0}}
}
func (s *controlLab) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, r.Method+" "+r.URL.Path)
	if r.Host != controlHost {
		w.WriteHeader(400)
		return
	}
	base := "/proxies/" + s.configuration.RunID
	if r.URL.RawQuery != "" {
		w.WriteHeader(400)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == "GET" && r.URL.Path == base:
		s.gets++
		p := proxyState{s.configuration.RunID, "0.0.0.0:8666", "dependency:8080", true, s.toxics}
		if s.mode == "wrong-proxy" {
			p.Name = "faultline-" + strings.Repeat("f", 32)
		}
		if s.mode == "wrong-upstream" {
			p.Upstream = "unrelated:8080"
		}
		if s.mode == "wrong-toxic" || s.mode == "changed-toxic" && len(s.toxics) > 0 {
			p.Toxics = []latencyToxic{expectedToxic(s.configuration)}
			p.Toxics[0].Name = "foreign"
		}
		switch s.mode {
		case "malformed":
			_, _ = io.WriteString(w, `{"name":`)
			return
		case "oversized":
			_, _ = w.Write(bytes.Repeat([]byte("x"), maxControlBody+1))
			return
		case "duplicate":
			_, _ = io.WriteString(w, `{"name":"a","Name":"b"}`)
			return
		case "null":
			_, _ = io.WriteString(w, `null`)
			return
		case "redirect":
			w.Header().Set("Location", "http://public.example.invalid/")
			w.WriteHeader(302)
			return
		}
		if s.mode == "inconsistent-absence" && len(s.toxics) == 0 && s.lab.latency.Load() == 0 && s.gets >= 4 && s.gets%2 == 1 {
			p.Toxics = []latencyToxic{expectedToxic(s.configuration)}
		}
		if s.mode == "missing-proxy" {
			w.WriteHeader(404)
			return
		}
		_ = json.NewEncoder(w).Encode(p)
	case r.Method == "POST" && r.URL.Path == base+"/toxics":
		b, e := io.ReadAll(io.LimitReader(r.Body, 2049))
		var toxic latencyToxic
		if e != nil || len(b) > 2048 || decodeExact(b, &toxic) != nil || toxic != expectedToxic(s.configuration) || len(s.toxics) != 0 {
			w.WriteHeader(400)
			return
		}
		s.toxics = []latencyToxic{toxic}
		s.lab.latency.Store(int64(toxic.Attributes.Latency))
		if s.afterPost != nil {
			s.afterPost()
		}
		if s.mode == "lost-post" {
			h, _, _ := w.(http.Hijacker).Hijack()
			_ = h.Close()
			return
		}
		if s.mode == "interrupted-post" {
			s.mu.Unlock()
			<-r.Context().Done()
			s.mu.Lock()
			return
		}
		if s.mode == "malformed-post" {
			_, _ = io.WriteString(w, `{}`)
			return
		}
		if s.mode == "wrong-post-toxic" {
			toxic.Attributes.Latency++
		}
		_ = json.NewEncoder(w).Encode(toxic)
	case r.Method == "DELETE" && r.URL.Path == base+"/toxics/"+toxicName:
		if s.mode == "failed-delete" {
			w.WriteHeader(500)
			_, _ = io.WriteString(w, `{"error":"private"}`)
			return
		}
		if s.mode != "still-present" {
			s.toxics = []latencyToxic{}
			s.lab.latency.Store(0)
		}
		if s.mode == "lost-delete" {
			h, _, _ := w.(http.Hijacker).Hijack()
			_ = h.Close()
			return
		}
		w.WriteHeader(204)
	default:
		w.WriteHeader(405)
	}
}
func (s *controlLab) snapshot() ([]string, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string{}, s.calls...), len(s.toxics)
}
func makeController(t *testing.T, s *controlLab) *scopedController {
	t.Helper()
	c, e := newScopedController(s.configuration, serverAddress(t, s.server))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(c.close)
	return c
}
func TestScopedControlSuccessAndOneUse(t *testing.T) {
	c := configuration(80, 1, 1000)
	s := newControlLab(t, c, &localLab{}, "")
	ctl := makeController(t, s)
	ctx := context.Background()
	i := intent(c)
	if state, e := ctl.ApplyLatency(ctx, i); e != nil || state != Applied {
		t.Fatal(state, e)
	}
	if state, e := ctl.ApplyLatency(ctx, i); e == nil || state != ApplicationUnknown {
		t.Fatal("replayed apply")
	}
	if e := ctl.RemoveLatency(ctx, i); e != nil {
		t.Fatal(e)
	}
	if p, e := ctl.VerifyRemoved(ctx, i); e != nil || p != Absent {
		t.Fatal(p, e)
	}
	if e := ctl.RemoveLatency(ctx, i); e == nil {
		t.Fatal("replayed removal")
	}
	calls, left := s.snapshot()
	if len(calls) != 7 || left != 0 {
		t.Fatal(calls, left)
	}
	base := "/proxies/" + c.RunID
	expected := []string{"GET " + base, "POST " + base + "/toxics", "GET " + base, "GET " + base, "DELETE " + base + "/toxics/" + toxicName, "GET " + base, "GET " + base}
	if fmt.Sprint(calls) != fmt.Sprint(expected) {
		t.Fatal(calls)
	}
	for _, b := range []any{ctl, &harnessBinding{}} {
		if _, e := json.Marshal(b); e == nil {
			t.Fatal("serialized authority")
		}
	}
	var forged scopedController
	if json.Unmarshal([]byte(`{}`), &forged) == nil {
		t.Fatal("forged controller")
	}
}
func TestControllerRejectsForeignOrMalformedState(t *testing.T) {
	for _, mode := range []string{"wrong-proxy", "wrong-upstream", "wrong-toxic", "missing-proxy", "malformed", "oversized", "duplicate", "null", "redirect"} {
		t.Run(mode, func(t *testing.T) {
			c := configuration(80, 1, 1000)
			s := newControlLab(t, c, &localLab{}, mode)
			ctl := makeController(t, s)
			if state, e := ctl.ApplyLatency(context.Background(), intent(c)); e == nil || state != ApplicationUnknown {
				t.Fatal(state, e)
			}
			_ = ctl.RemoveLatency(context.Background(), intent(c))
			_, _ = ctl.VerifyRemoved(context.Background(), intent(c))
			calls, _ := s.snapshot()
			if len(calls) != 1 || !strings.HasPrefix(calls[0], "GET ") {
				t.Fatal("mutated foreign or ambiguous scope", calls)
			}
		})
	}
	c := configuration(80, 1, 1000)
	s := newControlLab(t, c, &localLab{}, "")
	ctl := makeController(t, s)
	bad := intent(c)
	bad.LaunchID = "faultline-lab-" + strings.Repeat("f", 32)
	if _, e := ctl.ApplyLatency(context.Background(), bad); e == nil {
		t.Fatal("foreign launch")
	}
	if e := ctl.RemoveLatency(context.Background(), bad); e == nil {
		t.Fatal("foreign cleanup")
	}
	calls, _ := s.snapshot()
	if len(calls) != 0 {
		t.Fatal(calls)
	}
	if _, e := newScopedController(c, netip.MustParseAddrPort("192.0.2.1:8474")); e == nil {
		t.Fatal("public target")
	}
}
func TestControllerAmbiguousApplyNeverAdoptsToxic(t *testing.T) {
	for _, mode := range []string{"lost-post", "malformed-post", "wrong-post-toxic", "interrupted-post"} {
		t.Run(mode, func(t *testing.T) {
			c := configuration(80, 1, 1000)
			s := newControlLab(t, c, &localLab{}, mode)
			ctl := makeController(t, s)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if state, e := ctl.ApplyLatency(ctx, intent(c)); e == nil || state != ApplicationUnknown {
				t.Fatal(state, e)
			}
			if e := ctl.RemoveLatency(context.Background(), intent(c)); e == nil {
				t.Fatal("adopted unknown creation")
			}
			if p, _ := ctl.VerifyRemoved(context.Background(), intent(c)); p != Unknown {
				t.Fatal(p)
			}
			calls, left := s.snapshot()
			if len(calls) != 2 || left != 1 {
				t.Fatal(calls, left)
			}
		})
	}
}
func TestControllerAbsenceRequiresFreshConsistentQueries(t *testing.T) {
	for _, mode := range []string{"lost-delete", "failed-delete", "still-present", "inconsistent-absence", "changed-toxic"} {
		t.Run(mode, func(t *testing.T) {
			c := configuration(80, 1, 1000)
			s := newControlLab(t, c, &localLab{}, "")
			ctl := makeController(t, s)
			ctx := context.Background()
			if _, e := ctl.ApplyLatency(ctx, intent(c)); e != nil {
				t.Fatal(e)
			}
			s.mu.Lock()
			s.mode = mode
			s.mu.Unlock()
			removeErr := ctl.RemoveLatency(ctx, intent(c))
			p, verifyErr := ctl.VerifyRemoved(ctx, intent(c))
			if mode == "lost-delete" {
				if removeErr == nil || p != Absent {
					t.Fatal(removeErr, p)
				}
			}
			if mode == "failed-delete" || mode == "still-present" {
				if p != Present {
					t.Fatal(p)
				}
			}
			if mode == "changed-toxic" || mode == "inconsistent-absence" {
				if p != Unknown || verifyErr == nil {
					t.Fatal(p, verifyErr)
				}
			}
			calls, _ := s.snapshot()
			if len(calls) > maxControlRequests {
				t.Fatal(calls)
			}
			if mode == "changed-toxic" {
				for _, call := range calls {
					if strings.HasPrefix(call, "DELETE") {
						t.Fatal("deleted foreign toxic")
					}
				}
			}
		})
	}
}

func TestControllerConcurrencyAndBudgetFailClosed(t *testing.T) {
	c := configuration(80, 1, 1000)
	s := newControlLab(t, c, &localLab{}, "")
	ctl := makeController(t, s)
	ctx := context.Background()
	ctl.mu.Lock()
	_, applyErr := ctl.ApplyLatency(ctx, intent(c))
	removeErr := ctl.RemoveLatency(ctx, intent(c))
	_, verifyErr := ctl.VerifyRemoved(ctx, intent(c))
	ctl.mu.Unlock()
	if applyErr == nil || removeErr == nil || verifyErr == nil {
		t.Fatal("concurrent method gained authority")
	}
	ctl.calls = maxControlRequests
	if _, e := ctl.ApplyLatency(ctx, intent(c)); e == nil {
		t.Fatal("request budget bypass")
	}
	calls, _ := s.snapshot()
	if len(calls) != 0 {
		t.Fatal("unexpected HTTP effects", calls)
	}
}
