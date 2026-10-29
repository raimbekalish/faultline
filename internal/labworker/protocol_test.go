package labworker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func measurementRequest(c Configuration) MeasurementRequest {
	return MeasurementRequest{1, "faultline-worker-measurement-request", c.LaunchID, c.RunID, c.Specification, c.SpecificationSHA256, "off"}
}
func bindingFor(t *testing.T, c Configuration, l *localLab, s *controlLab) *harnessBinding {
	t.Helper()
	b, e := newHarnessBinding(c, serverAddress(t, l.frontend), serverAddress(t, s.server))
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func callMeasurement(t *testing.T, ctx context.Context, c Configuration, l *localLab, s *controlLab) (MeasurementResponse, error, []byte) {
	t.Helper()
	req := measurementRequest(c)
	raw, e := req.Encode()
	if e != nil {
		t.Fatal(e)
	}
	var out bytes.Buffer
	response, e := executeMeasurement(ctx, bytes.NewReader(raw), &out, bindingFor(t, c, l, s))
	decoded, decodeErr := DecodeMeasurementResponse(context.Background(), bytes.NewReader(out.Bytes()), req, c.SourceRevision)
	if decodeErr != nil {
		t.Fatalf("invalid protocol response: %v\n%s", decodeErr, out.Bytes())
	}
	again, encodeErr := decoded.Encode()
	if encodeErr != nil || !bytes.Equal(out.Bytes(), again) {
		t.Fatal("unstable response")
	}
	return response, e, out.Bytes()
}
func captureProtocolEvidence(t *testing.T, name string, raw []byte) {
	t.Helper()
	directory := os.Getenv("FAULTLINE_PROTOCOL_EVIDENCE_DIR")
	if directory == "" {
		return
	}
	if os.Getenv("FAULTLINE_WORKER_TEST_REVISION") == "" {
		t.Fatal("capture requires committed source")
	}
	info, e := os.Lstat(directory)
	if e != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		t.Fatal("private capture directory required")
	}
	f, e := os.OpenFile(filepath.Join(directory, name+".json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal(e)
	}
	n, e := f.Write(raw)
	ce := f.Close()
	if e != nil || ce != nil || n != len(raw) {
		t.Fatal("capture failed")
	}
}
func TestMeasurementProtocolRealLocalEndToEnd(t *testing.T) {
	for _, latency := range []int{80, 400} {
		t.Run(fmt.Sprint(latency), func(t *testing.T) {
			c := configuration(latency, 3, 1000)
			l := newLocalLab(t)
			s := newControlLab(t, c, l, "")
			response, e, raw := callMeasurement(t, context.Background(), c, l, s)
			r := response.Result
			if e != nil || r.Status != "complete" || r.FaultCleanup.Status != "confirmed" || r.RuntimeQualified || r.WholeLabCleanup != "unavailable" {
				t.Fatal(e, r.Status, r.FaultCleanup)
			}
			status := 200
			failures := 0
			if latency == 400 {
				status = 504
				failures = 3
			}
			for _, sample := range r.Fault.Samples {
				if sample.Status != status {
					t.Fatal(sample)
				}
			}
			if r.Fault.Summary.Failures != failures || r.Baseline.Summary.Failures != 0 || r.Recovery.Summary.Failures != 0 {
				t.Fatal("summary mismatch")
			}
			if l.requests.Load() != 10 || l.dependencies.Load() != 10 || l.connections.Load() != 10 {
				t.Fatal("measurement retry/count mismatch")
			}
			calls, left := s.snapshot()
			if len(calls) != 7 || left != 0 {
				t.Fatal(calls, left)
			}
			t.Logf("real HTTP control/data: latency=%d baseline=%.6f fault=%.6f recovery=%.6f HTTP=%d failures=%d/3", latency, r.Baseline.Summary.MeanMS, r.Fault.Summary.MeanMS, r.Recovery.Summary.MeanMS, status, failures)
			captureProtocolEvidence(t, fmt.Sprintf("protocol-%d", latency), raw)
		})
	}
}
func TestStrictMeasurementRequestAndBindingBeforeSideEffects(t *testing.T) {
	c := configuration(80, 1, 1000)
	r := measurementRequest(c)
	raw, _ := r.Encode()
	bad := [][]byte{[]byte(`{}`), []byte(`null`), []byte(`{"x":"\ud800"}`), []byte{255}, bytes.Repeat([]byte(" "), MaxMeasurementInput+1), append(append([]byte{}, raw...), '{')}
	for _, field := range []string{`"schema_version":2`, `"schema_version":null`, `"Schema_Version":1`, `"schema_version":1,"\u0073chema_version":1`, `"schema_version":1,"target":"http://public.example.invalid"`, `"schema_version":1,"source_revision":"` + strings.Repeat("f", 40) + `"`} {
		bad = append(bad, bytes.Replace(raw, []byte(`"schema_version":1`), []byte(field), 1))
	}
	bad = append(bad, bytes.Replace(raw, []byte(`"tracing":"off"`), []byte(`"tracing":"on"`), 1), bytes.Replace(raw, []byte(`"samples_per_phase":1`), []byte(`"samples_per_phase":1.0`), 1), bytes.Replace(raw, []byte(`"latency_ms":80`), []byte(`"latency_ms":null`), 1))
	l := newLocalLab(t)
	s := newControlLab(t, c, l, "")
	binding := bindingFor(t, c, l, s)
	for _, b := range bad {
		var out bytes.Buffer
		if _, e := executeMeasurement(context.Background(), bytes.NewReader(b), &out, binding); e == nil || out.Len() != 0 {
			t.Fatal("bad request executed")
		}
	}
	wrong := r
	wrong.RunID = "faultline-" + strings.Repeat("e", 32)
	mismatch, _ := wrong.Encode()
	var out bytes.Buffer
	if _, e := executeMeasurement(context.Background(), bytes.NewReader(mismatch), &out, binding); e == nil {
		t.Fatal("mismatched scope")
	}
	if binding.consumed.Load() || l.requests.Load() != 0 {
		t.Fatal("invalid request consumed authority or made requests")
	}
	calls, _ := s.snapshot()
	if len(calls) != 0 {
		t.Fatal(calls)
	}
	if _, e := DecodeMeasurementRequest(context.Background(), failingReader{}); e == nil {
		t.Fatal("input error")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := DecodeMeasurementRequest(ctx, bytes.NewReader(raw)); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	if _, e := json.Marshal(binding); e == nil {
		t.Fatal("serialized binding")
	}
	var forged harnessBinding
	if json.Unmarshal([]byte(`{}`), &forged) == nil {
		t.Fatal("forged binding")
	}
}
func TestMeasurementProtocolControlFailuresRemainHonest(t *testing.T) {
	for _, mode := range []string{"lost-post", "malformed-post", "wrong-toxic", "changed-toxic", "lost-delete", "failed-delete", "still-present", "inconsistent-absence"} {
		t.Run(mode, func(t *testing.T) {
			c := configuration(1, 1, 1000)
			l := newLocalLab(t)
			s := newControlLab(t, c, l, mode)
			response, e, raw := callMeasurement(t, context.Background(), c, l, s)
			r := response.Result
			if !errors.Is(e, ErrExecution) || r.Status != "incomplete" || r.FaultCleanup.Status != "unconfirmed" || r.Recovery.Summary != nil {
				t.Fatal(e, r.Status, r.FaultCleanup)
			}
			if mode == "lost-post" || mode == "malformed-post" || mode == "wrong-toxic" || mode == "changed-toxic" {
				if r.AppliedFault.Application != "unknown" {
					t.Fatal(r.AppliedFault)
				}
			}
			if mode == "lost-delete" && r.FaultCleanup.Verification != "absent" {
				t.Fatal("lost delete was not independently observed", r.FaultCleanup)
			}
			if bytes.Contains(raw, []byte("private")) {
				t.Fatal("raw error leaked")
			}
			calls, _ := s.snapshot()
			if len(calls) > 7 {
				t.Fatal("unbounded control", calls)
			}
			if mode == "lost-post" {
				captureProtocolEvidence(t, "protocol-lost-apply", raw)
			}
		})
	}
}
func TestMeasurementCancellationAndOutputFailure(t *testing.T) {
	t.Run("canceled-after-apply", func(t *testing.T) {
		c := configuration(400, 3, 1000)
		l := newLocalLab(t)
		s := newControlLab(t, c, l, "")
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() {
			ticker := time.NewTicker(time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if l.requests.Load() >= 5 {
						cancel()
						return
					}
				}
			}
		}()
		response, e, raw := callMeasurement(t, ctx, c, l, s)
		r := response.Result
		if !errors.Is(e, context.Canceled) || r.Status != "canceled" || r.FaultCleanup.Status != "confirmed" || len(r.Fault.Samples) != 1 || r.Fault.Samples[0].Error != "request_canceled" {
			t.Fatal(e, r.Status, r.FaultCleanup, r.Fault)
		}
		captureProtocolEvidence(t, "protocol-canceled", raw)
	})
	t.Run("output-failure-after-cleanup", func(t *testing.T) {
		c := configuration(1, 1, 1000)
		l := newLocalLab(t)
		s := newControlLab(t, c, l, "")
		binding := bindingFor(t, c, l, s)
		raw, _ := measurementRequest(c).Encode()
		response, e := executeMeasurement(context.Background(), bytes.NewReader(raw), shortMeasurementWriter{}, binding)
		if !errors.Is(e, ErrOutput) || response.Result.FaultCleanup.Status != "confirmed" || l.latency.Load() != 0 {
			t.Fatal(e, response.Result.FaultCleanup)
		}
		before := l.requests.Load()
		var out bytes.Buffer
		if _, e := executeMeasurement(context.Background(), bytes.NewReader(raw), &out, binding); e == nil || out.Len() != 0 || l.requests.Load() != before {
			t.Fatal("replayed after lost output")
		}
	})
}

type shortMeasurementWriter struct{}

func (shortMeasurementWriter) Write(b []byte) (int, error) { return len(b) / 2, nil }
func TestMeasurementResponseCorrelationAndConcurrentConsumption(t *testing.T) {
	c := configuration(1, 1, 1000)
	l := newLocalLab(t)
	s := newControlLab(t, c, l, "")
	binding := bindingFor(t, c, l, s)
	req := measurementRequest(c)
	raw, _ := req.Encode()
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	outs := make(chan []byte, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var out bytes.Buffer
			_, e := executeMeasurement(context.Background(), bytes.NewReader(raw), &out, binding)
			errs <- e
			outs <- out.Bytes()
		}()
	}
	wg.Wait()
	close(errs)
	close(outs)
	success := 0
	for e := range errs {
		if e == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatal("two execution authorities", success)
	}
	var response []byte
	for b := range outs {
		if len(b) > 0 {
			response = b
		}
	}
	if _, e := DecodeMeasurementResponse(context.Background(), bytes.NewReader(response), req, c.SourceRevision); e != nil {
		t.Fatal(e)
	}
	bad := req
	bad.RunID = "faultline-" + strings.Repeat("f", 32)
	if _, e := DecodeMeasurementResponse(context.Background(), bytes.NewReader(response), bad, c.SourceRevision); e == nil {
		t.Fatal("wrong run")
	}
	if _, e := DecodeMeasurementResponse(context.Background(), bytes.NewReader(response), req, strings.Repeat("f", 40)); e == nil {
		t.Fatal("wrong source")
	}
	for _, b := range [][]byte{[]byte(`{}`), []byte(`null`), bytes.Repeat([]byte("x"), MaxMeasurementOutput+1), bytes.Replace(response, []byte(`"schema_version":1`), []byte(`"schema_version":2`), 1), bytes.Replace(response, []byte(`"kind":`), []byte(`"Kind":`), 1)} {
		if _, e := DecodeMeasurementResponse(context.Background(), bytes.NewReader(b), req, c.SourceRevision); e == nil {
			t.Fatal("malformed response accepted")
		}
	}
	calls, _ := s.snapshot()
	if len(calls) != 7 || l.requests.Load() != 4 {
		t.Fatal("duplicate side effects", calls, l.requests.Load())
	}
}

var _ io.Writer = shortMeasurementWriter{}

func TestCanceledApplyKeepsCleanupUnconfirmed(t *testing.T) {
	c := configuration(80, 1, 1000)
	l := newLocalLab(t)
	s := newControlLab(t, c, l, "interrupted-post")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	response, e, raw := callMeasurement(t, ctx, c, l, s)
	if !errors.Is(e, context.DeadlineExceeded) || response.Result.Status != "canceled" || response.Result.AppliedFault.Application != "unknown" || response.Result.FaultCleanup.Status != "unconfirmed" {
		t.Fatal(e, response.Result)
	}
	calls, left := s.snapshot()
	if left != 1 {
		t.Fatal("lost ambiguous application")
	}
	for _, call := range calls {
		if strings.HasPrefix(call, "DELETE") {
			t.Fatal("adopted uncertain toxic")
		}
	}
	captureProtocolEvidence(t, "protocol-interrupted-apply", raw)
}
