import { describe, expect, it } from "vitest";
import { render } from "vitest-browser-svelte";
import Sparkline from "./Sparkline.svelte";

const kw = (v: number) => `${(v / 1000).toFixed(1)} kW`;

describe("Sparkline", () => {
  it("draws its series as one line, as wide as its room and a line tall", async () => {
    const screen = await render(Sparkline, {
      values: [1000, 2000, 3400],
      format: kw,
      label: "Measured export",
    });
    const svg = screen.container.querySelector("svg")!;
    expect(svg.querySelector("path")!.getAttribute("d")).toBe("M0 22.5L60 13.8L120 1.5");
    expect(svg.getBoundingClientRect().height).toBe(24);
    expect(svg.getBoundingClientRect().width).toBeGreaterThan(100);
    // Stretched to its room, with a line that keeps its weight.
    expect(getComputedStyle(svg.querySelector("path")!).vectorEffect).toBe("non-scaling-stroke");
  });

  it("says the same in words, and hides the picture from a screen reader", async () => {
    const screen = await render(Sparkline, {
      values: [1000, 2000, 3400],
      format: kw,
      label: "Measured export",
    });
    expect(screen.container.querySelector("svg")!.getAttribute("aria-hidden")).toBe("true");
    expect(screen.container.querySelector(".sr-only")!.textContent).toBe(
      "Measured export: Up from 1.0 kW to 3.4 kW over the range.",
    );
  });

  it("draws its series against a limit: a dashed rule, and a scale from nothing to the limit", async () => {
    const screen = await render(Sparkline, {
      values: [1000, 2000, 2000],
      format: kw,
      label: "Net export",
      limit: 4000,
    });
    const svg = screen.container.querySelector("svg")!;
    // Half the limit is half way up; without the limit it would be the top.
    expect(svg.querySelector("path")!.getAttribute("d")).toBe("M0 17.3L60 12L120 12");
    const rule = svg.querySelector("line.limit")!;
    expect(rule.getAttribute("y1")).toBe("1.5");
    expect(rule.getAttribute("x2")).toBe("120");
    expect(getComputedStyle(rule).strokeDasharray).not.toBe("none");
    expect(getComputedStyle(rule).stroke).not.toBe(
      getComputedStyle(svg.querySelector("path")!).stroke,
    );
    expect(screen.container.querySelector(".sr-only")!.textContent).toBe(
      "Net export: Up from 1.0 kW to 2.0 kW over the range. The limit is 4.0 kW.",
    );
    // Over the limit, the rule is below the top and the line above it.
    await screen.rerender({ values: [1000, 5000] });
    expect(Number(rule.getAttribute("y1"))).toBeGreaterThan(5);
    // With no limit there is no rule.
    await screen.rerender({ limit: undefined });
    expect(svg.querySelector("line.limit")).toBeNull();
  });

  it("has no line for a series of nothing, and says so", async () => {
    const screen = await render(Sparkline, { values: [], format: kw, label: "Measured export" });
    expect(screen.container.querySelector("path")).toBeNull();
    expect(screen.container.querySelector(".sr-only")!.textContent).toBe(
      "Measured export: No readings in this range.",
    );
  });
});
