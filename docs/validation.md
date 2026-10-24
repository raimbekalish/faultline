# Initial validation record

Date: 2026-10-24. Platform: macOS arm64 with Docker Desktop; Go 1.26.8 in
Linux arm64 containers and Toxiproxy 2.12.0. No host Go installation was used.

## Checks actually run

- `gofmt` over all Go sources and `git diff --check`.
- `go vet ./...` in the pinned Go container.
- `go test -race -count=1 ./...`: passed.
- `./scripts/experiment.sh test`: passed against the real sample services and
  Toxiproxy, with the race detector enabled for unit and integration tests.
- Shell syntax validation (`sh -n scripts/experiment.sh`).
- A remote `DOCKER_HOST` was rejected with exit 2 before any stack creation.
- Local Gitleaks v8.30.1 source scan: no leaks found (network disabled).
- Documented CLI smoke: five samples per phase, 100 ms latency, valid JSON report,
  zero failures, successful proxy and Compose cleanup (including owned image tag).
  Means: baseline 1.00 ms, fault 102.96 ms, recovery 0.21 ms.

The first real integration run measured:

| Injected latency | Baseline mean | Fault mean | Fault failures | Recovery mean |
| --- | ---: | ---: | ---: | ---: |
| 80 ms | 0.27 ms | 84.06 ms | 0 / 5 | 1.07 ms |
| 400 ms | 0.57 ms | 253.56 ms | 5 / 5 (HTTP 504) | 1.43 ms |

The cancellation test observed a toxic on the real control API before canceling
the engine. It verified bounded completion and removal of the owned proxy. All
real tests also created another owner's proxy and verified it remained present
after the experiment; they cleaned up that sentinel themselves. Compose teardown
removed the project's containers and network.

Unit tests cover ambiguous proxy creation, injection/removal errors, cleanup
failure reporting, independent cleanup context after cancellation, rejected
configuration and targets, failing-baseline refusal, percentile calculations,
request timeouts, response-size bounds, redirects and environment proxy refusal.

## Not claimed

No hosted CI, public deployment, production workload test, statistical benchmark,
long-duration soak test, cross-platform matrix, or host-crash recovery test has
run. SIGKILL and Docker daemon failure require explicit project teardown.
The README describes the supported scope and remaining measurement limits.
