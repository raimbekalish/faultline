// Package execution coordinates optional history around the unchanged engine.
package execution

import (
	"context"
	"errors"
	"fmt"

	"github.com/raimbekalish/faultline/internal/experiment"
	"github.com/raimbekalish/faultline/internal/history"
	"github.com/raimbekalish/faultline/internal/spec"
	"github.com/raimbekalish/faultline/internal/telemetry"
)

type Outcome struct {
	Report    experiment.Report
	HistoryID string
}
type engineFunc func(context.Context, experiment.Config, string, *telemetry.Runtime) (experiment.Report, error)

// Run takes ownership of the optional telemetry runtime, including setup failures.
// A persistence error is returned separately from the unmodified engine report.
func Run(ctx context.Context, sp spec.Spec, revision string, store *history.Store, tracing *telemetry.Runtime) (Outcome, error) {
	return run(ctx, sp, revision, store, tracing, experiment.RunWithTelemetry)
}

func run(ctx context.Context, sp spec.Spec, revision string, store *history.Store, tracing *telemetry.Runtime, engine engineFunc) (out Outcome, err error) {
	defer func() {
		shutdown, cancel := context.WithTimeout(context.Background(), telemetry.ShutdownTimeout)
		stats := tracing.Shutdown(shutdown)
		cancel()
		if out.Report.SchemaVersion != 0 {
			out.Report.Telemetry = stats
		}
		if store == nil || out.HistoryID == "" || out.Report.SchemaVersion == 0 {
			return
		}
		persist, cancel := context.WithTimeout(context.Background(), history.OperationTimeout)
		defer cancel()
		if saveErr := store.Finish(persist, out.HistoryID, out.Report); saveErr != nil {
			err = errors.Join(err, fmt.Errorf("persist history %s: %w", out.HistoryID, saveErr))
		}
	}()
	if err = sp.Validate(); err != nil {
		return out, err
	}
	if store != nil {
		start, cancel := context.WithTimeout(ctx, history.OperationTimeout)
		out.HistoryID, err = store.Start(start, sp)
		cancel()
		if err != nil {
			return out, fmt.Errorf("record run intent; experiment not started: %w", err)
		}
	}
	out.Report, err = engine(ctx, sp.Config(), revision, tracing)
	return out, err
}
