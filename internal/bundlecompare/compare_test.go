package bundlecompare

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/raimbekalish/faultline/internal/bundle"
	"github.com/raimbekalish/faultline/internal/experiment"
	"github.com/raimbekalish/faultline/internal/provenance"
	"github.com/raimbekalish/faultline/internal/spec"
	"github.com/raimbekalish/faultline/internal/telemetry"
)

func evidence(id int, duration float64, failure bool, environment bool) bundle.Evidence {
	sp := spec.FromConfig(experiment.Config{Samples: 1, LatencyMS: 80, RequestTimeoutMS: 1000})
	at := time.Date(2026, 10, 25, 1, 0, 0, 0, time.UTC)
	phase := func(status int) experiment.Phase {
		p := experiment.Phase{Samples: []experiment.Sample{{DurationMS: duration, Status: status}}}
		p.Summary = experiment.Summarize(p.Samples)
		return p
	}
	r := experiment.Report{SchemaVersion: 1, RunID: fmt.Sprintf("faultline-%032x", id), Status: "complete", StartedAt: at, FinishedAt: at.Add(time.Second), Config: sp.Config(), Target: experiment.Target, GoVersion: "go1.26.8", Revision: "abcdef0", ToxiproxyVersion: "2.12.0", CleanupComplete: true, Baseline: phase(200), Fault: phase(200), Recovery: phase(200)}
	if failure {
		r.Fault = phase(503)
	}
	r.Comparison = experiment.CompareSummaries(r.Baseline.Summary, r.Fault.Summary)
	hash, _ := sp.Hash()
	if environment {
		off := false
		r.Environment = &provenance.Record{SchemaVersion: 1, SpecHash: hash, Revision: r.Revision, GoVersion: r.GoVersion, RunnerOS: "linux", RunnerArch: "arm64", Methodology: provenance.Methodology, TracingEnabled: &off, Launch: &provenance.Launch{SchemaVersion: 1, DockerServerVersion: "29.8.0", Images: []provenance.Image{{Component: "sample", ImageID: "sha256:" + strings.Repeat("a", 64)}, {Component: "toxiproxy", ImageID: "sha256:" + strings.Repeat("b", 64), Reference: "ghcr.io/shopify/toxiproxy:2.12.0@sha256:" + strings.Repeat("c", 64)}}}}
	}
	b, _ := json.Marshal(r)
	return bundle.Evidence{History: bundle.Metadata{ID: fmt.Sprintf("history-%032x", id), RunID: r.RunID, SpecHash: hash, CreatedAt: at, FinishedAt: at.Add(2 * time.Second), Status: r.Status}, Spec: sp, Report: b}
}
func report(e bundle.Evidence) experiment.Report {
	var r experiment.Report
	_ = json.Unmarshal(e.Report, &r)
	return r
}
func update(e bundle.Evidence, r experiment.Report) bundle.Evidence {
	e.Report, _ = json.Marshal(r)
	e.History.Status = r.Status
	e.History.RunID = r.RunID
	return e
}
func field(t *testing.T, result Result, name string) Property {
	t.Helper()
	for _, p := range result.Provenance {
		if p.Field == name {
			return p
		}
	}
	t.Fatal("missing field", name)
	return Property{}
}
func TestKnownPairDirectionUnitsAndDeterminism(t *testing.T) {
	l, r := evidence(1, 1, false, true), evidence(2, 5, true, true)
	a, err := Compare(context.Background(), l, r)
	if err != nil {
		t.Fatal(err)
	}
	if a.Assessment.Classification != "recorded_conditions_match" || !a.Assessment.DescriptiveDeltas || a.Assessment.ControlledExperiment || a.Assessment.SignificanceAssessed || !a.SameSpec {
		t.Fatal(a.Assessment)
	}
	d := a.Phases[1].Changes
	if d.Mean.Absolute != 4 || d.Mean.Unit != "ms" || d.Mean.RelativePercent == nil || *d.Mean.RelativePercent != 400 || d.P95.Absolute != 4 || d.FailureRate.Absolute != 100 || d.FailureRate.Unit != "percentage_points" || d.FailureRate.RelativePercent != nil || d.FailureRate.RelativeStatus != "zero_reference" {
		t.Fatalf("wrong arithmetic: %+v", d)
	}
	if field(t, a, "images.sample.reference").State != "unknown" || field(t, a, "images.jaeger.image_id").State != "unknown" {
		t.Fatal("absent optional fields became equal")
	}
	reverse, err := Compare(context.Background(), r, l)
	if err != nil {
		t.Fatal(err)
	}
	rd := reverse.Phases[1].Changes
	if rd.Mean.Absolute != -4 || *rd.Mean.RelativePercent != -80 || rd.FailureRate.Absolute != -100 || *rd.FailureRate.RelativePercent != -100 {
		t.Fatal(rd)
	}
	b, err := Compare(context.Background(), l, r)
	if err != nil {
		t.Fatal(err)
	}
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	if !bytes.Equal(ab, bb) {
		t.Fatal("nondeterministic")
	}
	identical, err := Compare(context.Background(), l, l)
	if err != nil || identical.Assessment.IdentityRelation != "same_recorded_run" || identical.Phases[0].Changes.Mean.Absolute != 0 {
		t.Fatal(identical.Assessment, err)
	}
}
func TestConfigurationAndRecordedProvenanceDifferences(t *testing.T) {
	for _, name := range []string{"latency", "timeout", "samples", "revision", "go", "os", "arch", "docker", "image", "reference", "tracing", "dirty", "development", "no_launch"} {
		t.Run(name, func(t *testing.T) {
			l, r := evidence(1, 2, false, true), evidence(2, 3, false, true)
			rr := report(r)
			switch name {
			case "latency":
				r.Spec.LatencyMS = 400
			case "timeout":
				r.Spec.RequestTimeoutMS = 250
			case "samples":
				r.Spec.Samples = 2
				for _, p := range []*experiment.Phase{&rr.Baseline, &rr.Fault, &rr.Recovery} {
					p.Samples = append(p.Samples, p.Samples[0])
					p.Summary = experiment.Summarize(p.Samples)
				}
			case "revision":
				rr.Revision = "abcdef1"
				rr.Environment.Revision = rr.Revision
			case "dirty":
				rr.Revision = "abcdef0-dirty"
				rr.Environment.Revision = rr.Revision
			case "development":
				rr.Revision = "development"
				rr.Environment.Revision = rr.Revision
			case "go":
				rr.GoVersion = "go1.26.7"
				rr.Environment.GoVersion = rr.GoVersion
			case "os":
				rr.Environment.RunnerOS = "darwin"
			case "arch":
				rr.Environment.RunnerArch = "amd64"
			case "docker":
				rr.Environment.Launch.DockerServerVersion = "29.7.0"
			case "image":
				rr.Environment.Launch.Images[0].ImageID = "sha256:" + strings.Repeat("d", 64)
			case "reference":
				rr.Environment.Launch.Images[1].Reference = "ghcr.io/shopify/toxiproxy:2.12.1@sha256:" + strings.Repeat("c", 64)
			case "tracing":
				on := true
				rr.Environment.TracingEnabled = &on
				rr.Telemetry = &telemetry.Stats{Enabled: true}
			case "no_launch":
				rr.Environment.Launch = nil
			}
			rr.Config = r.Spec.Config()
			hash, _ := r.Spec.Hash()
			r.History.SpecHash = hash
			rr.Environment.SpecHash = hash
			r = update(r, rr)
			got, err := Compare(context.Background(), l, r)
			if err != nil {
				t.Fatal(err)
			}
			if got.Assessment.Classification != "descriptive_only" || !got.Assessment.DescriptiveDeltas {
				t.Fatal(got.Assessment)
			}
			if name == "latency" || name == "timeout" || name == "samples" {
				if got.SameSpec {
					t.Fatal("differing config became equal")
				}
			}
		})
	}
}
func TestLegacyMissingFactsNeverEqualOrCurrentMachine(t *testing.T) {
	l, r := evidence(1, 2, false, false), evidence(2, 3, false, false)
	got, err := Compare(context.Background(), l, r)
	if err != nil {
		t.Fatal(err)
	}
	if got.Assessment.Classification != "descriptive_only" {
		t.Fatal(got.Assessment)
	}
	for _, name := range []string{"runner_os", "runner_arch", "measurement_methodology", "tracing_enabled", "docker_server_version", "images.sample.image_id"} {
		p := field(t, got, name)
		if p.State != "unknown" || p.Left != nil || p.Right != nil {
			t.Fatal(p)
		}
	}
	if p := field(t, got, "faultline_revision"); p.State != "equal" || p.Left != "abcdef0" {
		t.Fatal("discarded recorded legacy value", p)
	}
	rr := report(r)
	rr.Telemetry = &telemetry.Stats{Enabled: false}
	r = update(r, rr)
	got, err = Compare(context.Background(), l, r)
	if err != nil {
		t.Fatal(err)
	}
	if p := field(t, got, "tracing_enabled"); p.State != "unknown" || p.Left != nil || p.Right != false {
		t.Fatal(p)
	}
	r.Report = bytes.Replace(r.Report, []byte(`"enabled":false`), []byte(`"enabled":null`), 1)
	got, err = Compare(context.Background(), l, r)
	if err != nil {
		t.Fatal(err)
	}
	if p := field(t, got, "tracing_enabled"); p.Right != nil {
		t.Fatal("null became false", p)
	}
}
func TestFailedCanceledMissingAndPartialHaveNoDeltas(t *testing.T) {
	for _, status := range []string{"failed", "canceled"} {
		for _, stage := range []string{"full", "partial", "missing"} {
			t.Run(status+"/"+stage, func(t *testing.T) {
				l, r := evidence(1, 2, false, true), evidence(2, 3, false, true)
				rr := report(r)
				rr.Status = status
				rr.Error = "interrupted"
				rr.CleanupComplete = status != "failed"
				if stage != "full" {
					rr.Recovery = experiment.Phase{}
				}
				if stage == "partial" {
					r.Spec.Samples = 2
					rr.Config = r.Spec.Config()
					hash, _ := r.Spec.Hash()
					r.History.SpecHash = hash
					rr.Environment.SpecHash = hash
					rr.Baseline.Samples = append(rr.Baseline.Samples, rr.Baseline.Samples[0])
					rr.Baseline.Summary = experiment.Summarize(rr.Baseline.Samples)
				}
				r = update(r, rr)
				got, err := Compare(context.Background(), l, r)
				if err != nil {
					t.Fatal(err)
				}
				if got.Assessment.Classification != "not_comparable" || got.Assessment.DescriptiveDeltas {
					t.Fatal(got.Assessment)
				}
				for _, p := range got.Phases {
					if p.Changes != nil {
						t.Fatal("fabricated full-run delta")
					}
				}
				if stage != "full" && (got.Phases[2].Right.Availability != "missing" || got.Phases[2].Right.Summary != nil) {
					t.Fatal(got.Phases[2])
				}
				if stage == "partial" && got.Phases[1].Right.Availability != "partial" {
					t.Fatal(got.Phases[0])
				}
			})
		}
	}
}
func TestIdentityConflictsAndMethodologyRule(t *testing.T) {
	l, r := evidence(1, 2, false, true), evidence(1, 3, false, true)
	got, err := Compare(context.Background(), l, r)
	if err != nil {
		t.Fatal(err)
	}
	if got.Assessment.IdentityRelation != "conflicting_recorded_identity" || got.Assessment.DescriptiveDeltas {
		t.Fatal(got.Assessment)
	}
	r = evidence(2, 3, false, true)
	rr := report(r)
	rr.Environment.Methodology = "different-v2"
	r = update(r, rr)
	if _, err = Compare(context.Background(), l, r); err == nil {
		t.Fatal("unsupported methodology accepted")
	}
	// Explicit rule for future supported differing methods; currently validation
	// rejects every methodology except v1 before this rule can be reached by CLI.
	a := assess(side(l, report(l)), side(r, rr), report(l), rr, true, []Property{property("measurement_methodology", provenance.Methodology, "different-v2")})
	if a.DescriptiveDeltas || a.Classification != "not_comparable" {
		t.Fatal(a)
	}
}
func TestZeroOverflowAndCancellationBounds(t *testing.T) {
	for _, values := range [][2]float64{{0, 0}, {0, 1}, {1, 0}, {math.SmallestNonzeroFloat64, math.MaxFloat64}} {
		got, err := Compare(context.Background(), evidence(1, values[0], false, true), evidence(2, values[1], false, true))
		if err != nil {
			t.Fatal(err)
		}
		d := got.Phases[0].Changes.Mean
		if values[0] == 0 {
			if d.RelativePercent != nil || d.RelativeStatus != "zero_reference" {
				t.Fatal(d)
			}
		} else if values[1] == 0 {
			if *d.RelativePercent != -100 {
				t.Fatal(d)
			}
		} else if d.RelativePercent != nil || d.RelativeStatus != "not_representable" {
			t.Fatal(d)
		}
		if _, err = json.Marshal(got); err != nil {
			t.Fatal("nonfinite JSON", err)
		}
		if got.Phases[0].Left.Availability != "complete" || got.Phases[0].Left.Summary == nil {
			t.Fatal("measured zero became missing")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Compare(ctx, evidence(1, 1, false, true), evidence(2, 2, false, true)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	l, r := evidence(1, 1, false, true), evidence(2, 2, false, true)
	r.Report = bytes.Replace(r.Report, []byte(`"mean_ms":2`), []byte(`"mean_ms":1`), 1)
	if _, err := Compare(context.Background(), l, r); err == nil {
		t.Fatal("invalid logical evidence accepted")
	}
}

func TestTracingEnabledRequiresRecordedJaegerConditions(t *testing.T) {
	l, r := evidence(1, 2, false, true), evidence(2, 3, false, true)
	enable := func(e bundle.Evidence, withImage bool) bundle.Evidence {
		rr := report(e)
		on := true
		rr.Environment.TracingEnabled = &on
		rr.Telemetry = &telemetry.Stats{Enabled: true}
		if withImage {
			rr.Environment.Launch.Images = append(rr.Environment.Launch.Images, provenance.Image{Component: "jaeger", ImageID: "sha256:" + strings.Repeat("d", 64), Reference: "cr.jaegertracing.io/jaegertracing/jaeger:2.22.0@sha256:" + strings.Repeat("e", 64)})
		}
		return update(e, rr)
	}
	got, err := Compare(context.Background(), enable(l, false), enable(r, false))
	if err != nil || got.Assessment.Classification != "descriptive_only" || field(t, got, "images.jaeger.image_id").State != "unknown" {
		t.Fatal(got.Assessment, err)
	}
	got, err = Compare(context.Background(), enable(l, true), enable(r, true))
	if err != nil || got.Assessment.Classification != "recorded_conditions_match" {
		t.Fatal(got.Assessment, err)
	}
}
func TestMaximumReportPayloadDoesNotExpandComparisonOutput(t *testing.T) {
	l, r := evidence(1, 2, false, false), evidence(2, 3, false, false)
	large := func(e bundle.Evidence) bundle.Evidence {
		rr := report(e)
		rr.Status = "failed"
		rr.CleanupComplete = false
		rr.Error = strings.Repeat("x", bundle.MaxReport-8192)
		return update(e, rr)
	}
	l, r = large(l), large(r)
	if len(l.Report) > bundle.MaxReport || len(r.Report) > bundle.MaxReport {
		t.Fatal("invalid resource fixture")
	}
	got, err := Compare(context.Background(), l, r)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(got)
	if err != nil || len(b) > 32768 || bytes.Contains(b, []byte(strings.Repeat("x", 100))) {
		t.Fatal("comparison copied raw report/error payload", len(b), err)
	}
}
func TestMetadataIdentityConflictAndUnassignedRunIDs(t *testing.T) {
	l := evidence(1, 2, false, true)
	r := l
	r.History.FinishedAt = r.History.FinishedAt.Add(time.Second)
	got, err := Compare(context.Background(), l, r)
	if err != nil || got.Assessment.DescriptiveDeltas || got.Assessment.IdentityRelation != "conflicting_recorded_identity" {
		t.Fatal(got.Assessment, err)
	}
	empty := func(e bundle.Evidence) bundle.Evidence {
		rr := report(e)
		rr.Status = "failed"
		rr.Error = "not started"
		rr.RunID = ""
		rr.CleanupComplete = false
		rr.Baseline = experiment.Phase{}
		rr.Fault = experiment.Phase{}
		rr.Recovery = experiment.Phase{}
		rr.Comparison = experiment.Comparison{}
		return update(e, rr)
	}
	got, err = Compare(context.Background(), empty(evidence(1, 2, false, false)), empty(evidence(2, 2, false, false)))
	if err != nil || got.Assessment.IdentityRelation != "distinct_recorded_identities" {
		t.Fatal("empty IDs became equal", got.Assessment, err)
	}
}
