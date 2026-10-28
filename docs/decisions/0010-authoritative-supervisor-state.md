# ADR 0010: Fixed supervisor authority, durable reservations and inspected intent

- Status: implemented for M8B2B review; no runtime execution authorization
- Date: 2026-10-28 UTC
- Builds on: [ADR 0009](0009-owner-selected-service-labs.md)

## Problem

A caller-selected empty journal directory could bypass future consumption checks.
An advisory process lock alone disappears on crash even when resources may remain.
A label/configuration hash supplied by a caller cannot establish actual ownership,
and optional retries can exhaust a bounded journal before teardown is recorded.

## Decision

Use one deterministic account-owned authority selected from the OS effective-UID
account record. Ignore HOME/XDG/state overrides; expose no state-path parameter.
Explicit enrollment writes a private anchor plus fixed root manifest. Open never
initializes missing state. Immutable hash-linked reservation/release records and
a bounded atomic checkpoint bind consumption across private runtime namespaces.
Runtime keys derive from inspected daemon identity, not socket aliases or plan salts.
A stable process lease and persistent reservation are separate: closing/exiting a
process does not resolve its resource ownership or permit another RUN.

Keep old plan/journal encodings. Low-level arbitrary-directory journal stores are
not execution authority. Explicitly validated legacy imports preserve history and
launch tombstones, but cannot authenticate source/ownership or authorize cleanup.
No automatic import, history reset or prefix discovery is introduced.

Selected exact-ID read-only inspection verifies the recorded resource profile and
runtime before producing opaque inspection evidence. Project counts/known values
rather than arbitrary labels/env/IPs. Role-specific inherited label keys are fixed;
configuration digests are recomputed after actual fields are checked. Repeated
inspection is not an atomic snapshot or proof of effective network isolation.

Reserve up to 21 cleanup events plus 21 retry events for normal writes, within the
existing 96-event bound. Old over-budget histories remain readable. A living held
lease can plan cleanup of its last proven IDs after recording failure/exhaustion;
it cannot manufacture a receipt, new identity or durable success. A restarted
process still requires intact authoritative state.

Expose only opaque non-executable mutation descriptions and a typed future
transport interface; implementations exist only as recording fakes. Trusted leases
reject uncertified success events. Presence inspection does not yet establish
absence or certify mutation responses, so attempted-resource reservations cannot
be released in this increment. Production remains plan-only.

## Consequences and limits

Availability is deliberately conservative. Partial enrollment/checkpoint/import,
missing history, capacity exhaustion or ambiguous state requires separately reviewed
recovery, not a new empty directory. Bound the ledger to 32 consumed launches and
4096 directory entries; no automatic deletion/compaction. Retain tombstones.

The authority is cooperative local protection, not authentication against same-user
administrators, malicious Docker/OS components or rollback of all local state.
Mode/ownership checks do not audit every ACL. OS account lookup, kernel stalls,
power loss and filesystem sync semantics limit deadline/durability guarantees.
Native account lookup retains existing CGO/C prerequisites; the independent offline
verifier does not import this module. No dependency or released evidence schema changes.

Real driver/result certification, fixed-profile image bootstrap and actual runtime
qualification remain separate review and authorization gates. See the
[implementation contract](../design/trusted-supervisor-boundaries.md) and
[validation record](../validation.md). No Docker resources were created to make this
ADR's tests pass; synthetic proofs are not qualification results.
