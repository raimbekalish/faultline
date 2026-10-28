// Package labprotocol defines the preflight-only trusted worker boundary. It
// never imports the sample engine, Docker, history or fault transport.
package labprotocol

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/raimbekalish/faultline/internal/jsonstrict"
	"github.com/raimbekalish/faultline/internal/spec"
	"io"
	"regexp"
)

const MaxInput = 8192

var ErrProtocol = errors.New("invalid worker preflight protocol")
var launch = regexp.MustCompile(`^faultline-lab-[a-f0-9]{32}$`)
var run = regexp.MustCompile(`^faultline-[a-f0-9]{32}$`)

type Request struct {
	SchemaVersion       int       `json:"schema_version"`
	Kind                string    `json:"kind"`
	LaunchID            string    `json:"launch_id"`
	RunID               string    `json:"run_id"`
	SpecificationSHA256 string    `json:"specification_sha256"`
	Experiment          spec.Spec `json:"experiment"`
	Tracing             string    `json:"tracing"`
}
type Response struct {
	SchemaVersion       int      `json:"schema_version"`
	Kind                string   `json:"kind"`
	LaunchID            string   `json:"launch_id"`
	RunID               string   `json:"run_id"`
	SpecificationSHA256 string   `json:"specification_sha256"`
	SourceRevision      string   `json:"source_revision"`
	Status              string   `json:"status"`
	ExecutionAvailable  bool     `json:"execution_available"`
	Capabilities        []string `json:"capabilities"`
	Measurements        any      `json:"measurements"`
}

func Decode(ctx context.Context, r io.Reader) (Request, error) {
	if ctx.Err() != nil {
		return Request{}, ctx.Err()
	}
	b, e := io.ReadAll(io.LimitReader(cancellableReader{ctx, r}, MaxInput+1))
	if ctx.Err() != nil {
		return Request{}, ctx.Err()
	}
	if e != nil || len(b) > MaxInput || jsonstrict.Validate(b) != nil {
		return Request{}, ErrProtocol
	}
	var raw map[string]json.RawMessage
	if json.Unmarshal(b, &raw) != nil || len(raw) != 7 {
		return Request{}, ErrProtocol
	}
	for _, k := range []string{"schema_version", "kind", "launch_id", "run_id", "specification_sha256", "experiment", "tracing"} {
		v, ok := raw[k]
		if !ok || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return Request{}, ErrProtocol
		}
	}
	var req Request
	if json.Unmarshal(b, &req) != nil {
		return Request{}, ErrProtocol
	}
	measurement, e := spec.Decode(bytes.NewReader(raw["experiment"]))
	if e != nil {
		return Request{}, ErrProtocol
	}
	req.Experiment = measurement
	digest, _ := measurement.Hash()
	// This is the measurement specification hash, explicitly distinct from the
	// outer application's plan/configuration hash. No ambient capture occurs.
	if req.SchemaVersion != 1 || req.Kind != "faultline-worker-preflight" || !launch.MatchString(req.LaunchID) || !run.MatchString(req.RunID) || req.SpecificationSHA256 != digest || req.Tracing != "off" {
		return Request{}, ErrProtocol
	}
	return req, ctx.Err()
}
func Preflight(ctx context.Context, r io.Reader, w io.Writer, revision string) error {
	req, e := Decode(ctx, r)
	if e != nil {
		return e
	}
	if revision != "development" && !regexp.MustCompile(`^[a-f0-9]{40}$`).MatchString(revision) {
		return ErrProtocol
	}
	response := Response{1, "faultline-worker-preflight-result", req.LaunchID, req.RunID, req.SpecificationSHA256, revision, "validated_preflight_only", false, []string{"validate_configuration"}, nil}
	b, e := json.Marshal(response)
	if e != nil || len(b) > 4096 {
		return ErrProtocol
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	b = append(b, '\n')
	n, e := w.Write(b)
	if e != nil || n != len(b) {
		return io.ErrShortWrite
	}
	return nil
}

type cancellableReader struct {
	ctx context.Context
	r   io.Reader
}

func (r cancellableReader) Read(b []byte) (int, error) {
	if r.ctx.Err() != nil {
		return 0, r.ctx.Err()
	}
	n, e := r.r.Read(b)
	if r.ctx.Err() != nil {
		return n, r.ctx.Err()
	}
	return n, e
}
