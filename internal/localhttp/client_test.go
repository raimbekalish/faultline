package localhttp

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRefusesRedirectAndEnvironmentProxy(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "http://unrelated.invalid", 302) }))
	defer s.Close()
	c := New(time.Second)
	defer c.CloseIdleConnections()
	r, err := c.Get(s.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	if r.StatusCode != 302 {
		t.Fatalf("status = %d", r.StatusCode)
	}
	if c.Transport.(*http.Transport).Proxy != nil {
		t.Fatal("environment proxies enabled")
	}
}
