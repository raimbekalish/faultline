# ADR 0002: Explicit, optional OpenTelemetry tracing

- Status: accepted for milestone 2 implementation
- Date: 2026-10-24

Use the official OpenTelemetry Go SDK and OTLP/HTTP exporter. Keep providers
explicit rather than process-global so disabled execution remains a nil fast
path and tests cannot accidentally replace shared global tracing state.

Instrument the known HTTP routes and outbound requests explicitly, including
response-body reads. This preserves the existing client's deadlines and limits
and avoids default capture of raw URLs, headers or request data. Propagate W3C
trace context and only validated run/phase/observation baggage. Toxiproxy does
not emit spans; network delay appears within its caller's dependency client span.

Use one trace per observation, with runner client → frontend server → frontend
dependency client → dependency server. Metadata identifies the run and phase.
Record optional trace/span IDs in schema-1 samples; existing fields keep their
meaning. A missing span is unknown telemetry, not proof a service was not called.

Classify completeness by the fixed sample graph, not its span count alone:
unique valid IDs, one span for each expected service/kind/operation, one runner
root, and the three exact same-trace parent edges. Reject ambiguous references
and never deduplicate or join separate snapshots to manufacture completeness.
This validates structure; observation correlation is checked separately.

The bounded nonblocking SDK queue holds at most 256 spans per process, batches
32, and exports asynchronously with finite timeouts and no retries. Trace data
may be dropped under pressure. SDK export failures must not affect fault cleanup.
Flush/shutdown occurs after the engine returns and uses a separate two-second
budget. Telemetry statistics describe only the current process.

Jaeger is an opt-in Compose overlay, pinned by digest, with memory-only bounded
storage and no default published ports. An additional explicit UI option may
publish only the query UI to a random loopback port for five minutes. Ingestion,
health and administrative endpoints remain internal. No persistent storage,
external collector, authentication or extra production telemetry stack is added.

Verify actual OTLP ingestion, retrieval and parent relationships against Jaeger;
also run the existing suite without it. Measure enabled/disabled overhead on the
same real topology and report distribution variability instead of assuming zero
overhead. Keep span durations distinct from complete HTTP measurements.
