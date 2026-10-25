# Local distributed tracing

Tracing is optional. Ordinary `./scripts/experiment.sh` and
`./scripts/experiment.sh test` start no collector for experiments. The integration
suite deliberately enables one runner exporter against the absent collector to
verify export-failure cleanup. The official OpenTelemetry
Go SDK and OTLP/HTTP exporter are pinned to 1.47.0. Jaeger is pinned to 2.22.0 by
manifest digest. No Toxiproxy spans are claimed or generated.

## Reproduce and inspect

```sh
mkdir -p artifacts
# A single experiment, JSON report with trace/span IDs:
./scripts/experiment.sh --tracing -samples 5 -latency 80ms > artifacts/traced.json
# Both latency cases, reports plus 30 retrieved traces and raw Jaeger responses:
./scripts/experiment.sh --tracing evidence > artifacts/evidence.json
# Full real integration suite, including cross-service parent/metadata checks:
./scripts/experiment.sh --tracing test
```

For interactive inspection, explicitly request the loopback query/UI port:

```sh
./scripts/experiment.sh --tracing --view evidence > artifacts/evidence.json
```

After the experiments, stderr prints `Jaeger UI: http://127.0.0.1:<random-port>`.
Open that exact URL, select `faultline-runner`, and find traces; or append
`/trace/<trace_id>` using an observation's ID. Run and phase appear as span tags
`faultline.run_id` and `faultline.phase`. The span also records zero-based
`faultline.observation`. All three are propagated as validated W3C baggage;
`traceparent`/`tracestate` carry W3C trace context.

The viewer keeps the stack available for five minutes, then removes it. Ctrl-C
cleans up early. The UI and its query API are the only published endpoint, bound
explicitly to loopback with a random port to avoid collisions. This opt-in exposure
lets the local browser inspect actual traces; it is not a public listener.
Ingestion on 4318 remains unpublished on Docker networks. No admin/metrics,
pprof, remote-sampling, MCP, or AI endpoints are configured. Memory storage holds
at most 2,000 traces and vanishes on teardown. Jaeger gets 192 MiB and 0.5 CPU;
the collector memory limiter is 128 MiB with a 32 MiB spike allowance.
Docker does not publish ports for an internal-only network, so `--view` attaches
only Jaeger to a second disposable bridge. Samples and the runner remain on the
internal network. The collector gains bridge egress in this explicit UI mode;
no ingestion port is published, and it has no configured external exporter.

Without `--view`, all tracing endpoints remain internal and the stack is removed
immediately after the command. Generated evidence is local and ignored by Git.
If forcibly interrupted, use the README's exact-project teardown with all three
Compose files. This selects the optional Jaeger service and both the internal and
viewer networks; the base file alone cannot select all tracing resources. Do not
target a shared or unrelated project.

## Reading a trace

The expected chain for each measured observation is:

```text
faultline-runner:     CLIENT GET /work       ← observation trace_id/span_id
  faultline-frontend: SERVER GET /work
    faultline-frontend: CLIENT GET /value    ← includes the delayed response
      faultline-dependency: SERVER GET /value
```

The dependency client talks through Toxiproxy. Toxiproxy is transparent to HTTP
headers and emits no spans. Its downstream toxic delays responses; it does not
add 80/400 ms of application processing to the dependency server span.

The evidence `complete` flag validates this exact topology in one retrieved
snapshot: four unique nonzero canonical span IDs, the four service/kind/operation
roles shown above, one runner root, and exactly one same-trace `CHILD_OF` edge
per child. Extra, duplicate, malformed, foreign, or incorrectly connected spans
remain incomplete. Span arrival order does not matter. Separate snapshots are
never combined, and an HTTP timeout can still have a structurally complete trace.
The flag does not establish experiment metadata correlation or a causal diagnosis
by itself; integration tests check those fields against actual observations.

Actual integration evidence from the initial successful milestone-2 run:

| Scenario | Observation | Dependency client span | Dependency server span | Outcome |
| --- | ---: | ---: | ---: | --- |
| Healthy baseline | 0.42 ms | 0.18 ms | <0.01 ms | HTTP 200 |
| 80 ms downstream latency | 81.28 ms | 80.95 ms | <0.01 ms | HTTP 200 |
| 400 ms downstream latency | 251.19 ms | 250.46 ms | <0.01 ms | HTTP 504; client `error.type=timeout` |
| Recovery after 400 ms toxic removed | 1.08 ms | 0.69 ms | <0.01 ms | HTTP 200 |

For the timeout case, the frontend server and runner client spans carry HTTP 504
and error status. The dependency client has an error event/status and no received
HTTP status. The dependency server completed successfully: the retrieved span
proves it executed, even though the caller never received its delayed response.
This diagnosis applies to the checked-in downstream latency toxic, not every
network failure. No missing spans were needed to explain these observations.

Root example IDs in that run:

- Baseline: `aa531b8acbe6d84a9c41b3fa649e64bf`
- 80 ms fault: `094d6b1391059d98bb796bed9ce91799`
- 400 ms fault: `c05f573f28345edacb953b4ed8f535c0`
- Recovery: `866a45a935e2a554df6c5714cd4ced5d`

These are historical validation IDs, not live hosted links; reruns generate new
IDs. The evidence command saves the actual raw query responses before teardown.
The query adapter reads Jaeger's internal JSON API, which is not a stable public
contract. Jaeger translates current HTTP status attributes to `http.status_code`
in this response; the adapter tests support that representation and the current
attribute name.

## Measurement and loss

`duration_ms` measures the complete runner HTTP operation, including tracing work
inside that operation and reading the bounded body. Each span measures its own
start/end interval. Do not sum nested spans or equate a dependency span with the
whole observation. The runner client span is close to, but not identical to,
`duration_ms`: setup and final bookkeeping have different boundaries. Telemetry
shutdown and Jaeger retrieval occur outside experiment measurements.

The SDK uses a nonblocking queue of 256 spans per process, batches of 32, 100 ms
batch delay, 500 ms export budget, and a 300 ms HTTP exporter/client timeout with
retries disabled. Span attributes/events/links have explicit limits. Export may
drop data; this small test topology samples 100% of owned request traces rather
than promising lossless tracing. No URL query strings, credentials, headers,
bodies or raw request errors become span attributes. Only the three validated
correlation baggage members are forwarded.

Export/shutdown uses a fresh bounded context **after** engine proxy cleanup.
Failed export is recorded separately and does not change fault cleanup semantics.
A trace ID in a report does not prove delivery. Missing/partial traces remain
explicit in evidence; timeout, sampling, drops, export failure or early termination
are possibilities, not a diagnosis without evidence. The cancellation path does
not promise a complete trace. Service shutdown tries to flush after HTTP handlers
finish, but SIGKILL/daemon failure can lose telemetry and require manual teardown.

## Measure overhead

Run this alone, without other Faultline experiments, to avoid deliberate load
contention. Python 3 is required only for this optional validation helper:

```sh
python3 scripts/measure-tracing-overhead.py > artifacts/overhead.json
```

It runs two alternating off/on pairs on the same topology, 50 requests per phase,
with an 80 ms fault, and retains every report/sample plus paired mean deltas.
Each run gets a disposable stack; build/startup time is outside request timings.
This small comparison captures local overhead and scheduling variability, not a
statistical confidence interval or a universal performance guarantee. CPU/memory
limits, Docker VM contention, connection warmup and phase order still matter.

An isolated local run of this command measured the following phase means (ms):

| Pair | Mode | Baseline | 80 ms fault | Recovery |
| --- | --- | ---: | ---: | ---: |
| 1 | Off | 0.567 | 83.457 | 0.478 |
| 1 | Jaeger | 0.647 | 83.386 | 0.559 |
| 2 | Off | 0.413 | 83.410 | 0.457 |
| 2 | Jaeger | 0.825 | 83.210 | 0.626 |

Enabled-minus-disabled mean deltas were +0.081/+0.412 ms for baseline and
+0.080/+0.169 ms for recovery. Fault deltas were -0.071/-0.200 ms; those negative
deltas are consistent with scheduling variation and do not establish a speedup.
Tracing therefore has measurable overhead on these sub-millisecond healthy calls;
no negligible-overhead or statistical-significance claim is made. Every run
completed with successful fault/Compose cleanup and zero measured HTTP failures.

Primary references: [OpenTelemetry Go instrumentation](https://opentelemetry.io/docs/languages/go/instrumentation/),
[Jaeger JSON API limitations](https://www.jaegertracing.io/docs/2.1/apis/),
and the [pinned Jaeger configuration source](https://github.com/jaegertracing/jaeger/blob/v2.22.0/cmd/jaeger/config.yaml).
