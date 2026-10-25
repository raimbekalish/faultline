import { useState } from "react";
import { useRemote } from "../api";
import type { Entry, PhaseName } from "../types";
import { phases } from "../types";
import { date, ms, percent, shortID } from "../format";
import { Legend, ObservationChart } from "../charts";
import type { Point } from "../charts";
import { Badge, State, PageTitle } from "../components";
function PhaseTable({ entry }: { entry: Entry }) {
  const r = entry.report!;
  return (
    <div className="table-scroll">
      <table>
        <thead>
          <tr>
            <th>Phase</th>
            <th>Observed / configured</th>
            <th>Mean</th>
            <th>p50</th>
            <th>p95</th>
            <th>p99</th>
            <th>Min / max</th>
            <th>Failures</th>
          </tr>
        </thead>
        <tbody>
          {phases.map((p) => {
            const s = r[p].summary;
            return (
              <tr key={p}>
                <td className="phase-label">
                  <i className={p} />
                  {p}
                </td>
                <td className="mono">
                  {s.requests} / {entry.spec.samples_per_phase}
                </td>
                <td>{s.requests ? ms(s.mean_ms) : "—"}</td>
                <td>{s.requests ? ms(s.p50_ms) : "—"}</td>
                <td>{s.requests ? ms(s.p95_ms) : "—"}</td>
                <td>{s.requests ? ms(s.p99_ms) : "—"}</td>
                <td>
                  {s.requests ? `${ms(s.min_ms)} / ${ms(s.max_ms)}` : "—"}
                </td>
                <td>
                  {s.requests
                    ? `${s.failures}/${s.requests} · ${percent(s.failure_rate)}`
                    : "—"}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
export default function Detail({ id }: { id: string }) {
  const result = useRemote<Entry>(`/api/v1/runs/${encodeURIComponent(id)}`),
    [phase, setPhase] = useState<PhaseName>("fault"),
    [selected, setSelected] = useState<Point | null>(null),
    e = result.data,
    r = e?.report;
  function choose(p: Point) {
    setPhase(p.phase);
    setSelected(p);
  }
  return (
    <>
      <a className="back-link" href="#/history">
        ← Experiment history
      </a>
      <PageTitle
        eyebrow="Experiment evidence"
        title={e ? `Run ${shortID(e.run_id ?? e.id)}` : "Experiment detail"}
      >
        {e && <Badge status={e.status} />}
      </PageTitle>
      <State {...result}>
        {e && (
          <>
            <div className="spec-strip">
              <div>
                <span>Injected latency</span>
                <strong>{e.spec.latency_ms} ms</strong>
              </div>
              <div>
                <span>Samples / phase</span>
                <strong>{e.spec.samples_per_phase}</strong>
              </div>
              <div>
                <span>Request timeout</span>
                <strong>{e.spec.request_timeout_ms} ms</strong>
              </div>
              <div>
                <span>Recorded</span>
                <strong className="small-value">{date(e.created_at)}</strong>
              </div>
            </div>
            {!r ? (
              <section className="panel state">
                <h2>Intent recorded. Outcome unknown.</h2>
                <p>
                  No final report was committed. The run may still be active,
                  have stopped abruptly, or have failed to persist its report.
                </p>
                <p>Missing measurements are unavailable, not zero.</p>
              </section>
            ) : (
              <>
                {(r.error ||
                  !r.cleanup_complete ||
                  r.status !== "complete") && (
                  <div className="notice" role="note">
                    <strong>
                      {r.status === "canceled"
                        ? "Canceled run · partial evidence"
                        : r.status === "failed"
                          ? "Failed run · recorded evidence"
                          : "Cleanup warning"}
                    </strong>
                    <p>{r.error || "Cleanup was not confirmed."}</p>
                    <span>
                      Cleanup{" "}
                      {r.cleanup_complete ? "confirmed" : "NOT confirmed"}.
                      Phase sample counts show what was actually recorded.
                    </span>
                  </div>
                )}
                <section className="panel">
                  <div className="panel-heading">
                    <div>
                      <span className="eyebrow">Request latency</span>
                      <h2>Baseline → fault → recovery</h2>
                    </div>
                    <Legend />
                  </div>
                  <ObservationChart report={r} onSelect={choose} />
                  <PhaseTable entry={e} />
                  <div className="panel-foot">
                    <span>
                      All outcomes included · nearest-rank percentiles · no
                      significance claim
                    </span>
                    {e.status === "complete" && (
                      <a href={`#/compare?left=${e.id}`}>Compare this run →</a>
                    )}
                  </div>
                </section>
                <section className="panel observations">
                  <div className="panel-heading">
                    <div>
                      <span className="eyebrow">Raw observations</span>
                      <h2>Request-level evidence</h2>
                    </div>
                    <div
                      className="tabs"
                      role="group"
                      aria-label="Observation phase"
                    >
                      {phases.map((p) => (
                        <button
                          key={p}
                          aria-pressed={phase === p}
                          className={phase === p ? "selected" : ""}
                          onClick={() => {
                            setPhase(p);
                            setSelected(null);
                          }}
                        >
                          {p} <small>{r[p].summary.requests}</small>
                        </button>
                      ))}
                    </div>
                  </div>
                  <div className="observation-grid">
                    <div className="observation-table">
                      <table>
                        <thead>
                          <tr>
                            <th>Request</th>
                            <th>Duration</th>
                            <th>HTTP outcome</th>
                            <th>Trace reference</th>
                          </tr>
                        </thead>
                        <tbody>
                          {(r[phase].samples ?? []).map((s, i) => (
                            <tr
                              key={i}
                              className={
                                selected?.index === i &&
                                selected.phase === phase
                                  ? "selected-row"
                                  : ""
                              }
                            >
                              <td>
                                <button
                                  className="text-button mono"
                                  onClick={() =>
                                    choose({ phase, index: i, sample: s })
                                  }
                                >
                                  #{String(i + 1).padStart(2, "0")}
                                </button>
                              </td>
                              <td className="mono">{ms(s.duration_ms)}</td>
                              <td>
                                <span
                                  className={
                                    s.error || s.status < 200 || s.status >= 300
                                      ? "outcome-failed"
                                      : "outcome-ok"
                                  }
                                >
                                  {s.status
                                    ? `HTTP ${s.status}`
                                    : "No response"}
                                </span>
                              </td>
                              <td className="mono">
                                {s.trace_id ? (
                                  `${s.trace_id.slice(0, 12)}…`
                                ) : (
                                  <span className="muted">Not recorded</span>
                                )}
                              </td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                      {!r[phase].summary.requests && (
                        <p className="state">
                          No observations recorded in this phase.
                        </p>
                      )}
                    </div>
                    <aside className="observation-inspector">
                      <span className="eyebrow">Observation detail</span>
                      {selected ? (
                        <>
                          <h3>
                            {selected.phase} · #{selected.index + 1}
                          </h3>
                          <dl>
                            <dt>End-to-end duration</dt>
                            <dd>{ms(selected.sample.duration_ms)}</dd>
                            <dt>HTTP response</dt>
                            <dd>{selected.sample.status || "Unavailable"}</dd>
                            <dt>Error</dt>
                            <dd>{selected.sample.error || "None recorded"}</dd>
                            <dt>Trace ID</dt>
                            <dd className="mono">
                              {selected.sample.trace_id ||
                                "No trace reference recorded"}
                            </dd>
                            <dt>Root span ID</dt>
                            <dd className="mono">
                              {selected.sample.span_id || "Not recorded"}
                            </dd>
                          </dl>
                        </>
                      ) : (
                        <>
                          <h3>Follow an observation.</h3>
                          <p>
                            Select a request row or chart point to inspect its
                            timing, outcome and telemetry references.
                          </p>
                        </>
                      )}
                      <p className="trace-note">
                        Trace IDs are references only. Temporary Jaeger traces
                        disappear at teardown; availability is not implied.
                      </p>
                    </aside>
                  </div>
                </section>
              </>
            )}
            <details className="panel provenance">
              <summary>Configuration & provenance</summary>
              <dl>
                <dt>History ID</dt>
                <dd className="mono">{e.id}</dd>
                <dt>Engine run ID</dt>
                <dd className="mono">{e.run_id || "Not assigned"}</dd>
                <dt>Specification hash</dt>
                <dd className="mono">{e.spec_hash}</dd>
                <dt>Revision / Go / Toxiproxy</dt>
                <dd>
                  {r
                    ? `${r.revision || "Not recorded"} / ${r.go_version || "Not recorded"} / ${r.toxiproxy_version || "Not recorded"}`
                    : "No report available"}
                </dd>
              </dl>
            </details>
          </>
        )}
      </State>
    </>
  );
}
