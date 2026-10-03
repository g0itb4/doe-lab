import { afterEach, describe, expect, it, vi } from "vitest";
import { theme, token } from "./theme.svelte.ts";

afterEach(() => {
  theme.set("system");
  vi.restoreAllMocks();
});

describe("the theme", () => {
  it("follows the system until the visitor chooses", () => {
    localStorage.removeItem("doelab.theme");
    theme.init();
    expect(theme.choice).toBe("system");
    expect(document.documentElement.dataset.theme).toBeUndefined();
  });

  it("applies a choice, remembers it, and cycles", () => {
    expect(theme.next()).toBe("light");
    theme.set("light");
    expect(document.documentElement.dataset.theme).toBe("light");
    expect(localStorage.getItem("doelab.theme")).toBe("light");
    expect(theme.next()).toBe("dark");

    theme.set("dark");
    expect(document.documentElement.dataset.theme).toBe("dark");
    expect(theme.next()).toBe("system");

    theme.set("system");
    expect(document.documentElement.dataset.theme).toBeUndefined();
    expect(localStorage.getItem("doelab.theme")).toBeNull();
  });

  it("starts from the stored choice, and ignores a stored value it does not know", () => {
    localStorage.setItem("doelab.theme", "dark");
    theme.init();
    expect(theme.choice).toBe("dark");
    localStorage.setItem("doelab.theme", "sepia");
    theme.init();
    expect(theme.choice).toBe("system");
  });

  it("changes the colours that the tokens resolve to", () => {
    theme.set("light");
    const light = token("--color-surface");
    theme.set("dark");
    const dark = token("--color-surface");
    expect(light).toBe("#ffffff");
    expect(dark).not.toBe(light);
  });

  it("says which theme is in force, for the stylesheet and for a map's tiles", () => {
    theme.set("dark");
    expect(theme.scheme).toBe("dark");
    expect(document.documentElement.dataset.scheme).toBe("dark");
    theme.set("light");
    expect(theme.scheme).toBe("light");
    expect(document.documentElement.dataset.scheme).toBe("light");
  });

  it("follows the system while the visitor has not chosen, and when the system changes", () => {
    let listener = () => {};
    const system = {
      matches: true,
      addEventListener: (_: string, run: () => void) => (listener = run),
    };
    vi.spyOn(window, "matchMedia").mockImplementation(() => system as unknown as MediaQueryList);
    localStorage.removeItem("doelab.theme");
    theme.init();
    expect(theme.scheme).toBe("dark");
    system.matches = false;
    listener();
    expect(theme.scheme).toBe("light");
    // A choice holds, whatever the system then does.
    theme.set("dark");
    listener();
    expect(theme.scheme).toBe("dark");
  });

  it("asks the browser for a token once, until the theme changes", () => {
    theme.set("light");
    const asked = vi.spyOn(window, "getComputedStyle");
    expect(token("--color-accent")).toBe(token("--color-accent"));
    expect(asked).toHaveBeenCalledTimes(1);
    theme.set("dark");
    token("--color-accent");
    expect(asked).toHaveBeenCalledTimes(2);
  });

  it("bumps its version when the colours change, so a canvas can redraw", () => {
    const before = theme.version;
    theme.set("dark");
    expect(theme.version).toBeGreaterThan(before);
  });

  it("still works when storage is blocked", () => {
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("blocked");
    });
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("blocked");
    });
    theme.set("dark");
    expect(theme.choice).toBe("dark");
    theme.init();
    expect(theme.choice).toBe("system");
  });
});
