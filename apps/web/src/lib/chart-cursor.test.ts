import { describe, expect, it } from "vitest";
import { CURSOR_KEYS, place, rowAt, spanAt, step, valueText } from "./chart-cursor.ts";

describe("the point at a time", () => {
  const x = [100, 200, 300, 400];

  it("is the point itself when the time is one", () => {
    expect(x.map((t) => rowAt(x, t))).toEqual([0, 1, 2, 3]);
  });

  it("is the nearest point when the time falls between two, and the earlier of two as near", () => {
    expect(rowAt(x, 149)).toBe(0);
    expect(rowAt(x, 151)).toBe(1);
    expect(rowAt(x, 150)).toBe(0);
    expect(rowAt(x, 399)).toBe(3);
  });

  it("is an end when the time is past it, and nothing for a chart of nothing", () => {
    expect(rowAt(x, -5)).toBe(0);
    expect(rowAt(x, 9999)).toBe(3);
    expect(rowAt([42], 7)).toBe(0);
    expect(rowAt([], 7)).toBe(-1);
  });

  it("finds its point in a long series as it does in a short one", () => {
    const long = Array.from({ length: 10_001 }, (_, i) => i * 60);
    for (const i of [0, 1, 4999, 5000, 10_000]) expect(rowAt(long, i * 60 + 20)).toBe(i);
  });
});

describe("where the readout sits", () => {
  const plot = { width: 600, height: 200 };

  it("is to the right of and below a cursor in the top left", () => {
    expect(place({ left: 100, top: 40 }, plot)).toEqual({ left: 112, top: 52, maxWidth: 488 });
  });

  it("is to the left of and above a cursor in the bottom right, so it stays in the plot", () => {
    expect(place({ left: 500, top: 150 }, plot)).toEqual({
      right: 112,
      bottom: 62,
      maxWidth: 488,
    });
  });

  it("flips at the middle, each way on its own", () => {
    expect(place({ left: 300, top: 100 }, plot, 8)).toMatchObject({ left: 308, top: 108 });
    expect(place({ left: 301, top: 101 }, plot, 8)).toMatchObject({ right: 307, bottom: 107 });
  });

  it("never starts outside the plot, for a cursor that is", () => {
    expect(place({ left: 10, top: -40 }, plot)).toMatchObject({ top: 0 });
    expect(place({ left: 10, top: 400 }, plot)).toMatchObject({ bottom: 0 });
  });

  it("is no wider than the room on its side, which on a narrow plot is not much", () => {
    const narrow = { width: 240, height: 200 };
    expect(place({ left: 72, top: 100 }, narrow).maxWidth).toBe(156);
    expect(place({ left: 200, top: 100 }, narrow).maxWidth).toBe(188);
    expect(place({ left: 700, top: 100 }, { width: 0, height: 0 }).maxWidth).toBe(688);
    expect(place({ left: -40, top: 100 }, { width: 20, height: 0 }, 80).maxWidth).toBe(0);
  });
});

describe("a key on the chart", () => {
  it("moves the cursor by a point, by a tenth, or to an end", () => {
    expect(step(100, 50, "ArrowRight")).toBe(51);
    expect(step(100, 50, "ArrowUp")).toBe(51);
    expect(step(100, 50, "ArrowLeft")).toBe(49);
    expect(step(100, 50, "ArrowDown")).toBe(49);
    expect(step(100, 50, "PageUp")).toBe(60);
    expect(step(100, 50, "PageDown")).toBe(40);
    expect(step(100, 50, "Home")).toBe(0);
    expect(step(100, 50, "End")).toBe(99);
  });

  it("stops at the ends", () => {
    expect(step(100, 0, "ArrowLeft")).toBe(0);
    expect(step(100, 99, "ArrowRight")).toBe(99);
    expect(step(100, 95, "PageUp")).toBe(99);
    expect(step(100, 3, "PageDown")).toBe(0);
    // A chart of a few points still moves by one.
    expect(step(4, 1, "PageUp")).toBe(2);
  });

  it("starts from where it is told when no point is chosen yet", () => {
    expect(step(100, undefined, "ArrowRight", 42)).toBe(42);
    expect(step(100, undefined, "ArrowLeft")).toBe(0);
    expect(step(100, undefined, "PageUp", 500)).toBe(99);
    expect(step(100, undefined, "End", 42)).toBe(99);
  });

  it("is not the cursor's when it is any other key, or the chart is empty", () => {
    expect(step(100, 50, "a")).toBeUndefined();
    expect(step(100, 50, "Enter")).toBeUndefined();
    expect(step(0, undefined, "ArrowRight")).toBeUndefined();
    expect(CURSOR_KEYS).toHaveLength(8);
  });
});

describe("a point in words", () => {
  it("is the time, then each series with its value", () => {
    expect(
      valueText("Tue 3 Nov, 12:00", [
        { label: "Export limit", value: "2.0 kW" },
        { label: "Fixed limit", value: "5.0 kW" },
      ]),
    ).toBe("Tue 3 Nov, 12:00. Export limit 2.0 kW, Fixed limit 5.0 kW.");
  });

  it("says so when every series is hidden", () => {
    expect(valueText("Tue 3 Nov, 12:00", [])).toBe("Tue 3 Nov, 12:00. No series is shown.");
  });
});

describe("the mark under the cursor", () => {
  const spans = [
    { id: "a", from: 100, to: 200 },
    { id: "b", from: 150, to: 400 },
    { from: 500, to: 600 },
  ];

  it("is the first mark that holds the time, by its id", () => {
    expect(spanAt(spans, 100)).toBe("a");
    expect(spanAt(spans, 175)).toBe("a");
    expect(spanAt(spans, 201)).toBe("b");
    expect(spanAt(spans, 400)).toBe("b");
  });

  it("is nothing outside every mark, in a mark with no id, or with no time", () => {
    expect(spanAt(spans, 450)).toBeUndefined();
    expect(spanAt(spans, 550)).toBeUndefined();
    expect(spanAt(spans, undefined)).toBeUndefined();
    expect(spanAt([], 100)).toBeUndefined();
  });
});
