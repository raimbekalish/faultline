# ADR 0005: Bounded portable evidence and recorded environment facts

- Status: accepted
- Date: 2026-10-24

Version 1 uses a ZIP containing exactly `manifest.json`, `history.json`,
`spec.json`, `report.json`, in that order. Files use Store (no compression), fixed
DOS timestamps and private regular-file permissions. No comments, extras, ZIP64,
encryption, arbitrary names or extraction are supported. The archive is capped
at 2 MiB, report at 1 MiB, metadata/spec at 4 KiB each, JSON depth at 32.

The canonical manifest includes SHA-256 and byte length for all three evidence
files. It cannot authenticate its own author or prevent coordinated rewriting of
content and hashes. The original validated report bytes are retained, not rebuilt
from a projection. The canonical spec/hash and original history identifiers,
status/timestamps are cross-checked. Manifest/history/spec use canonical JSON;
report JSON preserves optional schema-1 fields. Duplicate/case-ambiguous JSON
keys, unsupported versions and inconsistent statistics fail verification.

Move the existing semantic report validator to `internal/record`. History and
portable evidence call the same validator, which reuses the engine summarizer.
No statistical calculation is duplicated. No history schema migration is needed.
An optional `environment` report field preserves existing reports and database
schema 1. Older missing provenance remains missing on export.

Execution records process OS/architecture, reported build revision/Go version,
spec hash, tracing mode and versioned measurement methodology. The existing local
wrapper may supply a strictly bounded allowlisted JSON file with observed Docker
server version and exact container image IDs/pinned references. It reads only its
own Compose project and mounts that one file read-only; no Docker socket enters
containers. Local sample image tags are excluded. Runner OS is not host OS;
Image IDs retain Docker's `.Image` content identifier; configured `@sha256`
references retain registry digests. These identifiers are not assumed interchangeable.
Pinned references are recorded as configured, not as a claim of service-reported
versions. No usernames, paths, addresses, ambient environment dump or hardware
identity is collected. Unavailable launch metadata is omitted, with an explicit
inspection warning. Evidence preserves original raw errors; users must review
historical report contents before sharing.

The existing CLI adds `bundle export/verify/inspect`. A separate CGO-disabled
`faultline-bundle` binary verifies/inspects offline with no SQLite dependency,
Docker access, collector or network calls. Copy a platform-appropriate verifier
with the bundle for offline use; all evidence is self-contained JSON, not external
references except explicitly unavailable historical trace IDs. No full traces
or HTML viewer are added.

Export writes a private sibling temporary file, syncs/closes it and atomically
links it to a new destination without replacement. Existing files/symlinks are
refused. No rename fallback can overwrite a concurrent destination. Link support
on a local filesystem is required. Cancellation before publication removes the
temporary file; cancellation after the commit point may leave a valid published
bundle. Directory-sync failures after publication are reported explicitly. A
crash/power cut can leave a temporary file; no backup or repair guarantee is added.

The dashboard can download the same validated bytes through a same-origin GET
endpoint with existing Host/Origin/CSP/deadline guards. It accepts only a run ID,
never a path. Unfinished intents cannot be exported as completed evidence.
