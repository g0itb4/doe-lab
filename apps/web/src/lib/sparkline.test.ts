import { describe, expect, it } from "vitest";
import { limitDomain, sparkPath, sparkY, trendWords, upToLast } from "./sparkline.ts";

const kw = (v: number) => `${(v / 1000).toFixed(1)} kW`;

describe("a series as a small line", () => {
  it("runs from the left edge to the right, the lowest value at the bottom and the highest at the top", () => {
    expect(sparkPath([0, 10, 5], 100, 20, 0)).toBe("M0 20L50 0L100 10");
    // With a little room above and below, so that the line's own width fits.
    expect(sparkPath([0, 10], 100, 20)).toBe("M0 18.5L100 1.5");
  });

  it("breaks where a value is missing", () => {
    expect(sparkPath([0, null, 10, 10, undefined, 0], 100, 20, 0)).toBe("M0 20M40 0L60 0M100 20");
  });

  it("is a level line across the middle when nothing changes", () => {
    expect(sparkPath([7, 7, 7], 100, 20)).toBe("M0 10L50 10L100 10");
  });

  it("is a mark in the middle for one value, and nothing for none", () => {
    expect(sparkPath([5], 100, 20)).toBe("M50 10h0.1");
    expect(sparkPath([null, 5, null], 100, 20)).toBe("M50 10h0.1");
    expect(sparkPath([], 100, 20)).toBe("");
    expect(sparkPath([null, undefined], 100, 20)).toBe("");
  });
});

describe("a series in a few words", () => {
  it("says which way it went, from what to what, and its highest when that is neither", () => {
    expect(trendWords([1000, 2000, 3400], kw)).toBe("Up from 1.0 kW to 3.4 kW over the range.");
    expect(trendWords([3400, null, 1000], kw)).toBe("Down from 3.4 kW to 1.0 kW over the range.");
    expect(trendWords([1000, 4200, 2000], kw)).toBe(
      "Up from 1.0 kW to 2.0 kW over the range; its highest was 4.2 kW.",
    );
  });

  it("says steady when it ends where it began, unless it rose between", () => {
    expect(trendWords([2000, 2010, 2020], kw)).toBe("Steady at 2.0 kW over the range.");
    expect(trendWords([2000], kw)).toBe("Steady at 2.0 kW over the range.");
    expect(trendWords([2000, 4000, 2000], kw)).toBe(
      "Now 2.0 kW, as at the start of the range; its highest was 4.0 kW.",
    );
  });

  it("says so when there is nothing to describe", () => {
    expect(trendWords([], kw)).toBe("No readings in this range.");
    expect(trendWords([null, undefined], kw)).toBe("No readings in this range.");
  });
});

describe("a series up to its last value", () => {
  it("drops what is missing at the end, and keeps a gap in the middle", () => {
    expect(upToLast([1, null, 3, undefined, null])).toEqual([1, null, 3]);
    expect(upToLast([1, 2])).toEqual([1, 2]);
    expect(upToLast([null, undefined])).toEqual([]);
    expect(upToLast([])).toEqual([]);
  });
});

describe("a line drawn against a limit", () => {
  it("is scaled from nothing up to the limit, so its height says how much of the limit is used", () => {
    expect(limitDomain([1000, 2000], 4000)).toEqual([0, 4000]);
    // Half the limit is half way up the box.
    expect(sparkPath([2000, 2000], 100, 20, 0, [0, 4000])).toBe("M0 10L100 10");
    expect(sparkY(4000, [0, 4000], 20, 0)).toBe(0);
    expect(sparkY(0, [0, 4000], 20, 0)).toBe(20);
  });

  it("makes room for a value over the limit, and for an import under nothing", () => {
    expect(limitDomain([1000, 5000], 4000)).toEqual([0, 5000]);
    expect(limitDomain([-700, 300, null], 4000)).toEqual([-700, 4000]);
    expect(limitDomain([], 4000)).toEqual([0, 4000]);
    // The limit's own mark is then below the top.
    expect(sparkY(4000, [0, 5000], 20, 0)).toBe(4);
  });

  it("is half way down for a scale of no height", () => {
    expect(sparkY(0, [0, 0], 20)).toBe(10);
  });
});
