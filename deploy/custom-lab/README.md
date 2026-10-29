# Custom lab bootstrap source — preparation only, Unreleased

No image in this directory was built, pulled or run for M8B2C or M8B3A. These are
reviewable source definitions, **not qualified catalog artifacts**. The production CLI still
has only `lab plan`. Building/running requires separate owner authorization.

`Dockerfile` pins the existing Go 1.26.8 builder digest, selects dedicated
entrypoint sources and targets CGO-disabled Linux ARM64. The existing spec imports
engine validation and its compile-time dependencies. M8B3A corrects the selective
COPY closure and adds locked module download for a later authorized build; this
step was not executed in Docker. No dependency versions changed. The final scratch
image has fixed UID/GID 65532, `/` working directory, SIGTERM, no volumes/environment,
absolute entrypoint and the two contract/revision labels expected by the current
catalog. A full 40-lowercase-hex `SOURCE_REVISION` is mandatory; there is no mutable
image tag substitution or sample-image relabeling. Repeatable instructions do not
establish independently reproducible binary/image bytes: actual builds/digests,
builder availability and reproducibility were not tested here.

The dedicated `/app/faultline lab-worker` source accepts **preflight only**. It has
no Docker, fault injection or SQLite invocation. Spec validation transitively
imports engine types/modules; only shared validation is called, never sample Run.
One strict JSON request on stdin is capped at 8192 bytes and must contain exactly:

- `schema_version: 1`, `kind: "faultline-worker-preflight"`;
- `launch_id` in existing `faultline-lab-<32 hex>` form;
- `run_id` in existing `faultline-<32 hex>` form;
- `specification_sha256`, recomputed using the existing spec serializer;
- `experiment`, exactly the existing validated schema-1 measurement spec;
- `tracing: "off"`.

Duplicate/case/unknown/null/malformed fields reject through shared strict parsing.
The response (<4096 bytes) echoes only these validated identifiers/hash plus source
revision, `status: "validated_preflight_only"`, `execution_available: false`,
`capabilities: ["validate_configuration"]` and `measurements: null`. Exit zero means
configuration validated, **never** an experiment succeeded. Native development
builds may identify as `development`; the image recipe requires a real revision.
The entrypoint bounds input waiting to ten seconds and closes its own stdin on
cancellation. General reader cancellation still depends on the reader/OS behaving.
A future executing worker/result protocol must be separately reviewed.

The dedicated fixture is `/app/sample -role dependency` solely to preserve the
existing closed catalog pathname. Its source is `cmd/lab-fixture`, **not** the
original sample binary. Fixed GET `/healthz`, `/value` and `/count` on 8080 return
public contract data/counters only; no arbitrary application payload collection.
The counter and constant are fixture data, not experiment measurements.

`examples/custom-service/http-work` is an independently implemented standard-library
contract app (no Faultline imports), with its own pinned source recipe. It serves
GET `/work` on 8080, makes one bounded dependency GET through the fixed proxy, uses
a 250 ms deadline, no proxy environment/redirects/reused-connection retry and returns
504 on dependency timeout. Its public data constant differs from the fixture, so
it is not a copy/relabel of the original sample. Tests use fake transports and
in-process handlers; no selected container/application was launched.

Review immutable IDs, Linux ARM64 metadata, empty forbidden image configuration,
exact entrypoint/user/labels/volumes/environment and real worker capability before
any later catalog enrollment. A successful image inspection or contract label is
not isolation or execution attestation. Neither recipe is wired into packaging,
the existing Compose topology, releases or an automatic build/pull path.

M8B3A adds `internal/labworker` separately, exercised only with injected loopback
HTTP tests and recording fault control. It is not imported/copied into this worker
entrypoint recipe and does not change the preflight response. A host-side
`go list -deps` inspection confirms all ten first-party preflight/fixture packages
are represented by selective COPY entries; this is source-closure evidence, not a
successful Docker build or qualification of the image profile.


M8B3B adds internal measurement request/response and a closed HTTP control adapter
for test-owned loopback listeners. The production entrypoint, selective source
recipe and preflight capability are unchanged. No new measurement code is copied
into this image and no build/pull/run was performed. Real worker invocation and
runtime fault-control/provenance still require separate review/authorization.
