# ADR 0003: Versioned specifications and transactional local history

- Status: accepted
- Date: 2026-10-24

## Specifications and execution boundary

Use a four-field schema-1 JSON document: schema version, samples per phase,
latency milliseconds and request timeout milliseconds. Require every field and
reject unknown/case-mismatched/duplicate keys, unsupported versions and trailing
data. Reuse the existing engine's limits. No target URLs, workflow language or
implicit overrides are introduced. Canonical JSON and its SHA-256 identify the
configuration, not timing results or environmental equivalence.

Keep the engine lifecycle unchanged. A small execution layer commits a `running` history
intent before calling it. It records the final report under a new bounded context
after engine cleanup and telemetry shutdown. No database transaction spans an
experiment or an active fault. SQL failures before the intent commit prevent
execution; finalization failures leave the intent and return an error separately
from the engine report. Original schema-1 stdout reports remain unchanged.

An intent has a random `history-` ID before the engine's `faultline-` ID exists.
Final entries retain both, and lookups accept either. An unfinished intent is
not automatically labeled failed: it may represent an active process, a crash,
or a failed final commit. There is no per-request durable checkpoint or automatic
fault resumption. Normal failure/cancellation reports retain available samples.

## Driver evaluation

Compared `mattn/go-sqlite3` 1.14.52 and `modernc.org/sqlite` 1.61.0. The downloaded
module archives measured about 2.9 MiB and 23 MiB, respectively; this is source
archive size, not binary size or runtime memory. Mattn bundles SQLite's C source
and has no additional Go module requirements. Modernc avoids CGO and supports
Linux/macOS ARM64, but includes generated Go SQLite plus libc and other modules.

Choose mattn, using the existing pinned Debian Go builder's native C compiler.
Its CGO requirement also applies to host development (a C toolchain is needed).
Native Docker ARM64 builds avoid cross-compilation; another architecture needs
its own compiler/builder. Statically link the CLI with `netgo,osusergo` to retain
the scratch runtime and Go DNS behavior. Sample services remain CGO-disabled.
Verify actual static execution and race tests rather than assuming compatibility.

## Storage and durability

Use SQLite `database/sql` with one connection per store, a 500 ms busy timeout,
foreign keys on, immediate write transactions, DELETE rollback journaling and
`synchronous=EXTRA`. The extra directory sync favors durability over throughput;
only intent and finalization commits are needed per run. Keep the database on a
local filesystem. Do not disable journaling, use shared cache, or hold SQL locks
through workload measurement. File creation uses mode 0600; directory creation
uses 0700. Reject symlink database files, foreign application IDs and future
schema versions. Explicit ordered migrations and `user_version` update atomically.

Store canonical specifications, hash, indexed status/timestamps/engine ID and a
bounded complete report JSON in one row. The report contains phase samples,
summaries, errors, cleanup state and optional trace references. Keeping one report
snapshot avoids duplicated phase tables and multiple sources of truth. History
queries validate records, bound list sizes and never create a missing database.
They permit SQLite recovery writes so hot rollback journals can be recovered.
Validation recomputes summaries using the engine's existing pure calculation and
checks stored comparison deltas. Counts, rates and order statistics match exactly;
means and arithmetic deltas allow four ULPs of roundoff, with exact expected zero
and no nonfinite values. This covers partial failed/canceled reports and rejects
inconsistent data without repair or a schema change. It provides internal
consistency checking, not cryptographic integrity against coordinated alteration
of samples and their summaries.

Final reports are immutable. Failed transactions preserve prior committed data.
An abrupt process-exit test will exercise uncommitted journal recovery. This does
not prove durability against power loss, broken filesystems, Docker VM/storage
failure or dishonest `fsync`. Do not remove journal files or copy a live database
without SQLite's backup protocol. There is no export/backup or retention feature
in this milestone. Compare only completed, successfully cleaned runs; return
configuration snapshots and descriptive right-minus-left phase deltas.

Trace IDs are references only: transient Jaeger data disappears at teardown.
Persistent history is local and ignored by Git; no hosted storage or CI is added.

References: [mattn CGO/build requirements](https://github.com/mattn/go-sqlite3),
[modernc platforms and dependencies](https://pkg.go.dev/modernc.org/sqlite),
[SQLite synchronous/journal pragmas](https://sqlite.org/pragma.html),
[SQLite atomic commit and recovery](https://sqlite.org/atomiccommit.html).
