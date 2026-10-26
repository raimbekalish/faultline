# Installation and demonstration — proposed v0.1.0

There is no published v0.1.0 tag or Release yet. These instructions accompany the
release-gate documentation review. Do not use an assumed download URL. After
explicit publication, obtain matching archives and SHA256SUMS from the repository's
official Release. Before publication, use an owner-provided local candidate or the
qualified source snapshot below. The current candidates are qualification
artifacts only: their missing third-party notices and release-facing copy must be
reviewed and rebuilt before distribution.

## Choose the offline tool

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

## Verify and extract a candidate

The qualified candidate names contain version `0.1.0` and revision prefix
`4a2cc2257461`. Pick one matching your target; do not mix revisions. Later approved
rebuilds will have different names/checksums. From the archive's directory:

```sh
# Example target; compare this result with the matching outer SHA256SUMS line:
shasum -a 256 faultline-bundle_0.1.0_4a2cc2257461_darwin_arm64.tar.gz
# Linux uses sha256sum. Hash equality does not authenticate the publisher.
mkdir faultline-v010
cd faultline-v010
tar -xzf ../faultline-bundle_0.1.0_4a2cc2257461_darwin_arm64.tar.gz
shasum -a 256 -c SHA256SUMS
cat VERSION.json
cat USAGE.md
./faultline-bundle --version
```

Extract into a new directory you own. The candidate outer manifest includes the
separately evaluated native package; a proposed primary-only manifest lists exactly
the four offline assets. Verify the relevant line when you have only one archive.
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
is needed for this tool. The following pins the qualified pre-publication source,
not an uncreated tag:

```sh
git clone https://github.com/raimbekalish/faultline.git
cd faultline
git switch --detach 4a2cc225746115403205058d996f5392c30d3b51
mkdir -p artifacts/bin
CGO_ENABLED=0 go build -buildvcs=false -trimpath \
  -ldflags='-X github.com/raimbekalish/faultline/internal/buildinfo.Version=0.1.0 -X github.com/raimbekalish/faultline/internal/buildinfo.Revision=4a2cc225746115403205058d996f5392c30d3b51' \
  -o artifacts/bin/faultline-bundle ./cmd/faultline-bundle
./artifacts/bin/faultline-bundle --version
```

The final reviewed release SHA must replace this candidate pin before publication
if documentation changes are merged. No security settings are modified. A clean
tracked-source export/build and real bundle verification passed during qualification.

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
this qualification: 80 ms fault mean 82.613667 ms, 0/10 failures; 400 ms fault mean
252.369658 ms, 5/5 HTTP504; baseline/recovery all successful, cleanup confirmed.
These are recorded examples, not predictions or causal/significance claims.
Temporary Jaeger traces disappear at teardown; references do not keep them alive.

## Native package decision

The local macOS ARM64 CLI/dashboard candidate passed history/Chrome tests on macOS
26.6.2, but its CGO executables embed minimum macOS **26.0** / SDK 26.5. The offline
binaries' embedded 12.0 minimum is different and is not a tested-old-OS claim.
Unsigned distribution and this narrow native OS qualification make the local
package unsuitable as a default first-release asset. Keep it separate unless the
owner explicitly accepts the limitations and approves a qualified distribution.
