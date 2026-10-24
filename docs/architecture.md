# Architecture

## First vertical slice

```text
CLI / engine --> frontend --> Toxiproxy data port --> dependency
       |
       +-------------------> Toxiproxy control API
```

One Compose project owns the sample stack and its private network. No service
ports are published to the host. The runner and sample services use explicit
service names; the first release does not accept arbitrary network targets.

The Go engine runs baseline, latency and recovery phases in sequence. It owns
one uniquely named proxy and fault, measures bounded serial HTTP traffic, and
removes its resources on success, error or cooperative cancellation. A shell
entrypoint tears down only its own Compose project as a second cleanup layer.

Planned code boundaries:

- `cmd/faultline`: CLI parsing, signal handling and report output
- `internal/experiment`: lifecycle, measurement and comparison
- `internal/toxiproxy`: bounded control API client
- `internal/sample`: HTTP sample services
- `deploy/`: sample topology
- `scripts/`: reproducible developer entrypoints

## Safety model

The dedicated local topology is the allowlist. Toxiproxy is reachable only inside
the isolated network. The process never changes host firewall rules or mounts a
Docker socket. Limits bound samples, injected latency and total execution time.
Cleanup uses a separate timeout and surfaces errors. SIGKILL, host crashes and
Docker daemon failure cannot guarantee in-process cleanup; project teardown is
the recovery mechanism.

## Measurement model

Compare identical sequential workloads before, during and after injection.
Record individual end-to-end HTTP durations and outcomes, failure rate and
latency distributions. These small samples validate causal behavior; they do not
claim statistically rigorous performance benchmarks or saturation capacity.

See [ADR 0001](decisions/0001-local-vertical-slice.md) for scope decisions.
