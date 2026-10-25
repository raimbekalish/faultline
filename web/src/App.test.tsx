import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { test, expect, vi } from "vitest";
import userEvent from "@testing-library/user-event";
import App from "./App";
import Detail from "./views/Detail";
import type { Entry, Page, Report, Summary } from "./types";
const summary: Summary = {
  requests: 2,
  failures: 0,
  failure_rate: 0,
  min_ms: 1,
  mean_ms: 3,
  p50_ms: 1,
  p95_ms: 5,
  p99_ms: 5,
  max_ms: 5,
};
const entry: Entry = {
  id: `history-${"a".repeat(32)}`,
  run_id: `faultline-${"b".repeat(32)}`,
  created_at: "2026-10-24T12:00:00Z",
  status: "complete",
  spec: {
    schema_version: 1,
    samples_per_phase: 2,
    latency_ms: 80,
    request_timeout_ms: 1000,
  },
  spec_hash: "example",
};
const report: Report = {
  schema_version: 1,
  run_id: entry.run_id!,
  status: "canceled",
  started_at: entry.created_at,
  finished_at: entry.created_at,
  cleanup_complete: true,
  config: { samples_per_phase: 2, latency_ms: 80, request_timeout_ms: 1000 },
  revision: "test",
  go_version: "test",
  toxiproxy_version: "test",
  error: "context canceled",
  baseline: {
    summary,
    samples: [
      { duration_ms: 1, status: 200 },
      { duration_ms: 5, status: 200 },
    ],
  },
  fault: {
    summary: { ...summary, failures: 1, failure_rate: 0.5 },
    samples: [
      { duration_ms: 1, status: 200 },
      {
        duration_ms: 5,
        status: 0,
        error: "context canceled",
        trace_id: "c".repeat(32),
        span_id: "d".repeat(16),
      },
    ],
  },
  recovery: {
    summary: {
      requests: 0,
      failures: 0,
      failure_rate: 0,
      min_ms: 0,
      mean_ms: 0,
      p50_ms: 0,
      p95_ms: 0,
      p99_ms: 0,
      max_ms: 0,
    },
    samples: [],
  },
};
function mock(data: unknown, ok = true) {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue({
      ok,
      json: async () => ({
        api_version: 1,
        ...(ok
          ? { data }
          : {
              error: { message: "History contains an inconsistent record." },
            }),
      }),
    }),
  );
}
function page(items: Entry[] = []): Page {
  return {
    items,
    counts: { complete: items.length, failed: 0, canceled: 0, running: 0 },
    total: items.length,
    next_cursor: null,
  };
}
test("overview has a genuine empty state without invented metrics", async () => {
  window.location.hash = "#/";
  mock(page());
  render(<App />);
  expect(
    await screen.findByText("Your next insight starts with an experiment."),
  ).toBeInTheDocument();
  expect(
    screen.queryByLabelText("Recorded run counts"),
  ).not.toBeInTheDocument();
});
test("loading and corrupt-history states are explicit", async () => {
  vi.stubGlobal("fetch", () => new Promise(() => {}));
  const view = render(<Detail id={entry.id} />);
  expect(screen.getByRole("status")).toHaveTextContent("Reading local history");
  view.unmount();
  mock(null, false);
  render(<Detail id={entry.id} />);
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "inconsistent record",
  );
});
test("unfinished intents have no fabricated report or chart", async () => {
  mock({ ...entry, status: "running" });
  render(<Detail id={entry.id} />);
  expect(
    await screen.findByText("Intent recorded. Outcome unknown."),
  ).toBeInTheDocument();
  expect(
    screen.queryByRole("group", { name: /Raw request/ }),
  ).not.toBeInTheDocument();
  expect(
    screen.getByText("Missing measurements are unavailable, not zero."),
  ).toBeInTheDocument();
});
test("canceled partial evidence exposes keyboard-accessible raw timings and historical trace references", async () => {
  mock({ ...entry, status: "canceled", report });
  render(<Detail id={entry.id} />);
  expect(
    await screen.findByText("Canceled run · partial evidence"),
  ).toBeInTheDocument();
  const point = screen.getByRole("button", {
    name: "fault request 2: 5 ms, HTTP unavailable",
  });
  point.focus();
  fireEvent.keyDown(point, { key: "Enter" });
  expect(await screen.findByText("c".repeat(32))).toBeInTheDocument();
  expect(screen.getByText("d".repeat(16))).toBeInTheDocument();
  expect(
    screen.getByText(/Temporary Jaeger traces disappear/),
  ).toBeInTheDocument();
  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: /recovery 0/ }));
  expect(
    screen.getByText("No observations recorded in this phase."),
  ).toBeInTheDocument();
});
test("history filters are sent to the server and matching counts are rendered", async () => {
  window.location.hash = "#/history";
  mock(page([entry]));
  render(<App />);
  await screen.findByText("1 matching runs · page 1");
  await userEvent.selectOptions(screen.getByRole("combobox"), "canceled");
  await waitFor(() =>
    expect(
      vi
        .mocked(fetch)
        .mock.calls.some(([url]) => String(url).includes("status=canceled")),
    ).toBe(true),
  );
  expect(screen.getByRole("button", { name: "Next →" })).toBeDisabled();
});

test("history pagination follows server cursors and supports returning to the previous page", async () => {
  window.location.hash = "#/history";
  const second = {
    ...entry,
    id: `history-${"e".repeat(32)}`,
    run_id: `faultline-${"f".repeat(32)}`,
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => ({
      ok: true,
      json: async () => ({
        api_version: 1,
        data: url.includes("cursor=history-")
          ? { ...page([second]), total: 2 }
          : { ...page([entry]), total: 2, next_cursor: entry.id },
      }),
    })),
  );
  render(<App />);
  await screen.findByText("2 matching runs · page 1");
  await userEvent.click(screen.getByRole("button", { name: "Next →" }));
  await screen.findByText("2 matching runs · page 2");
  expect(screen.getByRole("link", { name: /ffffffffff/ })).toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "← Previous" }));
  await screen.findByText("2 matching runs · page 1");
  expect(screen.getByRole("link", { name: /bbbbbbbbbb/ })).toBeInTheDocument();
});

test("missing trace references are not replaced by invented telemetry", async () => {
  mock({ ...entry, status: "canceled", report });
  render(<Detail id={entry.id} />);
  await screen.findByText("Canceled run · partial evidence");
  await userEvent.click(screen.getByRole("button", { name: /baseline 2/ }));
  await userEvent.click(screen.getByRole("button", { name: "#01" }));
  expect(screen.getByText("No trace reference recorded")).toBeInTheDocument();
});
