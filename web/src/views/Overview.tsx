import { useRemote } from "../api";
import type { Page, Status } from "../types";
import { shortID } from "../format";
import { PhaseBars } from "../charts";
import {
  Icon,
  Badge,
  State,
  Empty,
  PageTitle,
  HistoryTable,
} from "../components";
export default function Overview() {
  const result = useRemote<Page>("/api/v1/runs?limit=6");
  const p = result.data,
    latest = p?.items[0],
    total = p ? Object.values(p.counts).reduce((a, b) => a + b, 0) : 0;
  return (
    <>
      <PageTitle
        eyebrow="Observe / understand / improve"
        title="Reliability, under pressure."
      >
        <button
          className="button secondary"
          onClick={() => window.location.reload()}
        >
          ↻ Refresh history
        </button>
      </PageTitle>
      <p className="lead">
        Controlled failures. Measured impact. Evidence you can inspect.
      </p>
      <State {...result}>
        {p &&
          (total === 0 ? (
            <Empty />
          ) : (
            <>
              <div className="count-strip" aria-label="Recorded run counts">
                {(
                  ["complete", "failed", "canceled", "running"] as Status[]
                ).map((status) => (
                  <a href={`#/history?status=${status}`} key={status}>
                    <span className={`status-dot ${status}`} />
                    <div>
                      <span>
                        {status === "running"
                          ? "Unfinished intents"
                          : status === "complete"
                            ? "Completed runs"
                            : `${status[0].toUpperCase() + status.slice(1)} runs`}
                      </span>
                      <strong>{p.counts[status]}</strong>
                    </div>
                    <small>
                      {status === "running"
                        ? "Outcome not inferred"
                        : "Recorded status"}
                    </small>
                  </a>
                ))}
              </div>
              {latest && (
                <div className="overview-grid">
                  <section className="panel impact-panel">
                    <div className="panel-heading">
                      <div>
                        <span className="eyebrow">
                          Latest recorded experiment
                        </span>
                        <h2>What changed under fault?</h2>
                      </div>
                      <Badge status={latest.status} />
                    </div>
                    <div className="run-caption">
                      <a href={`#/runs/${latest.id}`} className="mono">
                        {shortID(latest.run_id ?? latest.id)} ↗
                      </a>
                      <span>{latest.spec.latency_ms} ms injected latency</span>
                      <span>
                        {latest.spec.samples_per_phase} samples / phase
                      </span>
                    </div>
                    {latest.phases ? (
                      <PhaseBars summaries={latest.phases} />
                    ) : (
                      <p className="notice">
                        Only the intent was committed. No final report is
                        available; this does not establish a failure cause.
                      </p>
                    )}
                    <div className="panel-foot">
                      <span>One workload, three measured phases.</span>
                      <a href={`#/runs/${latest.id}`}>
                        Inspect evidence <Icon name="arrow" />
                      </a>
                    </div>
                  </section>
                  <aside className="context-panel">
                    <div className="eyebrow">The experiment model</div>
                    <h2>
                      Break the connection.
                      <br />
                      Keep the evidence.
                    </h2>
                    <div className="topology">
                      <span>Frontend</span>
                      <b>→</b>
                      <span className="toxic">Toxiproxy</span>
                      <b>→</b>
                      <span>Dependency</span>
                    </div>
                    <p>
                      Faultline measures a fixed local service topology before,
                      during and after controlled network latency.
                    </p>
                    <div className="context-note">
                      <i />
                      Historical results only
                      <br />
                      <small>No active fault or collector is required.</small>
                    </div>
                  </aside>
                </div>
              )}
              <section className="panel">
                <div className="panel-heading">
                  <div>
                    <span className="eyebrow">Persisted evidence</span>
                    <h2>Recent experiments</h2>
                  </div>
                  <a className="subtle-link" href="#/history">
                    View all history →
                  </a>
                </div>
                <HistoryTable items={p.items} />
              </section>
              <div className="footnote">
                Counts reflect recorded SQLite statuses. Unfinished intents may
                still be running or may have stopped before finalization.
              </div>
            </>
          ))}
      </State>
    </>
  );
}
