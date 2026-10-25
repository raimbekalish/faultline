# Reproducible experiments and local history

The existing `run` flags and schema-1 reports remain supported. History is optional
for flag-based runs; specifying a configuration file enables it by default. Every
experiment still uses the fixed isolated sample topology. No network targets,
workflows, environment interpolation or automatic fault resumption are added.

## Commands

From the repository root, with Docker running through a local Unix socket:

```sh
# Original ephemeral behavior; no database is opened:
./scripts/experiment.sh -samples 5 -latency 100ms

# Repeat the specification; default DB is artifacts/faultline.sqlite:
./scripts/experiment.sh run -spec examples/latency.json
./scripts/experiment.sh run -spec examples/latency.json

# Custom database, tracing, or optional persistence for legacy flags:
./scripts/experiment.sh --tracing run -spec examples/timeouts.json -db artifacts/demo.sqlite
./scripts/experiment.sh run -samples 5 -latency 80ms -db artifacts/demo.sqlite

./scripts/experiment.sh history list
./scripts/experiment.sh history list -db artifacts/demo.sqlite -limit 20
```

Replace the quoted placeholders with `id` or `run_id` from list output. Flags
precede positional IDs, as in Go's standard flag parser:

```sh
./scripts/experiment.sh history show -db artifacts/demo.sqlite 'RUN_ID_FROM_LIST'
./scripts/experiment.sh history compare -db artifacts/demo.sqlite 'LEFT_ID' 'RIGHT_ID'
```

The native CLI accepts the same commands. Building it requires Go, CGO and a C
compiler; Docker provides the pinned toolchain. The runtime CLI is statically
linked with SQLite; sample services remain CGO-disabled. Only Docker Linux/ARM64
was exercised here, not native macOS or a cross-platform build matrix.

The wrapper mounts only the selected specification read-only and database
directory read-write; its root stays read-only. Persistent commands run as the
non-root host UID/GID so files remain host-owned. Root host accounts are refused
for this container path. Queries start only a short-lived CLI container/network,
without sample services or Jaeger, and do not accept `--tracing`/`--view`.
Teardown removes the disposable stack/image tags and retains the database bind.

## Specification schema 1

```json
{
  "schema_version": 1,
  "samples_per_phase": 10,
  "latency_ms": 80,
  "request_timeout_ms": 1000
}
```

All fields are required integers. Engine limits remain samples 1–100, latency
1–5000 ms and request timeout 10–5000 ms. Unknown/case-mismatched/duplicate fields,
nulls, fractional values, unsupported versions, trailing data and inputs over
64 KiB are rejected. Workload flags cannot override a specification. Field order
and whitespace do not change canonical JSON or its SHA-256 `spec_hash`.
Identical specs reproduce configuration, not timing results or environments.

## Run lifecycle and query semantics

A random `history-…` ID identifies a committed `running` intent before the engine
starts. Its canonical specification, hash and UTC timestamp survive normal
execution errors. The engine creates its separate `faultline-…` ID, retained as
a unique lookup alias when available. The CLI prints the history ID to stderr
and emits the original experiment report to stdout without new history fields.

After engine fault cleanup, telemetry shutdown has its own two-second timeout.
History finalization then uses a fresh three-second context independent of
cancellation. One transaction stores status/timestamps and the whole report:
phase samples/summaries, errors, cleanup, configuration, provenance and optional
trace references. Final reports are immutable. No SQL transaction spans traffic
measurement or an active fault.

- Failed intent persistence prevents engine execution.
- Normal engine errors/cancellation preserve available measurements and return
  the engine error; phases may be partial.
- Failed finalization returns an error and leaves the durable `running` intent.
  A complete stdout experiment report does not imply successful persistence.
- Failed stdout writes after finalization leave history available. Combined
  execution/persistence and output errors are both reported.
- Unfinished intents can mean active execution, abrupt termination or failed
  persistence. The tool does not guess the cause or manufacture measurements.
  There is no per-request checkpoint; a mid-run kill loses unfinalized samples.

`list` returns at most 100 entries, newest first, omitting report payloads. `show`
returns the entry and full report. Missing, incompatible and malformed data are
errors. Queries never create a missing database. The entry finalization timestamp
is recorded during its transaction; report `finished_at` keeps its original
engine-cleanup boundary.

Comparison requires two complete, successfully cleaned runs. It returns both
specifications, phase summaries and **right minus left** mean/p95/failure-rate
deltas. `same_spec` compares only configuration hashes, not revision, tracing or
environment. Use `show` for report provenance. Differences are descriptive,
without statistical significance or automatic causal diagnosis.

Trace IDs are references only. Temporary Jaeger data disappears on teardown;
history neither stores full traces nor makes old IDs retrievable in a new collector.

## Durability and recovery

SQLite uses an application ID, transactional `user_version` migrations, one
connection per store, a 500 ms busy timeout, immediate writes, foreign keys,
DELETE journaling and `synchronous=EXTRA`. The extra directory sync favors
durability over throughput; there are two short commits per experiment.
Unrelated/future databases and symlink database files are rejected. New files
use 0600 and new directories 0700. Snapshots are capped at 4 KiB, report payloads
at 1 MiB and list sizes at 100. Validation is not cryptographic tamper protection.

Read commands need write access because SQLite may recover a hot rollback
journal. The subprocess test forces dirty-page spill, exits without SQLite
close/rollback, confirms a hot journal, and recovers the old intent and an earlier
committed report. This proves a process-failure path, not power-cut durability.

Operations use three-second contexts; kernel/filesystem stalls cannot have a hard
wall-clock guarantee. The wrapper's 12-second stop grace covers five-second fault
cleanup, two-second export, three-second persistence and a margin. SIGKILL or
daemon failure can still require README's exact-project Compose teardown;
database recovery does not remove faults.

Use a local filesystem. Retain rollback journals and do not copy a live database
instead of using SQLite's backup protocol. No backup, retention, automatic retry
or corrupted-file repair is implemented. History grows with usage. Disk-full and
physical I/O faults were not induced; failures use real locks, read-only mode and
aborting SQLite triggers. Filesystem/VM/hardware flush behavior remains an assumption.

## Verification

```sh
./scripts/experiment.sh test
./scripts/experiment.sh --tracing test
```

Suites use temporary databases, actual SQLite and real Toxiproxy faults. Packages
run serially (`-p 1`) because they share one sample proxy port. Tests cover
repeated execution, cancellation, tracing on/off, queries/comparison and output
failure, plus existing fault/trace regressions. Unit race tests cover strict
specs, migrations, rollback, contention, malformed data and process-exit recovery.
See [validation](validation.md) and [ADR 0003](decisions/0003-specifications-and-sqlite-history.md).
