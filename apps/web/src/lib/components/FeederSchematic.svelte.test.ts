import { describe, expect, it, vi } from "vitest";
import { render } from "vitest-browser-svelte";
import type { LineMark, NodeMark, Schematic } from "$lib/map/schematic.ts";
import FeederSchematic from "./FeederSchematic.svelte";

const bus = (fields: Partial<NodeMark>): NodeMark => ({
  id: "tx",
  name: "B1",
  x: 28,
  y: 50,
  leaf: false,
  vPu: [1.04, 1.04, 1.04],
  level: "ok",
  label: "239.2 V",
  sites: [],
  enrolled: false,
  ...fields,
});
const wire = (fields: Partial<LineMark>): LineMark => ({
  id: "l-main",
  name: "main",
  points: [
    [28, 50],
    [28, 50],
    [600, 50],
  ],
  isSwitch: false,
  loadingPct: 30,
  powerW: 6000,
  weight: 1,
  level: "ok",
  label: "6.0 kW, 30 % of its rating",
  ...fields,
});
const view: Schematic = {
  nodes: [
    bus({}),
    bus({ id: "mid", name: "B2", x: 600, level: "warn", label: "251.2 V" }),
    bus({
      id: "far",
      name: "B3",
      x: 972,
      level: "critical",
      label: "259.9 V",
      sites: ["NMI00000017"],
      enrolled: true,
    }),
    bus({ id: "house", name: "B4", x: 640, y: 34, leaf: true, sites: ["XDLAB000014"] }),
  ],
  lines: [
    wire({}),
    wire({
      id: "l-far",
      name: "L_far",
      points: [
        [600, 50],
        [600, 50],
        [972, 50],
      ],
      powerW: -3000,
      weight: 0.5,
      level: "critical",
      label: "3.0 kW towards the transformer, 132 % of its rating",
    }),
    wire({
      id: "l-house",
      name: "Switch_1",
      points: [
        [600, 50],
        [600, 34],
        [640, 34],
      ],
      isSwitch: true,
      loadingPct: undefined,
      powerW: 40,
      weight: 0.01,
      label: "0.0 kW",
    }),
  ],
  width: 1000,
  height: 100,
  lengthM: 500,
  solved: true,
  binding: { words: "high voltage at NMI00000017", nodeId: "far" },
  worst: [],
};

const g = (kind: "bus" | "line", name: string) =>
  document.querySelector<SVGGElement>(`[data-${kind}="${name}"]`)!;

describe("FeederSchematic", () => {
  it("is a named drawing in a box that scrolls on its own, with a scale", async () => {
    const screen = await render(FeederSchematic, { view, onselect: () => {} });
    await expect
      .element(screen.getByRole("region", { name: "Schematic of the feeder" }))
      .toBeVisible();
    const drawing = screen.getByRole("img", { name: /500 metres of cable away/ });
    await expect.element(drawing).toBeVisible();
    expect(drawing.element().getAttribute("viewBox")).toBe("0 0 1000 100");
    await expect.element(screen.getByText("0 m", { exact: true })).toBeVisible();
    await expect.element(screen.getByText("500 m of cable from the transformer")).toBeVisible();
  });

  it("draws each bus by what it is, coloured by its level, and names it for the pointer", async () => {
    await render(FeederSchematic, { view, onselect: () => {} });
    // The transformer is a square; a site that takes part is a ring; a
    // junction is the smallest spot.
    expect(g("bus", "B1").querySelector("rect.dot")).not.toBeNull();
    expect(g("bus", "B3").querySelector("circle.dot.enrolled")!.getAttribute("r")).toBe("6.5");
    expect(g("bus", "B4").querySelector("circle.dot")!.getAttribute("r")).toBe("4");
    expect(g("bus", "B2").querySelector("circle.dot")!.getAttribute("r")).toBe("2.5");
    expect(g("bus", "B3").dataset.level).toBe("critical");
    expect(g("bus", "B3").querySelector("title")!.textContent).toBe("B3 (NMI00000017): 259.9 V");
    expect(g("bus", "B2").querySelector("title")!.textContent).toBe("B2: 251.2 V");
    // Colour comes from the level, through the design tokens.
    const colour = (el: Element) => getComputedStyle(el).color;
    expect(colour(g("bus", "B3"))).not.toBe(colour(g("bus", "B1")));
    expect(colour(g("bus", "B2"))).not.toBe(colour(g("bus", "B3")));
  });

  it("draws a line as heavy as its power, with an arrow for a flow worth showing", async () => {
    await render(FeederSchematic, { view, onselect: () => {} });
    const width = (name: string) =>
      g("line", name).querySelector(".wire")!.getAttribute("stroke-width");
    expect(width("main")).toBe("6.5");
    expect(width("L_far")).toBe("4");
    expect(g("line", "main").querySelector(".wire")!.getAttribute("points")).toBe(
      "28,50 28,50 600,50",
    );
    // Forward points right; back towards the transformer points left; a
    // trickle has no arrow.
    expect(g("line", "main").querySelector(".arrow")!.getAttribute("d")).toBe("M-4 -5 4 0 -4 5");
    expect(g("line", "L_far").querySelector(".arrow")!.getAttribute("d")).toBe("M4 -5 -4 0 4 5");
    expect(g("line", "L_far").querySelector(".arrow")!.getAttribute("transform")).toBe(
      "translate(786 50)",
    );
    expect(g("line", "Switch_1").querySelector(".arrow")).toBeNull();
    expect(g("line", "Switch_1").querySelector(".wire.switch")).not.toBeNull();
    expect(g("line", "L_far").querySelector("title")!.textContent).toBe(
      "L_far: 3.0 kW towards the transformer, 132 % of its rating",
    );
  });

  it("rings what limits the feeder: a bus, or a line", async () => {
    const screen = await render(FeederSchematic, { view, onselect: () => {} });
    const halo = () => document.querySelectorAll(".halo");
    expect(halo()).toHaveLength(1);
    expect(halo()[0]!.tagName).toBe("circle");
    expect(halo()[0]!.getAttribute("cx")).toBe("972");

    await screen.rerender({
      view: { ...view, binding: { words: "the rating of line L_far", lineId: "l-far" } },
    });
    expect(halo()).toHaveLength(1);
    expect(halo()[0]!.tagName).toBe("polyline");
    // A limit at a place the drawing does not have, and no limit at all.
    await screen.rerender({
      view: { ...view, binding: { words: "low voltage at NOWHERE", nodeId: "gone" } },
    });
    expect(halo()).toHaveLength(0);
    await screen.rerender({
      view: { ...view, binding: { words: "the rating of line X", lineId: "gone" } },
    });
    expect(halo()).toHaveLength(0);
    await screen.rerender({ view: { ...view, binding: undefined } });
    expect(halo()).toHaveLength(0);
  });

  it("selects a mark on a click, and shows which is selected", async () => {
    const onselect = vi.fn();
    const screen = await render(FeederSchematic, { view, selected: "far", onselect });
    expect(g("bus", "B3").hasAttribute("data-selected")).toBe(true);
    expect(document.querySelectorAll("[data-selected]")).toHaveLength(1);

    g("bus", "B2")
      .querySelector(".hit")!
      .dispatchEvent(new MouseEvent("click", { bubbles: true }));
    expect(onselect).toHaveBeenLastCalledWith("bus", "mid");
    g("line", "L_far")
      .querySelector(".hit")!
      .dispatchEvent(new MouseEvent("click", { bubbles: true }));
    expect(onselect).toHaveBeenLastCalledWith("line", "l-far");

    await screen.rerender({ selected: "l-far" });
    expect(g("line", "L_far").hasAttribute("data-selected")).toBe(true);
    expect(g("bus", "B3").hasAttribute("data-selected")).toBe(false);
  });

  it("draws an empty feeder as an empty drawing", async () => {
    const screen = await render(FeederSchematic, {
      view: { ...view, nodes: [], lines: [], lengthM: 0, binding: undefined },
      onselect: () => {},
    });
    await expect.element(screen.getByRole("img")).toBeVisible();
    expect(document.querySelectorAll(".dot, .wire, .scale")).toHaveLength(0);
  });
});
