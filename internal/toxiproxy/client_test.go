package toxiproxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testName = "faultline-0123456789abcdef0123456789abcdef"

func TestLatencyContract(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/proxies/"+testName+"/toxics" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		b, _ := io.ReadAll(r.Body)
		want := `{"name":"faultline-latency","type":"latency","stream":"downstream","toxicity":1,"attributes":{"jitter":0,"latency":125}}`
		if string(b) != want {
			t.Errorf("body = %s", b)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer s.Close()
	c := New()
	defer c.Close()
	c.base = s.URL
	if err := c.AddLatency(context.Background(), testName, 125*time.Millisecond); err != nil {
		t.Fatal(err)
	}
}

func TestOwnershipAndLimits(t *testing.T) {
	c := New()
	defer c.Close()
	for _, name := range []string{"production", "../reset", "faultline-", ""} {
		if err := c.Delete(context.Background(), name); err == nil {
			t.Fatalf("accepted %q", name)
		}
	}
	for _, d := range []time.Duration{0, -time.Second, 6 * time.Second, time.Microsecond} {
		if err := c.AddLatency(context.Background(), testName, d); err == nil {
			t.Fatalf("accepted %s", d)
		}
	}
}

func TestDeleteMissingAndServerFailure(t *testing.T) {
	for _, status := range []int{404, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
			defer s.Close()
			c := New()
			defer c.Close()
			c.base = s.URL
			err := c.Delete(context.Background(), testName)
			if (err != nil) != (status == 500) {
				t.Fatalf("status %d: %v", status, err)
			}
		})
	}
}

func TestControlResponseBounded(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, strings.Repeat("x", 65537)) }))
	defer s.Close()
	c := New()
	defer c.Close()
	c.base = s.URL
	if err := c.Create(context.Background(), testName); err == nil {
		t.Fatal("accepted oversized response")
	}
}
