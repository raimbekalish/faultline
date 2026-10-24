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
