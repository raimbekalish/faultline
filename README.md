# Faultline

Local-first reliability experiments for containerized distributed systems.

Faultline is an early-stage, open-source engineering project. The first milestone
is a reproducible experiment that measures an HTTP application before, during,
and after latency injected into its dependency using Toxiproxy.

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

## Development status

Repository bootstrap and the bounded Go transport/measurement foundation are
complete. The initial executable vertical slice is next.
Do not aim this software at production or unrelated systems.

## License

[MIT](LICENSE)
