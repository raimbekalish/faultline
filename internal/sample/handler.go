// Package sample implements the small HTTP application used by experiments.
package sample

import (
	"fmt"
	"net/http"
	"time"

	"github.com/raimbekalish/faultline/internal/telemetry"
)

const DependencyTimeout = 250 * time.Millisecond
const DependencyURL = "http://toxiproxy:8666/value"

func Handler(role string, client *http.Client, tracing *telemetry.Runtime) (http.Handler, error) {
	if role != "frontend" && role != "dependency" {
		return nil, fmt.Errorf("role must be frontend or dependency")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok\n")) })
	if role == "dependency" {
		mux.Handle("GET /value", tracing.Handler("/value", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("{\"value\":42}\n"))
		})))
	} else {
		mux.Handle("GET /work", tracing.Handler("/work", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, DependencyURL, nil)
			if err != nil {
				http.Error(w, "dependency request", 500)
				return
			}
			result := tracing.Do(client, req, "/value", telemetry.Metadata{})
			if result.Err != nil && result.Status == 0 {
				http.Error(w, "dependency unavailable or timed out", 504)
				return
			}
			if result.Err != nil || result.Status != 200 {
				http.Error(w, "dependency response", 502)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(result.Body)
		})))
	}
	return mux, nil
}
