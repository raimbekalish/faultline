import { useEffect, useState } from "react";
import { useRemote } from "../api";
import type { Comparison, Page } from "../types";
import { date, deltaMS, deltaPP, ms, percent, shortID } from "../format";
import { State, PageTitle, Icon } from "../components";
export default function CompareView({
  initialLeft,
  initialRight,
}: {
  initialLeft: string;
  initialRight: string;
}) {
  const runs = useRemote<Page>("/api/v1/runs?limit=50&status=complete"),
    [left, setLeft] = useState(initialLeft),
    [right, setRight] = useState(initialRight),
    [pair, setPair] = useState<[string, string] | null>(
      initialLeft && initialRight ? [initialLeft, initialRight] : null,
    );
  useEffect(() => {
    if (runs.data) {
      setLeft((v) => v || runs.data!.items[0]?.id || "");
      setRight(
        (v) =>
          v || runs.data!.items.find((r) => r.id !== initialLeft)?.id || "",
      );
    }
  }, [runs.data, initialLeft]);
  const result = useRemote<Comparison>(
      pair
        ? `/api/v1/compare?${new URLSearchParams({ left: pair[0], right: pair[1] })}`
        : null,
    ),
    c = result.data;
  return (
    <>
      <PageTitle eyebrow="Cross-run analysis" title="Compare the evidence." />
      <p className="lead">
        Descriptive differences between two completed, successfully cleaned
        runs.
      </p>
      <section className="panel">
        <form
          className="comparison-form"
          onSubmit={(e) => {
            e.preventDefault();
            setPair([left.trim(), right.trim()]);
          }}
        >
          <label>
            Left · reference run
            <input
              list="complete-runs"
              value={left}
              onChange={(e) => setLeft(e.target.value)}
              placeholder="Select or paste a run ID"
              required
            />
          </label>
          <span className="comparison-arrow">→</span>
          <label>
            Right · comparison run
            <input
              list="complete-runs"
              value={right}
              onChange={(e) => setRight(e.target.value)}
              placeholder="Select or paste a run ID"
              required
            />
          </label>
          <button
            className="button primary"
            type="submit"
            disabled={!left || !right}
          >
            Compare runs
          </button>
          <datalist id="complete-runs">
            {runs.data?.items.map((r) => (
              <option value={r.id} key={r.id}>
                {shortID(r.run_id ?? r.id)} · {r.spec.latency_ms} ms ·{" "}
                {date(r.created_at)}
              </option>
            ))}
          </datalist>
        </form>
        <div className="form-note">
          Suggestions show the latest 50 completed runs. Paste any historical
          history/engine run ID.
        </div>
        {runs.error && (
          <p role="alert" className="notice">
            {runs.error}
          </p>
        )}
      </section>
      <State {...result}>
        {c ? (
          <>
            <div className={`notice ${c.same_spec ? "neutral" : ""}`}>
              <strong>
                {c.same_spec
                  ? "Matching configuration snapshots"
                  : "Different configurations — interpret with care"}
              </strong>
              <p>
                {c.same_spec
                  ? "Matching specs do not establish identical environments or statistical significance."
                  : "Observed differences may reflect configuration, environment or sampling variation."}
              </p>
              <div className="config-compare">
                <span>
                  LEFT: {c.left_spec.samples_per_phase} samples ·{" "}
                  {c.left_spec.latency_ms} ms latency ·{" "}
                  {c.left_spec.request_timeout_ms} ms timeout
                </span>
                <span>
                  RIGHT: {c.right_spec.samples_per_phase} samples ·{" "}
                  {c.right_spec.latency_ms} ms latency ·{" "}
                  {c.right_spec.request_timeout_ms} ms timeout
                </span>
              </div>
            </div>
            <section className="panel">
              <div className="panel-heading">
                <div>
                  <span className="eyebrow">Right minus left</span>
                  <h2>Impact, phase by phase</h2>
                </div>
                <span className="muted">
                  ms = milliseconds · pp = percentage points
                </span>
              </div>
              <p className="form-note">
                Bars begin at zero. Duration scales are shared within each
                metric and phase; failure-rate scales are 0–100%.
              </p>
              <div className="comparison-cards">
                {c.phases.map((p) => (
                  <article key={p.phase}>
                    <h3 className="phase-label">
                      <i className={p.phase} />
                      {p.phase}
                    </h3>
                    <div className="delta-primary">
                      <span>Mean delta</span>
                      <strong>
                        {deltaMS(p.right_minus_left.mean_delta_ms)}
                      </strong>
                    </div>
                    {(
                      [
                        { key: "mean_ms", label: "Mean latency", format: ms },
                        { key: "p95_ms", label: "p95 latency", format: ms },
                        {
                          key: "failure_rate",
                          label: "Failure rate",
                          format: percent,
                        },
                      ] as const
                    ).map((metric) => (
                      <div className="comparison-bars" key={metric.key}>
                        <h4>{metric.label}</h4>
                        {(["left", "right"] as const).map((side) => (
                          <div key={side}>
                            <span>{side}</span>
                            <meter
                              min={0}
                              max={
                                metric.key === "failure_rate"
                                  ? 1
                                  : Math.max(
                                      1,
                                      p.left[metric.key],
                                      p.right[metric.key],
                                    )
                              }
                              value={p[side][metric.key]}
                              aria-label={`${p.phase} ${side} ${metric.label}`}
                            />
                            <b>{metric.format(p[side][metric.key])}</b>
                          </div>
                        ))}
                      </div>
                    ))}
                    <dl>
                      <dt>p95 delta</dt>
                      <dd>{deltaMS(p.right_minus_left.p95_delta_ms)}</dd>
                      <dt>Failure-rate delta</dt>
                      <dd>{deltaPP(p.right_minus_left.failure_rate_delta)}</dd>
                      <dt>Left / right p95</dt>
                      <dd>
                        {ms(p.left.p95_ms)} / {ms(p.right.p95_ms)}
                      </dd>
                      <dt>Left / right failures</dt>
                      <dd>
                        {p.left.failures}/{p.left.requests} / {p.right.failures}
                        /{p.right.requests}
                      </dd>
                    </dl>
                  </article>
                ))}
              </div>
              <div className="panel-foot">
                <a href={`#/runs/${c.left_id}`}>Inspect left report →</a>
                <a href={`#/runs/${c.right_id}`}>Inspect right report →</a>
              </div>
            </section>
          </>
        ) : (
          !result.loading &&
          !result.error && (
            <div className="state">
              <Icon name="compare" />
              <h2>Two runs. One clearer picture.</h2>
              <p>
                Select completed runs above to compare their actual
                measurements.
              </p>
              {runs.data?.total === 0 && (
                <p>No completed runs are available yet.</p>
              )}
            </div>
          )
        )}
      </State>
    </>
  );
}
