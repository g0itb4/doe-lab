import AxeBuilder from "@axe-core/playwright";
import { expect, test as base, type Page } from "@playwright/test";
import { MAP, mockApi, NMI, OPERATOR_TOKEN, type Mock } from "./mock.ts";

// Every test gets a page whose API is the mock, and fails if the page logged
// an error.
const test = base.extend<{ api: Mock; errors: string[] }>({
  errors: async ({ page }, use) => {
    const errors: string[] = [];
    page.on("console", (m) => {
      // A request that the test made fail on purpose is not the page's error.
      if (m.type() === "error" && !m.text().startsWith("Failed to load resource"))
        errors.push(m.text());
    });
    page.on("pageerror", (e) => errors.push(String(e)));
    await use(errors);
  },
  api: async ({ page }, use) => {
    const api = await mockApi(page);
    // The intro panel is for a first visit; these are not.
    await page.addInitScript(() => {
      if (!sessionStorage.getItem("e2e.first-visit"))
        localStorage.setItem("doelab.intro", "dismissed");
    });
    await use(api);
  },
});

// The pages, and the heading and the content that say each has loaded.
const PAGES = [
  { path: "/", h1: "Fleet", nav: "Fleet", ready: "Ausgrid Lidcombe Zone" },
  {
    path: "/feeder",
    h1: "Feeder overview",
    nav: "Feeder",
    ready: "Export: allowed and measured",
  },
  { path: "/network", h1: "Network: LV10", nav: "Network", ready: "Closest to a limit" },
  { path: "/sites", h1: "Sites", nav: "Sites", ready: NMI.enrolled },
  {
    path: `/sites/${NMI.breaching}`,
    h1: `Site ${NMI.breaching}`,
    nav: "Sites",
    ready: "Envelope, forecast and telemetry",
  },
  { path: "/operations", h1: "Operations", nav: "Operations", ready: "Engine runs" },
  { path: "/config", h1: "Envelope config", nav: "Config", ready: "Version history" },
];

// A chart is drawn once its library has loaded and the reader has come near
// it. On a phone the feeder's first chart is below the fold, and how far
// depends on the fonts the machine has: go to it as a reader would, and back.
async function chartDrawn(page: Page) {
  const figure = page.locator("figure").first();
  await figure.scrollIntoViewIfNeeded();
  await expect(figure.locator("canvas")).toBeVisible();
  await page.evaluate(() => window.scrollTo(0, 0));
}

async function open(page: Page, path: string, ready: string) {
  await page.goto(path);
  await expect(page.getByText(ready).first()).toBeVisible();
  if (path.startsWith("/feeder") || path.startsWith("/sites/")) await chartDrawn(page);
  // The network is drawn once its state has arrived.
  if (path.startsWith("/network")) await expect(page.getByText("Constrained")).toBeVisible();
  // The map of the fleet's page is drawn after its library has loaded.
  if (path === "/" || path.startsWith("/?"))
    await expect(page.getByRole("button", { name: /^Ausgrid Lidcombe Zone:/ })).toBeVisible();
}

for (const p of PAGES) {
  test.describe(p.path, () => {
    test("loads, with one heading, the page marked in the navigation and no sideways scroll", async ({
      page,
      api,
      errors,
    }) => {
      void api;
      await open(page, p.path, p.ready);
      await expect(page.getByRole("heading", { level: 1 })).toHaveCount(1);
      await expect(page.getByRole("heading", { level: 1 })).toContainText(p.h1);
      await expect(page.locator('nav[aria-label="Main"] a[aria-current="page"]')).toHaveText(p.nav);
      await expect(page).toHaveTitle(/· doe-lab$/);
      const overflow = await page.evaluate(
        () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
      );
      expect(overflow, "the page body scrolls sideways").toBeLessThanOrEqual(0);
      expect(errors).toEqual([]);
    });

    for (const scheme of ["light", "dark"] as const) {
      test(`has no WCAG 2.2 AA violation in the ${scheme} theme`, async ({ page, api }) => {
        void api;
        await page.emulateMedia({ colorScheme: scheme });
        await open(page, p.path, p.ready);
        const results = await new AxeBuilder({ page })
          .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"])
          .analyze();
        expect(
          results.violations.map(
            (v) => `${v.id}: ${v.nodes.map((n) => n.target.join(" ")).join(", ")}`,
          ),
        ).toEqual([]);
      });
    }

    test("has pointer targets of at least 24 by 24 px", async ({ page, api }) => {
      void api;
      await open(page, p.path, p.ready);
      const small = await page.evaluate(() =>
        [...document.querySelectorAll<HTMLElement>("a, button, input, select, textarea, summary")]
          .filter((el) => el.offsetParent !== null && !el.classList.contains("sr-only"))
          // A link inside a sentence is exempt (WCAG 2.5.8, inline).
          .filter(
            (el) =>
              !(el.tagName === "A" && el.closest("p, li, footer, td, th") && !el.closest("nav")),
          )
          .map((el) => ({ el, box: el.getBoundingClientRect() }))
          .filter(({ box }) => box.width < 24 || box.height < 24)
          .map(
            ({ el, box }) =>
              `${el.tagName} "${el.textContent?.trim().slice(0, 30)}" ${Math.round(box.width)}x${Math.round(box.height)}`,
          ),
      );
      expect(small).toEqual([]);
    });
  });
}

test("the feeder's overview states the status in words, and the figures, before any chart", async ({
  page,
  api,
}) => {
  void api;
  await open(page, "/feeder", "Export: allowed and measured");
  await expect(
    page.getByText("Constrained: high voltage at XDLAB000022", { exact: true }),
  ).toBeVisible();
  await expect(page.getByRole("link", { name: "1 open alert" })).toBeVisible();
  const figures = page.locator("dl").first();
  // Beside what every site exports, its shape over the range on show, with
  // the same in words.
  const all = figures.locator(".card", { hasText: "Export, all sites" });
  await expect(all.locator(".spark path")).toHaveAttribute("d", /^M0 /);
  await expect(all.locator(".spark .sr-only")).toHaveText(
    /^Measured export: (Up|Down|Steady|Now) /,
  );
  // What the sites with a limit export, against the sum of those limits, as
  // a number and a bar; and beside it what every site exports.
  const controlled = figures.locator(".card", { hasText: "Controlled export" });
  await expect(controlled).toContainText("2.5 kW");
  await expect(controlled).toContainText("of 3.5 kW allowed");
  await expect(controlled).toContainText("73\u00a0%");
  await expect(controlled.locator(".bar .fill")).toBeVisible();
  await expect(figures.locator(".card", { hasText: "Export, all sites" })).toContainText("2.8 kW");
  await expect(figures).toContainText("2 of 2");
});

test("a first visit gets the intro panel, and dismissing it is remembered", async ({
  page,
  api,
}) => {
  void api;
  // After the fixture's script, which dismissed the panel: undo that, once.
  await page.addInitScript(() => {
    if (!sessionStorage.getItem("e2e.first-visit")) localStorage.removeItem("doelab.intro");
    sessionStorage.setItem("e2e.first-visit", "1");
  });
  await page.goto("/");
  await expect(page.getByText("What you are looking at.")).toBeVisible();
  await expect(page.getByRole("complementary")).toContainText(
    "This is a simulation, not a real network.",
  );
  await page.getByRole("button", { name: "Got it" }).click();
  await expect(page.getByText("What you are looking at.")).toBeHidden();
  await page.reload();
  await expect(page.getByRole("heading", { name: "Needs attention" })).toBeVisible();
  await expect(page.getByText("What you are looking at.")).toBeHidden();
});

test("each chart has a summary and a table with the same series", async ({ page, api }) => {
  void api;
  await open(page, "/feeder", "Export: allowed and measured");
  const figure = page.locator("figure", { hasText: "Customer voltage" });
  await expect(figure.locator("figcaption p")).toContainText("the limit is 253.0 V");
  await figure.getByRole("button", { name: "View as table" }).click();
  const table = figure.getByRole("table", { name: "Customer voltage" });
  await expect(table.locator("thead th")).toHaveText([
    "Time",
    "Highest, at the envelopes",
    "Highest, at a fixed limit",
    "Highest, with no limits",
    "Lowest, forecast",
    "Upper limit",
    "Lower limit",
  ]);
  await expect(table.locator("tbody tr")).toHaveCount(49);
  await expect(table.locator("tbody tr").first().locator("td").nth(4)).toHaveText("253.0 V");
});

test("a chart floats a readout beside the pointer, and the charts beside it follow", async ({
  page,
  api,
}) => {
  void api;
  await open(page, "/feeder", "Export: allowed and measured");
  const first = page.locator("figure", { hasText: "Export: allowed and measured" });
  const second = page.locator("figure", { hasText: "Customer voltage" });
  await second.scrollIntoViewIfNeeded();
  await expect(second.locator("canvas")).toBeVisible();
  await first.scrollIntoViewIfNeeded();
  const plot = (await first.locator(".u-over").boundingBox())!;
  await page.mouse.move(plot.x + plot.width * 0.3, plot.y + plot.height * 0.5);
  // The time, and each series with its value and its unit.
  const tip = first.locator(".tip");
  await expect(tip).toBeVisible();
  await expect(tip).toContainText(/^\w{3} \d{1,2} \w{3}, \d\d:\d\d/);
  await expect(tip).toContainText(/Allowed by the envelopes\s*\d+\.\d\u00a0kW/);
  // It stays inside its chart, at either width.
  const box = (await tip.boundingBox())!;
  const frame = (await first.boundingBox())!;
  expect(box.x).toBeGreaterThanOrEqual(frame.x);
  expect(box.x + box.width).toBeLessThanOrEqual(frame.x + frame.width);
  // The chart beside it shares the cursor: its legend has the values, and
  // nothing floats there.
  await expect(second.getByRole("button", { name: /Upper limit\s+253\.0\sV/ })).toBeVisible();
  await expect(second.locator(".tip")).toHaveCount(0);
  // When the pointer leaves, the readout goes.
  await page.mouse.move(2, 2);
  await expect(tip).toHaveCount(0);
});

test("a chart's cursor moves from the keyboard, and each point is said in words", async ({
  page,
  api,
}) => {
  void api;
  await open(page, "/feeder", "Export: allowed and measured");
  const slider = page.getByRole("slider", { name: /^Export: allowed and measured: a cursor/ });
  await slider.focus();
  await expect(slider).toHaveAttribute("aria-valuetext", /No point chosen\.$/);
  await page.keyboard.press("ArrowRight");
  await expect(slider).toHaveAttribute(
    "aria-valuetext",
    /^\w{3} \d{1,2} \w{3}, \d\d:\d\d\. Allowed by the envelopes \d+\.\d\u00a0kW, /,
  );
  const first = await slider.getAttribute("aria-valuetext");
  await page.keyboard.press("ArrowRight");
  await expect(slider).not.toHaveAttribute("aria-valuetext", first!);
  // The same readout a pointer gets, for a sighted reader at the keyboard.
  await expect(page.locator("figure .tip")).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.locator("figure .tip")).toHaveCount(0);
});

test("a series hidden from a chart's legend is hidden in the address too", async ({
  page,
  api,
}) => {
  void api;
  await open(page, "/feeder", "Export: allowed and measured");
  const figure = page.locator("figure", { hasText: "Export: allowed and measured" });
  const fixed = figure.getByRole("button", { name: /^Allowed by a fixed limit/ });
  await expect(fixed).toHaveAttribute("aria-pressed", "true");
  await fixed.click();
  await expect(page).toHaveURL(/[?&]hide=export\.1/);
  await expect(fixed).toHaveAttribute("aria-pressed", "false");
  // Shared as a link: the same view. And the table still has the series.
  await page.goto("/feeder?hide=export.1,voltage.0");
  await expect(fixed).toHaveAttribute("aria-pressed", "false");
  await expect(
    page
      .locator("figure", { hasText: "Customer voltage" })
      .getByRole("button", { name: /^Highest, at the envelopes/ }),
  ).toHaveAttribute("aria-pressed", "false");
  await figure.getByRole("button", { name: "View as table" }).click();
  await expect(
    figure.getByRole("columnheader", { name: "Allowed by a fixed limit" }),
  ).toBeVisible();
});

test("a chart zooms with Ctrl and the wheel, and around now, and the range is in the address", async ({
  page,
  api,
}) => {
  void api;
  await open(page, "/feeder", "Export: allowed and measured");
  const first = page.locator("figure", { hasText: "Export: allowed and measured" });
  await first.scrollIntoViewIfNeeded();
  const plot = (await first.locator(".u-over").boundingBox())!;
  await page.mouse.move(plot.x + plot.width / 2, plot.y + plot.height / 2);
  // A plain wheel is the page's: it scrolls, and the chart keeps its range.
  await page.mouse.wheel(0, 40);
  await page.waitForTimeout(350);
  await expect(page).not.toHaveURL(/from=/);
  await page.mouse.wheel(0, -40);

  await page.keyboard.down("Control");
  await page.mouse.wheel(0, -300);
  await page.mouse.wheel(0, -300);
  await page.keyboard.up("Control");
  // Told once, when the wheel has stopped.
  await expect(page).toHaveURL(/[?&]from=\d+&to=\d+/);
  const range = () => {
    const url = new URL(page.url());
    return Number(url.searchParams.get("to")) - Number(url.searchParams.get("from"));
  };
  expect(range()).toBeLessThan(12 * 3600);
  await page.getByRole("button", { name: "Reset zoom" }).click();
  await expect(page).not.toHaveURL(/from=/);

  // Two hours either side of now, in one press.
  await page.getByRole("button", { name: "Now ± 2 h" }).click();
  await expect(page).toHaveURL(/[?&]from=\d+&to=\d+/);
  expect(range()).toBe(4 * 3600);
  // And from the keyboard: out to everything.
  await page.getByRole("slider", { name: /^Export: allowed and measured/ }).focus();
  await page.keyboard.press("0");
  await expect(page).not.toHaveURL(/from=/);
});

test("a click on a chart pins an instant, in the address, with the way to the network then", async ({
  page,
  api,
}) => {
  void api;
  await open(page, "/feeder", "Export: allowed and measured");
  const first = page.locator("figure", { hasText: "Export: allowed and measured" });
  await first.scrollIntoViewIfNeeded();
  const plot = (await first.locator(".u-over").boundingBox())!;
  await page.mouse.click(plot.x + plot.width * 0.3, plot.y + plot.height / 2);
  await expect(page).toHaveURL(/[?&]pin=\d+/);
  const pin = Number(new URL(page.url()).searchParams.get("pin"));
  await expect(first.getByText(/^Pinned at \w{3} \d{1,2} \w{3}, \d\d:\d\d/)).toBeVisible();
  await expect(first.getByRole("link", { name: "See the network then" })).toHaveAttribute(
    "href",
    `/network?feeder=LV10&at=${Math.floor(pin / 1800) * 1800}`,
  );
  // Shared as a link: the same instant, with its values in the legend and no
  // pointer on the chart.
  await page.goto(`/feeder?pin=${pin}`);
  await chartDrawn(page);
  await expect(
    first.getByRole("button", { name: /^Allowed by the envelopes\s+\d+\.\d\skW/ }),
  ).toBeVisible();
  await first.getByRole("button", { name: "Unpin" }).click();
  await expect(page).not.toHaveURL(/pin=/);
});

test("a breach on a site's chart and its alert in the list point at each other", async ({
  page,
  api,
}) => {
  void api;
  await open(page, `/sites/${NMI.breaching}`, "Envelope, forecast and telemetry");
  const row = page.locator("li.alert", { hasText: "Exporting 2.7 kW against a limit of 1.5 kW" });
  await expect(row).not.toHaveAttribute("data-lit");
  // The pointer on the breach's mark lights its alert.
  const figure = page.locator("figure").first();
  await figure.scrollIntoViewIfNeeded();
  const plot = (await figure.locator(".u-over").boundingBox())!;
  await page.mouse.move(plot.x + plot.width * 0.47, plot.y + plot.height / 2);
  await expect(row).toHaveAttribute("data-lit", "");
  await page.mouse.move(plot.x + plot.width * 0.9, plot.y + plot.height / 2);
  await expect(row).not.toHaveAttribute("data-lit");
  // And the alert, from the keyboard, points at its mark.
  await row.focus();
  await expect(row).toHaveAttribute("data-lit", "");
});

test("the time range lives in the address", async ({ page, api }) => {
  void api;
  await open(page, "/feeder", "Export: allowed and measured");
  await page
    .getByRole("navigation", { name: "Time range" })
    .getByRole("link", { name: "6 hours" })
    .click();
  await expect(page).toHaveURL(/\?range=6h$/);
  await expect(page.locator('nav[aria-label="Time range"] a[aria-current="true"]')).toHaveText(
    "6 hours",
  );
  // Shared as a link: the same view.
  await page.goto("/feeder?range=3d");
  await expect(page.locator('nav[aria-label="Time range"] a[aria-current="true"]')).toHaveText(
    "3 days",
  );
});

for (const [path, ready] of [
  ["/feeder", "Export: allowed and measured"],
  [`/sites/${NMI.breaching}`, "Envelope, forecast and telemetry"],
] as const) {
  test(`${path}: another range keeps what is on screen until its answer arrives`, async ({
    page,
    api,
  }) => {
    await open(page, path, ready);
    const figure = page.locator("figure").first();
    await figure.scrollIntoViewIfNeeded();
    // What the page takes away or puts up between the two ranges, and whether
    // it moves under the reader.
    await page.evaluate(() => {
      const seen = { placeholders: 0, chartsGone: 0, heights: new Set<number>() };
      (window as unknown as { __range: typeof seen }).__range = seen;
      const main = document.querySelector("main")!;
      new MutationObserver((records) => {
        for (const record of records) {
          for (const node of record.addedNodes)
            if (node instanceof Element && node.matches('[role="status"].animate-pulse-soft'))
              seen.placeholders++;
          for (const node of record.removedNodes)
            if (
              node instanceof Element &&
              (node.matches("figure, canvas") || node.querySelector("canvas"))
            )
              seen.chartsGone++;
        }
      }).observe(main, { subtree: true, childList: true });
      const watch = () => {
        seen.heights.add(main.scrollHeight);
        requestAnimationFrame(watch);
      };
      watch();
    });
    // Slow enough that a placeholder would be seen, if there were one.
    api.delayMs = 400;
    const asked = page.waitForResponse((r) => /Get(Feeder|Site)Series/.test(r.url()));
    await page
      .getByRole("navigation", { name: "Time range" })
      .getByRole("link", { name: "3 days" })
      .click();
    await expect(page).toHaveURL(/[?&]range=3d/);
    await expect(figure.locator("canvas")).toBeVisible();
    await asked;
    await page.waitForTimeout(300);
    const seen = await page.evaluate(() => {
      const s = (
        window as unknown as {
          __range: { placeholders: number; chartsGone: number; heights: Set<number> };
        }
      ).__range;
      return { placeholders: s.placeholders, chartsGone: s.chartsGone, heights: s.heights.size };
    });
    expect(seen).toEqual({ placeholders: 0, chartsGone: 0, heights: 1 });
  });
}

test("when live updates stop, a banner says so and the figures stay; then they resume", async ({
  page,
  api,
}) => {
  await open(page, "/feeder", "Export: allowed and measured");
  await expect(page.locator("dl").first()).toContainText("2.8 kW");
  api.streamsDown = true;
  await expect(page.getByText("Live updates paused, reconnecting…")).toBeVisible({
    timeout: 15_000,
  });
  await expect(page.locator("dl").first()).toContainText("2.8 kW");
  api.streamsDown = false;
  await expect(page.getByText("Live updates paused, reconnecting…")).toBeHidden({
    timeout: 20_000,
  });
});

test("an unknown NMI gets a designed state, not an error", async ({ page, api }) => {
  void api;
  await page.goto("/sites/NOPE");
  await expect(page.getByText("There is no site with this NMI")).toBeVisible();
  await expect(page.getByRole("link", { name: "find the site in the list" })).toBeVisible();
});

test("an unknown address gets the not-found page", async ({ page, api }) => {
  void api;
  await page.goto("/nowhere");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(
    "There is no page at this address",
  );
});

test("the site page says why the site is limited, and marks its breach", async ({ page, api }) => {
  void api;
  await open(page, `/sites/${NMI.breaching}`, "Envelope, forecast and telemetry");
  await expect(
    page.getByText(/^Export limited to \d\.\d\skW by high voltage at XDLAB000022\.$/),
  ).toBeVisible();
  await expect(page.locator("figure")).toContainText("1 breach is marked.");
  // The last reading and the limit in force are one figure, with a bar.
  const tile = page.locator(".card", { hasText: "Net export against its limit" });
  await expect(tile).toContainText(/1\.5\u00a0kW of \d\.\d\u00a0kW/);
  await expect(tile).toContainText(/Exporting 1\.5\u00a0kW of \d\.\d\u00a0kW allowed: /);
  await expect(tile).toContainText("connection limit 5.0\u00a0kW");
  await expect(tile.locator(".bar .fill").first()).toBeVisible();
  await expect(page.getByText("Exporting 2.7 kW against a limit of 1.5 kW")).toBeVisible();
  await page.getByText("Envelopes in this range (49)").click();
  await expect(page.getByRole("table").locator("tbody tr").first()).toContainText(
    "high voltage at XDLAB000022",
  );
});

test("the sites list filters by what is typed, and keeps the filter in the address", async ({
  page,
  api,
}) => {
  void api;
  await open(page, "/sites", NMI.enrolled);
  await page.getByLabel("Find a site by NMI or name").fill("load_b");
  await expect(page.getByText("1 of 3 sites")).toBeVisible();
  await expect(page).toHaveURL(/\?q=load_b$/);
  await page.getByLabel("Find a site by NMI or name").fill("zzz");
  await expect(page.getByText("No site matches")).toBeVisible();
  await page.goto("/sites?enrolled=1");
  await expect(page.getByText("2 of 3 sites")).toBeVisible();
});

test("a table is put in order from its headings, and the order is in the address", async ({
  page,
  api,
}) => {
  void api;
  await open(page, "/sites", NMI.enrolled);
  const table = page.getByRole("region", { name: "Sites" });
  const first = () => table.locator("tbody tr").first();
  const heading = (name: string) => table.getByRole("columnheader", { name });
  // By NMI to begin with, and the heading says so.
  await expect(heading("NMI")).toHaveAttribute("aria-sort", "ascending");
  await expect(first()).toContainText(NMI.enrolled);

  // By connection limit: the site with none is last, whichever way.
  await heading("Connection limit, export").getByRole("link").click();
  await expect(page).toHaveURL(/[?&]sort=cap&dir=asc/);
  await expect(heading("Connection limit, export")).toHaveAttribute("aria-sort", "ascending");
  await expect(heading("NMI")).toHaveAttribute("aria-sort", "none");
  await expect(table.locator("tbody tr").last()).toContainText(NMI.passive);
  await heading("Connection limit, export").getByRole("link").click();
  await expect(page).toHaveURL(/[?&]sort=cap&dir=desc/);
  await expect(table.locator("tbody tr").last()).toContainText(NMI.passive);

  // Shared as a link: the same order. And back to the table's own order, the
  // address says nothing of it.
  await page.goto("/sites?sort=name&dir=desc");
  await expect(first()).toContainText("Ld3_LOAD_C");
  await heading("NMI").getByRole("link").click();
  await expect(page).not.toHaveURL(/sort=/);
  await expect(first()).toContainText(NMI.enrolled);

  // The heading stays in view while a long table scrolls under it.
  expect(await heading("NMI").evaluate((th) => getComputedStyle(th).position)).toBe("sticky");

  // The fleet's table the same way: the fullest use of a limit first.
  await open(page, "/?sub=SUB-001&sort=use&dir=desc", "Ausgrid Lidcombe Zone");
  const fleet = page.getByRole("region", { name: "Sites on the map" });
  await expect(fleet.locator("tbody tr").first()).toContainText(MAP.over);
  await expect(fleet.locator("tbody tr").last()).toContainText(MAP.charger);
});

test("the theme toggle switches the theme and remembers it", async ({ page, api }) => {
  void api;
  await page.emulateMedia({ colorScheme: "light" });
  await open(page, "/sites", NMI.enrolled);
  const background = () => page.evaluate(() => getComputedStyle(document.body).backgroundColor);
  const light = await background();
  const toggle = page.getByRole("button", { name: /^Theme:/ });
  await toggle.click(); // light
  await toggle.click(); // dark
  await expect(toggle).toHaveAccessibleName("Theme: Dark. Switch to System.");
  expect(await background()).not.toBe(light);
  await page.reload();
  await expect(page.getByRole("button", { name: "Theme: Dark. Switch to System." })).toBeVisible();
  expect(await background()).not.toBe(light);
});

test("the skip link is the first stop, and goes to the content", async ({ page, api }) => {
  void api;
  await open(page, "/sites", NMI.enrolled);
  await page.keyboard.press("Tab");
  const skip = page.getByRole("link", { name: "Skip to content" });
  await expect(skip).toBeFocused();
  await expect(skip).toBeVisible();
  await page.keyboard.press("Enter");
  await expect(page.locator("#main")).toBeFocused();
});

test("the backstop can be triggered and cleared with the keyboard alone", async ({ page, api }) => {
  await open(page, "/operations", "Engine runs");

  // Tab from the top of the page to the token field: every stop on the way
  // shows a focus ring.
  const focused = () =>
    page.evaluate(() => document.activeElement?.id || document.activeElement?.tagName || "");
  for (let i = 0; i < 40 && (await focused()) !== "backstop-token"; i++) {
    await page.keyboard.press("Tab");
    const ring = await page.evaluate(() => {
      const style = getComputedStyle(document.activeElement as HTMLElement);
      return style.outlineStyle !== "none" && parseFloat(style.outlineWidth) >= 2;
    });
    expect(ring, `a focus ring on ${await focused()}`).toBe(true);
  }
  expect(await focused()).toBe("backstop-token");
  await page.keyboard.type(OPERATOR_TOKEN);
  await page.keyboard.press("Tab");
  expect(await focused()).toBe("backstop-limit");
  await page.keyboard.press("Tab");
  await page.keyboard.type("transformer over temperature");
  // Enter in a field does nothing.
  await page.keyboard.press("Enter");
  await page.keyboard.press("Tab");
  await page.keyboard.type("lv10");
  await page.keyboard.press("Enter");
  expect(api.writes).toEqual([]);

  await page.keyboard.press("Tab");
  await expect(page.getByRole("button", { name: "Trigger the backstop" })).toBeFocused();
  await page.keyboard.press("Enter");
  await expect(page.getByText("A backstop is active.")).toBeVisible();
  expect(api.writes).toEqual(["BackstopService/CreateBackstopEvent"]);
  await expect(page.getByRole("status").filter({ hasText: "Backstop triggered" })).toBeVisible();

  // Clearing is as easy to reach: the same panel, the token already there.
  await page.getByRole("button", { name: "Clear the backstop" }).focus();
  await page.keyboard.press("Space");
  await expect(page.getByRole("button", { name: "Trigger the backstop" })).toBeVisible();
  expect(api.writes).toEqual([
    "BackstopService/CreateBackstopEvent",
    "BackstopService/ClearBackstop",
  ]);
  await expect(page.getByText("transformer over temperature")).toBeVisible();
});

test("a wrong token is an inline error on the backstop control, and the page stays", async ({
  page,
  api,
}) => {
  void api;
  await open(page, "/operations", "Engine runs");
  await page.getByLabel("Operator token").fill("not-the-token");
  await page.getByLabel("Reason").fill("drill");
  await page.getByLabel(/Type the feeder's code/).fill("LV10");
  await page.getByRole("button", { name: "Trigger the backstop" }).click();
  await expect(page.locator("#backstop-token-help")).toHaveText(
    "The operator token is missing or not valid.",
  );
  await expect(page.getByLabel("Operator token")).toHaveAttribute("aria-invalid", "true");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Operations");
  await expect(page.getByLabel("Reason")).toHaveValue("drill");
});

test("the backstop control cannot be submitted twice", async ({ page, api }) => {
  await open(page, "/operations", "Engine runs");
  await page.getByLabel("Operator token").fill(OPERATOR_TOKEN);
  await page.getByLabel("Reason").fill("drill");
  await page.getByLabel(/Type the feeder's code/).fill("LV10");
  api.delayMs = 600;
  await page.getByRole("button", { name: "Trigger the backstop" }).dblclick();
  await expect(page.getByText("A backstop is active.")).toBeVisible();
  expect(api.writes.filter((w) => w === "BackstopService/CreateBackstopEvent")).toHaveLength(1);
});

test("an alert links to its site, and can be acknowledged", async ({ page, api }) => {
  await open(page, "/operations", "Engine runs");
  const alert = page.locator("li", { hasText: "Exporting 2.7 kW against a limit of 1.5 kW" });
  await expect(alert.getByRole("link", { name: NMI.breaching })).toHaveAttribute(
    "href",
    `/sites/${NMI.breaching}`,
  );
  await page.getByLabel("Operator token").fill(OPERATOR_TOKEN);
  await alert.getByRole("button", { name: "Acknowledge" }).click();
  await expect(alert).toContainText("Acknowledged by operator");
  expect(api.writes).toEqual(["AlertService/AcknowledgeAlert"]);
  // Resolved alerts are one filter away, and the filter is in the address.
  await page
    .getByRole("navigation", { name: "Which alerts" })
    .getByRole("link", { name: "All" })
    .click();
  await expect(page).toHaveURL(/\?alerts=all$/);
  await expect(page.getByText("Device stopped reporting")).toBeVisible();
});

test("a completed run can be exported, and the link to the file appears in its row", async ({
  page,
  api,
}) => {
  await open(page, "/operations", "Engine runs");
  const runs = page.getByRole("region", { name: "Engine runs" }).locator("tbody tr");
  // The failed run has nothing to export.
  await expect(runs.nth(2).getByRole("button", { name: "Export" })).toHaveCount(0);
  await page.getByLabel("Operator token").fill(OPERATOR_TOKEN);
  await runs.first().getByRole("button", { name: "Export" }).click();
  const link = runs.first().getByRole("link", { name: "Download (96 rows)" });
  await expect(link).toHaveAttribute("href", /\/exports\/runs\/LV10\/.+\.csv\?signature=e2e$/);
  expect(api.writes).toEqual(["EnvelopeRunService/ExportEnvelopeRun"]);
  await expect(page.getByRole("status").filter({ hasText: "Exported 96 envelopes" })).toBeVisible();
});

test("the config form rejects a value out of range inline, previews the change and saves a new version", async ({
  page,
  api,
}) => {
  await open(page, "/config", "Version history");
  const band = page.getByLabel("Highest voltage allowed");
  await band.fill("300");
  await page.getByLabel("Solar scale").focus();
  await expect(page.locator("#config-vMaxV-help")).toHaveText("Enter from 184 to 276 V.");
  await expect(band).toHaveAttribute("aria-invalid", "true");

  await band.fill("250");
  await expect(page.getByRole("heading", { name: "1 change from version 1" })).toBeVisible();
  await expect(page.locator("form section li")).toHaveText(
    /Highest voltage allowed:\s+from\s+253 V\s+to\s+250 V/,
  );
  await page.getByLabel("Note for this version").fill("tighter band");
  await page.getByLabel("Operator token").fill(OPERATOR_TOKEN);
  await page.getByRole("button", { name: "Save as version 2" }).click();

  await expect(page.getByText("Saved as version 2. The next engine run uses it.")).toBeVisible();
  expect(api.writes).toEqual(["EnvelopeConfigService/CreateEnvelopeConfig"]);
  await expect(page.getByRole("heading", { name: /Version 2 is in force/ })).toBeVisible();
  const history = page.locator("ol > li");
  await expect(history.first()).toContainText("Version 2, in force");
  await expect(history.first()).toContainText('"tighter band"');
  await expect(history.first()).toContainText(
    /Highest voltage allowed:\s+from\s+253 V\s+to\s+250 V/,
  );
  await expect(band).toHaveValue("250");
});

test("leaving the config form with unsaved changes asks first", async ({ page, api }) => {
  void api;
  await open(page, "/config", "Version history");
  await page.getByLabel("Solar scale").fill("2");
  let asked = "";
  page.once("dialog", (dialog) => {
    asked = dialog.message();
    void dialog.dismiss();
  });
  await page.getByRole("navigation", { name: "Main" }).getByRole("link", { name: "Sites" }).click();
  await expect.poll(() => asked).toContain("changes you have not saved");
  await expect(page).toHaveURL(/\/config$/);
  page.once("dialog", (dialog) => void dialog.accept());
  await page.getByRole("navigation", { name: "Main" }).getByRole("link", { name: "Sites" }).click();
  await expect(page).toHaveURL(/\/sites$/);
});

test("nothing animates for a reader who asked for reduced motion", async ({ page, api }) => {
  api.delayMs = 1500;
  const animated = () =>
    page.evaluate(() =>
      [...document.querySelectorAll("*")]
        .filter((el) => {
          const style = getComputedStyle(el);
          const moving = (list: string) => list.split(",").some((d) => parseFloat(d) > 0);
          return (
            (style.animationName !== "none" && moving(style.animationDuration)) ||
            moving(style.transitionDuration)
          );
        })
        .map((el) => `${el.tagName}.${el.className.toString().slice(0, 40)}`),
    );
  const loading = page.getByRole("status").filter({ hasText: "Loading" }).first();

  // With motion allowed, the skeletons pulse: the check below can see it.
  await page.goto("/operations");
  await expect(loading).toBeVisible();
  expect((await animated()).length).toBeGreaterThan(0);

  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.goto("/feeder");
  await expect(loading).toBeVisible();
  expect(await animated()).toEqual([]);
  api.delayMs = 0;
  await chartDrawn(page);
  await page.getByRole("button", { name: /^Theme:/ }).hover();
  expect(await animated()).toEqual([]);
  // A card beside the pointer comes without moving in.
  await page.locator(".meter").first().hover();
  await expect(page.locator(".card-over")).toBeVisible();
  expect(await animated()).toEqual([]);
});

test("the network's dashes stand down for a reader who asked for reduced motion", async ({
  page,
  api,
}) => {
  void api;
  await page.emulateMedia({ reducedMotion: "reduce" });
  await open(page, "/network", "Closest to a limit");
  const drawing = page.getByRole("img", { name: /500 metres of cable away/ });
  // The arrows still say which way the power goes; nothing moves, and there
  // is nothing to stop.
  await expect(drawing.locator(".arrow")).toHaveCount(2);
  await expect(drawing.locator(".flow").first()).toBeHidden();
  await expect(page.getByRole("link", { name: "Stop the moving dashes" })).toBeHidden();
});

for (const [path, ready] of [
  ["/", "Ausgrid Lidcombe Zone"],
  ["/feeder", "Export: allowed and measured"],
] as const) {
  test(`${path} keeps its layout while it loads`, async ({ page, api }) => {
    api.delayMs = 300;
    await page.addInitScript(() => {
      let shift = 0;
      new PerformanceObserver((list) => {
        for (const entry of list.getEntries() as (PerformanceEntry & {
          value: number;
          hadRecentInput: boolean;
        })[]) {
          if (!entry.hadRecentInput) shift += entry.value;
        }
        (window as unknown as { __cls: number }).__cls = shift;
      }).observe({ type: "layout-shift", buffered: true });
    });
    await open(page, path, ready);
    await page.waitForTimeout(500);
    const cls = await page.evaluate(() => (window as unknown as { __cls?: number }).__cls ?? 0);
    expect(cls, "cumulative layout shift").toBeLessThan(0.1);
  });
}

test("the assistant answers a question about the site on screen, from the keyboard", async ({
  page,
  api,
  errors,
}) => {
  await open(page, `/sites/${NMI.breaching}`, "Envelope, forecast and telemetry");
  const ask = page.getByRole("button", { name: "Ask", exact: true });
  await ask.focus();
  await page.keyboard.press("Enter");

  // A modal dialog, with the focus in the question.
  const drawer = page.getByRole("dialog", { name: "Ask about LV10" });
  await expect(drawer).toBeVisible();
  await expect(drawer.getByLabel("Your question")).toBeFocused();
  await expect(
    drawer.getByRole("button", { name: `Why is ${NMI.breaching} limited now?` }),
  ).toBeVisible();

  await page.keyboard.type("Why is this site limited at 12:30?");
  await page.keyboard.press("Enter");
  const answer = drawer.getByRole("region", { name: "Answer" });
  await expect(answer).toContainText(
    `${NMI.breaching} is limited to 1.5 kW by voltage at XDLAB000022, which would pass 253 V.`,
  );
  await expect(answer.getByRole("listitem")).toHaveText([
    `What limits ${NMI.breaching} at 12:30 on 10 Nov`,
    "Config version 1",
  ]);
  // The question went with the feeder and the site on screen.
  expect(api.asked).toEqual([
    { feederCode: "LV10", question: "Why is this site limited at 12:30?", nmi: NMI.breaching },
  ]);
  expect(api.writes).toEqual([]);

  // Nothing of it scrolls the page sideways, at either size.
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
  );
  expect(overflow).toBeLessThanOrEqual(0);

  // Escape closes it, and the focus is back on the button that opened it.
  await page.keyboard.press("Escape");
  await expect(drawer).toBeHidden();
  await expect(ask).toBeFocused();
  expect(errors).toEqual([]);
});

for (const scheme of ["light", "dark"] as const) {
  test(`the assistant drawer has no WCAG 2.2 AA violation in the ${scheme} theme`, async ({
    page,
    api,
  }) => {
    void api;
    await page.emulateMedia({ colorScheme: scheme });
    await open(page, "/feeder", "Export: allowed and measured");
    await page.getByRole("button", { name: "Ask", exact: true }).click();
    const drawer = page.getByRole("dialog", { name: "Ask about LV10" });
    await drawer.getByRole("button", { name: "Which sites are over their limit now?" }).click();
    await expect(drawer.getByRole("region", { name: "Answer" })).toContainText("253 V");
    const results = await new AxeBuilder({ page })
      .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"])
      .analyze();
    expect(
      results.violations.map(
        (v) => `${v.id}: ${v.nodes.map((n) => n.target.join(" ")).join(", ")}`,
      ),
    ).toEqual([]);
  });
}

test("a server with no assistant offers no way to ask", async ({ page, api }) => {
  api.assistant = "off";
  await open(page, "/feeder", "Export: allowed and measured");
  await expect(page.getByRole("button", { name: "Ask", exact: true })).toHaveCount(0);
});

test("the map shows the substations, and a substation's sites when it is chosen", async ({
  page,
  api,
  errors,
}) => {
  await open(page, "/?region=all", "Ausgrid Lidcombe Zone");
  const map = page.getByRole("region", { name: "Map of the fleet" });
  // Both regions in one frame: the substations, and no site yet.
  await expect(page.getByRole("link", { name: "All regions" })).toHaveAttribute(
    "aria-current",
    "true",
  );
  const lidcombe = map.getByRole("button", { name: "Ausgrid Lidcombe Zone: 1 over limit" });
  await expect(lidcombe).toBeVisible();
  await expect(map.getByRole("button", { name: /^Jemena Footscray Zone:/ })).toBeVisible();
  await expect(map.locator(`[aria-label^="${MAP.over}"]`)).toHaveCount(0);
  expect(api.tiles).toBeGreaterThan(0);
  await expect(map.getByRole("link", { name: "OpenStreetMap" })).toBeVisible();

  // Choosing the substation frames it, in the address, and says what it is.
  // Again if need be: a click that lands while the map is still settling on
  // its first frame falls on the map, not on the mark that moved from under it.
  await expect(async () => {
    await lidcombe.click();
    await expect(page).toHaveURL(/[?&]sub=SUB-001/, { timeout: 1000 });
  }).toPass();
  await expect(
    page.getByRole("heading", { level: 2, name: "Ausgrid Lidcombe Zone", exact: true }),
  ).toBeVisible();
  // The two that report, against their own limits: the silent site's limit
  // is no part of the sum.
  await expect(page.getByText(/2 of 3 sites reporting\. Exporting 3\.1\u00a0kW of /)).toBeVisible();
  await expect(map.locator(`[aria-label="${MAP.over}, Solar: Over limit"]`)).toBeVisible();
  await expect(map.locator(`[aria-label="${MAP.within}, Battery: Within limit"]`)).toBeVisible();
  await expect(
    map.locator(`[aria-label="${MAP.charger}, EV charger: Not reporting"]`),
  ).toBeVisible();

  // A site on the map opens in the panel, with the way to its own page.
  await map.locator(`[aria-label^="${MAP.over}"]`).click();
  await expect(page).toHaveURL(new RegExp(`[?&]site=${MAP.over}`));
  await expect(page.getByRole("heading", { level: 2, name: MAP.over })).toBeVisible();
  await expect(page.getByText(/Exporting 2\.7\u00a0kW against a limit of/)).toBeVisible();
  // And its reading against its limit, in a sentence and as a bar.
  const panel = page.locator("section", { has: page.getByRole("heading", { name: MAP.over }) });
  await expect(panel).toContainText(/Exporting 2\.7\u00a0kW of \d\.\d\u00a0kW allowed: /);
  await expect(panel.locator(".bar .fill").first()).toBeVisible();
  await expect(page.getByRole("link", { name: "Open the site" })).toHaveAttribute(
    "href",
    `/sites/${MAP.over}?feeder=LV10`,
  );
  // And the way to the feeder it is on, and to that feeder's network.
  await expect(page.getByRole("link", { name: "Open its feeder" })).toHaveAttribute(
    "href",
    "/feeder?feeder=LV10",
  );
  await expect(page.getByRole("link", { name: "See its network" })).toHaveAttribute(
    "href",
    "/network?feeder=LV10",
  );
  expect(errors).toEqual([]);
});

test("the first page says how the whole fleet is, in words and figures, before the map", async ({
  page,
  api,
}) => {
  void api;
  await open(page, "/", "Ausgrid Lidcombe Zone");
  // The gravest thing in the fleet, and the sums of its feeders.
  const status = page.locator("section", {
    has: page.getByRole("heading", { name: "Status now" }),
  });
  await expect(status).toContainText("1 open alert");
  await expect(status).toContainText(
    /2 of 2 sites reporting on 1 feeder\. Exporting 2\.5\u00a0kW of/,
  );
  const figures = page.locator("dl").first();
  const controlled = figures.locator(".card", { hasText: "Controlled export" });
  await expect(controlled).toContainText("2.5 kW");
  await expect(controlled).toContainText("of 3.5 kW allowed");
  await expect(controlled).toContainText("73\u00a0%");
  await expect(controlled.locator(".bar .fill")).toBeVisible();
  await expect(figures.locator(".card", { hasText: "Sites reporting" })).toContainText("2 of 2");
  await expect(figures.locator(".card", { hasText: "Open alerts" })).toContainText(
    "on 1 of 1 feeders",
  );
  await expect(figures.locator(".card", { hasText: "Backstops" })).toContainText("None");
  // The figures come before the map in the page.
  const before = await page.evaluate(() => {
    const figures = document.querySelector("dl")!;
    const map = document.querySelector('[aria-label="Map of the fleet"]')!;
    return !!(figures.compareDocumentPosition(map) & Node.DOCUMENT_POSITION_FOLLOWING);
  });
  expect(before).toBe(true);
});

test("what needs attention is listed beside the map, and a row of it selects on the map", async ({
  page,
  api,
}) => {
  void api;
  await open(page, "/", "Ausgrid Lidcombe Zone");
  const panel = page.locator("section", {
    has: page.getByRole("heading", { level: 2, name: "Needs attention" }),
  });
  // The site over its limit, where it is, and its reading against its limit.
  const row = panel.getByRole("listitem").filter({ hasText: MAP.over });
  await expect(row).toContainText("Over limit");
  await expect(row).toContainText("Lidcombe · LV10");
  await expect(row).toContainText(/Exporting 2\.7\u00a0kW of \d\.\d\u00a0kW allowed: /);
  await expect(row.locator(".bar .fill").first()).toBeVisible();
  // The silent charger is not a thing to act on: it is in the table.
  await expect(panel.getByRole("listitem")).toHaveCount(1);

  // The row is a link, so the keyboard reaches it: it chooses the site, and
  // frames its substation so that the site is on the map.
  await row.getByRole("link", { name: MAP.over }).focus();
  await page.keyboard.press("Enter");
  await expect(page).toHaveURL(new RegExp(`[?&]site=${MAP.over}`));
  await expect(page).toHaveURL(/[?&]sub=SUB-001/);
  await expect(page.getByRole("heading", { level: 2, name: MAP.over })).toBeVisible();
  await expect(
    page
      .getByRole("region", { name: "Map of the fleet" })
      .locator(`[aria-label="${MAP.over}, Solar: Over limit"]`),
  ).toBeVisible();

  // And back to the list.
  await page.getByRole("link", { name: "Back to what needs attention" }).click();
  await expect(page).not.toHaveURL(/site=|sub=/);
  await expect(page.getByRole("heading", { level: 2, name: "Needs attention" })).toBeVisible();
});

test("a mark says what it is on a card beside the pointer, and the card goes when the pointer does", async ({
  page,
  api,
}) => {
  void api;
  await open(page, "/network", "Closest to a limit");
  const drawing = page.getByRole("img", { name: /500 metres of cable away/ });
  const card = page.locator(".card-over");
  await expect(card).toHaveCount(0);
  await drawing.locator('[data-bus="B2"] .hit').hover();
  await expect(card).toBeVisible();
  await expect(card).toContainText("Bus B2");
  await expect(card).toContainText("Inside its limits");
  await expect(card).toContainText(/\d{3}\.\d\sV/);
  // Inside the window, at either width.
  const box = (await card.boundingBox())!;
  expect(box.x).toBeGreaterThanOrEqual(0);
  expect(box.x + box.width).toBeLessThanOrEqual(page.viewportSize()!.width);
  await page.mouse.move(2, 2);
  await expect(card).toHaveCount(0);

  // A reading against its limit says its sentence the same way.
  await open(page, "/?sub=SUB-001", "Ausgrid Lidcombe Zone");
  const row = page
    .getByRole("region", { name: "Sites on the map" })
    .getByRole("row", { name: new RegExp(MAP.within) });
  await row.locator(".meter").hover();
  await expect(card).toContainText("Export against its limit");
  await expect(card).toContainText(/Exporting 0\.4\skW of \d\.\d\skW allowed/);
});

test("a site's mark on the map says little: its status, its export against its limit, and one line", async ({
  page,
  api,
}) => {
  await open(page, "/?sub=SUB-001", "Ausgrid Lidcombe Zone");
  const map = page.getByRole("region", { name: "Map of the fleet" });
  const mark = map.locator(`[aria-label^="${MAP.within}"]`);
  await expect(mark).toBeVisible();
  // On a phone the map is below the fold: the pointer goes where the mark is.
  await mark.scrollIntoViewIfNeeded();
  const at = (await mark.boundingBox())!;
  await page.mouse.move(at.x + at.width / 2 - 2, at.y + at.height / 2);
  await page.mouse.move(at.x + at.width / 2, at.y + at.height / 2);
  const card = page.locator(".card-over");
  await expect(card).toContainText(`${MAP.within}, Battery`);
  await expect(card).toContainText("Within limit");
  // The last six hours of its export, drawn against its limit: asked for
  // once the pointer has rested there.
  await expect(card.getByText("Net export, last 6 hours", { exact: true })).toBeVisible();
  await expect(card.locator(".spark path")).toHaveAttribute("d", /^M0 /);
  await expect(card.locator(".spark line.limit")).toHaveCount(1);
  expect(api.calls["TelemetryService/GetSiteSeries"]).toBe(1);
  // One line of what it is doing, and none of the sentences of the panel.
  await expect(card).toContainText(/Exporting 0\.4\skW of \d\.\d\skW/);
  await expect(card).not.toContainText("to spare");
  await expect(card).not.toContainText("Export limited to");
  // Inside the window, at either width.
  const box = (await card.boundingBox())!;
  expect(box.x).toBeGreaterThanOrEqual(0);
  expect(box.x + box.width).toBeLessThanOrEqual(page.viewportSize()!.width);
});

test("a site chosen on the first page shows the shape of its export, with words for it", async ({
  page,
  api,
}) => {
  await open(page, `/?sub=SUB-001&site=${MAP.over}`, "Ausgrid Lidcombe Zone");
  const panel = page.locator("section", { has: page.getByRole("heading", { name: MAP.over }) });
  await expect(panel.getByText("Net export, last 6 hours")).toBeVisible();
  await expect(panel.locator(".spark path")).toHaveAttribute("d", /^M0 /);
  await expect(panel.locator(".spark .sr-only")).toHaveText(/^Net export: (Up|Down|Steady|Now) /);
  // Asked for once, for the site that was chosen.
  expect(api.calls["TelemetryService/GetSiteSeries"]).toBe(1);
});

test("a substation on the first page leads to each of its feeders", async ({ page, api }) => {
  void api;
  await open(page, "/?sub=SUB-001", "Ausgrid Lidcombe Zone");
  await expect(page.getByRole("link", { name: "Open feeder LV10" })).toHaveAttribute(
    "href",
    "/feeder?feeder=LV10",
  );
  await expect(page.getByRole("link", { name: "See the network of LV10" })).toHaveAttribute(
    "href",
    "/network?feeder=LV10",
  );
});

test("the old address of the map leads to the fleet, with what it had chosen", async ({
  page,
  api,
}) => {
  void api;
  await page.goto("/map?sub=SUB-001&site=" + MAP.over);
  await expect(page).toHaveURL(new RegExp(`/\\?sub=SUB-001&site=${MAP.over}$`));
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Fleet");
  await expect(page.getByRole("heading", { level: 2, name: MAP.over })).toBeVisible();
});

test("the pages are in the order an operator works in", async ({ page, api }) => {
  void api;
  await open(page, "/sites", NMI.enrolled);
  await expect(page.locator('nav[aria-label="Main"] a')).toHaveText([
    "Fleet",
    "Feeder",
    "Network",
    "Operations",
    "Sites",
    "Config",
  ]);
});

test("every site of the map can be reached from the keyboard, through the table", async ({
  page,
  api,
}) => {
  void api;
  await open(page, "/?region=all", "Ausgrid Lidcombe Zone");
  const table = page.getByRole("region", { name: "Sites on the map" });
  await expect(table.getByRole("row")).toHaveCount(4);
  const row = table.getByRole("row", { name: new RegExp(MAP.charger) });
  await expect(row).toContainText("EV charger");
  await expect(row).toContainText("Not reporting");
  // Each row compares the site's reading with its limit, or says why not.
  await expect(table.getByRole("columnheader", { name: "Use of limit" })).toBeVisible();
  await expect(row).toContainText("No reading");
  await expect(table.getByRole("row", { name: new RegExp(MAP.within) })).toContainText(
    /Exporting 0\.4\u00a0kW of \d\.\d\u00a0kW allowed: \d+\u00a0%/,
  );
  await row.getByRole("link", { name: MAP.charger }).focus();
  await page.keyboard.press("Enter");
  await expect(page).toHaveURL(new RegExp(`[?&]site=${MAP.charger}`));
  await expect(page.getByRole("heading", { level: 2, name: MAP.charger })).toBeVisible();

  // The other region has a substation and nothing below it.
  await page.getByRole("link", { name: "VIC", exact: true }).click();
  await expect(page).toHaveURL(/[?&]region=VIC/);
  await expect(page.getByRole("link", { name: "Jemena Footscray Zone" })).toBeVisible();
  await expect(table.getByRole("row")).toHaveCount(1);
});

test("with more than one feeder the header offers the choice, and the choice is kept", async ({
  page,
  api,
}) => {
  api.feeders = 2;
  await open(page, "/sites", NMI.enrolled);
  const picker = page.getByLabel("Feeder");
  await expect(picker).toHaveValue("LV10");
  // With the choice in it, the header still fits: nothing of it is cut off
  // at the edge of a phone.
  const width = page.viewportSize()!.width;
  for (const part of [picker, page.getByRole("button", { name: /^Theme:/ })]) {
    const box = (await part.boundingBox())!;
    expect(box.x).toBeGreaterThanOrEqual(0);
    expect(box.x + box.width).toBeLessThanOrEqual(width);
  }
  const clock = (await page.getByText("Feeder time").boundingBox())!;
  expect(clock.x + clock.width).toBeLessThanOrEqual(width);
  await picker.selectOption("SUB-007-LV1");
  await expect(page).toHaveURL(/[?&]feeder=SUB-007-LV1/);
  await expect(page.getByText("AEDT").or(page.getByText("AEST")).first()).toBeVisible();
  // On another page, with no feeder in its address, the choice still holds.
  await page.getByRole("link", { name: "Config" }).click();
  await expect(page.getByRole("heading", { level: 1 })).toContainText("Envelope config");
  await expect(page.getByLabel("Feeder")).toHaveValue("SUB-007-LV1");
});

test("with one feeder there is nothing to choose", async ({ page, api }) => {
  void api;
  await open(page, "/sites", NMI.enrolled);
  await expect(page.getByLabel("Feeder")).toHaveCount(0);
});

test("the network is drawn as the engine solved it, at each of its three operating points", async ({
  page,
  api,
  errors,
}) => {
  void api;
  await open(page, "/network", "Closest to a limit");
  const drawing = page.getByRole("img", { name: /500 metres of cable away/ });
  await expect(drawing).toBeVisible();
  // What limits the feeder is said in words, before the drawing.
  await expect(page.getByText("Export is limited by high voltage at XDLAB000022")).toBeVisible();
  // The forecast: everything inside its limits.
  const far = drawing.locator('[data-bus="B3"]');
  await expect(far).toHaveAttribute("data-level", "ok");
  await expect(drawing.locator('[data-line="L_far"]')).toHaveAttribute("data-level", "ok");
  // Dashes move along each line, away from the transformer: the way the
  // forecast's power flows. They can be stopped, from the address.
  const flow = drawing.locator('.flow[data-flow="L_far"]');
  await expect(flow).toHaveAttribute("data-direction", "out");
  expect(await flow.evaluate((el) => getComputedStyle(el).animationName)).not.toBe("none");
  await page.getByRole("link", { name: "Stop the moving dashes" }).click();
  await expect(page).toHaveURL(/[?&]flow=off/);
  await expect(drawing.locator(".flow")).toHaveCount(0);
  await expect(drawing.locator(".arrow")).toHaveCount(2);
  await page.getByRole("link", { name: "Show the flow moving" }).click();
  await expect(page).not.toHaveURL(/flow=/);
  await expect(drawing.locator(".flow")).toHaveCount(2);

  // At the fixed limit the far end is past the band, and its line past its
  // rating. The choice is in the address.
  await page.getByRole("link", { name: "At the fixed limit" }).click();
  await expect(page).toHaveURL(/[?&]point=static/);
  await expect(far).toHaveAttribute("data-level", "critical");
  await expect(drawing.locator('[data-line="L_far"]')).toHaveAttribute("data-level", "warn");
  // There the power runs back towards the transformer, and so do the dashes.
  await expect(flow).toHaveAttribute("data-direction", "back");
  const table = page.getByRole("region", { name: "Closest to a limit" });
  await expect(table.getByRole("row").nth(1)).toContainText("B3");
  await expect(table.getByRole("row").nth(1)).toContainText("Past its limit");

  // A mark is chosen from the table with the keyboard, and described.
  await table.getByRole("link", { name: "L_far" }).focus();
  await page.keyboard.press("Enter");
  await expect(page).toHaveURL(/[?&]mark=/);
  await expect(page.getByRole("heading", { level: 2, name: "Line L_far" })).toBeVisible();
  await expect(
    page.getByText(/towards the transformer, 84\u00a0% of its rating/).last(),
  ).toBeVisible();
  await expect(drawing.locator('[data-line="L_far"]')).toHaveAttribute("data-selected", "");

  // And from the drawing, with the pointer.
  await far.locator(".hit").click();
  await expect(page.getByRole("heading", { level: 2, name: "Bus B3" })).toBeVisible();
  await expect(page.getByText(/Phase to neutral: 259\.9\u00a0V/)).toBeVisible();
  expect(errors).toEqual([]);
});

test("the network's drawing zooms from its buttons and its keys, and the part on show is in the address", async ({
  page,
  api,
}) => {
  void api;
  await open(page, "/network", "Closest to a limit");
  const sheet = page
    .getByRole("img", { name: /500 metres of cable away/ })
    .locator("svg")
    .first();
  await expect(sheet).toHaveAttribute("viewBox", "0 0 1000 100");
  await page.getByRole("button", { name: "Zoom in" }).click();
  await expect(page).toHaveURL(/[?&]view=\d+(%2C|,)\d+(%2C|,)1\.5/);
  await expect(sheet).not.toHaveAttribute("viewBox", "0 0 1000 100");
  // Shared as a link: the same part of the drawing.
  await page.goto("/network?view=250,25,2");
  await expect(page.getByText("Constrained")).toBeVisible();
  await expect(sheet).toHaveAttribute("viewBox", "250 25 500 50");
  // The keys, on the drawing's box: along, and back out to the whole.
  await page.getByRole("region", { name: "Schematic of the feeder" }).focus();
  await page.keyboard.press("ArrowRight");
  await expect(sheet).toHaveAttribute("viewBox", "350 25 500 50");
  await page.keyboard.press("0");
  await expect(page).not.toHaveURL(/view=/);
  await expect(sheet).toHaveAttribute("viewBox", "0 0 1000 100");
  await expect(page.getByRole("button", { name: "Show the whole feeder" })).toBeDisabled();
});

test("the network's instant can be moved with a slider, which says the time it is at", async ({
  page,
  api,
}) => {
  void api;
  await open(page, "/network", "Closest to a limit");
  const slider = page.getByRole("slider", { name: "The instant on show" });
  await expect(slider).toHaveAttribute("aria-valuetext", /^\w{3} \d{1,2} \w{3}, \d\d:\d\d$/);
  const now = Number(await slider.inputValue());
  expect(now % 1800).toBe(0);
  const asked = page.waitForRequest(
    (r) => r.url().includes("GetFeederState") && /at/.test(r.url()),
  );
  await slider.focus();
  await page.keyboard.press("ArrowRight");
  await expect(page).toHaveURL(new RegExp(`[?&]at=${now + 1800}`));
  await asked;
  await expect(slider).toHaveValue(String(now + 1800));
  // Back at now there is no instant in the address: the page follows the clock.
  await page.keyboard.press("ArrowLeft");
  await expect(page).not.toHaveURL(/[?&]at=/);
  await expect(
    page.getByRole("navigation", { name: "Time" }).getByRole("link", { name: "Now" }),
  ).toHaveAttribute("aria-current", "true");
});

test("the network can be looked at half an hour on, and back at now, from the address", async ({
  page,
  api,
}) => {
  void api;
  await open(page, "/network", "Closest to a limit");
  const time = page.getByRole("navigation", { name: "Time" });
  await expect(time.getByRole("link", { name: "Now" })).toHaveAttribute("aria-current", "true");

  const asked = page.waitForRequest(
    (r) => r.url().includes("GetFeederState") && /at/.test(r.url()),
  );
  await time.getByRole("link", { name: "30 minutes later" }).click();
  await expect(page).toHaveURL(/[?&]at=\d+/);
  await asked;
  // On a half-hour boundary, later than now.
  const at = Number(new URL(page.url()).searchParams.get("at"));
  expect(at % 1800).toBe(0);
  expect(at * 1000).toBeGreaterThan(Date.now());
  await expect(time.getByRole("link", { name: "Now" })).not.toHaveAttribute("aria-current");
  await expect(page.getByRole("img", { name: /500 metres of cable away/ })).toBeVisible();

  await time.getByRole("link", { name: "30 minutes earlier" }).click();
  await expect(page).toHaveURL(new RegExp(`[?&]at=${at - 1800}`));
  await time.getByRole("link", { name: "Now" }).click();
  await expect(page).not.toHaveURL(/[?&]at=/);
});

// The demo runs feeder time at sixty times the wall clock, and a page often
// has its feeder before the API has said so. These are the pages in that
// order of arrival, and at the size of the real feeder and fleet.
test.describe("at the demo's speed", () => {
  test.beforeEach(({ api }) => {
    api.speed = 60;
    api.clockDelayMs = 400;
  });
  // Longer than the clock's answer takes, and shorter than a poll.
  const settle = (page: Page) => page.waitForTimeout(1200);

  test("the feeder's overview asks for each thing once when it opens", async ({ page, api }) => {
    await open(page, "/feeder", "Export: allowed and measured");
    await settle(page);
    expect(api.calls["TelemetryService/GetFeederSeries"]).toBe(1);
    expect(api.calls["TelemetryService/GetDailyReport"]).toBe(1);
    expect(api.calls["SiteService/ListSites"]).toBe(1);
  });

  test("the network asks for each thing once when it opens", async ({ page, api }) => {
    await open(page, "/network", "Closest to a limit");
    await settle(page);
    expect(api.calls["FeederService/ListFeederNodes"]).toBe(1);
    expect(api.calls["TelemetryService/GetFeederState"]).toBe(1);
  });

  test("a site's page and operations ask for each thing once when they open", async ({
    page,
    api,
  }) => {
    await open(page, `/sites/${NMI.breaching}`, "Envelope, forecast and telemetry");
    await settle(page);
    expect(api.calls["TelemetryService/GetSiteSeries"]).toBe(1);
    await open(page, "/operations", "Engine runs");
    await settle(page);
    expect(api.calls["EnvelopeRunService/ListEnvelopeRuns"]).toBe(1);
  });

  test("the map asks for the fleet's state again within seconds", async ({ page, api }) => {
    await open(page, "/?region=all", "Ausgrid Lidcombe Zone");
    // A minute of feeder time is a second here; the floor is five seconds.
    await expect
      .poll(() => api.calls["TelemetryService/GetFleetState"], { timeout: 9000 })
      .toBeGreaterThanOrEqual(2);
  });

  // How many times the page's content changed, counted from now.
  const countChanges = (page: Page) =>
    page.evaluate(() => {
      const seen = { changes: 0 };
      (window as unknown as { __seen: typeof seen }).__seen = seen;
      new MutationObserver((records) => (seen.changes += records.length)).observe(
        document.querySelector("main")!,
        { subtree: true, childList: true, attributes: true, characterData: true },
      );
    });
  const changes = (page: Page) =>
    page.evaluate(() => (window as unknown as { __seen: { changes: number } }).__seen.changes);

  test("a poll that changes nothing touches nothing on the network", async ({ page, api }) => {
    api.network = "large";
    await open(page, "/network", "Closest to a limit");
    await expect(page.locator(".schematic .bus")).toHaveCount(223);
    await settle(page);
    const asked = api.calls["TelemetryService/GetFeederState"]!;
    await countChanges(page);
    await expect
      .poll(() => api.calls["TelemetryService/GetFeederState"], { timeout: 15_000 })
      .toBeGreaterThanOrEqual(asked + 2);
    await page.waitForTimeout(300);
    expect(await changes(page)).toBe(0);
  });

  test("a poll that changes nothing touches nothing on the map", async ({ page, api }) => {
    api.fleet = "large";
    await open(page, "/?sub=SUB-001", "Ausgrid Lidcombe Zone");
    await expect(
      page.getByRole("region", { name: "Sites on the map" }).getByRole("row"),
    ).toHaveCount(77);
    await settle(page);
    const asked = api.calls["TelemetryService/GetFleetState"]!;
    await countChanges(page);
    await expect
      .poll(() => api.calls["TelemetryService/GetFleetState"], { timeout: 15_000 })
      .toBeGreaterThanOrEqual(asked + 2);
    await page.waitForTimeout(300);
    expect(await changes(page)).toBe(0);
  });
});
