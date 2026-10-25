package dashboard

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/raimbekalish/faultline/internal/experiment"
	"github.com/raimbekalish/faultline/internal/history"
	"github.com/raimbekalish/faultline/internal/spec"
)

func setup(t *testing.T) (*history.Store, http.Handler, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "history.sqlite")
	s, err := history.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, Handler(s, fstest.MapFS{"index.html": {Data: []byte("<html>Faultline</html>")}, "assets/app.js": {Data: []byte("console.log('ok')")}}, "127.0.0.1:4000"), path
}
func request(h http.Handler, path string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", "http://127.0.0.1:4000"+path, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func put(t *testing.T, s *history.Store, status string, n int) string {
	t.Helper()
	sp := spec.FromConfig(experiment.Config{Samples: 2, LatencyMS: 80, RequestTimeoutMS: 1000})
	id, err := s.Start(context.Background(), sp)
	if err != nil {
		t.Fatal(err)
	}
	if status == "running" {
		return id
	}
	p := experiment.Phase{Samples: []experiment.Sample{{DurationMS: 1, Status: 200}, {DurationMS: 5, Status: 200}}}
	p.Summary = experiment.Summarize(p.Samples)
	r := experiment.Report{SchemaVersion: 1, RunID: fmt.Sprintf("faultline-%032x", n), Status: status, StartedAt: time.Now().Add(-time.Second), FinishedAt: time.Now(), Config: sp.Config(), Target: experiment.Target, CleanupComplete: true, Baseline: p, Fault: p, Recovery: p}
	if status != "complete" {
		r.Error = "request canceled"
		r.Recovery = experiment.Phase{}
	}
	if err = s.Finish(context.Background(), id, r); err != nil {
		t.Fatal(err)
	}
	return id
}
func TestAPIRealSQLiteContract(t *testing.T) {
	s, h, _ := setup(t)
	a := put(t, s, "complete", 1)
	b := put(t, s, "complete", 2)
	c := put(t, s, "canceled", 3)
	put(t, s, "running", 4)
	for _, path := range []string{"/api/v1/health", "/api/v1/runs?limit=2", "/api/v1/runs?status=canceled", "/api/v1/runs?q=" + c, "/api/v1/runs/" + a, "/api/v1/compare?left=" + a + "&right=" + b} {
		w := request(h, path)
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body)
		}
		var e map[string]json.RawMessage
		if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil || string(e["api_version"]) != "1" || len(e["data"]) == 0 {
			t.Fatal(w.Body.String(), err)
		}
	}
	w := request(h, "/api/v1/runs?limit=2")
	var p struct {
		Data list `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &p)
	if p.Data.Total != 4 || len(p.Data.Items) != 2 || p.Data.Next == nil || p.Data.Counts.Running != 1 {
		t.Fatal(w.Body.String())
	}
	if strings.Contains(w.Body.String(), "duration_ms") {
		t.Fatal("list included raw samples")
	}
	if w = request(h, "/api/v1/compare?left="+a+"&right="+c); w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestAPIInputErrorsAndEmpty(t *testing.T) {
	_, h, _ := setup(t)
	for _, path := range []string{"/api/v1/runs?limit=0", "/api/v1/runs?limit=51", "/api/v1/runs?limit=1&limit=2", "/api/v1/runs?target=http://example.org", "/api/v1/runs?status=nope", "/api/v1/runs?cursor=bad", "/api/v1/runs/%2e%2e", "/api/v1/runs?bad=%zz", "/api/v1/compare?left=foo&right=bar"} {
		w := request(h, path)
		if w.Code != 400 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body)
		}
	}
	for _, path := range []string{"/api/v2/runs", "/api/v1/runs/history-00000000000000000000000000000000", "/assets/", "/source.go"} {
		if w := request(h, path); w.Code != 404 {
			t.Fatal(path, w.Code)
		}
	}
	w := request(h, "/api/v1/runs")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"items":[]`) {
		t.Fatal(w.Body.String())
	}
}
func TestAPIRejectsCorruptHistory(t *testing.T) {
	s, h, path := setup(t)
	id := put(t, s, "complete", 1)
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec("UPDATE runs SET report_json=json_set(report_json,'$.fault.summary.mean_ms',2) WHERE id=?", id); err != nil {
		t.Fatal(err)
	}
	for _, route := range []string{"/api/v1/runs", "/api/v1/runs/" + id, "/api/v1/compare?left=" + id + "&right=" + id} {
		w := request(h, route)
		if w.Code != 500 || !strings.Contains(w.Body.String(), `"code":"corrupt_history"`) || len(w.Body.Bytes()) > 512 {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
	}
}
func TestOriginMethodsAndSecurityHeaders(t *testing.T) {
	_, h, _ := setup(t)
	for _, tc := range []struct {
		host, origin, site, method string
		want                       int
	}{{"evil.example:4000", "", "", "GET", 403}, {"127.0.0.1:4000", "null", "", "GET", 403}, {"127.0.0.1:4000", "http://evil.example", "", "GET", 403}, {"127.0.0.1:4000", "", "cross-site", "GET", 403}, {"127.0.0.1:4000", "http://127.0.0.1:4000", "same-origin", "POST", 405}, {"127.0.0.1:4000", "http://127.0.0.1:4000", "same-origin", "GET", 200}} {
		r := httptest.NewRequest(tc.method, "http://127.0.0.1:4000/api/v1/health", nil)
		r.Host = tc.host
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("Sec-Fetch-Site", tc.site)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatal(tc, w.Code)
		}
		if w.Header().Get("Access-Control-Allow-Origin") != "" || w.Header().Get("Content-Security-Policy") == "" {
			t.Fatal(w.Header())
		}
	}
	if w := request(h, "/"); w.Code != 200 || !strings.Contains(w.Body.String(), "Faultline") {
		t.Fatal(w.Code)
	}
}
