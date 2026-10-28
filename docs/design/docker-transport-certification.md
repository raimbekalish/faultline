# Docker transport preparation and result certification — Unreleased M8B2C

Production remains **`lab plan` only**. This increment contains command
representations, private attempt recording and certification logic exercised with
recording fakes. It has no concrete mutation subprocess transport, no custom RUN
or CLEANUP CLI and no production runtime-continuity provider. No catalog image was
built/pulled or selected application executed. See [ADR 0011](../decisions/0011-closed-transport-certification.md).

## Separate claims

| Claim | Meaning and current evidence |
| --- | --- |
| Intended operation | Closed graph and sealed `MutationIntent`, bound to current consumed authorization. |
| Executable command representation | `CommandPlan` contains deterministic private argv, no executable or invocation method. JSON exposes only a redacted non-executable summary. |
| Recorded attempt preparation | Private atomic no-replace sidecar binds token, runtime, journal and command hashes. Recording does not establish dispatch or success. |
| Certified creation | Logic requires response ID, durable intent and fresh matching inspection/continuity; positive tests are synthetic only. No real resource certified. |
| Certified absence | Logic requires known ID, successful removal response and repeated scoped absence/continuity; positive tests are synthetic only. No real absence certified. |
| Runtime-qualified isolation | **Not performed.** Requires actual routing/DNS/control/bypass and lifecycle tests after separate authorization. |

## Closed command construction

The immutable catalog alone supplies commands, roles, resource ceilings, environment
and memberships. Recompute the closed arguments and hash at preparation; copying a
JSON description cannot manufacture a capability. All target IDs are full 64-hex
identities. A fixed local Unix endpoint stays private. These are representations,
not commands this increment executes:

- Network create: local bridge, internal, IPv4, isolated gateway intent, IPv6 off,
  exactly three owner/launch/role labels and generated exact name.
- Container create: `--pull=never`, immutable image ID, Linux ARM64, initially
  stopped, fixed absolute entrypoint/args, UID/GID 65532, read-only root, ALL caps
  dropped, no-new-privileges, private IPC/cgroup intent, bounded memory/swap/CPU/
  PIDs/nofile, no restart/log payload capture, restricted DNS and fixed stop timeout.
  Networks use recorded IDs with fixed aliases. Only application gets its fixed
  environment and bounded tmpfs; proxy gets empty PATH. No binds, host ports,
  socket mounts, host networking, exec hooks or arbitrary options.
- Start/stop/remove: exact recorded container ID only. Stop timeout is 12 seconds;
  removal has no force/volume option. Start is forbidden after cleanup-only entry.
- Network remove: exact freshly verified empty network, only after every potentially
  attached container is absent or uncreated. An unknown container keeps it blocked.

Argv is at most 16 KiB / 80 elements. No driver accepts arbitrary strings, YAML,
remote endpoints, tags, images, URLs or user environment options. Construction
uses documented [container-create](https://docs.docker.com/reference/cli/docker/container/create/)
and [network-create](https://docs.docker.com/reference/cli/docker/network/create/)
flags; their actual closed-profile behavior remains unqualified on Docker.

## Attempts, freshness and nontransactionality

`PrepareCommand` and `PrepareAttempt` require the held authority lease, exact
consumed plan/runtime and matching current journal revision. Create needs durable
CreateIntent; normal work is forbidden in aborted/cleanup-only/unconfirmed state.
Removal ordinarily needs durable DeleteIntent. Independent best-effort cleanup
preparation may use already proven in-memory IDs after recording failure, without
claiming durable success. No production code invokes a transport or handoff.

New private sidecars live under `attempts-<exact launch identity>` inside the fixed
0700 authority. Records are 0600, bounded, canonical and no-replace:
`NN.prepared.json` (separate schema 1, kind `faultline-mutation-attempt`, <4 KiB)
and `NN.result.json` (redacted certification summary). There are 32 durable slots;
old slots are never recycled. A prior prepared create at the same revision blocks
another create even after losing the in-memory ticket map. Partial sidecars do
not adopt resources or repair journals. The existing registry, plan and journal
wire encodings remain unchanged; no automatic recovery/migration is introduced.

Activation is bounded to 15 seconds, revalidates exact targets/networks/image and
runtime before/after inspection, and requires a **qualified continuity provider**.
The one-use private handoff must occur within two seconds of activation. Result
processing is bounded to 15 seconds, within 45 seconds of handoff. Commit rechecks
runtime/continuity and a ten-second certificate window. Releasing the lease,
changing revision/runtime, cancellation or cleanup-only entry rejects stale work.
Only `_test.go` recording wires consume the represented argv.

These deadlines bound cooperative operations, not a stalled kernel/fsync or daemon
transaction. Docker has no transaction spanning authorization, mutation and inspect.
Another same-user actor can attach, rename, delete or replace resources between
checks, including after the final observation. No claim of atomic isolation or
protection against a malicious Docker/OS administrator is made.

## Creation certification

A private future response channel must capture one full lowercase ID with only an
optional newline, bounded to 65 bytes, exit zero and no interruption. Extra output,
wrong/short/multiple IDs, lost reply, timeout or cancellation stays UNKNOWN. A
successful exit alone is never accepted. The claimed ID is used only in a temporary
candidate state to inspect the exact ID; it is not durably adopted first.

The existing selected observer repeats matching metadata, verifies runtime, exact
name/nonce/role/configuration/image/network membership and security constraints,
and recomputes the profile digest. A new network must be empty; a new container
must have status `created`, not already running. Only an opaque positive certificate
can advance Created. Unknown results advance CreateUnknown with no invented ID.
Missing/foreign/stale observations never authorize cleanup adoption. Start/stop
certify container state only, never worker/report/proxy success.

## Absence and release

Failed `inspect` is not an absence proof. Require the exact recorded ID, trusted
successful remove reply and two successful filtered ID queries using fixed
[container-list](https://docs.docker.com/reference/cli/docker/container/ls/)
(`--all --no-trunc --filter=id=ID --format '{{json .ID}}'`) or
[network-list](https://docs.docker.com/reference/cli/docker/network/ls/)
(`--no-trunc --filter=id=ID --format '{{json .ID}}'`). Empty output twice may support
absence only under qualified stable continuity. A full matching ID means PRESENT;
foreign IDs, duplicate/malformed output, inconsistent reads, errors, interrupted or
nonzero remove, runtime restart or unavailable continuity mean UNKNOWN. No broad
inventory search is used for adoption.

Network removal also needs all containers absent/uncreated and fresh empty-network
inspection before handoff. An ambiguous possible attachment retains the network.
A reported deletion followed by presence never advances DeleteAbsent. Persistent
reservation release additionally requires durable per-resource certified absence,
a settled journal and a new bounded 30-second scoped absence/continuity sweep.
Current-process certificates are not reconstructed from labels or copied sidecars.
After restart this conservative implementation cannot release attempted resources;
a separately reviewed reconciliation protocol is required.

There is **no production continuity provider**. Daemon ID, socket inode, before/
after metadata equality and even a bridge ID cannot prove uninterrupted lifetime
through Desktop cache/lifecycle changes. Synthetic epochs exercise logic only.
Default activation and attempted-resource release fail closed. This intentionally
blocks real certification until a reviewed source of adequate continuity exists.

## Recording failures and budgets

The original 96-event journal and admission reserves (21 cleanup + 21 retry events)
remain intact. Attempt records are separate, bounded and never grant authority by
themselves. At 32 tickets/slots, new normal preparation fails. Completed in-memory
tickets can be discarded for exact-owned cleanup only; durable slots stay intact,
recording becomes unconfirmed and no successful receipt/release is permitted.
There is at most one outstanding ticket per resource. Exhaustion cannot silently
suppress cleanup preparation or invent success. A future real driver must still
supply an independent cleanup deadline.

Commit preserves newly inspected IDs in a validated in-memory cleanup snapshot
before durable publication. Journal read/write or sidecar publication failure
blocks provisioning and release. This is living-process cleanup scope, not durable
crash recovery. Loss before durable ID recording stays unresolved. No automatic
name/prefix reconciliation, recursive deletion or resetting history is provided.

## Before any real qualification

Require independent code/security review and explicit owner authorization; review
and implement qualified runtime continuity, the concrete closed invocation/response
channel, restart/lost-response reconciliation and failure-record durability. Finish
and review the measurement-capable worker protocol; explicitly build the dedicated
catalog and independent app from reviewed source, inspect exact immutable identities
and metadata, then run adversarial isolation/conformance and scoped lifecycle tests.
Do not relabel an original sample image or infer capability from a contract label.
Only after those gates may actual custom latency/recovery measurement be considered.
No custom evidence is inserted into existing sample history, format-1 bundles/gates
or tracing contracts. This increment adds no dependency, released command or schema.
