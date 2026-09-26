import { describe, expect, it } from "vitest";
import { withQuery } from "./query.ts";

describe("the address of a view", () => {
  const url = new URL("https://example.test/sites?phase=2&q=XD");

  it("sets, replaces and removes parameters, and keeps the rest", () => {
    expect(withQuery(url, { q: "LAB" })).toBe("/sites?phase=2&q=LAB");
    expect(withQuery(url, { phase: null })).toBe("/sites?q=XD");
    expect(withQuery(url, { range: "6h" })).toBe("/sites?phase=2&q=XD&range=6h");
  });

  it("has no question mark when nothing is left", () => {
    expect(withQuery(url, { phase: null, q: null })).toBe("/sites");
  });
});
