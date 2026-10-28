// Independent contract application: standard library only, no Faultline imports.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const dependency = "http://toxiproxy:8666/value"

func application(client *http.Client) http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	m.HandleFunc("GET /work", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 250*time.Millisecond)
		defer cancel()
		req, e := http.NewRequestWithContext(ctx, http.MethodGet, dependency, nil)
		if e != nil {
			w.WriteHeader(502)
			return
		}
		resp, e := client.Do(req)
		if e != nil {
			if dependencyTimeout(ctx, e) {
				w.WriteHeader(504)
			} else {
				w.WriteHeader(502)
			}
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			w.WriteHeader(502)
			return
		}
		b, e := io.ReadAll(io.LimitReader(resp.Body, 4097))
		if dependencyTimeout(ctx, e) {
			w.WriteHeader(504)
			return
		}
		var value struct {
			Value int `json:"value"`
		}
		if e != nil || len(b) > 4096 || json.Unmarshal(b, &value) != nil || value.Value != 42 {
			w.WriteHeader(502)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			PriceUnits int `json:"price_units"`
		}{value.Value + 1})
	})
	return m
}
func main() {
	if len(os.Args) != 1 || os.Getenv("FAULTLINE_DEPENDENCY_URL") != dependency || os.Getenv("FAULTLINE_DEPENDENCY_TIMEOUT_MS") != "250" {
		os.Exit(2)
	}
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, MaxConnsPerHost: 1, MaxIdleConns: 1, MaxIdleConnsPerHost: 1, ResponseHeaderTimeout: 250 * time.Millisecond, MaxResponseHeaderBytes: 8192}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 250 * time.Millisecond, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	s := &http.Server{Addr: ":8080", Handler: application(client), ReadHeaderTimeout: time.Second, ReadTimeout: 2 * time.Second, WriteTimeout: 2 * time.Second, IdleTimeout: 5 * time.Second, MaxHeaderBytes: 8192, ErrorLog: log.New(io.Discard, "", 0)}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		c, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = s.Shutdown(c)
	}()
	if e := s.ListenAndServe(); e != nil && e != http.ErrServerClosed {
		os.Exit(1)
	}
}

func dependencyTimeout(ctx context.Context, err error) bool {
	var timeout net.Error
	return errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &timeout) && timeout.Timeout())
}
