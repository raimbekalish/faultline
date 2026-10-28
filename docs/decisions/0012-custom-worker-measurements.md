# ADR 0012: Dependency-injected custom worker measurements

Status: proposed for M8B3A independent review (Unreleased).

## Context

[ADR 0009](0009-owner-selected-service-labs.md) requires separately versioned,
ephemeral custom results and reuse of sample statistics without widening sample
targets. M8B2C prepared commands/certification, but its worker entrypoint validates
preflight only. Measurements must become testable before any runtime provisioning.

## Decision

Implement `internal/labworker` as an internal, production-disconnected engine.
A private client constructor accepts only an explicit loopback address for scoped
HTTP tests, maps the fixed `http://frontend:8080/work` contract to that listener,
and accepts no URL in experiment configuration. There is no production caller or
real Toxiproxy/Docker driver. Keep `cmd/lab-worker` and preflight output unchanged.

Revalidate the existing spec and its canonical hash, preassigned launch/run IDs and
explicit source revision. Bound readiness by both time and attempts; use serial
baseline/fault/recovery measurements, fresh connections and existing
`experiment.Summarize`. Baseline failures prevent injection. A closed typed
controller can apply the one prescribed latency intent, remove that run's fault
and verify removal. Register cleanup before apply to cover lost/failed replies.
Removal/verification use one fresh five-second context independent of cancellation.
Never infer success from missing observations or ambiguous cleanup.

Use new result kind `faultline-custom-worker-result`, schema 1. Null summaries mean
unavailable, not zero. Recompute every summary from raw samples during validation.
Record bounded error codes rather than raw transport/control errors or payloads.
Determinism applies to serialization of identical recorded measurements, not
repeated HTTP timings. No timestamps, paths, ambient identity or environment data
are captured. The result is not a sample report, bundle, gate input, durable history
record, ownership certificate or whole-lab cleanup receipt.

Real local HTTP tests use a dependency handler with controlled delay, a frontend
with a 250 ms dependency deadline and a recording fake fault controller. Their
observations are real; their fault mechanism is handler delay, not Toxiproxy packet
latency or Docker isolation. Only future separately authorized runtime integration
can establish those properties.

## Tradeoffs and gates

A separate small lifecycle avoids changing the released sample engine's fixed
transport/readiness/report behavior. Numeric validation and statistics are reused;
no duplicate percentile algorithm, persistence migration or dependency is added.
The internal lifecycle is private so adding a measurement engine cannot silently
turn the existing entrypoint into execution capability. A later milestone must
review the host/worker execution protocol, real fault adapter and cancellation/
cleanup behavior with actual runtime resources.

The preflight recipe omitted transitive compile-time dependencies of `spec`
(which imports the sample engine for validation). Correct its selective COPY
closure and locked module-download step without refactoring sample code or building
an image. The new engine is deliberately not copied into that preflight image.
Source dependency closure checks are not successful Docker-build evidence.

Docker DNS documentation/source review supports keeping the existing loopback
upstream profile; it does not qualify resolution, egress or isolation. See the
[DNS qualification plan](../security/custom-lab-dns-qualification.md),
[worker contract](../design/custom-worker-measurements.md) and
[validation](../validation.md). Real provisioning still requires every unresolved
M8B2C continuity, transport, reconciliation and owner-authorization gate.
