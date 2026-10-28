// Package lab plans a closed local application lab. It cannot execute a lab.
package lab

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"regexp"

	"github.com/raimbekalish/faultline/internal/jsonstrict"
	"github.com/raimbekalish/faultline/internal/spec"
)

const MaxDescriptorBytes = 8 << 10
const MaxPlanBytes = 64 << 10
const MaxSummaryBytes = 8 << 10

var imageID = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
var digest = regexp.MustCompile(`^[a-f0-9]{64}$`)
var revisionID = regexp.MustCompile(`^[a-f0-9]{40}$`)

var ErrDescriptor = errors.New("invalid http-work-v1 descriptor")

type Descriptor struct {
	SchemaVersion int       `json:"schema_version"`
	Adapter       string    `json:"adapter"`
	ImageID       string    `json:"image_id"`
	Platform      string    `json:"platform"`
	Experiment    spec.Spec `json:"experiment"`
	Tracing       string    `json:"tracing"`
}

func (d Descriptor) Validate() error {
	if d.SchemaVersion != 1 || d.Adapter != "http-work-v1" || !imageID.MatchString(d.ImageID) || d.Platform != "linux/arm64" || d.Tracing != "off" || d.Experiment.Validate() != nil {
		return ErrDescriptor
	}
	return nil
}

func (d Descriptor) Canonical() ([]byte, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(d)
}

func (d Descriptor) Hash() (string, error) {
	b, err := d.Canonical()
	if err != nil {
		return "", err
	}
	return hash(b), nil
}

func hash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// exactObject complements jsonstrict: encoding/json alone accepts case aliases
// and nulls. All fields are required, and errors never echo untrusted values.
func exactObject(b []byte, fields ...string) error {
	if jsonstrict.Validate(b) != nil {
		return ErrDescriptor
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(b, &m) != nil || len(m) != len(fields) {
		return ErrDescriptor
	}
	for _, field := range fields {
		value, ok := m[field]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return ErrDescriptor
		}
	}
	return nil
}

func DecodeDescriptor(ctx context.Context, r io.Reader) (Descriptor, error) {
	var d Descriptor
	b, err := io.ReadAll(io.LimitReader(ctxReader{ctx, r}, MaxDescriptorBytes+1))
	if err != nil {
		return d, err
	}
	if len(b) > MaxDescriptorBytes || exactObject(b, "schema_version", "adapter", "image_id", "platform", "experiment", "tracing") != nil {
		return d, ErrDescriptor
	}
	var raw struct {
		Experiment json.RawMessage `json:"experiment"`
	}
	if json.Unmarshal(b, &d) != nil || json.Unmarshal(b, &raw) != nil {
		return Descriptor{}, ErrDescriptor
	}
	// Preserve the existing exact integer grammar as well as its numeric limits.
	sp, err := spec.Decode(bytes.NewReader(raw.Experiment))
	if err != nil || d.Validate() != nil {
		return Descriptor{}, ErrDescriptor
	}
	d.Experiment = sp
	return d, ctx.Err()
}

type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (r ctxReader) Read(b []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.r.Read(b)
	if canceled := r.ctx.Err(); canceled != nil {
		return n, canceled
	}
	return n, err
}
