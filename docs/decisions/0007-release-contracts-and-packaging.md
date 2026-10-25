# ADR 0007 — Explicit release boundaries, frozen contracts and local candidates

Status: accepted for Milestone 6A; publication remains owner-approved.

## Context

An externally usable candidate needs verified installable artifacts, stable JSON
and an honest platform matrix. Cross-compiling a Go executable does not exercise
signals, filesystem behavior, libc, SQLite or browser interactions on that target.
The offline tool must remain usable independently of the experiment runtime.

## Decision

Distribute two separately named candidate components. The CGO-disabled offline tool
cross-builds for macOS/Linux ARM64/AMD64. A local package builds the native Go/CGO
CLI/dashboard plus freshly built locked frontend assets for the build host only.
The source wrapper remains the full experiment entry point. Avoid extra packaging
or UI dependencies, cross-C toolchains, Windows promises and hosted release jobs.

Use a Python-standard-library packager with explicit clean source revision/version,
sorted regular tar members, real source commit epoch and deterministic gzip.
Version commands describe the executable, not experiment provenance. Include MIT
license, usage, metadata and SHA-256 manifests. Publish only under new ignored local
directories; no tag or remote asset API is called. A local native build still
inherits C compiler/SDK/OS requirements and unsigned-download policy limitations.

Freeze small privacy-safe, explicitly synthetic format-1 source fixtures and
expected offline/history/API JSON. Fixed IDs/times test semantics/hash identity;
ignore whitespace only. A deterministic archive digest protects the writer profile.
No automatic snapshot-updater can casually bless incompatible output. Real
persisted records and actual browser/network-disabled package tests complement,
rather than get replaced by, these fixtures.

Tighten the existing parser without a format migration: reject Unicode-folded
aliases as duplicate keys, unpaired JSON surrogates, nonzero central entry disks,
unsupported/conflicting ZIP reader versions and contradictory local sizes/CRC when
a descriptor is present. Keep all existing valid format-1 archives compatible;
malformed permissive-parser cases are intentionally no longer accepted. No new
statistical implementation, history migration, provenance capture or API mutation.

## Consequences

Every platform is labeled by actual execution evidence: native, VM/emulated,
build-only or unsupported. AMD64 emulation is not physical Intel qualification.
Windows evaluation builds are not packaged. Native Linux/AMD64 local packages need
additional builders/runtime checks before being offered. Repeated archives match
on the tested toolchain, not necessarily across independent C/SDK environments.

Checksums are not authenticity/signatures. The parser review and bounded fuzzing
are limited, not an audit. Before a first tag, review/approve this PR, regenerate
packages from the final approved merge revision, verify the installation journey
and obtain explicit publication approval. No CI or release automation is enabled.
