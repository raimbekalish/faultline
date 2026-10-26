# Changelog

No tagged release has been published. This entry is proposed for owner review;
it records implemented behavior, not a fabricated release date.

## 0.1.0 — pending release authorization

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
- Proposed distribution includes separate third-party license/notice texts with
  the packaged usage instructions; current qualification archives need rebuilding.
- Deterministic local packaging, version metadata, SHA-256 manifests and synthetic
  JSON compatibility contracts. Parser regressions cover ambiguous Unicode,
  contradictory ZIP metadata, corruption and resource limits.

Primary proposed release assets are the four offline tools. The macOS ARM64 native
CLI/dashboard remains a separately qualified local candidate, not a recommended
initial public asset. macOS downloads lack Developer ID signing/notarization and
require an explicit owner policy decision. Windows is unsupported. Full fault
injection still requires the source checkout and local Docker Compose. Checksums
are integrity metadata, not authenticity; comparison establishes no significance.
