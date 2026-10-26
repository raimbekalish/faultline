# Changelog

Entries describe version contents, not publication status. Check the repository's
[Releases page](https://github.com/raimbekalish/faultline/releases) for availability
and published assets; no release date is inferred here.

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
