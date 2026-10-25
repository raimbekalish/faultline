# Faultline

Local-first reliability experiments for containerized distributed systems.

Faultline measures a real HTTP application before, during, and after network
latency injected into its dependency using Toxiproxy. The first working milestone
is a Go CLI with a disposable Docker Compose sample stack, automated real-fault
tests and JSON reports. This is an early-stage project with a deliberately small
scope.

## Scope

- Go experiment engine and CLI
- A small, isolated Docker Compose sample application
- Bounded HTTP measurements, explicit fault ownership, cancellation and cleanup
- Machine-readable reports and automated tests against real network faults

Optional OpenTelemetry tracing and local Jaeger correlate observations across
the real sample services. Versioned JSON specifications and local SQLite history
support repeatable runs, inspection and comparisons. A read-only local dashboard
visualizes persisted evidence through a protected loopback API.
No hosted service or paid infrastructure is required.

## Engineering documentation

- [Architecture](docs/architecture.md)
- [Decisions](docs/decisions/0001-local-vertical-slice.md)
- [Roadmap](docs/roadmap.md)
- [Contributor instructions](AGENTS.md)
- [Report format and measurement limits](docs/reports.md)
- [Validation record](docs/validation.md)
- [Distributed tracing and failure correlation](docs/tracing.md)
- [Specifications, persistent history and recovery](docs/history.md)
- [Local dashboard, API and browser verification](docs/dashboard.md)
- [Portable evidence and environment provenance](docs/evidence.md)

## Run an experiment

Requirements: Docker Engine/Desktop with Compose v2 or later, Git, and a POSIX
shell on macOS or Linux. Docker must be running through a local Unix socket;
the wrapper refuses remote endpoints. A host Go installation is optional. The first run
downloads pinned Go and Toxiproxy images and builds two small static binaries.

```sh
git clone https://github.com/raimbekalish/faultline.git
cd faultline
mkdir -p artifacts
./scripts/experiment.sh > artifacts/report.json
```

The script starts the sample app, runs ten sequential requests per phase with
100 ms injected latency, writes JSON to stdout, and tears down its own stack.
Build/status messages and a short comparison go to stderr. Reports stay local
and are ignored by Git.

```text
runner → frontend → Toxiproxy → dependency
   └──────────────→ control API
```

Observe dependency timeouts (the frontend has a 250 ms deadline):

```sh
./scripts/experiment.sh -samples 5 -latency 400ms > artifacts/timeouts.json
```

The fault phase should return HTTP 504 failures while baseline and recovery
succeed. Measured failures are a valid experiment outcome; setup, baseline,
recovery, cancellation and cleanup errors return a nonzero exit status.

CLI options: `-samples` (1–100 per phase), `-latency` (1ms–5s), and
`-request-timeout` (10ms–5s). Durations must use whole milliseconds. Every run
has a two-minute execution deadline plus a separate five-second cleanup budget.
Large workloads may end with a partial report when that deadline is reached.

## Test

Run all unit and integration tests with the Go race detector against real
Toxiproxy, including latency, timeout failures, recovery and active-fault
cancellation:

```sh
./scripts/experiment.sh test
```

With Go 1.26 and a C compiler (CGO enabled), `make check` runs formatting verification, vet and unit
race tests, plus shell endpoint-selection regressions. The shell checks can run
alone with `sh scripts/test-wrapper.sh`. Without host Go:

```sh
docker build --target dev -t faultline-dev .
docker run --rm --network=none --cpus=2 --memory=1g faultline-dev \
  sh -c 'test -z "$(gofmt -l cmd internal)" && go vet ./... && go test -race -count=1 ./...'
```

The integration build tag is intentionally opt-in: `go test ./...` alone does
not prove network injection. No GitHub Actions or hosted CI is configured.

## Reproducible runs and history

```sh
./scripts/experiment.sh run -spec examples/latency.json
./scripts/experiment.sh run -spec examples/latency.json
./scripts/experiment.sh history list
```

Specification runs default to local `artifacts/faultline.sqlite`. Use `-db` for a
custom path or to opt legacy flag-based runs into persistence. `history show`
inspects a listed ID; `history compare` returns phase deltas for two completed
runs. Queries start no sample/tracing services. Specs reject unknown fields,
duplicates, unsupported versions and workload overrides. Existing ephemeral
commands and schema-1 stdout reports retain their behavior. History rejects stored
summaries or comparisons inconsistent with their underlying raw samples.

History commits an intent before execution and the complete/failed/canceled
report after fault cleanup. A failed final write leaves an unfinished intent and
returns an error. Unfinished entries do not imply a known failure cause. Trace
IDs survive as references, while transient Jaeger data does not. See
[history commands and durability limits](docs/history.md) before relying on storage.

## Local reliability dashboard

Inspect actual SQLite history: recorded-status counts, searchable runs,
baseline/fault/recovery charts, raw observations, historical trace references and
comparisons of completed runs. Experiment services and Jaeger can be stopped.

With Node 22.12+, Go 1.26+ and a C compiler installed:

```sh
# Generate history if needed; its isolated sample stack tears down afterward:
./scripts/experiment.sh run -spec examples/latency.json

# Build and serve UI + API on one ephemeral loopback origin:
./scripts/dashboard.sh -db artifacts/faultline.sqlite
```

Open the printed `http://127.0.0.1:…` URL. Ctrl-C stops the server; it also expires
after one hour by default. The dashboard has no fault execution or Docker access.
See [dashboard commands, API and limitations](docs/dashboard.md).

## Portable evidence

Export a finalized run and inspect it offline without Docker, SQLite, Jaeger or
network access. Build the native tools once, then use a listed run ID:

```sh
make cli offline
./artifacts/bin/faultline bundle export -db artifacts/faultline.sqlite -out artifacts/run.flb RUN_ID
./artifacts/bin/faultline-bundle verify artifacts/run.flb
./artifacts/bin/faultline-bundle inspect artifacts/run.flb
./artifacts/bin/faultline-bundle compare LEFT.flb RIGHT.flb
```

Copy the platform-appropriate verifier with the bundle for offline use. The
archive preserves actual reports/specifications and includes checksum metadata;
checksums do not establish authenticity. New runs record allowlisted environment
facts; missing historical provenance stays unavailable. Run details also offer a
same-origin download. See [format, safety and portability limits](docs/evidence.md).

Offline comparison validates both inputs, reports right-minus-left phase changes,
and distinguishes matching recorded conditions, descriptive-only differences and
non-comparable partial/failed evidence. Missing provenance stays unknown; relative
changes with zero references remain unavailable. It makes no significance or
causality claim. See [comparison rules and examples](docs/evidence.md#offline-comparison--result-schema-1).

## Optional distributed tracing

```sh
./scripts/experiment.sh --tracing -samples 5 -latency 80ms
./scripts/experiment.sh --tracing test
./scripts/experiment.sh --tracing evidence > artifacts/evidence.json
```

The evidence command runs 80/400 ms faults and saves real observations, correlated
span summaries and raw Jaeger responses. Add `--view` after `--tracing` to publish
the query UI on a random loopback port for five minutes; stderr prints its URL.
Ingestion and administrative endpoints stay internal. Default experiments start
no Jaeger service and create no SDK provider. See [tracing](docs/tracing.md) for
exact viewing commands, bounded export, interpretation, and overhead measurement.

## Safety and cleanup

Only the checked-in sample topology is supported; arbitrary target URLs are
not accepted. Each invocation gets a unique Compose project and internal network.
No host ports are published by default; explicit `--view` publishes only the
loopback Jaeger query/UI port. Containers do not receive the Docker socket, host
networking or elevated privileges. Runtime services use non-root users, read-only
filesystems, dropped capabilities and CPU/memory/process limits.

The engine deletes its uniquely named proxy under a fresh timeout even when a
request is canceled or a control response is lost. Deleting the proxy also removes
its fault. It never calls Toxiproxy's global reset. The script then removes only
its project's containers/network and image tags. Shared base images and build
cache remain for subsequent runs.

Ctrl-C/SIGTERM request cleanup. SIGKILL, Docker failure, or a host crash can leave
resources behind. If interrupted forcibly, use `docker compose ls -a` to identify
the exact `faultline-…` project, then run:

```sh
FAULTLINE_PROJECT=<exact-project-name> docker compose -p <exact-project-name> \
  -f deploy/compose.yaml -f deploy/compose.tracing.yaml -f deploy/compose.ui.yaml \
  down --volumes --remove-orphans --timeout 12
```

Use all three files even if tracing was disabled: this also selects any optional
Jaeger container and viewer network left by an interrupted run. Teardown does not
create missing services. Never substitute another project's name.
The control API has no authentication;
isolation assumes a trusted local Docker host. Do not expose it or use production
services. These tests show a small causal latency/timeout effect, not production
capacity or statistical significance.

## License

[MIT](LICENSE)
