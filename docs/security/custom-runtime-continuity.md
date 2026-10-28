# Runtime continuity and integration gates — M8B3B review

**Current decision: fail closed. No actual custom Docker execution is supported or
authorized.** The M8B2C continuity provider remains absent. This milestone adds no
Docker invocation, runtime probe, setup command, image build or host setting change.
Local protocol tests do not satisfy any production continuity/cleanup gate.

## What Docker Desktop observations can establish

A fresh successful scoped API read can describe the selected daemon's reported
resources/configuration at that moment, under trust in the CLI/API/OS. A visible
socket/daemon/profile mismatch is grounds for refusal. A stable daemon ID, Unix
socket inode, bridge ID, response fingerprint or stream connection does **not**
establish uninterrupted daemon/VM lifetime or the absence of cached/reconnected
responses. Hashing those values or adding a client clock does not create an epoch.

Docker's [Resource Saver documentation](https://docs.docker.com/desktop/use-desktop/resource-saver/)
explains that Desktop can stop/restart its Linux VM and can serve some read-only
commands without waking it. This supports concern about host-side snapshots;
it does not establish exact cause/caller attribution of Faultline's preserved
bridge discrepancy. That observation remains UNKNOWN; no new inventory or event
query was run here. Settings must not be changed to force equality.

Disconnect/event monitoring may reveal failures, but an uninterrupted host-side
connection or no observed event is not an independently authenticated VM-generation
witness through a Desktop proxy. Event gaps/retention/timeouts also cannot prove
no restart. Without a trusted complete witness, M8B2C must continue rejecting
activation/certification and attempted-resource reservation release.

Daemon/worker death is not resource absence. Docker's
[live-restore contract](https://docs.docker.com/engine/daemon/live-restore/) permits
containers to continue while the daemon is unavailable. Toxiproxy toxic lifetime
also has no automatic worker lease here. A cooperative worker can attempt bounded
fault removal; SIGKILL or simultaneous supervisor/runtime failure can prevent it.
No unconditional fault-duration/cleanup guarantee is claimed.

## Practical fail-closed alternative

Keep Desktop custom execution disabled and continue using the local harness for
protocol validation. Existing reviewed fixed-sample workflows remain separate and
unchanged. Do not initialize a fresh ledger, clear a reservation, adopt a matching
name or weaken absence checks to turn an unqualified run into success.

A **future review candidate**, not a supported target, is a dedicated native Linux
runtime whose lifecycle is directly supervised: private socket/state, exclusively
trusted control and no automatic daemon replacement behind a host proxy. Rootless
Engine is a possible least-privilege candidate subject to prerequisites and
network-profile qualification; see [Docker rootless documentation](https://docs.docker.com/engine/security/rootless/).
No install/start instruction is executed or implicitly authorized by this proposal.

A reviewed implementation could bind a generation to a directly held kernel child
process capability (for example a Linux pidfd) and an explicit private supervisor
session, rechecking the direct API peer/process relationship for operations. A
[pidfd](https://man7.org/linux/man-pages/man2/pidfd_open.2.html) refers to a process;
it does not attest Docker isolation or that containers disappeared. This is a
lifecycle-witness design direction, not an implementation or proof that a normal
shared Engine/RootlessKit process chain supplies the required guarantees.

Before considering that profile supported, independently review the entire child/
launcher/containerd/process/socket chain, kernel and account trust, exclusive
access assumptions, restart/live-restore and supervisor crash behavior. Loss of
any witness must stop new work and retain unresolved reservations; a new daemon
generation cannot automatically certify old cleanup. Same-user/root administrators
can still bypass cooperative controls. Rootless routing/gateway/DNS/cgroup behavior
may differ and must pass actual probes without weaker fallbacks. No Linux runtime
has been qualified for this design in M8B3B.

## Remaining production components

| Component | Current state / required gate |
| --- | --- |
| Closed Docker invocation/response driver | Command representations and synthetic certificates only. No executable mutation driver; needs independent review/authorization. |
| Runtime continuity | No production provider. Desktop metadata-only continuity is insufficient. Keep fail-closed or review/qualify a narrower directly supervised environment. |
| Lost-response/restart reconciliation | Unknown creates/attempts retain reservations. No prefix/label adoption, automatic repair or recovered worker-output authority. |
| Network isolation | Alias resolution, DNS forwarding, backend bypass, control binding, host reachability and topology/restart probes remain UNVERIFIED. See the [matrix](custom-lab-dns-qualification.md). |
| Catalog/provenance | Source-only recipes and dependency-closure checks. No actual build/digest, worker entrypoint activation, image metadata or conformance qualification. |
| Production worker IPC/fault binding | New protocol/controller are private loopback-harness code. Need reviewed authenticated/scoped host-to-worker binding, actual API compatibility and stream/process supervision. |
| Whole-lab cleanup/release | Worker toxic removal is not proxy deletion or certified Docker absence. Exact-ID verified teardown, durable recording, restart recovery and final reservation release remain required. |

The Toxiproxy API identifies proxies/toxics by names and exposes no immutable
creation generation or authorization nonce. Even matching profiles and successful
HTTP responses cannot defeat a malicious/replacing server or concurrent API actor.
Only fresh owned isolated runtime qualification and trusted lifecycle/control
binding could justify that assumption later; no new metadata claim substitutes
for it. Lost POST responses in the current controller intentionally cannot authorize
removal by matching name, leaving cleanup unconfirmed for host-level recovery.

GO for review of the local protocol/control integration. **NO-GO for actual custom
Docker execution**, including on the Linux candidate, until every gate above and
explicit owner authorization is satisfied. No public deployment, release or CI is
part of this milestone.
