package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/raimbekalish/faultline/internal/experiment"
)

var revision = "development"

func execute(ctx context.Context, args []string, out, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "run" {
		fmt.Fprintln(stderr, "usage: faultline run [-samples 10] [-latency 100ms] [-request-timeout 1s]")
		return 2
	}
	cfg := experiment.DefaultConfig()
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.IntVar(&cfg.Samples, "samples", cfg.Samples, "requests per phase (1–100)")
	latency := fs.Duration("latency", time.Duration(cfg.LatencyMS)*time.Millisecond, "downstream latency (1ms–5s, whole milliseconds)")
	timeout := fs.Duration("request-timeout", time.Duration(cfg.RequestTimeoutMS)*time.Millisecond, "HTTP request timeout (10ms–5s, whole milliseconds)")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	if fs.NArg() != 0 || *latency%time.Millisecond != 0 || *timeout%time.Millisecond != 0 {
		fmt.Fprintln(stderr, "unexpected arguments or fractional milliseconds")
		return 2
	}
	cfg.LatencyMS, cfg.RequestTimeoutMS = int(latency.Milliseconds()), int(timeout.Milliseconds())
	if err := cfg.Validate(); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	report, err := experiment.Run(ctx, cfg, revision)
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if writeErr := enc.Encode(report); writeErr != nil {
		fmt.Fprintln(stderr, "write report:", writeErr)
		return 1
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stderr, "baseline %.2fms → fault %.2fms → recovery %.2fms (mean); fault failures %d/%d; cleanup complete\n", report.Baseline.Summary.MeanMS, report.Fault.Summary.MeanMS, report.Recovery.Summary.MeanMS, report.Fault.Summary.Failures, report.Fault.Summary.Requests)
	return 0
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := execute(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}
