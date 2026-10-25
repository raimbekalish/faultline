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
