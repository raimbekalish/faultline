package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/raimbekalish/faultline/internal/execution"
	"github.com/raimbekalish/faultline/internal/experiment"
	"github.com/raimbekalish/faultline/internal/history"
	"github.com/raimbekalish/faultline/internal/spec"
	"github.com/raimbekalish/faultline/internal/telemetry"
)

const defaultDB = "artifacts/faultline.sqlite"

func executeRun(ctx context.Context, args []string, out, stderr io.Writer) (code int) {
	cfg := experiment.DefaultConfig()
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.IntVar(&cfg.Samples, "samples", cfg.Samples, "requests per phase (1–100)")
	latency := fs.Duration("latency", time.Duration(cfg.LatencyMS)*time.Millisecond, "downstream latency (1ms–5s, whole milliseconds)")
	timeout := fs.Duration("request-timeout", time.Duration(cfg.RequestTimeoutMS)*time.Millisecond, "HTTP timeout (10ms–5s, whole milliseconds)")
	path := fs.String("spec", "", "versioned experiment JSON specification")
	dbPath := fs.String("db", "", "local history file (default with -spec: "+defaultDB+")")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 || *latency%time.Millisecond != 0 || *timeout%time.Millisecond != 0 {
		fmt.Fprintln(stderr, "unexpected arguments or fractional milliseconds")
		return 2
	}
	cfg.LatencyMS, cfg.RequestTimeoutMS = int(latency.Milliseconds()), int(timeout.Milliseconds())
	sp := spec.FromConfig(cfg)
	if *path != "" {
		override := false
		fs.Visit(func(f *flag.Flag) {
			if f.Name == "samples" || f.Name == "latency" || f.Name == "request-timeout" {
				override = true
			}
		})
		if override {
			fmt.Fprintln(stderr, "specification fields cannot be overridden by workload flags")
			return 2
		}
		f, err := os.Open(*path)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		sp, err = spec.Decode(f)
		_ = f.Close()
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		if *dbPath == "" {
			*dbPath = defaultDB
		}
	}
	if err := sp.Validate(); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	var store *history.Store
	if *dbPath != "" {
		setup, cancel := context.WithTimeout(ctx, history.OperationTimeout)
		var err error
		store, err = history.Open(setup, *dbPath)
		cancel()
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		defer func() {
			if err := store.Close(); err != nil {
				fmt.Fprintln(stderr, "close history:", err)
				code = 1
			}
		}()
	}
	tracing, err := telemetry.NewFromMode(ctx, "faultline-runner", os.Getenv("FAULTLINE_TRACING"))
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	result, err := execution.Run(ctx, sp, revision, store, tracing)
	if result.HistoryID != "" {
		fmt.Fprintln(stderr, "history ID:", result.HistoryID)
	}
	if result.Report.SchemaVersion != 0 {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if writeErr := enc.Encode(result.Report); writeErr != nil {
			fmt.Fprintln(stderr, "write report:", writeErr)
			if err != nil {
				fmt.Fprintln(stderr, err)
			}
			return 1
		}
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	r := result.Report
	fmt.Fprintf(stderr, "baseline %.2fms → fault %.2fms → recovery %.2fms (mean); fault failures %d/%d; cleanup complete\n", r.Baseline.Summary.MeanMS, r.Fault.Summary.MeanMS, r.Recovery.Summary.MeanMS, r.Fault.Summary.Failures, r.Fault.Summary.Requests)
	return 0
}
