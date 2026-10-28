# Owner-selected HTTP service lab — M8A design

**Approved architecture; execution unimplemented.** M8B1 source implements only
the [strict descriptor/catalog and read-only planner](../custom-service-planner.md).
Unreleased M8B2A adds internal [graph/journal contracts](custom-service-supervisor.md).
M8B2B adds fixed authority/read-only observation; M8B2C adds
[transport preparation and synthetic certification contracts](docker-transport-certification.md).
No mutation driver or qualified continuity provider exists. M8B3A adds a
[private measurement engine and ephemeral result contract](custom-worker-measurements.md)
using local HTTP test harnesses, without changing production preflight.
RUN, CLEANUP and the execution/qualification design below remain future work;
none of these features is in v0.1.0. This document accompanies
[ADR 0009](../decisions/0009-owner-selected-service-labs.md) and the
[threat model](../security/custom-service-threat-model.md). Independent review is
required before implementation; runtime qualification is required before enabling
custom execution. Current sample experiments remain the only supported targets.

## Source-grounded assumptions

Inspected source at `257eab735c16e9daa2a700d88e329f84c72622af`:

| Current location | Sample-specific assumption / consequence |
| --- | --- |
| [Compose](../../deploy/compose.yaml), [wrapper](../../scripts/experiment.sh) | A single internal `lab` network connects frontend/dependency/runner/control. Frontend health runs `/app/sample -health`. CLI builds are trusted repo targets. Persistence mounts selected input/history paths. Project trap removes its Compose stack; builds/startup sit outside the engine timeout. None is an arbitrary-image adapter. |
| [Engine](../../internal/experiment/run.go) | `Target` is `http://frontend:8080/work`. One random `faultline-<32 hex>` proxy ID, fixed-port collision instead of sharing, two-minute execution, baseline-failure refusal, recovery validation, proxy cleanup registered before creation with fresh five-second context. Readiness is time-bounded but lacks an explicit request-count cap. |
| [Toxiproxy](../../internal/toxiproxy/client.go) | Fixed control `toxiproxy:8474`, listener `0.0.0.0:8666`, upstream `dependency:8080`, one downstream latency toxic, zero jitter, toxicity 1. Name syntax alone is not proof of ownership on a shared server. There is no fault TTL or global reset in the supported path. |
| [Sample](../../internal/sample/handler.go), [server](../../cmd/sample/main.go) | `/work` calls `/value` through the proxy with a 250 ms client deadline. A transport timeout yields 504; other dependency failures may yield 502. Both roles serve `/healthz` on 8080; dependency returns fixed public data. Custom apps do not automatically have these semantics. |
| [HTTP](../../internal/localhttp/client.go), [measurement](../../internal/experiment/measure.go) | Serial bodyless GETs, proxy environment ignored, no redirect following, 64 KiB response cap; all outcomes enter summaries. Request timeout includes body reads. Statistical calculations already have one implementation. |
| [Telemetry](../../internal/telemetry/telemetry.go), [trace query](../../internal/tracequery/topology.go) | Fixed route names, internal Jaeger endpoint, bounded SDK queues, safe metadata, and exactly four named sample span roles with strict parent edges. These cannot establish completeness of a different app. |
| [Spec](../../internal/spec/spec.go) | Schema 1 has exactly four integer fields and no topology/image identity. Its hash identifies measurement settings only. Do not append an arbitrary target flag. |
| [Execution](../../internal/execution/run.go), [history](../../internal/history/store.go) | Intent before engine, final persistence after proxy cleanup/export. Current finalization precedes the host wrapper's whole-stack teardown. SQLite stores typed schema-1 specs/reports; it cannot honestly describe the new outer cleanup scope without an explicit design. |
| [Record](../../internal/record/validate.go), [provenance](../../internal/provenance/provenance.go) | Report target must equal the sample constant; strict provenance accepts only sample/toxiproxy/optional Jaeger components and the existing methodology. Omitting provenance or reusing the frontend URL must not disguise a custom app as the sample. |
| [Bundle](../../internal/bundle/bundle.go), [comparison](../../internal/bundlecompare/compare.go), [gates](../../internal/bundleevaluate/evaluate.go) | Format 1 validates a schema-1 sample report/spec. Comparison checks a fixed provenance list. Gates revalidate and require completion/cleanup, but cannot infer topology from the four measurement settings or an unknown optional field. Preserve every current contract. |

Reviewed related lifecycle, transport, ownership, real integration, trace topology,
spec, provenance, history/recovery, archive/contract and policy tests, plus
[SECURITY.md](../../SECURITY.md) and ADRs 0001–0008. Relevant existing behaviors
include ambiguous-create cleanup, another owner's proxy sentinel, actual 80/400 ms
timeouts/recovery, canceled persistence, export failure after fault cleanup and
strict four-span completeness. This was source/test inspection, not new execution.

## MVP and adapter contract

One built-in adapter, `http-work-v1`, replaces only the frontend with a trusted,
owner-selected local Linux image. The rest is a new Faultline-owned disposable
lab. The owner adapts their app to this contract; this is not arbitrary-app support.

- Fixed executable `/app/service`, no arguments, UID/GID 65532, working directory
  `/`; listen HTTP on 8080 and implement bodyless `GET /work`.
- Initially qualify a static Linux executable in a scratch-style image with no
  inherited environment. Conventional framework/runtime images are not presumed
  compatible with this restricted first adapter.
- Receive only two protocol environment entries: `FAULTLINE_DEPENDENCY_URL`
  fixed to `http://toxiproxy:8666/value`, and `FAULTLINE_DEPENDENCY_TIMEOUT_MS=250`.
  One dependency call per work request, no retries, fan-out or background traffic.
  The timeout is configured/contractual, not an inferred fact about image behavior.
- Use disposable test state only. No credentials, runtime downloads, migrations,
  user files, database, authentication, external telemetry or other service needs.
- Return actual HTTP results. The independent qualification app implements 504
  on its dependency timeout; Faultline does not rewrite another app's responses
  or assume every 400 ms fault produces that code.
- Faultline supplies a trusted constant-data dependency fixture. Qualification
  checks actual proxy-path use with a trusted test-fixture request counter; a
  slow response by itself is not proof of the dependency path or causality.

The test app must be independently built from small reviewed source, with no import
of Faultline's sample handler and no renamed sample image. An example is a small
quote-calculation endpoint using the fixture as a price provider. User builds are
outside the run operation; no build context or Dockerfile path is accepted by it.

## Proposed descriptor and authorization interface

Illustrative descriptor **only**; angle-bracket values are placeholders, not IDs
to execute. No example file is added to the currently accepted experiment schema.

```json
{
  "schema_version": 1,
  "adapter": "http-work-v1",
  "image_id": "sha256:<64 lowercase hexadecimal digits>",
  "platform": "linux/arm64",
  "experiment": {
    "schema_version": 1,
    "samples_per_phase": 5,
    "latency_ms": 80,
    "request_timeout_ms": 1000
  },
  "tracing": "off"
}
```

All six fields required; no extension map. Maximum 8 KiB, depth no more than 32,
one object, strict UTF-8/surrogates, escaped/case/simple-fold duplicate rejection,
exact lowercase keys, no nulls/unknown fields/trailing input. Reuse `jsonstrict`
and the existing experiment numeric validator (1–100 samples, 1–5000 ms latency,
10–5000 ms requests); integer literals only. Canonicalize the typed descriptor
for a stable **configuration hash**. This outer namespace is distinct from
`spec.Spec`; nested measurement settings retain their current meaning.

The only initial platform candidate is Linux ARM64 in the local Docker Desktop
Linux VM on the owner's Mac. It is **not yet qualified**. Other Linux/architecture
and Compose combinations need separate tests; emulation/build success is not
runtime qualification. Windows/custom rootless/alternate network drivers are
unsupported until qualified, without weakening existing offline portability.

Interface (only `lab plan` is implemented in M8B1 source; run/cleanup are future):

```text
faultline lab plan --application app.json --out PLAN.json
faultline lab run --plan PLAN.json --authorize-plan <exact-plan-sha256>
faultline lab cleanup --journal JOURNAL.json --authorize-plan <exact-plan-sha256>
```

`plan` performs read-only runtime/image inspection and writes only the explicitly
selected no-replace private plan file. It never creates networks/containers,
builds, pulls or starts an app. It resolves the Docker context/host selection
once, rejects non-Unix endpoints, and records a private local runtime binding.
No descriptor field can override that selection. A Unix socket is a necessary
locality check, not attestation that a user has not configured a remote relay.

The plan contains canonical descriptor, adapter/template version and content hash,
all already-local runner/fixture/Toxiproxy/app image IDs, configured safety limits,
fresh launch/proxy IDs and a single-use authorization nonce. Its hash binds this
exact operation, distinct from the reproducible configuration hash. Private state
binds the selected socket identity and a per-plan salted daemon-ID fingerprint;
neither raw endpoint paths nor that runtime fingerprint enters public results.
Show image/platform, fixture substitution, zero mounts/ports, limits and trust/
cleanup limitations before the operator supplies the exact hash to `run`.

Trusted runner/fixture images must already be outputs of a reviewed Faultline
build/catalog; the proxy must match the catalog's pinned reference and inspected
local image ID. Validate their worker protocol/build compatibility before app
startup. The descriptor cannot replace these trusted components. Image acquisition
is a separate explicit owner operation, never an implicit pull/build in planning,
run or recovery. Existing image cache entries are read-only inputs, not owned
containers to delete.

Run rechecks the descriptor, catalog, image IDs/platform and runtime binding.
Changed bytes, consumed nonce, lost binding, missing image or unqualified runtime
means refusal; there is no `--force`, generic `--target` or unattended fallback.
A flag is explicit operator authorization, not authentication of human identity.

### Authorization lifecycle resolved for M8B1

RUN consumption and CLEANUP identity are different operations. A future RUN must
atomically create/sync a no-replace consumption record binding the entire plan
hash, launch/proxy IDs, nonce and private runtime binding **before provisioning**.
Concurrent/repeated RUN attempts fail once that record exists, including after
cancellation, failed creation, successful cleanup or a crash. A failed attempt
does not make the plan reusable. Generate a new plan for a new run. Never infer
unused authorization from resource absence or delete consumption records to retry.

The same hash/identity may subsequently authorize cleanup-only recovery of that
consumed run, including repeated idempotent cleanup attempts. CLEANUP requires
the matching private consumption record/journal, runtime binding and verified
owned resource IDs. The plan itself contains only intended roles/names, not
observed resource ownership. It grants no permission to adopt resources, search
by prefix or delete unrelated IDs. No journal, corrupt binding or unknown ID means
refusal; cleanup must never re-enable RUN, launch traffic or modify old evidence.

The M8B1 CLI only produces PLANNED, unqualified plans and their hashes. It creates no
consumption records, leases or resource journals, and implements no RUN/CLEANUP
command. Hashes bind content, not a human identity or resource ownership. Future
execution must revalidate the plan/catalog/runtime and satisfy the implementation
qualification gates; a successful plan is not an executable safety certificate.

## Files, image defaults and generated Compose policy

Input descriptors/plans/journals are bounded regular local files. Reuse the policy
loader's no-follow/nonblocking open and inode checks on qualified Darwin/Linux
hosts; reject URLs, final symlinks, devices, FIFOs and excess length. Parent
directories must be trusted. A same-user in-place writer is not an attested input
snapshot. Keep immutable validated bytes in memory; never reread a path as an
authorization shortcut. Cancellation is cooperative and I/O stays bounded.

Require an explicitly trusted private state directory (0700), generated children
only and no symlinked state components. Create the intent exclusively; update only
this run's recognized mutable journal under its lock using synced temporary files
and atomic rename. Recheck owner/plan identity before replacement. Plans and final
results/cleanup receipts use atomic no-replace publication, never journal-update
semantics. File/directory sync failures are errors, with OS/filesystem limitations
explicit. Keep descriptor paths out of generated container mounts and
out of public reports. Never recursively remove a descriptor-derived path.
One private per-runtime lock slot plus any unresolved prior journal blocks a new
custom run. This is cooperative single-user coordination, not a cross-user lease.
Journal size cap 64 KiB; refuse overflow before the next mutation. Results cap
1 MiB; subprocess response caps and typed fields prevent unbounded buffering.

Inspect only selected image fields, not arbitrary labels/commands/environment
values. Require exact local ID/OS/architecture; reject nonzero application Env,
Volumes, Healthcheck and OnBuild counts. Never run an image's own healthcheck.
Override entrypoint, command, user, working directory and stop signal explicitly.
Image EXPOSE metadata never authorizes publishing; no host port is allowed.
Content identity does not certify the image lacks embedded credentials or bugs.
Never remove or retag the owner's image during cleanup.

Generate Compose from a closed typed structure, not text substitution or a user
YAML fragment. Use only the generated file in a new private project directory,
an explicit unique project name, empty env file and controlled subprocess
environment. Do not inherit `COMPOSE_FILE`, profiles, env-file/override controls,
target/proxy/OTel settings or Docker endpoint variables after endpoint selection.
Use trusted absolute CLI/plugin paths; no shell, `eval` or command-string assembly.
Validate normalized **generated** configuration before create; never normalize
an untrusted Compose file first. Bound CLI calls/output and process shutdown;
killing the Docker CLI alone does not stop its containers.

Closed profile allowlist:

- Four steady-state roles and one temporary trusted probe; only the four networks
  in the threat model, all new/internal/isolated, IPv6 off and restricted DNS.
- Pinned local image IDs, never-pull/no-build/no-recreate creation; all containers
  created stopped, inspected and recorded before any application process starts.
  A preexisting project/name/nonce/resource collision is refusal, not adoption.
- Read-only roots, non-root, cap-drop ALL, no-new-privileges and qualified default
  seccomp/LSM. App: 128 MiB, 0.5 CPU, 64 PIDs, nofile 256, optional `/tmp` tmpfs
  ≤32 MiB with nodev/nosuid/noexec. Runner/fixture/proxy/probe: ≤64 MiB/0.5 CPU/
  64 PIDs each. Even a five-container overlap has ≤384 MiB configured memory;
  this is not total VM/daemon memory accounting.
- No restart and no application Docker logs; no automatic raw logs/inspect dumps.
  Only bounded, classified/redacted supervisor diagnostics. Headers/body sizes
  bounded (64 KiB body; proposed custom transport response-header cap 8 KiB),
  no redirects, ambient HTTP proxies, cookies or supplied request payloads.

Everything else is denied, including ports, publish-all, binds/volumes, secrets,
configs, devices/GPUs, external networks, network/namespace sharing, privileged,
cap-add, extra-hosts/host-gateway, custom DNS/search, user labels/hooks/commands,
build/pull, includes/extends/overrides, environment maps and daemon settings.
The named tmpfs is explicit and contains no host data. Docker-created runtime
metadata mounts are not a promise of zero host implementation machinery.
Only the host controller can invoke Docker; no lab container receives its socket.

## Exact ownership and cleanup state machine

1. **Validate / authorize:** no Docker mutations. Acquire the private slot and
   refuse unresolved earlier intent. Recheck exact plan and binding. IDs contain
   128 random bits; separate launch ID, proxy run ID and owner nonce. No cross-run
   resources or images are created/adopted from names alone.
2. **Prepared:** persist/sync the private intent with expected graph, IDs, plan hash
   and runtime binding before any create. Record each operation's intent before
   issuing it and its observed immutable resource IDs afterward. Write failure
   prevents further provisioning/workload mutations; it must not prevent cleanup
   of already proven-owned resources. No transaction spans workload measurement.
3. **Create stopped / inspect:** create only the empty fresh project with no
   recreate/pull/build. Refuse unexpected existing names before this step; verify
   IDs, nonce, role, images, resource limits, mounts and exact network membership
   before start. After an ambiguous response, reconcile only this journal's exact
   nonce/role/template candidates and config; unknown resources are not deletion
   authority. Never reconcile an existing user project.
4. **Trusted startup / preflight:** start proxy and fixture only. Verify control
   binds to its control address. A trusted probe on the future app attachments
   must fail to reach admin via control literal IP/data alias or dependency via
   backend IP; a control-side trusted check must succeed. Remove probe, confirm
   absence, then start the selected app. Failure goes directly to owned teardown.
5. **Worker:** start only the recorded runner. Supply bounded canonical settings,
   adapter and preassigned IDs via stdin, not a bind mount; no endpoint strings
   from the descriptor. Require a bounded versioned worker protocol, no raw app
   output. Register fresh-context proxy deletion before its create POST. Fixed
   control/upstream/listener; no global reset, listing/deleting by prefix or reuse.
6. **Readiness / measurements:** at most 20 full-path readiness calls within five
   seconds; exclude them from samples. Preserve serial baseline/fault/recovery,
   refusal after failed baseline, real downstream latency and actual HTTP outcomes.
   The 120 s engine deadline can yield partial data even for valid large sample/
   timeout settings. Outer startup/work budget is 180 s; no build/pull outside it.
7. **Proxy cleanup:** remove toxic before recovery; always delete the exact owned
   proxy under a fresh five-second context, including lost create responses.
   Cancellation, lost worker IPC and engine errors cannot suppress host teardown.
   Worker returns actual partial data and classified errors if available. The host
   strictly validates bounded worker JSON, matching launch/run/config identities
   and sample-derived summaries before accepting it; malformed/mismatched output
   is an error that still triggers teardown. Never accept a worker's success bit
   as confirmation that host cleanup occurred.
8. **Host teardown:** fresh 30 s budget, independent of work context; include up
   to five seconds for cooperative worker cleanup, then stop/kill/remove only
   verified recorded containers (parallel graceful stop ≤12 s), then remove exact
   empty recorded network IDs. Recheck binding/ownership first. Do not use broad
   `compose down --remove-orphans`, volume/image deletion, prune or global reset.
   Verify resource absence. Cleanup must proceed even if additional journal writes
   fail, using the last proven intent/IDs and surfacing the write error. If the
   daemon is unreachable, record unknown; elapsed timeout is not confirmation of
   cleanup.
9. **Finalize:** only after teardown attempts, atomically publish a separate custom
   result with measurement and both cleanup outcomes. Earlier cleanup errors stay
   errors even if removing the proxy container finally confirms the fault absent.
   No successful execution verdict can hide a failed/unconfirmed cleanup layer.
   Keep the private journal/receipt intentionally; remove only verified temporary
   generated files. Failure publishing a result is an error, never a new run or
   permission to overwrite an existing result. Release slot only after confirmed
   cleanup; unresolved intent blocks subsequent runs.

`run` retains a no-replace `result.json` in its own private run directory and
writes the validated result to stdout after cleanup attempts; diagnostics go to
stderr. Proposed lab exits are 0 only for completed measurements, successful
cleanup and result publication; 1 for precondition/execution/cancellation/I/O
errors; 2 for usage. A failed stdout write remains an error even if the private
result was saved. No use of policy-verdict exits 3/4 and no change to old commands.

All operations have bounded contexts and subprocess output; the 180+30 s budget is
a cooperative deadline, not a guarantee against kernel stalls, host suspension or
dead daemons. If the worker dies, a living host supervisor tears down its IDs. If
both die, Toxiproxy has no automatic lease expiry. No unconditional fault-duration
or crash-cleanup claim is possible. Keep the journal and report this limitation.

`lab cleanup` is an explicit cleanup-only recovery operation: strictly validate
the journal and plan, require matching runtime binding and resource fingerprints,
then delete only its recorded IDs. Missing/corrupt state or a different daemon is
refusal. Do not auto-resume traffic, infer why an intent stopped, relabel an old
report as complete or search/delete all `faultline-*` resources. A separate
cleanup receipt may confirm eventual absence without changing recorded samples.

## Result, provenance, tracing and legacy compatibility

The proposed ephemeral result has independent namespace
`kind: "faultline-custom-lab-result", schema_version: 1`, not a flattened
`experiment.Report`. It contains a launch/configuration snapshot, actual
measurement phases/raw samples/summaries, execution status and distinct cleanup
states. Reuse `experiment.Summarize` and shared sample invariants; do not invent
statistics or marshal a custom outcome as a schema-1 sample report.

Cleanup records proxy and whole-lab states (`complete`, `failed`, `unknown`,
`not_created` where actually established), final fault absence as a nullable fact,
and bounded classified errors. Missing worker output means unavailable measurements,
not fabricated zero samples. A normal worker may actually record a zero-sample
phase after cancellation; keep that distinction. Completion requires full measured
phases, successful baseline/recovery and both cleanup layers without hidden errors.
No finalized result is synthesized after a supervisor crash from an intent alone.

Record allowlisted facts: configured adapter/template and hashes, descriptor
measurement hash, all observed component image IDs, selected Linux platform,
Faultline/runner revision where known, observed worker Go version/OS/architecture,
Docker/Compose versions, measurement methodology and adapter workload identity,
and tracing explicitly off. Distinguish configured timeout from observed
behavior and selected platform from observed container metadata. No private tags,
registry URLs, paths, usernames, IPs, host names, env values, payloads or raw errors.
Runtime binding/resource IDs remain private recovery state. Hashes are consistency
metadata, not signatures, trusted builds or proof of independent execution.

M8B rejects custom `--tracing`, Jaeger and `--view` requests except explicit off.
It cannot sanitize arbitrary image instrumentation, and no collector is provided.
Future tracing must define application-owned instrumentation, allowed attributes,
collector reachability, bounded export and profile-specific graph completeness.
Never rename spans into the four sample roles or infer missing spans. Existing
sample tracing, W3C propagation and post-cleanup export remain unchanged; traces
are still temporary and Toxiproxy has none.

Compatibility boundaries:

| Path | M8B MVP decision |
| --- | --- |
| Existing `run`, flags and schema-1 spec/report | Unchanged; sample only. No optional `target` or target inference. |
| SQLite/history/dashboard | No custom insertion or UI; no schema migration. Keep intents, immutable reports and current cleanup meaning unchanged. Custom supervisor uses private journal, not fake history IDs. |
| Format-1 `.flb` / released binaries | Unchanged bytes/semantics/fixtures. No custom bundle export or downgrade. Existing commands reject the new non-bundle envelope. |
| Compare and evaluate | Existing input/output schemas, policy semantics and 0/1/2/3/4 exit meanings unchanged. Custom evaluation is unsupported, not a policy PASS/FAIL/INDETERMINATE manufactured from a schema-1 projection. |
| Future custom evidence (separate follow-up review) | Bind adapter/image/workload identity and outer cleanup into strict versioned records; dispatch explicitly and prove old-reader rejection. Review bundle envelope/history/API migrations then. Reuse existing statistics and policy thresholds only after validated custom evidence and result identity/cleanup semantics exist. |

Do not exploit schema-1's preserved optional fields to bypass a version decision.
An old comparison ignores custom identity fields and could overstate matching
conditions; provenance schema 1 also rejects new components. Supported producers
must not erase these facts to obtain acceptance. An attacker can already rewrite
evidence/hashes together; none of this provides cryptographic authenticity.

## Proposed module changes (M8B, not this PR)

| Boundary | Small change to consider |
| --- | --- |
| Native `cmd/faultline` | New explicit `lab plan/run/cleanup` adapter. No dashboard execution, no changes to old command defaults. |
| New host-only `internal/lab` | Strict descriptor, closed adapter catalog/plan, native supervisor, private journal/recovery and bounded Docker CLI adapter. No Docker SDK or orchestration/plugin framework. |
| Small shared closed profile definition | Fixed routes, control alias, upstream and worker protocol; only built-in IDs, no public constructor accepting arbitrary target strings. Share between host planner and trusted worker without importing host Docker operations into engine. |
| `internal/experiment` | Reuse lifecycle/measurements through a small internal measurement-outcome boundary. Existing public sample Run/report path stays unchanged; custom worker never emits a legacy sample report. Preserve deadlines and failure classification. |
| `internal/toxiproxy`, `internal/localhttp` | Closed profile-specific control endpoint and ownership checks; custom-only readiness/header caps. No arbitrary endpoint options, global reset or weaker legacy behavior. |
| New custom result validator | Own namespace/cleanup semantics; reuse shared sample validation/summarizer, not a copied statistics implementation. No imports into offline bundle code until separate evidence review. |

## M8B increments and acceptance gates

1. **Contracts/planner:** strict descriptor and regular-file tests, closed catalog,
   canonical hashes, single-use authorization/runtime binding and pure resource
   plans. Plan-only CLI with zero Docker mutations. Review error/redaction UX.
2. **Qualified supervisor:** journal before mutation; fixed generated topology,
   no-recreate/stopped inspection, control bind and DNS/isolation gates, bounded
   subprocesses and exact-ID teardown/recovery. Run the network/adversarial matrix
   with trusted probes/sentinels before permitting a selected application image.
3. **One real independent app:** implement the closed worker/measurement boundary,
   build the independent contract app separately, then prove real 80/400 ms runs,
   actual HTTP timeouts/recovery, cancellation and honest custom result/cleanup.
   No mocked transport substitutes for real Toxiproxy or app tests.
4. **Review and regressions:** complete failure/crash/ownership cases, legacy
   format/SQL/offline/gate/trace contracts and both sample integration suites;
   inspect no resource leaks or data changes. Independent security review of
   actual implementation and runtime results precedes custom-command enablement.

Custom SQLite history, portable evidence/gates, tracing and additional adapters
are a separate later decision, not automatic M8B scope or release authorization.

## Required M8B acceptance matrix

This matrix originated in M8A. M8B1 contract/read-only results are recorded in
[validation](../validation.md#milestone-8b1--strict-contracts-and-read-only-planning-unreleased).
All execution, network qualification and custom recovery rows remain future work.

| Area | Positive, negative and adversarial cases | Required observable result |
| --- | --- | --- |
| Descriptor/files | Correct local image/adapter; unknown/duplicate/escaped/case-fold fields; Unicode/null/version/type/overflow errors; exact/oversize boundary; symlink/FIFO/device/path replacement; cancellation | Deterministic canonical config; invalid input fails before Docker mutation; no remote/file includes or leaks. |
| Authorization/selection | Correct single-use hash; wrong/replayed/changed plan; image missing/retagged; socket/daemon switch; TCP/SSH context overrides Unix host and converse | Only exact unchanged authorized local plan runs; test actual selected endpoint precedence, not just an environment string. |
| Image/config | Independent compliant image; env/volume/healthcheck/OnBuild defaults; wrong arch, executable, unknown Compose controls; injected `COMPOSE_FILE`, `.env`, profile, namespace, host-gateway, build/pull/hook | No app process starts for rejected cases; no inherited files/credentials/hooks, implicit volumes, pulls or elevated settings. |
| Network | Positive runner→app→proxy→fixture and runner→control; app probes control literal IP/data alias, backend bypass, owned other-network and host sentinels, DNS sink, IPv4/IPv6 literals; wildcard/multiple control binds | Allowed paths work; every prohibited path fails. Use only created test endpoints, never scan/probe user services or public URLs. Unsupported profiles refuse without fallback. |
| Actual measurements | Independent app with 80 ms and 400 ms latency, baseline/recovery, failed baseline, no proxy bypass and known fixture counter | Genuine raw outcomes and measured distributions, actual 504 in the contract test app, successful cleanup. No promises about arbitrary apps or exact timing values. |
| Hostile behavior | Redirect, oversized/slow body/headers, endless stream, app exits/OOMs/forks/log floods, ignores timeout/dependency, retries/background traffic | Bounded capture/deadlines/resources, no payload/log recording, conformance failure or failed run; no synthetic success or inferred dependency behavior. |
| Failure/cleanup | Lost Docker/proxy create response, add/remove/delete error, cancellation at every stage, killed worker/CLI pipe, malformed/mismatched worker identities/statistics, output/disk failure, hung child process, daemon disappears during teardown | Cleanup under fresh deadlines, even when parsing or journal writes fail; exact journal IDs only; classified errors and incomplete/unknown states retained, no false completion. CLI termination alone is not assumed to terminate containers. |
| Crash/recovery | Supervisor SIGKILL with active toxic, daemon restart/same binding versus replacement, host sleep, corrupt/missing journal, reused name/foreign nonce | No guaranteed automatic cleanup claim. Explicit scoped recovery only when identity is proven; preserve unknown intent, original data and all other owners. |
| Ownership/concurrency | Two custom launches, active sample lab, same-prefix foreign sentinel, injected name collision/unknown resource, full journal, reused plan | Second custom launch fails busy; no cross-run reuse/adoption/deletion; compare sentinel resource identities before/after. |
| Compatibility/privacy | All existing fixtures, legacy/new missing provenance, canceled/partial custom envelope offered to v1 history/bundle/evaluate, secret canaries in app response/logs/labels | Legacy bytes/outputs/exit codes unchanged; custom paths refuse until explicitly supported; canaries absent from results/public errors. |
| Final qualification | Format/vet/full race and shell checks, both sample fault/tracing suites, real custom integration, owned-resource inventory, approved runtime/platform evidence | Actual passed/unrun/failed record, zero owned leaks, no original data/release mutation; cross-compilation is not runtime qualification. |

M8A validation is documentation-only: source/ADR/test inspection, official primary
documentation review, link/schema-example/scope checks and protected-file/release
comparisons. No new app, topology, network isolation, cleanup, Go test, Docker or
browser execution is claimed by this design. No qualification cell above is
checked off merely because existing sample or offline tests passed earlier.

## M8B2A implementation boundary

The internal [supervisor foundation](custom-service-supervisor.md) implements the
closed typed graph, single-use consumption contracts, replay-validated event model
and private immutable journal snapshots. It deliberately chooses append-only
no-replace updates rather than mutable journal rename replacement: committed
history remains inspectable, and interrupted/corrupt revisions fail closed.
This is not a new plan format or a custom result/evidence schema.

Ambiguous creation with no accepted ID remains unresolved in this increment;
matching names/labels cannot adopt an ID. Cleanup candidates must already have a
matching intent and accepted identity, plus a fresh exact-ID proof. A future
trusted observation driver must derive actual configuration rather than trust a
label hash. There is no driver, Docker mutation implementation or production
RUN/CLEANUP command. A fixed authoritative ledger/runtime lease and real isolation
qualification remain prerequisites before provisioning. The
[bridge follow-up](../security/docker-bridge-observation.md) records idle stop/start
windows but leaves exact network-event and wake-caller attribution UNKNOWN.

## M8B2B trusted implementation boundary

[ADR 0010](../decisions/0010-authoritative-supervisor-state.md) and the
[trusted contract](trusted-supervisor-boundaries.md) implement fixed authority,
cooperative leases, exact recorded-ID inspection and reserved cleanup recording.
The runtime key is private daemon-derived state under the OS account's fixed root;
there is no state-directory override or automatic replacement of missing history.
Process exit releases a lock, never a reservation. Legacy import is a consumed
identity tombstone, not permission to adopt or remove resources.

The mutation interface remains opaque/non-executable with recording fakes only.
Actual response certification/absence proofs and resource provisioning are deferred;
no attempted-resource reservation may be released based on journal assertions alone.
No runtime isolation, app conformance or source/Docker execution gate is passed by
these contracts. The preserved bridge findings still have unknown exact attribution.
