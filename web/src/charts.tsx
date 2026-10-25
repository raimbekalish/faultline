import { useState } from "react";
import type { PhaseName, Report, Sample, Summary } from "./types";
import { phases } from "./types";
import { ms, numeric } from "./format";

export function Legend() {
  return (
    <div className="legend">
      {phases.map((p) => (
        <span key={p}>
          <i className={p} />
          {p}
        </span>
      ))}
      <span>
        <i className="failed-key" />
        Request failure
      </span>
    </div>
  );
}
export function PhaseBars({
  summaries,
}: {
  summaries: Partial<Record<PhaseName, Summary>>;
}) {
  const max = Math.max(
    1,
    ...phases.map((p) => (summaries[p]?.requests ? summaries[p]!.mean_ms : 0)),
  );
  return (
    <div
      className="phase-bars"
      aria-label="Phase mean latencies, shared zero baseline"
    >
      {phases.map((p) => {
        const s = summaries[p];
        return (
          <div className="bar-row" key={p}>
            <span className="phase-label">
              <i className={p} />
              {p}
            </span>
            <svg
              className="bar-track"
              viewBox="0 0 1000 12"
              preserveAspectRatio="none"
              aria-hidden="true"
            >
              <rect
                className={`bar ${p}`}
                width={s?.requests ? (s.mean_ms / max) * 1000 : 0}
                height={12}
                rx={3}
              />
            </svg>
            <strong>{s?.requests ? ms(s.mean_ms) : "No samples"}</strong>
          </div>
        );
      })}
      <div className="axis-note">
        Mean request duration · shared scale starts at 0 ms
      </div>
    </div>
  );
}
export interface Point {
  phase: PhaseName;
  index: number;
  sample: Sample;
}
export function ObservationChart({
  report,
  onSelect,
}: {
  report: Report;
  onSelect?: (p: Point) => void;
}) {
  const [active, setActive] = useState<Point | null>(null);
  const points: Point[] = phases.flatMap((phase) =>
    (report[phase].samples ?? []).map((sample, index) => ({
      phase,
      sample,
      index,
    })),
  );
  if (!points.length)
    return (
      <div className="empty-chart">No request observations were recorded.</div>
    );
  const max = Math.max(1, ...points.map((p) => p.sample.duration_ms)) * 1.12,
    width = 920,
    height = 230,
    left = 54,
    right = 16,
    top = 18,
    bottom = 38;
  const x = (i: number) =>
    left + ((i + 0.5) / points.length) * (width - left - right);
  const y = (v: number) =>
    height - bottom - (v / max) * (height - top - bottom);
  return (
    <>
      <div className="plot-wrap">
        <svg
          className="observation-chart"
          viewBox={`0 0 ${width} ${height}`}
          role="group"
          aria-label="Raw request durations in observation order; vertical axis begins at zero"
        >
          {[0, 1, 2, 3].map((i) => {
            const value = (max * i) / 3;
            return (
              <g key={i}>
                <line
                  className="grid-line"
                  x1={left}
                  x2={width - right}
                  y1={y(value)}
                  y2={y(value)}
                />
                <text x={left - 10} y={y(value) + 4} textAnchor="end">
                  {numeric(value)}
                </text>
              </g>
            );
          })}
          {phases.map((phase) => {
            const indices = points
              .map((p, i) => (p.phase === phase ? i : -1))
              .filter((i) => i >= 0);
            if (!indices.length) return null;
            const start = indices[0],
              end = indices[indices.length - 1];
            return (
              <g key={phase}>
                <rect
                  className={`region ${phase}`}
                  x={x(start) - (width - left - right) / points.length / 2}
                  y={top}
                  width={
                    ((end - start + 1) / points.length) * (width - left - right)
                  }
                  height={height - top - bottom}
                />
                <polyline
                  className={`plot-line ${phase}`}
                  points={indices
                    .map((i) => `${x(i)},${y(points[i].sample.duration_ms)}`)
                    .join(" ")}
                />
                <text
                  x={(x(start) + x(end)) / 2}
                  y={height - 12}
                  textAnchor="middle"
                >
                  {phase} · {indices.length}
                </text>
              </g>
            );
          })}
          {points.map((p, i) => (
            <circle
              className={`plot-point ${p.phase} ${p.sample.error || p.sample.status < 200 || p.sample.status >= 300 ? "failed-point" : ""}`}
              key={`${p.phase}-${p.index}`}
              cx={x(i)}
              cy={y(p.sample.duration_ms)}
              r={
                active?.phase === p.phase && active.index === p.index ? 5 : 3.3
              }
              tabIndex={0}
              role="button"
              aria-label={`${p.phase} request ${p.index + 1}: ${ms(p.sample.duration_ms)}, HTTP ${p.sample.status || "unavailable"}`}
              onMouseEnter={() => setActive(p)}
              onFocus={() => setActive(p)}
              onClick={() => {
                setActive(p);
                onSelect?.(p);
              }}
              onKeyDown={(e) => {
                if (e.key === "Enter" || e.key === " ") {
                  e.preventDefault();
                  onSelect?.(p);
                }
              }}
            >
              <title>{`${p.phase} #${p.index + 1} · ${ms(p.sample.duration_ms)} · ${p.sample.error || `HTTP ${p.sample.status}`}`}</title>
            </circle>
          ))}
        </svg>
      </div>
      <div className="plot-caption">
        <span>Duration (ms) · serial request order</span>
        <span role="status">
          {active
            ? `${active.phase} #${active.index + 1} · ${ms(active.sample.duration_ms)} · ${active.sample.status ? `HTTP ${active.sample.status}` : "No HTTP response"}`
            : "Focus or hover a point to inspect; select for raw evidence."}
        </span>
      </div>
    </>
  );
}
