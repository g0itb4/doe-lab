import { describe, expect, it } from "vitest";
import { clamp, pan, views, zoomAt, type Range } from "./chart-view.svelte.ts";

const day: Range = [0, 86_400];

describe("a zoom about an instant", () => {
  it("keeps the instant where it is, and halves or doubles the range", () => {
    // Noon, in the middle of the day: six hours either side becomes three.
    expect(zoomAt([21_600, 64_800], 43_200, 0.5, day)).toEqual([32_400, 54_000]);
    // A quarter of the way in stays a quarter of the way in.
    expect(zoomAt([0, 40_000], 10_000, 0.5, day)).toEqual([5000, 25_000]);
    expect(zoomAt([20_000, 40_000], 25_000, 2, day)).toEqual([15_000, 55_000]);
  });

  it("is no zoom at all once it shows the whole of the bounds", () => {
    expect(zoomAt([10_000, 60_000], 30_000, 4, day)).toBeUndefined();
    expect(zoomAt(day, 43_200, 1, day)).toBeUndefined();
  });

  it("stays inside the bounds when it zooms out near an edge", () => {
    expect(zoomAt([0, 10_000], 1000, 2, day)).toEqual([0, 20_000]);
    expect(zoomAt([80_000, 86_400], 86_000, 2, day)).toEqual([73_600, 86_400]);
  });

  it("is never shorter than the least span, nor longer than bounds that are shorter still", () => {
    expect(zoomAt([43_000, 43_400], 43_200, 0.1, day)).toEqual([43_050, 43_350]);
    expect(zoomAt([43_000, 43_400], 43_200, 0.1, day, 60)).toEqual([43_170, 43_230]);
    expect(zoomAt([0, 100], 50, 0.5, [0, 100])).toBeUndefined();
  });

  it("zooms about the middle of a range of no length, and about an end for an instant outside", () => {
    expect(zoomAt([500, 500], 500, 1, day)).toEqual([350, 650]);
    expect(zoomAt([10_000, 20_000], 99_000, 0.5, day)).toEqual([81_400, 86_400]);
    expect(zoomAt([10_000, 20_000], -5000, 0.5, day)).toEqual([0, 5000]);
  });
});

describe("a range moved along", () => {
  it("moves by the seconds it is given, and stops at the bounds", () => {
    expect(pan([10_000, 20_000], 5000, day)).toEqual([15_000, 25_000]);
    expect(pan([10_000, 20_000], -5000, day)).toEqual([5000, 15_000]);
    expect(pan([10_000, 20_000], -50_000, day)).toEqual([0, 10_000]);
    expect(pan([10_000, 20_000], 500_000, day)).toEqual([76_400, 86_400]);
  });

  it("is in whole seconds, and no longer than the bounds", () => {
    expect(clamp([10.4, 20.6], day)).toEqual([10, 21]);
    expect(clamp([-10, 200_000], day)).toEqual([0, 86_400]);
  });
});

describe("the ranges in the making", () => {
  it("are kept by key until the page has them", () => {
    expect(views.get("feeder")).toBeUndefined();
    views.set("feeder", [10, 20]);
    expect(views.get("feeder")).toEqual([10, 20]);
    // The whole of it is a range in the making too.
    views.set("feeder", undefined);
    expect(views.get("feeder")).toBeNull();
    views.clear("feeder");
    expect(views.get("feeder")).toBeUndefined();
  });

  it("give a chart that shares its range with no other a key of its own", () => {
    const [a, b] = [views.own(), views.own()];
    expect(a).not.toBe(b);
    views.set(a, [1, 2]);
    expect(views.get(b)).toBeUndefined();
    views.clear(a);
  });
});
