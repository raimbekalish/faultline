package experiment

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/raimbekalish/faultline/internal/localhttp"
)

func TestSummary(t *testing.T) {
	s := summarize([]Sample{{DurationMS: 30, Status: 200}, {DurationMS: 10, Status: 504}, {DurationMS: 20, Error: "timeout"}})
	if s.Requests != 3 || s.Failures != 2 || s.MeanMS != 20 || s.P50MS != 20 || s.P95MS != 30 || s.P99MS != 30 || s.MinMS != 10 || s.MaxMS != 30 || s.FailureRate != 2.0/3 {
		t.Fatalf("%+v", s)
	}
	if s := summarize(nil); s != (Summary{}) {
		t.Fatalf("empty: %+v", s)
	}
}

func TestMeasurementOutcomes(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(504) }))
	defer s.Close()
	c := localhttp.New(time.Second)
	defer c.CloseIdleConnections()
	p, err := measure(context.Background(), c, s.URL, 3)
	if err != nil || p.Summary.Failures != 3 || p.Summary.Requests != 3 {
		t.Fatalf("%+v: %v", p, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p, err = measure(ctx, c, s.URL, 3)
	if err != context.Canceled || p.Summary.Requests != 0 {
		t.Fatalf("%+v: %v", p, err)
	}
}

func TestRequestTimeoutIsMeasured(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer s.Close()
	c := localhttp.New(20 * time.Millisecond)
	defer c.CloseIdleConnections()
	p, err := measure(context.Background(), c, s.URL, 1)
	if err != nil || p.Summary.Failures != 1 || p.Samples[0].Status != 0 {
		t.Fatalf("%+v: %v", p, err)
	}
}
