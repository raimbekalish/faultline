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

The dashboard, distributed tracing and SQLite history are planned; they are not
implemented yet. No hosted service or paid infrastructure is required.

## Engineering documentation

- [Architecture](docs/architecture.md)
- [Decisions](docs/decisions/0001-local-vertical-slice.md)
- [Roadmap](docs/roadmap.md)
- [Contributor instructions](AGENTS.md)
- [Report format and measurement limits](docs/reports.md)
- [Validation record](docs/validation.md)

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

With Go 1.26 installed, `make check` runs formatting verification, vet and unit
race tests. Without host Go:

```sh
docker build --target dev -t faultline-dev .
docker run --rm --network=none --cpus=2 --memory=1g faultline-dev \
  sh -c 'test -z "$(gofmt -l cmd internal)" && go vet ./... && go test -race -count=1 ./...'
```

The integration build tag is intentionally opt-in: `go test ./...` alone does
not prove network injection. No GitHub Actions or hosted CI is configured.

## Safety and cleanup

Only the checked-in sample topology is supported; arbitrary target URLs are
not accepted. Each invocation gets a unique Compose project and internal network.
No host ports are published. Containers do not receive the Docker socket, host
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
  -f deploy/compose.yaml down --volumes --timeout 5
```

Never substitute another project's name. The control API has no authentication;
isolation assumes a trusted local Docker host. Do not expose it or use production
services. These tests show a small causal latency/timeout effect, not production
capacity or statistical significance.

## License

[MIT](LICENSE)
