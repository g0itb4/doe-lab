import { flushSync } from "svelte";
import { describe, expect, it, vi } from "vitest";
import { render } from "vitest-browser-svelte";
import { hovercard } from "$lib/hovercard.svelte.ts";
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
// A line, with what the drawing is given ready-made: its corners as the
// points of a polyline, and the place of its arrow.
const wire = ({
  points = [
    [28, 50],
    [28, 50],
    [600, 50],
  ],
  ...fields
}: Partial<LineMark>): LineMark => ({
  id: "l-main",
  name: "main",
  points,
  path: points.map(([x, y]) => `${x},${y}`).join(" "),
  arrow: { x: (points[1]![0] + points[2]![0]) / 2, y: points[2]![1] },
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
  it("is a named drawing in a box of its own, as wide as the box, with a scale", async () => {
    const screen = await render(FeederSchematic, { view, onselect: () => {} });
    await expect
      .element(screen.getByRole("region", { name: "Schematic of the feeder" }))
      .toBeVisible();
    const drawing = screen.getByRole("img", { name: /500 metres of cable away/ });
    await expect.element(drawing).toBeVisible();
    // Three sheets of one drawing, each with the same box, and none of them
    // a picture of its own to a screen reader.
    const sheets = [...drawing.element().querySelectorAll("svg")];
    expect(sheets.map((sheet) => sheet.getAttribute("viewBox"))).toEqual(
      Array(3).fill("0 0 1000 100"),
    );
    expect(sheets.every((sheet) => sheet.getAttribute("aria-hidden") === "true")).toBe(true);
    const [lines, dashes, buses] = sheets.map((sheet) => sheet.getBoundingClientRect());
    for (const sheet of [dashes!, buses!]) {
      expect(sheet.left).toBeCloseTo(lines!.left, 0);
      expect(sheet.top).toBeCloseTo(lines!.top, 0);
      expect(sheet.width).toBeCloseTo(lines!.width, 0);
      expect(sheet.height).toBeCloseTo(lines!.height, 0);
    }
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
    // To the pointer a bus says what it is on a card: its status in words,
    // its voltage, and the sites at it.
    const over = (el: Element) => {
      el.dispatchEvent(new MouseEvent("mouseenter", { clientX: 30, clientY: 30 }));
      return hovercard.card;
    };
    expect(over(g("bus", "B3"))).toEqual({
      title: "Bus B3",
      lines: ["Past a limit", "259.9 V", "Sites here: NMI00000017"],
      level: "critical",
    });
    expect(over(g("bus", "B2"))).toEqual({
      title: "Bus B2",
      lines: ["Near a limit", "251.2 V"],
      level: "warn",
    });
    g("bus", "B2").dispatchEvent(new MouseEvent("mouseleave"));
    expect(hovercard.card).toBeUndefined();
    expect(document.querySelector(".schematic title")).toBeNull();
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
    g("line", "L_far").dispatchEvent(new MouseEvent("mouseenter", { clientX: 30, clientY: 30 }));
    expect(hovercard.card).toEqual({
      title: "Line L_far",
      lines: ["Past a limit", "3.0 kW towards the transformer, 132 % of its rating"],
      level: "critical",
    });
    g("line", "L_far").dispatchEvent(new MouseEvent("mouseleave"));
  });

  it("moves dashes along a line the way its power flows, quicker the more it carries", async () => {
    const screen = await render(FeederSchematic, { view, onselect: () => {} });
    const flow = (name: string) =>
      document.querySelector<SVGPolylineElement>(`.flow[data-flow="${name}"]`);
    expect(flow("main")!.dataset.direction).toBe("out");
    expect(flow("L_far")!.dataset.direction).toBe("back");
    // Along the wire itself, and thinner than it.
    expect(flow("main")!.getAttribute("points")).toBe("28,50 28,50 600,50");
    expect(Number(flow("main")!.getAttribute("stroke-width"))).toBeCloseTo(2.275);
    expect(flow("L_far")!.getAttribute("stroke-width")).toBe("1.4");
    const style = (name: string) => getComputedStyle(flow(name)!);
    expect(style("main").animationDuration).toBe("0.6s");
    expect(style("L_far").animationDuration).toBe("1.5s");
    expect(style("main").animationIterationCount).toBe("infinite");
    // One set of keyframes for each direction.
    expect(style("main").animationName).not.toBe("none");
    expect(style("main").animationName).not.toBe(style("L_far").animationName);
    // The dashes never take a click from the line under them.
    expect(style("main").pointerEvents).toBe("none");
    // A trickle has none, and neither has a switch, which is dashed already.
    expect(flow("Switch_1")).toBeNull();
    await screen.rerender({
      view: {
        ...view,
        lines: [view.lines[0]!, { ...view.lines[2]!, weight: 0.6, powerW: 3600 }],
      },
    });
    expect(flow("Switch_1")).toBeNull();
    expect(g("line", "Switch_1").querySelector(".arrow")).not.toBeNull();
    // A thin line's dashes are still a pixel wide.
    await screen.rerender({
      view: { ...view, lines: [view.lines[0]!, { ...view.lines[1]!, weight: 0.1 }] },
    });
    expect(flow("L_far")!.getAttribute("stroke-width")).toBe("1");

    // The motion can be stopped: the arrows remain.
    await screen.rerender({ view, animate: false });
    expect(document.querySelectorAll(".flow")).toHaveLength(0);
    expect(document.querySelectorAll(".arrow")).toHaveLength(2);
  });

  it("moves the dashes of the busiest lines only, on a sheet of their own", async () => {
    // Thirty lines that each carry enough to show, the later the heavier.
    const many = Array.from({ length: 30 }, (_, i) =>
      wire({ id: `l-${i}`, name: `L${i}`, weight: 0.2 + i * 0.02 }),
    );
    await render(FeederSchematic, { view: { ...view, lines: many }, onselect: () => {} });
    const moving = [...document.querySelectorAll<SVGPolylineElement>(".flow")];
    expect(moving).toHaveLength(24);
    // The six lightest stand still; every line keeps its arrow.
    const names = moving.map((flow) => flow.dataset.flow);
    expect(names).not.toContain("L5");
    expect(names).toContain("L6");
    expect(names[0]).toBe("L29");
    expect(document.querySelectorAll(".arrow")).toHaveLength(30);
    // The dashes are not in the sheet of the lines: painting them leaves the
    // lines alone.
    const sheet = moving[0]!.closest("svg")!;
    expect(sheet.querySelector(".wire")).toBeNull();
    expect(getComputedStyle(sheet).willChange).toBe("transform");
  });

  it("lets the pointer through the sheets above a line, to the line", async () => {
    const onselect = vi.fn();
    await render(FeederSchematic, { view, onselect });
    // The middle of the first line, where no bus is.
    const hit = g("line", "main").querySelector(".hit")!.getBoundingClientRect();
    const [x, y] = [hit.left + hit.width / 2, hit.top + hit.height / 2];
    const under = document.elementFromPoint(x, y)!;
    expect(under.closest("[data-line]")).toBe(g("line", "main"));
    under.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    expect(onselect).toHaveBeenLastCalledWith("line", "l-main");
    // And a bus, on the top sheet, takes the pointer itself: where two lines
    // meet under it, it is the bus that is chosen.
    // The drawing is wider than a narrow window: bring the bus into view.
    g("bus", "B2").scrollIntoView({ inline: "center" });
    const dot = g("bus", "B2").querySelector(".hit")!.getBoundingClientRect();
    const top = document.elementFromPoint(dot.left + dot.width / 2, dot.top + dot.height / 2)!;
    expect(top.closest("[data-bus]")).toBe(g("bus", "B2"));
    top.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    expect(onselect).toHaveBeenLastCalledWith("bus", "mid");
  });

  // The box of the first sheet, as numbers: x, y, width, height. What a
  // gesture did is put on the page first.
  const box = () => {
    flushSync();
    return document
      .querySelector(".schematic svg")!
      .getAttribute("viewBox")!
      .split(" ")
      .map(Number);
  };
  const settled = () => new Promise((r) => setTimeout(r, 260));

  it("zooms and fits from its buttons, and all three sheets move as one", async () => {
    const onview = vi.fn();
    const screen = await render(FeederSchematic, { view, onselect: () => {}, onview });
    const [closer, further, whole] = [
      screen.getByRole("button", { name: "Zoom in" }),
      screen.getByRole("button", { name: "Zoom out" }),
      screen.getByRole("button", { name: "Show the whole feeder" }),
    ];
    // With the whole feeder on show there is nothing to zoom out to.
    await expect.element(further).toBeDisabled();
    await expect.element(whole).toBeDisabled();
    await closer.click();
    // Half as close again, about the middle.
    expect(onview).toHaveBeenLastCalledWith({
      x: expect.closeTo(500 / 3, 6),
      y: expect.closeTo(50 / 3, 6),
      k: 1.5,
    });
    expect(box()[2]).toBeCloseTo(1000 / 1.5, 1);
    const boxes = [...document.querySelectorAll(".schematic svg")].map((sheet) =>
      sheet.getAttribute("viewBox"),
    );
    expect(new Set(boxes).size).toBe(1);
    await expect.element(further).toBeEnabled();
    await further.click();
    expect(onview).toHaveBeenLastCalledWith({ x: 0, y: 0, k: 1 });
    // As close as it goes, and no closer.
    for (let i = 0; i < 6; i++) await closer.click();
    await expect.element(closer).toBeDisabled();
    expect(box()[2]).toBe(125);
    await whole.click();
    expect(box()).toEqual([0, 0, 1000, 100]);
    // The buttons are targets of at least 24 px.
    const size = closer.element().getBoundingClientRect();
    expect(Math.min(size.width, size.height)).toBeGreaterThanOrEqual(24);
  });

  it("shows the part its page gives it, and gives up its own once the page has answered", async () => {
    const screen = await render(FeederSchematic, {
      view,
      onselect: () => {},
      zoom: { x: 250, y: 25, k: 2 },
    });
    expect(box()).toEqual([250, 25, 500, 50]);
    await screen.getByRole("button", { name: "Zoom out" }).click();
    expect(box()[2]).toBeCloseTo(750, 0);
    await screen.rerender({ zoom: { x: 0, y: 0, k: 4 } });
    expect(box()).toEqual([0, 0, 250, 25]);
  });

  it("zooms and moves from the keyboard, when the keys are pressed on the drawing", async () => {
    const screen = await render(FeederSchematic, { view, onselect: () => {} });
    const region = screen.getByRole("region", { name: "Schematic of the feeder" });
    await expect
      .element(region)
      .toHaveAccessibleDescription(/Plus and minus zoom the drawing.*arrow keys move it/);
    const key = (name: string, target: Element = region.element()) => {
      const event = new KeyboardEvent("keydown", { key: name, bubbles: true, cancelable: true });
      target.dispatchEvent(event);
      return event;
    };
    expect(key("+").defaultPrevented).toBe(true);
    key("=");
    expect(box()[2]).toBeCloseTo(1000 / 2.25, 1);
    const [x, y] = box();
    // A fifth of what is on show, each way.
    key("ArrowRight");
    expect(box()[0]).toBeCloseTo(x! + 1000 / 2.25 / 5, 1);
    key("ArrowLeft");
    key("ArrowDown");
    expect(box()[1]).toBeCloseTo(y! + 100 / 2.25 / 5, 1);
    key("ArrowUp");
    expect(box()[0]).toBeCloseTo(x!, 1);
    expect(box()[1]).toBeCloseTo(y!, 1);
    key("-");
    expect(box()[2]).toBeCloseTo(1000 / 1.5, 1);
    key("0");
    expect(box()).toEqual([0, 0, 1000, 100]);
    // Another key is the page's; and a key on a button is the button's.
    expect(key("a").defaultPrevented).toBe(false);
    const button = screen.getByRole("button", { name: "Zoom in" }).element();
    expect(key("+", button).defaultPrevented).toBe(false);
    expect(box()).toEqual([0, 0, 1000, 100]);
  });

  it("zooms about the pointer with Ctrl and the wheel, and is dragged along when zoomed in", async () => {
    const onview = vi.fn();
    const onselect = vi.fn();
    await render(FeederSchematic, { view, onselect, onview });
    const sheet = document.querySelector(".schematic") as HTMLElement;
    const rect = sheet.getBoundingClientRect();
    const at = (across: number, down: number) => ({
      clientX: rect.left + rect.width * across,
      clientY: rect.top + rect.height * down,
      bubbles: true,
      cancelable: true,
    });
    // A plain wheel is the page's.
    const plain = new WheelEvent("wheel", { deltaY: -300, ...at(0.25, 0.5) });
    sheet.dispatchEvent(plain);
    expect(plain.defaultPrevented).toBe(false);
    expect(box()).toEqual([0, 0, 1000, 100]);

    // A quarter of the way across stays a quarter of the way across.
    const zoomIn = new WheelEvent("wheel", { deltaY: -300, ctrlKey: true, ...at(0.25, 0.5) });
    sheet.dispatchEvent(zoomIn);
    expect(zoomIn.defaultPrevented).toBe(true);
    const [x, , width] = box();
    expect(width).toBeLessThan(500);
    expect((250 - x!) / width!).toBeCloseTo(0.25, 2);
    // The page is told when the wheel has stopped.
    expect(onview).not.toHaveBeenCalled();
    await settled();
    expect(onview).toHaveBeenCalledOnce();

    // A drag moves the drawing with the pointer, and its last click selects
    // nothing.
    const mouse = (target: EventTarget, type: string, across: number, down: number) =>
      target.dispatchEvent(new MouseEvent(type, { button: 0, ...at(across, down) }));
    mouse(sheet, "mousedown", 0.5, 0.5);
    mouse(document, "mousemove", 0.501, 0.5);
    expect(box()[0]).toBe(x);
    mouse(document, "mousemove", 0.3, 0.5);
    // To within a pixel of the pointer: a pointer is at whole pixels.
    expect(Math.abs(box()[0]! - (x! + 0.2 * width!))).toBeLessThan(3);
    mouse(document, "mouseup", 0.3, 0.5);
    expect(onview).toHaveBeenCalledTimes(2);
    g("line", "main")
      .querySelector(".hit")!
      .dispatchEvent(new MouseEvent("click", { bubbles: true }));
    expect(onselect).not.toHaveBeenCalled();
    // The next click, with no drag before it, selects as it always did.
    g("line", "main")
      .querySelector(".hit")!
      .dispatchEvent(new MouseEvent("click", { bubbles: true }));
    expect(onselect).toHaveBeenLastCalledWith("line", "l-main");
    // A press with no move is not a drag, and another button is not a press.
    mouse(sheet, "mousedown", 0.5, 0.5);
    mouse(document, "mouseup", 0.5, 0.5);
    sheet.dispatchEvent(new MouseEvent("mousedown", { button: 2, ...at(0.5, 0.5) }));
    expect(onview).toHaveBeenCalledTimes(2);
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
