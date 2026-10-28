package labworker

import (
	"context"
	"io"
	"net/netip"
	"sync/atomic"
	"time"
)

// harnessBinding is an opaque same-process test capability, NOT durable RUN
// authorization, resource ownership or runtime continuity. It cannot be decoded.
// Only package-local tests construct one; no production CLI imports this engine.
type harnessBinding struct {
	configuration Configuration
	app, control  netip.AddrPort
	consumed      atomic.Bool
}

func newHarnessBinding(c Configuration, app, control netip.AddrPort) (*harnessBinding, error) {
	if c.validate() != nil || !app.IsValid() || !control.IsValid() || !app.Addr().IsLoopback() || !control.Addr().IsLoopback() || app.Port() == 0 || control.Port() == 0 || app == control {
		return nil, ErrProtocol
	}
	return &harnessBinding{configuration: c, app: app, control: control}, nil
}
func (*harnessBinding) MarshalJSON() ([]byte, error) { return nil, ErrProtocol }
func (*harnessBinding) UnmarshalJSON([]byte) error   { return ErrProtocol }

// executeMeasurement is an internal local adapter, not a process/CLI entrypoint.
// Validation and capability consumption precede every HTTP/control side effect.
// Valid incomplete/canceled results are written only AFTER independent cleanup.
func executeMeasurement(ctx context.Context, in io.Reader, out io.Writer, binding *harnessBinding) (MeasurementResponse, error) {
	r, e := DecodeMeasurementRequest(ctx, in)
	if e != nil {
		return MeasurementResponse{}, e
	}
	if binding == nil {
		return MeasurementResponse{}, ErrProtocol
	}
	c, e := r.configuration(binding.configuration.SourceRevision)
	if e != nil || c != binding.configuration || !binding.consumed.CompareAndSwap(false, true) {
		return MeasurementResponse{}, ErrProtocol
	}
	client, e := newHarnessClient(binding.app, time.Duration(c.Specification.RequestTimeoutMS)*time.Millisecond)
	if e != nil {
		return MeasurementResponse{}, e
	}
	controller, e := newScopedController(c, binding.control)
	if e != nil {
		client.close()
		return MeasurementResponse{}, e
	}
	defer controller.close()
	report, runErr := run(ctx, c, client, controller)
	response := MeasurementResponse{1, "faultline-worker-measurement-response", report}
	raw, e := response.Encode()
	if e != nil {
		return response, e
	}
	// Cancellation must not discard a valid partial report after cleanup. A future
	// pipe owner must close a blocked writer; no unbounded goroutine is spawned.
	n, e := out.Write(raw)
	if e != nil || n != len(raw) {
		return response, ErrOutput
	}
	return response, runErr
}
