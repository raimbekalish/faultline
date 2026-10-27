package bundleevaluate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/raimbekalish/faultline/internal/bundle"
	"github.com/raimbekalish/faultline/internal/experiment"
)

type Status string

const (
	Pass          Status = "PASS"
	Fail          Status = "FAIL"
	Indeterminate Status = "INDETERMINATE"
)

type Identity struct {
	BundleFormatVersion int    `json:"bundle_format_version"`
	HistoryID           string `json:"history_id"`
	RunID               string `json:"run_id"`
	RunStatus           string `json:"run_status"`
	SpecHash            string `json:"spec_hash"`
	ReportSHA256        string `json:"report_sha256"`
}

type Requirement struct {
	Condition string `json:"condition"`
	Actual    bool   `json:"actual"`
	Required  bool   `json:"required"`
	Status    Status `json:"status"`
	Reason    string `json:"reason"`
}

type Check struct {
	Condition string   `json:"condition"`
	Actual    *float64 `json:"actual"`
	Threshold float64  `json:"threshold"`
	Unit      string   `json:"unit"`
	Operator  string   `json:"operator"`
	Status    Status   `json:"status"`
	Reason    string   `json:"reason"`
}

type Phase struct {
	Phase             string  `json:"phase"`
	SamplesRecorded   int     `json:"samples_recorded"`
	SamplesConfigured int     `json:"samples_configured"`
	Availability      string  `json:"availability"`
	Status            Status  `json:"status"`
	Checks            []Check `json:"checks"`
}

type Result struct {
	SchemaVersion       int           `json:"schema_version"`
	Kind                string        `json:"kind"`
	PolicySchemaVersion int           `json:"policy_schema_version"`
	PolicySHA256        string        `json:"policy_sha256"`
	Evidence            Identity      `json:"evidence"`
	Status              Status        `json:"status"`
	Requirements        []Requirement `json:"requirements"`
	Phases              []Phase       `json:"phases"`
	Notes               []string      `json:"notes"`
}

// Evaluate revalidates the policy and logical evidence. The caller must first
// use bundle.Read/ReadFile for ZIP, checksum and format validation. It never
// repairs evidence. Actual metrics use the engine's sole summarizer after that
// validation, avoiding decisions on the validator's permitted mean roundoff.
func Evaluate(ctx context.Context, e bundle.Evidence, p Policy) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if err := p.Validate(); err != nil {
		return Result{}, err
	}
	if err := e.Validate(); err != nil {
		return Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	var report experiment.Report
	if err := json.Unmarshal(e.Report, &report); err != nil {
		return Result{}, err
	}
	canonical, err := json.Marshal(p)
	if err != nil {
		return Result{}, err
	}
	ph, rh := sha256.Sum256(canonical), sha256.Sum256(e.Report)
	r := Result{SchemaVersion: 1, Kind: "offline_reliability_evaluation", PolicySchemaVersion: p.SchemaVersion, PolicySHA256: hex.EncodeToString(ph[:]), Evidence: Identity{1, e.History.ID, e.History.RunID, e.History.Status, e.History.SpecHash, hex.EncodeToString(rh[:])}, Status: Pass, Phases: []Phase{}}
	r.Requirements = []Requirement{requirement("run_complete", report.Status == "complete", "run_not_complete"), requirement("cleanup_complete", report.CleanupComplete, "cleanup_unconfirmed")}
	for _, req := range r.Requirements {
		r.Status = combine(r.Status, req.Status)
	}
	for i, thresholds := range []*Thresholds{p.Phases.Baseline, p.Phases.Fault, p.Phases.Recovery} {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		if thresholds == nil {
			continue
		}
		name := []string{"baseline", "fault", "recovery"}[i]
		phase := []experiment.Phase{report.Baseline, report.Fault, report.Recovery}[i]
		summary := experiment.Summarize(phase.Samples)
		result := Phase{Phase: name, SamplesRecorded: summary.Requests, SamplesConfigured: e.Spec.Samples, Availability: "complete", Status: Pass, Checks: []Check{}}
		if summary.Requests == 0 {
			result.Availability = "missing"
		} else if summary.Requests != e.Spec.Samples {
			result.Availability = "partial"
		}
		reason := ""
		switch {
		case summary.Requests == 0:
			reason = "no_observations"
		case summary.Requests != e.Spec.Samples:
			reason = "phase_incomplete"
		case thresholds.MinSamples != nil && summary.Requests < *thresholds.MinSamples:
			reason = "insufficient_samples"
		case report.Status != "complete":
			reason = "run_not_complete"
		case !report.CleanupComplete:
			reason = "cleanup_unconfirmed"
		}
		add := func(condition, unit, operator string, threshold, actual float64, available bool) {
			c := Check{Condition: condition, Threshold: threshold, Unit: unit, Operator: operator, Status: Pass, Reason: "bound_satisfied"}
			if available {
				c.Actual = &actual
			}
			if reason != "" {
				c.Status, c.Reason = Indeterminate, reason
			} else if (operator == "<=" && actual > threshold) || (operator == ">=" && actual < threshold) {
				c.Status, c.Reason = Fail, "bound_violated"
			}
			result.Status = combine(result.Status, c.Status)
			result.Checks = append(result.Checks, c)
		}
		if thresholds.MinSamples != nil {
			add("min_samples", "samples", ">=", float64(*thresholds.MinSamples), float64(summary.Requests), true)
		}
		for _, c := range []struct {
			name, unit, operator string
			threshold            *float64
			actual               float64
		}{
			{"max_failure_rate", "fraction", "<=", thresholds.MaxFailureRate, summary.FailureRate},
			{"min_failure_rate", "fraction", ">=", thresholds.MinFailureRate, summary.FailureRate},
			{"max_mean_ms", "ms", "<=", thresholds.MaxMeanMS, summary.MeanMS},
			{"max_p95_ms", "ms", "<=", thresholds.MaxP95MS, summary.P95MS},
		} {
			if c.threshold != nil {
				add(c.name, c.unit, c.operator, *c.threshold, c.actual, summary.Requests > 0)
			}
		}
		r.Status = combine(r.Status, result.Status)
		r.Phases = append(r.Phases, result)
	}
	r.Notes = []string{
		"Decisions describe recorded samples under an explicit policy, not statistical guarantees, causal inference, production readiness or SLO compliance.",
		"Metrics include all HTTP outcomes and use the existing engine summarizer; failure rates are fractions and latency is end-to-end milliseconds.",
		"Missing, partial, undersampled or unsuitable evidence is indeterminate and takes precedence over threshold failures; unavailable metrics are null, not zero.",
		"Hashes identify consistent content, not authenticity or independently executed experiments; trace availability is not inferred.",
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	return r, nil
}

func requirement(name string, actual bool, reason string) Requirement {
	r := Requirement{Condition: name, Actual: actual, Required: true, Status: Pass, Reason: "requirement_satisfied"}
	if !actual {
		r.Status, r.Reason = Indeterminate, reason
	}
	return r
}

func combine(a, b Status) Status {
	if a == Indeterminate || b == Indeterminate {
		return Indeterminate
	}
	if a == Fail || b == Fail {
		return Fail
	}
	return Pass
}
