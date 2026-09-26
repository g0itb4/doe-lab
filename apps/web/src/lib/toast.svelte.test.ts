import { afterEach, describe, expect, it, vi } from "vitest";
import { TOAST_MS, toasts } from "./toast.svelte.ts";

afterEach(() => {
  toasts.clear();
  vi.useRealTimers();
});

describe("toasts", () => {
  it("shows a success and takes it away again", () => {
    vi.useFakeTimers();
    toasts.show("ok", "Saved");
    expect(toasts.items.map((t) => t.text)).toEqual(["Saved"]);
    vi.advanceTimersByTime(TOAST_MS + 1);
    expect(toasts.items).toEqual([]);
  });

  it("keeps a failure until it is dismissed", () => {
    vi.useFakeTimers();
    const id = toasts.show("error", "Refused");
    vi.advanceTimersByTime(TOAST_MS * 10);
    expect(toasts.items).toHaveLength(1);
    toasts.dismiss(id);
    expect(toasts.items).toEqual([]);
  });

  it("stacks, each with its own id", () => {
    const a = toasts.show("error", "one");
    const b = toasts.show("error", "two");
    expect(a).not.toBe(b);
    toasts.dismiss(a);
    expect(toasts.items.map((t) => t.text)).toEqual(["two"]);
  });
});
