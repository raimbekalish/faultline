package labworker

import (
	"context"
	"time"

	"github.com/raimbekalish/faultline/internal/experiment"
)

const runTimeout = 120 * time.Second
const readinessTimeout = 5 * time.Second
const readinessRequests = 20
const cleanupTimeout = 5 * time.Second
const applyTimeout = 5 * time.Second

type ApplicationState string

const (
	Applied            ApplicationState = "applied"
	ApplicationUnknown ApplicationState = "unknown"
)

type Presence string

const (
	Absent  Presence = "absent"
	Present Presence = "present"
	Unknown Presence = "unknown"
)

// FaultController has no executable implementation. Each call is scoped to the
// validated intent's exact launch/run identity. Implementations must honor ctx;
// a future transport and its ownership/continuity proofs require separate review.
type FaultController interface {
	ApplyLatency(context.Context, LatencyIntent) (ApplicationState, error)
	RemoveLatency(context.Context, LatencyIntent) error
	VerifyRemoved(context.Context, LatencyIntent) (Presence, error)
}

// run is deliberately private and has no production caller. Tests inject a
// loopback-only client and a recording controller, never Toxiproxy or Docker.
func run(parent context.Context, c Configuration, client *contractClient, ctl FaultController) (Report, error) {
	return runWithBudget(parent, c, client, ctl, runTimeout)
}
func runWithBudget(parent context.Context, c Configuration, client *contractClient, ctl FaultController, budget time.Duration) (report Report, err error) {
	if c.validate() != nil || client == nil || client.client == nil || client.transport == nil || client.client.Timeout != time.Duration(c.Specification.RequestTimeoutMS)*time.Millisecond || ctl == nil || budget <= 0 || budget > runTimeout {
		return Report{}, ErrConfiguration
	}
	report = initial(c)
	ctx, cancel := context.WithTimeout(parent, budget)
	defer cancel()
	defer client.close()
	attempted, cleaned := false, false
	clean := func() {
		if !attempted || cleaned {
			return
		}
		cleaned = true
		// Background is intentional: cancellation/deadline of measurements never
		// cancels this one bounded independent cleanup/verification attempt.
		cleanupCtx, stop := context.WithTimeout(context.Background(), cleanupTimeout)
		defer stop()
		report.FaultCleanup = Cleanup{"unconfirmed", "unknown", "unknown"}
		removalErr := ctl.RemoveLatency(cleanupCtx, report.AppliedFault.Intent)
		if removalErr == nil && cleanupCtx.Err() == nil {
			report.FaultCleanup.Removal = "acknowledged"
		}
		presence, verifyErr := ctl.VerifyRemoved(cleanupCtx, report.AppliedFault.Intent)
		if verifyErr == nil && cleanupCtx.Err() == nil && (presence == Absent || presence == Present) {
			report.FaultCleanup.Verification = string(presence)
		}
		if report.FaultCleanup.Removal == "acknowledged" && report.FaultCleanup.Verification == "absent" {
			report.FaultCleanup.Status = "confirmed"
		} else {
			report.Issues = append(report.Issues, "cleanup_unconfirmed")
			report.Uncertainty = append(report.Uncertainty, "fault_removal_unconfirmed")
		}
	}
	defer func() {
		clean()
		if ctx.Err() != nil {
			report.Status = "canceled"
			err = ctx.Err()
			reason := "run_canceled"
			if ctx.Err() == context.DeadlineExceeded {
				reason = "run_deadline"
			}
			report.Issues = append(report.Issues, reason)
		} else if err == nil && report.FaultCleanup.Status == "confirmed" {
			report.Status = "complete"
		} else {
			report.Status = "incomplete"
			err = ErrExecution
		}
		if report.Baseline.Status != "complete" || report.Fault.Status != "complete" || report.Recovery.Status != "complete" {
			report.Uncertainty = append(report.Uncertainty, "measurements_incomplete")
		}
	}()
	report.Readiness = readiness(ctx, client)
	if report.Readiness.Status != "complete" {
		report.Issues = append(report.Issues, "readiness_failed")
		return report, ErrExecution
	}
	report.Baseline, err = measure(ctx, client, c.Specification.Samples)
	if err != nil || report.Baseline.Summary == nil || report.Baseline.Summary.Failures != 0 {
		report.Issues = append(report.Issues, "baseline_failed")
		return report, ErrExecution
	}
	// Register removal before Apply: even a lost/error reply may follow an applied
	// fault. Never infer not-applied from an error and never remove another run.
	attempted = true
	report.AppliedFault.Application = "unknown"
	applyCtx, stop := context.WithTimeout(ctx, applyTimeout)
	outcome, applyErr := ctl.ApplyLatency(applyCtx, report.AppliedFault.Intent)
	applyDone := applyCtx.Err()
	stop()
	if applyErr != nil || applyDone != nil || outcome != Applied {
		report.Issues = append(report.Issues, "apply_failed")
		report.Uncertainty = append(report.Uncertainty, "fault_application_unknown")
		return report, ErrExecution
	}
	report.AppliedFault.Application = "confirmed"
	report.Fault, err = measure(ctx, client, c.Specification.Samples)
	if err != nil {
		report.Issues = append(report.Issues, "fault_interrupted")
		return report, ErrExecution
	}
	clean()
	if report.FaultCleanup.Status != "confirmed" {
		return report, ErrExecution
	}
	report.Recovery, err = measure(ctx, client, c.Specification.Samples)
	if err != nil || report.Recovery.Summary == nil || report.Recovery.Summary.Failures != 0 {
		report.Issues = append(report.Issues, "recovery_failed")
		return report, ErrExecution
	}
	return report, nil
}
func measure(ctx context.Context, client *contractClient, n int) (Phase, error) {
	p := emptyPhase()
	for range n {
		if ctx.Err() != nil {
			return p, ctx.Err()
		}
		p.Samples = append(p.Samples, client.observe(ctx))
		p.Status = "partial"
		s := experiment.Summarize(p.Samples)
		p.Summary = &s
		if ctx.Err() != nil {
			return p, ctx.Err()
		}
	}
	p.Status = "complete"
	return p, nil
}
func readiness(parent context.Context, client *contractClient) Phase {
	ctx, cancel := context.WithTimeout(parent, readinessTimeout)
	defer cancel()
	p := emptyPhase()
	for n := 0; n < readinessRequests; n++ {
		if ctx.Err() != nil {
			return p
		}
		sample := client.observe(ctx)
		p.Samples = append(p.Samples, sample)
		p.Status = "partial"
		s := experiment.Summarize(p.Samples)
		p.Summary = &s
		if ctx.Err() != nil {
			return p
		}
		if sample.Error == "" && sample.Status >= 200 && sample.Status < 300 {
			p.Status = "complete"
			return p
		}
		if n+1 < readinessRequests {
			timer := time.NewTimer(50 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return p
			case <-timer.C:
			}
		}
	}
	return p
}
