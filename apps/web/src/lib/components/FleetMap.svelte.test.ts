import { afterEach, describe, expect, it, vi } from "vitest";
import { render } from "vitest-browser-svelte";
import type { FleetView, SiteMark, SubstationMark } from "$lib/map/fleet.ts";
import { hovercard } from "$lib/hovercard.svelte.ts";
import { theme } from "$lib/theme.svelte.ts";
import FleetMap, { siteHtml, SITES_FROM_ZOOM, substationHtml, TILES } from "./FleetMap.svelte";
import MapLegend from "./MapLegend.svelte";

afterEach(() => theme.set("system"));

// One transparent pixel for every tile: the test asks no other host for
// anything.
const tiles = {
  url: "data:image/gif;base64,R0lGODlhAQABAAAAACH5BAEKAAEALAAAAAABAAEAAAICTAEAOw==",
  attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a>',
};

const ok = { level: "ok", label: "Within limit", detail: "" } as const;
const substation = (fields: Partial<SubstationMark> = {}): SubstationMark => ({
  id: "sub-1",
  code: "SUB-001",
  name: "Ausgrid Lidcombe Zone",
  place: "Lidcombe",
  dnsp: "Ausgrid",
  state: "NSW",
  latitude: -33.8524,
  longitude: 151.0621,
  status: { level: "ok", label: "Normal", detail: "" },
  feeders: 1,
  feederCodes: ["LV10"],
  sites: 2,
  reporting: 2,
  use: undefined,
  fill: 0,
  ...fields,
});
const site = (fields: Partial<SiteMark> = {}): SiteMark => ({
  id: "s-1",
  nmi: "NMI00000017",
  latitude: -33.85,
  longitude: 151.078,
  feederCode: "LV10",
  substationCode: "SUB-001",
  feederIndex: 0,
  kind: "solar",
  status: ok,
  exportW: 1500,
  limitW: 3000,
  use: undefined,
  fill: 0.5,
  ...fields,
});
const view: FleetView = {
  substations: [substation({ fill: 0.8 })],
  sites: [
    site(),
    site({
      id: "s-2",
      nmi: "NMI00000025",
      latitude: -33.86,
      longitude: 151.05,
      kind: "ev",
      feederIndex: 1,
      status: { level: "critical", label: "Over limit", detail: "" },
    }),
    // On a feeder that hangs from no substation: a mark with no line.
    site({
      id: "s-3",
      nmi: "NMI00000900",
      substationCode: "",
      latitude: -33.855,
      longitude: 151.06,
    }),
  ],
};
// Around the substation and its sites: close enough for the sites to show.
const near: [[number, number], [number, number]] = [
  [-33.86, 151.05],
  [-33.85, 151.078],
];
const far: [[number, number], [number, number]] = [
  [-37.8, 144.9],
  [-33.7, 151.2],
];

const mark = (label: string) => document.querySelector<HTMLElement>(`[aria-label^="${label}"]`);

describe("the marks", () => {
  it("draw a site as its equipment in a ring, with its status on its shoulder unless all is well", () => {
    const well = siteHtml("solar", "ok", 0.5, false);
    expect(well).toContain('data-level="ok"');
    expect(well).not.toContain("fleet-badge");
    expect(well).not.toContain("data-selected");
    // Half of the ring's circumference, 2π × 14.
    expect(well).toContain('stroke-dasharray="44.0 88.0"');
    const over = siteHtml("ev", "critical", 1, true);
    expect(over).toContain("fleet-badge");
    expect(over).toContain("data-selected");
    expect(over).toContain('stroke-dasharray="88.0 88.0"');
    // Each kind has its own shape.
    const shapes = (["solar", "battery", "ev", "hybrid"] as const).map(
      (kind) => /fleet-glyph"[^>]* d="([^"]+)"/.exec(siteHtml(kind, "ok", 0, false))![1],
    );
    expect(new Set(shapes).size).toBe(4);
  });

  it("draw a substation as its status and its name, whatever the name holds", () => {
    const html = substationHtml('Crows <Nest> & "Co"', "warn", 0, true);
    expect(html).toContain('data-level="warn"');
    expect(html).toContain("data-selected");
    expect(html).toContain("Crows &#60;Nest&#62; &#38; &#34;Co&#34;");
    expect(substationHtml("Lidcombe", "ok", 0, false)).not.toContain("data-selected");
  });

  it("fill a substation's border as its sites use their limits", () => {
    // In hundredths of the border's own length, whatever that is.
    expect(substationHtml("Lidcombe", "ok", 0.8, false)).toContain(
      'pathLength="100" stroke-dasharray="80.0 100"',
    );
    expect(substationHtml("Lidcombe", "ok", 0, false)).toContain('stroke-dasharray="0.0 100"');
  });

  it("come from OpenStreetMap, which is named on the map", () => {
    expect(TILES.url).toBe("https://tile.openstreetmap.org/{z}/{x}/{y}.png");
    expect(TILES.attribution).toContain("OpenStreetMap");
  });
});

describe("FleetMap", () => {
  it("is a named region with the substations on it, and the sites from a closer zoom", async () => {
    const onselect = vi.fn();
    const screen = await render(FleetMap, { view, bounds: far, tiles, onselect });
    await expect.element(screen.getByRole("region", { name: "Map of the fleet" })).toBeVisible();
    await vi.waitFor(() => expect(mark("Ausgrid Lidcombe Zone")).not.toBeNull());
    // A substation is a button a keyboard reaches; its name says its status.
    const sub = mark("Ausgrid Lidcombe Zone")!;
    expect(sub.getAttribute("aria-label")).toBe("Ausgrid Lidcombe Zone: Normal");
    expect(sub.getAttribute("role")).toBe("button");
    expect(sub.tabIndex).toBe(0);
    expect(sub.textContent).toBe("Lidcombe");
    // Four fifths of its border is drawn in the colour of its status.
    const ring = sub.querySelector<SVGRectElement>("rect.fleet-ring")!;
    expect(ring.getAttribute("stroke-dasharray")).toBe("80.0 100");
    expect(getComputedStyle(ring).stroke).toBe(
      getComputedStyle(sub.querySelector(".fleet-substation")!).color,
    );
    expect(getComputedStyle(sub.querySelector("rect.fleet-disc")!).stroke).not.toBe(
      getComputedStyle(ring).stroke,
    );
    const box = sub.getBoundingClientRect();
    expect(Math.min(box.width, box.height)).toBeGreaterThanOrEqual(24);
    // From far away the sites are not drawn.
    expect(mark("NMI00000017")).toBeNull();
    // The map names where its streets come from.
    await expect.element(screen.getByRole("link", { name: "OpenStreetMap" })).toBeVisible();

    await screen.rerender({ bounds: near });
    await vi.waitFor(() => expect(mark("NMI00000017")).not.toBeNull());
    const solar = mark("NMI00000017")!;
    expect(solar.getAttribute("aria-label")).toBe("NMI00000017, Solar: Within limit");
    // Not in the tab order: the table under the map is the keyboard's way.
    expect(solar.tabIndex).toBe(-1);
    expect(solar.getAttribute("role")).toBe("img");
    expect(mark("NMI00000025")!.getAttribute("aria-label")).toBe(
      "NMI00000025, EV charger: Over limit",
    );
    expect(mark("NMI00000025")!.querySelector(".fleet-site")!.getAttribute("data-level")).toBe(
      "critical",
    );
    // A line from each site to its substation, in its feeder's style; none
    // for a site with no substation.
    const spokes = [...document.querySelectorAll(".fleet-spoke")];
    expect(spokes.map((s) => s.classList.contains("fleet-spoke-1"))).toEqual([false, true]);

    solar.click();
    expect(onselect).toHaveBeenLastCalledWith("site", "NMI00000017");
    sub.click();
    expect(onselect).toHaveBeenLastCalledWith("substation", "SUB-001");
    expect(SITES_FROM_ZOOM).toBe(12);
  });

  it("marks what is selected, redraws what changes and removes what has gone", async () => {
    const screen = await render(FleetMap, {
      view,
      bounds: near,
      tiles,
      selected: "NMI00000017",
      onselect: () => {},
    });
    await vi.waitFor(() => expect(mark("NMI00000017")).not.toBeNull());
    const selected = () => [...document.querySelectorAll("[data-selected]")];
    expect(selected()).toHaveLength(1);
    expect(mark("NMI00000017")!.querySelector("[data-selected]")).not.toBeNull();
    const untouched = mark("NMI00000025")!.innerHTML;

    // The substation is selected instead, a site changes its status, and
    // another leaves the fleet.
    await screen.rerender({
      selected: "SUB-001",
      view: {
        substations: [substation({ status: { level: "warn", label: "1 offline", detail: "" } })],
        sites: [
          site({ status: { level: "warn", label: "Offline", detail: "" }, fill: 0 }),
          view.sites[1]!,
        ],
      },
    });
    await vi.waitFor(() =>
      expect(mark("NMI00000017")!.getAttribute("aria-label")).toBe("NMI00000017, Solar: Offline"),
    );
    expect(mark("Ausgrid Lidcombe Zone")!.getAttribute("aria-label")).toBe(
      "Ausgrid Lidcombe Zone: 1 offline",
    );
    expect(mark("Ausgrid Lidcombe Zone")!.querySelector("[data-selected]")).not.toBeNull();
    expect(selected()).toHaveLength(1);
    expect(mark("NMI00000900")).toBeNull();
    expect(document.querySelectorAll(".fleet-spoke")).toHaveLength(2);
    // A mark that did not change was not drawn again.
    expect(mark("NMI00000025")!.innerHTML).toBe(untouched);

    // A status that looks the same and reads differently is renamed in place.
    const before = mark("NMI00000017")!;
    await screen.rerender({
      view: {
        substations: [
          substation({ status: { level: "warn", label: "1 at its limit", detail: "" } }),
        ],
        sites: [
          site({ status: { level: "warn", label: "At its limit", detail: "" }, fill: 0 }),
          view.sites[1]!,
        ],
      },
    });
    await vi.waitFor(() =>
      expect(before.getAttribute("aria-label")).toBe("NMI00000017, Solar: At its limit"),
    );
    // To the pointer the mark says the same on a card, with its status as it
    // is now; and a substation, which the keyboard reaches, says it on focus.
    expect(before.hasAttribute("title")).toBe(false);
    before.dispatchEvent(new MouseEvent("mouseenter", { clientX: 40, clientY: 40 }));
    expect(hovercard.card).toMatchObject({
      title: "NMI00000017, Solar",
      level: "warn",
      lines: ["At its limit", ""],
    });
    before.dispatchEvent(new MouseEvent("mouseleave"));
    mark("Ausgrid Lidcombe Zone")!.dispatchEvent(new FocusEvent("focus"));
    expect(hovercard.card).toMatchObject({
      title: "Ausgrid Lidcombe Zone",
      lines: ["1 at its limit", ""],
      level: "warn",
    });
    mark("Ausgrid Lidcombe Zone")!.dispatchEvent(new FocusEvent("blur"));
    expect(hovercard.card).toBeUndefined();
    expect(mark("Ausgrid Lidcombe Zone")!.getAttribute("aria-label")).toBe(
      "Ausgrid Lidcombe Zone: 1 at its limit",
    );
  });

  it("darkens its streets with the theme, and stays where it is with nothing to frame", async () => {
    const screen = await render(FleetMap, { view, bounds: undefined, tiles, onselect: () => {} });
    const region = screen.getByRole("region", { name: "Map of the fleet" }).element();
    await vi.waitFor(() => expect(region.querySelector(".leaflet-tile-pane")).not.toBeNull());
    theme.set("light");
    await vi.waitFor(() => expect(region.hasAttribute("data-night")).toBe(false));
    expect(getComputedStyle(region.querySelector(".leaflet-tile-pane")!).filter).toBe("none");
    theme.set("dark");
    await vi.waitFor(() => expect(region.hasAttribute("data-night")).toBe(true));
    expect(getComputedStyle(region.querySelector(".leaflet-tile-pane")!).filter).toContain(
      "invert",
    );
    // With no bounds the map shows the whole of the fleet's corner of the
    // country: the sites are not drawn.
    expect(mark("NMI00000017")).toBeNull();

    screen.unmount();
    expect(document.querySelector(".leaflet-tile-pane")).toBeNull();
  });
});

describe("MapLegend", () => {
  it("says what each status and each shape means", async () => {
    const screen = await render(MapLegend);
    const items = screen.getByRole("list", { name: "Status of a mark" }).element();
    expect([...items.querySelectorAll("li")].map((li) => li.textContent?.trim())).toEqual([
      "Within limit",
      "At its limit, or offline",
      "Over limit, or backstop",
      "Not reporting",
    ]);
    await expect.element(screen.getByText(/they are not the\s+routes of cables/)).toBeVisible();
  });
});
