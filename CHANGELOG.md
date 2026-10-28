# Changelog

Entries describe version contents, not publication status. Check the repository's
[Releases page](https://github.com/raimbekalish/faultline/releases) for availability
and published assets; no release date is inferred here.

## Unreleased

- M8B2B: fixed account-private supervisor authority, durable reservation history,
  cooperative process leases, bounded exact-ID read-only observations and cleanup
  event reserves. Opaque mutation plans have no executable driver; production
  remains plan-only. Existing plan/journal wire formats and v0.1.0 are unchanged.
- Explicit legacy consumed-identity import is a blocking tombstone, never cleanup
  authority. Missing/partial state and uncertified success stay unresolved.

- M8B2A: deterministic closed lab resource graph, conservative ownership/cleanup
  journal state machine and bounded private atomic journal snapshots. Production
  CLI remains plan-only; no Docker mutation, worker or runtime qualification.
- Correct top-level CLI usage to include the existing `lab` command. Document
  read-only bridge diagnostics, supporting idle lifecycle evidence and unknown
  exact causal/caller attribution.

- M8B1: strict `http-work-v1` descriptors, closed catalog and read-only
  `faultline lab plan` with private no-replace plans/redacted summaries. No
  custom run, cleanup, worker execution or runtime qualification is enabled.
- Offline reliability gates: strict version-1 local policies and deterministic
  per-condition PASS/FAIL/INDETERMINATE JSON with separate verdict exit codes.
- Inclusive failure-rate, mean/p95 latency and sample-sufficiency bounds;
  mandatory completed-run/cleanup evidence and conservative partial handling.
- Bounded policy parsing, semantic/format-1 validator reuse and original command
  compatibility. No database/bundle/report/specification migration or new dependency.

These additions require a source build and are not part of v0.1.0 or its assets.

## 0.1.0

- Fixed-topology local latency experiments with baseline, fault and recovery
  measurements, bounded requests, owned cleanup and cancellation handling.
- Optional OpenTelemetry trace propagation/correlation and temporary local Jaeger.
- Strict versioned specifications, SQLite history, failed/canceled/unfinished
  states, rollback/recovery tests and raw-sample consistency validation.
- Read-only local dashboard for real history, observations and comparisons.
- Format-1 portable evidence, privacy-conscious recorded provenance, bounded
  archive verification and deterministic offline descriptive comparison.
- Four runtime-qualified offline candidate targets; execution through Rosetta or
  Docker emulation is explicitly distinguished from native hardware testing.
- Distribution instructions include separate third-party license/notice texts.
  The historical 2026-10-25 qualification archives lack them and must not be distributed.
- Deterministic local packaging, version metadata, SHA-256 manifests and synthetic
  JSON compatibility contracts. Parser regressions cover ambiguous Unicode,
  contradictory ZIP metadata, corruption and resource limits.

The primary binary scope is the four offline tools. The macOS ARM64 native
CLI/dashboard remains a separately qualified local candidate, not a recommended
initial public asset. macOS downloads lack Developer ID signing/notarization and
require an explicit owner policy decision. Windows is unsupported. Full fault
injection still requires the source checkout and local Docker Compose. Checksums
are integrity metadata, not authenticity; comparison establishes no significance.
