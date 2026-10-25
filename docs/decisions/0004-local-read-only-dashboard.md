# ADR 0004: One local origin for reliability analysis

- Status: accepted
- Date: 2026-10-24

Serve built React/TypeScript/Vite assets and `/api/v1` from a Go standard-library
HTTP server. Bind only an explicit IPv4 loopback address, with an ephemeral port
by default and a bounded lifetime. Validate exact Host and Origin, refuse cross-site
fetches and all mutations, and set a restrictive CSP. No CORS, experiment execution,
collector access or Docker access exists in this process.

Use the existing SQLite store and report validator. Add snapshot-based keyset
pagination and status/ID/configuration search in the history package. Each returned
record is validated before projection into a list summary; detail retains the
original report. Global counts count persisted intent/status metadata, not inferred
outcomes or averages. Counts do not certify off-page report payloads. Queries have
three-second deadlines, page limits, bounded search strings and bounded concurrency.
Unknown/duplicate query parameters fail. No migration is needed. SQLite can still
perform hot-journal recovery; read-only API describes product capabilities, not an
immutable filesystem mount.

Contract: `GET /api/v1/health`, `/api/v1/runs?limit=20&cursor=history-…&status=…&q=…`,
`/api/v1/runs/{id}`, `/api/v1/compare?left=…&right=…`. Every response has
`api_version: 1` and either `data` or a bounded `{code,message}` error. Lists return
`items`, global `counts`, filtered `total` and nullable `next_cursor`. Cursors are
validated history IDs with stable timestamp/ID ordering, not offsets. Newer inserts
do not duplicate older pages; refreshing starts a new snapshot. Invalid input is
400, missing runs 404, incomplete comparisons 409, corrupt records 500, and bounded
resource/deadline failures 503/429. Do not expose internal paths/SQL errors.

React is the sole production JavaScript library. Use small accessible SVG plots
for this fixed three-phase model rather than a general chart/UI framework. Zero
baselines, shared within-chart scales, raw observation inspection and tables keep
the evidence inspectable. A hash-based view URL needs no router dependency. Vite
builds static assets; the supported startup serves one origin without a proxy.
Vitest/Testing Library and Playwright are development-only dependencies, locked
along with TypeScript and Vite. System fonts avoid third-party requests.

Partial, canceled and unfinished data remain explicit. Missing samples do not
become zeros; unavailable reports have no charts. Trace IDs are historical
references with no promised collector link. Comparisons show differing specs and
descriptive right-minus-left deltas without significance claims.

The native server requires Go/CGO and a C compiler, preserving literal loopback
binding without a container port-forwarding exception. No dashboard container,
socket mount or host networking is needed. Node is needed to build, not serve.
There is no live push/polling or persistent browser storage of reports. Retention,
arbitrary targets, fault execution and authentication remain out of scope.

Primary references: [React TypeScript guidance](https://react.dev/learn/typescript),
[Vite toolchain requirements](https://vite.dev/guide/), and
[Go HTTP server documentation](https://pkg.go.dev/net/http). Exact npm versions
and transitive integrity hashes are pinned in `web/package-lock.json`.
