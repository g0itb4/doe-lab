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
