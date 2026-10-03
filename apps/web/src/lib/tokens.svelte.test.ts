import { afterEach, describe, expect, it } from "vitest";
import { theme, token } from "./theme.svelte.ts";

// WCAG relative luminance and contrast ratio, from a #rrggbb colour.
function luminance(hex: string): number {
  const channel = (i: number) => {
    const c = parseInt(hex.slice(1 + 2 * i, 3 + 2 * i), 16) / 255;
    return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
  };
  return 0.2126 * channel(0) + 0.7152 * channel(1) + 0.0722 * channel(2);
}
function contrast(a: string, b: string): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x) as [number, number];
  return (hi + 0.05) / (lo + 0.05);
}

// Each pair that the app puts together, and the least contrast it needs:
// 4.5:1 for text, 3:1 for chart lines, icons and the borders of controls.
const PAIRS: [fg: string, bg: string, min: number][] = [
  ["--color-text", "--color-bg", 4.5],
  ["--color-text", "--color-surface", 4.5],
  ["--color-text", "--color-sunken", 4.5],
  ["--color-muted", "--color-bg", 4.5],
  ["--color-muted", "--color-surface", 4.5],
  ["--color-muted", "--color-sunken", 4.5],
  ["--color-accent", "--color-surface", 4.5],
  ["--color-accent", "--color-bg", 4.5],
  ["--color-accent-text", "--color-accent", 4.5],
  ["--color-ok", "--color-surface", 4.5],
  ["--color-warn", "--color-surface", 4.5],
  ["--color-critical", "--color-surface", 4.5],
  ["--color-critical-text", "--color-critical", 4.5],
  ["--color-control", "--color-surface", 3],
  ["--color-control", "--color-bg", 3],
  ["--color-series-1", "--color-surface", 3],
  ["--color-series-2", "--color-surface", 3],
  ["--color-series-3", "--color-surface", 3],
  ["--color-series-4", "--color-surface", 3],
  ["--color-series-ref", "--color-surface", 3],
  // The bar of a LimitMeter on its track.
  ["--color-ok", "--color-sunken", 3],
  ["--color-accent", "--color-sunken", 3],
  ["--color-warn", "--color-sunken", 3],
  ["--color-critical", "--color-sunken", 3],
  // What floats over the page: a tooltip, a hover card.
  ["--color-text", "--color-raised", 4.5],
  ["--color-muted", "--color-raised", 4.5],
  ["--color-accent", "--color-raised", 4.5],
  ["--color-control", "--color-raised", 3],
  ["--color-series-1", "--color-raised", 3],
  ["--color-series-2", "--color-raised", 3],
  ["--color-series-3", "--color-raised", 3],
  ["--color-series-4", "--color-raised", 3],
  ["--color-series-ref", "--color-raised", 3],
  // A status as text on its own tint, and plain text on a tinted row.
  ["--color-ok", "--color-ok-soft", 4.5],
  ["--color-warn", "--color-warn-soft", 4.5],
  ["--color-critical", "--color-critical-soft", 4.5],
  ["--color-accent", "--color-accent-soft", 4.5],
  ["--color-text", "--color-ok-soft", 4.5],
  ["--color-text", "--color-warn-soft", 4.5],
  ["--color-text", "--color-critical-soft", 4.5],
  ["--color-text", "--color-accent-soft", 4.5],
];

afterEach(() => theme.set("system"));

describe.each(["light", "dark"] as const)("the %s theme", (choice) => {
  it.each(PAIRS)("%s on %s has a contrast of at least %s:1", (fg, bg, min) => {
    theme.set(choice);
    const ratio = contrast(token(fg), token(bg));
    expect(ratio, `${token(fg)} on ${token(bg)} is ${ratio.toFixed(2)}:1`).toBeGreaterThanOrEqual(
      min,
    );
  });
});

describe("the dark values in app.css", () => {
  it("are written once, under the theme in force", () => {
    // Twice, they could drift apart: a visitor who chose dark would not see
    // what the system's dark shows.
    const rules = [...document.styleSheets].flatMap((sheet) => [...sheet.cssRules]);
    const blocks: string[] = [];
    const visit = (rule: CSSRule) => {
      if (rule instanceof CSSStyleRule && rule.style.getPropertyValue("--color-bg")) {
        blocks.push(rule.selectorText);
      }
      if ("cssRules" in rule) [...(rule as CSSGroupingRule).cssRules].forEach(visit);
    };
    rules.forEach(visit);
    // The light values, and the dark ones.
    expect(blocks.filter((selector) => selector.includes("data-scheme"))).toEqual([
      ':root[data-scheme="dark"]',
    ]);
    expect(blocks).toHaveLength(2);
  });

  it("give every colour token of the light theme a dark value, or keep it on purpose", () => {
    const colours = (selector: (rule: CSSStyleRule) => boolean) => {
      const names = new Set<string>();
      const visit = (rule: CSSRule) => {
        if (rule instanceof CSSStyleRule && selector(rule)) {
          for (const name of rule.style) if (name.startsWith("--color-")) names.add(name);
        }
        if ("cssRules" in rule) [...(rule as CSSGroupingRule).cssRules].forEach(visit);
      };
      [...document.styleSheets].flatMap((sheet) => [...sheet.cssRules]).forEach(visit);
      return names;
    };
    const dark = colours((rule) => rule.selectorText.includes("data-scheme"));
    const light = colours(
      (rule) =>
        !rule.selectorText.includes("data-scheme") &&
        rule.style.getPropertyValue("--color-bg") !== "",
    );
    // The scrim is the same in both themes.
    const kept = ["--color-scrim"];
    expect([...light].filter((name) => !dark.has(name) && !kept.includes(name))).toEqual([]);
  });
});
