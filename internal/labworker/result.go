// Package labworker implements dependency-injected custom HTTP measurements.
// There is no production caller, runtime-bound fault driver or Docker execution.
package labworker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"reflect"
	"regexp"

	"github.com/raimbekalish/faultline/internal/experiment"
	"github.com/raimbekalish/faultline/internal/jsonstrict"
	"github.com/raimbekalish/faultline/internal/record"
	"github.com/raimbekalish/faultline/internal/spec"
)

const MaxResultBytes = 128 << 10

var ErrConfiguration = errors.New("invalid closed worker configuration")
var ErrResult = errors.New("invalid custom worker result")
var ErrExecution = errors.New("custom worker measurement incomplete")
var launchID = regexp.MustCompile(`^faultline-lab-[a-f0-9]{32}$`)
var runID = regexp.MustCompile(`^faultline-[a-f0-9]{32}$`)
var revision = regexp.MustCompile(`^[a-f0-9]{40}$`)

type Configuration struct {
	LaunchID            string    `json:"launch_id"`
	RunID               string    `json:"run_id"`
	Specification       spec.Spec `json:"specification"`
	SpecificationSHA256 string    `json:"specification_sha256"`
	SourceRevision      string    `json:"source_revision"`
}

func (c Configuration) validate() error {
	if c.Specification.Validate() != nil || !launchID.MatchString(c.LaunchID) || !runID.MatchString(c.RunID) || !revision.MatchString(c.SourceRevision) {
		return ErrConfiguration
	}
	h, _ := c.Specification.Hash()
	if c.SpecificationSHA256 != h {
		return ErrConfiguration
	}
	return nil
}

type Phase struct {
	Status  string              `json:"status"`
	Samples []experiment.Sample `json:"samples"`
	Summary *experiment.Summary `json:"summary"`
}
type LatencyIntent struct {
	LaunchID  string `json:"launch_id"`
	RunID     string `json:"run_id"`
	LatencyMS int    `json:"latency_ms"`
	Direction string `json:"direction"`
	JitterMS  int    `json:"jitter_ms"`
	Toxicity  int    `json:"toxicity"`
}

func intent(c Configuration) LatencyIntent {
	return LatencyIntent{c.LaunchID, c.RunID, c.Specification.LatencyMS, "downstream", 0, 1}
}

type Fault struct {
	Intent      LatencyIntent `json:"intent"`
	Application string        `json:"application"`
}
type Cleanup struct {
	Status       string `json:"status"`
	Removal      string `json:"removal"`
	Verification string `json:"verification"`
}
type Report struct {
	SchemaVersion            int           `json:"schema_version"`
	Kind                     string        `json:"kind"`
	Configuration            Configuration `json:"configuration"`
	ImplementationCapability string        `json:"implementation_capability"`
	RuntimeQualified         bool          `json:"runtime_qualified"`
	WholeLabCleanup          string        `json:"whole_lab_cleanup"`
	Status                   string        `json:"status"`
	Readiness                Phase         `json:"readiness"`
	Baseline                 Phase         `json:"baseline"`
	Fault                    Phase         `json:"fault"`
	Recovery                 Phase         `json:"recovery"`
	AppliedFault             Fault         `json:"applied_fault"`
	FaultCleanup             Cleanup       `json:"fault_cleanup"`
	Issues                   []string      `json:"issues"`
	Uncertainty              []string      `json:"uncertainty"`
}

func emptyPhase() Phase { return Phase{Status: "unavailable", Samples: []experiment.Sample{}} }
func initial(c Configuration) Report {
	return Report{1, "faultline-custom-worker-result", c, "http-work-measurement-v1", false, "unavailable", "incomplete", emptyPhase(), emptyPhase(), emptyPhase(), emptyPhase(), Fault{intent(c), "not_attempted"}, Cleanup{"not_needed", "not_attempted", "not_attempted"}, []string{}, []string{}}
}
func (r Report) Encode() ([]byte, error) {
	if r.Validate() != nil {
		return nil, ErrResult
	}
	b, e := json.Marshal(r)
	if e != nil || len(b) > MaxResultBytes {
		return nil, ErrResult
	}
	return append(b, '\n'), nil
}
func Decode(ctx context.Context, in io.Reader) (Report, error) {
	if ctx.Err() != nil {
		return Report{}, ctx.Err()
	}
	b, e := io.ReadAll(io.LimitReader(contextReader{ctx, in}, MaxResultBytes+1))
	if ctx.Err() != nil {
		return Report{}, ctx.Err()
	}
	if e != nil || len(b) > MaxResultBytes || jsonstrict.Validate(b) != nil {
		return Report{}, ErrResult
	}
	var r Report
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(&r) != nil || r.Validate() != nil {
		return Report{}, ErrResult
	}
	// Exact field spellings, no omitted/null fields except explicitly nullable
	// summaries. Normal JSON whitespace/key order and float spellings are allowed.
	canonical, _ := json.Marshal(r)
	var a, bv any
	_ = json.Unmarshal(b, &a)
	_ = json.Unmarshal(canonical, &bv)
	if !reflect.DeepEqual(a, bv) {
		return Report{}, ErrResult
	}
	return r, nil
}
func (r Report) Validate() error {
	if r.Configuration.validate() != nil || r.SchemaVersion != 1 || r.Kind != "faultline-custom-worker-result" || r.ImplementationCapability != "http-work-measurement-v1" || r.RuntimeQualified || r.WholeLabCleanup != "unavailable" || r.AppliedFault.Intent != intent(r.Configuration) {
		return ErrResult
	}
	if r.Status != "complete" && r.Status != "incomplete" && r.Status != "canceled" {
		return ErrResult
	}
	if r.Issues == nil || r.Uncertainty == nil || len(r.Issues) > 8 || len(r.Uncertainty) > 8 {
		return ErrResult
	}
	if !unique(r.Issues) || !unique(r.Uncertainty) {
		return ErrResult
	}
	for _, s := range r.Issues {
		if !oneOf(s, "readiness_failed", "baseline_failed", "apply_failed", "fault_interrupted", "cleanup_unconfirmed", "recovery_failed", "run_canceled", "run_deadline") {
			return ErrResult
		}
	}
	for _, s := range r.Uncertainty {
		if !oneOf(s, "fault_application_unknown", "fault_removal_unconfirmed", "measurements_incomplete") {
			return ErrResult
		}
	}
	canceledReason := false
	for _, s := range r.Issues {
		if s == "run_canceled" || s == "run_deadline" {
			canceledReason = true
		}
	}
	if (r.Status == "canceled") != canceledReason {
		return ErrResult
	}
	for i, p := range []Phase{r.Readiness, r.Baseline, r.Fault, r.Recovery} {
		n := r.Configuration.Specification.Samples
		if i == 0 {
			n = readinessRequests
		}
		if validatePhase(p, n, i == 0) != nil {
			return ErrResult
		}
	}
	if !oneOf(r.AppliedFault.Application, "not_attempted", "confirmed", "unknown") {
		return ErrResult
	}
	c := r.FaultCleanup
	if c.Status == "not_needed" {
		if r.AppliedFault.Application != "not_attempted" || c.Removal != "not_attempted" || c.Verification != "not_attempted" {
			return ErrResult
		}
	} else {
		if r.AppliedFault.Application == "not_attempted" || !oneOf(c.Removal, "acknowledged", "unknown") || !oneOf(c.Verification, "absent", "present", "unknown") || !oneOf(c.Status, "confirmed", "unconfirmed") {
			return ErrResult
		}
		if (c.Status == "confirmed") != (c.Removal == "acknowledged" && c.Verification == "absent") {
			return ErrResult
		}
	}
	if len(r.Baseline.Samples) > 0 && r.Readiness.Status != "complete" {
		return ErrResult
	}
	if r.AppliedFault.Application != "not_attempted" && (r.Baseline.Status != "complete" || r.Baseline.Summary.Failures != 0) {
		return ErrResult
	}
	if len(r.Fault.Samples) > 0 && r.AppliedFault.Application != "confirmed" {
		return ErrResult
	}
	if len(r.Recovery.Samples) > 0 && (r.FaultCleanup.Status != "confirmed" || r.Fault.Status != "complete") {
		return ErrResult
	}

	incomplete := r.Baseline.Status != "complete" || r.Fault.Status != "complete" || r.Recovery.Status != "complete"
	if oneOf("measurements_incomplete", r.Uncertainty...) != incomplete || oneOf("fault_application_unknown", r.Uncertainty...) != (r.AppliedFault.Application == "unknown") || oneOf("fault_removal_unconfirmed", r.Uncertainty...) != (c.Status == "unconfirmed") {
		return ErrResult
	}
	if oneOf("cleanup_unconfirmed", r.Issues...) != (c.Status == "unconfirmed") {
		return ErrResult
	}
	if r.Status == "complete" {
		if len(r.Issues) != 0 || len(r.Uncertainty) != 0 || r.Readiness.Status != "complete" || r.Baseline.Status != "complete" || r.Fault.Status != "complete" || r.Recovery.Status != "complete" || r.Baseline.Summary.Failures != 0 || r.Recovery.Summary.Failures != 0 || r.AppliedFault.Application != "confirmed" || c.Status != "confirmed" {
			return ErrResult
		}
	} else if len(r.Issues) == 0 {
		return ErrResult
	}
	return nil
}
func validatePhase(p Phase, n int, ready bool) error {
	if p.Samples == nil || len(p.Samples) > n || !oneOf(p.Status, "unavailable", "partial", "complete") {
		return ErrResult
	}
	if p.Status == "unavailable" {
		if len(p.Samples) != 0 || p.Summary != nil {
			return ErrResult
		}
		return nil
	}
	if p.Summary == nil || len(p.Samples) == 0 {
		return ErrResult
	}
	for index, s := range p.Samples {
		if oneOf(s.Error, "request_canceled", "context_deadline") && (p.Status != "partial" || index != len(p.Samples)-1) {
			return ErrResult
		}
		if s.DurationMS < 0 || math.IsNaN(s.DurationMS) || math.IsInf(s.DurationMS, 0) || s.Status < 0 || s.Status > 999 || s.TraceID != "" || s.SpanID != "" || !oneOf(s.Error, "", "http_status", "invalid_http_status", "request_timeout", "request_canceled", "context_deadline", "connection_error", "transport_error", "body_read_error", "body_too_large") {
			return ErrResult
		}
		if s.Error == "" && (s.Status < 200 || s.Status >= 300) {
			return ErrResult
		}
		if s.Error == "http_status" && (s.Status < 100 || s.Status > 599 || (s.Status >= 200 && s.Status < 300)) {
			return ErrResult
		}
		if s.Error == "invalid_http_status" && s.Status >= 100 && s.Status <= 599 {
			return ErrResult
		}
	}
	want := experiment.Summarize(p.Samples)
	got := *p.Summary
	meanOK := record.SameArithmetic(got.MeanMS, want.MeanMS)
	got.MeanMS = want.MeanMS
	if !meanOK || got != want {
		return ErrResult
	}
	if p.Status == "complete" {
		if ready {
			last := p.Samples[len(p.Samples)-1]
			if last.Error != "" || last.Status < 200 || last.Status >= 300 {
				return ErrResult
			}
		} else if len(p.Samples) != n {
			return ErrResult
		}
	}
	return nil
}
func oneOf(value string, allowed ...string) bool {
	for _, s := range allowed {
		if value == s {
			return true
		}
	}
	return false
}

// Cooperative cancellation; an arbitrary blocked Reader still needs its owner
// to close it. This decoder does not spawn an unbounded goroutine to escape IO.
type contextReader struct {
	ctx context.Context
	in  io.Reader
}

func (r contextReader) Read(b []byte) (int, error) {
	if r.ctx.Err() != nil {
		return 0, r.ctx.Err()
	}
	n, e := r.in.Read(b)
	if r.ctx.Err() != nil {
		return n, r.ctx.Err()
	}
	return n, e
}

func unique(values []string) bool {
	seen := map[string]bool{}
	for _, v := range values {
		if seen[v] {
			return false
		}
		seen[v] = true
	}
	return true
}
