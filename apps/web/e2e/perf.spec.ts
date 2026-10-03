import { mkdirSync, writeFileSync } from "node:fs";
import { expect, test as base, type CDPSession, type Page } from "@playwright/test";
import { mockApi, NMI, type Mock } from "./mock.ts";

// What the pages cost while they are open: the work of each stretch of polls,
// the frames the moving dashes get, and how long a click takes to show. The
// API is the mock, at the demo's speed and at the size of the real feeder and
// fleet, on a processor slowed four times.
//
// The numbers go to test-results/perf/results.json and to the terminal. The
// limits here are loose, about three times what was measured: they catch a
// page that has started to do its work again on every poll, not a slow day.
// `just perf` runs this; `just e2e` does not.

const SLOWDOWN = 4;
// A window holds two polls: the pages poll every five seconds at this speed.
const WINDOW_MS = 10_500;
const WINDOWS = 3;

const results: Record<string, Record<string, number>> = {};

const test = base.extend<{ api: Mock; cdp: CDPSession }>({
  api: async ({ page }, use) => {
    const api = await mockApi(page);
    api.speed = 60;
    api.network = "large";
    api.fleet = "large";
    await page.addInitScript(() => localStorage.setItem("doelab.intro", "dismissed"));
    await use(api);
  },
  cdp: async ({ page }, use) => {
    const cdp = await page.context().newCDPSession(page);
    await cdp.send("Performance.enable");
    await cdp.send("Emulation.setCPUThrottlingRate", { rate: SLOWDOWN });
    await use(cdp);
  },
});

test.afterAll(() => {
  mkdirSync("test-results/perf", { recursive: true });
  writeFileSync("test-results/perf/results.json", JSON.stringify(results, null, 2) + "\n");
  for (const [name, values] of Object.entries(results)) {
    console.log(
      `${name.padEnd(28)} ${Object.entries(values)
        .map(([k, v]) => `${k} ${v}`)
        .join("  ")}`,
    );
  }
});

type Work = { script: number; layout: number; style: number; task: number };

// The renderer's running totals, in milliseconds.
async function work(cdp: CDPSession): Promise<Work> {
  const { metrics } = await cdp.send("Performance.getMetrics");
  const ms = (name: string) => 1000 * (metrics.find((m) => m.name === name)?.value ?? 0);
  return {
    script: ms("ScriptDuration"),
    layout: ms("LayoutDuration"),
    style: ms("RecalcStyleDuration"),
    task: ms("TaskDuration"),
  };
}

const median = (values: number[]) =>
  [...values].sort((a, b) => a - b)[Math.floor(values.length / 2)]!;

// What a page costs while it sits open: the median of a few windows, each of
// which holds two polls.
async function idle(page: Page, cdp: CDPSession, api: Mock, name: string, polled: string) {
  const windows: Work[] = [];
  const before = api.calls[polled] ?? 0;
  for (let i = 0; i < WINDOWS; i++) {
    const from = await work(cdp);
    await page.waitForTimeout(WINDOW_MS);
    const to = await work(cdp);
    windows.push({
      script: to.script - from.script,
      layout: to.layout - from.layout,
      style: to.style - from.style,
      task: to.task - from.task,
    });
  }
  const polls = (api.calls[polled] ?? 0) - before;
  const seconds = WINDOW_MS / 1000;
  const perSecond = (pick: (w: Work) => number) =>
    Math.round((10 * median(windows.map(pick))) / seconds) / 10;
  results[name] = {
    "script ms/s": perSecond((w) => w.script),
    "style ms/s": perSecond((w) => w.style),
    "layout ms/s": perSecond((w) => w.layout),
    "task ms/s": perSecond((w) => w.task),
    polls,
  };
  return results[name];
}

// How long the page's main thread was held in one piece, from the load on.
async function watchLongTasks(page: Page) {
  await page.addInitScript(() => {
    const seen = { count: 0, total: 0, longest: 0 };
    (window as unknown as { __long: typeof seen }).__long = seen;
    new PerformanceObserver((list) => {
      for (const entry of list.getEntries()) {
        seen.count++;
        seen.total += entry.duration;
        seen.longest = Math.max(seen.longest, entry.duration);
      }
    }).observe({ type: "longtask", buffered: true });
    // What a click costs, from the press to the next paint.
    const events = { longest: 0 };
    (window as unknown as { __events: typeof events }).__events = events;
    new PerformanceObserver((list) => {
      for (const entry of list.getEntries())
        events.longest = Math.max(events.longest, entry.duration);
    }).observe({ type: "event", durationThreshold: 16, buffered: true } as PerformanceObserverInit);
  });
}

const longTasks = (page: Page) =>
  page.evaluate(() => {
    const seen = (
      window as unknown as { __long: { count: number; total: number; longest: number } }
    ).__long;
    return {
      "long tasks": seen.count,
      "long task ms": Math.round(seen.total),
      "longest ms": Math.round(seen.longest),
    };
  });

const slowestEvent = async (page: Page) => {
  // The entry arrives after the paint that ends it.
  await page.waitForTimeout(400);
  return page.evaluate(() =>
    Math.round((window as unknown as { __events: { longest: number } }).__events.longest),
  );
};

// The frames the page gets in five seconds, and the worst of them.
const frames = (page: Page) =>
  page.evaluate(
    () =>
      new Promise<{ frames: number; "p95 ms": number; "over 34 ms": number }>((resolve) => {
        const gaps: number[] = [];
        const start = performance.now();
        let last = start;
        const tick = (now: number) => {
          gaps.push(now - last);
          last = now;
          if (now - start < 5000) return requestAnimationFrame(tick);
          gaps.sort((a, b) => a - b);
          resolve({
            frames: gaps.length,
            "p95 ms": Math.round(10 * gaps[Math.floor(gaps.length * 0.95)]!) / 10,
            "over 34 ms": gaps.filter((g) => g > 34).length,
          });
        };
        requestAnimationFrame(tick);
      }),
  );

test("the network of a whole feeder: what a poll costs, and what the dashes cost", async ({
  page,
  api,
  cdp,
}) => {
  test.setTimeout(120_000);
  await watchLongTasks(page);
  await page.goto("/network");
  const drawing = page.getByRole("img", { name: /metres of cable away/ });
  await expect(drawing.locator(".bus")).toHaveCount(223);
  await expect(page.getByText("Constrained")).toBeVisible();

  results["network: load"] = await longTasks(page);
  results["network: drawing"] = {
    elements: await drawing.locator("*").count(),
    "moving lines": await drawing.locator(".flow").count(),
    animations: await page.evaluate(() => document.getAnimations().length),
  };
  results["network: frames, dashes moving"] = await frames(page);
  const open = await idle(page, cdp, api, "network: open", "TelemetryService/GetFeederState");
  expect(open.polls).toBeGreaterThanOrEqual(WINDOWS);

  // Choosing a bus: one click, and the panel says what it is.
  await drawing.locator(".bus .hit").nth(120).click();
  await expect(page.getByRole("heading", { level: 2, name: /^Bus B/ })).toBeVisible();
  results["network: choosing a bus"] = { "event ms": await slowestEvent(page) };

  await page.goto("/network?flow=off");
  await expect(drawing.locator(".bus")).toHaveCount(223);
  results["network: frames, dashes stopped"] = await frames(page);
  await idle(page, cdp, api, "network: open, dashes stopped", "TelemetryService/GetFeederState");
});

test("the map of the whole fleet: what a poll costs", async ({ page, api, cdp }) => {
  test.setTimeout(90_000);
  await watchLongTasks(page);
  await page.goto("/?sub=SUB-001");
  const table = page.getByRole("region", { name: "Sites on the map" });
  await expect(table.getByRole("row")).toHaveCount(77);
  await expect(page.getByRole("button", { name: /^Ausgrid Lidcombe Zone:/ })).toBeVisible();
  results["map: load"] = await longTasks(page);
  results["map: page"] = { elements: await page.locator("main *").count() };
  await idle(page, cdp, api, "map: open", "TelemetryService/GetFleetState");
});

test("the feeder's overview: what the stream and the polls cost", async ({ page, api, cdp }) => {
  test.setTimeout(90_000);
  await watchLongTasks(page);
  await page.goto("/feeder");
  await expect(page.locator("figure canvas").first()).toBeVisible();
  results["overview: load"] = await longTasks(page);
  await idle(page, cdp, api, "overview: open", "TelemetryService/GetFeederSeries");
  results["overview: first fetches"] = {
    series: api.calls["TelemetryService/GetFeederSeries"] ?? 0,
  };

  // A chart as a table: every row of it at once.
  await page.locator("figure").first().getByRole("button", { name: "View as table" }).click();
  await expect(page.getByRole("table", { name: "Export: allowed and measured" })).toBeVisible();
  results["overview: chart as a table"] = { "event ms": await slowestEvent(page) };
});

test("one site: what the clock and the polls cost", async ({ page, api, cdp }) => {
  test.setTimeout(90_000);
  await watchLongTasks(page);
  await page.goto(`/sites/${NMI.breaching}`);
  await expect(page.locator("figure canvas").first()).toBeVisible();
  results["site: load"] = await longTasks(page);
  await idle(page, cdp, api, "site: open", "TelemetryService/GetSiteSeries");
});

test("operations: what the clock and the polls cost", async ({ page, api, cdp }) => {
  test.setTimeout(90_000);
  await watchLongTasks(page);
  await page.goto("/operations");
  await expect(page.getByText("Engine runs")).toBeVisible();
  await idle(page, cdp, api, "operations: open", "AlertService/ListAlerts");
});
