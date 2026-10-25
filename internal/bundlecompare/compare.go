// Package bundlecompare performs deterministic descriptive arithmetic on evidence.
// It has no filesystem, database, environment capture or network operations.
package bundlecompare

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"regexp"

	"github.com/raimbekalish/faultline/internal/bundle"
	"github.com/raimbekalish/faultline/internal/experiment"
	"github.com/raimbekalish/faultline/internal/spec"
)

type Property struct {
	Field string `json:"field"`
	State string `json:"state"`
	Left  any    `json:"left"`
	Right any    `json:"right"`
}
type Side struct {
	BundleFormatVersion int             `json:"bundle_format_version"`
	History             bundle.Metadata `json:"history"`
	Spec                spec.Spec       `json:"spec"`
	ReportSHA256        string          `json:"report_sha256"`
	CleanupComplete     bool            `json:"cleanup_complete"`
}
type Measurement struct {
	Availability      string              `json:"availability"`
	SamplesRecorded   int                 `json:"samples_recorded"`
	SamplesConfigured int                 `json:"samples_configured"`
	Summary           *experiment.Summary `json:"summary"`
}
type Change struct {
	Absolute        float64  `json:"absolute"`
	Unit            string   `json:"unit"`
	RelativePercent *float64 `json:"relative_percent"`
	RelativeStatus  string   `json:"relative_status"`
}
type Changes struct {
	Mean        Change `json:"mean_latency"`
	P95         Change `json:"p95_latency"`
	FailureRate Change `json:"failure_rate"`
}
type Phase struct {
	Name    string      `json:"phase"`
	Left    Measurement `json:"left"`
	Right   Measurement `json:"right"`
	Changes *Changes    `json:"right_minus_left"`
}
type Reason struct {
	Code    string `json:"code"`
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
}
type Assessment struct {
	Classification       string   `json:"classification"`
	DescriptiveDeltas    bool     `json:"descriptive_deltas_available"`
	ControlledExperiment bool     `json:"controlled_experiment_established"`
	SignificanceAssessed bool     `json:"statistical_significance_assessed"`
	IdentityRelation     string   `json:"identity_relation"`
	Reasons              []Reason `json:"reasons"`
}
type Result struct {
	SchemaVersion int        `json:"schema_version"`
	Kind          string     `json:"kind"`
	Left          Side       `json:"left"`
	Right         Side       `json:"right"`
	SameSpec      bool       `json:"same_spec"`
	Configuration []Property `json:"configuration"`
	Provenance    []Property `json:"provenance"`
	Assessment    Assessment `json:"assessment"`
	Phases        []Phase    `json:"phases"`
	Notes         []string   `json:"notes"`
}

var cleanRevision = regexp.MustCompile(`^[a-f0-9]{7,40}$`)
var provenanceFields = []string{"environment_schema_version", "faultline_revision", "runner_os", "runner_arch", "go_version", "measurement_methodology", "tracing_enabled", "docker_server_version", "toxiproxy_reported_version", "images.sample.image_id", "images.sample.reference", "images.toxiproxy.image_id", "images.toxiproxy.reference", "images.jaeger.image_id", "images.jaeger.reference"}
var requiredConditions = []string{"environment_schema_version", "faultline_revision", "runner_os", "runner_arch", "go_version", "measurement_methodology", "tracing_enabled", "docker_server_version", "images.sample.image_id", "images.toxiproxy.image_id", "images.toxiproxy.reference"}

// Compare revalidates both logical payloads. The caller must independently use
// bundle.Read/ReadFile to validate archives and hashes before supplying evidence.
func Compare(ctx context.Context, left, right bundle.Evidence) (Result, error) {
	var result Result
	for i, e := range []bundle.Evidence{left, right} {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if err := e.Validate(); err != nil {
			return result, fmt.Errorf("input %d: %w", i+1, err)
		}
	}
	var l, r experiment.Report
	if err := json.Unmarshal(left.Report, &l); err != nil {
		return result, err
	}
	if err := json.Unmarshal(right.Report, &r); err != nil {
		return result, err
	}
	result = Result{SchemaVersion: 1, Kind: "offline_bundle_comparison", Left: side(left, l), Right: side(right, r), SameSpec: left.Spec == right.Spec && left.History.SpecHash == right.History.SpecHash}
	result.Configuration = []Property{
		property("schema_version", left.Spec.SchemaVersion, right.Spec.SchemaVersion), property("samples_per_phase", left.Spec.Samples, right.Spec.Samples), property("latency_ms", left.Spec.LatencyMS, right.Spec.LatencyMS), property("request_timeout_ms", left.Spec.RequestTimeoutMS, right.Spec.RequestTimeoutMS), property("spec_hash", left.History.SpecHash, right.History.SpecHash),
	}
	lf, rf := facts(l, left.Report), facts(r, right.Report)
	result.Provenance = make([]Property, 0, len(provenanceFields))
	for _, field := range provenanceFields {
		result.Provenance = append(result.Provenance, property(field, lf[field], rf[field]))
	}
	result.Assessment = assess(result.Left, result.Right, l, r, result.SameSpec, result.Provenance)
	lp, rp := []experiment.Phase{l.Baseline, l.Fault, l.Recovery}, []experiment.Phase{r.Baseline, r.Fault, r.Recovery}
	result.Phases = make([]Phase, 0, 3)
	for i, name := range []string{"baseline", "fault", "recovery"} {
		phase := Phase{Name: name, Left: measurement(lp[i], left.Spec.Samples), Right: measurement(rp[i], right.Spec.Samples)}
		if result.Assessment.DescriptiveDeltas {
			a, b := lp[i].Summary, rp[i].Summary
			delta := experiment.CompareSummaries(a, b)
			phase.Changes = &Changes{change(delta.MeanDeltaMS, a.MeanMS, "ms"), change(delta.P95DeltaMS, a.P95MS, "ms"), change(delta.FailureRateDelta, a.FailureRate, "percentage_points")}
		}
		result.Phases = append(result.Phases, phase)
	}
	result.Notes = []string{"Differences are descriptive; no statistical significance, performance improvement or causal conclusion is established.", "Matching specification hashes and recorded conditions do not establish an identical or controlled execution environment.", "Checksums identify consistent content, not authenticity or independently executed experiments.", "Summary durations are end-to-end HTTP milliseconds; summary failure_rate is a fraction. Absolute failure-rate changes use percentage points; relative changes use percent.", "Trace/span references do not establish availability of temporary Jaeger traces; no collector is contacted."}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	return result, nil
}
func side(e bundle.Evidence, r experiment.Report) Side {
	h := sha256.Sum256(e.Report)
	return Side{1, e.History, e.Spec, hex.EncodeToString(h[:]), r.CleanupComplete}
}
func property(field string, left, right any) Property {
	state := "equal"
	if left == nil || right == nil {
		state = "unknown"
	} else if !reflect.DeepEqual(left, right) {
		state = "different"
	}
	return Property{field, state, left, right}
}
func known(value string) any {
	if value == "" {
		return nil
	}
	return value
}
func facts(r experiment.Report, raw json.RawMessage) map[string]any {
	f := map[string]any{"faultline_revision": known(r.Revision), "go_version": known(r.GoVersion), "toxiproxy_reported_version": known(r.ToxiproxyVersion)}
	if r.Environment == nil {
		// Only an explicitly present, non-null legacy flag is a recorded fact.
		var original struct {
			Telemetry map[string]json.RawMessage `json:"telemetry"`
		}
		if json.Unmarshal(raw, &original) == nil {
			if b, ok := original.Telemetry["enabled"]; ok {
				var flag *bool
				if json.Unmarshal(b, &flag) == nil && flag != nil {
					f["tracing_enabled"] = *flag
				}
			}
		}
		return f
	}
	e := r.Environment
	f["environment_schema_version"] = e.SchemaVersion
	f["runner_os"] = e.RunnerOS
	f["runner_arch"] = e.RunnerArch
	f["measurement_methodology"] = e.Methodology
	f["tracing_enabled"] = *e.TracingEnabled
	if e.Launch != nil {
		f["docker_server_version"] = e.Launch.DockerServerVersion
		for _, image := range e.Launch.Images {
			f["images."+image.Component+".image_id"] = known(image.ImageID)
			f["images."+image.Component+".reference"] = known(image.Reference)
		}
	}
	return f
}
func measurement(p experiment.Phase, configured int) Measurement {
	m := Measurement{Availability: "missing", SamplesRecorded: p.Summary.Requests, SamplesConfigured: configured}
	if p.Summary.Requests > 0 {
		s := p.Summary
		m.Summary = &s
		m.Availability = "partial"
		if s.Requests == configured {
			m.Availability = "complete"
		}
	}
	return m
}
func change(delta, reference float64, unit string) Change {
	absolute := delta
	if unit == "percentage_points" {
		absolute *= 100
	}
	if absolute == 0 {
		absolute = 0
	}
	c := Change{Absolute: absolute, Unit: unit, RelativeStatus: "zero_reference"}
	if reference == 0 {
		return c
	}
	relative := (delta / reference) * 100
	if math.IsNaN(relative) || math.IsInf(relative, 0) {
		c.RelativeStatus = "not_representable"
		return c
	}
	if relative == 0 {
		relative = 0
	}
	c.RelativePercent = &relative
	c.RelativeStatus = "available"
	return c
}
func assess(left, right Side, l, r experiment.Report, sameSpec bool, properties []Property) Assessment {
	a := Assessment{Classification: "recorded_conditions_match", DescriptiveDeltas: true, IdentityRelation: "distinct_recorded_identities", Reasons: []Reason{}}
	add := func(code, field, message string) { a.Reasons = append(a.Reasons, Reason{code, field, message}) }
	block := func(code, field, message string) { a.DescriptiveDeltas = false; add(code, field, message) }
	for i, s := range []Side{left, right} {
		label := []string{"left", "right"}[i]
		report := []experiment.Report{l, r}[i]
		if s.History.Status != "complete" {
			block("execution_not_complete", label, "Failed/canceled evidence remains inspectable; cross-run deltas are withheld.")
		}
		if !s.CleanupComplete {
			block("cleanup_unconfirmed", label, "Successful fault cleanup was not recorded.")
		}
		for j, p := range []experiment.Phase{report.Baseline, report.Fault, report.Recovery} {
			if p.Summary.Requests != s.Spec.Samples {
				block("incomplete_phase", label+"."+[]string{"baseline", "fault", "recovery"}[j], "Only the recorded side measurements are shown; missing samples are not zeros.")
			}
		}
	}
	sameHistory := left.History.ID == right.History.ID
	sameEngine := left.History.RunID != "" && left.History.RunID == right.History.RunID
	if sameHistory || sameEngine {
		samePayload := left.ReportSHA256 == right.ReportSHA256 && left.History.SpecHash == right.History.SpecHash
		sameMetadata := left.History.ID == right.History.ID && left.History.RunID == right.History.RunID && left.History.Status == right.History.Status && left.History.CreatedAt.Equal(right.History.CreatedAt) && left.History.FinishedAt.Equal(right.History.FinishedAt)
		if !samePayload || (sameHistory && !sameMetadata) {
			a.IdentityRelation = "conflicting_recorded_identity"
			block("identity_conflict", "identity", "A reused identifier has different recorded content or history metadata.")
		} else {
			a.IdentityRelation = "same_recorded_run"
			add("same_recorded_run", "identity", "These inputs describe the same recorded run, not independent replication.")
		}
	}
	if !sameSpec {
		add("configuration_differs", "configuration", "Configuration differs; deltas cannot isolate an environmental or failure effect.")
	}
	required := append([]string(nil), requiredConditions...)
	for _, p := range properties {
		if p.Field == "tracing_enabled" && p.State == "equal" && p.Left == true {
			required = append(required, "images.jaeger.image_id", "images.jaeger.reference")
		}
	}
	allConditions := sameSpec
	for _, p := range properties {
		if p.Field == "measurement_methodology" && p.State == "different" {
			block("methodology_differs", p.Field, "Different known measurement methodologies cannot be numerically combined.")
		}
		needed := false
		for _, name := range required {
			if name == p.Field {
				needed = true
				break
			}
		}
		if p.State == "different" || (needed && p.State == "unknown") {
			allConditions = false
			code, message := "condition_differs", "Recorded conditions differ; descriptive arithmetic does not isolate their effects."
			if p.State == "unknown" {
				code = "condition_unknown"
				message = "A required condition was not recorded on one or both sides; absence is not equality."
			}
			add(code, p.Field, message)
		}
		if p.Field == "faultline_revision" {
			for _, v := range []any{p.Left, p.Right} {
				if value, ok := v.(string); ok && !cleanRevision.MatchString(value) {
					allConditions = false
					add("revision_not_pinned", p.Field, "Dirty/development/test labels do not identify a reproducible clean source state.")
					break
				}
			}
		}
	}
	if !a.DescriptiveDeltas {
		a.Classification = "not_comparable"
	} else if !allConditions {
		a.Classification = "descriptive_only"
	}
	return a
}
