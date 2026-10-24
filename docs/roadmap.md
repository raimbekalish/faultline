# Roadmap

## 0 — Repository and engineering foundation

- [x] Public standalone repository, MIT license and verified commit identity
- [x] Architecture, contributor instructions and first design decision
- [ ] Executable Go foundation with bounded transport and tests

## 1 — Reproducible local experiment

- [ ] Isolated frontend → Toxiproxy → dependency sample
- [ ] Baseline, injected latency and recovery measurements
- [ ] CLI JSON report with raw samples and comparisons
- [ ] Cancellation, timeouts, ownership and deterministic cleanup
- [ ] Real Toxiproxy integration tests including dependency timeouts

## 2 — Explain failures with traces

Instrument the sample with OpenTelemetry. Correlate experiment/run IDs with
traces, inspect dependency spans, and add an optional local Jaeger profile.
Keep collector overhead measurable and optional.

## 3 — Reproducible experiment definitions and history

Versioned experiment specifications, SQLite migrations, report replay/export,
and explicit allowlists for user-owned local Compose applications. Design crash
recovery and exclusive ownership before supporting shared infrastructure.

## 4 — Dashboard and broader reliability scenarios

A React/TypeScript UI over a stable local API, additional fault types, controlled
concurrent traffic, repeated comparisons and documented benchmark methodology.
Choose each feature from measured user needs. CI requires owner approval.
