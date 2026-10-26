# Security boundaries and limitations

Faultline is early-stage local developer tooling. It is not hardened production
chaos infrastructure, a multi-user service, an attestation system or a sandbox for
hostile local processes. Use the fixed sample topology and trusted local build
assets/database directories. Do not expose the API, collector or control ports.

## Untrusted evidence boundary

The offline parser never extracts bundles or accesses history/collectors. Format 1
accepts exactly four ordered regular stored ZIP entries, one disk, no ZIP64,
compression, comments, extras, prefix/trailing/hidden data, traversal or symlinks.
It cross-checks offsets, local/central/descriptor metadata, CRC, sizes, digests,
canonical metadata/specification and sample-derived statistics. Bounds: 2 MiB
archive, 1 MiB report, 4 KiB other entries, JSON depth 32, at most 100 samples/phase.
These bounds limit work; they are not a constant-memory or constant-time claim.

JSON rejects invalid UTF-8, unpaired escaped UTF-16 surrogates, trailing values and
Unicode simple-fold-equivalent duplicate keys, including escaped names. Optional
report fields remain preserved for format-1 compatibility. Source fixtures are
synthetic; actual measured evidence is kept separately.

Files must be bounded regular inputs, not symlinks/devices/FIFOs. Identity is
rechecked after opening. Cancellation is cooperative; filesystem syscalls/stalls
and a hostile process replacing filesystem entries cannot be made a reliable
security boundary by context checks. Use trusted local directories/filesystems.
Exports use synced private files and no-replace hard links. Existing files are
never deliberately overwritten; directory sync/power-loss guarantees vary by OS/FS.
Treat text in untrusted reports as untrusted content in terminals and viewers.

SHA-256/CRC establish internal consistency, not authenticity, independent execution
or protection from coordinated rewriting of evidence and hashes. Recorded
provenance is not signed attestation. Package hashes have the same limitation.
Temporary Jaeger traces are not retained or downloaded during verification.

## Review performed

Milestone 6A's targeted adversarial review reproduced and fixed Unicode duplicate/
surrogate ambiguity and contradictory ZIP disk/version/local metadata acceptance.
Regression tests, source review, short bounded fuzz runs and isolated no-network
runtime checks passed. This is **not a comprehensive independent security audit**;
no guarantee covers every archive parser, filesystem, SDK or dependency defect.
Native local packages are unsigned/unnotarized; no Windows runtime is qualified.

## Reporting a concern

Do not post tokens, personal reports, private databases or identifying environment
data in public issues. A dedicated private disclosure channel is not configured.
Coordinate sensitive disclosure with the repository owner through their GitHub
profile before sharing details; public issues should use a minimal privacy-safe
reproduction. No response-time or security-maintenance SLA is currently promised.
