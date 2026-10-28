# ADR 0011: Closed transport preparation and conservative result certification

Status: accepted/merged for non-mutating M8B2C in PR #14 (Unreleased).

## Context

[ADR 0009](0009-owner-selected-service-labs.md) fixes the topology and
[ADR 0010](0010-authoritative-supervisor-state.md) binds single-use authorization,
private history and cooperative leases. Neither a mutation description nor an
exit-zero Docker response proves creation, removal or effective network isolation.
The preserved Desktop bridge observations also show why daemon ID and a Unix
socket are not sufficient evidence of uninterrupted runtime lifetime.

## Decision

Construct bounded opaque command plans from the closed graph and current consumed
journal. Permit only network/container create, exact-ID start/stop and exact-ID
container/verified-empty-network removal. Container creation is initially stopped,
uses immutable local image IDs and never pulls. No shell, Compose input, generic
argv setter, invocation driver or RUN/CLEANUP CLI is introduced.

Bind a private no-replace attempt sidecar to the current journal hash, runtime,
command digest and random single-use token. Preserve plan/journal wire formats.
Activation rechecks the lease, revision, exact target/network/image observations
and runtime. A one-use handoff expires after two seconds; results expire after
45 seconds; committing a positive certificate requires another runtime check
within ten seconds. These checks narrow TOCTOU windows; they are not Docker
transactions or scheduling guarantees.

Creation needs a durable creation intent, an unambiguous full ID returned by the
operation and fresh matching selected metadata. Never recover a lost create by
name, label or prefix. Removal needs a known exact ID, an unambiguous successful
response and two successful exact-ID filtered presence queries under stable
runtime continuity. Failed inspection alone cannot establish absence. Networks
additionally require every possibly attached container resolved before removal.
Nonzero/interrupted/ambiguous responses or inconsistent observations stay UNKNOWN.
Container running/exited observations do not certify worker completion or reports.

**Require qualified runtime continuity.** There is deliberately no production
provider: before/after daemon/socket/bridge equality cannot rule out a hidden
restart or stale Desktop response. Only explicitly synthetic test providers exist.
Without this provider activation and positive certification fail closed. This is
an unresolved engineering/review prerequisite, not a fabricated epoch identifier.

Result application accepts only an opaque certificate, never caller-supplied raw
success events. Keep proven in-memory IDs on recording failure, block provisioning
and refuse reservation release. Final release requires durable certified absence
for every known resource plus a new bounded absence/continuity check. Restarted
reconciliation, imported history and unknown creations cannot manufacture authority.

Prepare dedicated source-only catalog/fixture and independent contract-app recipes.
The dedicated worker implements **preflight only**, explicitly reports no execution
capability and emits no measurements. Do not build/pull/run these sources to satisfy
planning; an executable worker/result protocol and actual image qualification need
separate review and owner authorization.

## Alternatives and consequences

Exit-code-only success, failed-inspect-as-absence and label/name discovery are
rejected: all confuse ambiguity with authority. Treating daemon ID as an epoch is
also rejected. Using the old sample image would hide missing worker capability.

Availability is conservative: unknown creation, missing continuity, interrupted
recording, exhausted quotas and restarted certificate state retain reservations.
The closed command representation is reviewable and deterministic but not a
production transport. No effective isolation, actual creation/absence or custom
experiment is qualified by synthetic tests. No dependency or released evidence,
SQLite, plan, journal or experiment schema changes.

See the [contract](../design/docker-transport-certification.md),
[bootstrap preparation](../../deploy/custom-lab/README.md) and
[validation](../validation.md) for exact bounds, tests and residual risks.
