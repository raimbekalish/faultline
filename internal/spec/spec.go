// Package spec defines the versioned, fixed-topology experiment configuration.
package spec

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"

	"github.com/raimbekalish/faultline/internal/experiment"
)

const MaxBytes = 64 << 10

type Spec struct {
	SchemaVersion    int `json:"schema_version"`
	Samples          int `json:"samples_per_phase"`
	LatencyMS        int `json:"latency_ms"`
	RequestTimeoutMS int `json:"request_timeout_ms"`
}

func FromConfig(c experiment.Config) Spec { return Spec{1, c.Samples, c.LatencyMS, c.RequestTimeoutMS} }
func (s Spec) Config() experiment.Config {
	return experiment.Config{Samples: s.Samples, LatencyMS: s.LatencyMS, RequestTimeoutMS: s.RequestTimeoutMS}
}
func (s Spec) Validate() error {
	if s.SchemaVersion != 1 {
		return fmt.Errorf("unsupported experiment schema_version %d", s.SchemaVersion)
	}
	return s.Config().Validate()
}
func (s Spec) Canonical() ([]byte, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(s)
}
func (s Spec) Hash() (string, error) {
	b, err := s.Canonical()
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(b)
	return hex.EncodeToString(hash[:]), nil
}

// Decode rejects duplicate, unknown and differently cased fields, omitted values,
// non-integer numbers, unsupported versions and trailing data. No targets exist.
func Decode(r io.Reader) (Spec, error) {
	var s Spec
	b, err := io.ReadAll(io.LimitReader(r, MaxBytes+1))
	if err != nil {
		return s, err
	}
	if len(b) > MaxBytes {
		return s, fmt.Errorf("specification exceeds %d bytes", MaxBytes)
	}
	d := json.NewDecoder(bytes.NewReader(b))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return s, fmt.Errorf("specification must be a JSON object")
	}
	seen := map[string]bool{}
	for d.More() {
		token, err := d.Token()
		if err != nil {
			return s, err
		}
		key, ok := token.(string)
		if !ok || seen[key] {
			return s, fmt.Errorf("duplicate or invalid specification field %q", key)
		}
		seen[key] = true
		var dst *int
		switch key {
		case "schema_version":
			dst = &s.SchemaVersion
		case "samples_per_phase":
			dst = &s.Samples
		case "latency_ms":
			dst = &s.LatencyMS
		case "request_timeout_ms":
			dst = &s.RequestTimeoutMS
		default:
			return s, fmt.Errorf("unsupported specification field %q", key)
		}
		var raw json.RawMessage
		if err := d.Decode(&raw); err != nil {
			return s, err
		}
		if bytes.Equal(raw, []byte("null")) {
			return s, fmt.Errorf("%s must be an integer", key)
		}
		if err := json.Unmarshal(raw, dst); err != nil {
			return s, fmt.Errorf("%s must be an integer: %w", key, err)
		}
	}
	if _, err := d.Token(); err != nil {
		return s, err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return s, fmt.Errorf("trailing specification data")
	}
	if len(seen) != 4 {
		return s, fmt.Errorf("all four specification fields are required")
	}
	return s, s.Validate()
}
