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

## 4 — Dashboard and broader reliability scenarios

A React/TypeScript UI over a stable local API, additional fault types, controlled
concurrent traffic, repeated comparisons and documented benchmark methodology.
Choose each feature from measured user needs. CI requires owner approval.
