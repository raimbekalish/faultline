# ADR 0001: A Go CLI and an isolated Compose vertical slice

- Status: accepted
- Date: 2026-10-24

## Context

The initial product must prove actual network fault injection and recovery on a
MacBook, with understandable ownership and modest resource consumption.

## Decision

Use one Go module and standard-library HTTP clients/servers. Use Docker Compose
for a disposable frontend, dependency, Toxiproxy and runner. Toxiproxy performs
the actual TCP latency injection; do not simulate latency in the application.
Restrict the first CLI to this sample topology. Use bounded serial traffic and
JSON reports with raw measurements before adding concurrent load generation.

Defer React, SQLite, OpenTelemetry and Jaeger until the lifecycle and real-fault
tests work. A dashboard adds no evidence of correctness yet. JSON is a sufficient
first persistence format. Distributed tracing and indexed history are valuable
subsequent milestones, but would expand this slice and consume more resources.

Pin build/runtime versions and provide a Docker-based test path so a host Go
installation is optional. Do not configure CI or hosted services.

## Consequences

The initial tool cannot test arbitrary user applications. This is an intentional
safety boundary. The architecture remains extensible through concrete lifecycle
and transport boundaries, without premature plugin abstractions or microservices.
Small serial samples show latency and timeout effects, not production throughput.
