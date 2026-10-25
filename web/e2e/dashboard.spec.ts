import { test, expect } from "@playwright/test";
import { ms, deltaMS, deltaPP } from "../src/format";
import type { Page, Entry, Comparison } from "../src/types";

test("actual persisted evidence: overview, filtering, raw traces, comparison and responsive navigation", async ({
  page,
  request,
  baseURL,
}) => {
  const response = await request.get("/api/v1/runs?limit=50");
  expect(response.ok()).toBeTruthy();
  const data: Page = (await response.json()).data;
  expect(
    data.items.length,
    "Generate at least two real persisted experiments before browser validation",
  ).toBeGreaterThanOrEqual(2);
  const left = data.items.find(
    (r) => r.status === "complete" && r.spec.latency_ms === 80,
  )!;
  const right = data.items.find(
    (r) => r.status === "complete" && r.spec.latency_ms === 400,
  )!;
  expect(left).toBeTruthy();
  expect(right).toBeTruthy();
  const entry: Entry = (
    await (await request.get(`/api/v1/runs/${right.id}`)).json()
  ).data;
  const comparison: Comparison = (
    await (
      await request.get(`/api/v1/compare?left=${left.id}&right=${right.id}`)
    ).json()
  ).data;
  const page1: Page = (await (await request.get("/api/v1/runs?limit=1")).json())
    .data;
  const page2: Page = (
    await (
      await request.get(`/api/v1/runs?limit=1&cursor=${page1.next_cursor}`)
    ).json()
  ).data;
  expect(page1.items[0].id).not.toBe(page2.items[0].id);
  const errors: string[] = [],
    external: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  page.on("console", (message) => {
    if (message.type() === "error") errors.push(message.text());
  });
  page.on("request", (r) => {
    if (!r.url().startsWith(baseURL!)) external.push(r.url());
  });
  await page.setViewportSize({ width: 1440, height: 1050 });
  await page.goto("/");
  await expect(
    page.getByRole("heading", { name: "Reliability, under pressure." }),
  ).toBeVisible();
  await expect(page.getByLabel("Recorded run counts")).toContainText(
    String(data.counts.complete),
  );
  await expect(page.locator("tbody tr")).toHaveCount(
    Math.min(6, data.items.length),
  );
  await page.screenshot({
    path: "../artifacts/m4-overview.png",
    fullPage: true,
  });
  await page
    .getByRole("link", { name: "Experiment history", exact: true })
    .click();
  await page
    .getByRole("textbox", { name: "Search run IDs or configuration JSON" })
    .fill(right.id);
  await expect(page.getByText("1 matching runs · page 1")).toBeVisible();
  await expect(page.locator("tbody tr")).toHaveCount(1);
  await page.locator("tbody tr a.run-link").click();
  await expect(
    page.getByRole("heading", { name: "Baseline → fault → recovery" }),
  ).toBeVisible();
  const first = entry.report!.fault.samples![0];
  await page
    .getByRole("button", {
      name: `fault request 1: ${ms(first.duration_ms)}, HTTP ${first.status}`,
    })
    .focus();
  await page.keyboard.press("Enter");
  if (first.trace_id)
    await expect(page.getByText(first.trace_id, { exact: true })).toBeVisible();
  await expect(
    page.getByText("Temporary Jaeger traces disappear at teardown;", {
      exact: false,
    }),
  ).toBeVisible();
  await page.screenshot({ path: "../artifacts/m4-detail.png", fullPage: true });
  // Disabled tracing is real historical evidence too, not a fixture response.
  const disabled: Entry = (
    await (await request.get(`/api/v1/runs/${left.id}`)).json()
  ).data;
  await page.goto(`/#/runs/${left.id}`);
  const untraced = disabled.report!.fault.samples![0];
  expect(untraced.trace_id).toBeUndefined();
  await page
    .getByRole("button", {
      name: `fault request 1: ${ms(untraced.duration_ms)}, HTTP ${untraced.status}`,
    })
    .focus();
  await page.keyboard.press("Enter");
  await expect(
    page.getByText("No trace reference recorded", { exact: true }),
  ).toBeVisible();
  await page.getByRole("link", { name: "Compare runs", exact: true }).click();
  await page.getByLabel("Left · reference run").fill(left.id);
  await page.getByLabel("Right · comparison run").fill(right.id);
  await page.getByRole("button", { name: "Compare runs", exact: true }).click();
  await expect(
    page.getByText("Different configurations — interpret with care"),
  ).toBeVisible();
  const fault = comparison.phases.find((p) => p.phase === "fault")!;
  await expect(
    page.getByText(deltaMS(fault.right_minus_left.mean_delta_ms), {
      exact: true,
    }),
  ).toBeVisible();
  await expect(
    page.getByText(deltaPP(fault.right_minus_left.failure_rate_delta), {
      exact: true,
    }),
  ).toBeVisible();
  await page.screenshot({
    path: "../artifacts/m4-comparison.png",
    fullPage: true,
  });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.getByRole("link", { name: "Overview", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Reliability, under pressure." }),
  ).toBeVisible();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
  await page.screenshot({ path: "../artifacts/m4-mobile.png", fullPage: true });
  await page.goto(`/#/runs/${right.id}`);
  await expect(
    page.getByRole("heading", { name: "Baseline → fault → recovery" }),
  ).toBeVisible();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
  await page.screenshot({
    path: "../artifacts/m4-mobile-detail.png",
    fullPage: true,
  });
  await page.keyboard.press("ControlOrMeta+Home");
  await page.goto("/");
  await page.keyboard.press("Tab");
  await expect(
    page.getByRole("link", { name: "Skip to content" }),
  ).toBeFocused();
  await page.keyboard.press("Enter");
  await expect(page.locator("main")).toBeFocused();
  expect(errors).toEqual([]);
  expect(external).toEqual([]);
});
