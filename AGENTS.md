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

## Git workflow

- Use the repository-local Git identity associated with the owner's GitHub account.
- Make small conventional commits for completed, verified units. Push milestones.
- Direct main commits are acceptable during initial bootstrap. Larger subsequent
  features belong on `codex/` branches with reviewed pull requests.
- Keep main stable. Never force-push, fabricate dates, or create empty commits.
- Never commit credentials, generated reports, caches, datasets or large binaries.
