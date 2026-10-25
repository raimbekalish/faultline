package history

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"testing"

	"github.com/raimbekalish/faultline/internal/experiment"
	"github.com/raimbekalish/faultline/internal/record"
	"github.com/raimbekalish/faultline/internal/spec"
)

func twoSampleReport(status string) (experiment.Report, spec.Spec) {
	sp := spec.FromConfig(experiment.Config{Samples: 2, LatencyMS: 80, RequestTimeoutMS: 1000})
	r := testReport(status, 1)
	r.Config = sp.Config()
	phase := func() experiment.Phase {
		return experiment.Phase{
			Samples: []experiment.Sample{{DurationMS: 1, Status: 200}, {DurationMS: 5, Status: 200}},
			Summary: experiment.Summary{Requests: 2, MinMS: 1, MeanMS: 3, P50MS: 1, P95MS: 5, P99MS: 5, MaxMS: 5},
		}
	}
	r.Baseline, r.Fault, r.Recovery = phase(), phase(), phase()
	return r, sp
}

func TestRejectsPlausibleMeanMismatch(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	r, sp := twoSampleReport("complete")
	id, err := s.Start(ctx, sp)
	if err != nil {
		t.Fatal(err)
	}
	r.Fault.Summary.MeanMS = 2 // Inside [1,5], but the raw-sample mean is 3.
	if err := s.Finish(ctx, id, r); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("accepted plausible but incorrect mean: %v", err)
	}
}

func phaseFromSamples(samples ...experiment.Sample) experiment.Phase {
	return experiment.Phase{Samples: samples, Summary: experiment.Summarize(samples)}
}

func updateComparison(r *experiment.Report) {
	a, b := experiment.Summarize(r.Baseline.Samples), experiment.Summarize(r.Fault.Samples)
	r.Comparison = experiment.Comparison{MeanDeltaMS: b.MeanMS - a.MeanMS, P95DeltaMS: b.P95MS - a.P95MS, FailureRateDelta: b.FailureRate - a.FailureRate}
}

func hundredSampleReport() (experiment.Report, spec.Spec) {
	r, sp := twoSampleReport("complete")
	sp.Samples = 100
	r.Config = sp.Config()
	phase := func() experiment.Phase {
		samples := make([]experiment.Sample, 100)
		for i := range samples {
			samples[i] = experiment.Sample{DurationMS: float64(100 - i), Status: 200}
		}
		// Independent known arithmetic and nearest-rank order statistics.
		return experiment.Phase{Samples: samples, Summary: experiment.Summary{Requests: 100, MinMS: 1, MeanMS: 50.5, P50MS: 50, P95MS: 95, P99MS: 99, MaxMS: 100}}
	}
	r.Baseline, r.Fault, r.Recovery = phase(), phase(), phase()
	return r, sp
}

func TestEverySummaryFieldMatchesRawSamples(t *testing.T) {
	for _, tc := range []struct {
		name  string
		alter func(*experiment.Summary)
	}{
		{"requests", func(s *experiment.Summary) { s.Requests = 99 }},
		{"failures", func(s *experiment.Summary) { s.Failures = 1; s.FailureRate = .01 }},
		{"failure rate", func(s *experiment.Summary) { s.FailureRate = .01 }},
		{"min inside plausible range", func(s *experiment.Summary) { s.MinMS = .5 }},
		{"max inside plausible range", func(s *experiment.Summary) { s.MaxMS = 101 }},
		{"mean inside min max", func(s *experiment.Summary) { s.MeanMS = 50 }},
		{"p50 monotonic", func(s *experiment.Summary) { s.P50MS = 49 }},
		{"p95 monotonic", func(s *experiment.Summary) { s.P95MS = 94 }},
		{"p99 monotonic", func(s *experiment.Summary) { s.P99MS = 98 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, sp := hundredSampleReport()
			if err := validateReport(r, sp); err != nil {
				t.Fatal(err)
			}
			tc.alter(&r.Fault.Summary)
			if err := validateReport(r, sp); !errors.Is(err, ErrCorrupt) {
				t.Fatalf("accepted inconsistent %s: %v", tc.name, err)
			}
		})
	}
}

func TestSummaryUsesAllOutcomes(t *testing.T) {
	r, sp := twoSampleReport("complete")
	sp.Samples = 5
	r.Config = sp.Config()
	baseline := []experiment.Sample{{DurationMS: 5, Status: 200}, {DurationMS: 1, Status: 200}, {DurationMS: 3, Status: 200}, {DurationMS: 2, Status: 200}, {DurationMS: 4, Status: 200}}
	r.Baseline = phaseFromSamples(baseline...)
	r.Recovery = phaseFromSamples(append([]experiment.Sample(nil), baseline...)...)
	r.Fault = experiment.Phase{Samples: []experiment.Sample{
		{DurationMS: 5, Status: 200, Error: "body read failed"}, {DurationMS: 1, Status: 0, Error: "timeout"},
		{DurationMS: 3, Status: 302}, {DurationMS: 2, Status: 200}, {DurationMS: 4, Status: 504},
	}, Summary: experiment.Summary{Requests: 5, Failures: 4, FailureRate: .8, MinMS: 1, MeanMS: 3, P50MS: 3, P95MS: 5, P99MS: 5, MaxMS: 5}}
	r.Comparison = experiment.Comparison{FailureRateDelta: .8}
	if err := validateReport(r, sp); err != nil {
		t.Fatalf("valid mixed outcomes rejected: %v", err)
	}
	r.Fault.Summary.Failures = 3
	r.Fault.Summary.FailureRate = .6
	if err := validateReport(r, sp); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("ignored read/transport/HTTP failure: %v", err)
	}
}

func TestValidPartialAndEmptyReports(t *testing.T) {
	for _, status := range []string{"failed", "canceled"} {
		for _, stage := range []string{"empty", "baseline partial", "before fault", "fault partial", "recovery partial"} {
			t.Run(status+"/"+stage, func(t *testing.T) {
				r, sp := twoSampleReport(status)
				switch stage {
				case "empty":
					r.Baseline = experiment.Phase{}
					r.Fault = experiment.Phase{}
					r.Recovery = experiment.Phase{}
				case "baseline partial":
					r.Baseline = phaseFromSamples(experiment.Sample{DurationMS: 1, Status: 200})
					r.Fault = experiment.Phase{}
					r.Recovery = experiment.Phase{}
				case "before fault":
					r.Fault = experiment.Phase{}
					r.Recovery = experiment.Phase{}
				case "fault partial":
					r.Fault = phaseFromSamples(experiment.Sample{DurationMS: 3, Status: 504})
					r.Recovery = experiment.Phase{}
					updateComparison(&r)
				case "recovery partial":
					r.Recovery = phaseFromSamples(experiment.Sample{DurationMS: 1, Status: 200})
				}
				b, err := json.Marshal(r)
				if err != nil {
					t.Fatal(err)
				}
				var roundtrip experiment.Report
				if err := json.Unmarshal(b, &roundtrip); err != nil {
					t.Fatal(err)
				}
				if err := validateReport(roundtrip, sp); err != nil {
					t.Fatalf("valid partial report rejected: %v", err)
				}
				roundtrip.Recovery.Summary.MeanMS = math.SmallestNonzeroFloat64
				if err := validateReport(roundtrip, sp); !errors.Is(err, ErrCorrupt) {
					t.Fatalf("inconsistent partial/empty summary accepted: %v", err)
				}
			})
		}
	}
	r, sp := twoSampleReport("canceled")
	r.RunID = ""
	r.CleanupComplete = false
	r.Baseline = experiment.Phase{}
	r.Fault = experiment.Phase{}
	r.Recovery = experiment.Phase{}
	if err := validateReport(r, sp); err != nil {
		t.Fatalf("pre-engine cancellation rejected: %v", err)
	}
}

func TestArithmeticRoundoffAndSerialization(t *testing.T) {
	for _, values := range [][]float64{{.1, .2, .3}, {1e-200, 2e-200}, {1e200, 2e200}, {0, 0}} {
		r, sp := twoSampleReport("complete")
		sp.Samples = len(values)
		r.Config = sp.Config()
		makePhase := func() experiment.Phase {
			samples := make([]experiment.Sample, len(values))
			for i, v := range values {
				samples[i] = experiment.Sample{DurationMS: v, Status: 200}
			}
			return phaseFromSamples(samples...)
		}
		r.Baseline, r.Fault, r.Recovery = makePhase(), makePhase(), makePhase()
		b, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		var restored experiment.Report
		if err := json.Unmarshal(b, &restored); err != nil {
			t.Fatal(err)
		}
		if err := validateReport(restored, sp); err != nil {
			t.Fatalf("lossless float roundtrip rejected (%v): %v", values, err)
		}
		if r.Fault.Summary.MeanMS == 0 {
			r.Fault.Summary.MeanMS = math.SmallestNonzeroFloat64
			if err := validateReport(r, sp); !errors.Is(err, ErrCorrupt) {
				t.Fatal("fabricated nonzero mean accepted")
			}
			continue
		}
		for range 4 {
			r.Fault.Summary.MeanMS = math.Nextafter(r.Fault.Summary.MeanMS, math.Inf(1))
		}
		if err := validateReport(r, sp); err != nil {
			t.Fatalf("four-ULP mean roundoff rejected: %v", err)
		}
		r.Fault.Summary.MeanMS = math.Nextafter(r.Fault.Summary.MeanMS, math.Inf(1))
		if err := validateReport(r, sp); !errors.Is(err, ErrCorrupt) {
			t.Fatal("mean drift outside tolerance accepted")
		}
	}
	r, sp := twoSampleReport("complete")
	r.Fault.Summary.MinMS = math.Nextafter(1, 0)
	if err := validateReport(r, sp); !errors.Is(err, ErrCorrupt) {
		t.Fatal("non-sample order statistic accepted")
	}
	r, sp = twoSampleReport("complete")
	for i := range r.Fault.Samples {
		r.Fault.Samples[i].DurationMS = math.MaxFloat64
	}
	r.Fault.Summary = experiment.Summary{Requests: 2, MinMS: math.MaxFloat64, MeanMS: math.MaxFloat64, P50MS: math.MaxFloat64, P95MS: math.MaxFloat64, P99MS: math.MaxFloat64, MaxMS: math.MaxFloat64}
	if err := validateReport(r, sp); !errors.Is(err, ErrCorrupt) {
		t.Fatal("nonfinite recomputed sum accepted")
	}
	for _, bad := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if record.SameArithmetic(bad, bad) {
			t.Fatal("nonfinite arithmetic accepted")
		}
	}
}

func TestDerivedComparisonIntegrityAndCanceledBoundary(t *testing.T) {
	for _, alter := range []func(*experiment.Comparison){
		func(c *experiment.Comparison) { c.MeanDeltaMS = 1 }, func(c *experiment.Comparison) { c.P95DeltaMS = 1 }, func(c *experiment.Comparison) { c.FailureRateDelta = .5 },
	} {
		r, sp := twoSampleReport("complete")
		alter(&r.Comparison)
		if err := validateReport(r, sp); !errors.Is(err, ErrCorrupt) {
			t.Fatal("fabricated derived comparison accepted")
		}
	}
	r, sp := twoSampleReport("canceled")
	r.Fault = experiment.Phase{}
	r.Recovery = experiment.Phase{}
	if err := validateReport(r, sp); err != nil {
		t.Fatal("before-fault zero comparison rejected", err)
	}
	r.Comparison = experiment.Comparison{MeanDeltaMS: -3, P95DeltaMS: -5}
	if err := validateReport(r, sp); err != nil {
		t.Fatal("empty-fault computed comparison rejected", err)
	}
	r.Comparison.P95DeltaMS = 0
	if err := validateReport(r, sp); !errors.Is(err, ErrCorrupt) {
		t.Fatal("mixed uncomputed/computed deltas accepted")
	}
	r.Comparison = experiment.Comparison{MeanDeltaMS: -3, P95DeltaMS: -5}
	r.Status = "failed"
	if err := validateReport(r, sp); !errors.Is(err, ErrCorrupt) {
		t.Fatal("computed empty-fault comparison accepted before fault measurement")
	}
}

func TestSummaryIntegrityAtWriteAndReadBoundaries(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	r, sp := twoSampleReport("complete")
	id, err := s.Start(ctx, sp)
	if err != nil {
		t.Fatal(err)
	}
	r.Fault.Summary.MeanMS = 2
	if err := s.Finish(ctx, id, r); !errors.Is(err, ErrCorrupt) {
		t.Fatal(err)
	}
	e, err := s.Get(ctx, id)
	if err != nil || e.Status != "running" || e.Report != nil {
		t.Fatalf("invalid finalization changed intent: %+v %v", e, err)
	}
	r.Fault.Summary.MeanMS = 3
	if err := s.Finish(ctx, id, r); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("UPDATE runs SET report_json=json_set(report_json,'$.fault.summary.mean_ms',2) WHERE id=?", id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, id); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("corrupt read accepted: %v", err)
	}
	if _, err := s.List(ctx, 20); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("corrupt list accepted: %v", err)
	}
	if _, err := s.Compare(ctx, id, id); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("corrupt comparison accepted: %v", err)
	}
}
