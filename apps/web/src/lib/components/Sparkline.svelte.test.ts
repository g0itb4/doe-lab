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

  it("has no line for a series of nothing, and says so", async () => {
    const screen = await render(Sparkline, { values: [], format: kw, label: "Measured export" });
    expect(screen.container.querySelector("path")).toBeNull();
    expect(screen.container.querySelector(".sr-only")!.textContent).toBe(
      "Measured export: No readings in this range.",
    );
  });
});
