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
  { path: "/", h1: "Feeder overview", nav: "Overview", ready: "Export: allowed and measured" },
  { path: "/map", h1: "Map", nav: "Map", ready: "Ausgrid Lidcombe Zone" },
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

async function open(page: Page, path: string, ready: string) {
  await page.goto(path);
  await expect(page.getByText(ready).first()).toBeVisible();
  // The charts are drawn after their library has loaded.
  if (path === "/" || path.startsWith("/sites/"))
    await expect(page.locator("canvas").first()).toBeVisible();
  // The network is drawn once its state has arrived.
  if (path.startsWith("/network")) await expect(page.getByText("Constrained")).toBeVisible();
  // The map is drawn after its library has loaded.
  if (path.startsWith("/map"))
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

test("the overview states the status in words, and the figures, before any chart", async ({
  page,
  api,
}) => {
  void api;
  await open(page, "/", "Export: allowed and measured");
  await expect(
    page.getByText("Constrained: high voltage at XDLAB000022", { exact: true }),
  ).toBeVisible();
  await expect(page.getByRole("link", { name: "1 open alert" })).toBeVisible();
  const figures = page.locator("dl").first();
  await expect(figures).toContainText("2.8 kW");
  await expect(figures).toContainText("of 3.5 kW allowed");
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
  await expect(page.getByText("Export: allowed and measured")).toBeVisible();
  await expect(page.getByText("What you are looking at.")).toBeHidden();
});

test("each chart has a summary and a table with the same series", async ({ page, api }) => {
  void api;
  await open(page, "/", "Export: allowed and measured");
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

test("the time range lives in the address", async ({ page, api }) => {
  void api;
  await open(page, "/", "Export: allowed and measured");
  await page
    .getByRole("navigation", { name: "Time range" })
    .getByRole("link", { name: "6 hours" })
    .click();
  await expect(page).toHaveURL(/\?range=6h$/);
  await expect(page.locator('nav[aria-label="Time range"] a[aria-current="true"]')).toHaveText(
    "6 hours",
  );
  // Shared as a link: the same view.
  await page.goto("/?range=3d");
  await expect(page.locator('nav[aria-label="Time range"] a[aria-current="true"]')).toHaveText(
    "3 days",
  );
});

test("when live updates stop, a banner says so and the figures stay; then they resume", async ({
  page,
  api,
}) => {
  await open(page, "/", "Export: allowed and measured");
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
  await page.goto("/");
  await expect(loading).toBeVisible();
  expect(await animated()).toEqual([]);
  api.delayMs = 0;
  await expect(page.locator("canvas").first()).toBeVisible();
  await page.getByRole("button", { name: /^Theme:/ }).hover();
  expect(await animated()).toEqual([]);
});

test("a page keeps its layout while it loads", async ({ page, api }) => {
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
  await open(page, "/", "Export: allowed and measured");
  await page.waitForTimeout(500);
  const cls = await page.evaluate(() => (window as unknown as { __cls?: number }).__cls ?? 0);
  expect(cls, "cumulative layout shift").toBeLessThan(0.1);
});

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
    await open(page, "/", "Export: allowed and measured");
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
  await open(page, "/", "Export: allowed and measured");
  await expect(page.getByRole("button", { name: "Ask", exact: true })).toHaveCount(0);
});

test("the map shows the substations, and a substation's sites when it is chosen", async ({
  page,
  api,
  errors,
}) => {
  await open(page, "/map?region=all", "Ausgrid Lidcombe Zone");
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
  await expect(page.getByText("2 of 3 sites reporting, exporting")).toBeVisible();
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
  await expect(page.getByRole("link", { name: "Open the site" })).toHaveAttribute(
    "href",
    `/sites/${MAP.over}?feeder=LV10`,
  );
  expect(errors).toEqual([]);
});

test("every site of the map can be reached from the keyboard, through the table", async ({
  page,
  api,
}) => {
  void api;
  await open(page, "/map?region=all", "Ausgrid Lidcombe Zone");
  const table = page.getByRole("region", { name: "Sites on the map" });
  await expect(table.getByRole("row")).toHaveCount(4);
  const row = table.getByRole("row", { name: new RegExp(MAP.charger) });
  await expect(row).toContainText("EV charger");
  await expect(row).toContainText("Not reporting");
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

  // At the fixed limit the far end is past the band, and its line past its
  // rating. The choice is in the address.
  await page.getByRole("link", { name: "At the fixed limit" }).click();
  await expect(page).toHaveURL(/[?&]point=static/);
  await expect(far).toHaveAttribute("data-level", "critical");
  await expect(drawing.locator('[data-line="L_far"]')).toHaveAttribute("data-level", "warn");
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
