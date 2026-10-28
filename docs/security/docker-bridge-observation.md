# Default bridge observation — bounded read-only follow-up

Date: 2026-10-27 PDT / 2026-10-28 UTC. This records an environment observation,
not a custom-lab qualification. See the [threat model](custom-service-threat-model.md).
No Docker restart, settings change, resource create/delete, image acquisition or
application execution was requested. Diagnostics used the trusted local Docker
CLI against the selected Unix endpoint; raw endpoint/daemon identity and inventories
remain in private ignored artifacts, not public results.

## Observed facts

- M8B1 snapshots saved around 04:13–04:27 UTC have identical container, image,
  volume and unrelated network IDs; the default bridge differs. A separately
  retained bridge inspect record says creation at **04:18:14.408433875 UTC**.
  The next retained bridge has creation **04:24:48.024185583 UTC**.
- The bounded Docker Desktop backend log contains `starting VM` and engine
  stopped → starting at **04:18:13**, followed by engine start complete at
  **04:18:15**. It records idle VM stop at **04:23:16**, stopped confirmation,
  VM/engine starting at **04:24:46**, and start complete at **04:24:48**.
  These bridge creation timestamps fall inside the observed startup windows.
- The same log shows a five-minute idle timer and later VM stops/starts, including
  a startup during this diagnostic session. Read-only API calls are not a promise
  that Docker Desktop's own idle lifecycle remains frozen. We did not issue a
  restart or change its policy.
- Selected daemon inspection reported Docker Engine **29.8.0**, Linux ARM64.
  A current daemon identity was read privately. M8B1 did not retain a raw earlier
  daemon identity suitable for proving continuity; daemon identity alone would
  not establish uninterrupted VM lifetime in any event.
- Two bounded event queries (historical 04:00–04:50 UTC and a recent twenty-minute
  window) each timed out after eight seconds. No usable event history was obtained.
  This is **not** evidence that no other Docker activity happened.

Only bounded selected log data was examined; no credentials, application payloads,
IP inventory or arbitrary user files were collected. Public findings omit raw
backend lines, endpoint paths, daemon identity, resource names and ownership tokens.

## Attribution and missing evidence

The observed idle stop/start cycles provide **supporting evidence for an automatic
Docker Desktop lifecycle explanation** of bridge recreation; the prior observation
is no longer unsupported by restart evidence. They do not prove the complete causal
chain or identify which caller triggered each wake. Exact bridge create/delete
and caller attribution remain **UNKNOWN**. No evidence establishes that Faultline
issued a mutating Docker command; retained planner/diagnostic commands are read-only.
Conversely, a read-only command audit is not a complete audit of concurrent actors.

Missing: retained network lifecycle events, complete daemon generation/uptime
records around every earlier snapshot, and an attributable caller audit. Historical
retention/timeouts cannot be repaired by recreating resources. No attempt was made
to force an inventory-equality pass, disable idle management or restart Docker.
Future qualification must bind/check effective runtime resources and react to
changed or unavailable state; a saved plan or stable label is insufficient.

Global inventory equality must continue to be reported as failed whenever this
bridge difference is present, even when unrelated resources remain unchanged.
The separate supervisor tests prove only pure/synthetic and tested local
filesystem behavior. They provide no real network isolation qualification.

## End-of-task inventory qualification

The follow-up inventories again have unchanged container/image/volume IDs and the
same five known unrelated network IDs, with one network ID added and one removed.
A final bridge metadata read reported creation at **05:36:07.017801875 UTC**, but
that ID was **not present in the immediately preceding network-list snapshot**.
Those reads therefore cannot be represented as one atomic daemon snapshot or as
an exact-ID attribution of that final network difference. Global inventory
equality failed; the final-only-default-bridge matching assertion was false.
The known unrelated networks remain unchanged, but the mismatch is retained as
an unresolved observation, not repaired by retrying until equality appears.

## M8B2B follow-up (2026-10-28 UTC)

Existing read-only diagnostics again reported unchanged container/image/volume IDs,
but one network ID changed between snapshots. A subsequent bridge inspection
reported creation at **06:58:24.196007041 UTC** and an ID absent from the preceding
network list. Global equality and exact final-difference attribution therefore
failed again. The reads cannot be represented as an atomic state snapshot.
No event-history retry or attempt to force equality/restart the VM was made.
Historical exact cause/wake caller remains UNKNOWN. Fresh binding/configuration
checks fail closed on visible mismatch, but stable metadata alone cannot attest
uninterrupted VM life, cache freshness or effective network isolation.

## M8B2C follow-up (2026-10-28 UTC)

Bounded trusted local CLI reads reported Engine 29.8.0/Linux ARM64 and 9 containers,
6 networks, 67 image IDs and 10 volumes. Start bridge creation was
06:58:24.196007041 UTC, with its ID present in that list. At the end, container,
image and volume IDs were unchanged, but one network ID was added and one removed.
A subsequent bridge read reported **22:05:16.764570541 UTC**, with an ID absent from
the immediately preceding list. Global equality and exact final-difference
attribution failed again. No retry, restart or configuration change forced equality.

All task-issued Docker operations were read-only; no catalog build/pull, container
start/stop/create/remove or network mutation was performed. The earlier idle
lifecycle explanation remains plausible; exact cause/caller attribution is UNKNOWN.
This is not a complete audit of other actors or Docker Desktop internals. No new
event-history evidence was collected. The M8B2C certification boundary deliberately
has no production continuity provider: snapshot equality alone cannot attest an
uninterrupted runtime, and a mismatch must fail closed.
