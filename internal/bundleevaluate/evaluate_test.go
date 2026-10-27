package bundleevaluate

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/raimbekalish/faultline/internal/bundle"
	"github.com/raimbekalish/faultline/internal/experiment"
	"github.com/raimbekalish/faultline/internal/spec"
)

// Controlled synthetic evidence; never presented as a measured experiment.
func evidence(t *testing.T, change func(*experiment.Report)) bundle.Evidence {
	t.Helper()
	sp := spec.FromConfig(experiment.Config{Samples: 2, LatencyMS: 80, RequestTimeoutMS: 1000})
	at := time.Date(2026, 10, 27, 1, 0, 0, 0, time.UTC)
	p := experiment.Phase{Samples: []experiment.Sample{{DurationMS: 1, Status: 200}, {DurationMS: 5, Status: 200}}}
	f := experiment.Phase{Samples: []experiment.Sample{{DurationMS: 1, Status: 200}, {DurationMS: 5, Status: 504}}}
	r := experiment.Report{SchemaVersion: 1, RunID: "faultline-" + strings.Repeat("a", 32), Status: "complete", Config: sp.Config(), Target: experiment.Target, StartedAt: at, FinishedAt: at.Add(time.Second), CleanupComplete: true, Baseline: p, Fault: f, Recovery: p}
	if change != nil {
		change(&r)
	}
	r.Baseline.Summary = experiment.Summarize(r.Baseline.Samples)
	r.Fault.Summary = experiment.Summarize(r.Fault.Samples)
	r.Recovery.Summary = experiment.Summarize(r.Recovery.Samples)
	if len(r.Fault.Samples) > 0 {
		r.Comparison = experiment.CompareSummaries(r.Baseline.Summary, r.Fault.Summary)
	}
	hash, _ := sp.Hash()
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	e := bundle.Evidence{History: bundle.Metadata{ID: "history-" + strings.Repeat("b", 32), RunID: r.RunID, SpecHash: hash, CreatedAt: at, FinishedAt: at.Add(2 * time.Second), Status: r.Status}, Spec: sp, Report: raw}
	if err := e.Validate(); err != nil {
		t.Fatal(err)
	}
	return e
}

func evaluate(t *testing.T, e bundle.Evidence, p Policy) Result {
	t.Helper()
	r, err := Evaluate(context.Background(), e, p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestPassFailAndExactThresholds(t *testing.T) {
	e := evidence(t, nil)
	p := policy(t, `{"schema_version":1,"require_complete":true,"require_cleanup":true,"phases":{"baseline":{"min_samples":2,"max_failure_rate":0,"max_mean_ms":3,"max_p95_ms":5},"fault":{"min_failure_rate":0.5,"max_failure_rate":0.5,"max_mean_ms":3,"max_p95_ms":5},"recovery":{"max_failure_rate":0,"max_mean_ms":3,"max_p95_ms":5}}}`)
	r := evaluate(t, e, p)
	if r.Status != Pass || len(r.Requirements) != 2 || len(r.Phases) != 3 {
		t.Fatal(r)
	}
	for _, phase := range r.Phases {
		for _, c := range phase.Checks {
			if c.Status != Pass || c.Actual == nil || c.Reason != "bound_satisfied" {
				t.Fatal(c)
			}
		}
	}
	p.Phases.Fault.MaxMeanMS = ptr(math.Nextafter(3, 0))
	p.Phases.Fault.MaxP95MS = ptr(math.Nextafter(5, 0))
	p.Phases.Fault.MinFailureRate = ptr(math.Nextafter(.5, 1))
	p.Phases.Fault.MaxFailureRate = nil
	r = evaluate(t, e, p)
	if r.Status != Fail || r.Phases[1].Status != Fail {
		t.Fatal(r)
	}
	for _, c := range r.Phases[1].Checks {
		if c.Status != Fail || c.Reason != "bound_violated" {
			t.Fatal(c)
		}
	}
	// The upper failure-rate bound is independently exercised.
	p.Phases.Fault.MinFailureRate = nil
	p.Phases.Fault.MaxFailureRate = ptr(math.Nextafter(.5, 0))
	if evaluate(t, e, p).Phases[1].Checks[0].Status != Fail {
		t.Fatal("maximum failure rate not checked")
	}
}

func ptr[T any](v T) *T { return &v }

func TestIncompleteFailedCanceledAndCleanup(t *testing.T) {
	for _, status := range []string{"failed", "canceled"} {
		for _, count := range []int{0, 1, 2} {
			for _, cleanup := range []bool{false, true} {
				e := evidence(t, func(r *experiment.Report) {
					r.Status, r.Error, r.CleanupComplete = status, "recorded failure", cleanup
					r.Recovery.Samples = r.Recovery.Samples[:count]
				})
				r := evaluate(t, e, policy(t, validPolicy))
				if r.Status != Indeterminate || r.Requirements[0].Status != Indeterminate || r.Requirements[1].Actual != cleanup {
					t.Fatal(r)
				}
				c := r.Phases[2].Checks[0]
				want := "run_not_complete"
				if count == 0 {
					want = "no_observations"
					if c.Actual != nil {
						t.Fatal("missing metrics fabricated as zero", c)
					}
				} else if count == 1 {
					want = "phase_incomplete"
				}
				if c.Status != Indeterminate || c.Reason != want {
					t.Fatal(c)
				}
				for _, phase := range r.Phases {
					for _, check := range phase.Checks {
						if check.Status != Indeterminate {
							t.Fatal("unsuitable run produced a conclusive check", check)
						}
					}
				}
			}
		}
	}
	// A valid pre-engine failed report can contain no run ID or observations.
	e := evidence(t, func(r *experiment.Report) {
		r.Status, r.Error, r.RunID = "failed", "setup failed", ""
		r.Baseline, r.Fault, r.Recovery = experiment.Phase{}, experiment.Phase{}, experiment.Phase{}
	})
	p := policy(t, strings.Replace(validPolicy, `"min_failure_rate":0.5`, `"min_samples":1,"min_failure_rate":0.5`, 1))
	r := evaluate(t, e, p)
	if r.Status != Indeterminate || *r.Phases[1].Checks[0].Actual != 0 || r.Phases[1].Checks[1].Actual != nil {
		t.Fatal(r)
	}
}

func TestSampleSufficiencyAndIndeterminatePrecedence(t *testing.T) {
	p := policy(t, strings.Replace(validPolicy, `"max_p95_ms":50`, `"min_samples":3,"max_p95_ms":50`, 1))
	p.Phases.Fault.MaxMeanMS = ptr(0.0)
	r := evaluate(t, evidence(t, nil), p)
	if r.Status != Indeterminate || r.Phases[1].Status != Fail {
		t.Fatal(r)
	}
	for _, check := range r.Phases[0].Checks {
		if check.Status != Indeterminate || check.Reason != "insufficient_samples" || check.Actual == nil {
			t.Fatal(check)
		}
	}
}

func TestCanonicalStatisticsWithoutThresholdEpsilon(t *testing.T) {
	e := evidence(t, nil)
	var report experiment.Report
	if err := json.Unmarshal(e.Report, &report); err != nil {
		t.Fatal(err)
	}
	// Existing record validation allows four ULPs for a stored arithmetic mean.
	report.Fault.Summary.MeanMS = math.Nextafter(3, math.Inf(1))
	e.Report, _ = json.Marshal(report)
	p := policy(t, strings.Replace(validPolicy, `"min_failure_rate":0.5`, `"max_mean_ms":3`, 1))
	r := evaluate(t, e, p)
	if r.Status != Pass || *r.Phases[1].Checks[0].Actual != 3 {
		t.Fatal(r)
	}
	tiny := evidence(t, func(r *experiment.Report) {
		r.Fault.Samples = []experiment.Sample{{DurationMS: math.SmallestNonzeroFloat64, Status: 200}, {DurationMS: math.SmallestNonzeroFloat64, Status: 200}}
	})
	p.Phases.Fault.MaxMeanMS = ptr(0.0)
	if evaluate(t, tiny, p).Status != Fail {
		t.Fatal("epsilon hid a real positive measurement")
	}
	p.Phases.Fault.MaxMeanMS = ptr(math.SmallestNonzeroFloat64)
	if evaluate(t, tiny, p).Status != Pass {
		t.Fatal("representable boundary rejected")
	}
}

func TestRevalidationDeterminismAndBounds(t *testing.T) {
	e := evidence(t, nil)
	p := policy(t, validPolicy)
	first, err := json.Marshal(evaluate(t, e, p))
	if err != nil {
		t.Fatal(err)
	}
	for range 30 {
		b, _ := json.Marshal(evaluate(t, e, p))
		if string(b) != string(first) {
			t.Fatal("nondeterministic output")
		}
	}
	reordered := policy(t, `{"phases":{"recovery":{"max_mean_ms":50,"max_failure_rate":0},"fault":{"min_failure_rate":0.50},"baseline":{"max_p95_ms":50,"max_failure_rate":0}},"require_cleanup":true,"require_complete":true,"schema_version":1}`)
	b, _ := json.Marshal(evaluate(t, e, reordered))
	if string(b) != string(first) {
		t.Fatal("input field order affected result or policy identity")
	}
	max := Thresholds{ptr(2), ptr(1.0), ptr(0.0), ptr(5.0), ptr(5.0)}
	p.Phases = Phases{&max, &max, &max}
	r := evaluate(t, e, p)
	n := len(r.Requirements)
	for _, phase := range r.Phases {
		n += len(phase.Checks)
	}
	if n != MaxConditions || r.Status != Pass {
		t.Fatal(n, r)
	}
	p.RequireComplete = false
	if _, err := Evaluate(context.Background(), e, p); err == nil {
		t.Fatal("unvalidated policy accepted")
	}
	p = policy(t, validPolicy)
	e.Report = json.RawMessage(strings.Replace(string(e.Report), `"mean_ms":3`, `"mean_ms":2`, 1))
	if _, err := Evaluate(context.Background(), e, p); err == nil {
		t.Fatal("unvalidated raw statistics accepted")
	}
}

type cancelAfter struct {
	context.Context
	checks int
}

func (c *cancelAfter) Err() error {
	c.checks++
	if c.checks >= 4 {
		return context.Canceled
	}
	return nil
}

func TestEvaluationCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p, e := policy(t, validPolicy), evidence(t, nil)
	for _, ctx := range []context.Context{ctx, &cancelAfter{Context: context.Background()}} {
		r, err := Evaluate(ctx, e, p)
		if !errors.Is(err, context.Canceled) || r.Status != "" {
			t.Fatal(r, err)
		}
	}
}
