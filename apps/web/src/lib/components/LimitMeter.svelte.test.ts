import { hovercard } from "$lib/hovercard.svelte.ts";
import { describe, expect, it } from "vitest";
import { render } from "vitest-browser-svelte";
import { useOf } from "$lib/limit.ts";
import LimitMeter from "./LimitMeter.svelte";

const meter = () => document.querySelector<HTMLElement>(".meter")!;
const bar = () => document.querySelector<HTMLElement>(".bar")!;
const fills = () => [...document.querySelectorAll<HTMLElement>(".fill")];
const tick = () => document.querySelector<HTMLElement>(".tick")!;

describe("LimitMeter", () => {
  it("draws the reading as a bar and the limit as a tick, on a track as long as the connection's limit", async () => {
    const use = useOf(1500, 3000, undefined);
    const screen = await render(LimitMeter, { use, capW: 5000 });
    expect(fills()).toHaveLength(1);
    expect(fills()[0]!.style.width).toBe("30%");
    // On the screen as in the style: the bar is three tenths of its track,
    // and the tick is centred six tenths of the way along it.
    const track = bar().getBoundingClientRect().width;
    expect(fills()[0]!.getBoundingClientRect().width / track).toBeCloseTo(0.3, 1);
    expect(tick().getBoundingClientRect().left - bar().getBoundingClientRect().left).toBeCloseTo(
      0.6 * track - 1,
      0,
    );
    // The bar is a picture of the words beside it, and only the words are read.
    expect(bar().getAttribute("aria-hidden")).toBe("true");
    await expect.element(screen.getByText("50 %", { exact: true })).toBeVisible();
    expect(meter().dataset.level).toBe("ok");
  });

  it("ends its track at the limit when it is told of no other end", async () => {
    await render(LimitMeter, { use: useOf(1500, 3000, undefined) });
    expect(fills()[0]!.style.width).toBe("50%");
    // The tick is at the end, and stays on the track.
    const [t, b] = [tick().getBoundingClientRect(), bar().getBoundingClientRect()];
    expect(t.right).toBeCloseTo(b.right, 0);
  });

  it("hatches what is over the limit, and is a warning unless the page says more", async () => {
    const use = useOf(4000, 3000, undefined);
    const screen = await render(LimitMeter, { use });
    expect(fills().map((f) => [f.style.left, f.style.width])).toEqual([
      ["", "75%"],
      ["75%", "25%"],
    ]);
    expect(fills()[1]!.classList.contains("over")).toBe(true);
    expect(getComputedStyle(fills()[1]!).backgroundImage).toContain("repeating-linear-gradient");
    expect(getComputedStyle(fills()[0]!).backgroundImage).toBe("none");
    expect(meter().dataset.level).toBe("warn");
    const warn = getComputedStyle(bar()).color;

    await screen.rerender({ level: "critical" });
    expect(meter().dataset.level).toBe("critical");
    expect(getComputedStyle(bar()).color).not.toBe(warn);
    await screen.rerender({ level: "info" });
    expect(getComputedStyle(bar()).color).not.toBe(warn);
    // Within the band it is at the limit, not over it: no warning of its own.
    await screen.rerender({ level: undefined, use: useOf(3020, 3000, undefined) });
    expect(meter().dataset.level).toBe("ok");
  });

  it("is one line: a word or two for the eye, the sentence for a screen reader and the pointer", async () => {
    const use = useOf(-1200, 3000, 4800);
    const screen = await render(LimitMeter, { use });
    await expect.element(screen.getByText("25 %", { exact: true })).toBeVisible();
    const sentence = use!.text;
    expect(sentence).toMatch(
      /^Importing 1\.2\skW of 4\.8\skW allowed: 25\s%, 3\.6\skW to spare\.$/,
    );
    // The pointer gets the sentence on a card, not from the browser's own
    // tooltip.
    expect(meter().hasAttribute("title")).toBe(false);
    meter().dispatchEvent(new MouseEvent("mouseenter", { clientX: 20, clientY: 20 }));
    expect(hovercard.card).toEqual({ title: "Import against its limit", lines: [sentence] });
    meter().dispatchEvent(new MouseEvent("mouseleave"));
    expect(hovercard.card).toBeUndefined();

    expect(meter().querySelector(".sr-only")!.textContent).toBe(sentence);
    expect(meter().querySelector("[aria-hidden=true] + .sr-only")).not.toBeNull();
    // An export says so on its card.
    await screen.rerender({ use: useOf(1200, 3000, 4800) });
    meter().dispatchEvent(new MouseEvent("mouseenter", { clientX: 20, clientY: 20 }));
    expect(hovercard.card?.title).toBe("Export against its limit");
    meter().dispatchEvent(new MouseEvent("mouseleave"));
  });

  it("says so when there is nothing to compare", async () => {
    const screen = await render(LimitMeter, { use: undefined });
    await expect.element(screen.getByText("No reading")).toBeVisible();
    expect(bar().classList.contains("none")).toBe(true);
    expect(fills()).toHaveLength(0);
    expect(document.querySelector(".tick")).toBeNull();
    expect(getComputedStyle(bar().querySelector(".track")!).borderTopStyle).toBe("dashed");

    // With nothing to compare the pointer gets no card.
    meter().dispatchEvent(new MouseEvent("mouseenter", { clientX: 20, clientY: 20 }));
    expect(hovercard.card).toBeUndefined();

    await screen.rerender({ empty: "No limit in force" });
    await expect.element(screen.getByText("No limit in force")).toBeVisible();
  });

  it("draws a limit of nothing as a tick at the start, and all of a flow as over it", async () => {
    await render(LimitMeter, { use: useOf(2000, 0, undefined), capW: 5000 });
    expect(fills().map((f) => f.style.width)).toEqual(["0%", "40%"]);
    expect(tick().getBoundingClientRect().left).toBeCloseTo(bar().getBoundingClientRect().left, 0);
  });
});
