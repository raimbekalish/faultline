# Custom-service supervisor foundation — Unreleased M8B2A

This records the M8B2A foundation. Unreleased M8B2B adds
[fixed authority, leases and read-only observation](trusted-supervisor-boundaries.md)
without enabling execution. Historical missing-boundary statements below describe
M8B2A; future callers must now use that fixed authority rather than raw stores.

This is an internal Go contract and filesystem implementation, **not an executable
supervisor**. The production CLI still implements only `lab plan`; `lab run`,
`lab cleanup`, a worker, Docker mutation transport and image provisioning are
absent. The approved [design](custom-service-lab.md), [ADR 0009](../decisions/0009-owner-selected-service-labs.md)
and [threat model](../security/custom-service-threat-model.md) remain execution gates.
Nothing changes v0.1.0, existing plan bytes, sample targets or portable evidence.

## Four different claims

| Level | This increment |
| --- | --- |
| Planned resources | `GenerateGraph` deterministically derives private typed intent from a revalidated M8B1 plan. |
| Structurally validated generated configuration | Exact comparison with the closed generator rejects changed topology, commands, images or security/resource ceilings. This is not a generated Compose file, Compose normalization or daemon inspection. |
| Runtime-qualified isolation | **Not performed.** Effective gateway, DNS, control-plane and bypass protections need real adversarial probes on a reviewed runtime. |
| Authorized real execution | **Unavailable.** Internal consumption/state APIs do not expose execution capability. Future RUN additionally needs a fixed authoritative ledger, exclusive runtime slot, qualification and a reviewed mutation transport. |

## Closed graph

`internal/lab/graph.go` defines four internal bridge networks, isolated gateway
intent and IPv6 disabled. Every container is named from the exact fresh launch
identity, references an already-inspected immutable image ID and declares local-only
image acquisition (no pull/build). Container memberships are fixed:

| Role | Networks |
| --- | --- |
| Application | edge, data |
| Runner | edge, control |
| Toxiproxy | control, data, backend |
| Dependency fixture | backend |

The profile is the existing compiled catalog, including fixed executables/commands,
aliases, environment contract, UID/GID, read-only root, dropped capabilities,
no-new-privileges, resource/time ceilings, restricted DNS intent and no host
publications, privileged mode, host mounts or Docker socket. The graph accepts no
user Compose, plugin, external network, runtime hooks or arbitrary configuration.
A per-resource configuration digest binds the plan, owner nonce and exact resource
profile. It is a consistency reference, not a substitute for inspecting effective
configuration. Graphs include private ownership material and are not public output.

## Journal and authorization model

Private journal schema 1 has kind `faultline-lab-journal`, a distinct namespace
from plans/results/bundles. It stores the unchanged validated plan, graph digest
and an ordered, bounded event history. `execution_boundary` is always
`CONTRACTS_ONLY_NOT_RUNTIME_QUALIFIED`. State is derived by replaying every event;
there is no trusted stored boolean saying execution/cleanup succeeded.

`NewJournal` and `Advance` require exact plan hash, launch/run identities, nonce
and private runtime binding. The stored endpoint, socket device/inode, salt and
daemon fingerprint must match. No current-machine information is inferred by
these pure functions. A future caller must supply fresh trusted inspection facts,
not merely copy the saved binding to pretend it was rechecked.

The consumption snapshot is revision 000. Within the authoritative private store,
any object at that name blocks consumption, even a corrupt file or symlink.
A later revision without 000 is corruption, never unused authorization. The same
launch cannot be consumed again after failure, cancellation, interruption or
cleanup. Different plan bytes with the same launch identity also cannot consume
that identity again. Missing/corrupt state grants no cleanup authority.

The caller's state root is a **trust boundary**. A future RUN must use one fixed,
trusted ledger and cannot offer a caller-selected empty directory to bypass
consumption. This increment does not implement a global runtime lease or RUN
consumer; copying/deleting/rebuilding private state is not a supported replay
recovery mechanism. A malicious same-user administrator can rewrite local state;
no hashes or filesystem mode authenticate that actor.

| Recorded transition | Conservative meaning |
| --- | --- |
| consumption → prepared | Authorized identity is consumed; graph intent is prepared before any future mutation. |
| create intent → observed identity | Only the intended role/name/nonce/configuration with a unique full resource ID can enter the owned set. Container intent requires its networks already observed. |
| create intent → confirmed absent / unknown | Both stop further preparation. Unknown retains no cleanup-eligible ID; it cannot be adopted later from a matching name/label. Ambiguous creation reconciliation is deliberately deferred. |
| worker intent → running / exited / unknown | Worker intent requires all resources observed. Start intent already makes possible proxy activity unconfirmed; losing the response cannot imply no fault was created. |
| proxy cleanup intent → absence / unconfirmed | No global reset or inferred successful cleanup. Unknown worker/proxy state remains visible. |
| lab cleanup intent → exact resource intents → absence / unconfirmed | Cleanup stops creation/work. Known resources can be retried; unknown identities never become deletion candidates. |
| lab absence confirmed → receipt | Every resource must be confirmed absent or never attempted. Pending/unknown creation blocks completion. Final state cannot reopen RUN or preparation. |

A future transport's `Proof` means a verified exact-ID observation: full immutable
ID, expected generated name, nonce and digest computed from **complete allowlisted
actual configuration**, not read from a Docker label. The journal must already
record its creation intent and accepted identity. Future cleanup must re-inspect
that same ID/binding immediately before acting. Labels, prefixes and plan hashes
alone are insufficient. The current pure API has no Docker observation driver and
cannot itself establish that a supplied proof describes reality.

Container cleanup precedes network cleanup. Networks additionally require a fresh
verified empty observation and all containers absent/uncreated. An ambiguous
container therefore blocks network removal even if its unknown ID cannot be
acted on. Repeated cleanup skips known-absent IDs; it may retry a known ID after
an unknown delete response. It never searches/adopts by prefix or deletes images,
volumes, unknown IDs or unrelated resources. No delete implementation exists here.

A final `faultline-lab-cleanup-receipt` schema-1 record binds the exact journal hash
and plan identity. It confirms only **modeled whole-lab absence**, retaining the
proxy cleanup status, including unconfirmed. It does not assert successful
measurements, worker completion or runtime qualification. Identical receipt retry
is acknowledged; different/corrupt existing bytes are refused, never overwritten.

## Private persistence and recovery

`OpenJournalStore` requires an existing canonical, owned private directory. It
rejects symlinked path components, does not create directories and pins the opened
root. Generated record names contain only a validated launch ID and fixed revision.
Files must be owned, mode 0600, regular and singly linked; final symlinks, FIFOs,
public files and inode substitution are refused on qualified Darwin/Linux hosts.
Other hosts fail closed. Ancestor trust and absence of concurrent hostile in-place
writers remain operator/OS assumptions.

A stable `journal.guard` uses exclusive creation, validated existing-file opening
without create/truncate and advisory `flock`. The guard is retained: unlinking it
could split locks. Lock wait respects cancellation and a three-second store-operation
deadline; interrupted flock is retried. Process exit releases the kernel lock in
the tested host scenario. The guard is neither a Docker resource lease nor
protection from uncooperative administrators.

Updates publish immutable numbered full snapshots through the existing private
write/sync/atomic-hard-link-no-replace path; they do **not** replace earlier
snapshots. This chooses bounded append-only updates over mutable rename recovery.
Load validates all committed revisions, identical history prefixes, exact plan
identity and event counts. Stale updates, missing middle revisions, corrupt/torn
committed records and out-of-range revision names fail closed. There is no
"last valid record means safe" fallback. Only uncommitted temporary files are
ignored; they are not consumed authority and are retained after process interruption.

Limits: 64 KiB per snapshot/receipt, 96 events (97 snapshots including consumption),
4096 directory entries examined, three seconds per cooperative store operation.
The maximum committed snapshot payload is about 6.1 MiB per launch. At the event
ceiling, append refuses; it does not garbage-collect authority or invent cleanup.
No transaction, SQLite change, recursive deletion, automatic state repair or
unbounded audit log is introduced. These are foundation bounds; a future supervisor
must reserve enough transitions for teardown or handle persistence exhaustion
without suppressing cleanup of already proven-owned IDs.

A write/sync/cancellation error can leave a complete published revision. Reload
under the guard; never retry consumption as if no write occurred. A failed update
must prevent further provisioning, while the living future supervisor retains its
last proven in-memory IDs for independent-timeout cleanup. Store calls do not
perform cleanup and do not reuse a canceled work context automatically.

Tests use real temporary files, concurrent writers and a child exiting while
holding a guard after an incomplete temporary write. They establish the observed
host filesystem behavior and successful `File.Sync`/directory sync calls, **not**
power-loss, storage-controller, full-fsync, sleep/SIGKILL cleanup or arbitrary
filesystem durability guarantees. If a crash leaves a committed snapshot with a temporary hard-link alias, the
singly-linked-file check refuses it as unresolved; this increment does not repair
or unlink aliases automatically. Private history is intentionally retained.

## Qualification still required

All create/delete/start/worker operations in tests are synthetic recordings or
state events. No synthetic observation is a real Docker qualification result.
The [bridge diagnostic record](../security/docker-bridge-observation.md) supplies
supporting idle-lifecycle evidence, with missing events/causal attribution explicit.
It does not qualify this topology or justify changing host settings.

Before any real resource provisioning, independently review this foundation,
fixed ledger/lease wiring, bounded mutation adapter, image bootstrap and read-only
proof construction. Runtime qualification itself needs separate explicit owner
authorization and purpose-created probes. No selected application may start
before those gates pass. M8B2A does not begin that work automatically.
