package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/raimbekalish/faultline/internal/experiment"
	"github.com/raimbekalish/faultline/internal/telemetry"
	"github.com/raimbekalish/faultline/internal/tracequery"
)

// evidence is a reproducible diagnostic fixture with real observations and the
// corresponding raw Jaeger query responses. It makes no automatic root-cause claim.
func evidence(ctx context.Context, out, stderr io.Writer) int {
	if os.Getenv("FAULTLINE_TRACING") != "jaeger" {
		fmt.Fprintln(stderr, "evidence requires --tracing")
		return 2
	}
	result := struct {
		SchemaVersion int                 `json:"schema_version"`
		Reports       []experiment.Report `json:"reports"`
		Traces        []tracequery.Record `json:"traces"`
	}{SchemaVersion: 1}
	code := 0
	query := tracequery.New()
	defer query.Close()
	for _, latency := range []int{80, 400} {
		rt, err := telemetry.NewFromMode(ctx, "faultline-runner", "jaeger")
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		r, runErr := experiment.RunWithTelemetry(ctx, experiment.Config{Samples: 5, LatencyMS: latency, RequestTimeoutMS: 1000}, revision, rt)
		shutdown, cancel := context.WithTimeout(context.Background(), telemetry.ShutdownTimeout)
		r.Telemetry = rt.Shutdown(shutdown)
		cancel()
		result.Reports = append(result.Reports, r)
		if runErr != nil {
			fmt.Fprintln(stderr, runErr)
			code = 1
			break
		}
		var ids []string
		for _, phase := range []experiment.Phase{r.Baseline, r.Fault, r.Recovery} {
			for _, s := range phase.Samples {
				if s.TraceID != "" {
					ids = append(ids, s.TraceID)
				}
			}
		}
		collect, cancel := context.WithTimeout(ctx, 5*time.Second)
		traces, queryErr := query.Collect(collect, ids)
		cancel()
		if queryErr != nil {
			fmt.Fprintln(stderr, queryErr)
			code = 1
		}
		for _, trace := range traces {
			if !trace.Complete {
				code = 1
			}
		}
		result.Traces = append(result.Traces, traces...)
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(result); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return code
}
