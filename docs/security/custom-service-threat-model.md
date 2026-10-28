# Custom-service lab threat model — proposed M8A design

Status: approved architecture; M8B1 planning and Unreleased M8B2A internal contracts
are implemented. Nothing here enables custom execution. Reviewed
source: `257eab735c16e9daa2a700d88e329f84c72622af`; research date: 2026-10-27.
See [ADR 0009](../decisions/0009-owner-selected-service-labs.md) and the
[interface/lifecycle/test plan](../design/custom-service-lab.md).

## Assets, actors and assumptions

Protect the host filesystem and credentials, unrelated Docker resources/networks,
application images owned by the user, integrity of recorded observations, and
exclusive fault/cleanup authority. Only disposable lab resources are expendable.

The operator authorizes a specific plan and trusts their local OS, Docker daemon,
Docker/Compose executable/plugin and reviewed Faultline build. The selected image
must be owner-reviewed/trusted and contain no real data or secrets. Its behavior
may be buggy; it must not be treated as a sandboxed hostile tenant. Untrusted
descriptors, malformed image metadata, hostile responses and interrupted control
operations remain in scope even when the operator is trusted.

Out of scope: a malicious host user with Docker authority, compromised daemon,
kernel/container-runtime escape, a malicious Docker executable/plugin, colluding
administrators or an OS-level attacker rewriting private journals. These actors
can change networks, read containers or forge labels independently of Faultline.
Do not claim a project prefix, hash or plan confirmation authenticates a human,
an image publisher or a Docker administrator.

## Trust boundaries and network graph

```text
owner's local descriptor → strict parser → closed adapter + immutable image IDs
                                             │
                                  single-use authorized plan
                                             │
                              trusted native host supervisor
                              │ private intent/cleanup journal
                              │ local Docker Unix socket (host only)
                              ▼
              fresh, run-owned Linux container lab

 runner ── edge ── frontend application
   │                    │
 control                data
   │                    │
   └─────────────── Toxiproxy ── backend ── fixture dependency
```

All four networks are fresh, internal bridges in isolated gateway mode; IPv6 is
disabled for the initial qualified profile. App: edge/data only. Runner:
edge/control only. Toxiproxy: data/control/backend. Fixture: backend only.
No host publications, external/other-run networks, default bridge, host aliases
or shared network/PID/IPC namespaces. The runner exposes no listening API.
A temporary **trusted** probe uses the app's intended attachments before the app
starts; it is removed and its absence confirmed. It never probes an unrelated
service. Qualification uses only purpose-created sentinel endpoints.

Toxiproxy must listen for control on the address of its control-only alias
`toxiproxy-control`, port 8474. Its data listener is port 8666; only fixed
`dependency:8080` is upstream. A multi-homed proxy listening for administration
on all interfaces would defeat segmentation. The pinned 2.12.0 server has a
`-host` option, whereas its image defaults to `-host=0.0.0.0`; the custom profile
must explicitly replace that default. Resolve/bind the control alias to exactly
the inspected control address; ambiguity, bind failure or a reachable data-side
admin port is a refusal, never a wildcard fallback.

Default Docker DNS can forward external lookups. Proposed containers set the
external DNS destination to their own loopback (`127.0.0.1`), with no search
suffix; embedded service-name resolution must still work. **This mechanism is
unqualified** until real tests prove local aliases work and external queries do
not escape on the exact multi-network runtime. DNS failure must not trigger a
public resolver fallback. An application can run its own resolver, but must have
no route outside its owned graph. Disable IPv6 and test literal-address bypasses,
not just DNS names. These controls are not substitutes for a trusted runtime.

## Invariants and enforcement limits

| ID | Required invariant | Proposed enforcement / failure behavior |
| --- | --- | --- |
| S1 | Explicit scope and consent | Descriptor names one adapter/image; run requires exact single-use plan hash. No implicit target discovery, descriptor hooks or ambient target variables. A plan authorizes only its listed lab, not infrastructure changes elsewhere. |
| S2 | No public/remote target | No URL, host, IP or port fields. Fixed internal route/upstream. Resolve Docker selection once using existing context-over-host precedence; reject TCP, SSH and other non-Unix endpoints before mutation. A local Unix relay's physical destination cannot be independently attested; trusted local configuration/operator is required. |
| S3 | No container access to Docker/host files | Zero host bind mounts, volumes, devices, secrets or socket mounts in this custom path, including read-only input/history mounts. Bounded spec is supplied over the runner protocol. The trusted host coordinator keeps journals/results. |
| S4 | No elevated execution | Non-root UID/GID 65532, read-only root, all capabilities dropped, no-new-privileges, default qualified seccomp/LSM policy. No privileged mode, host networking, shared namespaces, unsafe sysctls, builds, post-start hooks or user entrypoints. |
| S5 | No unrelated-network/control access | Generated graph only; isolated gateways; closed DNS; dedicated control bind; exact attachment inspection and negative probes before app start. No published ports, proxy API token workaround or collector exposure. Unknown/unsupported isolation is a refusal. |
| S6 | Strong ownership, no reuse | 128-bit random launch and proxy IDs; nonce/role/template labels plus immutable resource IDs in a private journal. Refuse existing names, foreign labels and unverified IDs. Never identify delete authority by name prefix/project alone. No shared Toxiproxy server or global reset. |
| S7 | Bounded work | Existing 1–100 serial samples/phase, 1–5000 ms latency and 10–5000 ms request timeouts. At most 20 readiness requests, five seconds total, so at most 320 workload/readiness requests. Native work deadline 180 s, engine 120 s, fresh outer cleanup budget 30 s. These bound cooperative Faultline work, not arbitrary app fan-out or stalled OS calls. |
| S8 | Cleanup independent of cancellation | Register proxy deletion before create; fresh five-second engine cleanup. Host teardown has its own context/budget, recorded IDs and final absence checks. Any failure/unknown state is reported and blocks reuse; cleanup never relies on the canceled work context. |
| S9 | No payload/credential collection | No user environment/headers/bodies/auth settings. Refuse application images with baked environment entries; inspect counts rather than copy their values. Disable application Docker logging. Bounded response drain is needed for timing, but content is discarded, never logged, parsed for business data or persisted. No screenshots, automatic app logs or arbitrary OTel data. |
| S10 | Fail-closed records | No custom report is sent through current sample-only validators/history/export/gates. The distinct custom result identifies adapter/image/workload and both cleanup layers. Missing facts, partial phases and interrupted operations are explicit, never synthesized as success. |

These are implementation requirements and qualification criteria, **not claims
that M8A or existing Docker defaults already enforce them**.

## Threats and proposed mitigations

| Threat | Mitigation and residual risk |
| --- | --- |
| Malformed descriptor, duplicate/case/escaped keys, invalid Unicode, oversized/deep JSON, overflow or fields masquerading as Compose | Reuse `jsonstrict`; closed 8 KiB descriptor schema and existing numeric validation; reject unknowns, nulls, unsupported versions and interpolation. Parser/planner must complete before any Docker mutation. |
| Arbitrary Compose input imports host files, env or remote configuration | Never accept user Compose/YAML, overrides, `include`, `extends`, `env_file`, build contexts or plugin definitions. Do not pass them to `compose config` even as a supposed validation step: evaluation can already read files. Only generate trusted typed configuration. |
| Ambient Compose/Docker settings change the plan | Pin the selected Unix endpoint and trusted CLI paths; use a fresh private project directory, explicit generated file/project and empty env file. Clear inherited Compose and target/proxy/OTel controls from subprocesses. Revalidate normalized generated configuration and actual containers; no auto-pull/build/recreate. Host CLI configuration itself remains trusted. |
| Image defaults run hooks, allocate volumes, expose secrets or exhaust host logs | No automatic pull/build; exact local image ID/platform. Refuse declared volumes, image healthchecks, OnBuild triggers and application environment defaults. Override executable/args, user, working directory, stop signal and restart policy from the adapter. Disable container logging and use bounded tmpfs/resource limits. Image layers can still contain secrets or malicious code; metadata inspection is not an image audit. |
| App reaches admin API and creates arbitrary proxies | Bind administration only to the control interface; no app membership there. Test control-network literal IP as well as data aliases from app attachments; merely hiding a DNS name is insufficient. The API remains trusted-only; no authentication feature from newer Toxiproxy versions is assumed. |
| App bypasses the fault, uses DNS to exfiltrate or connects to host/unrelated containers | No backend membership, isolated gateways, disabled IPv6 and restricted DNS. Test owned cross-network/host sentinels and DNS sinks. Ordinary `internal` mode is insufficient for host-gateway isolation. Host/runtime misconfiguration or escapes can defeat networking; refuse unqualified configurations. |
| App ignores the dependency, caches responses or fabricates successful replies | Require a reviewed adapter contract and conformance tests using a trusted fixture counter, including bypass attempts. Record actual HTTP outcomes; do not infer that a 400 ms fault guarantees a 504 for every app. A dishonest application can still forge success; no behavioral attestation or causal guarantee. |
| Fork/memory/log/response floods or background work | App: 128 MiB, 0.5 CPU, 64 PIDs, nofile 256 and at most 32 MiB tmpfs. Other roles at most 64 MiB/0.5 CPU/64 PIDs each; probe also bounded. One custom lab per user/runtime, no restart, 64 KiB response cap and finite subprocess I/O. Limits do not cap all daemon/VM overhead or app-originated request counts. Noncompliant retry/fan-out/background workloads are unsupported. |
| Symlink/path substitution, plan replacement, image retagging | Bounded regular files, no final symlink, no-follow/nonblocking open and inode recheck on qualified OSes. Trusted parent directories and private generated state; no paths derived from descriptor strings. Hash validated canonical bytes. Image IDs, not tags; recheck at run. Same-user in-place rewriting is outside an attested snapshot guarantee. |
| Name collision, concurrent launch, cleanup of another owner's resources | Exclusive private runtime slot; fresh random IDs; no-recreate creation, inspect before start, exact journal IDs for teardown and daemon-binding checks. Partial create discovers only matching nonce/role/config resources. Names/labels alone cannot resist a malicious Docker administrator. |
| Lost create/delete response, canceled CLI, killed worker or failed persistence | Durable intent before create; register cleanup before proxy create; reconcile observed IDs after ambiguous outcomes. Host deadline/teardown survives a worker failure while supervisor lives. Receipt writes are atomic/no-replace; a failed final write remains an error with journal retained. Never hold a DB transaction through measurements. |
| Supervisor SIGKILL, host sleep/crash, unavailable daemon | No unconditional fault TTL exists in current Toxiproxy. Both controllers gone or daemon inaccessible means cleanup unconfirmed, not guaranteed after a deadline. Retain journal; explicit scoped cleanup after daemon recovery, never auto-resume measurements or infer why they stopped. |
| Private logs, paths, host names or Docker metadata leak into evidence | Query allowlisted inspect fields only, cap/redact subprocess diagnostics and do not copy labels/env/command output. Public result uses image/content/adapter identifiers, platform and observed software versions. Endpoint/daemon-binding fingerprint and resource IDs stay private in recovery state. No host identity/IP inventory. |

## What Faultline cannot enforce alone

Docker's Linux isolation depends on the daemon, firewall backend, kernel and
privileges. Faultline must not edit host firewall, quarantine, Docker settings or
security policy to make a failing qualification pass. Use a separately provisioned
qualified local runtime if needed; do not automatically create or reconfigure it.
Docker may install its normal scoped rules when creating owned networks; that is
not permission for Faultline to rewrite global firewall or daemon policy.
Host processes/administrators retain access to their daemon and can interfere.
An image digest is not provenance authenticity, absence of embedded secrets or
proof the application honors the adapter. Dependency/request bounds constrain
Faultline, not hostile image behavior. Suspend/crash/SIGKILL can outlive timers;
no cleanup or durability guarantee covers those conditions. Operator trust and
these limitations must appear in the plan UX before authorization.
Output caps also do not bound a trusted Docker CLI's internal heap or all runtime
overhead; no total host-memory or disk-isolation guarantee is claimed.
The native supervisor has the operator's broad Docker authority. Its scoped
allowlist is an application control, not a daemon-enforced tenant permission.
Where that authority is unacceptable, a separately administered dedicated local
daemon/VM is required; provisioning or hardening it is outside this MVP.

## Primary-source research (2026-10-27)

These sources inform the proposed controls; documentation is not runtime testing.

- [Compose trust model](https://docs.docker.com/compose/trust-model/): Compose files
  exercise host authority, including transitive file/config references. Rejecting
  arbitrary files is safer than evaluating and trying to sanitize them afterward.
- [Internal network behavior](https://docs.docker.com/reference/cli/docker/network/create/#network-internal-mode---internal):
  network isolation does not by itself remove bridge-gateway host access.
- [Isolated gateway mode](https://docs.docker.com/engine/network/port-publishing/#gateway-modes)
  and [Engine 28 notes](https://docs.docker.com/engine/release-notes/28/): the bridge
  can omit its host address. Require capability and behavioral qualification.
- [Docker DNS](https://docs.docker.com/engine/network/#dns-services): embedded DNS
  forwarding and per-container resolver selection require explicit treatment.
- Toxiproxy **v2.12.0**, not latest: [server flags](https://github.com/Shopify/toxiproxy/blob/v2.12.0/cmd/server/server.go),
  [image defaults](https://github.com/Shopify/toxiproxy/blob/v2.12.0/Dockerfile),
  [API](https://github.com/Shopify/toxiproxy/blob/v2.12.0/README.md#http-api).
  Separate administration and data-plane control; do not rely on newer features.
- [Docker security](https://docs.docker.com/engine/security/): runtime/daemon
  authority remains a trust boundary rather than an image-sandbox guarantee.

## M8B2A findings and preserved boundaries

The [supervisor foundation](../design/custom-service-supervisor.md) validates
intended configuration and replayed private journal transitions; it cannot attest
actual Docker observations. Future drivers must check complete effective profiles
and immutable IDs, not turn labels or plan hashes into cleanup authority. Unknown
creation has no adopted ID. Ambiguous container attachments retain networks.
A consumed identity stays consumed after failure, interruption or final receipt.

The ledger root and advisory guard are trusted local state, not authenticated
execution attestation. Future RUN must use a fixed authoritative namespace and
exclusive runtime slot; a caller-selected fresh directory cannot reset replay
protection. Store/event limits and I/O failure must never prevent independent
cleanup of already proven in-memory IDs. Corrupt committed history is unresolved,
not permission to fall back and provision again. Private paths/nonces/bindings and
resource IDs remain outside public evidence.

The [read-only bridge investigation](docker-bridge-observation.md) found Docker
Desktop idle stop/start records coinciding with historical bridge creation times.
This supports a lifecycle explanation without establishing exact causal/caller
attribution. Event queries timed out. Read-only requests do not freeze automatic
runtime lifecycle; identity/configuration must be rechecked during future
qualification. No Docker settings, runtime or isolation policy was modified.
