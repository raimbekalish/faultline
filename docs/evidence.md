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

## Offline reliability gates — Unreleased

Milestone 7 adds `evaluate` in source. **v0.1.0 does not contain this command**;
its tag, binaries, assets and format-1 encoding remain unchanged. Build the current
source (`make offline`), then use this exact positional syntax:

```sh
./artifacts/bin/faultline-bundle evaluate RUN.flb --policy reliability.json
```

`RUN.flb` independently passes the existing archive, checksum, schema and
raw-sample/summary validators. Only then is the local policy decoded and validated.
The pure evaluator revalidates both logical inputs. It never changes the bundle,
repairs measurements, reads history, contacts Jaeger, or captures machine facts.
No Docker, SQLite, network, account, API key or environment configuration is needed.

### Policy version 1

```json
{
  "schema_version": 1,
  "require_complete": true,
  "require_cleanup": true,
  "phases": {
    "baseline": { "min_samples": 5, "max_failure_rate": 0, "max_p95_ms": 50 },
    "fault": { "min_samples": 5, "min_failure_rate": 0.9 },
    "recovery": { "min_samples": 5, "max_failure_rate": 0, "max_p95_ms": 50 }
  }
}
```

This illustrative policy asks whether an intentional fault produced failures and
baseline/recovery succeeded within selected latency limits. It is not a universal
default. [Example policies](../examples/policies/) ask two different questions;
select thresholds appropriate to the experiment rather than a desired verdict.

All four top-level fields are required. Both lifecycle flags must be literally
true: version 1 has no unsafe opt-out. At least one of `baseline`, `fault` and
`recovery` must contain a condition; configured phases cannot be empty. Unrequested
phases have no threshold verdicts, but a conclusive run still requires completed
execution and cleanup. Up to 15 phase conditions plus two lifecycle requirements
bound the result to 17 checks.

| Phase field | Bound and unit | Validation |
| --- | --- | --- |
| `max_failure_rate` | actual ≤ threshold, **fraction** | finite number, 0–1 |
| `min_failure_rate` | actual ≥ threshold, **fraction** | finite number, 0–1; cannot exceed maximum if both exist |
| `max_mean_ms` | actual ≤ threshold, end-to-end **ms** | finite, nonnegative number |
| `max_p95_ms` | actual ≤ threshold, end-to-end **ms** | finite, nonnegative number |
| `min_samples` | actual ≥ threshold, **samples** | integer literal, 1–100; a sufficiency requirement |

Every bound is inclusive. There is no epsilon or display rounding in decisions.
Numbers use float64 semantics; a nonzero decimal that would underflow to zero is
rejected. After semantic validation, metrics come from the **existing engine
summarizer over raw samples**, including failed requests and nearest-rank p95.
This avoids decisions on a stored mean's permitted four-ULP serialization
allowance, without modifying that stored report or existing comparison semantics.
No new statistics algorithm or statistical-significance test is introduced.

Policies are at most **8 KiB**, including whitespace, and contain no includes,
expressions, endpoints or paths. Unknown fields, case aliases, escaped/simple-fold
duplicates, null fields, invalid UTF-8/surrogates, malformed numbers, infinity,
NaN, overflow, negative limits, conflicting rates and structural emptiness are
errors. Explicit permissive boundary rates 0/1 remain legitimate user choices.

### Verdicts, availability and precedence

| Overall status | Exact meaning | Exit |
| --- | --- | --- |
| `PASS` | Completed, cleaned run; every requested phase is fully measured, satisfies any minimum count and passes every selected threshold. | 0 |
| `FAIL` | Evidence meets those prerequisites, but at least one selected numeric bound is violated. | 3 |
| `INDETERMINATE` | Valid evidence lacks a required prerequisite: failed/canceled execution, unconfirmed cleanup, missing/partial requested phase, or fewer samples than policy demands. | 4 |

INDETERMINATE takes precedence over FAIL. The result still shows a conclusive
failure in another sufficiently measured phase, but cannot conceal the missing
prerequisite. Failed/canceled reports never produce conclusive numeric check
verdicts, even when some phases are complete. Their actual recorded values remain
inspectable. Confirmed cleanup alone cannot turn a canceled run into PASS.

A phase is `complete` only when observed count equals the specification's configured
count; it is `partial` for a nonzero smaller count, and `missing` for zero samples.
Partial samples cannot satisfy a metric check merely by meeting `min_samples`.
For a missing phase, count zero is a real count; rate/latency values are **null**.
An observed zero rate or duration remains zero. A completed ten-sample run evaluated
under `min_samples: 20` is indeterminate rather than a performance failure.

Invalid evidence is different: a report falsely claiming completion with missing
samples or unsuccessful cleanup fails the existing semantic validator and is an
**error**, not INDETERMINATE. Unfinished history intents have no final report to
export; evaluation does not infer their outcome.

### Result schema 1

Output is a single JSON object with a newline, in fixed order:

- `schema_version: 1`, `kind: "offline_reliability_evaluation"`,
  `policy_schema_version: 1`, and `policy_sha256` of the validated typed policy's
  fixed-order Go JSON encoding (not RFC 8785 or authentication).
- `evidence`: format version, history/run IDs, recorded run status, spec hash and
  original report SHA-256. No file paths, timestamps, raw errors, trace payloads
  or ambient environment details are copied into the result.
- `status`, two boolean `requirements`, and requested `phases` in
  baseline/fault/recovery order. Each phase carries observed/configured counts,
  availability, status and checks. Check order is minimum samples, maximum rate,
  minimum rate, maximum mean and maximum p95, skipping unrequested conditions.
- Each check has `condition`, nullable `actual`, `threshold`, `unit`, `operator`,
  `status` and `reason`. Units are `samples`, `fraction` or `ms`.
- Fixed notes explain scope, all-outcome measurements and missing data. Hashes
  identify consistent content, not authenticity or execution attestation.

Stable reason codes are `requirement_satisfied`, `run_not_complete`,
`cleanup_unconfirmed`, `bound_satisfied`, `bound_violated`, `no_observations`,
`phase_incomplete` and `insufficient_samples`. A reason plus the condition,
actual value, operator and threshold identifies exactly what was not met.

Valid verdicts write JSON to stdout and nothing to stderr. Malformed/unsupported
bundles or policies, I/O failures, cancellation and output errors exit **1** with
stderr; usage exits **2**. No result is written for errors before output begins.
A failed stdout write can already contain partial bytes and overrides a verdict
code. Existing verify/inspect/compare outputs and exit meanings are unchanged;
in particular, a valid non-comparable `compare` result still exits 0.

### Examples

```sh
# Both user-selected policies require completed execution and confirmed cleanup.
faultline-bundle evaluate latency-80.flb --policy examples/policies/dependency-budget.json
faultline-bundle evaluate latency-400.flb --policy examples/policies/dependency-budget.json
faultline-bundle evaluate latency-400.flb --policy examples/policies/expected-timeouts.json
faultline-bundle evaluate canceled.flb --policy examples/policies/dependency-budget.json
```

For the retained real demonstration runs, the dependency budget yields PASS for
80 ms and FAIL for 400 ms; the expected-timeout policy yields PASS for 400 ms and
FAIL for 80 ms. A retained genuine canceled run yields INDETERMINATE, with recovery
metrics null. These are observations for those recordings, not promised outcomes
for future runs. Exact measurements and exit codes are in the validation record.
Evaluation makes no causal, significance, production-readiness or SLO claim beyond
whether the explicit conditions held for recorded samples.

Only explicitly selected bounded regular local files are opened; final symlinks
are refused. Darwin/Linux additionally use no-follow/nonblocking open and verify
file identity, covering substitution between stat/open. Parent directories resolve
normally and must be trusted. There is no path traversal driven by policy contents.
Cancellation is cooperative; kernel filesystem stalls and arbitrary output writers
retain OS limitations. Concurrent in-place mutation is not an attested snapshot;
evaluate stable inputs. No output files or network connections are created;
input handles are opened and closed by the CLI. See
[ADR 0008](decisions/0008-offline-reliability-gates.md).

## Release compatibility and parser profile

`faultline-bundle --version` identifies the executable; it does not replace or
amend provenance inside the input report. Local candidate installation, platform
evidence, manifest verification and runtime dependencies are described in
[release preparation](release.md). Existing verify/inspect/compare JSON and format 1
are frozen by explicitly synthetic compatibility fixtures; actual experiments
remain separate validation evidence.

The version-1 boundary rejects Unicode simple-fold-equivalent duplicate fields,
unpaired JSON UTF-16 escapes and contradictory ZIP disk/version/local descriptor
metadata. Earlier legitimate exports remain valid; previously tolerated malformed
archives are intentionally rejected. See [security limits](../SECURITY.md).
