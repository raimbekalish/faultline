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
	var calculated [3]experiment.Summary
	for i, phase := range []experiment.Phase{r.Baseline, r.Fault, r.Recovery} {
		s := phase.Summary
		if len(phase.Samples) > sp.Samples || s.Requests != len(phase.Samples) || s.Failures < 0 || s.Failures > s.Requests {
			return bad()
		}
		for _, sample := range phase.Samples {
			if sample.DurationMS < 0 || math.IsNaN(sample.DurationMS) || math.IsInf(sample.DurationMS, 0) || (sample.Status != 0 && (sample.Status < 100 || sample.Status > 599)) {
				return bad()
			}
		}
		want := experiment.Summarize(phase.Samples)
		calculated[i] = want
		// Counts/rates and order statistics are exact functions of the serialized
		// samples. Only arithmetic means need a small floating-point allowance.
		if s.Requests != want.Requests || s.Failures != want.Failures || s.FailureRate != want.FailureRate ||
			s.MinMS != want.MinMS || s.MaxMS != want.MaxMS || s.P50MS != want.P50MS || s.P95MS != want.P95MS || s.P99MS != want.P99MS ||
			!sameArithmetic(s.MeanMS, want.MeanMS) {
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
	computed := experiment.Comparison{
		MeanDeltaMS:      calculated[1].MeanMS - calculated[0].MeanMS,
		P95DeltaMS:       calculated[1].P95MS - calculated[0].P95MS,
		FailureRateDelta: calculated[1].FailureRate - calculated[0].FailureRate,
	}
	if len(r.Fault.Samples) > 0 {
		if !sameComparison(r.Comparison, computed) {
			return bad()
		}
	} else {
		// Before fault measurement the engine leaves all deltas zero. A cancel
		// immediately after successful baseline/injection can enter measure with
		// zero samples and still compute empty-fault minus baseline. No stage is
		// persisted, so accept exactly those two possibilities, never a mixture.
		emptyFaultCanceled := r.Status == "canceled" && r.Baseline.Summary.Requests == sp.Samples && r.Baseline.Summary.Failures == 0
		if !sameComparison(r.Comparison, experiment.Comparison{}) && !(emptyFaultCanceled && sameComparison(r.Comparison, computed)) {
			return bad()
		}
	}
	return nil
}

// Four ULPs permit arithmetic roundoff at every scale without an absolute
// epsilon that could accept a fabricated nonzero result for an expected zero.
// Go's normal JSON float64 roundtrip is lossless and needs no rounding repair.
func sameArithmetic(got, want float64) bool {
	if math.IsNaN(got) || math.IsNaN(want) || math.IsInf(got, 0) || math.IsInf(want, 0) {
		return false
	}
	if got == want {
		return true
	}
	if got == 0 || want == 0 || math.Signbit(got) != math.Signbit(want) {
		return false
	}
	a, b := math.Float64bits(math.Abs(got)), math.Float64bits(math.Abs(want))
	if a > b {
		return a-b <= 4
	}
	return b-a <= 4
}

func sameComparison(got, want experiment.Comparison) bool {
	return sameArithmetic(got.MeanDeltaMS, want.MeanDeltaMS) && sameArithmetic(got.P95DeltaMS, want.P95DeltaMS) && sameArithmetic(got.FailureRateDelta, want.FailureRateDelta)
}
