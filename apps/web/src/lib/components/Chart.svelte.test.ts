import { afterEach, describe, expect, it, vi } from "vitest";
import { render } from "vitest-browser-svelte";
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
  });
});
