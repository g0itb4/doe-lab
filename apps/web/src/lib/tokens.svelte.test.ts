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

describe("the two dark blocks in app.css", () => {
  it("are the same: a visitor who chose dark sees what the system's dark shows", () => {
    // The media-query block cannot be switched on from a test, but both
    // blocks are in the stylesheet as text.
    const rules = [...document.styleSheets].flatMap((sheet) => [...sheet.cssRules]);
    const blocks: string[] = [];
    const visit = (rule: CSSRule) => {
      if (
        rule instanceof CSSStyleRule &&
        rule.selectorText.includes("data-theme") &&
        rule.style.getPropertyValue("--color-bg")
      ) {
        blocks.push(
          [...rule.style]
            .filter((name) => name.startsWith("--color-"))
            .sort()
            .map((name) => `${name}:${rule.style.getPropertyValue(name).trim()}`)
            .join(";"),
        );
      }
      if ("cssRules" in rule) [...(rule as CSSGroupingRule).cssRules].forEach(visit);
    };
    rules.forEach(visit);
    expect(blocks).toHaveLength(2);
    expect(blocks[0]).toBe(blocks[1]);
  });
});
