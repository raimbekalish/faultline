# JSON reports (schema version 1)

The CLI writes one JSON object to stdout after proxy cleanup. Capture stdout to a
local file; build/progress messages use stderr. Reports are ignored by Git.

| Field | Meaning |
| --- | --- |
| `schema_version` | Currently 1; consumers must reject unsupported versions |
| `run_id` | Unique proxy ownership ID; absent on preflight failure |
| `status` | `complete`, `failed`, or `canceled` |
| `started_at`, `finished_at` | UTC timestamps including readiness and cleanup |
| `config` | Samples per phase, latency and per-request timeout in milliseconds |
| `target` | Fixed sample frontend URL |
| `go_version`, `revision`, `toxiproxy_version` | Runtime/build provenance; dirty builds are marked |
| `baseline`, `fault`, `recovery` | Raw samples and phase summaries |
| `fault_vs_baseline` | Mean/p95 latency deltas (ms), failure-rate delta |
| `cleanup_complete` | Owned proxy deletion was acknowledged, or proxy was already absent |
| `error` | Original and cleanup errors when applicable |
| `telemetry` | Optional runner-only export counters and shutdown outcome; absent when disabled |

Each sample stores `duration_ms`, HTTP `status` (0 when no response was received),
and an optional `error`. Any non-2xx status, transport error, deadline or oversized
body counts as a failure. Failure rate is a fraction in [0, 1]. Phase summaries
include request/failure counts, min/mean/max and nearest-rank p50/p95/p99.
All durations include reading the response body and include failed requests.

Only compare completed phases in complete reports. Failure/cancellation reports
can contain partial or empty phases. `cleanup_complete` concerns the engine's
proxy, not the subsequent Compose teardown; the shell exit status also covers
that teardown. Invalid CLI flags return usage and exit 2 before creating resources
and do not emit an experiment report. Other execution errors exit 1.
`canceled` means the parent context was canceled or the overall execution deadline
expired. Individual control, readiness and cleanup timeouts are operational
failures and use `failed` unless the experiment itself was also canceled.

## Reproducibility and limits

The checked-in topology and pinned image digests define the services. The CLI
embeds the Git revision supplied by the shell and marks uncommitted source as
dirty. The frontend dependency timeout is 250 ms; toxics use zero jitter. Reusing
configuration reproduces the experiment structure, not identical timing values.

The workload is sequential and closed-loop. Readiness traffic warms the path and
is excluded from results. Baseline, fault and recovery have a fixed order. Small
samples and nearest-rank tail percentiles are descriptive only; they provide no
confidence interval or statistical significance. CPU contention, Docker's VM,
connection reuse and host scheduling affect results. Timeouts censor the underlying
dependency's latency; a 400 ms toxic produces roughly a 250 ms frontend response,
not a completed 400 ms dependency call. Zero baseline failures is a precondition
for injection. A fault is allowed to cause failures; recovery failures invalidate
the run. Future repeated trials and concurrent load need a separate methodology.

## Optional trace correlation (additive schema-1 extension)

With `--tracing`, samples also contain `trace_id` and `span_id` identifying the
runner's root HTTP client span. Without tracing those keys and `telemetry` are
omitted, preserving the original JSON shape. Existing fields and their units
retain their meaning; consumers should tolerate additive optional fields.

Span IDs are correlation handles, not proof that Jaeger stored the trace. Export
can fail or drop spans. `telemetry.exported_spans` and `export_failures` describe
only the runner and acknowledged exporter batches, not service delivery or queue
drops. Telemetry shutdown failure does not change the experiment's status or
fault cleanup result. The engine always cleans up before export shutdown starts.

The `evidence` command emits a separate schema-1 diagnostic envelope containing
two ordinary reports (80/400 ms latency), retrieved trace summaries and raw Jaeger
responses. `complete` requires one observed trace snapshot with four unique,
valid nonzero span IDs, the expected service/kind/operation roles, one runner
root, and the exact runner-client → frontend-server → frontend-client →
dependency-server parent chain. Every child must have one same-trace `CHILD_OF`
reference; ambiguous, foreign, or unsupported references are incomplete. Separate
snapshots are never joined and duplicate spans are never silently deduplicated.
Integration tests additionally correlate the root ID and metadata with each
experiment observation. HTTP errors do not by themselves make a trace incomplete.
Partial/malformed traces remain `complete: false` and include `retrieval_error`
when collection ends. Missing evidence has no inferred cause. JSON fields and
schema versions are unchanged. See [tracing](tracing.md) for interpretation.

## Optional environment provenance

Schema 1 permits an optional `environment` object with its own explicit schema
version. New finalized execution reports record allowlisted process/build/spec
facts, methodology, an explicit tracing state, and optional wrapper-observed
Docker/image metadata. Legacy reports keep this absent; export never reconstructs
it. Missing/null tracing facts inside a present environment record are invalid.
See [portable evidence](evidence.md) for field scope, privacy and version rules.
Raw measurement, status, cleanup and tracing fields retain their prior semantics.
