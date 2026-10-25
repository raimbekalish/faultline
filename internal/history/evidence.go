package history

import (
	"fmt"
	"github.com/raimbekalish/faultline/internal/bundle"
)

var ErrNoReport = fmt.Errorf("unfinished intent has no finalized report to export")

// Evidence adapts a validated history entry; the portable package imports no SQL.
func Evidence(e Entry) (bundle.Evidence, error) {
	if e.Status == "running" || e.FinishedAt == nil || len(e.Report) == 0 {
		return bundle.Evidence{}, ErrNoReport
	}
	result := bundle.Evidence{History: bundle.Metadata{ID: e.ID, RunID: e.RunID, SpecHash: e.SpecHash, CreatedAt: e.CreatedAt, FinishedAt: *e.FinishedAt, Status: e.Status}, Spec: e.Spec, Report: e.Report}
	if err := result.Validate(); err != nil {
		return bundle.Evidence{}, fmt.Errorf("%w: evidence validation failed", ErrCorrupt)
	}
	return result, nil
}
