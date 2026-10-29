package labworker

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/netip"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Test-binary helper only. No measurement command is added to the actual worker.
// Endpoints arrive from this parent test's owned servers, never a public CLI.
func TestMeasurementProcessHelper(t *testing.T) {
	if os.Getenv("FAULTLINE_PROTOCOL_TEST_CHILD") != "1" {
		t.Skip("test subprocess only")
	}
	raw, e := io.ReadAll(io.LimitReader(os.Stdin, MaxMeasurementInput+1))
	if e != nil {
		os.Exit(11)
	}
	request, e := DecodeMeasurementRequest(context.Background(), bytes.NewReader(raw))
	if e != nil {
		os.Exit(12)
	}
	cfg, e := request.configuration(os.Getenv("FAULTLINE_PROTOCOL_TEST_REVISION"))
	if e != nil {
		os.Exit(13)
	}
	app, e := netip.ParseAddrPort(os.Getenv("FAULTLINE_PROTOCOL_TEST_APP"))
	if e != nil {
		os.Exit(14)
	}
	ctl, e := netip.ParseAddrPort(os.Getenv("FAULTLINE_PROTOCOL_TEST_CONTROL"))
	if e != nil {
		os.Exit(15)
	}
	binding, e := newHarnessBinding(cfg, app, ctl)
	if e != nil {
		os.Exit(16)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM)
	defer stop()
	_, e = executeMeasurement(ctx, bytes.NewReader(raw), os.Stdout, binding)
	if errors.Is(e, context.Canceled) {
		os.Exit(3)
	}
	if e != nil {
		os.Exit(17)
	}
	os.Exit(0)
}
func TestMeasurementProcessInterruption(t *testing.T) {
	for _, mode := range []string{"cooperative", "abrupt"} {
		t.Run(mode, func(t *testing.T) {
			c := configuration(400, 5, 1000)
			l := newLocalLab(t)
			s := newControlLab(t, c, l, "")
			req := measurementRequest(c)
			raw, _ := req.Encode()
			binary, e := os.Executable()
			if e != nil {
				t.Fatal(e)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, binary, "-test.run=^TestMeasurementProcessHelper$")
			cmd.Env = append(os.Environ(), "FAULTLINE_PROTOCOL_TEST_CHILD=1", "FAULTLINE_PROTOCOL_TEST_APP="+l.frontend.Listener.Addr().String(), "FAULTLINE_PROTOCOL_TEST_CONTROL="+s.server.Listener.Addr().String(), "FAULTLINE_PROTOCOL_TEST_REVISION="+c.SourceRevision)
			cmd.Stdin = bytes.NewReader(raw)
			var out, stderr bytes.Buffer
			cmd.Stdout = &out
			cmd.Stderr = &stderr
			if e = cmd.Start(); e != nil {
				t.Fatal(e)
			}
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			waited := false
			t.Cleanup(func() {
				if !waited {
					_ = cmd.Process.Kill()
					<-done
				}
			})
			ticker := time.NewTicker(5 * time.Millisecond)
			defer ticker.Stop()
			deadline := time.NewTimer(5 * time.Second)
			defer deadline.Stop()
		waitForFault:
			for {
				select {
				case e = <-done:
					waited = true
					t.Fatalf("child exited before fault: %v", e)
				case <-deadline.C:
					t.Fatal("owned child did not reach fault phase")
				case <-ticker.C:
					if l.latency.Load() == 400 && l.requests.Load() >= int32(2+c.Specification.Samples) {
						break waitForFault
					}
				}
			}
			if mode == "cooperative" {
				e = cmd.Process.Signal(syscall.SIGTERM)
			} else {
				e = cmd.Process.Kill()
			}
			if e != nil {
				t.Fatal(e)
			}
			select {
			case e = <-done:
				waited = true
			case <-time.After(5 * time.Second):
				t.Fatal("owned child failed to exit")
			}
			if mode == "cooperative" {
				var exit *exec.ExitError
				if !errors.As(e, &exit) || exit.ExitCode() != 3 {
					t.Fatal("wrong signal result", e, stderr.String())
				}
				response, err := DecodeMeasurementResponse(context.Background(), bytes.NewReader(out.Bytes()), req, c.SourceRevision)
				if err != nil || response.Result.Status != "canceled" || response.Result.FaultCleanup.Status != "confirmed" || l.latency.Load() != 0 {
					t.Fatal("cooperative cleanup not confirmed", err, response.Result.FaultCleanup)
				}
				captureProtocolEvidence(t, "protocol-process-canceled", out.Bytes())
			} else {
				if e == nil || out.Len() != 0 || l.latency.Load() != 400 {
					t.Fatal("abrupt interruption invented cleanup/result", e, out.Len(), l.latency.Load())
				}
				calls, left := s.snapshot()
				if left != 1 {
					t.Fatal("lost unresolved fault")
				}
				for _, call := range calls {
					if strings.HasPrefix(call, "DELETE") {
						t.Fatal("unexpected cleanup after SIGKILL")
					}
				}
				captureProtocolEvidence(t, "process-abrupt-observation", []byte("{\"test_observation\":\"SIGKILL\",\"worker_result_bytes\":0,\"mock_toxic_present\":true,\"cleanup\":\"unconfirmed\",\"runtime_qualified\":false}\n"))
			}
			// Only the parent-owned mock servers are torn down by t.Cleanup. The abrupt
			// case remains unconfirmed; no real proxy or Docker deletion is attempted.
		})
	}
}
