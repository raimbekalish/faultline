# ADR 0013: Versioned internal worker execution with closed fault control

Status: proposed for M8B3B independent review (Unreleased).

## Context

[ADR 0012](0012-custom-worker-measurements.md) added real local HTTP measurements
but no execution-message adapter or concrete control-API client. The approved
[custom-lab design](../design/custom-service-lab.md) requires strict identity,
bounded results and honest failure/cleanup without widening sample evidence.

## Decision

Add separately named schema-1 measurement request/response envelopes in
`internal/labworker`. Validate exact fields, spec/hash, tracing off and assigned
launch/run identity before binding a one-use internal execution session. The
request cannot choose endpoints, revision, environment or commands. The trusted
harness supplies revision and explicit owned loopback listeners out of band.

Keep the adapter and endpoint constructors private. No production command imports
or invokes them; the existing preflight worker and `lab plan` behavior remain
unchanged. The in-memory session is a test capability, not a replacement for the
fixed host ledger or consumed RUN authorization.

Implement the closed Toxiproxy 2.12 HTTP contract for one existing run-named proxy:
selected proxy reads, one fixed downstream latency-toxic POST, one exact-toxic
DELETE and repeated empty-toxic verification. Do not create/delete proxies, reset,
list globally, adopt by prefix or accept arbitrary HTTP operations. Pin configuration
and reject foreign/toxic/profile mismatch. A successful POST must return exact
metadata and be followed by matching inspection before fault measurement.

A lost/malformed POST response does not grant toxic ownership, even when later
metadata looks identical. Removal is refused without acknowledged creation and
fresh matching profile. A lost DELETE remains unconfirmed even if later reads show
absence. This sacrifices availability; a future host must retain unresolved state
and perform separately certified whole-lab cleanup, not adopt an uncertain toxic.

All tests use test-owned loopback HTTP servers. The server changes dependency
handler delay; it is **not** a Toxiproxy process or network-fault qualification.
Worker-level completion/removal remains distinct from proxy/container deletion,
runtime qualification and durable reservation release.

## Consequences and alternatives

Reject generic proxy-client exposure and URL configuration: they would extend
fault authority beyond this closed scope. Reusing the sample client's raw-error
and missing-is-success behavior would weaken these new contracts, so keep this
small adapter separate without changing released sample behavior/dependencies.

Only validated responses are written, after independent cleanup. Input errors
produce no output; valid incomplete/canceled results remain inspectable. Output
failure never re-enables execution. Arbitrary blocked IO still needs its owner to
interrupt the stream; no unbounded escape goroutine is introduced.

Cooperative SIGTERM can finish cleanup; abrupt worker death cannot. Process tests
retain missing output and a still-present mock toxic, not a fabricated canceled
report or cleanup receipt. Protocol names/API metadata are not authentication.
Exclusive run-owned server authority and actual runtime continuity/isolation are
not established by this HTTP client.

Docker Desktop metadata/connection snapshots cannot independently attest the
continuity required by M8B2C. Keep its provider absent and execution fail-closed;
do not invent a generation from daemon/socket/bridge identity. A future dedicated
native Linux supervised-daemon profile is a review candidate, not supported or
qualified here. See [continuity review](../security/custom-runtime-continuity.md),
[protocol contract](../design/custom-worker-protocol.md) and [validation](../validation.md).
