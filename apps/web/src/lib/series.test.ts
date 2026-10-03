import { describe, expect, it } from "vitest";
import {
  DEFAULT_RANGE,
  downsample,
  hiddenOf,
  rangeKey,
  toggleHidden,
  windowed,
  windowOf,
  withHidden,
  zoomOf,
} from "./series.ts";

describe("the time range", () => {
  it("reads a known range, and falls back for anything else", () => {
    expect(rangeKey("6h")).toBe("6h");
    expect(rangeKey("3d")).toBe("3d");
    expect(rangeKey("1y")).toBe(DEFAULT_RANGE);
    expect(rangeKey(null)).toBe(DEFAULT_RANGE);
  });

  it("is centred on now, on half-hour boundaries", () => {
    const now = 1_000_000_000; // 46 min 40 s past the hour
    const w = windowOf("6h", now);
    expect(w.from % 1800).toBe(0);
    expect(w.to - w.from).toBe(6 * 3600 + 1800);
    expect(w.from).toBeLessThanOrEqual(now - 3 * 3600);
    expect(w.to).toBeGreaterThan(now + 3 * 3600);
    // A moment later, the same window.
    expect(windowOf("6h", now + 5)).toEqual(w);
  });

  it("reads a zoom only when it is two times in order", () => {
    expect(zoomOf("100", "200")).toEqual([100, 200]);
    expect(zoomOf("200", "100")).toBeUndefined();
    expect(zoomOf("100", null)).toBeUndefined();
    expect(zoomOf(null, "200")).toBeUndefined();
    expect(zoomOf("abc", "200")).toBeUndefined();
    expect(zoomOf("100", "Infinity")).toBeUndefined();
  });
});

describe("downsampling", () => {
  it("leaves a series alone that fits its pixels", () => {
    const x = [1, 2, 3, 4];
    const columns = [[1, 2, 3, 4]];
    expect(downsample(x, columns, 2)).toEqual({ x, columns });
  });

  it("keeps the ends, the lowest and the highest of each bucket", () => {
    const n = 10_000;
    const x = Array.from({ length: n }, (_, i) => i);
    const values = x.map((i) => Math.sin(i / 50));
    values[4321] = 99; // a spike that must survive
    values[8765] = -99;
    const out = downsample(x, [values], 100);

    expect(out.x.length).toBeLessThanOrEqual(2 * 100 + 2);
    expect(out.x.length).toBeGreaterThan(100);
    expect(out.x[0]).toBe(0);
    expect(out.x.at(-1)).toBe(n - 1);
    expect(out.columns[0]).toContain(99);
    expect(out.columns[0]).toContain(-99);
    // Still in time order.
    expect([...out.x].sort((a, b) => a - b)).toEqual(out.x);
  });

  it("keeps the columns aligned, gaps included", () => {
    const n = 1000;
    const x = Array.from({ length: n }, (_, i) => i);
    const a = x.map((i) => i);
    const b = x.map((i) => (i % 2 === 0 ? null : -i));
    const gap = x.map(() => null);
    const out = downsample(x, [a, b, gap], 10);
    expect(out.columns).toHaveLength(3);
    for (const column of out.columns) expect(column).toHaveLength(out.x.length);
    out.x.forEach((xi, k) => {
      expect(out.columns[0]![k]).toBe(xi);
      expect(out.columns[1]![k]).toBe(xi % 2 === 0 ? null : -xi);
      expect(out.columns[2]![k]).toBeNull();
    });
  });

  it("copes with a width of nothing", () => {
    const x = [1, 2, 3, 4, 5];
    const out = downsample(x, [[5, 1, 9, 2, 3]], 0);
    expect(out.x).toEqual([1, 2, 3, 5]);
  });
});

describe("the part of a series in view", () => {
  const x = [10, 20, 30, 40, 50, 60];
  const columns = [
    [1, 2, 3, 4, 5, 6],
    [null, 20, 30, null, 50, 60],
  ];

  it("is the whole series when there is no range", () => {
    expect(windowed(x, columns, undefined)).toEqual({ x, columns });
    expect(windowed(x, columns, undefined).x).toBe(x);
  });

  it("is the points inside the range, and one either side so the line reaches the edge", () => {
    expect(windowed(x, columns, [28, 42])).toEqual({
      x: [20, 30, 40, 50],
      columns: [
        [2, 3, 4, 5],
        [20, 30, null, 50],
      ],
    });
    // A range that ends on a point keeps the point.
    expect(windowed(x, columns, [30, 40]).x).toEqual([20, 30, 40, 50]);
  });

  it("stops at the ends of the series, and keeps the same arrays when everything is in view", () => {
    expect(windowed(x, columns, [0, 25]).x).toEqual([10, 20, 30]);
    expect(windowed(x, columns, [45, 999]).x).toEqual([40, 50, 60]);
    expect(windowed(x, columns, [0, 999]).x).toBe(x);
    expect(windowed(x, columns, [10, 60]).columns).toBe(columns);
  });

  it("has the neighbours of a range that holds no point, and nothing of a series of nothing", () => {
    expect(windowed(x, columns, [32, 38]).x).toEqual([30, 40]);
    expect(windowed(x, columns, [70, 80]).x).toEqual([60]);
    expect(windowed([], [[]], [0, 10])).toEqual({ x: [], columns: [[]] });
  });
});

describe("the hidden series in the address", () => {
  it("are read for one chart, in order, and anything else is ignored", () => {
    expect(hiddenOf("export.1,load.0,export.0", "export")).toEqual([0, 1]);
    expect(hiddenOf("export.1,load.0", "load")).toEqual([0]);
    expect(hiddenOf("export.1,export.1", "export")).toEqual([1]);
    expect(hiddenOf(null, "export")).toEqual([]);
    for (const junk of ["", "export", "export.", "export.x", "export.-1", "export.1.2", "exp.1"])
      expect(hiddenOf(junk, "export")).toEqual([]);
  });

  it("are written for one chart, leaving the others as they were", () => {
    expect(withHidden("export.1,load.0", "export", [2, 0])).toBe("load.0,export.0,export.2");
    expect(withHidden(null, "export", [1])).toBe("export.1");
    expect(withHidden("export.1,load.0", "export", [])).toBe("load.0");
    expect(withHidden("export.1", "export", [3, 3])).toBe("export.3");
    // With nothing hidden anywhere the parameter goes.
    expect(withHidden("export.1", "export", [])).toBeNull();
    expect(withHidden(null, "export", [])).toBeNull();
  });
});

describe("a click on a series of the legend", () => {
  it("hides it, and shows it again", () => {
    expect(toggleHidden([], 1, 3)).toEqual([1]);
    expect(toggleHidden([2], 0, 3)).toEqual([0, 2]);
    expect(toggleHidden([0, 2], 2, 3)).toEqual([0]);
  });

  it("with the other key held, shows it alone; and alone already, shows everything", () => {
    expect(toggleHidden([], 1, 3, true)).toEqual([0, 2]);
    expect(toggleHidden([1], 1, 3, true)).toEqual([0, 2]);
    expect(toggleHidden([0, 2], 1, 3, true)).toEqual([]);
    expect(toggleHidden([0], 0, 1, true)).toEqual([]);
  });
});
