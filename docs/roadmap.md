# Roadmap

## 0 — Repository and engineering foundation

- [x] Public standalone repository, MIT license and verified commit identity
- [x] Architecture, contributor instructions and first design decision
- [x] Go foundation with bounded transport, measurement and race-tested units

## 1 — Reproducible local experiment

- [x] Isolated frontend → Toxiproxy → dependency sample
- [x] Baseline, injected latency and recovery measurements
- [x] CLI JSON report with raw samples and comparisons
- [x] Cancellation, timeouts, ownership and deterministic cooperative cleanup
- [x] Real Toxiproxy integration tests including dependency timeouts

## 2 — Explain failures with traces

- [x] Official OpenTelemetry SDK; W3C propagation and meaningful HTTP spans
- [x] Observation trace/span IDs plus validated run/phase correlation
- [x] Optional bounded, memory-only Jaeger with internal ingestion
- [x] Real export/retrieval and parent/status tests for latency, timeout and recovery
- [x] Disabled experiments and cancellation/cleanup/export-failure regressions
- [x] Record repeated enabled/disabled overhead measurements and loopback UI verification

## 3 — Reproducible experiment definitions and history

- [x] Strict schema-1 JSON specifications and deterministic snapshots/hashes
- [x] SQLite migrations, durable intents and immutable final reports
- [x] CLI run/list/show/compare with original report compatibility
- [x] Failed/canceled persistence, rollback, lock and process-exit recovery tests
- [x] Repeated real experiments and tracing-enabled/disabled regression suites
- [x] Actual statically linked CLI, host history bind and SIGTERM verification
- [x] Raw-sample validation of stored summaries/comparisons and corruption regressions

Per-request checkpoints, backup/export/retention and automatic crash-state
resolution remain future work. User-owned application targets require a separate
reviewed allowlist/ownership design; this milestone keeps only the fixed sample.

## 4A — Local reliability dashboard

- [x] Versioned, protected loopback API using validated SQLite history
- [x] Snapshot cursor pagination, filters/search and recorded-status counts
- [x] React overview/history/detail/comparison and raw-observation inspection
- [x] Explicit partial/unfinished/missing-telemetry states and zero-based charts
- [x] Component, API, production-build and actual-data Chrome verification
- [x] Existing fault/tracing, persistence and recovery regressions

The dashboard remains read-only. Milestone 5A adds portable evidence/provenance.
Browser execution requires a separate reviewed ownership/cancellation/security
design.

## Later — Broader reliability scenarios

Additional fault types, controlled concurrent traffic, repeated comparisons and
documented benchmark methodology.
Choose each feature from measured user needs. CI requires owner approval.

## 5A — Portable evidence and environment provenance

- [x] Versioned bounded archives, canonical specs/hashes and original report bytes
- [x] Shared semantic validation and a CGO-disabled offline verifier/inspector
- [x] Atomic no-replace export of complete/failed/canceled final reports
- [x] Allowlisted execution facts and observed Docker/image metadata when supplied
- [x] Read-only dashboard downloads and explicit missing-provenance information
- [x] Actual new/legacy/canceled exports, deterministic repeats and offline checks

Offline comparison is implemented in Milestone 5B. Cross-platform release
packaging and a broader archive-compatibility matrix remain separate work.

## 5B — Offline evidence comparison

- [x] Independently validated, database-free CGO-disabled comparison command
- [x] Shared delta arithmetic and explicit ms/percentage-point/relative units
- [x] Literal configuration/provenance differences with unknowns preserved
- [x] Full/partial/missing measurements and conservative comparability assessment
- [x] Real exported records, reverse/identity/legacy/canceled and bounded-memory tests
- [x] Existing fault/tracing, history, archive and frontend regressions

## 6A — Release readiness

- [x] Local candidate archives with explicit component/dependency boundaries
- [x] Four offline cross-builds and native/VM/emulated runtime qualification
- [x] Native macOS ARM64 CLI/dashboard/assets clean-install validation
- [x] Stable synthetic evidence, offline, history and API compatibility fixtures
- [x] Targeted ZIP/JSON adversarial review, confirmed fixes and bounded fuzzing
- [x] External quick start, package usage, platform matrix and security limitations
- [x] Owner review/merge of PR #6; fresh 0.1.0 candidates from the approved merge
- [x] Requalification with real evidence, cross-target runtimes and macOS policy checks
- [x] Review publication-neutral usage, third-party notices, signing warnings and release docs
- [x] Rebuild/reverify that approved documentation merge and decide unsigned asset policy
- [x] Explicit owner authorization before the first tag, Release and asset upload

v0.1.0 is published with four offline packages and SHA256SUMS after owner approval;
the native package remains withheld. No GitHub Actions were enabled. Further
release work should qualify independent builders/OS versions and native
Linux/Intel local packages, decide artifact authenticity/distribution policy, and collect external clean-install
feedback. Windows remains unsupported until filesystem/signal/runtime checks exist.

## 7 — Offline reliability gates (Unreleased)

- [x] Strict bounded schema-1 policies with explicit lifecycle requirements
- [x] Pure deterministic evaluation using shared evidence/statistics validators
- [x] Inclusive rate/mean/p95 thresholds, sample sufficiency and null missing data
- [x] Versioned per-condition results and distinct PASS/FAIL/INDETERMINATE exits
- [x] Real retained 80 ms, 400 ms and SIGTERM-canceled evidence replay
- [x] Full regression, isolated offline runtime and cross-target determinism checks
- [x] Owner code review and merge (PR #9; remains Unreleased)

This source feature is not included in v0.1.0 binaries or assets. It changes no
existing format-1 bundle, report, experiment spec, SQLite schema or comparison
semantics. Broader policy languages, dashboard execution and statistical/SLO
claims remain outside this milestone.

## 8A — Safe custom service architecture (design review)

- [x] Inspect fixed-topology assumptions and current validation/cleanup boundaries
- [x] Compare selected-image, reviewed-adapter and user-owned Compose approaches
- [x] Propose one closed HTTP adapter, explicit plan authorization and isolated lab
- [x] Document threat model, layered cleanup, residual risks and adversarial matrix
- [x] Define legacy compatibility and staged M8B implementation gates
- [x] Independent architecture/security review and owner approval (PR #10)

See [ADR 0009](decisions/0009-owner-selected-service-labs.md),
[design](design/custom-service-lab.md) and
[threat model](security/custom-service-threat-model.md). This milestone is
documentation only: no custom execution, runtime qualification, accepted format
change or expansion of today's sample allowlist. v0.1.0 remains unchanged.

## 8B — One owner-selected service lab (staged, Unreleased)

- [x] M8B1: strict descriptor, closed catalog and read-only plan-only CLI
- [x] M8B1: private atomic plans, redaction, authorization lifecycle and negative tests
- [x] M8B1 owner implementation review/merge (PR #11)
- [x] M8B2A: deterministic closed graph, pure ownership/state transitions and private journal snapshots
- [x] M8B2A: synthetic failure/replay/cleanup and real filesystem interruption tests
- [x] Bounded read-only bridge investigation; idle lifecycle evidence, exact attribution remains UNKNOWN
- [x] M8B2A independent implementation/security review and merge (PR #12)
- [x] M8B2B fixed authority, durable reservations and cooperative runtime leases
- [x] M8B2B recorded-ID observation, cleanup reserves and opaque mutation planning
- [x] M8B2B synthetic/filesystem/process-exit and actual read-only diagnostics
- [x] M8B2B independent review/merge (PR #13)
- [x] M8B2C closed command preparation, private attempts and synthetic result/absence certification
- [x] M8B2C dedicated source-only preflight worker/fixture and independent contract app
- [x] M8B2C independent code/security review and merge (PR #14)
- [ ] Qualified continuity provider, actual mutation driver and restart reconciliation review
- [x] M8B3A private measurement engine, local HTTP integration and ephemeral result validation
- [x] M8B3A preflight compatibility, DNS source review and qualification test plan
- [ ] M8B3A independent implementation/security review and merge
- [ ] Production worker invocation/result binding and real fault-control adapter review
- [ ] Authorized catalog builds/qualification
- [ ] Qualified local runtime isolation and actual scoped teardown/failure tests
- [ ] One independently built contract app with real latency/timeout/recovery runs
- [ ] Separate ephemeral custom result and legacy regressions/security review

Begin only after the M8A design is independently approved. No selected app starts
before isolation/control/DNS/ownership qualification. No user Compose ingestion,
existing project mutation, arbitrary URLs, host mounts or daemon access inside
containers. Custom SQLite history, `.flb`/gate support, tracing and additional
adapters require separate reviewed follow-ups; they are not implicit M8B scope.

M8B2A is an internal non-mutating foundation, not runtime qualification or an
execution feature. See the [contract](design/custom-service-supervisor.md) and
[bridge observations](security/docker-bridge-observation.md). Do not begin
provisioning or build the missing catalog image as a side effect of planning.

M8B2B remains non-mutating and unqualified. Its fixed state initialization is
explicit and not connected to the plan-only CLI. Legacy imports retain tombstones
without adopting resources. Real runtime qualification/provisioning requires a
separate owner authorization and reviewed implementation; it is not automatic.

M8B2C remains disconnected from production execution. No image was built/pulled or
Docker resource mutated. The missing continuity provider deliberately blocks real
activation/certification; daemon metadata equality is insufficient. The worker
source validates preflight only, with measurements null. See the
[transport contract](design/docker-transport-certification.md) for prerequisites
and residual TOCTOU/recovery limitations. This work is Unreleased.

M8B3A records actual local HTTP timings with handler-delay/fake fault control; it
is not a real network fault experiment. The existing worker entrypoint remains
preflight-only. New ephemeral results are not accepted by sample history, bundles
or gates. [DNS analysis](security/custom-lab-dns-qualification.md) preserves the
prepared profile and marks all runtime probes UNVERIFIED. No runtime resources
were started, no image built, and no new released feature is advertised.
