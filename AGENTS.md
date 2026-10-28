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

- Host Go: `make check` (format verification, vet, unit race tests; Python 3.10+
  also runs standard-library packaging tests).
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
- Schema 1 permits documented optional fields; disabled observations/telemetry omit
  tracing keys. Optional environment metadata explicitly records tracing off.
- No published collector/admin ports. Collector UI publication requires explicit `--view`
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
- Dashboard: `make frontend-check` checks formatting, component tests and build.
  `./scripts/dashboard.sh -db PATH` serves UI/API on a bounded native loopback
  listener; Go, CGO/C and Node are build prerequisites. Never implicitly add CORS,
  remote bindings, browser fault execution or external fonts/assets.
- Keep dashboard queries in history and reuse record validation. Validate each
  returned record and show partial/unavailable evidence explicitly. Global counts
  describe status metadata, not integrity of off-page report payloads.
- Run `FAULTLINE_TEST_URL=http://127.0.0.1:PORT npm --prefix web run test:browser`
  against real persisted 80 ms and 400 ms experiments. Component fixtures must
  never supply real application metrics or replace this browser integration test.
- Query connections use deferred transactions without writer reservations;
  execution stores retain immediate writes. Preserve hot-journal recovery.

## Portable evidence

- `make cli offline` builds the exporter and CGO-disabled offline verifier.
- Bundle parsing never extracts files or imports SQLite/history. Keep payload,
  count, nesting and archive-profile bounds. Reuse `internal/record` validation
  and the engine summarizer; never create a second statistics implementation.
- Preserve original reports, including missing provenance. Environment capture
  belongs to execution, never export. No ambient identity/environment collection.
- SHA-256 is integrity metadata, not authenticity or execution attestation.
- Publish exports atomically without replacing files/symlinks. Surface failed
  writes/cleanup; test actual temporary files, cancellation and partial reports.
- `faultline-bundle compare LEFT RIGHT` validates both independently. Keep pure
  comparison separate from paths/presentation and share absolute delta arithmetic.
  Never treat missing provenance as equal, missing samples as zero, or metadata
  equality as controlled execution. Failed/canceled/partial pairs have no deltas.
- Comparison schema 1 uses ms and failure-rate percentage points; relative values
  are null for zero references or nonrepresentable arithmetic. Preserve format 1
  and existing history/API fraction units. No current-machine capture in compare.
- The dashboard downloads only by validated ID, never filesystem paths. It stays
  read-only and never makes historical Jaeger references appear permanently live.
- Reliability gates must revalidate policies and evidence before evaluation and
  reuse the engine summarizer. Schema 1 requires complete execution and cleanup;
  missing/partial/undersampled evidence cannot pass. Preserve inclusive bounds,
  fraction units, null missing metrics and INDETERMINATE precedence over FAIL.
- Evaluate alone uses 0/3/4 for PASS/FAIL/INDETERMINATE; keep existing error 1,
  usage 2 and all old command contracts. Keep policy input/output bounded and
  decisions independent of filesystem paths, clocks, environment and services.

## Git workflow

- The production custom-lab CLI exposes only `lab plan`. Keep Docker inspection on the closed read-only
  operation set; no generic subprocess/Compose hooks, provisioning, run or cleanup.
- Plans stay PLANNED/unqualified. Private runtime bindings/nonce never enter
  stdout; publish only to an owned private directory with atomic no-replace writes.
- Future RUN consumption is single-use; cleanup identity requires that run's
  verified journal/IDs and cannot re-enable RUN or gain authority from a prefix.

- M8B2A graph/journal code is a non-mutating foundation, never runtime proof.
  Future cleanup needs consumed matching private history plus fresh exact-ID,
  nonce/config/runtime proof; no prefix/label adoption or unknown-create success.
- Keep journal snapshots private, bounded, append-only and no-replace. Never
  reset consumption after cancellation/cleanup or recover corrupt committed
  history by assuming no resources exist. Retain unresolved state and receipts.
- A future RUN needs one authoritative state namespace and exclusive runtime
  slot. Do not wire journal contracts to a mutation transport or expose RUN/CLEANUP
  before separate review/qualification. Synthetic tests are not Docker isolation.

- Future custom execution must use the OS-account fixed authority, not an env/
  directory override or a raw JournalStore. Enrollment is explicit; preserve
  anchors/checkpoints/tombstones and never reset missing/corrupt state to fresh.
- Kernel lease release is not resource cleanup. Durable unresolved reservations
  block new work; imported legacy history never adopts resources. Keep waits,
  registry/file sizes and quotas bounded; no automatic state repair/deletion.
- Observer commands inspect exact recorded IDs with fixed projections only.
  Recompute verified profiles; labels/digests alone are not ownership or isolation.
  Do not connect the opaque mutation interface to a real driver or certify absence/
  mutation success before separate review. No new RUN/CLEANUP CLI is authorized.
- Preserve 21-event cleanup plus 21-event retry admission reserves. Recording
  failure/exhaustion cannot erase already-proven in-memory cleanup scope, invent a
  successful receipt or enable more provisioning. Use a fresh cleanup context.

- Closed command argv stays private and disconnected from any subprocess driver.
  Only opaque revision/lease-bound certificates may record mutation success;
  exit zero, labels, names and failed inspect never establish ownership/absence.
- Require qualified continuity; daemon/socket/bridge equality is insufficient.
  No production provider or mutation driver exists. Never replace this gate with
  a fabricated epoch or describe synthetic certification as real qualification.
- Preserve durable attempt slots and already-proven cleanup IDs after recording
  failure. Unconfirmed/exhausted recording cannot release reservations or enable
  provisioning. Container state is not worker/proxy/report completion.
- Dedicated catalog sources are preflight-only, not qualified execution images.
  Do not build/pull/run them or expose RUN/CLEANUP without separate authorization.

- Use the repository-local Git identity associated with the owner's GitHub account.
- Make small conventional commits for completed, verified units. Push milestones.
- Direct main commits are acceptable during initial bootstrap. Larger subsequent
  features belong on `codex/` branches with reviewed pull requests.
- Keep main stable. Never force-push, fabricate dates, or create empty commits.
- Never commit credentials, generated reports, caches, datasets or large binaries.

## Release candidates

- `scripts/package-release.py` only builds new ignored local candidate directories;
  require clean committed source. Never tag, upload or create a GitHub Release
  without explicit approval. Do not overwrite previous candidates/evidence.
- Offline builds use CGO=0; native local packages use host CGO and freshly built
  locked assets. Cross-compilation is build evidence, not runtime qualification.
- Keep synthetic compatibility fixtures labeled and small; review schema, units,
  nulls and reason changes rather than automatically regenerating expected output.
- Use distinct `FAULTLINE_SCREENSHOT_DIR` / `FAULTLINE_BROWSER_OUTPUT` paths when
  preserving earlier browser evidence. Record emulated runtimes as emulated.
- Parser/header/Unicode regression and bounded fuzz checks complement real evidence;
  never describe them as a comprehensive security audit or hashes as authenticity.
