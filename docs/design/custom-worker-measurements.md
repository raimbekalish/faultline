# Measurement-capable worker foundation — Unreleased M8B3A

`internal/labworker` contains real HTTP measurement code and a bounded lifecycle,
exercised only by scoped dependency-injected tests. **Production still has only
`lab plan`; `cmd/lab-worker lab-worker` remains preflight-only.** No production
caller imports the measurement package. No Docker command, Toxiproxy driver,
container execution, image build/pull or runtime qualification is added.

See [ADR 0012](../decisions/0012-custom-worker-measurements.md), the
[original lab design](custom-service-lab.md) and
[transport gate](docker-transport-certification.md).

## Inputs and closed HTTP binding

The typed configuration contains the existing schema-1 experiment spec, its
recomputed canonical SHA-256, preassigned launch/run identity and explicit full
40-lowercase-hex source revision. Reuse spec validation: 1–100 samples per phase,
1–5000 ms latency and 10–5000 ms request deadline. Invalid configuration is an
error before any request or controller call. No target URL, commands, environment,
mount or plugin fields exist. Source revision is caller-supplied metadata, not
execution authentication.

A private same-package harness constructor accepts only a valid loopback numeric
address/port, never hostnames/public addresses. Requests always use GET `/work`
and fixed Host `frontend:8080`; the transport dials only the injected loopback
listener. The endpoint is never serialized. This is **not** the future Docker
binding implementation or authorization to test arbitrary local applications.

The client disables environment proxies, redirects, compression and keep-alive
reuse. Each observation opens a fresh connection, preventing Go's automatic retry
of an idempotent request on a reused connection. No retry loop exists in measured
phases. At most one request is active; per-request timeout covers headers and body.
Headers are capped at 8192 bytes; read/discard at most 65537 response bytes to detect
the 64 KiB limit. Bodies, headers, raw URLs/errors and arbitrary application data
never enter results. Intentional fresh-connection overhead is part of these
end-to-end timings; do not compare them as identical to sample reuse behavior.

## Lifecycle and budgets

1. Validate configuration/client deadline before work. Whole-run context is bounded
   to 120 seconds; all stage contexts inherit cancellation.
2. At most 20 full-path readiness GETs within five seconds, 50 ms between failed
   attempts. Preserve readiness observations separately; they do not enter the
   baseline/fault/recovery summaries. One successful full-path request ends readiness.
3. Measure exactly the configured baseline sample count unless canceled/deadlined.
   Any baseline failures refuse injection and leave later phases unavailable.
4. Register cleanup **before** the single controller apply call. Intent fixes exact
   launch/run IDs, configured milliseconds, downstream direction, zero jitter and
   toxicity one. Apply has a five-second context bounded by the run deadline.
   Error, unavailable acknowledgement or interruption means application UNKNOWN.
5. Measure the configured fault sample count. HTTP failures are observations; a
   timeout sample is not fabricated or silently dropped. Cancellation aborts the
   phase and preserves any in-flight outcome as partial evidence.
6. Once only, remove and verify that run's fault under a **fresh independent
   five-second context**, also used on failed/ambiguous apply or interrupted work.
   Both acknowledged removal and verified absence are needed for confirmed cleanup.
   A remove error followed by apparent absence remains unconfirmed. No automatic
   control retries, shared reset or deletion by prefix exists.
7. Measure recovery only after full fault measurements and confirmed removal.
   Any recovery failures leave the worker result incomplete.
8. Finalize actual phase data, uncertainty and status. Complete means all worker
   phases exist at configured counts, healthy baseline/recovery, confirmed apply
   and confirmed **fault** removal. It does not mean fault samples must succeed.

The maximum is 320 HTTP attempts (20 readiness + 3×100), one apply and one remove/
verify pair. A valid large workload may hit the 120-second budget and remain partial.
Cleanup adds at most five seconds of cooperative work. A controller must honor its
context; no engine can forcibly interrupt arbitrary blocking injected code or a
stalled kernel. There is no real controller implementation in this increment.

## Error and missing-data semantics

- `http_status` preserves the actual non-2xx status (including 504). A fully read
  504 response is an HTTP outcome, not a worker request-timeout classification.
- `request_timeout` means the client deadline expired while the parent phase/run
  context was still active. A baseline timeout fails baseline, not the whole run
  as canceled. Body-read timeouts retain an already received HTTP status.
- `request_canceled` and `context_deadline` describe a canceled/expired parent
  request context. Whole-run issues separately identify `run_canceled` or
  `run_deadline`; readiness has its own shorter deadline.
- `invalid_http_status` retains a received nonstandard numeric response (tested
  with 700) as failed evidence rather than making the report unencodable. This
  custom result permits bounded three-digit numeric outcomes; legacy sample
  report validation is unchanged.
- Connection, transport, body-read and oversize outcomes use bounded fixed codes.
  Low-level diagnostic text is discarded. No arbitrary controller error is copied.
- Phase status is `unavailable`, `partial` or `complete`. Missing phase summaries
  are null and samples are empty arrays. Partial summaries describe only recorded
  attempts. Readiness complete means its final attempt succeeded, not 20 attempts.
- Worker status is `complete`, `incomplete` or `canceled`. Operational apply/remove/
  readiness/baseline/recovery failures are incomplete; parent/whole-run cancellation
  is canceled. Cleanup failure always prevents complete.

A readiness GET exercises the expected application path but does not by itself
prove the selected application actually used the dependency/proxy. Actual contract
conformance, counter-path verification and network isolation remain future gates.

## New ephemeral result and validation

Result schema 1 has kind `faultline-custom-worker-result`, configuration/identity,
`implementation_capability: "http-work-measurement-v1"`, four phase records,
`applied_fault`, `fault_cleanup`, bounded issues/uncertainty and status.
`runtime_qualified` is always false and `whole_lab_cleanup` is always unavailable.
**Worker fault removal is not proxy deletion, Docker absence or a host cleanup
receipt.** The controller's acknowledgement is not M8B2C resource certification.

Durations/statistics are milliseconds, failure rates fractions, sample counts exact.
Every summary is recomputed by the existing engine summarizer; arithmetic mean
roundoff uses existing `record.SameArithmetic`, all other fields compare exactly.
Validate phase/count/status/cleanup/uncertainty relationships and redacted error
codes before encoding or accepting output. No private endpoint or ownership nonce
is included. There are no comparison deltas or significance/causality claims.

`Report.Encode` emits deterministic struct-field JSON plus newline for identical
observations. `Decode` accepts at most 128 KiB, uses shared strict Unicode/duplicate/
nesting validation, refuses unknown/case-ambiguous/null/omitted required fields and
revalidates semantics. Nullable summaries and the existing sample's optional error
are explicit. Reader cancellation is cooperative; a blocked reader needs its owner
to close it. No stream/process attachment or filesystem publication is implemented.

This result is not format-1 evidence, a schema-1 sample report, SQLite history or
input to current reliability gates. All those contracts remain unchanged. Preflight
still emits its original kind/status, `execution_available: false`, capability
`validate_configuration` and `measurements: null`. The measurement engine exists
internally without changing that protocol or advertising a released command.

## Reproduce local HTTP verification

```sh
go test -race -count=1 -v ./internal/labworker
go test -race -count=1 ./internal/labprotocol ./cmd/lab-worker
```

Tests use genuine httptest HTTP frontend/dependency services. A deterministic fake
controller changes a dependency **handler delay**. At 400 ms, the frontend's real
250 ms HTTP deadline yields actual 504s. This is useful functional evidence, not a
Toxiproxy network experiment, independently built container qualification or a
statistical benchmark. Broad timing bounds allow scheduler/connection overhead.
Exact request and fresh-connection counts detect retries independently of latency.
Evidence capture is an opt-in test harness facility under ignored private artifacts;
it never modifies old experiment data or enables a production command.
