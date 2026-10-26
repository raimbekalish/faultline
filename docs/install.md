# Installation and demonstration — v0.1.0

Check the repository's [Releases page](https://github.com/raimbekalish/faultline/releases)
for actual v0.1.0 availability; these instructions do not assert that a tag or
Release exists. For a published release, obtain its matching archive and outer
SHA256SUMS. Before publication, use reviewed source as described below or an
owner-approved candidate. Do not use an assumed download URL or unreviewed PR head.
The historical 2026-10-25 qualification archives lack required notices and must
not be distributed; see [the dated validation record](validation.md).

## Choose the offline tool

Runtime evidence below is from the 2026-10-25 qualification; it is not a claim
that every subsequent artifact or older OS version has been tested.

| Package suffix | Runtime qualification | Limit |
| --- | --- | --- |
| `darwin_arm64` | Native macOS ARM64 | macOS 26.6.2 tested; no broad OS-version promise |
| `darwin_amd64` | Rosetta 2 on macOS ARM64 | No physical Intel qualification |
| `linux_arm64` | ARM64 Linux VM | No broad Linux distribution qualification |
| `linux_amd64` | Docker AMD64 emulation | No native x86 hardware qualification |

All are CGO-disabled. The existing-bundle workflow needs no Docker, SQLite,
collector, Go installation, network or account. Windows is unsupported.

**macOS policy limitation:** no package has Developer ID signing or notarization.
The ARM64 executable has a linker ad-hoc signature; AMD64 is unsigned. Gatekeeper
assessment rejected the candidates even though local unquarantined execution
worked. We did not qualify an externally downloaded/quarantined execution journey.
Do not disable security policy automatically. If blocked, follow organizational
policy and consider building reviewed source locally. Source building does not
imply publisher authentication or guaranteed permission under managed policies.

## Verify and extract a package

Choose your target's exact archive filename from the actual Release or approved
candidate inventory, and use its matching outer manifest. Do not mix revisions.
Replace EXACT_ARCHIVE_FILENAME.tar.gz below; it is a placeholder, not a download
name or a guessed release revision. From the archive's directory:

```sh
# Compare this result with the matching outer SHA256SUMS line:
archive_file='EXACT_ARCHIVE_FILENAME.tar.gz'
shasum -a 256 "$archive_file"
# Linux uses sha256sum. Hash equality does not authenticate the publisher.
mkdir faultline-v010
cd faultline-v010
tar -xzf "../$archive_file"
shasum -a 256 -c SHA256SUMS
cat VERSION.json
cat USAGE.md
./faultline-bundle --version
```

Extract into a new directory you own. A release manifest must list exactly its
published asset selection; do not expect the withheld native package. Historical
candidate manifests can include it. Verify the relevant line when you have only
one archive.
The member manifest is checked from inside the extracted package. Checksums detect
accidental inconsistency, not coordinated replacement of assets and hashes.

Inspect real evidence copied from another machine:

```sh
./faultline-bundle verify /absolute/path/to/left.flb
./faultline-bundle inspect /absolute/path/to/left.flb
./faultline-bundle compare /absolute/path/to/left.flb /absolute/path/to/right.flb
```

Inputs must be regular `.flb` files. Inspect includes actual samples, status,
cleanup, provenance and trace references; missing evidence is not reconstructed.
Compare uses right-minus-left deltas in ms and failure-rate percentage points.
Undefined relative changes are null. Exit 0 includes `not_comparable`: check
eligibility and reasons before using deltas. Recorded condition equality does not
prove controlled execution, independent replication or statistical significance.
See [evidence semantics](evidence.md).

## Reviewed-source fallback

Build prerequisites: Git and Go 1.26.8. A first build can need module downloads;
this is distinct from the network-independent executable. No C compiler or SQLite
is needed for this tool. Before publication, obtain the full reviewed commit SHA
from the maintainer. After publication, use the source revision recorded in the
release's VERSION.json and cross-check it against the official source/release
record. Checksums and metadata do not authenticate that source. Replace the
FULL_REVIEWED_COMMIT_SHA placeholder below; do not guess a final SHA or assume
that the current main branch is the release source:

```sh
(
set -eu
git clone https://github.com/raimbekalish/faultline.git
cd faultline
reviewed_revision='FULL_REVIEWED_COMMIT_SHA'
git switch --detach "$reviewed_revision"
test "$(git rev-parse HEAD)" = "$reviewed_revision"
source_revision=$(git rev-parse HEAD)
mkdir -p artifacts/bin
CGO_ENABLED=0 go build -buildvcs=false -trimpath \
  -ldflags="-X github.com/raimbekalish/faultline/internal/buildinfo.Version=0.1.0 -X github.com/raimbekalish/faultline/internal/buildinfo.Revision=$source_revision" \
  -o artifacts/bin/faultline-bundle ./cmd/faultline-bundle
./artifacts/bin/faultline-bundle --version
)
```

No security settings are modified. The 2026-10-25 qualification checked a clean
tracked-source export/build and real bundle verification; that historical snapshot
is not a permanent installation pin. Build only the reviewed revision you selected.

## Full experiment demonstration

Prerequisites: the source checkout, Git, a POSIX shell, local Docker/Compose with a
Unix socket, and permission to run its isolated sample. Images/build dependencies
may need network access initially. No Docker socket is mounted into containers.
Use a **new** output directory and retain your evidence:

```sh
mkdir -p artifacts/demo-v010
./scripts/experiment.sh run -spec examples/latency.json \
  -db artifacts/demo-v010/history.sqlite > artifacts/demo-v010/latency.json
./scripts/experiment.sh --tracing run -spec examples/timeouts.json \
  -db artifacts/demo-v010/history.sqlite > artifacts/demo-v010/timeouts.json
```

Each command tears down its owned stack. For export/dashboard from source,
Go/C and Node/npm are additional build prerequisites; they are not needed for
standalone offline bundle inspection:

```sh
make cli offline
./artifacts/bin/faultline history list -db artifacts/demo-v010/history.sqlite
```

Choose the 80 ms and 400 ms history IDs from that output, replacing LEFT_ID/RIGHT_ID:

```sh
./artifacts/bin/faultline bundle export -db artifacts/demo-v010/history.sqlite \
  -out artifacts/demo-v010/left.flb LEFT_ID
./artifacts/bin/faultline bundle export -db artifacts/demo-v010/history.sqlite \
  -out artifacts/demo-v010/right.flb RIGHT_ID
./artifacts/bin/faultline-bundle compare artifacts/demo-v010/left.flb artifacts/demo-v010/right.flb
./scripts/dashboard.sh -db artifacts/demo-v010/history.sqlite -ttl 30m
```

Open the exact printed loopback URL. The dashboard reads persisted evidence with
experiment services already stopped. Ctrl-C requests shutdown; lifetime is bounded.
No browser experiment execution or arbitrary targets exist.

The demonstration differs in configuration, sample count and tracing, so expect
`descriptive_only`. A 400 ms injection exceeds the frontend's 250 ms dependency
deadline; actual HTTP outcomes and recovery must be inspected, not assumed. During
the historical 2026-10-25 qualification: 80 ms fault mean 82.613667 ms, 0/10
failures; 400 ms fault mean 252.369658 ms, 5/5 HTTP504; baseline/recovery all successful, cleanup confirmed.
These are recorded examples, not predictions or causal/significance claims.
Temporary Jaeger traces disappear at teardown; references do not keep them alive.

## Native package decision

The historical 2026-10-25 macOS ARM64 CLI/dashboard candidate passed tests on macOS
26.6.2, but its CGO executables embed minimum macOS **26.0** / SDK 26.5. The offline
binaries' embedded 12.0 minimum is different and is not a tested-old-OS claim.
Unsigned distribution and this narrow native OS qualification make the local
package unsuitable as a default first-release asset. Keep it separate unless the
owner explicitly accepts the limitations and approves a qualified distribution.
