# Trusted supervisor boundaries — Unreleased M8B2B

This records the M8B2B boundary. M8B2C adds
[closed command/certification contracts](docker-transport-certification.md);
historical missing-certification statements below describe M8B2B. No production
mutation driver or qualified runtime continuity is available.

M8B2B implements local authority, cooperative leases, selected read-only observation
and opaque mutation **planning**. Production remains `lab plan` only. There is no
executable mutation driver, worker, runtime qualification, `lab run` or `lab cleanup`.
Nothing authorizes actual provisioning. See [ADR 0010](../decisions/0010-authoritative-supervisor-state.md),
[ADR 0009](../decisions/0009-owner-selected-service-labs.md), the
[foundation](custom-service-supervisor.md) and [threat model](../security/custom-service-threat-model.md).

## Fixed authority

`OpenAuthority(ctx)` has no directory argument. The trusted OS account record is
looked up by effective UID; `HOME`, `XDG_STATE_HOME`, working directory and custom
state environment variables cannot select authorization state. Unknown account or
noncanonical/symlinked home fails closed. Native builds retain their existing CGO/C
requirements; CGO-disabled account lookup may fail closed on Darwin. This does not
affect the separate portable verifier.

The fixed layout under that account's home is:

```text
.faultline-supervisor-v1.anchor.json     private enrollment anchor
.faultline-supervisor-v1/                owned mode 0700
  authority.json                        matching enrollment nonce/version
  authority.head.json                   bounded mutable registry checkpoint
  registry.NNN.json                     immutable hash-linked reservations/releases
  journal.guard                         stable global metadata lock
  runtime-<private daemon key>.guard     stable process lease lock
  faultline-lab-<identity>.NNN.journal.json
  faultline-lab-<identity>.receipt.json
```

Enrollment is a separate explicit programmatic `InitializeAuthority(ctx)` operation,
not called by Open/Begin or any production CLI. Neither an existing anchor nor an
existing directory can be silently adopted/reset. The anchor is published before
initialization, so partial enrollment remains blocked. No actual operator-home
enrollment was performed during this milestone; tests use private temporary homes.

Files are owned mode 0600 regular singly-linked records. State components reject
symlinks, FIFOs, hard links, substituted inodes and unsafe permissions. The opened
root is pinned and checked against its named directory. Home ownership and no
group/world write are checked. OS/account/ancestor/ACL trust is still required;
POSIX mode checks are not a complete ACL or malicious-administrator audit.

A private runtime namespace key is SHA-256 of a versioned domain and the inspected
daemon ID, not a context name, socket inode, plan salt or user option. Socket aliases
cannot create independent consumption histories. Exact plan endpoint/socket and
salted daemon binding are still rechecked. The raw ID is not persisted. Namespace
hashes stay private and do not authenticate a daemon or prove uninterrupted VM life.

Reservations carry that key under one account-wide ledger. Launch IDs are consumed
across all its runtime keys, so changing a directory/context does not reset them.
The implementation conservatively allows only one unresolved custom lab across
this account's ledger, stronger than one per runtime. Existing sample workflows
do not use this authority/lease and are unaffected.

## Durable reservation versus process lease

Begin first verifies the exact plan authorization hash and local runtime binding.
Under the stable global guard it validates registry history, existing journals and
acknowledged receipts, then acquires the runtime process lock. A synced reservation
is recorded **before** the consumption snapshot is created. A missing/failed
consumption write therefore leaves a blocking reservation, not reusable authority.
A useful lease cannot be constructed through the caller-selected low-level
`JournalStore`; only this fixed authority can construct it.

A process lease stays held by the live supervisor. `Close` or process exit releases
the kernel advisory lock **only**. The durable reservation survives. Another RUN
refuses while unresolved history exists. Cleanup recovery can reacquire the same
consumed plan's lock after the process exits, but cannot displace a live holder,
reconsume the plan or adopt another ID. Waits are bounded by a three-second authority
operation deadline. The guard is never unlinked to make a stale-lock check pass.

The registry checkpoint and immutable hash-linked records detect missing,
extra/uncheckpointed, truncated or inconsistent history. Updates sync a new record,
then atomically replace only the recognized private checkpoint and sync its directory.
An interrupted checkpoint update is unresolved, with no rollback/repair fallback.
Existing journals retain their immutable snapshot format. Hashes detect consistency
errors, not coordinated tampering or publisher authenticity.

Limits: 32 reservations, at most 65 registry records including genesis, 2 KiB per
registry/enrollment/checkpoint record, 4096 directory entries and existing 64 KiB /
96-event journal bounds. Capacity exhaustion refuses new authority; consumption
records are never automatically removed. A reviewed future migration/compaction
protocol is required before changing those limits or retiring state.

This does not defend against a same-user administrator deleting both anchors and
all history, rolling back an entire backup, forging Docker observations or bypassing
Faultline. Missing state through supported APIs is never interpreted as fresh.

## Legacy compatibility and conservative migration

Plan encoding and journal schema 1 are unchanged. Existing journals are still
strictly decoded/replay-validated, including valid histories above the new write
budget. `ImportConsumed` accepts explicitly supplied validated journal data and
matching plan authorization/runtime, never a directory override or filesystem scan.
It retains the old snapshot history and consumes the launch identity as a blocking
**tombstone**, including if import fails partway.

Import does **not** establish trusted resource ownership or grant cleanup/RUN
capability, even if labels and statistics look plausible. Its lease is not usable
for mutation planning or conversion into cleanup recovery. No automatic import or
reset exists. Partial/legacy state requires separately reviewed recovery; operators
must not enroll a fresh namespace as a substitute for preserving known consumption.
No existing evidence or legacy file is edited by import. This deliberately favors
refusal over adopting resources from an unauthenticated journal envelope.

## Read-only observation boundary

The closed adapter adds only these command families, targeted by a full recorded
64-hex resource ID:

```text
docker --host UNIX container inspect --format FIXED_ROLE_PROJECTION ID
docker --host UNIX network inspect --format FIXED_NETWORK_PROJECTION ID
```

The original context/server/Compose/identity/image inspection operations remain.
No inventory search, prefix lookup, name lookup, arbitrary template, URL, exec,
Compose evaluation or mutation is exposed by the observer. Runtime checks require
the plan's exact socket device/inode, salted daemon fingerprint, Linux/ARM64 and
recorded server version (Engine 28+ is a prerequisite, never qualification).

An ID must already exist in the matching journal's accepted resource set before
observation. The adapter verifies runtime before/after and repeats the exact target
read, refusing differences or missing/malformed replies. Output is capped at 32 KiB,
subprocess stderr at 4 KiB discarded, each subprocess at three seconds, total
observation at fifteen seconds. Cancellation is preserved. Errors exclude raw
metadata, paths and daemon diagnostics.

Selected actual fields verify:

- Exact ID/name/image and configured image, role/launch/owner nonce, executable,
  args/entrypoint/command, UID/GID, working directory and stop signal.
- Exact memory/swap/CPU/PID/nofile ceilings, read-only root, dropped capabilities,
  no-new-privileges and private namespace intent. No added privileges/capabilities,
  host namespaces, binds/devices/volumes-from, links/extra hosts or port publication.
- Restricted DNS/search, logging/restart policy, no healthcheck, fixed environment
  count/match flags and the only permitted application tmpfs. The future proxy
  profile explicitly overrides its inherited PATH with empty `PATH=`; executables
  are absolute. This is a candidate execution-profile restriction, not a changed
  plan format or a claim that it has been run.
- Exact generated network names/recorded IDs and bounded alias sets; no extra
  attachments, endpoint driver options, static IPAM overrides, links or gateway
  priority changes. Network inspection checks internal local bridge/IPv4/isolated
  gateway intent, no IPv6/ingress/attachability/config-only source, bounded IPAM
  metadata and only expected known container members.

Ownership labels alone are insufficient. Application containers have the three
ownership keys; lab roles inherit the two known contract/revision keys (five total).
The pinned proxy inherits eight fixed OCI keys plus ownership (eleven total).
Only fixed keys/counts and necessary contract/ownership values are projected;
arbitrary OCI values, environment values, IPs, credentials and application payloads
are not copied. Selected effective configuration is checked and normalized back
into the existing profile digest; no label-provided digest is accepted.

`VerifiedResource` has opaque fields and is bound to the exact journal hash and
fresh runtime witness. It is an **inspection proof**, not historical creation
attestation or runtime isolation qualification. The trusted CLI/daemon can lie;
byte-identical repeated reads do not form an atomic transaction and cannot eliminate
later TOCTOU, cached Desktop responses or a hidden VM lifecycle change. Actual
control binding, routing/DNS leakage and bypass resistance still need real probes.

## Cleanup-budget discipline

Journal schema/read validation stays unchanged; new appends apply admission rules.
The worst complete cleanup attempt reserves **21 events**: up to 16 resource
intent/results, two proxy events, one worker closure, lab begin and final outcome.
Normal work additionally reserves another 21-event attempt: at most **42 remaining
events** are needed at the worst state, within the existing 96-event ceiling.
Real required cost shrinks as pending operations finish. Every creation stage and
worker/proxy/retry boundary is tested; repeated optional retries cannot eat the
capacity for required remaining transitions.

Cleanup records may finish honestly as unconfirmed. Unconfirmed is not clean,
does not release a durable reservation and cannot permit another RUN. If an old
96-event journal, byte-size limit or failed persistence leaves no record capacity,
a still-held living lease can retain its last proven consumed snapshot/IDs and
prepare scoped cleanup from a fresh verified observation under an independent
caller-supplied cleanup context. It cannot claim recording succeeded, publish a
successful receipt, start more work or gain new IDs. A restarted process still
requires intact authoritative state; corrupt state grants no recovered authority.
There is no unconditional cleanup or power-loss guarantee.

## Opaque mutation planning, no executable driver

The smallest typed interface describes create-network/create-container and exact-ID
start/stop/remove intentions. It accepts opaque `MutationIntent`, not argv, URLs,
mount maps, images or user Compose. Detached descriptions cannot change the sealed
intent; deserialization cannot manufacture one. Factory methods check journal/
lease scope and runtime binding. All descriptions say `execution_available: false`.
Only recording fakes implement the interface in tests. No production code invokes
it and no subprocess mutation adapter exists.

A trusted lease refuses caller-supplied success events for resource creation/
absence, worker completion or proxy absence. Presence inspection is implemented;
certified creation responses and actual absence confirmation are **not**. Therefore
reservations involving any attempted resource cannot be released in this increment,
even from a plausible complete journal. Only a never-attempted-resource reservation
can durably settle. Future transport/result certification and absence proofs require
separate review before this boundary can enable real provisioning or cleanup.

Primary references: [Docker Engine API](https://docs.docker.com/reference/api/engine/version/v1.56/)
for selected metadata spellings and [Engine 28 notes](https://docs.docker.com/engine/release-notes/28/)
for the gateway/endpoint prerequisites. These are implementation references, not
runtime verification. See [validation](../validation.md) for actual synthetic,
filesystem and bounded real read-only checks and the preserved bridge uncertainty.
