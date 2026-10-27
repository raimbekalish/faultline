// Package bundleevaluate evaluates explicit reliability thresholds against
// validated evidence. Evaluation does not access files, services or ambient state.
package bundleevaluate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/raimbekalish/faultline/internal/jsonstrict"
)

const MaxPolicyBytes = 8 << 10

// The grammar permits two lifecycle requirements and at most five checks in each
// of three named phases. No user-controlled expression/condition lists exist.
const MaxConditions = 17

type Policy struct {
	SchemaVersion   int    `json:"schema_version"`
	RequireComplete bool   `json:"require_complete"`
	RequireCleanup  bool   `json:"require_cleanup"`
	Phases          Phases `json:"phases"`
}

type Phases struct {
	Baseline *Thresholds `json:"baseline,omitempty"`
	Fault    *Thresholds `json:"fault,omitempty"`
	Recovery *Thresholds `json:"recovery,omitempty"`
}

type Thresholds struct {
	MinSamples     *int     `json:"min_samples,omitempty"`
	MaxFailureRate *float64 `json:"max_failure_rate,omitempty"`
	MinFailureRate *float64 `json:"min_failure_rate,omitempty"`
	MaxMeanMS      *float64 `json:"max_mean_ms,omitempty"`
	MaxP95MS       *float64 `json:"max_p95_ms,omitempty"`
}

func (p Policy) Validate() error {
	if p.SchemaVersion != 1 {
		return fmt.Errorf("unsupported policy schema_version %d", p.SchemaVersion)
	}
	if !p.RequireComplete || !p.RequireCleanup {
		return fmt.Errorf("require_complete and require_cleanup must both be explicitly true")
	}
	count := 0
	for _, phase := range []*Thresholds{p.Phases.Baseline, p.Phases.Fault, p.Phases.Recovery} {
		if phase == nil {
			continue
		}
		count++
		if phase.MinSamples == nil && phase.MaxFailureRate == nil && phase.MinFailureRate == nil && phase.MaxMeanMS == nil && phase.MaxP95MS == nil {
			return fmt.Errorf("a configured phase must contain at least one threshold")
		}
		if phase.MinSamples != nil && (*phase.MinSamples < 1 || *phase.MinSamples > 100) {
			return fmt.Errorf("min_samples must be an integer from 1 to 100")
		}
		for _, v := range []*float64{phase.MaxFailureRate, phase.MinFailureRate, phase.MaxMeanMS, phase.MaxP95MS} {
			if v != nil && (math.IsNaN(*v) || math.IsInf(*v, 0) || *v < 0) {
				return fmt.Errorf("thresholds must be finite and nonnegative")
			}
		}
		for _, v := range []*float64{phase.MaxFailureRate, phase.MinFailureRate} {
			if v != nil && *v > 1 {
				return fmt.Errorf("failure-rate thresholds must be fractions from 0 to 1")
			}
		}
		if phase.MinFailureRate != nil && phase.MaxFailureRate != nil && *phase.MinFailureRate > *phase.MaxFailureRate {
			return fmt.Errorf("min_failure_rate exceeds max_failure_rate")
		}
	}
	if count == 0 {
		return fmt.Errorf("at least one phase with an explicit threshold is required")
	}
	return nil
}

// Decode rejects case aliases, nulls and float underflow as well as ambiguous
// JSON. Shared jsonstrict validation owns UTF-8, surrogate and duplicate checks.
func Decode(ctx context.Context, r io.Reader) (Policy, error) {
	var p Policy
	b, err := io.ReadAll(io.LimitReader(contextReader{ctx, r}, MaxPolicyBytes+1))
	if err != nil {
		return p, err
	}
	if len(b) > MaxPolicyBytes {
		return p, fmt.Errorf("policy exceeds %d bytes", MaxPolicyBytes)
	}
	if err = ctx.Err(); err != nil {
		return p, err
	}
	if err = jsonstrict.Validate(b); err != nil {
		return p, fmt.Errorf("invalid policy JSON: %w", err)
	}
	root, err := object(b, "schema_version", "require_complete", "require_cleanup", "phases")
	if err != nil {
		return p, err
	}
	phases, err := object(root["phases"], "baseline", "fault", "recovery")
	if err != nil {
		return p, err
	}
	for _, name := range []string{"baseline", "fault", "recovery"} {
		raw, ok := phases[name]
		if !ok {
			continue
		}
		fields, err := object(raw, "min_samples", "max_failure_rate", "min_failure_rate", "max_mean_ms", "max_p95_ms")
		if err != nil {
			return p, err
		}
		for _, key := range []string{"max_failure_rate", "min_failure_rate", "max_mean_ms", "max_p95_ms"} {
			if raw, ok := fields[key]; ok {
				literal := string(raw)
				v, err := strconv.ParseFloat(literal, 64)
				if err != nil || math.IsInf(v, 0) {
					return p, fmt.Errorf("%s must be a representable finite number", key)
				}
				// ParseFloat may round a nonzero decimal to zero without error.
				// That would silently weaken a minimum threshold.
				if v == 0 && strings.ContainsAny(strings.SplitN(strings.ToLower(literal), "e", 2)[0], "123456789") {
					return p, fmt.Errorf("%s underflows float64", key)
				}
			}
		}
	}
	if err = json.Unmarshal(b, &p); err != nil {
		return Policy{}, fmt.Errorf("invalid policy field type: %w", err)
	}
	if err = p.Validate(); err != nil {
		return Policy{}, err
	}
	if err = ctx.Err(); err != nil {
		return Policy{}, err
	}
	return p, nil
}

func object(raw []byte, allowed ...string) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return nil, fmt.Errorf("policy objects must be non-null JSON objects")
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := fields[key]
		known := false
		for _, name := range allowed {
			known = known || key == name
		}
		if !known {
			return nil, fmt.Errorf("unknown or incorrectly cased policy field %q", key)
		}
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, fmt.Errorf("policy field %q cannot be null", key)
		}
	}
	return fields, nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(b []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.r.Read(b)
	if canceled := r.ctx.Err(); canceled != nil {
		return n, canceled
	}
	return n, err
}
