# ADR 0009: Owner-selected services through a closed local adapter

- Status: design approved/merged in PR #10; execution remains unimplemented
- Date: 2026-10-27
- Source reviewed: `257eab735c16e9daa2a700d88e329f84c72622af`
- Scope: Milestone 8A. Approval of this document is not runtime qualification.

## Problem and existing boundary

Faultline can measure only its sample application. Replacing an image in the
current Compose file is unsafe as a custom-service feature: all peers share one
network with the Toxiproxy control API. The current report, provenance, trace
completeness and portable validators also encode sample-specific assumptions.
An arbitrary Compose override would expand both execution authority and the
meaning of recorded evidence without a reviewed boundary.

Design a useful first step for an owner who can build a disposable HTTP application
with one replaceable dependency. Preserve the built-in sample path unchanged.

## Alternatives

| Approach | Usability and feasibility | Portability and reproduction | Authority, isolation and cleanup | Decision |
| --- | --- | --- | --- | --- |
| 1. Any selected image in a Faultline-owned Compose topology | Convenient image selection, but an image alone says nothing about routes, dependency wiring, retries, state or secrets. Generic ports/commands/environment quickly become an orchestration language. | Architecture-specific local image IDs are reproducible references, not portable image delivery or trusted builds. Arbitrary application defaults are not captured by a tag. | Faultline can own all lab resources, but image hooks, implicit volumes, network egress and control-plane access still require restrictions. A flat network is inadequate. | Reject an unrestricted image interface. Use image selection only inside option 2's contract. |
| 2. Reviewed adapter in a Faultline-owned lab | Requires a small explicit HTTP contract; one independently built application can qualify it. Built-in adapter code, not user plugins or scripts, fixes the topology and protocol. | Local immutable Linux image IDs, an adapter version and a canonical launch snapshot identify configured conditions. Qualify one architecture/runtime first. | Fresh lab per run; control/data/backend separation; no user infrastructure joins or mounts. Whole-lab teardown provides a second cleanup layer. Docker and image trust remain prerequisites. | **Recommend: one closed adapter and one selected frontend image.** |
| 3. Permission-scoped faulting in an existing user Compose environment | Less adaptation for an existing stack, but permission must cover rewiring, restarts, connection draining and recovery of application state. | Existing volumes, overrides, concurrent owners and mutable service state make repeatability weaker. Compose input can itself exercise host authority. | Faultline does not own the stack. A project label is not permission to alter it; cleanup cannot safely undo concurrent user changes. Safe support needs a separate capability/lease and recovery design. | Defer. No attachment, reconfiguration or fault injection into an existing user project. |

## Proposed decision

Add a future, explicit native `faultline lab` workflow with the built-in adapter
`http-work-v1`. The owner selects **one already-local immutable image ID**. The
image supplies the frontend; Faultline supplies runner, Toxiproxy and a disposable
deterministic HTTP dependency fixture. All are new containers, not existing user
services. The app must support the adapter contract in the [design](../design/custom-service-lab.md).
This tests the app's dependency client and recovery against a fixture, not its
production dependency or complete deployment.

The adapter is compiled/reviewed Faultline code with fixed routes, port, executable,
environment, resource ceilings and network graph. A descriptor cannot redefine
those fields. No arbitrary URLs, Compose files, Dockerfiles, commands, plugins,
builds, pulls, secrets, host mounts or external networks enter the run interface.
Selecting an image by content ID does not make its code safe; the owner must trust
the image and its contents. This is not a hostile-code sandbox.

The host supervisor alone invokes the trusted local Docker/Compose CLI. It creates
a private intent journal before Docker mutations, never exposes the socket to
containers, and tears down only recorded, verified IDs. Use a fresh launch and
proxy owner ID per run, never reset a proxy server, and never adopt existing
resources. Start with one active custom lab per user/runtime; refuse another
until cleanup is confirmed. Existing sample experiments retain their own path.

### Conditions before any custom image starts

1. Independent review accepts the adapter, [threat model](../security/custom-service-threat-model.md)
   and implementation tests. M8A does not establish these tests have passed.
2. A local Linux-container runtime/Compose combination passes qualification,
   including isolated bridge gateways, DNS leakage and control-plane separation.
   Require the `internal` + `isolated` gateway capability introduced in Engine 28,
   but **a version comparison alone is insufficient**. No weaker fallback.
3. The owner authorizes the exact single-use plan hash; image and adapter identities,
   runtime binding and generated resource profile are revalidated at use.
4. Before app startup, inspect the created resources and use trusted probes to
   verify that the app's networks cannot reach the control API or bypass the proxy.
   Unexpected attachments or unverified state cause cleanup and refusal.

The Toxiproxy API binds only its control-network address, not `0.0.0.0` on every
interface. Separate networks alone do not enforce that restriction. DNS forwarding
and bridge-host reachability are explicit qualification gates, not assumptions.
No runtime has been qualified for this proposed topology during M8A.

## Evidence, tracing and compatibility decision

M8B's first useful execution slice is intentionally **ephemeral**: a separately
named, bounded custom-lab result with raw observations, configuration/provenance
and distinct proxy/whole-lab cleanup states. It is not a schema-1 sample report or
an `.flb` bundle. The exact proposed interface and finalization order are in the
design. Do not issue synthetic history IDs or put custom results in the current
SQLite store. Do not add arbitrary targets to `spec.Spec` or weaken `record.Validate`.

Custom history/export/comparison/gates require a subsequent compatibility review
that binds adapter, image identities, workload and cleanup scope to validated
evidence. Review explicit version dispatch and old-reader rejection then; do not
hide new semantics in optional schema-1 fields that old tools ignore. Reuse the
engine summarizer. Existing format-1 fixtures, policy schema 1, units, output and
exit codes stay unchanged. Current evaluate must reject a custom result envelope,
not infer PASS or accept it as the sample.

Tracing is **off-only** for the M8B MVP, with Jaeger/view flags rejected. Sample
tracing remains available. Future custom tracing needs a separate privacy/collector
boundary and profile-specific completeness rules; the sample's four-span chain
does not apply. Toxiproxy does not emit spans. Missing provenance stays missing;
recorded image IDs and hashes are consistency references, not trust or attestation.

## Consequences and implementation gate

This requires adaptation and excludes databases, persistent workloads, multi-service
stacks and secret-dependent applications. That restriction is deliberate: it
provides one useful real-app path without a general Compose executor or borrowing
authority over user infrastructure. No new dependency is proposed; use typed Go
configuration, bounded subprocesses and the existing measurement/statistics code.

**GO to begin the staged M8B work only after independent approval of this design.**
Begin with validation/planning and adversarial qualification, not an enabled
custom-execution flag. **NO-GO to enable custom execution today**: there is no
implementation or runtime qualification yet. If the selected runtime cannot
enforce the required boundaries, stop; require a separately provisioned qualified
local runtime rather than changing host security or weakening the contract.

Current sample safety rules and `SECURITY.md` remain operative. M8A changes only
documentation; v0.1.0, its assets and the README demo remain unchanged.

M8B1 clarification: [authorization lifecycle](../design/custom-service-lab.md#authorization-lifecycle-resolved-for-m8b1)
consumes a plan once for future RUN, while preserving its identity for journal-bound
cleanup-only recovery. Cleanup cannot grant RUN replay or ownership from names.
The planner implements neither consumption nor execution and cannot qualify a lab.
M8B1's [implementation contract](../custom-service-planner.md) specifies the
private plan/redacted summary, closed inspection operations and absent-catalog
failure behavior. No existing experiment/evidence schema or runtime is changed.
