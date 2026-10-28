# Custom-service planner — Unreleased M8B1

Only `faultline lab plan` is exposed by the CLI. **RUN, CLEANUP, a worker command and
custom container execution are unavailable.** This source-only feature is absent
from v0.1.0 binaries. It does not qualify network isolation or application behavior.
The approved [design](design/custom-service-lab.md) and
[threat model](security/custom-service-threat-model.md) still gate execution.

## Descriptor schema 1

All six fields below are required, with exactly these keys. Replace the image
placeholder with the full immutable ID of an already-local image; it is not a tag,
registry URL or executable example ID.

```json
{
  "schema_version": 1,
  "adapter": "http-work-v1",
  "image_id": "sha256:<64 lowercase hexadecimal digits>",
  "platform": "linux/arm64",
  "experiment": {
    "schema_version": 1,
    "samples_per_phase": 5,
    "latency_ms": 80,
    "request_timeout_ms": 1000
  },
  "tracing": "off"
}
```

Input is at most 8 KiB including whitespace, JSON depth at most 32. Shared strict
JSON validation rejects invalid UTF-8/surrogates and escaped/case/simple-fold
duplicates. Exact keys, required values and existing spec decoding reject nulls,
unknown fields, floats/exponents for integers, unsupported versions and invalid
ranges: samples 1–100, latency 1–5000 ms, timeout 10–5000 ms. There are no target,
port, command, environment, Compose, plugin, mount or Docker-config fields. No
interpolation, includes or URL loading. Config hashing reuses typed canonical
serialization and the current numeric validator, not new measurement semantics.

## Closed catalog and prerequisites

The one catalog fixes `/app/service`, HTTP port 8080, `/work`, the fixture URL,
250 ms configured dependency timeout, UID/GID 65532, role/network membership,
resource ceilings, intended control-interface bind and security restrictions.
These are **planned constraints**, not active containers or verified behavior.
The native CLI requires its existing Go/CGO/C build prerequisites, a trusted
Docker/Compose CLI, an available local Linux ARM64 daemon and preloaded images.

- App: exact descriptor image ID, Linux ARM64, no image environment defaults,
  volumes, healthcheck or OnBuild triggers. Metadata cannot prove `/app/service`
  exists, uses its dependency correctly or contains no secrets/malicious code.
- Runner/fixture: the already-local catalog discovery reference
  `faultline-lab:http-work-v1`, with the same metadata restrictions, selected label
  `com.faultline.lab.contract=http-work-v1` and a full 40-hex
  `org.opencontainers.image.revision`. Only these two label values are inspected.
  This reference is a trusted local lookup convention, **not an immutable pin or
  publisher authentication**. The plan pins its inspected image ID; future RUN
  must recheck the catalog and must never execute a tag instead of that ID.
- Proxy: the existing digest-pinned Toxiproxy 2.12.0 reference from the closed
  catalog. Inspect the actual ID, platform and pinned RepoDigest match; require
  its single expected environment entry by count, without copying its value.

App, lab and proxy IDs must differ. A matching contract/revision label is a
declaration, not executed worker validation. There is no image provisioning or
compatible worker-image build in M8B1; catalog absence is an expected fail-closed
prerequisite error. Do not substitute a sample image, invent labels, pull/build or
run images merely to obtain a successful plan. Trusted bootstrap and actual
worker qualification belong to later reviewed work.

## Use and local filesystem boundary

```sh
make cli
mkdir -m 700 artifacts/lab-plans
./artifacts/bin/faultline lab plan --application app.json --out artifacts/lab-plans/plan.json
```

The output parent must already exist, be owned by the current user and have no
group/world permission bits. A final symlinked output parent is refused. Parent
ancestors and host Docker configuration must be trusted. No output directory is
silently provisioned; repeat with a new output filename, not an overwrite.
`--application`/`--out` (also their normal single-dash spellings) are each accepted
once; unknown/repeated flags, extra arguments and run/cleanup commands are usage
errors. Paths are literal OS paths, not environment expressions or URLs.

Inputs must be bounded regular files. Darwin/Linux use directory capabilities,
no-follow/nonblocking file open and inode rechecks to reject final symlinks,
special files and substitution after stat. Rooted file operations pin the opened
parent; they do not attest parent trust or prevent concurrent in-place writers.
Other hosts fail closed. A trusted local Unix endpoint may itself be a symlink;
resolve it and bind its actual socket device/inode, checking again after inspection.
Socket locality is not authentication or proof against a deliberately configured
remote relay.

Planning has a 20 s cooperative deadline, with at most 3 s per subprocess. Capture
at most 32 KiB stdout and discard bounded stderr (4 KiB); excessive output cancels
the child process group. Paths, raw CLI errors, daemon ID and arbitrary metadata
are never echoed. Faultline retains only selected fields/counts; the trusted Docker
CLI may internally read its configuration/full API objects. No credentials are
requested, copied into plans or passed to containers. Host CLI memory, stalled
filesystem calls and malicious local administrators remain outside these bounds.

## Exact read-only command boundary

The adapter has no generic execution API. Its only operations are:

1. `docker context inspect [NAME] --format '{{json .Endpoints.docker.Host}}'`
   when context selection is needed. `DOCKER_CONTEXT` takes precedence over
   `DOCKER_HOST`; otherwise inspect the current context. Reject TCP/SSH/npipe or
   malformed endpoints before daemon inspection. Context names cannot inject flags.
2. `docker --host UNIX_ENDPOINT version --format FIXED_SERVER_TEMPLATE` for
   server version/OS/architecture only.
3. `docker --host UNIX_ENDPOINT compose version --format json` for version only.
4. `docker --host UNIX_ENDPOINT info --format '{{json .ID}}'` before/after image
   inspection. Raw ID is transient; only a per-plan salted fingerprint is saved.
5. `docker --host UNIX_ENDPOINT image inspect --format FIXED_ROLE_TEMPLATE REF`:
   REF is the descriptor's immutable app ID or one of the two compiled catalog
   references. Templates select ID, OS/architecture, Env/volume/OnBuild counts,
   healthcheck presence, the two lab labels or the fixed proxy-digest boolean.
   They never print environment values, arbitrary labels or registry inventories.

The selected canonical Unix endpoint is passed explicitly afterward. Child
environment contains only host `HOME`, `PATH` and optional `DOCKER_CONFIG` needed
by the trusted CLI/plugin; inherited Docker target/TLS/API, Compose, proxy, OTel
and secret variables are omitted. The descriptor cannot influence these settings.
No create/start/run/exec, network/volume mutation, build/pull/tag/remove, Compose
configuration evaluation, runtime startup or automatic qualification is possible.

## Plan format and authorization

The private JSON file is schema 1, kind `faultline-lab-plan`. It contains:

- `status: PLANNED`, runtime/application qualification `NOT_PERFORMED`, and
  `execution_available: false`. Even all successful metadata checks change none
  of these fields into a runtime safety assertion.
- Canonical descriptor/configuration SHA-256, fixed catalog/hash, role image IDs
  with the lab's declared revision, and observed version/OS/architecture facts.
- Independent random 128-bit launch and proxy IDs, a random 256-bit authorization
  nonce, and private socket binding with a separate random 256-bit salt and salted
  daemon fingerprint. Raw daemon ID is never persisted.
- Explicit future RUN/CLEANUP scope strings and `authorization_sha256`: SHA-256 of
  the fixed-order typed Go JSON payload **excluding that hash field**, before
  pretty printing. It binds the entire private plan, not just the measurement spec;
  it is not RFC 8785, a publisher signature or resource ownership evidence.

Same descriptor/inspection/entropy yields identical bytes. Configuration hashing
is stable across JSON field order. New plans have distinct random identity/binding
fields and authorization hashes; no clocks or unrelated machine facts are added.
The pure plan JSON codec accepts the generated fixed-order representation (with
optional whitespace), rejecting unknown/case-aliased/duplicate fields, ignored
array elements and noncanonical spellings. Editing/reordering a plan is unsupported.
There is no file-reading RUN/CLEANUP consumer in this increment.

The file is at most 64 KiB, mode 0600, written/synced privately and published with
an atomic no-replace hard link in the opened private directory. Sync/temp-cleanup
errors are surfaced. Existing files, symlinks and directories are never replaced.
There is no rename-overwrite fallback. Cancellation/error after publication can
leave a **complete private plan**; inspect it rather than automatically deleting
or replacing it. No consumption file, resource journal or lease is created.

Stdout is a separate bounded (8 KiB) redacted summary with identities/hashes and
explicit unqualified notes. It omits endpoint, socket numbers, nonce, salt and
daemon fingerprint. **Do not publish the private plan file.** A valid plan exits
0, input/inspection/cancellation/write failure 1, usage 2. A failed stdout write
does not undo the private file. Old commands and evaluate's 0/3/4 contract remain.

Future RUN consumes the identity durably exactly once before provisioning; a
failed/canceled run does not restore it. The same consumed identity may authorize
repeat cleanup-only attempts with its verified journal/runtime/resource IDs.
The plan alone grants no deletion authority. See the resolved
[lifecycle](design/custom-service-lab.md#authorization-lifecycle-resolved-for-m8b1).
**Neither RUN replay enforcement nor cleanup is implemented by this planner.**

Existing sample specs, engine, tracing, SQLite, format-1 bundles and gates are
unchanged. A plan is neither an experiment report nor portable evidence, and no
custom evaluation or release is implied. Qualification and execution remain M8B2
or later review gates; they are not enabled by producing or sharing a hash.

## M8B2A internal foundation (Unreleased)

[Supervisor contracts](design/custom-service-supervisor.md) now generate and
structurally validate the closed resource graph and provide pure ownership/state
transitions plus private bounded journal persistence. These APIs are not connected
to `lab plan` or an execution command. Existing plan format/files and stdout are
unchanged. No resources are created, no actual proof is collected and no runtime
is qualified. A private consumed journal is not a runnable authorization token.
See the [bounded bridge follow-up](security/docker-bridge-observation.md) for
observed idle VM lifecycle evidence and still-unknown causal/caller details.

## M8B2B trusted boundaries (Unreleased)

The [trusted supervisor modules](design/trusted-supervisor-boundaries.md) now
provide fixed private authority, durable reservations/process leases, strict
recorded-ID inspections, cleanup event admission and opaque non-executable
mutation descriptions. No enrollment occurs during planning, no state-directory
flag is accepted and no execution/cleanup command is enabled. Presence inspection
is not isolation qualification or certified absence. Plans and their format remain
unchanged; v0.1.0 contains none of these unreleased supervisor APIs.

## M8B2C preparation (Unreleased)

[Transport contracts](design/docker-transport-certification.md) add closed private
command plans, attempt recording and conservative certification logic. Production
continues to expose only this read-only planner. Dedicated bootstrap sources are
preflight-only and were not built. No runtime continuity provider, mutation driver
or custom RUN/CLEANUP command exists. Existing plans are not rewritten or upgraded.


## M8B3A measurement foundation (Unreleased)

The [measurement engine](design/custom-worker-measurements.md) runs only through
scoped dependency-injected tests. The planner, descriptor and existing preflight
protocol/entrypoint are unchanged. Actual local HTTP observations are not evidence
of Docker DNS/isolation, which remain [unverified qualification gates](security/custom-lab-dns-qualification.md).


## M8B3B internal protocol (Unreleased)

The [measurement protocol/controller](design/custom-worker-protocol.md) now
integrates validation, local HTTP measurements and scoped API-shaped fault control
in tests. Its private loopback binding is not RUN authorization or runtime
qualification. The public preflight worker rejects these requests; no lab run or
cleanup command is added. [Runtime continuity](security/custom-runtime-continuity.md)
remains fail-closed with no production provider.
