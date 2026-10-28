package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeRoundTrip func(*http.Request) (*http.Response, error)

func (f fakeRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestIndependentContractOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name    string
		body    string
		status  int
		timeout bool
		want    int
	}{{"healthy", `{"value":42}`, 200, false, 200}, {"timeout", "", 0, true, 504}, {"badbody", `{}`, 200, false, 502}, {"upstream", ``, 503, false, 502}, {"oversize", strings.Repeat("x", 4097), 200, false, 502}} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: fakeRoundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.String() != dependency || r.Method != "GET" {
					t.Fatal("dynamic dependency")
				}
				deadline, ok := r.Context().Deadline()
				if !ok || time.Until(deadline) > 250*time.Millisecond {
					t.Fatal("missing bound")
				}
				if tc.timeout {
					<-r.Context().Done()
					return nil, context.DeadlineExceeded
				}
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body)), Header: http.Header{}}, nil
			})}
			w := httptest.NewRecorder()
			application(client).ServeHTTP(w, httptest.NewRequest("GET", "/work", nil))
			if w.Code != tc.want || calls != 1 {
				t.Fatal(w.Code, calls)
			}
			if tc.want == 200 && w.Body.String() != "{\"price_units\":43}\n" {
				t.Fatal(w.Body.String())
			}
		})
	}
}
