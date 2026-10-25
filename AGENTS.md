# Working on Faultline

Build a real, local-first reliability tool. Prefer a small working vertical slice
over speculative interfaces or mocked product features.

## Boundaries and safety

- Work only in this repository. Never modify sibling projects.
- Faults may target only the explicit sample topology in an isolated Compose
  project. Do not add arbitrary targets without a reviewed safety design.
- Never mount the Docker socket into a container, use host networking, publish
  the Toxiproxy control API, or run privileged containers.
- Every created fault needs an ownership identifier, bounded duration, and cleanup
  under a fresh timeout independent of the canceled experiment context.
- Never reset a shared Toxiproxy server or delete resources owned by another run.
- Report cleanup failures; do not hide them behind a successful result.
- No public deployment, paid services, or GitHub Actions without explicit approval.

## Engineering

- Keep the Go engine separate from CLI, sample services, and fault transport.
- Propagate context cancellation; bound requests, response sizes, and workloads.
- Use standard library packages where practical. Justify new dependencies in an ADR.
- Keep measurements reproducible: record configuration, raw samples and versions.
- Test observable behavior, failure paths and cleanup. A fake control API is useful
  for edge cases, but real Toxiproxy tests are required for fault claims.
- Run formatting, vet and race tests where supported. Clearly report unrun checks.
- Update README, architecture, roadmap and decisions when behavior changes.

## Verification commands

- Host Go: `make check` (format verification, vet, unit race tests).
- Docker only: build the `dev` target and run the command documented in README.
- Real faults: `./scripts/experiment.sh test` (isolated Compose stack, unit and
  integration race tests, automatic teardown).
- CLI smoke: `./scripts/experiment.sh -samples 5 -latency 100ms`; capture stdout
  under ignored `artifacts/` when inspecting JSON. Keep progress on stderr.
- Run integration tests through the wrapper, never against a shared proxy server.
- `sh scripts/test-wrapper.sh` checks Docker endpoint precedence without a daemon.
  `DOCKER_CONTEXT` overrides `DOCKER_HOST`; reject the endpoint actually selected.
- Tracing: `./scripts/experiment.sh --tracing test`; default `test` must also pass
  without Jaeger. `--tracing evidence` retains reports and real query responses.
- Run `python3 scripts/measure-tracing-overhead.py` alone when measuring overhead.
- Keep providers explicit, queues nonblocking/bounded, export after fault cleanup,
  and response-body reads inside client spans. Preserve existing HTTP deadlines.
- Do not record headers, bodies, raw URLs/errors or arbitrary baggage in spans.
  Only validated run/phase/observation metadata may propagate. Toxiproxy has no spans.
- Schema 1 permits documented optional fields; disabled reports omit tracing keys.
- No published collector/admin ports. UI publication requires explicit `--view`
  and must bind loopback, use an ephemeral port, and have a bounded lifetime.
- History uses CGO SQLite; native builds require a C compiler. Keep the CLI static
  in Docker and sample services CGO-disabled. Do not use a system SQLite override.
- `run -spec examples/latency.json` persists by default; original flag-only runs
  stay ephemeral. Test both modes. Never mount a database into source control.
- Commit an intent before the engine; finalize only after fault cleanup/export.
  Keep SQL transactions out of measurements. Cancellation must not cancel final
  persistence. Never overwrite completed reports or guess why an intent is unfinished.
- Preserve database journals. Use temporary files in tests and real rollback/
  process-exit recovery, not SQL mocks. Keep integration packages serialized.
- History stores trace references, not durable Jaeger data. Comparison is
  descriptive; matching spec hashes do not establish identical environments.

## Git workflow

- Use the repository-local Git identity associated with the owner's GitHub account.
- Make small conventional commits for completed, verified units. Push milestones.
- Direct main commits are acceptable during initial bootstrap. Larger subsequent
  features belong on `codex/` branches with reviewed pull requests.
- Keep main stable. Never force-push, fabricate dates, or create empty commits.
- Never commit credentials, generated reports, caches, datasets or large binaries.
