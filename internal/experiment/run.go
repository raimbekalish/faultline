// Package experiment runs a bounded baseline/fault/recovery comparison against
// Faultline's fixed, isolated sample topology. It accepts no arbitrary targets.
package experiment

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"runtime"
	"time"

	"github.com/raimbekalish/faultline/internal/localhttp"
	"github.com/raimbekalish/faultline/internal/toxiproxy"
)

const Target = "http://frontend:8080/work"
const MaxDuration = 2 * time.Minute
const CleanupTimeout = 5 * time.Second

type Config struct {
	Samples          int `json:"samples_per_phase"`
	LatencyMS        int `json:"latency_ms"`
	RequestTimeoutMS int `json:"request_timeout_ms"`
}

func DefaultConfig() Config { return Config{Samples: 10, LatencyMS: 100, RequestTimeoutMS: 1000} }

func (c Config) Validate() error {
	if c.Samples < 1 || c.Samples > 100 {
		return fmt.Errorf("samples must be between 1 and 100")
	}
	if c.LatencyMS < 1 || c.LatencyMS > 5000 {
		return fmt.Errorf("latency must be between 1 and 5000 milliseconds")
	}
	if c.RequestTimeoutMS < 10 || c.RequestTimeoutMS > 5000 {
		return fmt.Errorf("request timeout must be between 10 and 5000 milliseconds")
	}
	return nil
}

type Comparison struct {
	MeanDeltaMS      float64 `json:"mean_delta_ms"`
	P95DeltaMS       float64 `json:"p95_delta_ms"`
	FailureRateDelta float64 `json:"failure_rate_delta"`
}

type Report struct {
	SchemaVersion    int        `json:"schema_version"`
	RunID            string     `json:"run_id"`
	Status           string     `json:"status"`
	StartedAt        time.Time  `json:"started_at"`
	FinishedAt       time.Time  `json:"finished_at"`
	Config           Config     `json:"config"`
	Target           string     `json:"target"`
	GoVersion        string     `json:"go_version"`
	Revision         string     `json:"revision"`
	ToxiproxyVersion string     `json:"toxiproxy_version"`
	Baseline         Phase      `json:"baseline"`
	Fault            Phase      `json:"fault"`
	Recovery         Phase      `json:"recovery"`
	Comparison       Comparison `json:"fault_vs_baseline"`
	CleanupComplete  bool       `json:"cleanup_complete"`
	Error            string     `json:"error,omitempty"`
}

type control interface {
	Create(context.Context, string) error
	AddLatency(context.Context, string, time.Duration) error
	RemoveLatency(context.Context, string) error
	Delete(context.Context, string) error
}

// Run owns exactly one proxy in a disposable Compose stack. Concurrent runners
// in one stack fail on the fixed data port rather than reusing another proxy.
func Run(ctx context.Context, cfg Config, revision string) (Report, error) {
	c := toxiproxy.New()
	defer c.Close()
	return run(ctx, cfg, revision, c, Target)
}

func run(parent context.Context, cfg Config, revision string, ctl control, target string) (report Report, err error) {
	executionCtx := parent
	report = Report{SchemaVersion: 1, Status: "failed", Config: cfg, Target: target, GoVersion: runtime.Version(), Revision: revision, ToxiproxyVersion: "2.12.0", StartedAt: time.Now().UTC()}
	defer func() {
		report.FinishedAt = time.Now().UTC()
		if err != nil {
			report.Error = err.Error()
			// An individual control/readiness/cleanup timeout is an operational
			// failure, not cancellation of the experiment. The deferred local
			// cancel below must not change that classification.
			if parent.Err() != nil || errors.Is(context.Cause(executionCtx), context.DeadlineExceeded) {
				report.Status = "canceled"
			}
		} else {
			report.Status = "complete"
		}
	}()
	if err = cfg.Validate(); err != nil {
		return report, err
	}
	if err = parent.Err(); err != nil {
		return report, err
	}
	id := make([]byte, 16)
	if _, err = rand.Read(id); err != nil {
		return report, fmt.Errorf("run ID: %w", err)
	}
	report.RunID = "faultline-" + hex.EncodeToString(id)
	ctx, cancel := context.WithTimeout(parent, MaxDuration)
	executionCtx = ctx
	defer cancel()
	client := localhttp.New(time.Duration(cfg.RequestTimeoutMS) * time.Millisecond)
	defer client.CloseIdleConnections()
	// Register cleanup before the create request: a timed-out POST can still have
	// succeeded on the server. Never use the canceled experiment context here.
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), CleanupTimeout)
		defer cancel()
		cleanupErr := ctl.Delete(cleanupCtx, report.RunID)
		report.CleanupComplete = cleanupErr == nil
		if cleanupErr != nil {
			err = errors.Join(err, fmt.Errorf("proxy cleanup: %w", cleanupErr))
		}
	}()
	if err = ctl.Create(ctx, report.RunID); err != nil {
		return report, fmt.Errorf("create isolated proxy: %w", err)
	}
	// Readiness exercises the complete application path and is excluded from data.
	readyCtx, readyCancel := context.WithTimeout(ctx, 5*time.Second)
	defer readyCancel()
	for {
		var p Phase
		p, err = measure(readyCtx, client, target, 1)
		if err == nil && p.Summary.Failures == 0 {
			break
		}
		select {
		case <-readyCtx.Done():
			return report, fmt.Errorf("sample readiness: %w", readyCtx.Err())
		case <-time.After(50 * time.Millisecond):
		}
	}
	report.Baseline, err = measure(ctx, client, target, cfg.Samples)
	if err != nil {
		return report, err
	}
	if report.Baseline.Summary.Failures != 0 {
		return report, fmt.Errorf("baseline has failures; refusing an invalid comparison")
	}
	if err = ctl.AddLatency(ctx, report.RunID, time.Duration(cfg.LatencyMS)*time.Millisecond); err != nil {
		return report, fmt.Errorf("inject latency: %w", err)
	}
	report.Fault, err = measure(ctx, client, target, cfg.Samples)
	report.Comparison = Comparison{
		MeanDeltaMS:      report.Fault.Summary.MeanMS - report.Baseline.Summary.MeanMS,
		P95DeltaMS:       report.Fault.Summary.P95MS - report.Baseline.Summary.P95MS,
		FailureRateDelta: report.Fault.Summary.FailureRate - report.Baseline.Summary.FailureRate,
	}
	if err != nil {
		return report, err
	}
	if err = ctl.RemoveLatency(ctx, report.RunID); err != nil {
		return report, fmt.Errorf("remove latency: %w", err)
	}
	report.Recovery, err = measure(ctx, client, target, cfg.Samples)
	if err == nil && report.Recovery.Summary.Failures != 0 {
		err = fmt.Errorf("recovery has failures after removing latency")
	}
	return report, err
}
