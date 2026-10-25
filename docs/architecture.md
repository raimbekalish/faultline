# Architecture

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
