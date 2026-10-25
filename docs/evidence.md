# Portable evidence and recorded provenance

A `.flb` file is a self-contained evidence ZIP. Verification and inspection need
only the platform-appropriate `faultline-bundle` executable and that file. They
never open SQLite, start Docker, contact Jaeger or fetch network data. Copy the
verifier with the bundle before going offline; no Go installation is needed at
runtime. Ordinary ZIP tools can also read the included JSON files.

## Commands

Build once while Go 1.26+ and dependencies are available. The exporter requires
a C compiler for the existing SQLite driver; the offline verifier is CGO-disabled.

```sh
make cli offline

# Generate real history using the isolated wrapper if needed:
./scripts/experiment.sh run -spec examples/latency.json
./artifacts/bin/faultline history list -db artifacts/faultline.sqlite

# Replace RUN_ID with a listed history or engine ID; flags precede IDs:
./artifacts/bin/faultline bundle export \
  -db artifacts/faultline.sqlite -out artifacts/run.flb RUN_ID

./artifacts/bin/faultline-bundle verify artifacts/run.flb
./artifacts/bin/faultline-bundle inspect artifacts/run.flb
```

The full CLI also accepts `faultline bundle verify/inspect FILE`. The Compose
wrapper intentionally refuses `bundle` commands before contacting Docker; portable
commands run natively. Native `run` is not part of this workflow: use the isolated
Compose wrapper for fault experiments.

`verify` checks the whole supported schema and statistics before printing a small
JSON result. `inspect` performs those same checks, then prints the original report,
specification, history metadata and warnings. Both return nonzero without a result
on verification failure. Inspection preserves actual measurements rather than
manufacturing absent samples or inferring an unfinished intent's outcome.

Export supports validated complete, failed and canceled final reports, including
partial phases and failures before an engine run ID was assigned. Unfinished
intents cannot be exported because no final report exists. Export does not alter
history, migrate databases, recalculate stored results or access a collector.

The dashboard's run detail page offers **Download evidence** for finalized reports.
It calls `GET /api/v1/runs/{id}/bundle` on the same local origin. That route accepts
an ID only, never a filesystem path, and retains existing Host/Origin/CSP/method/
concurrency/deadline protection. It returns `application/zip` with a safe attachment
filename; API errors remain versioned JSON. Unfinished exports return HTTP 409.

## Format version 1

Exactly four regular entries appear in this fixed order:

| Entry | Contents / maximum size |
| --- | --- |
| `manifest.json` | Format name/version, sizes and SHA-256 for the three evidence files; 4 KiB |
| `history.json` | Original history/run IDs, spec hash, creation/finalization timestamps, status; 4 KiB |
| `spec.json` | Canonical schema-1 experiment specification; 4 KiB |
| `report.json` | Original validated report bytes, including raw observations and optional metadata; 1 MiB |

The ZIP uses Store, not compression; fixed DOS file timestamps make repeat exports
of the same recorded run deterministic with the supported writer. Those timestamps
are container metadata, not experiment times. Manifest/history/spec have the fixed
canonical JSON encoding defined by their Go struct fields. Report whitespace and
optional schema-1 fields remain intact. There is no export timestamp or current
export-machine environment injected into the evidence.

SHA-256 covers the included evidence files. Checksums detect changed content and
inconsistent hashes; they do **not** provide authenticity, attest execution or
prevent coordinated alteration of samples, summaries and hashes. The manifest
cannot authenticate itself. Review original reports/errors before sharing them;
provenance collection is allowlisted, but historical errors are preserved verbatim.

The reader does not extract anything. It rejects extra/missing/duplicate entries,
unsafe names, symlinks, compression, ZIP64, extras/comments, hidden local data,
header/descriptor mismatches and CRC/hash/size failures. Archive size is capped at
2 MiB; central-directory size/count is checked before ZIP parsing. JSON depth is
limited to 32; duplicate/case-colliding schema keys are rejected. Unsupported bundle,
spec/report or provenance versions and noncanonical metadata fail verification.
This is a deliberately narrow archive profile, not a general ZIP reader.

Report integrity uses `internal/record`, shared with SQLite history. It calls the
existing engine summarizer for counts, failures, rates, min/max, nearest-rank
percentiles and means; arithmetic tolerance remains four ULPs where documented.
Stored comparison deltas and configuration/hash/identity/status relationships are
checked too. There is no separate statistics implementation for portable evidence.

## New environment records

New finalized execution reports may include optional schema-1 `environment`:

- The recorded Faultline build revision, Go version and canonical spec hash.
- **Runner process** OS/architecture, not an inferred laptop/host OS.
- `serial-http-nearest-rank-v1` measurement methodology and an explicitly recorded
  tracing-enabled boolean. Missing/null tracing facts are rejected, not read as off.
- When supplied by the local wrapper: observed Docker **server** version, the
  dependency/sample container's image identifier, and Toxiproxy/optional Jaeger
  image identifiers plus configured pinned references. Local sample image tags
  are excluded. Docker `.Image` identifiers and configured registry digests are
  retained as distinct facts; they are not assumed interchangeable.

The wrapper queries only its own isolated project before execution and passes a
bounded allowlisted file read-only; no container receives a Docker socket. Invalid
supplied metadata fails before engine execution. Direct execution integrations
without launch metadata retain process facts and report Docker/image information
as unavailable. No usernames, home paths, IPs, ambient environment dump, hardware
serials or tokens are collected as provenance.

Older reports keep `environment` absent. Export never fills those gaps from the
exporter's machine, current images or current Docker version. Inspection warns
when environment or Docker/image metadata was not recorded. Trace/span IDs remain
references only: full Jaeger traces are not in this format and temporary collector
storage disappears at teardown. Partial phase warnings give recorded/configured
sample counts; cleanup that was not confirmed remains explicit.

This is minimal provenance, not a complete performance environment or attestation.
Images/source trees are not included. Dirty revisions are retained as dirty;
image IDs do not guarantee future image availability. CPU load/resource scheduling,
host hardware and all runtime settings are not reconstructed.

## File safety, portability and limits

CLI export writes a 0600 sibling temporary file, syncs/closes it, then publishes
through a hard link that refuses an existing destination, including symlinks.
There is no overwrite flag or unsafe rename fallback. The parent directory must
already exist and support local hard links/directory sync. Ordinary failure or
observed cancellation before publication cleans temporary files. Cancellation can
race publication; a fully published bundle may remain. Directory-sync failures
are reported as occurring after publication. Output/terminal failures do not delete
valid published evidence. SIGKILL/power loss can leave temporary files; no crash
repair guarantee is claimed.

Payload/work bounds do not guarantee deadlines through stalled filesystem/kernel
operations. NFS/other unusual filesystems and Windows publication were not tested.
Native macOS/ARM64 and Linux/ARM64 offline verification were exercised. Build a
matching-platform verifier before transferring to another architecture; the data
format itself is platform-independent. Existing SQLite durability/recovery limits
still apply to the source history, not to runtime inspection of an exported bundle.

```sh
make check frontend-check
./scripts/experiment.sh test
./scripts/experiment.sh --tracing test
```

See [validation](validation.md) for actual export hashes, clean-commit experiments,
network-disabled verification, browser downloads, failed checks and remaining limits;
[ADR 0005](decisions/0005-portable-evidence-and-provenance.md) records the design.

## Offline comparison — result schema 1

```sh
./artifacts/bin/faultline-bundle compare LEFT.flb RIGHT.flb
# The full native CLI delegates to the same offline command:
./artifacts/bin/faultline bundle compare LEFT.flb RIGHT.flb
```

Each input independently passes the existing archive, checksum, schema and raw-
observation validation. The comparison package revalidates the logical payloads;
it never opens files, queries history or collects environment information. Bundle
format 1 is unchanged. Output has `schema_version: 1` and
`kind: "offline_bundle_comparison"`. It is deterministic for the same ordered
inputs: no current timestamps, paths or verification-machine facts are added.

`left` and `right` identify original history/run IDs, timestamps, status, spec/hash,
cleanup and SHA-256 of original report bytes. These are logical recorded-payload
identities, not whole-ZIP hashes or authenticated executions. Configuration lists
all five properties: spec version, samples, latency, request timeout and canonical
spec hash. `same_spec` requires both equal fields and equal hashes.

`provenance` classifies each recorded property `equal`, `different` or `unknown`,
with nullable left/right values. Two absent fields are **unknown**, never equal.
Legacy revision/Go/Toxiproxy version labels and explicit, non-null telemetry flags
are retained when present. Missing runner OS/architecture, methodology, Docker or
images stay unknown; no data is borrowed from this machine. Image IDs, pinned
references and revision labels are compared literally, without remote resolution.

Every phase contains each side's `samples_recorded`, `samples_configured`,
`availability` (complete/partial/missing), and summary. Zero observations gives a
null summary; an observed zero duration remains a real zero. Summary failure rates
retain their original **fraction** units. `right_minus_left` contains mean/p95
latency changes in **ms**, and failure-rate changes in **percentage points**.
The `absolute` field is the signed difference, not an absolute-value operation.
Relative percentages are `(right-left)/left*100`. They are null with
`relative_status: "zero_reference"` for a zero left value, including 0/0. A
non-finite/overflowing percentage is null with `not_representable`. Otherwise
status is `available`; relative reversal generally is not simple negation because
the reference changes. No rounding or replacement of stored statistics occurs.

### Comparability policy

`assessment` never asserts controlled execution or statistical significance;
`controlled_experiment_established` and `statistical_significance_assessed` remain
false. Differences do not constitute performance-improvement or causal claims.

| Classification | Exact rule |
| --- | --- |
| `not_comparable` | Either report failed/canceled, cleanup is unconfirmed, any phase is incomplete/missing, identities conflict, or known methodologies differ. All cross-run deltas are null; side measurements remain available. |
| `descriptive_only` | Both reports are complete/clean/full, but configuration differs, required provenance is unknown, any known provenance differs, or revision labels are dirty/development/test. Descriptive deltas are available. |
| `recorded_conditions_match` | Both reports are complete/clean/full, specs/hash match, required recorded conditions match, no known provenance differs, and revision labels are clean hex prefixes/hashes. This establishes recorded equality only. |

Required conditions are environment version, revision, runner OS/architecture, Go
version, methodology, tracing mode, Docker server version, sample/Toxiproxy image
IDs and Toxiproxy pinned reference. Jaeger ID/reference are additionally required
only when tracing is explicitly enabled on both sides. An absent local sample
reference or Jaeger-off field remains unknown but is not a required condition.
Any known difference, including an optional property, downgrades matching conditions.

Input contradictions may fail validation before assessment: for example, a report
claiming completion without successful cleanup is invalid evidence.

Current validation supports only `serial-http-nearest-rank-v1`; another recorded
methodology is unsupported evidence and is rejected before comparison. The policy
also withholds deltas if future supported known methodologies differ. Missing
legacy methodology allows descriptive arithmetic with an explicit unknown reason.
Matching hashes do not establish the same environment; matching recorded fields
omit unrecorded load, scheduling, host resources and other confounders.

A reused history/run identifier with changed original report bytes or contradictory
history metadata is an identity conflict. Identical recorded runs are explicitly
labeled `same_recorded_run`: zeros are an identity check, not independent replication.
Different identifiers do not attest that execution was independent.

### Examples and exit codes

```sh
# An eligible full pair; matching conditions require the recorded fields above:
faultline-bundle compare run-a.flb run-b.flb

# A deterministic identity check. Warns same_recorded_run, not replication:
faultline-bundle compare run-a.flb run-a.flb

# Both complete, with different latency/timeout/specs: descriptive_only:
faultline-bundle compare latency-80.flb latency-400.flb

# Complete legacy evidence: unavailable fields stay unknown, descriptive_only:
faultline-bundle compare recorded-environment.flb legacy.flb

# Valid canceled evidence: not_comparable, phase deltas null:
faultline-bundle compare complete.flb canceled.flb
faultline-bundle inspect canceled.flb

# Corrupt/unsupported inputs fail without a comparison result:
faultline-bundle compare complete.flb corrupt.flb
```

Exit 0 means two valid inputs produced a result, including valid non-comparable
pairs. Automation must check `assessment.descriptive_deltas_available` before
using deltas. Exit 1 means invalid/unsupported input, I/O/output failure or
cancellation; exit 2 means usage. No output/export files are created. Inputs are
read sequentially, each retaining the 2 MiB archive/1 MiB report limits; comparison
output contains summaries, not duplicate raw observations/errors. Cancellation is
cooperative and filesystem stalls retain the existing OS limitation. All data
remains local. Use `inspect` for raw observations, original errors and trace IDs.

No new dependency or dashboard/API feature is needed. Existing history/report
comparison now calls the same pure absolute-delta helper, preserving its schema,
fraction-valued failure-rate deltas and behavior. See
[ADR 0006](decisions/0006-offline-evidence-comparison.md) and the validation record
for actual native/network-disabled comparisons and remaining test limits.
