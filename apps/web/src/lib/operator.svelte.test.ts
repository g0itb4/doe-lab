import { afterEach, describe, expect, it, vi } from "vitest";
import { Operator, operator } from "./operator.svelte.ts";

afterEach(() => {
  vi.restoreAllMocks();
  operator.token = "";
});

describe("the operator's token", () => {
  it("is kept for the tab, and forgotten when cleared", () => {
    operator.token = "s3cret";
    expect(operator.token).toBe("s3cret");
    expect(sessionStorage.getItem("doelab.operator-token")).toBe("s3cret");
    expect(localStorage.getItem("doelab.operator-token")).toBeNull();
    operator.token = "";
    expect(sessionStorage.getItem("doelab.operator-token")).toBeNull();
  });

  it("is read back when the page loads again in the same tab", () => {
    sessionStorage.setItem("doelab.operator-token", "kept");
    expect(new Operator().token).toBe("kept");
    sessionStorage.removeItem("doelab.operator-token");
  });

  it("still works when storage is blocked", () => {
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("blocked");
    });
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("blocked");
    });
    operator.token = "held";
    expect(operator.token).toBe("held");
    expect(new Operator().token).toBe("");
  });
});
