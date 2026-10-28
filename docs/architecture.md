# Architecture

Milestone 8A proposes a future owner-selected service boundary in
[ADR 0009](decisions/0009-owner-selected-service-labs.md), the
[custom-lab design](design/custom-service-lab.md) and its
[threat model](security/custom-service-threat-model.md). M8A was approved/merged.
Unreleased M8B1 adds only a [strict read-only planner](custom-service-planner.md):
`internal/lab` owns the closed contracts, private file publication and bounded
inspection adapter; the native CLI exposes only `lab plan`. There is no generic
Docker execution API. Unreleased M8B2A adds the internal
[closed graph and private journal foundation](design/custom-service-supervisor.md),
without connecting it to an execution consumer or mutation transport. Current execution remains sample-only; no custom runtime,
tracing, SQLite or portable-evidence support is enabled.

## First vertical slice

```text
CLI / engine --> frontend --> Toxiproxy data port --> dependency
       |
       +-------------------> Toxiproxy control API
```

One Compose project owns the sample stack and its private network. No service
ports are published to the host by default. The runner and sample services use explicit
service names; the first release does not accept arbitrary network targets.

The Go engine runs baseline, latency and recovery phases in sequence. It owns
one uniquely named proxy and fault, measures bounded serial HTTP traffic, and
removes its resources on success, error or cooperative cancellation. A shell
entrypoint tears down only its own Compose project as a second cleanup layer.

Code boundaries:

- `cmd/faultline`: CLI parsing, signal handling and report output
- `internal/experiment`: lifecycle, measurement and comparison
- `internal/toxiproxy`: bounded control API client
- `internal/sample`: HTTP sample services
- `internal/telemetry`: explicit OpenTelemetry providers, safe propagation, HTTP spans
- `internal/tracequery`: bounded retrieval from the pinned local Jaeger query API
- `internal/spec`: strict versioned specifications and canonical configuration hashes
- `internal/execution`: durable intent, engine invocation, export and finalization order
- `internal/history`: SQLite migrations, immutable reports, validated queries/comparison
- `deploy/`: sample topology
- `scripts/`: reproducible developer entrypoints

## Safety model

The dedicated local topology is the allowlist. Toxiproxy is reachable only inside
the isolated network. The process never changes host firewall rules or mounts a
Docker socket. Limits bound samples, injected latency and total execution time.
Cleanup uses a separate timeout and surfaces errors. SIGKILL, host crashes and
Docker daemon failure cannot guarantee in-process cleanup; project teardown is
the recovery mechanism.

## Measurement model

Compare identical sequential workloads before, during and after injection.
Record individual end-to-end HTTP durations and outcomes, failure rate and
latency distributions. These small samples validate causal behavior; they do not
claim statistically rigorous performance benchmarks or saturation capacity.

See [ADR 0001](decisions/0001-local-vertical-slice.md) for scope decisions.

## Implemented lifecycle

1. Validate sample count and durations before any control request.
2. Generate a cryptographically random 128-bit ownership identifier. Register
   proxy deletion before sending create, covering ambiguous POST outcomes.
3. Create one proxy on the fixed data port, upstream `dependency:8080`. A second
   runner in the same stack fails to bind instead of reusing the first proxy.
4. Exercise `frontend:8080/work` until ready (five seconds maximum).
5. Collect baseline; refuse injection if any baseline request fails.
6. Add a downstream latency toxic with zero jitter and toxicity 1; collect fault
   observations, including HTTP errors and client request deadlines.
7. Remove the toxic and collect recovery; recovery failures fail the run.
8. Always delete the owned proxy with a fresh five-second context. Report
   cleanup errors together with any original error. Only then finalize JSON.

The frontend calls `toxiproxy:8666/value` with a 250 ms timeout. The dependency
returns a constant JSON value. Latency below that timeout increases request
duration; latency above it causes a real HTTP 504. No application handler sleeps
to simulate network effects. HTTP transports ignore environment proxies and
refuse redirects. Bodies are capped at 64 KiB. Workload phases issue one request
at a time; there is no unbounded worker pool.

The two-minute engine deadline bounds the workload independently of the CLI's
SIGINT/SIGTERM cancellation. Sample HTTP servers bound headers, reads, writes,
idle connections and graceful shutdown. Runtime containers have 64 MiB RAM,
0.5 CPU and 64-process limits each. The test container gets 1 GiB and two CPUs.
Image versions are pinned by manifest digest. Runtime images contain only static
Go binaries; the compiler and race detector remain in the development stage.

The shell entrypoint owns startup and teardown, not the Go engine. The runner has
no Docker access. Compose startup has a 30-second readiness wait; builds/downloads
are outside the engine deadline. The shell trap handles ordinary failures and
signals, but cannot guarantee cleanup after SIGKILL or daemon failure. No durable
crash recovery or shared-server lease is claimed.

## Optional tracing lifecycle

`Run` retains its disabled behavior; `RunWithTelemetry` accepts an explicit SDK
runtime. The engine records optional root trace/span IDs for each sample and
passes validated run/phase/observation metadata. It owns no exporter shutdown.
The CLI waits for the engine to finish proxy cleanup, then shuts down telemetry
under a fresh two-second timeout and records runner-only export statistics.
Telemetry loss cannot prolong an injected fault or hide a cleanup error.

Known sample routes have server spans; HTTP calls have client spans ending after
body reads. Existing request/client deadlines and response limits remain in place.
W3C trace context and allowlisted baggage connect the four spans per observation.
Readiness is tagged separately; health checks and Toxiproxy control operations are
not traced. Status/error attributes distinguish HTTP failures from dependency
client deadlines. Raw request errors are classified before recording, avoiding
credentials and arbitrary network strings in spans. Providers are not global.

The tracing overlay adds memory-only Jaeger with bounded trace storage and a
memory limiter. It is absent by default. SDK queues, batches, attributes, events,
export calls and shutdown waits are bounded. A full queue drops spans rather
than blocking application traffic. The query adapter polls at most 300 trace IDs
under one caller deadline and retains explicit missing/partial results. Its
completeness flag validates four unique nonzero span IDs, expected service/kind/
operation roles, exactly one runner root and all three same-trace parent edges.
Ambiguous references and duplicate/extra spans are rejected. It never assembles
one trace from separate query objects or polls. A corrected complete snapshot
supersedes an earlier larger malformed response; query failures retain observed
partial evidence. Tests also verify observation IDs and correlation metadata.

An explicit `--view` overlay publishes only the query/UI endpoint to a random
loopback port and limits the viewer lifetime to five minutes. There are no
persistent volumes, published ingestion/admin endpoints, or external targets.
Compose teardown includes Jaeger and uses twelve seconds for graceful service
and exporter shutdown. See [tracing](tracing.md), [reports](reports.md), and
[ADR 0002](decisions/0002-bounded-optional-tracing.md).

## Reproducible execution and persistence

```text
specification / legacy flags → execution → existing experiment engine
                                  │                  │
                             intent commit      proxy cleanup
                                  │                  │
                                SQLite ← export shutdown → final report commit
```

The engine lifecycle and instrumentation are unchanged. A specification maps directly to
the existing configuration; it cannot change topology. Legacy flag runs remain
ephemeral unless `-db` is explicit. File-based runs opt into local history.

Execution commits a uniquely identified intent before calling the engine, then
finalizes under a fresh three-second context after cleanup and telemetry shutdown.
No database transaction is held through traffic generation or injection. Failure
before the intent commit prevents engine execution. Failure after execution leaves
the committed intent, returns an error and preserves the actual engine report on
stdout. A subsequent stdout failure does not undo a committed history record.

SQLite stores indexed identity/status/timestamps and canonical spec/report JSON,
with transactional migration/version checks. Completed reports are immutable;
failed and canceled reports may contain partial observations. An unfinished intent
is not labeled with an inferred failure cause. There is no per-request checkpoint.
Read/compare paths validate records, bound payloads/list sizes and use no fault
control or collector access. Summary validation reuses the engine's existing
pure calculation through a small exported wrapper, checking raw samples rather
than only plausible metric ranges. Trace IDs persist as references only.

The pinned mattn driver uses bundled C SQLite. The CLI is statically linked in
the existing compiler image; samples retain their previous CGO-disabled build.
Scratch execution with real SQLite was verified. Persistent container commands
mount only the selected input and history directory and run as the host's non-root
UID/GID. Query commands start no sample or collector services. Integration packages
are serialized because the fixed test topology shares one proxy port.

DELETE journaling, `synchronous=EXTRA`, bounded lock waits and short transactions
favor durability over throughput. A subprocess test exercises real hot-journal
recovery. Filesystem/VM/hardware failures remain outside that guarantee. Details,
commands and comparison semantics are in [history](history.md) and
[ADR 0003](decisions/0003-specifications-and-sqlite-history.md).

## Local reliability dashboard

```text
React + TypeScript + SVG (static Vite build)
                 │ same-origin GET /api/v1
native Go HTTP server (127.0.0.1, bounded lifetime)
                 │
history Browse / Get / Compare → existing SQLite database
```

`cmd/dashboard` owns listener/signal/lifetime shutdown; `internal/dashboard`
owns transport validation and typed JSON projections. `internal/history` owns
snapshot pagination, filters, counts, comparisons and record integrity. The UI
renders validated summaries and observations rather than recalculating experiment
statistics. Views live under `web/src/views`, with shared request/format/chart
components. No router, UI kit or chart runtime is required beyond React/React DOM.

Lists validate fetched records before projecting summaries; detail retains the
original report, including partial samples and optional trace IDs. Global counts
describe status metadata, not integrity of unreturned payloads. Query connections
use deferred transactions; execution retains immediate writes. DELETE journal read
snapshots can briefly contend with writers; three-second deadlines bound queries.
Hot-journal recovery can write to disk even though the API offers no mutations.

Exact Host/Origin validation, cross-site fetch rejection, GET-only routes,
concurrency/header/time limits and a restrictive CSP protect the loopback server.
Built assets and API share one origin. There are no Docker ports/socket mounts,
third-party fonts/requests, collector calls or experiment execution routes. The
one-hour default lifetime is explicitly bounded to eight hours. Shutdown waits for
requests before closing SQLite. See [dashboard](dashboard.md) and
[ADR 0004](decisions/0004-local-read-only-dashboard.md).

## Portable evidence and provenance

`internal/record` is the shared semantic validator for history and offline data;
it calls the unchanged engine summarizer. `internal/bundle` owns the bounded ZIP
profile, hashes and atomic no-replace file publication; `internal/bundlecli` owns
SQLite-free verify/inspect commands. History adapts validated entries into the
portable structure without becoming an offline dependency. The original CLI
exports from existing history; `cmd/faultline-bundle` builds with CGO disabled.

Execution records optional `internal/provenance` metadata after fault cleanup and
telemetry shutdown, before finalization. The wrapper can provide observed Docker/
image facts read-only before execution. Process/build facts never come from the
export machine. Optional report metadata requires no SQLite migration and legacy
records remain unchanged. Bundle/JSON/provenance versions fail closed when unknown.
The dashboard adds an ID-scoped GET download using the same builder and guards.
See [evidence](evidence.md) and [ADR 0005](decisions/0005-portable-evidence-and-provenance.md).

## Offline comparison

`internal/bundlecompare` consumes logical evidence and implements deterministic
configuration/provenance comparison, measurement availability, relative-change
edge handling and explicit eligibility. It has no filesystem, database or network
operations. `bundlecli` owns two independent bounded archive reads and versioned
JSON presentation. The CGO-disabled verifier and full CLI share this command.

`experiment.CompareSummaries` centralizes the existing absolute right-minus-left
arithmetic for reports, record validation, history and offline comparison. Sample
summary calculations remain in the existing engine summarizer. Old history JSON
units/fields are preserved; the new offline result explicitly labels percentage
points and nullable relative percentages. No bundle, report or database migration.
No dashboard/API expansion. See [ADR 0006](decisions/0006-offline-evidence-comparison.md)
and [offline comparison](evidence.md#offline-comparison--result-schema-1).

## Offline reliability gates (Unreleased)

```text
local policy JSON → bounded strict decoder → validated Policy
                                                   │
format-1 .flb → existing bundle/record validators → pure evaluator → result JSON
                                                                  + exit code
```

`internal/bundleevaluate` separates pure decisions from its local policy-file
adapter. It reuses `jsonstrict` for Unicode/duplicate checks and the existing
record/bundle validators for evidence integrity. Only explicit version-1 fields
exist. A fixed grammar bounds input to 8 KiB and output to 17 conditions; there
are no expressions, external targets, includes, database or network operations.

The engine summarizer supplies canonical metrics after validation. Policies never
recalculate statistics using a separate algorithm or repair original report bytes.
Completed execution and cleanup are mandatory; partial/missing/undersampled phases
stay indeterminate. Inclusive thresholds use actual float64 values and fraction
failure rates, with no display rounding/epsilon. Fixed output order and identities
without paths/clocks make identical inputs deterministic across qualified targets.

`bundlecli` owns the existing strict archive read, policy read, JSON presentation
and additive 0/3/4 verdict exits; old 1/2 error/usage meanings remain. No existing
result, report, specification, bundle or database version changes. The native
CLI's existing delegation reaches the same command without new persistence logic.
Policy files on Darwin/Linux are opened without following a final symlink or
blocking on a substituted FIFO, then checked against their original regular inode.
See [ADR 0008](decisions/0008-offline-reliability-gates.md) for semantics and limits.
This command is not in v0.1.0; no published binary is changed.

## Release and compatibility boundary

The offline executable remains CGO-disabled/database-free; native CLI/dashboard
builds retain embedded CGO SQLite. Dashboard assets travel with their matching
native binary. Full experiments still enter through the source Compose wrapper;
no native package bypasses fixed-topology ownership or cleanup.

`internal/buildinfo` exposes executable version/revision/toolchain/target metadata
through `--version`, independently of experiment provenance. The standard-library
packager builds known files into deterministic, no-replace local candidates with
license/instructions/checksums. No release tag/upload path exists in the script.
`internal/contracts/testdata` freezes synthetic format-1 evidence and public JSON;
Go API tests replay those inputs through real temporary SQLite databases.

The parser now rejects simple-fold-equivalent JSON duplicates and invalid surrogate
escapes, and validates central disk/reader-version/local descriptor consistency.
This narrows malformed input acceptance without changing valid format 1. No schema
or database migration, statistics algorithm, fault transport or tracing change.
See [release matrix](release.md), [security](../SECURITY.md) and ADR 0007.
