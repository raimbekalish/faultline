# Initial validation record

Date: 2026-10-24. Platform: macOS arm64 with Docker Desktop; Go 1.26.8 in
Linux arm64 containers and Toxiproxy 2.12.0. No host Go installation was used.

## Checks actually run

- `gofmt` over all Go sources and `git diff --check`.
- `go vet ./...` in the pinned Go container.
- `go test -race -count=1 ./...`: passed.
- `./scripts/experiment.sh test`: passed against the real sample services and
  Toxiproxy, with the race detector enabled for unit and integration tests.
- Shell syntax validation (`sh -n scripts/experiment.sh`).
- A remote `DOCKER_HOST` was rejected with exit 2 before any stack creation.
- Local Gitleaks v8.30.1 source scan: no leaks found (network disabled).
- Documented CLI smoke: five samples per phase, 100 ms latency, valid JSON report,
  zero failures, successful proxy and Compose cleanup (including owned image tag).
  Means: baseline 1.00 ms, fault 102.96 ms, recovery 0.21 ms.

The first real integration run measured:

| Injected latency | Baseline mean | Fault mean | Fault failures | Recovery mean |
| --- | ---: | ---: | ---: | ---: |
| 80 ms | 0.27 ms | 84.06 ms | 0 / 5 | 1.07 ms |
| 400 ms | 0.57 ms | 253.56 ms | 5 / 5 (HTTP 504) | 1.43 ms |

The cancellation test observed a toxic on the real control API before canceling
the engine. It verified bounded completion and removal of the owned proxy. All
real tests also created another owner's proxy and verified it remained present
after the experiment; they cleaned up that sentinel themselves. Compose teardown
removed the project's containers and network.

Unit tests cover ambiguous proxy creation, injection/removal errors, cleanup
failure reporting, independent cleanup context after cancellation, rejected
configuration and targets, failing-baseline refusal, percentile calculations,
request timeouts, response-size bounds, redirects and environment proxy refusal.
The final unit race suite and vet also passed after regression tests were added
to distinguish control/cleanup deadlines from cancellation of the whole run.

## Not claimed

No hosted CI, public deployment, production workload test, statistical benchmark,
long-duration soak test, cross-platform matrix, or host-crash recovery test has
run. SIGKILL and Docker daemon failure require explicit project teardown.
The README describes the supported scope and remaining measurement limits.

# Milestone 2 validation — distributed tracing

Date: 2026-10-24 (America/Los_Angeles). Same macOS arm64/Docker environment;
official OpenTelemetry Go SDK/exporter 1.47.0 and digest-pinned Jaeger 2.22.0.

## Checks actually run

- `gofmt`, formatting verification, `go mod verify`, `go vet ./...`, and
  `go test -race -count=1 ./...`: passed in the Go development container.
- `./scripts/experiment.sh test`: passed without Jaeger. Includes the original
  real latency/recovery cases, active-fault cancellation, disabled trace-ID
  assertions, and a real experiment with the runner exporter pointing at the
  absent collector. That test verified proxy deletion **before** telemetry
  shutdown and observed export failures without changing experiment success.
- `./scripts/experiment.sh --tracing test`: passed with real OTLP export/retrieval.
  Every measured observation in the correlation cases had four stored spans.
  Tests checked exact trace/root span IDs, every parent edge, service/kind,
  run ID, phase, observation index, HTTP status and timeout error status.
- Context propagation/metadata, response-header/body deadlines, sanitized error
  events, queue pressure/nonblocking producers, bounded exporter shutdown, missing
  trace representation and Jaeger tag decoding all have passing unit race tests.
- Existing control/cleanup timeout classification and cancellation regressions
  passed in both real topologies. Disabled sample JSON retains its original shape.
- Shell syntax (`sh -n`), Python syntax compilation, Compose config assertions,
  `git diff --check`, and local Gitleaks v8.30.1 scan: passed; no secrets found.

## Stored trace evidence

The diagnostic command `./scripts/experiment.sh --tracing --view evidence`
completed two five-sample-per-phase experiments and retrieved 30 traces / 120
spans. The resulting ignored local artifact is `artifacts/m2-evidence.json`; it
includes complete reports, parsed span summaries, and raw Jaeger query responses.
Both runner reports acknowledged 16 exported spans (15 observations + readiness)
with zero export failures. Full-chain delivery is established by retrieval, not
those runner-only counters.

Representative observations from this CLI evidence run:

| Scenario | Trace ID | HTTP measurement | Dependency client | Dependency server | Outcome |
| --- | --- | ---: | ---: | ---: | --- |
| Baseline | `e79bc8b889144c14d90c08482be32227` | 0.577 ms | 0.259 ms | 0.007 ms | 200 |
| 80 ms latency | `a1494c8e671fb52adc607f66a34947fd` | 81.764 ms | 81.571 ms | 0.001 ms | 200 |
| 400 ms latency | `407d0a16393345a9a06d5775afb75ee6` | 251.980 ms | 251.700 ms | 0.022 ms | 504; dependency client timeout |
| Recovery | `3b4b1559fbf00ac8848811f12f3a059e` | 2.642 ms | 2.159 ms | 0.011 ms | 200 |

The timeout trace contains a successful dependency server span, an errored
dependency client span without a received HTTP status, and HTTP-504 error spans
at the frontend server and runner client. Toxiproxy emits no spans. The response
delay lies within the dependency client interval; the evidence does not imply
the dependency handler spent 400 ms computing. See [tracing](tracing.md) for
additional integration-run samples and interpretation limits.

## Viewer and resources

The validation helper fetched the actual loopback UI (HTTP 200) and a trace via
`/api/traces/<id>` (four spans). Inspection confirmed that only query/UI port
16686 was mapped to `127.0.0.1`, with no published ingestion or admin ports.
The explicit viewer bridge contained only Jaeger; sample networking stayed
internal. Sending SIGTERM to the viewer wrapper returned the expected 143 and
removed containers, both networks and the run image tag. No Faultline containers
or networks remained after the final runs.

A single post-experiment memory snapshot measured frontend 8.82 MiB, dependency
8.70 MiB, Toxiproxy 7.14 MiB and Jaeger 19.37 MiB. These are idle snapshots, not
peak-memory guarantees; configured limits and queue/storage bounds remain needed.

## Measured overhead

`python3 scripts/measure-tracing-overhead.py` was run with no other Faultline
experiments/builds active: two sequential off/on pairs, 50 observations per phase,
80 ms fault. All four reports completed, with cleanup acknowledged and no HTTP
failures. Ignored raw artifact: `artifacts/m2-overhead.json`.

| Pair | Mode | Baseline mean | Fault mean | Recovery mean |
| --- | --- | ---: | ---: | ---: |
| 1 | Off | 0.567 ms | 83.457 ms | 0.478 ms |
| 1 | Jaeger | 0.647 ms | 83.386 ms | 0.559 ms |
| 2 | Off | 0.413 ms | 83.410 ms | 0.457 ms |
| 2 | Jaeger | 0.825 ms | 83.210 ms | 0.626 ms |

Enabled baseline deltas were +0.081/+0.412 ms; recovery +0.080/+0.169 ms.
Negative fault-phase deltas (-0.071/-0.200 ms) do not establish a speedup; timing
variability exceeds that difference. These small trials quantify local impact
without establishing statistical significance or universal overhead bounds.

## Failures found and corrected during implementation

1. The first real correlation run retrieved complete traces but failed status
   assertions because the query adapter expected only `http.response.status_code`.
   Jaeger translates it to `http.status_code` in its JSON response. Added a parser
   regression for both forms; rerunning the real suite passed.
2. The first viewer check could not obtain a usable host port from the internal-only
   network. Docker returned `invalid IP:0`. Added an explicit Jaeger-only viewer
   bridge and a fail-fast port guard. Actual loopback UI/query retrieval and
   signal teardown then passed. The failed check also completed teardown.
3. Final safety review found that the inherited preflight preferred `DOCKER_HOST`
   even when `DOCKER_CONTEXT` selected another endpoint. Updated it to follow
   Docker's precedence. `sh scripts/test-wrapper.sh` passed four shell cases,
   including a remote context with a local host variable and the reverse. These
   guard tests use a CLI stub and contact no daemon; fault tests remain real.

No hosted CI, cross-platform matrix, long soak, production workload, statistically
controlled benchmark, or SIGKILL/daemon-crash trace durability test was run.
Telemetry is best effort and memory-only. Partial/missing traces are represented
explicitly; cancellation does not promise a complete stored trace.

# PR #1 focused review — semantic trace completeness

Date: 2026-10-24 (America/Los_Angeles). Reviewed from synchronized PR head
`37028cb16fe1c0c4b394bedc8cf0656b8ab92277`; `main` remained at `596d0bc`.

## Fixes and review findings

- `complete` now requires four unique canonical nonzero span IDs, the expected
  service/kind/operation roles, exactly one runner root, and the exact three
  same-trace `CHILD_OF` edges. Span order and HTTP success are not requirements.
- Duplicate/extra spans, orphaned or cyclic parents, foreign/ambiguous references,
  and unexpected roles remain incomplete. Raw observed evidence is preserved;
  separate trace objects and polling snapshots are never assembled into a graph.
- A corrected complete four-span snapshot supersedes a previous malformed
  five-span response. Partial evidence survives query errors. Collection canceled
  before starting issues no query. Zero trace IDs are rejected before HTTP access.
- Manual recovery previously selected only the base Compose file. The README and
  failure message now include all tracing/UI overlays and orphan cleanup so an
  interrupted viewer's Jaeger container and both networks are selected.

## Actual checks and outcomes

All checks passed in the pinned Go 1.26.8 / Jaeger 2.22.0 environment:

- `gofmt` and formatting verification, `go mod verify`, `go vet ./...`, and
  `go test -race -count=1 ./...`.
- Focused `go test -race -count=1 -v ./internal/tracequery`: 35 topology cases,
  plus regressions for multiple query objects, corrected smaller snapshots,
  retained partial evidence, canceled collection and invalid IDs. Tests include
  valid unordered and timeout traces, duplicates, each missing role, malformed
  IDs, unexpected services/kinds/routes, root errors, cycles and bad references.
- `./scripts/experiment.sh test`: real 80/400 ms faults, latency/recovery,
  active-fault cancellation and unavailable-exporter cleanup passed without
  Jaeger. The Jaeger-only correlation case was intentionally skipped.
- `./scripts/experiment.sh --tracing test`: real OTLP export/retrieval and all
  18 correlated observation traces passed the stronger completeness validator,
  along with the existing parent/metadata/status checks. Cancellation, timeout
  classification and recovery passed. The no-collector-only export-failure case
  was intentionally skipped here and passed in the disabled suite.
- `sh -n scripts/experiment.sh scripts/test-wrapper.sh`, all four endpoint
  precedence shell checks, and `git diff --check` passed.
- Actual recovery test: started only Jaeger in a uniquely named disposable
  project with the internal and viewer networks, then ran the documented
  all-overlay teardown. Jaeger and both networks were removed; scoped Docker
  queries confirmed no project containers or networks remained.

Representative trace evidence from this review's real integration run:

| Fault | Phase | Trace ID | HTTP observation | Dependency client | Outcome |
| --- | --- | --- | ---: | ---: | --- |
| 80 ms | Baseline | `28ea1978ce2e30a35498764d41d907c6` | 0.85 ms | 0.42 ms | 200 |
| 80 ms | Fault | `13f815ee4f416d02aab8c5b6605f906f` | 81.67 ms | 81.12 ms | 200 |
| 400 ms | Fault | `c959735362f2c261e6734e4d914eb7b6` | 251.86 ms | 251.04 ms | 504; client timeout |
| 400 ms | Recovery | `b35570525c885115a36006d5f439ff08` | 1.70 ms | 1.33 ms | 200 |

No unexpected test failures occurred in this review. Expected exporter deadline
and unavailable-collector diagnostics came from deliberate negative tests.
Ignored logs are `artifacts/pr1-review-{checks,query-tests,disabled,tracing,recovery}.log`.

## Compatibility and cleanup review

JSON schema versions, record fields and optional tracing behavior are unchanged;
only the meaning of `complete` is stricter. Existing disabled-sample JSON tests
passed. The engine, fault client, instrumentation, Compose service definitions,
ownership IDs, independent cleanup context and export-shutdown ordering were not
modified. Both real suites verified owned-proxy removal while retaining the
sentinel proxy, and their Compose teardown removed all run resources.

This is validation of the fixed local topology, not a general trace validator or
a guarantee of future telemetry arrival. Missing/partial telemetry remains best
effort. Overhead measurements, cross-platform testing and crash durability were
not rerun; no instrumentation or workload measurement behavior changed. No merge,
new PR, CI, deployment, or product-scope additions are part of this review.
