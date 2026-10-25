# Local reliability dashboard

Faultline reads existing SQLite history without starting experiments, collectors
or Docker services. No account, API key or paid service is required.

## Startup

Build prerequisites: Node 22.12+ (24.11.0 tested), Go 1.26+ (1.26.8 tested), npm,
and a C compiler for the existing SQLite driver. Docker is needed only to generate
experiments. From the repository root:

```sh
# Each experiment tears down its isolated sample stack:
./scripts/experiment.sh run -spec examples/latency.json
./scripts/experiment.sh --tracing run -spec examples/timeouts.json

# Lockfile install, production build, native Go build, single local origin:
./scripts/dashboard.sh -db artifacts/faultline.sqlite
```

Use the exact printed `http://127.0.0.1:port` URL. Default port: ephemeral; lifetime:
one hour. Ctrl-C/SIGTERM requests bounded graceful shutdown. To choose a port:

```sh
./scripts/dashboard.sh -db artifacts/faultline.sqlite -listen 127.0.0.1:8080 -ttl 30m
```

`-ttl` accepts 1s–8h. Remote/hostname/IPv6 bindings are refused. `FAULTLINE_GO`
can select an existing Go executable. The script never downloads a Go toolchain or
changes system installations. Missing databases fail without creating files; an
existing empty database shows a first-run guide.

After building, serve without Node/Go at runtime:

```sh
./artifacts/bin/dashboard -db artifacts/faultline.sqlite -assets web/dist
```

Keep the binary with matching build assets. No hot-reload/proxy development server
is configured; rebuild/restart after edits. History refresh is explicit, with no
polling or report storage in browser local storage.

## Reading evidence

- Overview counts recorded statuses. **Completed** means the experiment finished
  and cleaned up; expected fault-phase HTTP errors can occur in a completed run.
- History searches IDs and canonical configuration JSON, with status filters and
  cursor pages. Search is literal, not SQL or a query language.
- Detail renders real sample counts, metrics and request durations. Chart axes
  start at zero; the horizontal axis is serial observation order, not wall time.
  All outcomes contribute durations; percentiles use nearest rank. Focus/select
  chart points or rows for HTTP errors and trace/span references.
- Failed/canceled reports can have partial phases. Unfinished intents have no
  inferred outcome or final report. Empty phases display unavailable values;
  recorded zeros stay zero and tiny nonzero displayed values use `<0.01`.
- Comparisons require completed, successfully cleaned runs. Suggestions contain
  the newest 50; paste any historical history/engine ID. Deltas are **right minus
  left**; failure-rate deltas use percentage points. Paired duration bars share
  scales within each metric/phase; failure rates use 0–100%. Configuration
  differences are visible. Equal specs imply neither equal environments nor
  statistical significance.
- Trace IDs are references only. Temporary Jaeger data disappears at teardown;
  missing IDs mean no reference was recorded. There are no implied collector
  links. End-to-end HTTP durations are not individual span durations.

## API version 1

GET-only routes return `application/json`, `Cache-Control: no-store` and
`{"api_version":1,"data":…}`. Errors instead return
`{"api_version":1,"error":{"code":"…","message":"…"}}`.

| Route | Data |
| --- | --- |
| `/api/v1/health` | Process liveness and `read_only` mode; not an integrity scan |
| `/api/v1/runs` | `items`, global `counts`, filtered `total`, nullable `next_cursor` |
| `/api/v1/runs/{id}` | Existing entry and original report, including raw samples |
| `/api/v1/compare?left=ID&right=ID` | Existing comparison, specifications and phase deltas |

List options: `limit` (1–50, default 20), `cursor` (history ID from the previous
page), `status` (complete/failed/canceled/running), `q` (ID/configuration substring,
at most 128 UTF-8 bytes). Clear cursors when changing filters. Unknown/duplicate
parameters and malformed IDs fail. Lists validate fetched records, omit raw reports
and expose phase summaries, cleanup and bounded error previews. Detail accepts
both ID aliases. Corrupt returned records fail without repair. Counts aggregate
status metadata, not report integrity outside the page or averages.

Each page uses one SQLite snapshot ordered by descending stored creation timestamp
and ID. A validated cursor anchors the next page, so new inserts do not shift older
pages. Separate requests may see new totals or finalized intents. Counts/filtering
can scan history; the deadline does not imply constant-time queries at any scale.

| HTTP status | Meaning |
| --- | --- |
| 400 | Invalid query or ID |
| 403 | Unexpected Host/Origin or cross-site fetch |
| 404 | Missing run, cursor anchor, API route or asset |
| 405 | Unsupported method |
| 409 | A comparison includes a non-complete run |
| 429 | Four API queries are already in flight |
| 500 | Corrupt history or encoding failure |
| 503 | Query unavailable, canceled, locked or timed out |

Existing payload limits, four API slots, three-second query deadlines, 8 KiB
headers and HTTP read/write/idle timeouts bound work. Error responses omit internal
paths, SQL and driver errors. Only the index, favicon and built asset routes are
served; directory listings are refused.

## Security and compatibility

The server binds only IPv4 loopback. Exact Host and optional Origin validation plus
cross-site fetch rejection defend against unexpected origins/DNS rebinding. There
is no permissive CORS. CSP limits scripts/styles/fonts/connections to the local
origin and forbids framing. System fonts/static SVG need no third-party requests.
The API has no mutations, fault control or collector access. It is not an
authentication boundary against local processes or untrusted code as the same user.

Use trusted build assets and a local database. SQLite hot-journal recovery needs a
writable directory/journal: read-only API does not mean immutable filesystem.
Deferred reader transactions avoid immediate writer reservations, though DELETE
journal snapshots can briefly contend with finalization. Startup does not migrate
or repair history. Existing JSON/database schema 1 and optional tracing are
preserved. Prior crash/power-loss limitations remain.

## Verification

```sh
make check
make frontend-check
./scripts/experiment.sh test
./scripts/experiment.sh --tracing test
```

For actual-data browser testing, generate both example runs above, start the server
on the selected port and install Chrome (the Playwright Chrome channel). In another
terminal:

```sh
FAULTLINE_TEST_URL=http://127.0.0.1:8080 npm --prefix web run test:browser
```

The test uses actual API responses without interception. It checks pagination,
filtering, exact measurements/deltas, trace references, keyboard access, mobile
overflow, browser errors and external requests. Screenshots of overview, detail,
comparison and mobile views are saved under ignored `artifacts/`. Component
fixtures cover loading, empty, corrupt, partial/canceled, unfinished and missing
telemetry states. Go API tests use actual temporary SQLite files. See
[validation](validation.md) for actual outcomes and [ADR 0004](decisions/0004-local-read-only-dashboard.md)
for API/dependency decisions.
