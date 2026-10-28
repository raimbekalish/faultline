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

## Milestone 6A — Release readiness (2026-10-25)

### Approved merge and candidate source

PR #5 was OPEN/MERGEABLE/CLEAN with approved head
`1f0bc04f0848eda1098d3670fca6be64e02b8f0d` and base
`34770c4387fc5bc29593b562633f3a60e16a49ea`. GitHub regular merge produced
`53d3d498203f420c7055c217857e26746a5043b0`, with exactly those parents. All three
feature commits remain reachable with the repository owner's noreply attribution.
Local main fast-forwarded to that remote SHA, clean. Work continued on
`codex/release-readiness`; no tag, release, CI or remote asset publication.

Final **local candidate** label: `0.1.0-rc.1`, executable/source revision
`b8d5efd56389757f7320387972e0f10cf1071ab5`. The subsequent documentation commit does
not change executables/assets. These are review artifacts, not the final tagged
release: rebuild from the approved merge revision after review. Prior candidate
attempts and repeated builds remain separate, never overwritten.

Host: macOS 26.6.2 ARM64, Go 1.26.8, Apple clang 21.0.0, Python 3.12.6, Node 24.11.0,
npm 11.6.1; local Docker Desktop ARM64 Linux VM and installed Chrome/Rosetta 2.
No Windows machine or physical Intel runtime was available. These validation facts
are not inserted into historical experiment provenance.

### Parser findings and negative tests

Reproduction log: `artifacts/m6-parser-reproduction.log`. The pre-fix implementation
accepted the adversarial cases below; the new rejection tests failed as intended:

- `status` / `ſtatus` and `s` / escaped long-s duplicate keys: encoding/json uses
  Unicode simple folding while the previous check only lowercased keys.
- Unpaired escaped high/low UTF-16 surrogates: encoding/json silently substituted
  replacement characters. Valid paired surrogates, emoji and literal escaped
  backslashes remain accepted; invalid UTF-8 remains rejected.
- Nonzero per-entry central-directory disk, conflicting/unsupported local/central
  reader versions, and contradictory local CRC/sizes when a descriptor exists.

The fixes retain valid format-1 exports and reject these malformed cases. Existing
regressions also reject duplicate/missing/traversal/absolute/symlink/compressed
entries, prefixes/trailers, CRC/digest corruption, rehashed inconsistent statistics,
excessive sizes/depth, invalid identities and unsupported versions. New actual
filesystem checks reject directories, FIFOs and dangling symlinks. Short reads and
mid-read cancellation fail; no extraction occurs. Focused race runs passed in
`m6-parser-fixed.log` / `m6-parser-final.log`.

This is a targeted review, not a comprehensive or independent security audit.
Filesystem syscalls remain cooperatively cancellable, not interruptible guarantees;
trusted local directories are required. No authenticity claim follows from CRC or
SHA-256. See SECURITY.md for limits.

### Stable public contracts

`internal/contracts/testdata` contains small explicitly **synthetic** fixtures;
they are not experimental measurements. Complete, legacy missing-provenance and
canceled/partially measured evidence freeze verify/inspect/compare output. The
complete fixture also freezes deterministic archive bytes via SHA-256. Unsupported
manifest and invalid/ambiguous JSON fixtures reject. API/history fixtures replay
through real temporary SQLite files, including pagination, partial detail,
comparison, missing-record errors and fraction-valued legacy comparison units.

Fixed fixture timestamps/IDs deliberately exercise identity, ordering and raw
report hashes. Whitespace is ignored for JSON comparisons; encoder field ordering
is frozen to make intentional deterministic CLI byte changes reviewable, not as a
claim that JSON object order has semantic meaning. No wall-clock paths/host facts
are snapped. Fixture changes require review, with no auto-update switch.

### Candidate inventory and reproducibility

Selected packages: `artifacts/release-candidates/m6a-final/`. Five archives, about
34 MiB total, plus outer SHA256SUMS. Each contains member SHA256SUMS, LICENSE,
USAGE.md and VERSION.json. Local package also has matching built assets/examples.
Only binaries, metadata/instructions and known static files appear in archives.
No database, evidence, credentials, test output or build cache is included.

| Filename | SHA-256 |
| --- | --- |
| `faultline-bundle_0.1.0-rc.1_b8d5efd56389_darwin_amd64.tar.gz` | `b9cf0156eceabd0f1dd7ac23ae2842475f7a198b081720771e2a2ab84ae0a1e4` |
| `faultline-bundle_0.1.0-rc.1_b8d5efd56389_darwin_arm64.tar.gz` | `0fdf3ff6bef5527226e9b94fd9620d1b5aa9e485370ea032647f26ee9823a1e5` |
| `faultline-bundle_0.1.0-rc.1_b8d5efd56389_linux_amd64.tar.gz` | `af109d3e2e84e725104b2159e48a73a1f0cb0ddba2584fe343a326460ce74dfa` |
| `faultline-bundle_0.1.0-rc.1_b8d5efd56389_linux_arm64.tar.gz` | `882b4db9b42bf611ee02b42dce076874261d27226d6b41f5c0c6163c9b02ff62` |
| `faultline-local_0.1.0-rc.1_b8d5efd56389_darwin_arm64.tar.gz` | `e1402c088310fb276dfd50fb423791392b462166b4a700a17b115ebf8ad97cfd` |

A second clean-source build into `m6a-final-repeat` produced identical complete
archive hashes for all five. This verifies repeatability on the recorded
compiler/SDK/toolchain, not independent cross-machine reproducibility. Packager
unit tests prove stable tar metadata/order, exclusive output, unsafe-path rejection
and cleanup on failed publication. Native binaries are unsigned/unnotarized; no
external-download Gatekeeper qualification or minimum-OS support promise.

### Actual installation and platform tests

Clean private directories, both checksum levels checked, instructions read:

| Target | Actual execution | Result |
| --- | --- | --- |
| macOS ARM64 offline/local | Native host; no build tools on PATH | PASS version, verify, inspect, deterministic compare; CLI real history |
| macOS AMD64 offline | Installed Rosetta 2 on ARM64 macOS | PASS version, verify, inspect, compare; no Intel hardware claim |
| Linux ARM64 offline | ARM64 Docker Linux VM, scratch | PASS version, verify, inspect, deterministic compare |
| Linux AMD64 offline | Docker Desktop AMD64 emulation, scratch | PASS same workflow; no bare-metal x86 claim |
| Windows AMD64 offline evaluation | Cross-build only; PE32+ inspected | BUILD ONLY, unsupported/unpackaged; evaluation binary removed |

Logs: `m6-native-smoke-final.log`, `m6-macos-amd64.log`, `m6-linux-smoke.log`,
`m6-windows.log`. Other native local package targets were not built/tested and are
not advertised. CGO-disabled dependency graph excludes database/sql, history and
the SQLite driver (`m6-offline-dependencies.txt`). Standard-library driver interfaces
may be transitive, but no database is opened by offline commands.

Linux images had no shell, SQLite, Docker client/socket, collector or external
service. Network was disabled; root filesystem read-only; user 65532, capabilities
removed, no-new-privileges, 0.5 CPU, 64 MiB and 32 PIDs. Real 80/400 ms exported
bundles were used. A separate clearly synthetic **resource-only** fixture with
1,048,542 report bytes and many unique JSON keys passed two-input comparison under
the same 64 MiB bound (`m6-bounds.log`); output was 9,834 bytes. This exercises a
near-limit path, not a universal peak-memory guarantee. Inputs and result semantics
were not used as new measured experimental evidence.

### Actual dashboard and source journey

The extracted local candidate ran native CLI queries and dashboard with no Go/C/Node
programs on its PATH, against a copy of real `artifacts/m5-clean/history.sqlite`.
Its health, static assets and real Chrome test passed. Chrome exercised overview,
filtering/pagination, raw trace references, actual measurement/delta formatting,
keyboard selection/download, comparison, responsive desktop/mobile layout, browser
errors and external requests. **1 browser test passed**; no external requests or
console/page errors. Screenshots are under `artifacts/m6-browser/`; the reviewed
README preview is an unmodified copy of that real overview, not a fixture render.
Original bundles/database hashes stayed unchanged; copied data and listener removed.

The complete source quick start also ran anew, after each isolated stack teardown:

| New real run | Baseline mean | Fault mean / p95 | Fault failures | Recovery mean | Cleanup |
| --- | --- | --- | --- | --- | --- |
| 80 ms, tracing off, 10 samples/phase | 0.473763 ms | 82.563208 / 84.823709 ms | 0/10 | 0.227875 ms | confirmed |
| 400 ms, tracing on, 5 samples/phase | 0.815392 ms | 253.854234 / 259.145417 ms | 5/5 HTTP 504 | 0.368842 ms | confirmed |

Both recorded revision `b8d5efd`, completed, with zero baseline/recovery failures.
New actual database: `artifacts/m6-real/history.sqlite`; raw stdout JSON:
`m6-real-80.json`, `m6-real-400.json`. Native candidate exported both finalized
records, then the offline tool verified/inspected/compared them without toolchain
programs. Export SHA-256 values:

- 80 ms: `e947348d5220293e63be892eaf53127969fdd8e47d9fe85eca1316dc34837361`
- 400 ms: `56ac03c6bcb5bc1c6a33753f46fd55c1b4034671ab5d4c8fc381288d76b9041d`

Fresh comparison: `descriptive_only`; fault right-minus-left mean +171.291026 ms,
p95 +174.321708 ms, failures +100 percentage points. Relative failure-rate change
is null/zero_reference. Configurations, sample counts and tracing differ; these
are end-to-end request observations, not statistical significance or an isolated
causal estimate. See `m6-fresh-evidence.log` and `m6-real/comparison.json`.
Actual new records/bundles are intentionally retained, separate from fixtures.

### Regression command results and failures corrected

- `make check`: PASS formatting, shell endpoint-selection checks, 2 packaging
  tests, vet and all Go unit race packages, including SQLite migrations, rollback,
  locks, failed/canceled writes, process-exit/hot-journal recovery, API and cleanup.
  Final log `m6-check-final.log`; `go mod verify`: all modules verified.
- `CGO_ENABLED=0 go test ./internal/bundle ./internal/bundlecli
  ./internal/bundlecompare ./internal/jsonstrict ./internal/contracts`: PASS.
- `./scripts/experiment.sh test`: PASS real nontracing integration race suite;
  `m6-integration-disabled.log`.
- `./scripts/experiment.sh --tracing test`: PASS real tracing integration race
  suite; `m6-integration-tracing.log`. Includes real export/query/topology/timeouts,
  active-fault cancellation and owned cleanup. Tests remain wrapper-isolated.
- `go test ./internal/bundle -run '^$' -fuzz FuzzReadNeverPanics -fuzztime=20s
  -parallel=2`: PASS, 397,810 executions. JSON `FuzzValidate` with the same bounds:
  PASS, 669,167 executions. Both elapsed about 21 s; no panics/failing inputs.
  New interesting corpus cases are coverage inputs, not vulnerabilities. Short
  fuzz smoke is not exhaustive security validation.
- `make frontend-check`: PASS locked install (audit reported 0 vulnerabilities),
  formatting, **9 tests**, TypeScript and Vite production build. This is not a
  comprehensive dependency vulnerability audit. Final log `m6-frontend-final.log`.
- Native and four-target package runtime checks, repeated-build hash comparison,
  real-data Chrome and network-disabled/resource tests: PASS as detailed above.

The first new Makefile check failed because the Python test received an accidental
shell-file argument; wiring was corrected before final checks. The first clean
install caught dashboard --version emitting JSON on stderr; stdout handling was
fixed in a separate commit and candidates rebuilt/retested. An initial prettier
command used the wrong working-directory path and was corrected. Expected pre-fix
adversarial failures are retained in the reproduction log. No unresolved test
failure remains; unrun native platform/Gatekeeper checks are explicit above.

### Cleanup and release recommendation

`m6-cleanup.log` confirms no Faultline test containers, networks, tagged images or
volumes, no native dashboard processes/listener and no owned extraction/build/
export temporary paths. Every wrapper and package test removed only owned resources;
no global prune, other-project action or prior evidence/database deletion.
Build caches/base images and intentionally retained candidates/evidence/logs remain.

**NO-GO for a first tagged release now**: this PR needs review/approval and candidate
packages must be rebuilt/reverified from the approved merge SHA before explicit
owner approval to tag/publish. No additional confirmed code blocker is known for
the narrowly documented platform scope. Native non-ARM64-mac local packages,
Windows, minimum OS versions, physical Intel qualification, signing/notarization
and independent-builder reproduction are not claimed. They need qualification
before expanding release support, not invented passing results.

## Final v0.1.0 gate — 2026-10-25 PDT / 2026-10-26 UTC

### Approved merge and immutable candidate source

PR #6 was OPEN/MERGEABLE/CLEAN at approved head
`19ebabd5697c92b4ffab071449fd83b5d176b796`, with exact approved base
`53d3d498203f420c7055c217857e26746a5043b0`. GitHub regular merge produced
`4a2cc225746115403205058d996f5392c30d3b51` at 2026-10-26T01:02:55Z; parent SHAs
match the approved base and head. All five individual feature commits and owner
attribution remain intact. Local main fast-forwarded cleanly and matches origin.

**All candidates and functional qualification in this gate use exactly
`4a2cc225746115403205058d996f5392c30d3b51`**, intended metadata version `0.1.0`,
not the earlier feature-branch candidate. No tag was needed or created. Authenticated
account and repository-local noreply identity were verified. All 30 feature commits
across PRs #1–#6 remain reachable, with owner attribution (7/6/4/5/3/5 respectively).
No existing local/remote `v0.1.0` tag or GitHub Release was found.

Application source was not changed. Release-facing documentation changes are on
`codex/v0.1.0-release-docs` for separate owner review; those unreviewed files were
not substituted into the qualified archives. No asset upload, Release, tag, CI,
paid service, deployment or unrelated-repository action was performed.

### Exact local candidate inventory

New output: `artifacts/release-candidates/v0.1.0-4a2cc225/`. Earlier candidates were
preserved. A repeat into `v0.1.0-4a2cc225-repeat/` produced byte-identical hashes for
all five complete archives on this toolchain. Every executable's metadata checked
where run: version 0.1.0, exact merge revision, target, Go 1.26.8 and CGO mode.

| Candidate asset | SHA-256 |
| --- | --- |
| `faultline-bundle_0.1.0_4a2cc2257461_darwin_amd64.tar.gz` | `2d74aba5bd26bb0a0975641fa48de209786550f621d361e0f839b530df79329d` |
| `faultline-bundle_0.1.0_4a2cc2257461_darwin_arm64.tar.gz` | `b5680ebc481ef3f745201a6227c32ea6b3574b279f307c401f2d26fdcb214949` |
| `faultline-bundle_0.1.0_4a2cc2257461_linux_amd64.tar.gz` | `ac2ba6c3f5bd869a6b1f0ce584b4ba1ab2a1c9bfee5f74e3faf79d0436e39760` |
| `faultline-bundle_0.1.0_4a2cc2257461_linux_arm64.tar.gz` | `c0989f5ad1a7c02c374f53f67efe68aec4199eae02800d0c1ec6eb048d5f0765` |
| `faultline-local_0.1.0_4a2cc2257461_darwin_arm64.tar.gz` | `cdec2a14bcbc9ab0b6e5b644d110db1d253a0f9410e62e4451a48e238b140145` |

The master SHA256SUMS lists all five. A **separate local-only** selection at
`artifacts/release-gate-v0.1.0-4a2cc225/primary-offline-assets/` hard-links just the
four offline archives and has a manifest listing exactly those four. The native
candidate remains withheld. This stages a proposed asset scope, not publication.
No archive bytes or prior manifests were changed. A later approved rebuild needs
new filenames/checksums/selection; these are not relabelable final release assets.

Audit checked outer and every member checksum; ordered regular-file whitelist,
modes and zero numeric ownership; exact source MIT license and package instructions;
matching locked production assets/spec examples; version/revision/target/CGO/lock
metadata. No database, `.flb`, report, environment file, cache or test output is in
an archive. Known credential patterns and private workspace/home path prefixes
were absent. These are targeted checks, not a comprehensive secret/binary audit.
`package-inventory.json` records archive and individual executable hashes.

### Platform and installation qualification

| Target | Actual runtime evidence | Native local package |
| --- | --- | --- |
| macOS ARM64 | Native macOS 26.6.2; clean extraction, real verify/inspect/compare | CLI/history/dashboard/Chrome passed locally; withhold by default |
| macOS AMD64 | Installed Rosetta 2 on ARM64 macOS; same offline workflow | Not offered/tested |
| Linux ARM64 | ARM64 Docker Linux VM, scratch, network disabled | Not offered/tested |
| Linux AMD64 | Docker Desktop AMD64 emulation; same isolation | Not offered/tested |
| Windows | No new candidate/runtime qualification | Unsupported, excluded |

Cross-compilation alone is not treated as runtime success. AMD64 tests are
explicitly emulated, not native Intel hardware. Full platform host orchestration
was run on macOS with Docker's ARM64 Linux VM; this does not qualify every Linux
host/distribution. No minimum-OS runtime sweep was performed.

All four offline packages ran actual newly exported 80/400 ms evidence. The
comparison JSON was **byte-identical across all four target environments**: 10,629
bytes, SHA-256 `55ba088a53b6a7a359c6e48f4a1568513e44652f17b9a8fae76d58c28f4b65e3`.
Rosetta checks additionally covered identity/reversed pairs, actual legacy/canceled
bundles, truncated archives and oversized inputs; invalid cases exit 1 with no
success JSON. Valid canceled evidence remains inspectable/non-comparable.

Native packages were extracted in clean private directories; outer/member hashes,
license and usage were read. PATH lacked Docker, SQLite CLI and build tools. Native
history read a copy of the new real database. Dashboard health/assets and **one
real-data Chrome test** passed: overview, pagination/filtering, exact measurements,
raw outcomes/traces, comparison, evidence download, keyboard and mobile layouts;
no browser errors/external requests. Original real database and bundles were not
modified. Screenshots are under the gate's `browser/`, without replacing older ones.

Linux scratch ran as user 65532, no network, read-only root, 0.5 CPU, 64 MiB, 32 PIDs,
no capabilities and no-new-privileges. It had no SQLite, Docker client/socket,
shell or collector. A separate **synthetic resource-only** many-key report of
1,048,547 bytes passed two-input comparison under 64 MiB; output was 9,858 bytes.
It is not a new measured experiment or a universal peak-memory guarantee.

A trusted-source fallback was also tested from a fresh tracked-source export of
the exact merge: CGO=0 build, explicit version/revision, correct --version and real
bundle verification, using cached modules with the module proxy disabled. No
security policy was changed. This does not claim first-time builds need no network.
The offline dependency graph still excludes database/sql, history and the SQLite
driver. Full injection continues to need source/Docker/Compose/the isolated wrapper.

### macOS policy findings and release-copy blockers

`codesign` shows ARM64 binaries use linker **ad-hoc** signatures, with no Team ID;
AMD64 offline is unsigned. Ad-hoc verification succeeded on ARM64; AMD64 signature
verification reports no signature. None is Developer ID signed or notarized.
`spctl --assess --type execute` **rejected every assessed executable**, exit 3.
These are recorded distribution limitations, not passing Gatekeeper checks. No
policy or quarantine settings were changed, and no external-download/quarantined
execution was attempted. Local unquarantined success is not a download UX guarantee.

Mach-O load inspection: offline macOS executables embed minimum/SDK 12.0; native
CGO CLI/dashboard embed **minimum 26.0 / SDK 26.5**. These are binary build facts,
not evidence that old macOS versions were tested or supported. The narrow native
OS qualification and unsigned UX justify withholding the native archive initially.

The source's package USAGE.md says "not a published/signed release" and places its
macOS warning only under the local-package section. That would be misleading when
published and hides the offline macOS restriction. This documentation PR makes
usage publication-neutral and warnings universal. It also clarifies that a GitHub
profile is not a private contact channel. Read-only GitHub API verification found
private vulnerability reporting disabled (`enabled:false`); nothing was enabled.
No dedicated private channel/SLA is advertised. README has no fabricated live
release/download link. Draft notes, changelog and installation/demo are explicitly
pre-publication. Corrected instructions must be reviewed and rebuilt into packages,
not patched into/re-labeled over the qualified archive bytes.

### Confirmed third-party notice omission

The qualified archives contain Faultline's MIT LICENSE but no third-party license
packet. The Go SDK LICENSE explicitly requires reproducing its copyright/conditions/
disclaimer in binary-distribution materials. Native SQLite and production React
have their own MIT notices, and the runtime Go module graph includes other
licenses/notices. A correct project MIT license does not replace those texts.
This is an additional **publication blocker**, not a functional-test failure.

The documentation PR appends unmodified notices to USAGE.md, which the existing
approved packager already copies into every archive. No application or packaging
code changes are needed. Sources: Go 1.26.8 root/vendor license files, all **23**
modules in the actual CLI/dashboard/offline runtime package graph (including gRPC
NOTICE.txt), and React/react-dom/scheduler production licenses. **32 source files,
13 distinct complete texts**, deduplicated by exact SHA-256, are represented in
65,610 bytes of package instructions including the appendix. All source texts and
identifiers were checked against the copied bytes; no private paths were included.
This is scoped license-source coverage, not a comprehensive legal/compliance audit;
review again on dependency, toolchain or release-scope changes.

An initial over-broad `go list -m all` discovery tried unused/test-module metadata
lookups and failed under sandbox DNS restrictions. The successful inventory used
`GOPROXY=off go list -deps -json` for the actual runtime commands, entirely from
already available checked modules, without changing dependencies. This discovery
failure did not affect the functional/module verification results. No download or
external legal/compliance service was added. Logs/index: `notices.log`,
`runtime-packages.jsonstream`, `notice-sources.json`. Current candidate archives are
unchanged and still lack this appendix: review, rebuild and inspect the new
approved packages before **any** distribution.

### Fresh actual experiment evidence

Both real wrapper-isolated runs recorded clean revision `4a2cc22`, status complete,
confirmed cleanup, and zero baseline/recovery failures:

| Configuration | Baseline mean | Fault mean / p95 | Fault outcomes | Recovery mean |
| --- | --- | --- | --- | --- |
| 80 ms, 10 samples/phase, tracing off | 0.290204 ms | 82.613667 / 84.598459 ms | 0/10 failures | 0.203417 ms |
| 400 ms, 5 samples/phase, tracing on | 1.321592 ms | 252.369658 / 255.431250 ms | 5/5 HTTP504 | 0.371183 ms |

The candidate native CLI exported both, then offline tools verified and compared
without database/collector access. New actual evidence is intentionally retained
under `artifacts/release-gate-v0.1.0-4a2cc225/real/`, separate from synthetic fixtures.
Bundle SHA-256:

- 80 ms `run-80.flb`: `2c185567a0505fe6ba82e0ea004f0af121051fd1f2201c92199088c4d4c8e4c0`
- 400 ms `run-400.flb`: `9e267c2879e165416b68b5f4f03c19cb4710d8ed7d0031cece2b7b84fc3333c5`

Fault right-minus-left: mean +169.755992 ms, p95 +170.832791 ms, failures +100
percentage points; relative failure change null/zero_reference. Correct assessment:
`descriptive_only`, with differing configuration/sample counts/tracing. These are
observations, not isolation of causality or statistical significance. Trace IDs
are retained references, not permanent Jaeger traces.

### Commands, results and remaining gates

Logs are under `artifacts/release-gate-v0.1.0-4a2cc225/`:

- `make check`: PASS Go formatting, shell checks, 2 packager tests, vet and full
  uncached unit race suite. SQLite migration/rollback/lock/cancellation/failed-write/
  process-exit/hot-journal recovery and API contracts passed. `go mod verify`: PASS.
- CGO-disabled `go test -count=1 ./internal/bundle ./internal/bundlecli
  ./internal/bundlecompare ./internal/jsonstrict ./internal/contracts`: PASS.
  Format-1 synthetic compatibility snapshots/bytes remain unchanged.
- `./scripts/experiment.sh test` and `--tracing test`: both PASS real integration
  race suites with automatic owned teardown; timeout/propagation/export/cancellation/
  cleanup behavior remains unchanged. No engine/fault/tracing source modification.
- Bounded fuzz, 20 seconds requested / two workers each: ZIP **298,963** executions;
  JSON **364,957** executions; both PASS, elapsed about 21 seconds each. No failures;
  new coverage inputs are not vulnerabilities. Not a comprehensive security audit.
- `make frontend-check`: PASS locked install (audit reports 0 vulnerabilities),
  formatting, **9 component/format tests**, TypeScript and Vite production build.
  Chrome **1 test passed** against the packaged server and new real history.
- Five exact candidate builds, repeated hashes, content/version/license audits,
  four runtime targets, no-network/resource tests and source fallback: PASS.
  macOS trust assessment is **rejected**, explicitly not a functional-test pass.

Cleanup inventory found no Faultline test containers, networks, tagged images or
volumes, no native dashboard processes/listener and no owned extraction/build/export
temporary paths. No global prune or deletion of previous evidence/databases.
Candidates, primary-selection hard links, actual new evidence, logs/screenshots and
normal build caches remain intentionally. No application source change was needed.

**NO-GO to publish the current candidate set.** Required gates: owner review/merge
of the release-copy and third-party-notice documentation; fresh build/reverification from that approved
SHA with updated manifests; explicit acceptance of unsigned offline macOS UX (or
a separately qualified signing/asset-scope decision); final version-uniqueness
check and explicit owner authorization to tag/publish. Prefer four offline tools;
withhold the native package by default. No other confirmed application-code
blocker was found within the narrow documented runtime scope.

## Milestone 7 — offline reliability gates (Unreleased)

Date: 2026-10-27. Branch: `codex/offline-reliability-gates`, based on reviewed
main `da65e5a14e88ffc181a115dd30e77483685c3772`. This is source-only work;
`v0.1.0` does not gain `evaluate`. No release/tag/assets, dependency, engine,
format-1 encoding, original compatibility fixture, database schema or experiment
specification changed. Task outputs/logs are retained separately under ignored
`artifacts/m7-gates-023cb538/`; original experiments were not rerun for nicer data.

### Genuine evidence replay

Reused byte-identical, previously exported format-1 bundles, independently
validated by the new CGO-disabled CLI before each evaluation:

| Recorded experiment | Real run ID | Observations and cleanup |
| --- | --- | --- |
| 80 ms, tracing off | `faultline-ba5331968a02ffc39a5719035c06565e` | Complete; 10 per phase; confirmed cleanup |
| 400 ms, tracing on | `faultline-2f70fc38cdcc1a42744ba40f71a5c5ee` | Complete; 5 per phase; confirmed cleanup |
| SIGTERM during a fault | `faultline-f2a0c543e9bf04dc3aec78223ae19aec` | Canceled; baseline 100, fault 17, recovery 0 of 100 configured; confirmed cleanup |

The completed originals are `artifacts/demo-video/data/latency-{80,400}.flb`.
The canceled original is `artifacts/m5-bundles/canceled-0.flb`, whose actual
SIGTERM/persistence provenance is recorded earlier in this document under
Milestone 5A (history `history-75f1b8e0de28c9a219a3b7602b7f2b40`). It is not a
synthetically truncated completed run. Input hashes and exact commands/results
are retained in `real-e2e.json` under the task directory. Original SHA-256 values:

- 80 ms: `8e28262efcef42e1dc6524b0a91b6a958bdffc0c7de9a052b715c5b34a7399d1`
- 400 ms: `9e1589b8805c2bfdfef3c577c66ba33ad19dbd8ff4f0075e7346c4a8c918178e`
- Canceled: `0b624807e76194bbd35e8f263b90e75d95a140b5a3ebc79786fffbbda0859df4`

| Evidence / explicit example policy | Actual status | Exit | Recorded fault evidence |
| --- | --- | ---: | --- |
| 80 ms / dependency budget | PASS | 0 | Mean 86.57662090000001 ms; p95 97.238375 ms; failure fraction 0 |
| 400 ms / dependency budget | FAIL | 3 | Mean 256.6229166 ms >150; p95 260.273583 ms >200; fraction 1 >0 (5 HTTP 504) |
| 400 ms / expected timeouts | PASS | 0 | Fault fraction 1 ≥0.9; baseline/recovery successful within the selected limits |
| 80 ms / expected timeouts | FAIL | 3 | Fault fraction 0 <0.9 |
| Canceled / dependency budget | INDETERMINATE | 4 | Run incomplete; fault partial; recovery metrics null, count 0; cleanup true |

All five valid verdicts wrote one JSON object to stdout and empty stderr.
Actual values in the result come from the existing engine summarizer after
validation. Baseline and recovery failure fractions were 0 in both completed
recordings. These distinct policies ask different questions, not statistical
significance, isolated causality or SLO compliance. No old Jaeger trace storage is
implied. No measurements, statuses or original evidence bytes were edited.

### Executed checks

- Host Go 1.26.8 on native macOS ARM64: `make check` and `go mod verify` PASS.
  Includes shell syntax/endpoint precedence, two packaging tests, gofmt, vet and
  full uncached unit race tests. SQLite migrations, rollback, bounded contention,
  failed/canceled writes, abrupt-process/hot-journal recovery and API/history
  contracts passed. The first sandboxed run could not bind httptest loopback
  listeners; the authorized rerun passed. This was an environment restriction,
  not a hidden passing result.
- New tests cover all three verdicts; failed/canceled/partial and pre-engine
  reports; unconfirmed cleanup; null versus zero; minimum-count sufficiency;
  inclusive exact thresholds/adjacent floats; canonical summarizer versus allowed
  stored-mean roundoff; revalidation and deterministic encoding. CLI tests cover
  0/3/4 versus 1/2, stderr/stdout, real ZIP corruption, invalid inputs, canceled
  contexts, broken/short output and unchanged input files/no created state.
- Policy boundary tests cover unknown/case-aliased/escaped and simple-fold
  duplicate fields, malformed UTF-8/surrogates, nulls, unsupported versions,
  contradictory rates, range/type/overflow/underflow errors, exact 8 KiB versus
  excess input, bounded readers, read failures and cancellation. Actual temporary
  regular files, final symlinks and FIFOs exercise local filesystem behavior.
- CGO-disabled tests PASS for bundleevaluate, bundlecli, bundlecompare, bundle,
  jsonstrict, contracts and buildinfo; offline builds PASS for Darwin and Linux,
  ARM64 and AMD64. Offline dependency listing contains no history, database/sql
  or SQLite driver. Focused race coverage: evaluator 93.8%, CLI 83.6% of statements;
  coverage is not a correctness/security guarantee.
- Existing format-1 compatibility contracts PASS unchanged. Four new explicitly
  **synthetic** reviewed evaluation contracts freeze PASS, FAIL, canceled/null
  and legacy/no-provenance output plus 0/3/4 codes. Tests never regenerate them.
- Bounded fuzz smoke checks PASS, two workers and 15 seconds requested per target:
  policy 134,396 executions; ZIP 2,891; strict JSON 34,256 (about 16 seconds each).
  No confirmed parser bug/crash found; this is not a comprehensive security audit.
- Frontend formatting, all 9 component/format tests, TypeScript and Vite production
  build PASS. No frontend/API behavior changed; browser tests were not rerun.
- `./scripts/experiment.sh test` and `./scripts/experiment.sh --tracing test`:
  both PASS full serialized unit/integration race suites against real Toxiproxy,
  sample services and, in the latter, Jaeger. The no-collector export-failure
  test passed with tracing off; the real Jaeger export/correlation test passed
  with tracing on (each skips its inapplicable opposite topology). Real active
  cancellation, cleanup/other-owner preservation and persisted canceled/repeated
  runs passed. Fresh regression fault means were 83.55/251.48 ms without tracing
  and 84.59/254.81 ms with tracing for 80/400 ms injection; 400 ms produced 5/5
  failures, with successful recovery. These are separate test runs, not an
  overhead benchmark or replacements for the retained evidence above. Docker
  Desktop was initially stopped; it was started before the successful suites.

### Independent offline execution and compatibility

`runtime-check.py` in the task directory executed **14 cases on each of four
runtime targets**: five evaluation verdict cases, verify/inspect on each of the
three real bundles, and three comparison pairs (different, identical, canceled).
All 56 results had expected exits, empty stderr, valid JSON and **byte-identical
stdout across targets**:

| Target | Actual runtime qualification |
| --- | --- |
| macOS ARM64 | Native on this Mac |
| macOS AMD64 | Rosetta 2 on this Mac; no physical Intel qualification |
| Linux ARM64 | Docker Desktop Linux ARM64 virtual machine |
| Linux AMD64 | Docker emulation in that VM; no physical Intel qualification |

Each Linux binary ran in a scratch image with only the binary, copied bundles and
example policies: no database, shell/toolchain, Docker client or collector.
Containers were non-root, read-only, network `none`, all capabilities dropped,
no-new-privileges, 64 MiB memory, 0.5 CPU and 32 PIDs. They had no published ports,
mounts or external access. This proves these offline workflows ran without network,
not absence of networking code in Go dependencies. Runtime contexts, named images
and containers were removed in `finally` cleanup. Detailed bytes/hashes are in
`runtime/RESULTS.json` and per-target JSON files.

The nine original-command results also matched the actual released v0.1.0 macOS
ARM64 executable byte for byte. Its trusted existing archive was opened read-only,
only the verifier extracted into the task directory, and that temporary executable
removed after comparison. No old command output/exit semantics changed. Windows
remains unsupported; these tests add no macOS signing/notarization qualification.

### Review limits

The fixed schema has at most 17 checks and an 8 KiB policy cap. No expressions,
remote configuration, includes, environment capture, dependencies, API expansion
or new persistence were added. Evaluation is pure; existing archive/record/JSON
validators and the engine summarizer remain the only evidence/statistics path.
Policy input on Darwin/Linux uses no-follow/nonblocking open and regular inode
identity checks. Parent directories must be trusted; concurrent in-place writes
are not an attested snapshot. Cancellation is cooperative and cannot forcibly
interrupt kernel filesystem stalls or arbitrary output writers. Use stable local
inputs. Checksums/policy identities are consistency references, not authentication.

Final inventory: container and network IDs match the pre-suite inventory; no
owned wrapper/runtime image tags, Faultline volumes, native Faultline listeners
or task temporary paths remain. Shared base images/build caches and all previous
evidence/databases remain. No global cleanup was used. The 44 protected original
fixture/dependency/release-package/demo files and three replay source bundles
remain byte-identical; the README's public video URL is unchanged. Remote main,
the v0.1.0 annotated tag, release body/metadata and its five asset identities,
sizes and digests are unchanged. No release mutation was performed.

No confirmed merge blocker was found in this scoped review. Owner review/merge
is still required; the feature remains Unreleased. Policy design/thresholds are
the user's responsibility, and evaluation provides no confidence intervals,
cross-run aggregation or guarantee about future traffic.

## Milestone 8B1 — strict contracts and read-only planning (Unreleased)

Date: 2026-10-27. PR #10 merged with regular merge commit
`1374c8c55bcb83c2b6b80d34af88a3f663c9467a`, parents
`257eab735c16e9daa2a700d88e329f84c72622af` and
`00286f37c613b480616f3261424c40c2227f1f87`; both reviewed documentation commits
remain reachable with original authorship. Local main was synchronized/clean
before creating `codex/custom-service-planner`.

M8B1 adds only descriptor/catalog/plan generation. There is no custom run,
cleanup, worker, consumed-plan record, resource journal, Docker provisioning or
network qualification. Source/CLI behavior and private/public plan boundaries
are specified in [the planner contract](custom-service-planner.md). The plan's
random identity is for future single-use RUN consumption; journal-bound cleanup
may later reuse that consumed identity without restoring RUN permission. Neither
consumer is implemented or implicitly authorized by generating a hash.

### Tests actually executed

Task records are kept separately under private ignored
`artifacts/m8b1-planner-a13c7e92/`.

- Host Go 1.26.8/macOS ARM64: `make check` PASS (shell endpoint precedence,
  packaging tests, Go formatting/vet and full uncached unit race suite).
  `go mod verify` PASS. Existing SQLite migration/write/cancellation/process-exit
  recovery, sample engine, tracing, API and format-1/evaluation contracts passed.
- `CGO_ENABLED=0 go test -count=1` for lab, bundle, bundlecli, bundlecompare,
  bundleevaluate, jsonstrict and contracts: PASS. Native CLI and CGO-disabled
  offline executable built under the task directory; no release package rebuilt.
- New race tests cover valid/invalid descriptors, exact keys, Unicode/duplicates,
  versions/platforms, integer/range/size limits, canonical configuration hashes,
  distinct plan identities, catalog mutation/revalidation and private/public
  redaction. The plan codec rejects ignored/ambiguous fields and noncanonical
  material; cancellation after a full entropy read is checked explicitly.
  Real temporary files exercise final symlinks/FIFOs, inode replacement,
  directory privacy, atomic no-replace races, output limits and cancellation.
- Recording command tests exercise missing images, malformed/conflicting runtime
  and image responses, changed socket/daemon identity, endpoint precedence and
  invalid command/flag injection. Read-only operation construction is closed;
  unknown operations cannot reach a subprocess. Subprocess tests verify bounded
  stdout/stderr, killed process groups, cancellation and sanitized environment.
- Full native CLI dispatch through the actual subprocess/filesystem adapters,
  using a recording **synthetic Docker executable**, successfully wrote a private
  mode-0600 plan and redacted PLANNED/unqualified JSON. Exactly eight read-only
  fixture invocations; run/cleanup returned usage 2 without another invocation.
  These metadata responses are synthetic, not image/runtime qualification.
- Bounded descriptor fuzz smoke: two workers, 15 seconds requested, 258,168
  executions, PASS (about 16 seconds elapsed). Not a comprehensive security audit.
- Fourteen retained real `.flb` replays (five evaluate verdict cases and nine
  verify/inspect/compare cases) matched the earlier reviewed JSON byte for byte,
  with original 0/3/4 evaluation exits and unchanged source bundle hashes.

The first focused run exposed two test-fixture assumptions: Go's per-test child
folder was not mode 0700, and its long name exceeded macOS Unix-socket path limits.
Fixtures now explicitly create private/short paths; production privacy checks
were not relaxed. Subsequent focused and full suites passed.

### Real local read-only checks and findings

The existing local Docker daemon was available. It was not explicitly started or
provisioned. Production planner calls were recorded through a guard that accepted
only context/version/identity/image inspection; no create/run/start/exec/network
mutation/build/pull/tag/remove was permitted or invoked.

The initial live inspection found a real compatibility bug: Docker omitted empty
`Config.Volumes`/other optional map keys, while the template assumed their presence.
That caused an inspection error, not unsafe execution. Fixed templates now handle
absent/null counts explicitly. Added regression checks also ensure a declared
empty healthcheck object is rejected rather than treated as absent. Full checks
passed after those fixes.

Actual negative planner cases then behaved correctly: a nonexistent immutable
local image failed with exit 1 and no plan/stdout; the already-local proxy image,
intentionally unsuitable as an app, failed metadata validation with exit 1. The
production adapter made eight total allowed read-only calls across these cases.
Error text excluded endpoint paths and private binding data. The closed
`faultline-lab:http-work-v1` catalog image is absent; it was **not built, pulled or
substituted**. A successful plan against a real independent app/catalog pair was
therefore **not demonstrated**. Successful end-to-end publication uses the clearly
labeled recording fixture above. No application was started to satisfy planning.

Inventory result is deliberately qualified: container, image, volume and unrelated
network IDs were unchanged, with no Faultline lab resources created. Docker's
**default `bridge` network ID was replaced** during the longer observation and
again across the live check window. The saved prior/current records identify the
changed network as that default bridge; cause was not established. Global
inventory equality assertions failed and are retained as such, not reported as a
pass or repaired by cleanup. All recorded planner commands were read-only; no
network-create command was issued. This observation provides no network isolation
qualification and must be considered in later runtime work.

### Compatibility, scope and checks not run

No sample engine, experiment spec, tracing implementation, SQLite migration,
format-1 bundle, gate semantics, dependencies or release scripts changed. Protected
original fixtures/packages/demo files and genuine replay evidence remain unchanged;
README retains the same public video URL. v0.1.0 tag/release/five assets are not
modified. No external publication, CI, image provisioning or host policy change.

Real fault/tracing Docker integration suites were **not run**: they create
containers/networks, which this milestone explicitly forbids. Existing host
regressions ran instead. No frontend/browser tests, custom isolation probes,
application execution, native Intel/Linux runtime qualification or custom recovery
qualification was performed. All such claims remain unsupported, and every saved
plan remains PLANNED / NOT_PERFORMED with execution unavailable.

Security review checked the entire new execution boundary: only fixed read
commands, exact local selection, bounded data/subprocesses, strict file publication
and redaction. Trusted parent directories, Docker CLI/config/daemon and operator
image review remain requirements. Unix sockets, labels and checksums do not
provide authentication. Future RUN must perform new qualification and durable
single-use consumption; a private plan is not deletion authority or a safety
certificate. M8B2 is not started by this work.

## M8B2A — Non-mutating supervisor foundation (2026-10-27 PDT)

PR #11 was checked OPEN/MERGEABLE with exact head
`691abd711c618d33b821622f5628214b26353942`, base
`1374c8c55bcb83c2b6b80d34af88a3f663c9467a`, eight identical reviewed commits and
27 identical reviewed files. Release/tag/protected evidence matched the prior
baseline and the working tree was clean. Owner CLI regular merge produced
`053fc73676cb2116be1ecb2ce85ab7d994770738` with those exact base/head parents.
GitHub reports merged; all eight original commits/authors are preserved, and local
main fast-forwarded to origin/main before the new feature branch was created.

Implementation scope: pure closed resource graph and strict journal event replay,
exact identity/runtime/ownership decisions, private bounded atomic snapshot store
and recording-only failure tests. The only existing production CLI change is
adding `lab` to top-level usage. No RUN/CLEANUP/worker, Docker mutation transport,
provisioning, catalog image build, custom app execution or qualification is enabled.
See the [foundation contract](design/custom-service-supervisor.md).

### Actual checks

- Host Go 1.26.8/macOS ARM64: `make check` PASS, including wrapper endpoint tests,
  two Python packaging tests, formatting, vet and the full uncached unit race suite.
  Existing SQLite migration/process-exit recovery, engine/tracing, dashboard API,
  schema-1 bundle/comparison/gate compatibility tests passed.
- `go mod verify` PASS. CGO-disabled tests for lab, bundle, bundlecli, bundlecompare,
  bundleevaluate and contracts PASS. Native CLI and CGO-disabled offline executable
  built in the new ignored task directory, without rebuilding release assets.
- Focused race suites PASS for graph/authorization/journal/filesystem/CLI behavior.
  They cover invalid topology/security/image policy, replay and altered binding,
  unique/foreign/missing resource proofs, incomplete creation, worker interruption,
  unknown proxy cleanup, exact-ID cleanup retry and conservative network ordering.
- Real temporary-file tests PASS: owned/private canonical directories, no final
  symlinks/FIFOs/hard links/public files, corruption/gaps, stale writers, bounded
  revisions, cancellation/lock wait and post-publication error/replay. An actual
  test child exited while holding the advisory guard after writing a torn temporary
  file. Parent recovery reacquired the lock and retained unresolved creation;
  it did not invent success, repair committed history or delete recovery evidence.
- Recording-only transport tests exercise creation failure/lost response at every
  resource stage, cancellation at every creation/worker intent, cleanup response
  loss/retry and refusal of foreign IDs. Journal write failure prevents the next
  synthetic create while retaining exact prior ownership for independent cleanup;
  uncommitted cleanup cannot produce a final receipt. These are synthetic outcomes,
  not actual Docker creation/removal or isolation evidence.
- Fifty repeated concurrent consumption/stale-update race-test runs PASS after
  the guard creation fix. Exactly one consumer wins; completed/failed/uncertain
  consumption cannot be replayed. Final focused suite passed after the additional
  retained-ownership regression.
- Strict journal fuzz smoke PASS: 15 seconds requested, two workers, 772 executions
  reported, approximately 16 seconds elapsed, including a valid generated seed.
  This short run is not a comprehensive parser/security audit.
- Fourteen retained genuine 80 ms, 400 ms and canceled-bundle CLI cases produced
  byte-identical verify/inspect/compare/evaluate JSON with unchanged exits, including
  PASS/FAIL/INDETERMINATE 0/3/4. Original bundles were not modified.

Initial checks exposed environment and test issues accurately retained in logs:
Go was not on the default PATH; the existing repository toolchain was selected.
Sandboxed Unix socket fixtures were denied and the default symlinked temporary
path broke an existing path assertion; host-qualified runs used canonical private
`/private/tmp` test directories without relaxing product checks. A synthetic
cleanup helper initially tried to remove networks with ambiguous container
attachments; the state machine correctly refused, and the helper was corrected.

Repeated concurrency testing found a real guard-open race: simultaneous opens with
creation sometimes returned ENOENT on this macOS host. Exclusive create followed
by validated existing-file open (without create/truncate), bounded absent-file
retry and interrupted-flock retry passed 50 repeated runs. Guard symlinks cannot
cause creation of their missing targets. No chmod/unlink/overwrite workaround or
weaker ownership validation was added.

### Read-only Docker diagnostics and unresolved observations

The [sanitized follow-up](security/docker-bridge-observation.md) records bounded
trusted-CLI version/identity/bridge/inventory/event reads and selected Docker
Desktop backend log timestamps. No restart, setting/firewall change, image
build/pull, container start/stop/exec or resource create/delete/prune was invoked.
Raw endpoint/daemon identity and logs remain private under ignored artifacts.

Engine 29.8.0/Linux ARM64 was reported. Retained Desktop idle stop/start windows
coincide with bridge creation at 04:18:14 and 04:24:48 UTC, supporting an automatic
lifecycle explanation. Exact create/delete causal chain and wake caller remain
UNKNOWN. Both historical/recent bounded event queries timed out after eight seconds;
no complete activity audit or uninterrupted daemon lifetime was established.

End-of-task container, image and volume IDs matched the diagnostic baseline; all
five known unrelated network IDs remained present. One network ID was added and
one removed. A final bridge inspect's ID was absent from the preceding network
list, demonstrating the reads were not an atomic consistent snapshot. Global
inventory equality and final exact-default-bridge attribution checks therefore
FAILED and are retained as failed. No retries, cleanup or host changes forced a
pass. The new code has no Docker mutation implementation and recorded diagnostics
were read-only; this does not audit every concurrent actor or qualify isolation.

### Compatibility and unrun work

Existing plan encoding, sample topology/engine, tracing, SQLite, format-1 bundles,
policy semantics, dependencies and release scripts are unchanged. Protected v0.1.0
package/evidence/demo hashes and the README video URL remain unchanged; no tag or
release is modified. Private state is new and separate from experimental data.
No source databases, existing journals or user evidence were edited or deleted.

Real fault/tracing integration wrappers were **not run** because they create
containers/networks outside this increment's authorization. No frontend/browser,
Linux filesystem/runtime, native Intel, actual worker/application, custom network
probe, image qualification or power-loss durability test was performed. Filesystem
claims cover observed host file/directory sync, atomic publication and process exit,
not hardware/full-fsync or unconditional crash cleanup guarantees.

GO for independent review of this non-mutating foundation only. NO-GO for real
runtime qualification/execution in this increment: fixed ledger/lease wiring,
trusted actual observation/mutation adapters, catalog bootstrap and explicit
qualification authorization remain separate reviewed gates. No synthetic proof,
journal hash or structurally validated graph establishes effective Docker safety.

## M8B2B — Trusted supervisor boundaries (2026-10-28 UTC)

PR #12 was verified OPEN/MERGEABLE at exact approved head
`8b9e3f339f75b8c7f6488907b9f4026db12bbba2`, base
`053fc73676cb2116be1ecb2ce85ab7d994770738`, with six identical reviewed commits
and 20 identical reviewed files. v0.1.0/tag/five assets/protected files matched the
baseline; working tree was clean. Regular merge produced
`a7d48f06fdea53e889d47c27ab028abc5bf3fa9e`, with those exact base/head parents.
GitHub reports merged, all six commits/authorship are preserved and local main
fast-forwarded to origin/main before the feature branch was created.

Scope: [ADR 0010](decisions/0010-authoritative-supervisor-state.md) and the
[trusted boundary contract](design/trusted-supervisor-boundaries.md). State-root,
reservation/lease, strict exact-ID inspection, cleanup admission and opaque intended
operations are implemented. No production RUN/CLEANUP, actual mutation driver,
application execution or isolation qualification is enabled. Operator-home state
was not enrolled; authority/filesystem tests use private temporary homes only.

### Actual checks and outcomes

- `make check` PASS on Go 1.26.8/macOS ARM64: shell syntax/endpoint precedence,
  two Python packaging tests, full Go formatting/vet and uncached unit race tests.
  Existing SQLite migration/process-exit recovery, engine/tracing, dashboard API,
  format-1 bundle/comparison/gate and contract tests passed.
- `go mod verify` PASS. CGO-disabled tests for lab, bundle, bundlecli, bundlecompare,
  bundleevaluate and contracts PASS. Native CLI and independent CGO-disabled offline
  executable built in the new ignored task directory; no release binary rebuilt.
- Fixed-root tests PASS: OS-account selection ignores HOME/XDG/state variables;
  an alternate empty directory cannot open authority; consumed identities remain
  blocked after close/cleanup and partial consumption. Missing anchors, manifests,
  journals, checkpoints, receipts, corrupt/ambiguous/oversized registry data and
  uncheckpointed records fail closed. Symlink/FIFO/hard-link/permission/root-inode
  substitution tests use actual temporary files.
- Lease tests PASS: eight concurrent attempts yield exactly one reservation;
  cleanup cannot displace a live process holder within a bounded wait. An actual
  test child exits with its lease held; kernel lock recovery succeeds while the
  durable reservation still prevents a new RUN. Closing a process handle does
  not release durable ownership.
- Legacy schema-1 journal import tests PASS: validated history is retained and
  the identity is consumed, but import is a tombstone and never enables RUN,
  cleanup recovery or foreign-resource adoption. Original input files/bytes are
  not rewritten. Existing over-budget journals remain readable.
- Observation tests PASS: exact ID/name/nonce/role/image/configuration checks,
  fixed attachments, unexpected mount/port/privilege/network rejection, malformed/
  duplicate/escaped/case-aliased/Unicode/oversized metadata, missing resource,
  changed daemon/socket/runtime and cancellation. Real template evaluation checks
  that security/env flags are derived from selected metadata and arbitrary env
  values are not copied. All full lab observations in these tests are synthetic.
- Budget tests PASS: every creation stage has required cleanup/retry reserve;
  repeated optional proxy retries refuse before required capacity is spent;
  a valid old 96-event history remains readable and can still prepare exact-ID
  cleanup with recording explicitly unconfirmed. Recorded cleanup attempts never
  invent success or another RUN after exhaustion/write failure.
- Opaque mutation-contract tests PASS: detached descriptions cannot change intent,
  deserialization cannot forge one, unknown/foreign resources cannot reach the
  recording transport and runtime changes invalidate planning. Public success
  assertions are refused. A held living lease retains known cleanup scope after
  recording failure; no successful receipt/release or further provisioning follows.
- Fourteen retained real 80 ms, 400 ms and canceled-bundle cases matched previous
  verify/inspect/compare/evaluate JSON byte for byte with unchanged exits, including
  evaluation 0/3/4. Original `.flb` hashes remained unchanged. Native CLI smoke
  confirms `lab`, `lab run`, `lab cleanup` remain usage 2 with no stdout.

A prototype compile failure from a misplaced atomic field was corrected before
verification/commit. The final full suite passed; failed intermediate diagnostics
are retained separately, not counted as passes. No new fuzz campaign was executed
in this increment; shared strict parser and existing regression tests ran.

### Actual read-only Docker observations

Trusted local CLI inspection reported Docker Engine 29.8.0/Linux ARM64. The opt-in
`TestLiveReadOnlyRuntimeWitness` PASS used only context/version/identity reads to
check real socket/daemon binding and reject an altered socket identity. Its plan
image/application fields were explicitly synthetic, not a real qualified app.
No image, container or network was started or provisioned by the test.

Bounded diagnostics inspected the existing default bridge metadata/ID and resource
ID inventories. A selected IPAM projection executed successfully without copying
addresses. Read-only inspection of the existing pinned Toxiproxy image found
**eight inherited OCI label keys**. No arbitrary label values were collected.
That exposed a real candidate-profile incompatibility: requiring three labels on
all containers would reject both this image and the lab's two inherited contract
labels. Fixed role profiles now expect application 3, lab 5 and proxy 11 fixed
keys/counts; foreign nonce/keys/contracts still reject. OCI values are not ownership
or provenance authentication. This was image metadata inspection, not runtime testing.

The start inventory had 9 containers, 6 networks, 67 image IDs and 10 volumes.
End container/image/volume IDs were unchanged. Network inventory changed by one ID
added/one removed. The final default bridge metadata reported creation
`2026-10-28T06:58:24.196007041Z`, but that ID was absent from the preceding list.
**Global inventory equality and exact attribution of that final difference failed.**
They remain failed; no retries/restarts/settings changes or cleanup forced a pass.
The earlier idle VM lifecycle evidence remains a plausible explanation, while
exact causal/caller attribution is UNKNOWN. See the
[preserved investigation](security/docker-bridge-observation.md).

All task-issued Docker commands were read-only (context/version/identity, selected
metadata/image inspection and bounded ID snapshots). No create/run/start/exec,
network modification, build/pull/delete/prune, socket mounting, host policy change
or application execution occurred. This command audit is not a complete audit of
other actors or proof that Docker Desktop remained internally unchanged.

### Compatibility, limits and work not run

Plan/catalog/graph encoding, original journal schema/decode, existing sample/CLI
execution, SQLite, tracing, format-1 evidence, gates, dependencies and release scripts
are unchanged. Only new journal writes apply cleanup admission. Protected packages,
evidence/demo hashes and README video URL remain unchanged; no v0.1.0 tag, Release
or asset was modified. No authority initialization occurred outside test fixtures.

Real fault/tracing integration wrappers were NOT RUN: they create resources outside
this milestone's authorization. No frontend/browser, real custom-container presence,
absence/cleanup driver, runtime isolation/app conformance, Linux filesystem or
power-loss qualification was performed. The missing catalog image was not built
or pulled. Tests do not establish production readiness, statistical guarantees,
effective Docker isolation, ACL completeness or unconditional fsync/deadline safety.

Reservations with attempted resources cannot be released until a separate reviewed
actual absence/result certification path exists. Legacy imports also retain no
cleanup authority. The fixed ledger/lease is cooperative and does not resist a
malicious same-user administrator or rollback of all private state. Partial
checkpoint/enrollment/import and quota exhaustion deliberately remain unresolved;
there is no automatic reset, compaction, repair or arbitrary recursive deletion.

GO for independent review/merge of non-mutating M8B2B. NO-GO for beginning real
provisioning/runtime qualification without the separately reviewed driver, certified
outcomes/absence path, catalog bootstrap and explicit owner authorization. Synthetic
observations and inspection metadata cannot satisfy those gates.
