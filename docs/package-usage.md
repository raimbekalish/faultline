# Faultline candidate package

This is a local release candidate, not a published/signed release. Read VERSION.json
for the source revision, target and build toolchain. Check the outer SHA256SUMS
before extraction (`shasum -a 256 ARCHIVE.tar.gz` on macOS, `sha256sum` on Linux),
then check this package's SHA256SUMS after extraction. Hashes detect inconsistency;
they do not authenticate the publisher. Extract into a new directory you own.

## Portable offline tool (all package variants)

No installation, Docker, SQLite, collector, network or account is needed:

    ./faultline-bundle --version
    ./faultline-bundle verify /absolute/path/to/left.flb
    ./faultline-bundle inspect /absolute/path/to/left.flb
    ./faultline-bundle compare /absolute/path/to/left.flb /absolute/path/to/right.flb

Inputs must be regular files, not symlinks. JSON goes to stdout; errors go to stderr.
Compare exit 0 includes valid non-comparable evidence: inspect
`assessment.descriptive_deltas_available` and `assessment.classification`.
Right-minus-left changes use milliseconds and failure-rate percentage points;
relative percentages are null for zero references. Missing measurements are not
zeros. Recorded equality does not establish causality, independent execution or
statistical significance. Trace IDs are references, not bundled Jaeger traces.
Use trusted terminals/viewers for untrusted report text.

## Local package only: history and dashboard

From this extracted directory (no Go, C compiler or Node required at runtime):

    ./faultline --version
    ./faultline history list -db /absolute/path/to/history.sqlite
    ./dashboard --version
    ./dashboard -db /absolute/path/to/history.sqlite -assets ./web/dist -ttl 30m

Open the exact printed loopback URL. Ctrl-C stops it; the lifetime is bounded.
The dashboard is read-only and needs no running experiment, Docker or Jaeger.
The database directory must be writable for SQLite journal recovery. The SQLite
engine is compiled into the CLI/dashboard; there is no system SQLite dependency.
macOS binaries are unsigned/unnotarized; Gatekeeper policy may prevent execution
of externally downloaded packages. Do not bypass security policy automatically.

## Full experiments require the source topology and Docker

These native packages do not orchestrate the sample network. Check out the exact
revision from VERSION.json, read README.md, and use the repository's POSIX wrapper:

    ./scripts/experiment.sh run -spec examples/latency.json

Requires local Docker with Compose, Git and a POSIX shell. First use requires
pinned image downloads/build dependencies; it is not an offline platform workflow.
Never point the engine at arbitrary services. No Docker socket is mounted into
containers. Reports and databases remain local and cleanup removes the owned stack.
