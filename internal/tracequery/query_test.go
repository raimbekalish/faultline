package tracequery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMissingTraceIsExplicitAndBounded(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) }))
	defer s.Close()
	c := New()
	defer c.Close()
	c.base = s.URL
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	r, err := c.Collect(ctx, []string{strings.Repeat("a", 32), strings.Repeat("b", 32)})
	if err != nil || len(r) != 2 {
		t.Fatalf("%+v %v", r, err)
	}
	for _, record := range r {
		if record.Complete || record.RetrievalError == "" || len(record.Spans) != 0 {
			t.Fatalf("invented evidence: %+v", record)
		}
	}
	if _, err := c.Collect(context.Background(), []string{"../services"}); err == nil {
		t.Fatal("invalid query accepted")
	}
}

func TestDecodeRejectsAnotherTrace(t *testing.T) {
	if _, err := decode(strings.Repeat("a", 32), []byte(`{"data":[{"traceID":"wrong"}]}`)); err == nil {
		t.Fatal("accepted unrelated trace")
	}
}

func TestJaegerStatusTranslation(t *testing.T) {
	for _, key := range []string{"http.status_code", "http.response.status_code"} {
		body := []byte(`{"data":[{"traceID":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","spans":[{"traceID":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","spanID":"0000000000000001","tags":[{"key":"` + key + `","value":504}]}]}]}`)
		r, err := decode(strings.Repeat("a", 32), body)
		if err != nil || len(r.Spans) != 1 || r.Spans[0].HTTPStatus != 504 {
			t.Fatalf("%s: %+v %v", key, r.Spans, err)
		}
	}
}
