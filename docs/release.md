# Release preparation

This guide describes the publication process, not current release availability.
Check the repository's [Releases page](https://github.com/raimbekalish/faultline/releases).
See [release notes](releases/v0.1.0.md), [installation/demo](install.md), and
[dated validation records](validation.md).

## Historical pre-publication gate — 2026-10-25 PDT

PR #6 had been merged. At this gate no tag, GitHub Release or asset upload existed.
Local `0.1.0` candidates were built from clean main
`4a2cc225746115403205058d996f5392c30d3b51`, without a tag; earlier `0.1.0-rc.1`
artifacts were separate. These are historical review artifacts, not final assets.

The gate found missing third-party notices and release-copy problems. Those archives
have only Faultline's own MIT license and must not be distributed. The USAGE.md
appendix supplies SDK/module/runtime license texts through the existing packager.
Before any publication, review the intended source, build/reverify from its approved
merge revision and record the actual source/asset inventory. Do not relabel older
packages or infer a final release SHA from this historical checkpoint.

## Distribution boundaries

- `faultline-bundle`: CGO-disabled Go executable, no SQLite driver/history or
  `database/sql` dependency. It validates, inspects and compares regular bundles
  without an account, Docker, collector or network. It never captures the current
  machine's experiment provenance. JSON output and schema-1 semantics are shared.
- `faultline-local`: native CGO CLI, offline tool, dashboard, matching production
  static assets and example specifications. SQLite's amalgamation is compiled in;
  system SQLite is not loaded. Native OS facilities/libc still apply. Existing
  local history needs a writable directory for hot-journal recovery. No Go/C/Node
  compiler is required to use a built package.
- Full fault injection uses the exact source checkout, wrapper and pinned Compose
  images. The native package does not orchestrate the isolated sample network.
  It is not a Docker-free full platform. First use downloads images/modules;
  experiment execution is distinct from the offline package workflow.

## Build local candidates

Build prerequisites: Git, Python 3.10+ (3.12.6 tested), Go 1.26.8; `--local`
additionally needs a native C compiler, Node 22.12+ and npm (24.11.0 Node and npm 11.6.1 tested).
Use the repository-local Git identity and a clean committed worktree. No tag is
needed. Do not replace someone else's candidate output directory.

```sh
# From reviewed, clean source; no publication side effects:
python3 scripts/package-release.py --version 0.1.0 \
  --out artifacts/release-candidates/my-candidate --local
```

`FAULTLINE_GO=/absolute/path/to/go` selects the compiler. Standard GOPATH,
GOMODCACHE and GOCACHE overrides are honored. The script builds all four pure-Go
matrix targets and optionally the **build host's** native local package. It does
not pretend a host C compiler can cross-build SQLite for other operating systems.
`--local` installs locked frontend dependencies with lifecycle scripts disabled
and builds assets afresh; it never packages an unchecked stale `web/dist`.

Outputs have predictable names:
`COMPONENT_VERSION_REVISION12_OS_ARCH.tar.gz`, plus outer `SHA256SUMS`. Each archive
contains only known binaries/assets, LICENSE, USAGE.md, VERSION.json and member
SHA256SUMS. No source data, databases, traces, secrets, caches or test logs.
`--version` prints version, full revision, Go version, OS/arch and CGO setting as
JSON, without opening history, listening or starting an experiment.

Builds use `-trimpath`, disabled automatic VCS stamping and explicit revision/version
linker flags; build IDs are omitted. Tar headers are sorted regular files with
fixed ownership/modes and the real source commit timestamp; gzip has no filename
and uses that timestamp. This changes no Git author dates. The frontend lock hash
and compiler executable's Go version are recorded in package metadata. Temporary
build trees are cleaned; output directories are exclusively created and files are
published with no-replace hard links. Existing directories are refused.

In the 2026-10-25 qualification, repeated candidate builds were byte-identical on
the recorded machine/toolchain.
This is **not** a claim of reproducibility across C compilers, SDKs, OS versions,
Node toolchains or independent builders. Native toolchain/SDK OS compatibility needs further qualification. Inspection of
the final candidate shows native CGO minimum macOS 26.0 / SDK 26.5, while offline
macOS binaries embed 12.0; that is build metadata, not old-OS runtime coverage.
ARM64 has linker ad-hoc signatures; AMD64 offline is unsigned. No executable has
Developer ID signing/notarization. Gatekeeper assessment rejected the candidates;
no downloaded/quarantined execution journey was qualified. Checksums
are consistency checks, not publisher authentication. No security-policy bypasses.

## Platform evidence

The runtime and binary-inspection evidence below is historical: 2026-10-25 PDT.
See dated validation records for any later qualification.

| Target | Offline candidate | Native CLI/dashboard candidate | Evidence/limits |
| --- | --- | --- | --- |
| macOS ARM64 | Runtime-tested locally | Runtime-tested, withhold by default | macOS 26.6.2; native minimum 26.0; Gatekeeper assessment rejected |
| macOS AMD64 | Runtime-tested with Rosetta 2 | Not packaged/tested | No Intel hardware or broad macOS version claim |
| Linux ARM64 | Runtime-tested | Not packaged/tested | ARM64 Linux VM, read-only scratch, network disabled, 64 MiB |
| Linux AMD64 | Runtime-tested with Docker emulation | Not packaged/tested | Same isolation; no native x86 hardware claim |
| Windows AMD64 | Evaluation cross-build only; unsupported | Unsupported | No Windows runtime/filesystem/signal verification; no distribution archive |

All four offline cross-builds ran verify/inspect/compare on actual exported
experiments. No four-target offline package remains build-only in this validation;
AMD64 execution is explicitly emulated. Successful compilation alone would only
qualify as build-only. Linux CGO engine/history behavior is separately exercised by
ARM64 container integration tests; that does not qualify a native Linux local
package or a Linux-host wrapper installation. The source wrapper's host journey
was run on macOS ARM64. No Windows support is promised.

## Clean installation journey

From the directory containing a candidate archive and the outer SHA256SUMS:

```sh
# Check the archive against its matching line in the outer SHA256SUMS:
shasum -a 256 faultline-bundle_VERSION_REVISION_OS_ARCH.tar.gz
# Linux uses sha256sum. Hash equality does not authenticate the publisher.
mkdir faultline-candidate
cd faultline-candidate
tar -xzf ../faultline-bundle_VERSION_REVISION_OS_ARCH.tar.gz
shasum -a 256 -c SHA256SUMS
cat USAGE.md
./faultline-bundle --version
./faultline-bundle verify /absolute/path/to/left.flb
./faultline-bundle inspect /absolute/path/to/left.flb
./faultline-bundle compare /absolute/path/to/left.flb /absolute/path/to/right.flb
```

Use actual filenames, not the placeholders above. Complete/partial observations,
nulls and comparability reasons remain evidence, not generated diagnosis. Compare
exit 0 includes valid `not_comparable` pairs; check eligibility before using deltas.
The candidate's USAGE.md includes native history/dashboard commands.

The validation journey extracted into private temporary directories, checked both
manifests, read instructions, ran with PATH lacking Docker/SQLite/toolchain programs,
and verified original evidence hashes stayed unchanged. Linux scratch images
included only the binary and two actual bundles; `--network none --read-only
--memory 64m --cpus .5 --pids-limit 32 --cap-drop ALL` was enforced. Test images,
containers, contexts and native listeners were removed. Actual evidence,
candidate packages, screenshots and validation logs are retained under ignored
`artifacts/`, not source control.

## Reproducible source demonstration

Requires the full-platform prerequisites above. Use a **new** database/output
location; existing evidence must not be overwritten. Both runs tear down their
owned stacks before the dashboard opens:

```sh
mkdir -p artifacts/demo
./scripts/experiment.sh run -spec examples/latency.json \
  -db artifacts/demo/history.sqlite > artifacts/demo/latency.json
./scripts/experiment.sh --tracing run -spec examples/timeouts.json \
  -db artifacts/demo/history.sqlite > artifacts/demo/timeouts.json
make cli offline
./artifacts/bin/faultline history list -db artifacts/demo/history.sqlite
# Use two history IDs from list:
./artifacts/bin/faultline bundle export -db artifacts/demo/history.sqlite \
  -out artifacts/demo/left.flb LEFT_ID
./artifacts/bin/faultline bundle export -db artifacts/demo/history.sqlite \
  -out artifacts/demo/right.flb RIGHT_ID
./artifacts/bin/faultline-bundle compare artifacts/demo/left.flb artifacts/demo/right.flb
./scripts/dashboard.sh -db artifacts/demo/history.sqlite
```

The 80 ms and 400 ms specifications differ; the traced second run also differs in
tracing mode. Expect a descriptive comparison, not a controlled causal claim. A
250 ms frontend dependency deadline produces fault-phase 504s when injected latency
exceeds it. Baseline/recovery success and cleanup are verified, not assumed.
Use [validation](validation.md) for actual measurements rather than copying
numbers into a new experiment. Read [evidence](evidence.md) for units/unknowns.

Real-data browser checks can preserve prior screenshots/test outputs:

```sh
FAULTLINE_TEST_URL=http://127.0.0.1:PORT \
FAULTLINE_SCREENSHOT_DIR="$PWD/artifacts/my-browser-evidence" \
FAULTLINE_BROWSER_OUTPUT="$PWD/artifacts/my-browser-results" \
  npm --prefix web run test:browser
```

## Release scope recommendation

Prefer the four offline packages as primary binaries once the documentation and
unsigned macOS policy are approved. Keep the native macOS ARM64 package separate;
its minimum OS and Gatekeeper restrictions limit a first-time installation. Other
native targets and Windows are excluded. The historical candidate master SHA256SUMS
listed five archives. A four-asset publication must use a separately checked manifest listing exactly those four,
not a manifest requiring the withheld archive.

## Release gate

Before publication, review the intended source/documentation and approve the
release scope/version. Build from that approved merge revision, rerun package,
checksum and clean-install checks, decide the unsigned distribution policy, and obtain explicit tag/Release publication approval.
No production-readiness, statistical confidence, exhaustive parser-audit or
untested operating-system claim follows from the current checks. See [SECURITY.md](../SECURITY.md),
[ADR 0007](decisions/0007-release-contracts-and-packaging.md), and validation.
