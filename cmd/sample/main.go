package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/raimbekalish/faultline/internal/localhttp"
	"github.com/raimbekalish/faultline/internal/sample"
	"github.com/raimbekalish/faultline/internal/telemetry"
)

func main() {
	role := flag.String("role", "", "frontend or dependency")
	health := flag.Bool("health", false, "check this container's HTTP health")
	flag.Parse()
	client := localhttp.New(sample.DependencyTimeout)
	defer client.CloseIdleConnections()
	if *health {
		r, err := client.Get("http://127.0.0.1:8080/healthz")
		if err != nil {
			log.Fatal(err)
		}
		defer r.Body.Close()
		if r.StatusCode != 200 {
			log.Fatal("unhealthy")
		}
		return
	}
	tracing, err := telemetry.NewFromMode(context.Background(), "faultline-"+*role, os.Getenv("FAULTLINE_TRACING"))
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), telemetry.ShutdownTimeout)
		defer cancel()
		if stats := tracing.Shutdown(ctx); stats != nil && (stats.ShutdownError != "" || stats.ExportFailures > 0) {
			fmt.Fprintln(os.Stderr, "telemetry export incomplete")
		}
	}()
	handler, err := sample.Handler(*role, client, tracing)
	if err != nil {
		log.Fatal(err)
	}
	server := &http.Server{Addr: ":8080", Handler: handler, ReadHeaderTimeout: time.Second, ReadTimeout: 2 * time.Second, WriteTimeout: 2 * time.Second, IdleTimeout: 10 * time.Second, MaxHeaderBytes: 8 << 10}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServe() }()
	select {
	case err := <-done:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			fmt.Fprintln(os.Stderr, err)
			_ = server.Close()
		}
	}
}
