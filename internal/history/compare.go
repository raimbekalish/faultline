package history

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/raimbekalish/faultline/internal/experiment"
	"github.com/raimbekalish/faultline/internal/spec"
)

var ErrNotComparable = errors.New("comparison requires two completed runs with successful cleanup")

type PhaseComparison struct {
	Phase string                `json:"phase"`
	Left  experiment.Summary    `json:"left"`
	Right experiment.Summary    `json:"right"`
	Delta experiment.Comparison `json:"right_minus_left"`
}
type Comparison struct {
	SchemaVersion int               `json:"schema_version"`
	LeftID        string            `json:"left_id"`
	RightID       string            `json:"right_id"`
	LeftSpec      spec.Spec         `json:"left_spec"`
	RightSpec     spec.Spec         `json:"right_spec"`
	SameSpec      bool              `json:"same_spec"`
	Phases        []PhaseComparison `json:"phases"`
}

func (s *Store) Compare(ctx context.Context, left, right string) (Comparison, error) {
	a, err := s.Get(ctx, left)
	if err != nil {
		return Comparison{}, err
	}
	b, err := s.Get(ctx, right)
	if err != nil {
		return Comparison{}, err
	}
	if a.Status != "complete" || b.Status != "complete" {
		return Comparison{}, ErrNotComparable
	}
	var ar, br experiment.Report
	if err := json.Unmarshal(a.Report, &ar); err != nil {
		return Comparison{}, err
	}
	if err := json.Unmarshal(b.Report, &br); err != nil {
		return Comparison{}, err
	}
	c := Comparison{SchemaVersion: 1, LeftID: a.ID, RightID: b.ID, LeftSpec: a.Spec, RightSpec: b.Spec, SameSpec: a.SpecHash == b.SpecHash}
	for i, p := range []struct {
		name  string
		phase experiment.Phase
	}{{"baseline", ar.Baseline}, {"fault", ar.Fault}, {"recovery", ar.Recovery}} {
		r := []experiment.Phase{br.Baseline, br.Fault, br.Recovery}[i].Summary
		l := p.phase.Summary
		c.Phases = append(c.Phases, PhaseComparison{Phase: p.name, Left: l, Right: r, Delta: experiment.Comparison{MeanDeltaMS: r.MeanMS - l.MeanMS, P95DeltaMS: r.P95MS - l.P95MS, FailureRateDelta: r.FailureRate - l.FailureRate}})
	}
	return c, nil
}
