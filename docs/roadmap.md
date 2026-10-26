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
- [ ] Review publication-neutral usage, third-party notices, signing warnings and release docs
- [ ] Rebuild/reverify that approved documentation merge and decide unsigned asset policy
- [ ] Explicit owner authorization before any first tag, Release or asset upload

No tag, Release, CI or asset publication yet. Further release work should qualify
independent builders/OS versions and native Linux/Intel local packages, decide
artifact authenticity/distribution policy, and collect external clean-install
feedback. Windows remains unsupported until filesystem/signal/runtime checks exist.
