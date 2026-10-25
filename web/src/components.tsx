import type { ReactNode } from "react";
import type { Run, Status, Summary } from "./types";
import { date, ms, percent, shortID } from "./format";
export function Icon({ name }: { name: string }) {
  const paths: Record<string, ReactNode> = {
    overview: (
      <>
        <rect x="3" y="3" width="7" height="7" rx="1" />
        <rect x="14" y="3" width="7" height="7" rx="1" />
        <rect x="3" y="14" width="7" height="7" rx="1" />
        <rect x="14" y="14" width="7" height="7" rx="1" />
      </>
    ),
    history: (
      <>
        <path d="M4 5h16M4 12h16M4 19h16" />
        <path d="M8 5v14" />
      </>
    ),
    compare: (
      <>
        <path d="M7 3v18M17 3v18M3 7h8M13 17h8" />
        <circle cx="7" cy="14" r="3" />
        <circle cx="17" cy="9" r="3" />
      </>
    ),
    arrow: <path d="M5 12h14m-6-6 6 6-6 6" />,
    pulse: <path d="M2 12h5l3-8 4 16 3-8h5" />,
  };
  return (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      aria-hidden="true"
    >
      {paths[name] ?? paths.pulse}
    </svg>
  );
}
export function Badge({ status }: { status: Status }) {
  return (
    <span className={`badge ${status}`}>
      <i />
      {status === "running"
        ? "Unfinished"
        : status === "complete"
          ? "Completed"
          : status[0].toUpperCase() + status.slice(1)}
    </span>
  );
}
export function State({
  loading,
  error,
  children,
}: {
  loading: boolean;
  error?: string;
  children?: ReactNode;
}) {
  if (loading)
    return (
      <div className="state" role="status">
        <span className="loader" />
        Reading local history…
      </div>
    );
  if (error)
    return (
      <div className="state error-state" role="alert">
        <strong>History unavailable</strong>
        <p>{error}</p>
        <button onClick={() => window.location.reload()}>Retry</button>
      </div>
    );
  return <>{children}</>;
}
export function Empty() {
  return (
    <div className="state empty-state">
      <Icon name="pulse" />
      <h2>Your next insight starts with an experiment.</h2>
      <p>Run a local experiment in your terminal, then refresh this page.</p>
      <code>./scripts/experiment.sh run -spec examples/latency.json</code>
      <p>The dashboard reads history; it never injects faults.</p>
    </div>
  );
}
export function PageTitle({
  eyebrow,
  title,
  children,
}: {
  eyebrow: string;
  title: string;
  children?: ReactNode;
}) {
  return (
    <div className="page-title">
      <div>
        <div className="eyebrow">{eyebrow}</div>
        <h1>{title}</h1>
      </div>
      {children}
    </div>
  );
}
function Metrics({ summary }: { summary?: Summary }) {
  return (
    <>
      {summary?.requests ? (
        <>
          <strong>{ms(summary.mean_ms)}</strong>
          <small>
            {percent(summary.failure_rate)} failed · {summary.requests} requests
          </small>
        </>
      ) : (
        <span className="muted">No observations</span>
      )}
    </>
  );
}
export function HistoryTable({ items }: { items: Run[] }) {
  return (
    <div className="table-scroll">
      <table className="history-table">
        <thead>
          <tr>
            <th>Experiment</th>
            <th>Status</th>
            <th>Fault configuration</th>
            <th>Baseline mean</th>
            <th>Fault mean</th>
            <th>Recorded locally</th>
          </tr>
        </thead>
        <tbody>
          {items.map((r) => (
            <tr key={r.id}>
              <td>
                <a className="run-link mono" href={`#/runs/${r.id}`}>
                  {shortID(r.run_id ?? r.id)}
                  <span>↗</span>
                </a>
                <small>{r.spec.samples_per_phase} samples / phase</small>
              </td>
              <td>
                <Badge status={r.status} />
              </td>
              <td>
                <strong>{r.spec.latency_ms} ms latency</strong>
                <small>{r.spec.request_timeout_ms} ms request timeout</small>
              </td>
              <td>
                <Metrics summary={r.phases?.baseline} />
              </td>
              <td>
                <Metrics summary={r.phases?.fault} />
              </td>
              <td>
                <span className="date">{date(r.created_at)}</span>
                <small>
                  {r.cleanup_complete === false
                    ? "Cleanup not confirmed"
                    : r.status === "running"
                      ? "Outcome unknown"
                      : "Final report recorded"}
                </small>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
