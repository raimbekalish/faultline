package labworker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"time"

	"github.com/raimbekalish/faultline/internal/jsonstrict"
	"github.com/raimbekalish/faultline/internal/spec"
)

const MaxMeasurementInput = 8192
const MaxMeasurementOutput = MaxResultBytes + 1024
const protocolReadTimeout = 10 * time.Second

var ErrProtocol = errors.New("invalid measurement protocol or binding")
var ErrOutput = errors.New("measurement output unconfirmed after cleanup")

type MeasurementRequest struct {
	SchemaVersion       int       `json:"schema_version"`
	Kind                string    `json:"kind"`
	LaunchID            string    `json:"launch_id"`
	RunID               string    `json:"run_id"`
	Specification       spec.Spec `json:"specification"`
	SpecificationSHA256 string    `json:"specification_sha256"`
	Tracing             string    `json:"tracing"`
}

func (r MeasurementRequest) configuration(rev string) (Configuration, error) {
	c := Configuration{r.LaunchID, r.RunID, r.Specification, r.SpecificationSHA256, rev}
	if r.SchemaVersion != 1 || r.Kind != "faultline-worker-measurement-request" || r.Tracing != "off" || c.validate() != nil {
		return Configuration{}, ErrProtocol
	}
	return c, nil
}
func (r MeasurementRequest) Encode() ([]byte, error) {
	// The request carries no worker-revision claim; its syntax can be validated
	// against a fixed valid placeholder without ambient environment capture.
	if _, e := r.configuration("0000000000000000000000000000000000000000"); e != nil {
		return nil, e
	}
	b, e := json.Marshal(r)
	if e != nil || len(b) > MaxMeasurementInput {
		return nil, ErrProtocol
	}
	return append(b, '\n'), nil
}
func DecodeMeasurementRequest(ctx context.Context, in io.Reader) (MeasurementRequest, error) {
	b, e := readBounded(ctx, in, MaxMeasurementInput)
	if e != nil {
		return MeasurementRequest{}, e
	}
	var r MeasurementRequest
	if decodeExact(b, &r) != nil {
		return r, ErrProtocol
	}
	if _, e = r.Encode(); e != nil {
		return r, ErrProtocol
	}
	return r, nil
}

type MeasurementResponse struct {
	SchemaVersion int    `json:"schema_version"`
	Kind          string `json:"kind"`
	Result        Report `json:"result"`
}

func (r MeasurementResponse) Encode() ([]byte, error) {
	if r.SchemaVersion != 1 || r.Kind != "faultline-worker-measurement-response" {
		return nil, ErrProtocol
	}
	if _, e := r.Result.Encode(); e != nil {
		return nil, ErrProtocol
	}
	b, e := json.Marshal(r)
	if e != nil || len(b)+1 > MaxMeasurementOutput {
		return nil, ErrProtocol
	}
	return append(b, '\n'), nil
}
func DecodeMeasurementResponse(ctx context.Context, in io.Reader, request MeasurementRequest, rev string) (MeasurementResponse, error) {
	expected, e := request.configuration(rev)
	if e != nil {
		return MeasurementResponse{}, e
	}
	b, e := readBounded(ctx, in, MaxMeasurementOutput)
	if e != nil {
		return MeasurementResponse{}, e
	}
	var r MeasurementResponse
	if decodeExact(b, &r) != nil || r.Result.Configuration != expected {
		return r, ErrProtocol
	}
	if _, e = r.Encode(); e != nil {
		return r, ErrProtocol
	}
	return r, nil
}
func readBounded(parent context.Context, in io.Reader, limit int) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parent, protocolReadTimeout)
	defer cancel()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	b, e := io.ReadAll(io.LimitReader(contextReader{ctx, in}, int64(limit+1)))
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if e != nil || len(b) > limit {
		return nil, ErrProtocol
	}
	return b, nil
}
func decodeExact(b []byte, out any) error {
	if jsonstrict.Validate(b) != nil {
		return ErrProtocol
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil {
		return ErrProtocol
	}
	canonical, e := json.Marshal(out)
	if e != nil {
		return ErrProtocol
	}
	var actual, want any
	if json.Unmarshal(b, &actual) != nil || json.Unmarshal(canonical, &want) != nil || !reflect.DeepEqual(actual, want) {
		return ErrProtocol
	}
	return nil
}
