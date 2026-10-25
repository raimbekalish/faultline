import { useEffect, useState } from "react";
import { useRemote } from "../api";
import type { Page } from "../types";
import { PageTitle, State, HistoryTable } from "../components";
export default function HistoryView({
  initialStatus,
}: {
  initialStatus: string;
}) {
  const [status, setStatus] = useState(initialStatus),
    [query, setQuery] = useState(""),
    [search, setSearch] = useState(""),
    [cursors, setCursors] = useState<string[]>([]);
  useEffect(() => {
    const timer = setTimeout(() => {
      setSearch(query);
      setCursors([]);
    }, 250);
    return () => clearTimeout(timer);
  }, [query]);
  const params = new URLSearchParams({
      limit: "10",
      status,
      q: search,
      cursor: cursors.at(-1) ?? "",
    }),
    result = useRemote<Page>(`/api/v1/runs?${params}`);
  return (
    <>
      <PageTitle eyebrow="Experiment library" title="History with context." />
      <p className="lead">
        Every persisted run, including the ones that didn’t finish.
      </p>
      <section className="panel">
        <div className="filter-bar">
          <label className="search-box">
            <span aria-hidden="true">⌕</span>
            <input
              aria-label="Search run IDs or configuration JSON"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Search run IDs or configuration…"
              maxLength={128}
            />
          </label>
          <label className="select-label">
            Status{" "}
            <select
              value={status}
              onChange={(e) => {
                setStatus(e.target.value);
                setCursors([]);
              }}
            >
              <option value="">All statuses</option>
              <option value="complete">Completed</option>
              <option value="failed">Failed</option>
              <option value="canceled">Canceled</option>
              <option value="running">Unfinished</option>
            </select>
          </label>
        </div>
        <State {...result}>
          {result.data && (
            <>
              {result.data.items.length ? (
                <HistoryTable items={result.data.items} />
              ) : (
                <div className="state">
                  <h2>No matching experiments</h2>
                  <p>Try a different ID, configuration value or status.</p>
                </div>
              )}
              <div className="pagination">
                <span>
                  {result.data.total} matching runs · page {cursors.length + 1}
                </span>
                <div>
                  <button
                    disabled={!cursors.length}
                    onClick={() => setCursors(cursors.slice(0, -1))}
                  >
                    ← Previous
                  </button>
                  <button
                    disabled={!result.data.next_cursor}
                    onClick={() => {
                      if (result.data?.next_cursor)
                        setCursors([...cursors, result.data.next_cursor]);
                    }}
                  >
                    Next →
                  </button>
                </div>
              </div>
            </>
          )}
        </State>
      </section>
      <p className="footnote">
        Search matches IDs and canonical configuration JSON. An unfinished
        intent has no recorded final outcome.
      </p>
    </>
  );
}
