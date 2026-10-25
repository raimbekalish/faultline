package history

import (
	"fmt"
	"math"

	"github.com/raimbekalish/faultline/internal/experiment"
	"github.com/raimbekalish/faultline/internal/spec"
)

func validateReport(r experiment.Report, sp spec.Spec) error {
	bad := func() error { return fmt.Errorf("%w: invalid experiment report", ErrCorrupt) }
	if r.SchemaVersion != 1 || r.Config != sp.Config() || r.Target != experiment.Target || r.StartedAt.IsZero() || r.FinishedAt.Before(r.StartedAt) {
		return bad()
	}
	if r.Status != "complete" && r.Status != "failed" && r.Status != "canceled" {
		return bad()
	}
	if r.RunID != "" && !engineID.MatchString(r.RunID) {
		return bad()
	}
	if r.Status == "complete" && (r.RunID == "" || !r.CleanupComplete || r.Error != "") {
		return bad()
	}
	if r.Status != "complete" && r.Error == "" {
		return bad()
	}
	for _, phase := range []experiment.Phase{r.Baseline, r.Fault, r.Recovery} {
		s := phase.Summary
		if len(phase.Samples) > sp.Samples || s.Requests != len(phase.Samples) || s.Failures < 0 || s.Failures > s.Requests {
			return bad()
		}
		failures := 0
		for _, sample := range phase.Samples {
			if sample.DurationMS < 0 || math.IsNaN(sample.DurationMS) || math.IsInf(sample.DurationMS, 0) || (sample.Status != 0 && (sample.Status < 100 || sample.Status > 599)) {
				return bad()
			}
			if sample.Error != "" || sample.Status < 200 || sample.Status >= 300 {
				failures++
			}
		}
		if failures != s.Failures {
			return bad()
		}
		for _, v := range []float64{s.MinMS, s.MeanMS, s.P50MS, s.P95MS, s.P99MS, s.MaxMS} {
			if v < 0 || math.IsNaN(v) || math.IsInf(v, 0) {
				return bad()
			}
			if s.Requests == 0 && v != 0 {
				return bad()
			}
		}
		if s.Requests > 0 && (s.MinMS > s.MeanMS+1e-9 || s.MeanMS > s.MaxMS+1e-9 || s.MinMS > s.P50MS || s.P50MS > s.P95MS || s.P95MS > s.P99MS || s.P99MS > s.MaxMS) {
			return bad()
		}
		rate := 0.0
		if s.Requests > 0 {
			rate = float64(s.Failures) / float64(s.Requests)
		}
		if rate != s.FailureRate {
			return bad()
		}
		if r.Status == "complete" && s.Requests != sp.Samples {
			return bad()
		}
		if r.RunID == "" && s.Requests != 0 {
			return bad()
		}
	}
	if r.Status == "complete" && (r.Baseline.Summary.Failures != 0 || r.Recovery.Summary.Failures != 0) {
		return bad()
	}
	return nil
}
