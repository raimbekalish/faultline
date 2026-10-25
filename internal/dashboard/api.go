// Package dashboard exposes read-only historical analysis, never fault control.
package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/raimbekalish/faultline/internal/experiment"
	"github.com/raimbekalish/faultline/internal/history"
	"github.com/raimbekalish/faultline/internal/spec"
)

type envelope struct {
	Version int       `json:"api_version"`
	Data    any       `json:"data,omitempty"`
	Error   *apiError `json:"error,omitempty"`
}
type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
type Run struct {
	ID         string                        `json:"id"`
	RunID      string                        `json:"run_id,omitempty"`
	Status     string                        `json:"status"`
	CreatedAt  time.Time                     `json:"created_at"`
	FinishedAt *time.Time                    `json:"finished_at,omitempty"`
	Spec       spec.Spec                     `json:"spec"`
	SpecHash   string                        `json:"spec_hash"`
	Phases     map[string]experiment.Summary `json:"phases,omitempty"`
	Cleanup    *bool                         `json:"cleanup_complete,omitempty"`
	Error      string                        `json:"error,omitempty"`
}
type list struct {
	Items  []Run          `json:"items"`
	Counts history.Counts `json:"counts"`
	Total  int            `json:"total"`
	Next   *string        `json:"next_cursor"`
}

func project(e history.Entry) Run {
	r := Run{ID: e.ID, RunID: e.RunID, Status: e.Status, CreatedAt: e.CreatedAt, FinishedAt: e.FinishedAt, Spec: e.Spec, SpecHash: e.SpecHash}
	if e.Report != nil {
		var report experiment.Report
		_ = json.Unmarshal(e.Report, &report)
		r.Phases = map[string]experiment.Summary{"baseline": report.Baseline.Summary, "fault": report.Fault.Summary, "recovery": report.Recovery.Summary}
		r.Cleanup = &report.CleanupComplete
		r.Error = report.Error
		if len(r.Error) > 240 {
			r.Error = r.Error[:240]
		}
	}
	return r
}

// Handler's authority must be the literal bound listener host:port.
func Handler(store *history.Store, assets fs.FS, authority string) http.Handler {
	slots := make(chan struct{}, 4)
	static := http.FileServer(http.FS(assets))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; font-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
		if r.Host != authority || r.Header.Get("Sec-Fetch-Site") == "cross-site" || (r.Header.Get("Origin") != "" && r.Header.Get("Origin") != "http://"+authority) {
			fail(w, 403, "forbidden_origin", "Use the dashboard's local origin.")
			return
		}
		if r.Method != "GET" {
			w.Header().Set("Allow", "GET")
			fail(w, 405, "method_not_allowed", "Only GET requests are supported.")
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			if r.URL.Path != "/" && !strings.HasPrefix(r.URL.Path, "/assets/") && r.URL.Path != "/favicon.svg" {
				fail(w, 404, "not_found", "Resource not found.")
				return
			}
			// Never expose directory listings or source files.
			name := strings.TrimPrefix(r.URL.Path, "/")
			if name == "" {
				name = "index.html"
			}
			info, err := fs.Stat(assets, name)
			if err != nil || !info.Mode().IsRegular() {
				fail(w, 404, "not_found", "Resource not found.")
				return
			}
			static.ServeHTTP(w, r)
			return
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			fail(w, 429, "busy", "Too many concurrent queries.")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), history.OperationTimeout)
		defer cancel()
		q, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil {
			fail(w, 400, "invalid_query", "Invalid query parameters.")
			return
		}
		var data any
		switch {
		case r.URL.Path == "/api/v1/health":
			if !validQuery(q) {
				badQuery(w)
				return
			}
			data = map[string]string{"status": "ok", "mode": "read_only"}
		case r.URL.Path == "/api/v1/runs":
			if !validQuery(q, "limit", "cursor", "status", "q") {
				badQuery(w)
				return
			}
			limit := 20
			if q.Has("limit") {
				limit, err = strconv.Atoi(q.Get("limit"))
				if err != nil {
					badQuery(w)
					return
				}
			}
			f := history.Filter{Limit: limit, Cursor: q.Get("cursor"), Status: q.Get("status"), Search: q.Get("q")}
			if f.Validate() != nil {
				badQuery(w)
				return
			}
			var page history.Page
			page, err = store.Browse(ctx, f)
			if err == nil {
				items := make([]Run, 0, len(page.Items))
				for _, e := range page.Items {
					items = append(items, project(e))
				}
				data = list{items, page.Counts, page.Total, page.NextCursor}
			}
		case strings.HasPrefix(r.URL.Path, "/api/v1/runs/"):
			id := strings.TrimPrefix(r.URL.Path, "/api/v1/runs/")
			if !validQuery(q) || !history.ValidID(id) {
				badQuery(w)
				return
			}
			data, err = store.Get(ctx, id)
		case r.URL.Path == "/api/v1/compare":
			if !validQuery(q, "left", "right") || !history.ValidID(q.Get("left")) || !history.ValidID(q.Get("right")) {
				badQuery(w)
				return
			}
			data, err = store.Compare(ctx, q.Get("left"), q.Get("right"))
		default:
			fail(w, 404, "not_found", "API route not found.")
			return
		}
		if err != nil {
			switch {
			case errors.Is(err, history.ErrNotFound):
				fail(w, 404, "not_found", "History run not found.")
			case errors.Is(err, history.ErrNotComparable):
				fail(w, 409, "not_comparable", "Select two completed runs with successful cleanup.")
			case errors.Is(err, history.ErrCorrupt):
				fail(w, 500, "corrupt_history", "History contains an inconsistent record. No data was repaired.")
			default:
				fail(w, 503, "history_unavailable", "History query unavailable. Retry or inspect the database locally.")
			}
			return
		}
		write(w, 200, envelope{Version: 1, Data: data})
	})
}
func validQuery(q url.Values, names ...string) bool {
	for key, values := range q {
		found := false
		for _, n := range names {
			if key == n {
				found = true
			}
		}
		if !found || len(values) != 1 {
			return false
		}
	}
	return true
}
func badQuery(w http.ResponseWriter) {
	fail(w, 400, "invalid_query", "Invalid or unsupported query parameters.")
}
func fail(w http.ResponseWriter, status int, code, message string) {
	write(w, status, envelope{Version: 1, Error: &apiError{code, message}})
}
func write(w http.ResponseWriter, status int, v envelope) {
	b, err := json.Marshal(v)
	if err != nil {
		status = 500
		b = []byte(`{"api_version":1,"error":{"code":"encoding_error","message":"Unable to encode response."}}`)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(append(b, '\n'))
}
