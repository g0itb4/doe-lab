import { afterEach, describe, expect, it, vi } from "vitest";
import { render } from "vitest-browser-svelte";
import { views } from "$lib/chart-view.svelte.ts";
import { theme } from "$lib/theme.svelte.ts";
import Chart, { type ChartSeries } from "./Chart.svelte";

// Half-hourly points from 00:00 UTC on 3 November 2026: 11:00 in Sydney.
const start = Date.UTC(2026, 10, 3) / 1000;
const x = Array.from({ length: 8 }, (_, i) => start + i * 1800);
const series: ChartSeries[] = [
  {
    label: "Export limit",
    values: [3000, 2500, 2000, 1500, 1500, 2000, null, 3000],
    color: 1,
    stepped: true,
  },
  { label: "Fixed limit", values: x.map(() => 5000), color: "ref" },
];
const kw = (v: number) => `${(v / 1000).toFixed(1)} kW`;
const props = {
  title: "Export limits",
  summary: "The envelope against a fixed limit.",
  x,
  series,
  format: kw,
  zone: "Australia/Sydney",
};

afterEach(() => theme.set("system"));

// The canvas, once uPlot has drawn on it.
async function canvas(container: Element): Promise<HTMLCanvasElement> {
  await vi.waitFor(() => expect(container.querySelector("canvas")).not.toBeNull());
  return container.querySelector("canvas")!;
}

// The colours on the canvas, as "r,g,b" strings, most used first.
function colours(c: HTMLCanvasElement): Set<string> {
  const { data } = c.getContext("2d")!.getImageData(0, 0, c.width, c.height);
  const seen = new Set<string>();
  for (let i = 0; i < data.length; i += 4)
    if (data[i + 3] === 255) seen.add(`${data[i]},${data[i + 1]},${data[i + 2]}`);
  return seen;
}
const rgb = (hex: string) => [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16)).join(",");

const slider = (screen: { container: Element }) =>
  screen.container.querySelector('[role="slider"]') as HTMLElement;

describe("Chart", () => {
  it("draws its series in the theme's colours, with a title and a summary", async () => {
    theme.set("light");
    const screen = await render(Chart, props);
    await expect.element(screen.getByRole("heading", { name: "Export limits" })).toBeVisible();
    await expect.element(screen.getByText("The envelope against a fixed limit.")).toBeVisible();

    const c = await canvas(screen.container);
    await vi.waitFor(() => expect(colours(c)).toContain(rgb("#0b5cad")));
    expect(screen.container.querySelector("[data-points]")!.getAttribute("data-points")).toBe("8");
  });

  it("labels its axis with the shorter form when it is given one", async () => {
    const axisFormat = vi.fn((v: number) => `${v / 1000} kW`);
    const screen = await render(Chart, { ...props, axisFormat });
    await canvas(screen.container);
    await vi.waitFor(() => expect(axisFormat).toHaveBeenCalled());
  });

  it("is redrawn in the other theme's colours", async () => {
    theme.set("light");
    const screen = await render(Chart, props);
    await canvas(screen.container);
    theme.set("dark");
    await vi.waitFor(() =>
      expect(colours(screen.container.querySelector("canvas")!)).toContain(rgb("#6cb2ff")),
    );
  });

  it("tells its series apart by dash pattern as well as colour", async () => {
    const screen = await render(Chart, props);
    const dashes = [...screen.container.querySelectorAll("li line")].map((l) =>
      l.getAttribute("stroke-dasharray"),
    );
    expect(dashes).toEqual(["", "4 4"]);
    expect(new Set(dashes).size).toBe(dashes.length);
  });

  it("hides the canvas from a screen reader, which has the summary and the table", async () => {
    const screen = await render(Chart, props);
    const c = await canvas(screen.container);
    expect(c.closest("[aria-hidden=true]")).not.toBeNull();
  });

  it("shows the same values as a table, in the feeder's zone, with units", async () => {
    const screen = await render(Chart, props);
    const toggle = screen.getByRole("button", { name: "View as table" });
    await expect.element(toggle).toHaveAttribute("aria-pressed", "false");
    await toggle.click();

    const table = screen.getByRole("table", { name: "Export limits" });
    await expect.element(table).toBeVisible();
    const rows = [...table.element().querySelectorAll("tbody tr")].map((tr) =>
      [...tr.children].map((td) => td.textContent?.trim()),
    );
    expect(rows).toHaveLength(x.length);
    expect(rows[0]).toEqual(["Tue 3 Nov, 11:00", "3.0 kW", "5.0 kW"]);
    expect(rows[3]).toEqual(["Tue 3 Nov, 12:30", "1.5 kW", "5.0 kW"]);
    // A gap is a dash, not a zero.
    expect(rows[6]![1]).toBe("–");
    expect([...table.element().querySelectorAll("thead th")].map((th) => th.textContent)).toEqual([
      "Time",
      "Export limit",
      "Fixed limit",
    ]);
    // The table can be scrolled by keyboard.
    await expect
      .element(screen.getByRole("region", { name: "Export limits, as a table" }))
      .toHaveAttribute("tabindex", "0");

    await screen.getByRole("button", { name: "View as chart" }).click();
    await canvas(screen.container);
  });

  it("reacts to new data", async () => {
    const screen = await render(Chart, props);
    await canvas(screen.container);
    const more = [...x, start + 8 * 1800];
    await screen.rerender({
      x: more,
      series: [
        { ...series[0]!, values: [...series[0]!.values, 9000] },
        { ...series[1]!, values: more.map(() => 5000) },
      ],
    });
    await vi.waitFor(() =>
      expect(screen.container.querySelector("[data-points]")!.getAttribute("data-points")).toBe(
        "9",
      ),
    );
    await screen.getByRole("button", { name: "View as table" }).click();
    await expect.element(screen.getByText("9.0 kW")).toBeVisible();
  });

  it("is rebuilt when the set of series changes", async () => {
    const screen = await render(Chart, props);
    const first = await canvas(screen.container);
    await screen.rerender({
      series: [series[0]!, series[1]!, { label: "Measured", values: x.map(() => 1000), color: 2 }],
    });
    await vi.waitFor(() => expect(screen.container.querySelector("canvas")).not.toBe(first));
    expect(screen.container.querySelectorAll("li line")).toHaveLength(3);
  });

  it("thins a long series to what its pixels can show", async () => {
    const n = 20_000;
    const long = Array.from({ length: n }, (_, i) => start + i * 60);
    const screen = await render(Chart, {
      ...props,
      x: long,
      series: [
        {
          label: "Net export",
          values: long.map((_, i) => Math.sin(i / 300) * 1000),
          color: 1 as const,
        },
      ],
    });
    await canvas(screen.container);
    const host = screen.container.querySelector("[data-points]") as HTMLElement;
    const points = Number(host.getAttribute("data-points"));
    expect(points).toBeLessThanOrEqual(2 * host.clientWidth + 2);
    expect(points).toBeGreaterThan(host.clientWidth / 2);
  });

  it("follows its container's width", async () => {
    const screen = await render(Chart, props);
    const c = await canvas(screen.container);
    const figure = screen.container.querySelector("figure") as HTMLElement;
    figure.style.width = "420px";
    await vi.waitFor(() => expect(c.getBoundingClientRect().width).toBeLessThanOrEqual(420));
    expect(c.getBoundingClientRect().width).toBeGreaterThan(300);
  });

  it("reports a dragged range, and a double-click as no range", async () => {
    const onzoom = vi.fn();
    const screen = await render(Chart, { ...props, onzoom, syncKey: "feeder", now: start + 3600 });
    await canvas(screen.container);
    await expect
      .element(screen.getByText("Drag to zoom, double-click to reset"))
      .toBeInTheDocument();
    const over = screen.container.querySelector(".u-over") as HTMLElement;
    const box = over.getBoundingClientRect();
    const y = box.top + box.height / 2;
    const fire = (target: EventTarget, type: string, clientX: number) =>
      // movementX: uPlot ignores a move that says it did not move.
      target.dispatchEvent(
        new MouseEvent(type, { clientX, clientY: y, bubbles: true, button: 0, movementX: 5 }),
      );

    fire(over, "mousemove", box.left + box.width * 0.25);
    fire(over, "mousedown", box.left + box.width * 0.25);
    fire(over, "mousemove", box.left + box.width * 0.75);
    // uPlot follows the mouse once per frame.
    await new Promise((r) => requestAnimationFrame(r));
    fire(document, "mouseup", box.left + box.width * 0.75);

    await vi.waitFor(() => expect(onzoom).toHaveBeenCalledOnce());
    const [from, to] = onzoom.mock.calls[0]![0] as [number, number];
    const span = x.at(-1)! - x[0]!;
    expect(from).toBeGreaterThan(x[0]! + span * 0.2);
    expect(from).toBeLessThan(x[0]! + span * 0.3);
    expect(to).toBeGreaterThan(x[0]! + span * 0.7);
    expect(to).toBeLessThan(x[0]! + span * 0.8);

    // A click without a drag is not a zoom.
    fire(over, "mousedown", box.left + 50);
    fire(document, "mouseup", box.left + 51);
    expect(onzoom).toHaveBeenCalledOnce();

    over.dispatchEvent(new MouseEvent("dblclick", { bubbles: true }));
    expect(onzoom).toHaveBeenLastCalledWith(undefined);
  });

  it("shows the range a drag is selecting: a tinted box with an edge and a time at each end", async () => {
    theme.set("light");
    const onzoom = vi.fn();
    const screen = await render(Chart, { ...props, onzoom });
    await canvas(screen.container);
    const over = screen.container.querySelector(".u-over") as HTMLElement;
    const box = over.getBoundingClientRect();
    const fire = (target: EventTarget, type: string, across: number) =>
      target.dispatchEvent(
        new MouseEvent(type, {
          clientX: box.left + box.width * across,
          clientY: box.top + box.height / 2,
          bubbles: true,
          button: 0,
          movementX: 5,
        }),
      );
    const select = over.querySelector(".u-select") as HTMLElement;
    const [from, to] = [
      select.querySelector(".from"),
      select.querySelector(".to"),
    ] as HTMLElement[];
    // Before a drag there is nothing to see.
    expect(select.classList.contains("dragging")).toBe(false);
    expect(from!.hidden && to!.hidden).toBe(true);

    over.dispatchEvent(new MouseEvent("mouseenter"));
    fire(over, "mousemove", 0.25);
    await vi.waitFor(() => expect(screen.container.querySelector(".tip")).not.toBeNull());
    fire(over, "mousedown", 0.25);
    fire(over, "mousemove", 0.75);
    await vi.waitFor(() => expect(select.classList.contains("dragging")).toBe(true));
    // The readout stands down while the range is dragged: the box and its
    // times are what the reader is looking at.
    await vi.waitFor(() => expect(screen.container.querySelector(".tip")).toBeNull());
    // Half the plot wide, from a quarter of the way across.
    const drawn = select.getBoundingClientRect();
    expect(drawn.width).toBeGreaterThan(box.width * 0.45);
    expect(drawn.width).toBeLessThan(box.width * 0.55);
    expect(drawn.height).toBeCloseTo(box.height, 0);
    // Edged in the accent, and tinted with it: on a dark page too.
    const style = getComputedStyle(select);
    expect(style.borderLeftColor).toBe(`rgb(${rgb("#0b5cad").replace(/,/g, ", ")})`);
    expect(parseFloat(style.borderLeftWidth)).toBeGreaterThanOrEqual(2);
    const tint = getComputedStyle(select, "::before");
    expect(tint.backgroundColor).toBe(style.borderLeftColor);
    expect(Number(tint.opacity)).toBeGreaterThan(0.1);
    // The time at each edge, outside the box, in the feeder's zone: 3.5 hours
    // from 11:00, a quarter and three quarters of the way in.
    expect(from!.hidden || to!.hidden).toBe(false);
    expect(from!.textContent).toMatch(/^Tue 3 Nov, 11:5\d$/);
    expect(to!.textContent).toMatch(/^Tue 3 Nov, 13:3\d$/);
    expect(from!.getBoundingClientRect().right).toBeLessThanOrEqual(drawn.left);
    expect(to!.getBoundingClientRect().left).toBeGreaterThanOrEqual(drawn.right);

    // Let go: the page has the range, and the box is put away.
    fire(document, "mouseup", 0.75);
    await vi.waitFor(() => expect(onzoom).toHaveBeenCalledOnce());
    expect(select.classList.contains("dragging")).toBe(false);
    expect(from!.hidden && to!.hidden).toBe(true);
  });

  it("marks a period with a shade of the critical colour and a label", async () => {
    theme.set("light");
    const screen = await render(Chart, {
      ...props,
      spans: [{ from: x[2]!, to: x[4]!, label: "breach" }],
    });
    const c = await canvas(screen.container);
    // The shade: the critical colour, #b3261e, at a low alpha. Nothing else
    // on the canvas is translucent red.
    const shaded = (el: HTMLCanvasElement) => {
      const { data } = el.getContext("2d")!.getImageData(0, 0, el.width, el.height);
      let n = 0;
      for (let i = 0; i < data.length; i += 4) {
        const [r, g, b, a] = [data[i]!, data[i + 1]!, data[i + 2]!, data[i + 3]!];
        if (
          a > 20 &&
          a < 90 &&
          Math.abs(r - 179) < 16 &&
          Math.abs(g - 38) < 16 &&
          Math.abs(b - 30) < 16
        )
          n++;
      }
      return n;
    };
    await vi.waitFor(() => expect(shaded(c)).toBeGreaterThan(500));

    // With no span there is no such shade.
    const plain = await render(Chart, props);
    const other = await canvas(plain.container);
    await vi.waitFor(() => expect(colours(other)).toContain(rgb("#0b5cad")));
    expect(shaded(other)).toBe(0);
  });

  it("shows the range it is given, and the values under the cursor", async () => {
    const zoom: [number, number] = [x[2]!, x[5]!];
    const screen = await render(Chart, { ...props, zoom, now: x[3] });
    await canvas(screen.container);
    const over = screen.container.querySelector(".u-over") as HTMLElement;
    const box = over.getBoundingClientRect();
    // The left edge of the plot is the start of the zoom: x[2].
    over.dispatchEvent(
      new MouseEvent("mousemove", {
        clientX: box.left + 2,
        clientY: box.top + box.height / 2,
        bubbles: true,
      }),
    );
    await expect.element(screen.getByText("Tue 3 Nov, 12:00")).toBeInTheDocument();
    await expect.element(screen.getByText("2.0 kW")).toBeInTheDocument();
    // Nothing floats: the pointer never came in. A chart that follows
    // another's cursor says its values in its legend alone.
    expect(screen.container.querySelector(".tip")).toBeNull();
  });

  // The pointer over the plot, a share of the way across and down.
  const point = (container: Element, across: number, down = 0.5, type = "mousemove") => {
    const over = container.querySelector(".u-over") as HTMLElement;
    const box = over.getBoundingClientRect();
    over.dispatchEvent(
      new MouseEvent(type, {
        clientX: box.left + box.width * across,
        clientY: box.top + box.height * down,
        bubbles: type !== "mouseenter" && type !== "mouseleave",
        movementX: 5,
      }),
    );
    return { over, box };
  };

  it("floats a readout beside the pointer, on the side with more room, and puts it away", async () => {
    const screen = await render(Chart, props);
    await canvas(screen.container);
    point(screen.container, 0.1, 0.2, "mouseenter");
    const { box } = point(screen.container, 0.1, 0.2);
    await vi.waitFor(() => expect(screen.container.querySelector(".tip")).not.toBeNull());
    const tip = () => screen.container.querySelector(".tip") as HTMLElement;
    // The time, and each series with its value.
    expect(tip().textContent).toContain("Tue 3 Nov, 11:30");
    expect(tip().textContent).toContain("Export limit");
    expect(tip().textContent).toContain("2.5 kW");
    expect(tip().textContent).toContain("5.0 kW");
    // A sighted reader's aid: the same is in the legend, and in the table.
    expect(tip().getAttribute("aria-hidden")).toBe("true");
    expect(getComputedStyle(tip()).pointerEvents).toBe("none");
    // To the right of and below the pointer, inside the plot.
    let at = tip().getBoundingClientRect();
    expect(at.left).toBeGreaterThan(box.left + box.width * 0.1);
    expect(at.top).toBeGreaterThan(box.top + box.height * 0.2);

    // Near the other corner it goes to the other side.
    point(screen.container, 0.9, 0.8);
    await vi.waitFor(() => {
      at = tip().getBoundingClientRect();
      expect(at.right).toBeLessThan(box.left + box.width * 0.9);
    });
    expect(at.bottom).toBeLessThan(box.top + box.height * 0.8 + 1);
    expect(at.left).toBeGreaterThanOrEqual(box.left);

    point(screen.container, 0.9, 0.8, "mouseleave");
    await vi.waitFor(() => expect(screen.container.querySelector(".tip")).toBeNull());
  });

  it("moves a cursor from the keyboard, as a slider does, and says each point in words", async () => {
    const screen = await render(Chart, { ...props, now: x[3] });
    await canvas(screen.container);
    const slider = screen.getByRole("slider", { name: /Export limits: a cursor through time/ });
    await expect.element(slider).toHaveAttribute("aria-valuemax", "7");
    await expect
      .element(slider)
      .toHaveAttribute("aria-valuetext", "Tue 3 Nov, 11:00 to Tue 3 Nov, 14:30. No point chosen.");
    const key = (name: string) =>
      slider.element().dispatchEvent(new KeyboardEvent("keydown", { key: name, bubbles: true }));
    (slider.element() as HTMLElement).focus();

    // The first move goes to now; then a point at a time.
    key("ArrowRight");
    await expect
      .element(slider)
      .toHaveAttribute(
        "aria-valuetext",
        "Tue 3 Nov, 12:30. Export limit 1.5 kW, Fixed limit 5.0 kW.",
      );
    await expect.element(slider).toHaveAttribute("aria-valuenow", "3");
    // A sighted reader at the keyboard sees the same readout a pointer gets.
    await vi.waitFor(() =>
      expect(screen.container.querySelector(".tip")?.textContent).toContain("1.5 kW"),
    );
    key("ArrowLeft");
    await expect.element(slider).toHaveAttribute("aria-valuenow", "2");
    key("End");
    await expect.element(slider).toHaveAttribute("aria-valuenow", "7");
    key("Home");
    await expect
      .element(slider)
      .toHaveAttribute(
        "aria-valuetext",
        "Tue 3 Nov, 11:00. Export limit 3.0 kW, Fixed limit 5.0 kW.",
      );
    // A key that is not the cursor's is left to the page.
    const other = new KeyboardEvent("keydown", { key: "a", bubbles: true, cancelable: true });
    slider.element().dispatchEvent(other);
    expect(other.defaultPrevented).toBe(false);
    await expect.element(slider).toHaveAttribute("aria-valuenow", "0");

    // Escape puts the cursor away, and so does leaving the chart.
    key("Escape");
    await vi.waitFor(() => expect(screen.container.querySelector(".tip")).toBeNull());
    await expect
      .element(slider)
      .toHaveAttribute("aria-valuetext", expect.stringMatching(/No point chosen\.$/));
    key("PageUp");
    await vi.waitFor(() => expect(screen.container.querySelector(".tip")).not.toBeNull());
    (slider.element() as HTMLElement).blur();
    await vi.waitFor(() => expect(screen.container.querySelector(".tip")).toBeNull());
    // Leaving a chart whose cursor the keyboard never moved changes nothing.
    (slider.element() as HTMLElement).focus();
    (slider.element() as HTMLElement).blur();
    expect(screen.container.querySelector(".tip")).toBeNull();
  });

  it("shares its cursor with the charts of the same key: they say their values in their legends", async () => {
    const first = await render(Chart, { ...props, syncKey: "pair" });
    const second = await render(Chart, {
      ...props,
      title: "Voltage",
      syncKey: "pair",
      series: [{ label: "Highest", values: x.map((_, i) => 240 + i), color: 2 as const }],
      format: (v: number) => `${v} V`,
    });
    await canvas(first.container);
    await canvas(second.container);
    const slider = first.getByRole("slider", { name: /Export limits/ });
    slider.element().dispatchEvent(new KeyboardEvent("keydown", { key: "End", bubbles: true }));
    // The other chart follows, in its legend, and floats nothing.
    await expect.element(second.getByText("247 V")).toBeInTheDocument();
    expect(second.container.querySelector(".tip")).toBeNull();
    expect(first.container.querySelector(".tip")).not.toBeNull();
  });

  it("hides a series from its legend, and shows one alone", async () => {
    theme.set("light");
    const screen = await render(Chart, props);
    const c = await canvas(screen.container);
    await vi.waitFor(() => expect(colours(c)).toContain(rgb("#0b5cad")));
    const limit = screen.getByRole("button", { name: /Export limit/ });
    const fixed = screen.getByRole("button", { name: /Fixed limit/ });
    await expect.element(limit).toHaveAttribute("aria-pressed", "true");
    // A switch is a target of at least 24 px.
    expect(limit.element().getBoundingClientRect().height).toBeGreaterThanOrEqual(24);

    await limit.click();
    await expect.element(limit).toHaveAttribute("aria-pressed", "false");
    await vi.waitFor(() => expect(colours(c)).not.toContain(rgb("#0b5cad")));
    // Struck through as well as faded: not by colour alone.
    expect(getComputedStyle(limit.element().querySelector(".name")!).textDecorationLine).toBe(
      "line-through",
    );
    // The readout and the words leave it out; the table still has it.
    point(screen.container, 0.1, 0.5, "mouseenter");
    point(screen.container, 0.1);
    await vi.waitFor(() => expect(screen.container.querySelector(".tip")).not.toBeNull());
    expect(screen.container.querySelector(".tip")!.textContent).not.toContain("Export limit");
    await expect
      .element(screen.getByRole("slider"))
      .toHaveAttribute("aria-valuetext", "Tue 3 Nov, 11:30. Fixed limit 5.0 kW.");

    await limit.click();
    await expect.element(limit).toHaveAttribute("aria-pressed", "true");
    await vi.waitFor(() => expect(colours(c)).toContain(rgb("#0b5cad")));

    // With Alt, the series alone; and again, everything.
    await fixed.click({ modifiers: ["Alt"] });
    await expect.element(limit).toHaveAttribute("aria-pressed", "false");
    await expect.element(fixed).toHaveAttribute("aria-pressed", "true");
    await fixed.click({ modifiers: ["Alt"] });
    await expect.element(limit).toHaveAttribute("aria-pressed", "true");
  });

  it("takes its hidden series from the page, and tells the page of a change", async () => {
    const onhide = vi.fn();
    const screen = await render(Chart, { ...props, hidden: [1], onhide });
    await canvas(screen.container);
    const limit = screen.getByRole("button", { name: /Export limit/ });
    const fixed = screen.getByRole("button", { name: /Fixed limit/ });
    await expect.element(fixed).toHaveAttribute("aria-pressed", "false");
    await limit.click();
    expect(onhide).toHaveBeenLastCalledWith([0, 1]);
    // The page decides: until it says so, the chart shows what it was given.
    await expect.element(limit).toHaveAttribute("aria-pressed", "true");
    await screen.rerender({ hidden: [0, 1] });
    await expect.element(limit).toHaveAttribute("aria-pressed", "false");
    await expect
      .element(screen.getByRole("slider"))
      .toHaveAttribute("aria-valuetext", expect.stringMatching(/No point chosen\.$/));
  });

  it("marks now with a line over the plot, which moves without a new drawing", async () => {
    const screen = await render(Chart, { ...props, now: x[2] });
    await canvas(screen.container);
    const line = () => screen.container.querySelector(".u-over .now") as HTMLElement;
    await vi.waitFor(() => expect(line().hidden).toBe(false));
    expect(line().textContent).toBe("now");
    const over = screen.container.querySelector(".u-over")!.getBoundingClientRect();
    const first = line().getBoundingClientRect().left;
    // Two sevenths of the way across: the third of eight points.
    expect(first - over.left).toBeCloseTo((over.width * 2) / 7, 0);

    await screen.rerender({ now: x[4] });
    await vi.waitFor(() =>
      expect(line().getBoundingClientRect().left - over.left).toBeCloseTo((over.width * 4) / 7, 0),
    );
    // Out of the range on show, and with no now at all, there is no line.
    await screen.rerender({ now: x[7]! + 7200 });
    await vi.waitFor(() => expect(line().hidden).toBe(true));
    await screen.rerender({ now: undefined });
    expect(line().hidden).toBe(true);
  });

  it("spends its pixels on the range in view when it is zoomed in", async () => {
    const n = 20_000;
    const long = Array.from({ length: n }, (_, i) => start + i * 60);
    const screen = await render(Chart, {
      ...props,
      x: long,
      zoom: [long[5000]!, long[5100]!] as [number, number],
      series: [
        {
          label: "Net export",
          values: long.map((_, i) => Math.sin(i / 3) * 1000),
          color: 1 as const,
        },
      ],
    });
    await canvas(screen.container);
    const host = screen.container.querySelector("[data-points]") as HTMLElement;
    // Every point of the range, and one either side: none was thinned away.
    expect(host.getAttribute("data-points")).toBe("103");
  });

  const span = x.at(-1)! - x[0]!;
  const settled = () => new Promise((r) => setTimeout(r, 260));

  it("zooms about the pointer with Ctrl and the wheel, and leaves a plain wheel to the page", async () => {
    const onzoom = vi.fn();
    const screen = await render(Chart, { ...props, onzoom, syncKey: "wheel" });
    await canvas(screen.container);
    const over = screen.container.querySelector(".u-over") as HTMLElement;
    const box = over.getBoundingClientRect();
    const wheel = (deltaY: number, ctrlKey: boolean, across = 0.5) => {
      const event = new WheelEvent("wheel", {
        deltaY,
        ctrlKey,
        clientX: box.left + box.width * across,
        clientY: box.top + 10,
        bubbles: true,
        cancelable: true,
      });
      over.dispatchEvent(event);
      return event;
    };
    // The page scrolls: the chart does not take the wheel.
    expect(wheel(-300, false).defaultPrevented).toBe(false);
    expect(views.get("wheel")).toBeUndefined();

    // In, about the middle: the charts that share the key have the range at
    // once, and the page is told when the wheel has stopped.
    expect(wheel(-300, true).defaultPrevented).toBe(true);
    wheel(-300, true);
    const making = views.get("wheel")!;
    expect(making[1] - making[0]).toBeLessThan(span / 3);
    expect((making[0] + making[1]) / 2).toBeCloseTo(x[0]! + span / 2, -2);
    expect(onzoom).not.toHaveBeenCalled();
    await settled();
    expect(onzoom).toHaveBeenCalledExactlyOnceWith(making);

    // The page has it: nothing is in the making any more.
    await screen.rerender({ zoom: making });
    expect(views.get("wheel")).toBeUndefined();
    // All the way out is no zoom at all.
    wheel(4000, true);
    expect(views.get("wheel")).toBeNull();
    await settled();
    expect(onzoom).toHaveBeenLastCalledWith(undefined);
  });

  it("does not zoom a chart whose page keeps no range", async () => {
    const screen = await render(Chart, props);
    await canvas(screen.container);
    const over = screen.container.querySelector(".u-over") as HTMLElement;
    const event = new WheelEvent("wheel", { deltaY: -300, ctrlKey: true, cancelable: true });
    over.dispatchEvent(event);
    expect(event.defaultPrevented).toBe(false);
    const slider = screen.getByRole("slider");
    const plus = new KeyboardEvent("keydown", { key: "+", bubbles: true, cancelable: true });
    slider.element().dispatchEvent(plus);
    expect(plus.defaultPrevented).toBe(false);
    // Nor does Shift and a drag move it along: uPlot is left to select.
    const down = new MouseEvent("mousedown", {
      shiftKey: true,
      button: 0,
      bubbles: true,
      cancelable: true,
    });
    over.dispatchEvent(down);
    expect(down.defaultPrevented).toBe(false);
    document.dispatchEvent(new MouseEvent("mouseup", { bubbles: true }));
  });

  it("moves the range along with Shift and a drag, the plot following the pointer", async () => {
    const onzoom = vi.fn();
    const onpin = vi.fn();
    const zoom: [number, number] = [x[2]!, x[5]!];
    const screen = await render(Chart, { ...props, zoom, onzoom, onpin });
    await canvas(screen.container);
    const over = screen.container.querySelector(".u-over") as HTMLElement;
    const box = over.getBoundingClientRect();
    const mouse = (target: EventTarget, type: string, across: number, shiftKey = true) =>
      target.dispatchEvent(
        new MouseEvent(type, {
          clientX: box.left + box.width * across,
          clientY: box.top + 20,
          shiftKey,
          button: 0,
          bubbles: true,
          cancelable: true,
        }),
      );
    // A third of the plot to the left: a third of the range later.
    mouse(over, "mousedown", 0.6);
    mouse(document, "mousemove", 0.6 - 1 / 3);
    mouse(document, "mouseup", 0.6 - 1 / 3);
    // To within a pixel of the pointer: a pointer is at whole pixels.
    expect(onzoom).toHaveBeenCalledOnce();
    const [from, to] = onzoom.mock.calls[0]![0] as [number, number];
    expect(Math.abs(from - x[3]!)).toBeLessThan(30);
    expect(to - from).toBe(x[5]! - x[2]!);
    // The click that ends the drag pins nothing.
    mouse(over, "click", 0.6 - 1 / 3);
    expect(onpin).not.toHaveBeenCalled();

    // It stops at the end of the data.
    await screen.rerender({ zoom: [x[3]!, x[6]!] });
    mouse(over, "mousedown", 0.9);
    mouse(document, "mousemove", 0.1);
    mouse(document, "mouseup", 0.1);
    expect(onzoom).toHaveBeenLastCalledWith([x[4], x[7]]);
    // Another button, or no Shift, is not a move of the range.
    over.dispatchEvent(
      new MouseEvent("mousedown", { shiftKey: true, button: 2, bubbles: true, cancelable: true }),
    );
    expect(onzoom).toHaveBeenCalledTimes(2);
  });

  it("zooms and moves the range from the keyboard", async () => {
    const onzoom = vi.fn();
    const screen = await render(Chart, { ...props, onzoom });
    await canvas(screen.container);
    const slider = screen.getByRole("slider", { name: /Plus and minus zoom/ });
    const key = (name: string, shiftKey = false) => {
      const event = new KeyboardEvent("keydown", {
        key: name,
        shiftKey,
        bubbles: true,
        cancelable: true,
      });
      slider.element().dispatchEvent(event);
      return event;
    };
    // With no cursor, about the middle: half the range.
    expect(key("+").defaultPrevented).toBe(true);
    expect(onzoom).toHaveBeenLastCalledWith([x[0]! + span / 4, x[0]! + (3 * span) / 4]);
    await screen.rerender({ zoom: [x[0]! + span / 4, x[0]! + (3 * span) / 4] });
    // Along by a quarter of the range. Until the page answers, the chart
    // shows the range it reached: back again is where the page still is,
    // and the page is not told what it knows.
    key("ArrowRight", true);
    expect(onzoom).toHaveBeenLastCalledWith([x[0]! + (3 * span) / 8, x[0]! + (7 * span) / 8]);
    key("ArrowLeft", true);
    expect(onzoom).toHaveBeenCalledTimes(2);
    key("ArrowLeft", true);
    expect(onzoom).toHaveBeenLastCalledWith([x[0]! + span / 8, x[0]! + (5 * span) / 8]);
    await screen.rerender({ zoom: [x[0]! + span / 8, x[0]! + (5 * span) / 8] });
    // Out again to everything; and 0 goes there in one.
    key("-");
    expect(onzoom).toHaveBeenLastCalledWith(undefined);
    await screen.rerender({ zoom: [x[0]! + span / 8, x[0]! + (5 * span) / 8] });
    onzoom.mockClear();
    key("0");
    expect(onzoom).toHaveBeenLastCalledWith(undefined);
    // "=" is the same key as "+" without Shift.
    await screen.rerender({ zoom: [x[0]! + span / 4, x[0]! + (3 * span) / 4] });
    key("=");
    expect(onzoom).toHaveBeenLastCalledWith([x[0]! + (3 * span) / 8, x[0]! + (5 * span) / 8]);

    // With a cursor, about the cursor: the point stays where it is.
    await screen.rerender({ zoom: undefined });
    key("Home");
    key("+");
    expect(onzoom).toHaveBeenLastCalledWith([x[0], x[0]! + span / 2]);
    // Back to everything, which is where the page still is. With everything
    // on show there is nowhere to move to, and the page is not told what it
    // knows.
    key("0");
    onzoom.mockClear();
    key("ArrowRight", true);
    key("0");
    expect(onzoom).not.toHaveBeenCalled();
  });

  // A click as a browser makes one: a press and a release where the pointer
  // is, and then the click. uPlot swallows a click that ends a drag.
  const click = (container: Element, across: number, detail = 1) => {
    const { over, box } = point(container, across);
    const at = { clientX: box.left + box.width * across, clientY: box.top + box.height / 2 };
    over.dispatchEvent(new MouseEvent("mousedown", { ...at, bubbles: true, button: 0 }));
    document.dispatchEvent(new MouseEvent("mouseup", { ...at, bubbles: true, button: 0 }));
    over.dispatchEvent(new MouseEvent("click", { ...at, bubbles: true, detail }));
    return over;
  };

  it("pins a point with a click or Enter, keeps its values in the legend, and lets it go", async () => {
    const onpin = vi.fn();
    const onzoom = vi.fn();
    const pinHref = (time: number) => ({
      href: `/network?at=${time}`,
      label: "See the network then",
    });
    const screen = await render(Chart, { ...props, onpin, onzoom, pinHref });
    await canvas(screen.container);
    point(screen.container, 0.15, 0.5, "mouseenter");
    point(screen.container, 0.15);
    await vi.waitFor(() => expect(screen.container.querySelector(".tip")).not.toBeNull());
    const over = click(screen.container, 0.15);
    expect(onpin).toHaveBeenLastCalledWith(x[1]);
    // The second click of a double-click is the zoom's, and takes the pin back.
    click(screen.container, 0.15, 2);
    expect(onpin).toHaveBeenCalledTimes(1);
    await screen.rerender({ pin: x[1] });
    over.dispatchEvent(new MouseEvent("dblclick", { bubbles: true }));
    expect(onzoom).toHaveBeenLastCalledWith(undefined);
    expect(onpin).toHaveBeenLastCalledWith(undefined);

    // Pinned: a line on the plot, the values in the legend with no cursor,
    // and the ways on.
    point(screen.container, 0.15, 0.5, "mouseleave");
    slider(screen).dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
    const line = screen.container.querySelector(".u-over .pin") as HTMLElement;
    await vi.waitFor(() => expect(line.hidden).toBe(false));
    await expect
      .element(screen.getByText("Pinned at Tue 3 Nov, 11:30", { exact: false }))
      .toBeVisible();
    await expect.element(screen.getByRole("button", { name: /Export limit 2.5 kW/ })).toBeVisible();
    await expect
      .element(screen.getByRole("link", { name: "See the network then" }))
      .toHaveAttribute("href", `/network?at=${x[1]}`);
    await screen.getByRole("button", { name: "Unpin" }).click();
    expect(onpin).toHaveBeenLastCalledWith(undefined);

    // A click on the pinned point lets it go; on another, pins that one.
    onpin.mockClear();
    await screen.rerender({ pin: x[1] });
    click(screen.container, 0.15);
    expect(onpin).toHaveBeenLastCalledWith(undefined);
    click(screen.container, 0.43);
    expect(onpin).toHaveBeenLastCalledWith(x[3]);
    await screen.rerender({ pin: undefined });
    expect(screen.container.querySelector(".u-over .pin")!.hasAttribute("hidden")).toBe(true);

    // From the keyboard: Enter or Space on the cursor's point.
    const key = (name: string) => {
      const event = new KeyboardEvent("keydown", { key: name, bubbles: true, cancelable: true });
      slider(screen).dispatchEvent(event);
      return event;
    };
    key("End");
    expect(key("Enter").defaultPrevented).toBe(true);
    expect(onpin).toHaveBeenLastCalledWith(x[7]);
    key(" ");
    expect(onpin).toHaveBeenLastCalledWith(x[7]);
    // With no point under the cursor there is nothing to pin.
    key("Escape");
    onpin.mockClear();
    expect(key("Enter").defaultPrevented).toBe(false);
    expect(onpin).not.toHaveBeenCalled();
  });

  it("follows a finger with its cursor, and two fingers with its range", async () => {
    const onzoom = vi.fn();
    const screen = await render(Chart, { ...props, onzoom, syncKey: "touch" });
    await canvas(screen.container);
    const over = screen.container.querySelector(".u-over") as HTMLElement;
    const box = over.getBoundingClientRect();
    const touch = (type: string, target: EventTarget, ...across: number[]) =>
      target.dispatchEvent(
        new TouchEvent(type, {
          bubbles: true,
          touches: across.map(
            (share, identifier) =>
              new Touch({
                identifier,
                target: over,
                clientX: box.left + box.width * share,
                clientY: box.top + box.height / 2,
              }),
          ),
        }),
      );
    // One finger: the readout, as a pointer gets.
    touch("touchstart", over, 0.15);
    await vi.waitFor(() =>
      expect(screen.container.querySelector(".tip")?.textContent).toContain("Tue 3 Nov, 11:30"),
    );
    touch("touchmove", over, 0.43);
    await vi.waitFor(() =>
      expect(screen.container.querySelector(".tip")?.textContent).toContain("Tue 3 Nov, 12:30"),
    );
    // Two fingers that part: the range between them grows to fill the plot.
    touch("touchstart", over, 0.4, 0.6);
    touch("touchmove", over, 0.3, 0.7);
    const making = views.get("touch")!;
    expect(making[1] - making[0]).toBeCloseTo(span / 2, -1);
    expect((making[0] + making[1]) / 2).toBeCloseTo(x[0]! + span / 2, -1);
    touch("touchend", over);
    await settled();
    expect(onzoom).toHaveBeenLastCalledWith(making);
    // Three fingers are not a gesture of the chart's.
    touch("touchstart", over, 0.2, 0.5, 0.8);
    // A touch somewhere else puts the cursor away.
    touch("touchstart", over, 0.15);
    await vi.waitFor(() => expect(screen.container.querySelector(".tip")).not.toBeNull());
    touch("touchstart", document.body, 0.5);
    await vi.waitFor(() => expect(screen.container.querySelector(".tip")).toBeNull());
    touch("touchstart", document.body, 0.5);
  });

  it("makes the mark that a list points at heavier, and says which mark the pointer is on", async () => {
    theme.set("light");
    const onspan = vi.fn();
    const spans = [{ id: "breach-1", from: x[2]!, to: x[4]!, label: "breach" }];
    const screen = await render(Chart, { ...props, spans, onspan });
    const c = await canvas(screen.container);
    // How much of the canvas is the critical colour, weighted by how solid.
    const weight = () => {
      const { data } = c.getContext("2d")!.getImageData(0, 0, c.width, c.height);
      let sum = 0;
      for (let i = 0; i < data.length; i += 4) {
        if (Math.abs(data[i]! - 179) < 24 && Math.abs(data[i + 1]! - 38) < 24) sum += data[i + 3]!;
      }
      return sum;
    };
    await vi.waitFor(() => expect(weight()).toBeGreaterThan(0));
    const plain = weight();
    await screen.rerender({ emphasis: "breach-1" });
    await vi.waitFor(() => expect(weight()).toBeGreaterThan(plain * 1.3));
    await screen.rerender({ emphasis: "another" });
    await vi.waitFor(() => expect(weight()).toBeLessThan(plain * 1.1));

    // The pointer on the mark, and off it.
    point(screen.container, 0.4, 0.5, "mouseenter");
    point(screen.container, 0.4);
    await vi.waitFor(() => expect(onspan).toHaveBeenLastCalledWith("breach-1"));
    point(screen.container, 0.9);
    await vi.waitFor(() => expect(onspan).toHaveBeenLastCalledWith(undefined));
  });

  it("has no readout to float for a chart of nothing", async () => {
    const screen = await render(Chart, { ...props, x: [], series: [] });
    await canvas(screen.container);
    const slider = screen.getByRole("slider");
    await expect
      .element(slider)
      .toHaveAttribute("aria-valuetext", "No data to no data. No point chosen.");
    slider.element().dispatchEvent(new KeyboardEvent("keydown", { key: "End", bubbles: true }));
    expect(screen.container.querySelector(".tip")).toBeNull();
  });
});
