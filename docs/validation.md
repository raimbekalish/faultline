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

# Milestone 3 validation — reproducible experiments and history

Date: 2026-10-24 (America/Los_Angeles). Docker Linux/ARM64, pinned Go 1.26.8,
mattn/go-sqlite3 1.14.52, Toxiproxy 2.12.0 and optional Jaeger 2.22.0.

## Approved merge verification

PR #1 was open/mergeable with the exact approved head `0601b80d77ffd385b248f382b0856854bda2d339`.
Main still pointed to `596d0bc1c5eedd0b401a2a94923502d8b3931707`.
GitHub performed a regular merge, producing
`73e2430f7ebd356b641a9f8d97a96579c11b3b32`, with those two commits as parents.
GitHub reported `MERGED`. All seven original feature commits remained reachable
from synchronized local main, with the owner's verified GitHub noreply attribution.
The working tree was clean before creating `codex/experiment-history`.

## Actual checks

- Specification race tests: required/duplicate/unknown/case-sensitive fields,
  incompatible versions, bounds, null/non-integer values, trailing/oversized data,
  forbidden targets and deterministic canonical snapshots/hashes passed.
- Actual SQLite race tests passed for migration initialization/rollback, future
  version/foreign database refusal, connection pragmas, file permissions, unique
  aliases, immutable finalization, comparisons, failed/canceled reports, query
  bounds, missing/malformed/oversized data, filenames, and symlink refusal.
- Real write-failure paths passed: aborting trigger rollback, canceled operations,
  SQLite read-only mode, bounded writer contention, and subsequent successful
  retry. These are actual SQLite operations, not SQL mocks.
- The abrupt subprocess exited without SQLite close with an uncommitted large
  update after dirty-page spill. A nonempty hot journal was confirmed. Reopening
  recovered the original running intent and preserved another committed report.
- Execution-order tests passed: a committed intent is visible before the engine;
  failed intent persistence prevents execution; canceled runs finalize through an
  independent context; combined engine/finalization errors preserve the engine
  result and durable intent. Output errors do not hide execution errors.
- `gofmt`, formatting verification, `go mod verify`, `go vet ./...`, and the final
  full `go test -race -count=1 ./...` passed in the pinned dev container.
- Shell syntax and all Docker endpoint-precedence regressions passed.
- Final `./scripts/experiment.sh test` and `./scripts/experiment.sh --tracing test`
  both passed with package serialization. New real tests repeatedly ran the same
  specification against real sample services/Toxiproxy, reopened the temporary DB,
  verified distinct IDs/config hashes/reports, compared runs, and persisted a run
  canceled after observing its active toxic. Tracing on/off references survived
  database reopen. The CLI tests also confirmed committed history survives an
  output-write failure. Existing latency, recovery, trace graph/correlation,
  unavailable-exporter, cancellation and cleanup tests passed.

## Actual scratch runtime and host persistence

The statically linked history-capable CLI executed inside the scratch image,
using a writable host-owned database-directory bind and read-only root filesystem.
Both specification runs survived stack teardown; history list/show/compare then
ran without frontend/dependency/Toxiproxy/Jaeger startup. The host database was
28,672 bytes with mode 0600 after two runs. No host Go installation or system
SQLite library was used.

| Specification | Baseline mean | Fault mean | Recovery mean | Fault failures |
| --- | ---: | ---: | ---: | ---: |
| 80 ms, 10 samples, tracing off | 1.80 ms | 81.90 ms | 0.42 ms | 0/10 |
| 400 ms, 5 samples, tracing on | 0.75 ms | 254.21 ms | 7.68 ms | 5/5 (504) |

These are separate configurations and tracing modes, not overhead benchmarks.
The persisted comparison correctly returned `same_spec=false` and a fault
failure-rate delta of +1. Trace IDs were retained in the second report after
Jaeger was removed; no permanent trace storage is claimed.

An actual production CLI cancellation check observed an active toxic through
the project's internal control endpoint, then sent SIGTERM to that runner only.
The wrapper returned the expected exit 1, the emitted report was `canceled` with
cleanup confirmed, and 17 fault-phase observations were persisted under
`history-75f1b8e0de28c9a219a3b7602b7f2b40`. The record was queried after teardown;
no project containers remained. The runner/wrapper now allow 12 seconds to stop,
covering cleanup (5 s), export (2 s), persistence (3 s), and a margin.

Ignored local evidence includes `artifacts/m3-checks-final.log`,
`m3-disabled-final.log`, `m3-tracing-final.log`, `m3-cli-*.json`, and
`m3-signal-validation.log`. Databases and generated outputs are excluded from Git
and the Docker build context.

## Limits and regression scope

No unexpected test failures occurred. Negative cases intentionally exercise
export loss, read-only/aborted SQL writes, contention, cancellation, output failure,
and abrupt subprocess exit. Topology-specific tests are skipped only in the
inapplicable tracing mode; the subprocess fixture is skipped except in its child.

The engine, sample services, fault client, tracing instrumentation and semantic
trace validator were not modified. Legacy flag-based execution and schema-1 report
fields remain supported; persistence is opt-in for those flags. No new network
targets, services, hosted CI or paid infrastructure were introduced.

Unfinished intents have unknown outcomes and no per-request checkpoint. Final
writes may fail, leaving only the intent. Disk-full/physical I/O errors, power cuts,
Docker VM/storage failure, backups, retention and native/cross-platform build
matrices were not tested or implemented. SQLite durability still depends on the
filesystem and flush behavior. Jaeger remains transient. See [history](history.md)
and [ADR 0003](decisions/0003-specifications-and-sqlite-history.md).

# PR #2 focused review — raw-sample integrity

Date: 2026-10-24 (America/Los_Angeles). Docker Linux/ARM64, pinned Go 1.26.8,
mattn/go-sqlite3 1.14.52, real Toxiproxy and optional Jaeger. The starting local
and remote PR head matched `bb8fb2c05b3b5d555acac13ea6b7f0dcaad34169`.
Main remained `73e2430f7ebd356b641a9f8d97a96579c11b3b32`; PR #2 was open.

## Reproduction and correction

Before the fix, a real temporary SQLite store accepted finalization of a report
whose fault samples were 1 ms and 5 ms but whose mean was changed from 3 ms to
2 ms. `TestRejectsPlausibleMeanMismatch` failed with “accepted plausible but
incorrect mean: <nil>”. This confirmed the reported range-validation gap.
The same regression passes after the correction.

Finalization and reads now recompute every summary through the engine's existing
summarizer. Counts, failures, failure rates, min/max and nearest-rank p50/p95/p99
are exact; means allow four ULPs. Go JSON float64 roundtrips are lossless. The
tests cover tiny/large values, four/five-ULP boundaries, exact zero and nonfinite
arithmetic. All failed-request durations remain in the distribution.

The related stored fault-versus-baseline deltas were also unchecked. These now
match recomputed summaries, with the same arithmetic tolerance. Tests preserve
both valid cancellation boundaries before the first fault sample: uncomputed
zero deltas, or the engine's computed empty-fault deltas. Mixed alternatives
are rejected. Complete, failed, canceled, empty and partial reports pass JSON
roundtrips; no missing observations are filled in.

## Actual checks and results

- Focused `go test -race -count=1 -v ./internal/history` passed. Regressions alter
  each metric to a superficially plausible value, exercise mixed HTTP/transport/
  response-read failures and verify write rejection preserves the running intent.
  Raw SQL alteration of an already finalized report makes Get/List/Compare reject
  it as corrupt. CLI list/show/compare also return failure without JSON output.
- Formatting verification, `go mod verify`, `go vet ./...` and full
  `go test -race -count=1 ./...` passed in the pinned development container.
  Host Go was unavailable; these were actual Docker Go/race runs.
- `sh -n scripts/experiment.sh scripts/test-wrapper.sh`,
  `sh scripts/test-wrapper.sh`, and `git diff --check` passed.
- `./scripts/experiment.sh test` and `./scripts/experiment.sh --tracing test`
  both passed. Real CLI/history roundtrips, repeated specification execution,
  persisted cancellation after observing an active toxic, latency/recovery,
  HTTP timeout classification, owned-proxy cleanup and trace correlation passed.
  Unavailable-exporter cleanup passed in the disabled suite; the tracing-enabled
  suite exercised real export/query. Mode-specific skips were expected.
- Existing SQLite migration/rollback, read-only writes, aborting triggers,
  canceled operations, bounded writer contention and abrupt-process hot-journal
  recovery tests passed. The subprocess fixture skips in its parent process.
- The updated static scratch CLI successfully listed both pre-review stored
  complete runs, showed an existing report and compared those runs after their
  original stacks/collector had been removed. Original schema-1 report JSON and
  optional tracing behavior remain compatible; no database migration was added.

There were no unexpected test failures. The initial red reproduction was
intentional, as were negative-test exporter and SQLite error diagnostics. Ignored
logs are `artifacts/pr2-review-{reproduction,history,checks,disabled,tracing}.log`,
`pr2-review-shell.log`, and `pr2-review-existing-{list,show,compare}.{json,log}`.

## Cleanup and remaining limits

Engine execution/measurement, fault transport, tracing, transaction boundaries
and cleanup ordering were not changed. The only engine addition is a four-line
wrapper exposing its existing pure summarizer. Both real suites verified owned
fault removal and preservation of unrelated sentinel proxies. Wrapper teardown
removed their containers, networks and disposable image tags. The review-only
compiler image and build-cache volume were then removed. Docker inventories
confirmed no Faultline containers, networks, image tags or volumes remained.
Shared base images/build caches and existing local history artifacts were retained.

Validation detects inconsistent summaries, not coordinated tampering with both
samples and statistics. Existing process-failure/durability limitations still
apply: no power-cut, disk-full, physical I/O or cross-platform testing was added;
Jaeger remains transient. No cleanup guarantee after SIGKILL/daemon failure is
introduced. No CI, new service, product feature, new PR or merge is part of this
review.

# Milestone 4A validation — local reliability dashboard

Date: 2026-10-24 (America/Los_Angeles). Native macOS/ARM64 and Docker Linux/ARM64;
Go 1.26.8, Node 24.11.0, Chrome 156.0.8078.47, Playwright 1.64.1.

## Approved PR #2 merge

Immediately before merging, GitHub reported PR #2 OPEN and MERGEABLE at exactly
`602817280c7004711a66f8328a9a379f99235503`. Its base and remote main still matched
`73e2430f7ebd356b641a9f8d97a96579c11b3b32`. A regular merge with a required head
SHA produced `ed0552ca15d64a565142d670c9eea78960bbc8db`. GitHub reported MERGED.
The new main has the old main and approved feature head as its two parents.
All six individual commits remain reachable with the owner's verified noreply
attribution. Local main fast-forwarded cleanly before creating
`codex/reliability-dashboard`. No squash, rebase, force-push or author-date edits.

## Implemented boundary and integrity checks

The dashboard serves a static React/TypeScript/Vite build plus a GET-only `/api/v1`
API from one native loopback origin. No Go dependency, database migration, engine,
fault transport, tracing instrumentation or legacy command/report change was
needed. Typed API responses reuse history validation and comparison calculations.
Query connections now defer transaction begin without reserving the writer lock;
execution connections retain immediate writes and existing cleanup ordering.

Real temporary SQLite API/history tests passed for empty data, malformed/missing
IDs, corrupt summaries, unavailable stores, unknown/duplicate queries, cursor
ordering/ties, pagination, status/search filters, bounded limits, comparisons and
partial/canceled/unfinished records. SQL search injection strings remain literal.
Host/Origin/method/CSP and static-directory refusal tests passed. A real native
listener served HTTP then expired at its configured lifetime, with connection
refusal after shutdown. SQLite recovery/write-failure tests remain intact.

## Final actual checks

- `make check` passed natively using a checksum-verified repository-local Go SDK:
  shell checks, Go formatting, vet and full unit race tests. `go mod verify` passed.
- `make frontend-check` passed: clean `npm ci --ignore-scripts`, Prettier checks,
  eight Vitest tests in two files, strict TypeScript and production Vite build.
  Component cases include loading/error/empty, canceled partial reports, unfinished
  intents, missing trace references, cursor navigation, filters and numeric units.
  The final JS bundle was 249.56 kB (76.80 kB gzip); CSS 19.73 kB (5.44 kB gzip).
  These are bundle sizes, not measured browser performance/overhead claims.
- `./scripts/experiment.sh test` and `./scripts/experiment.sh --tracing test`
  both passed with race tests and package serialization. Real CLI/history
  roundtrips, repeated persisted experiments, active-fault cancellation,
  latency/recovery, timeout classification, owned-proxy cleanup, exporter-loss
  cleanup and real trace export/query correlation passed in their applicable
  mode. Both suites passed real hot-journal recovery. Only mode-specific cases
  and the parent-process crash fixture skipped.
- `FAULTLINE_TEST_URL=http://127.0.0.1:53446 npm --prefix web run test:browser`
  passed against the production Go server and newly generated experiment data.
  The single end-to-end Chrome test uses no intercepted/fake backend responses.
  It verifies API cursor pages, history filtering, exact displayed measurements
  and comparison deltas, keyboard point selection and skip navigation, actual
  tracing-enabled IDs, actual tracing-disabled missing IDs, and 390 px mobile
  overview/detail page overflow. It asserted no browser errors or external
  requests. Desktop screenshots used 1440 px width.
- The documented startup script completed a lockfile install/build and served
  the real history DB. A final `-ttl 1s` startup exited successfully; both that
  listener and the earlier browser-test server were confirmed closed. SIGTERM
  stopped the earlier owned server. No native dashboard process remains.
- List/detail/compare requests against all three fresh records left the SQLite
  file byte-for-byte unchanged (SHA-256 comparison); file mode remained 0600.
  The initial visual inspection also opened the prior Milestone 3 database.
  No permanent Jaeger availability is assumed by any view.

## Real data shown in the browser

Three new experiments ran through the existing isolated wrapper, into ignored
`artifacts/m4-history/history.sqlite`. All completed and confirmed fault cleanup.

| Injected latency / tracing | Samples per phase | Baseline mean | Fault mean | Recovery mean | Fault failures |
| --- | ---: | ---: | ---: | ---: | ---: |
| 80 ms / disabled | 10 | 0.63 ms | 89.71 ms | 0.49 ms | 0/10 |
| 400 ms / enabled | 5 | 0.82 ms | 253.55 ms | 0.43 ms | 5/5 (504) |
| 100 ms / disabled, legacy flags | 5 | 0.61 ms | 101.19 ms | 0.25 ms | 0/5 |

These are individual runs with different configurations, not controlled tracing
benchmarks or statistical significance evidence. The actual comparison of 80 ms
(left) and 400 ms (right) showed +163.84 ms fault mean, +134.47 ms fault p95 and
+100 percentage points of fault failures, with the differing specs visible.

Actual browser screenshots are local ignored artifacts:
`m4-overview.png`, `m4-detail.png`, `m4-comparison.png`, `m4-mobile.png` and
`m4-mobile-detail.png`. Logs include `m4-checks-final.log`, `m4-frontend-final.log`,
`m4-browser-final.log`, `m4-disabled-final.log`, `m4-tracing-final.log` and
`m4-startup-final.log`. Fresh original reports are `m4-run-{80,400,100}.json`.
Screenshots, reports, databases, toolchains and caches are not committed.

## Failures found and resolved

Type checking caught missing Vite CSS declarations and an icon import during the
view-module split; both were fixed. The initial contention regression relied on a
query transaction reserving a writer lock. After readers became deferred, that
test failed as expected; it now explicitly acquires a real SQLite write lock and
still proves bounded failure preserves the intent. The first full native `./...`
run also included the downloaded SDK's compiler test corpus under `artifacts/`.
Moving that SDK under a hidden artifacts directory excluded it from package
traversal; the full project checks then passed. The initial real disabled suite
failed on the old lock fixture; both complete real suites were rerun and passed.
No failing check remains. Negative exporter/SQLite diagnostics are intentional.

## Resources and limits

All wrapper-created containers, networks and disposable image tags were removed.
Docker inventories confirmed no Faultline containers, networks, image tags or
volumes remained. Cleanup tests preserved unrelated sentinel proxies. No global
prune/reset, Docker socket mount, published collector endpoint, CI or paid service
was used. Shared base images/build caches and ignored local evidence remain.

The native UI is IPv4-loopback only and trusts the local user/build assets. It is
not an authentication boundary against other local programs. Global counts describe
metadata and do not validate off-page report bodies. Large histories may hit the
query deadline; no large-history benchmark, full accessibility audit, Firefox/
Safari, Windows or Linux/AMD64 build/browser matrix was run. Native macOS/ARM64
and Docker Linux/ARM64 race checks passed. Existing power-loss/disk-full durability
limits remain. Traces remain transient. The dashboard is read-only; portable
reports and recorded environment provenance are recommended next.

# Milestone 5A validation — portable evidence and provenance

Date: 2026-10-25 (America/Los_Angeles). Native macOS/ARM64, Docker Linux/ARM64,
Go 1.26.8, Node 24.11.0 and Chrome/Playwright from the dashboard toolchain.

## Approved PR #3 merge

PR #3 was OPEN/MERGEABLE at exactly
`27cf15b7bbfc1954317c515d83f9e9a86f105757`; its base and remote main still matched
`ed0552ca15d64a565142d670c9eea78960bbc8db`. The SHA-gated regular merge produced
`bd3cc64d0224c0a7e4a70054c9484a5023f7d47e`. GitHub reported MERGED; its two
parents are that prior main and the approved head. All four feature commits and
verified GitHub noreply authors remain reachable. Local main fast-forwarded
cleanly before branching to `codex/portable-evidence`.

## Actual checks and corrected failures

- `make check cli offline` passed: shell/format checks, vet, full unit race tests,
  native SQLite exporter build and CGO-disabled offline verifier build.
  `go mod verify` passed; no Go/npm dependency was added.
- Actual temporary SQLite and filesystem tests passed for complete/failed/
  canceled exports, partial fault phases, failures before a run ID, unfinished
  refusal, immutable destinations/symlinks, context cancellation, failed writes,
  cleanup and stdout failure after publication. Exported records were inspected
  while their database path was temporarily absent.
- Archive tests passed for deterministic roundtrips, CRC/hash damage, altered
  statistics even after recalculating hashes, inconsistent identities, unsupported
  versions, duplicate/missing/unsafe entries, symlinks, compression, oversize
  payloads, excessive JSON depth, duplicate/case-colliding fields, prefixes,
  truncation and trailing data. The parser never extracts files.
- Provenance tests passed for field allowlists, bounded launch metadata, version
  checks, snapshot copying, unavailable launch data, missing/null tracing facts,
  unsupported fields and persisted process/spec correlation. Existing history
  integrity and real SQLite rollback/hot-journal recovery regressions passed.
- Final `./scripts/experiment.sh test` and `./scripts/experiment.sh --tracing test`
  both passed with real faults and race tests. Existing latency/recovery, timeout,
  active cancellation/cleanup, persisted CLI execution and real trace export/query
  tests passed in their applicable mode. Only mode-specific tests and the parent
  crash-helper fixture skipped.
- `make frontend-check` passed: clean lockfile install, formatting, nine tests,
  TypeScript and production build. Final Chrome regression passed against clean
  real history, including keyboard/mobile views and the evidence download.
  Downloaded bytes matched the API response, then independently verified with
  the standalone binary. Browser errors/external requests remained zero.
- Final parser fuzz smoke passed with two workers for 10 seconds: 185 executions
  and one additional interesting input, no panic. This is a smoke test, not a
  comprehensive security audit.

Resolved failures were test/harness issues: a privacy assertion matched “serial”
in the methodology name instead of checking field names; a canceled-export test
reused an existing successful export path; an initial scratch fixture was 0600
but owned by root instead of the unprivileged verifier user. These were corrected
and rerun. A dependency check also matched `database/sql/driver` as a substring;
exact package checks confirmed no `database/sql`, history or SQLite dependency.
The standard driver value interfaces are transitive only, not a database engine.
A review found omitted tracing metadata could otherwise look like `false`; the
field is now required inside a present environment record, with regressions.
No failing final check remains.

## Actual portable files

Initial validation exported two new runs, two prior Milestone 3 complete runs,
and the prior real SIGTERM-canceled run. All verified and inspected from copied
files in temporary directories with `PATH=/nonexistent`, without database or Docker
commands available. Legacy/canceled runs retained absent provenance and explicit
warnings; no current-machine facts were added.

The final measured runs were generated from clean implementation commit `27b3775`
into `artifacts/m5-clean/history.sqlite`, after all source changes were committed.
Each run completed and confirmed cleanup. Each was exported twice with identical
bytes, then independently inspected by the latest offline verifier.

| Latency / tracing | Samples | Baseline mean | Fault mean | Recovery mean | Fault failures | Bundle bytes |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 80 ms / disabled | 10 | 2.17 ms | 83.16 ms | 0.25 ms | 0/10 | 4,170 |
| 400 ms / enabled | 5 | 0.58 ms | 253.20 ms | 0.36 ms | 5/5 (504) | 5,162 |

These are separate individual experiments, not significance or overhead benchmarks.
Whole-file SHA-256 values recorded for these local validation artifacts:

- `clean-80.flb`: `bb6c3d4ca25d3db32b83f5cdd189027085fb61cab86d796e5dc0dde435ea8572`
- `clean-400.flb`: `e265b73a29f2deaa079045996aafc9cf064b05949193389c10319ceff07caea5`

Recorded facts include runner Linux/ARM64, Go 1.26.8, revision `27b3775`, canonical
spec hashes, `serial-http-nearest-rank-v1`, explicit tracing mode, Docker server
29.8.0, observed sample/Toxiproxy/Jaeger image identifiers and configured pinned
Toxiproxy 2.12.0 / Jaeger 2.22.0 references. No laptop OS, username, home path, IP,
ambient environment dump, serial number or token was collected as provenance.

The final Linux/ARM64 verifier was cross-built with CGO disabled and ran inside a
64 MiB, read-only scratch image with network disabled, no SQLite/database, shell,
Docker client/socket or collector. It inspected the clean 400 ms bundle successfully.
The temporary image was removed. Native macOS inspection also succeeded without
Docker invocation; dependency inspection excludes SQLite/history/database access,
and the binary links only standard OS libraries/frameworks, not SQLite.

## Evidence locations, compatibility and limits

Ignored outputs include `m5-checks-final.log`, `m5-frontend-final.log`,
`m5-disabled-final.log`, `m5-tracing-final.log`, `m5-browser-final.log`,
`m5-fuzz-final.log`, `m5-offline-final.json`, `m5-browser-final-verified.json`,
`m5-clean-evidence-summary.json`, original clean reports and `m5-bundles/*.flb` /
inspection JSON. These data, binaries, caches and scratch Docker context are not
committed. The portable format and commands are in [evidence](evidence.md).

SQLite remains schema 1 with no migration. Report schema 1 gains only documented
optional environment metadata; legacy reports stay unchanged. Shared validation
still uses the original summarizer and tolerance. Engine measurement, fault
transport, ownership/cleanup and tracing implementation remain unchanged. Query
recovery may still write SQLite journals; verification of an exported bundle has
no database access at all.

Checksums are not authenticity, execution attestation or protection from coordinated
rewrites. Full traces, images and source trees are not included. The supported ZIP
profile is intentionally narrow. No power-cut/disk-full or unusual-filesystem/
Windows publication tests were added; hard links/directory sync are required for
CLI publication. Archive fuzzing and browser/platform coverage are limited. All
these limits remain explicit rather than being filled with inferred evidence.

Final cleanup checked that no real export `.faultline-evidence-*.tmp` files,
Faultline containers, networks, temporary image tags or volumes remained. The
owned final dashboard server was stopped; prior review servers were replaced or
expired. Shared base images/BuildKit caches and ignored local evidence were retained.
No unrelated project cleanup, public deployment, CI or paid service was performed.

# Milestone 5B validation — offline evidence comparison

Date: 2026-10-25 (America/Los_Angeles). Native macOS/ARM64 and Docker Linux/ARM64,
pinned Go 1.26.8, Node 24.11.0; no new dependency or bundle format change.

## Approved PR #4 merge

PR #4 was OPEN/MERGEABLE at approved head
`89fa35ed2c6959ecd593e36af315c674d76094c4` and exact expected base
`bd3cc64d0224c0a7e4a70054c9484a5023f7d47e`. The SHA-gated regular merge produced
`34770c4387fc5bc29593b562633f3a60e16a49ea`, verified on GitHub as MERGED with
those two parents. All five original commits remained reachable with the owner's
verified GitHub noreply attribution. Local main synchronized cleanly before
creating `codex/offline-bundle-comparison`; no published history was rewritten.

## Implemented contract and actual unit checks

`faultline-bundle compare LEFT RIGHT` independently validates both archives with
the existing reader. The pure comparison module consumes validated logical
payloads, preserves actual side summaries and outputs version-1 JSON with ordered
configuration/provenance classifications, identity assessment and explicit units.
The full native CLI delegates `bundle compare` to the same command. Existing
verify/inspect JSON and bundle/report/SQLite versions remain supported.

The former identical absolute-delta formulas were extracted into one pure
`experiment.CompareSummaries` helper used by engine reports, record validation,
history and offline comparison. Existing sample summarization and phase lifecycle
were not changed. Relative arithmetic and comparability rules live only in the
new comparison module. It captures no current-machine facts and performs no I/O.

`make check cli offline` passed: shell/Go formatting, vet, full unit race suite,
exporter and CGO-disabled verifier builds. Focused tests passed for:

- Known-condition pairs, identical inputs, reversed order and byte-deterministic
  repeated JSON. Absolute differences reverse sign; relative values use the new
  left reference and therefore need not be simple negations.
- Different latency, timeout, sample count, revision, Go version, OS, architecture,
  Docker version, image ID/reference and tracing mode. Dirty/development labels
  downgrade classification; unknown provenance never becomes equal.
- Legacy recorded labels/explicit telemetry flags, absent metadata and null
  telemetry flags. No runner/Docker/methodology data is inferred from this machine.
- Failed/canceled full or partial reports, missing phases, unconfirmed cleanup,
  conflicting identities and unassigned run IDs. Ineligible pair deltas are null;
  measured zeros remain actual measurements rather than unavailable summaries.
- Zero references (including 0/0), positive-to-zero changes, fraction-to-percentage-
  point units and overflowing relative percentages. No nonfinite JSON is emitted.
- Unsupported methodology rejection plus the explicit rule withholding deltas
  for differing known methods. Current input support recognizes only methodology v1.
- Left and right malformed/corrupt/oversized inputs, missing files, usage errors,
  canceled contexts and failed output. Invalid input emits no comparison result.
- Near-limit report payloads with bounded summary-only comparison output, without
  copying large raw errors or observations into the result.

Existing archive safety/corruption/semantic tests passed. A two-worker, 10-second
archive fuzz smoke passed with 823 executions and two additional interesting
inputs, without a panic. This is bounded smoke coverage, not a full security audit.

## Actual re-exported experiment evidence

The end-to-end test re-exported four original persisted records into a private
owned temporary directory: the clean Milestone 5A 80 ms and 400 ms runs, one real
legacy run, and the real SIGTERM-canceled run. No input measurements were fabricated.
Those source databases and older source bundle artifacts were preserved.

The verifier binary and bundle copies were moved into an isolated temporary
directory and invoked with `PATH=/nonexistent`, without database/Docker commands.
Real, reverse, identical, legacy and canceled comparisons all passed. Repeated
outputs were byte-identical, the full-CLI alias matched the standalone result,
and SHA-256 checks confirmed inputs remained unchanged.

For the real left 80 ms / right 400 ms pair:

| Fault-phase metric | Signed right-minus-left | Relative change |
| --- | ---: | ---: |
| Mean request latency | +170.0357542 ms | +204.4640491% |
| p95 request latency | +170.689167 ms | +201.2132111% |
| Failure rate | +100 percentage points | null: zero reference |

Classification was `descriptive_only`: samples/latency/config hashes, tracing mode
and sample image identifiers differed. These deltas do not isolate causality or
establish statistical significance. Reversing the pair reversed absolute signs;
relative references changed as specified. Comparing the exact same recorded run
produced `recorded_conditions_match` with a `same_recorded_run` reason, not an
independent replication claim. The legacy pair remained `descriptive_only` with
missing facts unknown. The canceled pair was `not_comparable` with null deltas
and an unavailable recovery summary.

The actual input bundle SHA-256 values (re-export matched existing evidence):

- Left: `bb6c3d4ca25d3db32b83f5cdd189027085fb61cab86d796e5dc0dde435ea8572`
- Right: `e265b73a29f2deaa079045996aafc9cf064b05949193389c10319ceff07caea5`

## Offline isolation, resource bounds and regressions

The final CGO-disabled Linux/ARM64 binary ran in a read-only scratch image with
network disabled, 0.5 CPU, 64 MiB RAM, 32 PIDs, dropped capabilities and no SQLite,
database, Docker client/socket, shell or collector. It successfully compared both
the real files and two synthetic resource-only bundles with report payloads near
1 MiB each. Real output was 10,654 bytes; bounded-fixture output was 9,599 bytes.
Those synthetic failed/cleanup-unconfirmed inputs correctly had no deltas; they
were never presented as actual measured experiments. The owned image was removed.

Dependency inspection confirmed no history, `database/sql` connection package or
SQLite driver. Standard `database/sql/driver` value interfaces remain transitive
Go dependencies, not a database engine. Comparison has no runtime external service
or SQL access.

Both real wrapper suites passed: `./scripts/experiment.sh test` and
`./scripts/experiment.sh --tracing test`, including actual faults, latency/recovery,
timeouts, active cancellation/owned cleanup, trace export/query, persisted CLI
runs and SQLite journal recovery. Existing history/API regressions passed after
the shared-helper refactor. `make frontend-check` passed a clean install, format
checks, nine frontend tests, strict TypeScript and production build. Browser
regressions were not rerun: no frontend/API response behavior changed. The API's
comparison arithmetic/shape is covered by existing Go integration/unit checks.

All final checks passed without an implementation or test failure in this
milestone. Expected negative-test errors and mode-specific/crash-fixture skips
remain intentional. Logs/JSON evidence under ignored `artifacts/` include
`m5b-checks-final.log`, `m5b-frontend.log`, `m5b-disabled.log`, `m5b-tracing.log`,
`m5b-fuzz.log`, `m5b-real-validation.log`, comparison JSON variants and the two
scratch outputs. Generated test inputs/exports and scratch context are temporary;
comparison itself creates no files.

## Limits and release-readiness recommendation

Matching recorded conditions, identifiers or hashes establish neither authenticity,
independent execution, equal unrecorded environments, significance nor causality.
Short revision/image identifiers are compared literally. Missing optional sample
references and Jaeger-off metadata remain unknown; they are not required conditions.
Unknown methodologies are refused, not interpreted using an assumed algorithm.
Only existing schema-1 bundles and the supported methodology are accepted.

No new dashboard/API behavior, experiment target, fault type, collection, CI or
paid infrastructure was introduced. No new native listener was started. File/CPU
bounds and cancellation remain cooperative through normal local I/O; kernel/FS
stalls retain existing limitations. No comprehensive parser audit, Windows,
Linux/AMD64 or unusual-filesystem matrix was performed. Recommended next is a
release-readiness pass for platform-specific verifier artifacts, versioned JSON
contract fixtures, broader filesystem/platform tests and archive/parser review.

Final inventory found no Faultline containers, networks, image tags, volumes or
native dashboard servers. All newly created temporary export/bounds files and the
scratch context were removed. Preexisting Milestone 5A source bundles/databases
were preserved; comparison JSON/logs and shared build caches remain ignored local
artifacts. No unrelated Docker resources were pruned or modified.
