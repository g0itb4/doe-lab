import { describe, expect, it } from "vitest";
import {
  boxOf,
  centre,
  fit,
  MAX_ZOOM,
  panBy,
  viewOf,
  viewParam,
  WHOLE,
  zoomBy,
} from "./viewbox.ts";

const size = { width: 1000, height: 400 };

describe("the part of a drawing on show", () => {
  it("is the whole drawing to begin with", () => {
    expect(boxOf(WHOLE, size)).toBe("0 0 1000 400");
    expect(centre(WHOLE, size)).toEqual({ x: 500, y: 200 });
  });

  it("zooms about a point, which stays where it is", () => {
    // Twice as close about the middle: the middle half of the drawing.
    const twice = zoomBy(WHOLE, 2, { x: 500, y: 200 }, size);
    expect(twice).toEqual({ x: 250, y: 100, k: 2 });
    expect(boxOf(twice, size)).toBe("250 100 500 200");
    // About a point a fifth of the way across: it is still a fifth across.
    const about = zoomBy(WHOLE, 4, { x: 200, y: 80 }, size);
    expect(about).toEqual({ x: 150, y: 60, k: 4 });
    expect((200 - about.x) / (size.width / about.k)).toBeCloseTo(0.2);
    // And out again about the same point is where it began.
    expect(zoomBy(about, 0.25, { x: 200, y: 80 }, size)).toEqual(WHOLE);
  });

  it("zooms no closer than its limit, and no further out than the whole", () => {
    expect(zoomBy(WHOLE, 100, { x: 500, y: 200 }, size).k).toBe(MAX_ZOOM);
    expect(zoomBy({ x: 250, y: 100, k: 2 }, 0.1, { x: 500, y: 200 }, size)).toEqual(WHOLE);
    expect(fit({ x: 10, y: 10, k: 0.5 }, size)).toEqual(WHOLE);
    expect(fit({ x: 0, y: 0, k: 99 }, size).k).toBe(MAX_ZOOM);
  });

  it("moves, and stops at the edges of the drawing", () => {
    const view = { x: 250, y: 100, k: 2 };
    expect(panBy(view, 100, -50, size)).toEqual({ x: 350, y: 50, k: 2 });
    expect(panBy(view, 9999, 9999, size)).toEqual({ x: 500, y: 200, k: 2 });
    expect(panBy(view, -9999, -9999, size)).toEqual({ x: 0, y: 0, k: 2 });
    // The whole drawing has nowhere to move to.
    expect(panBy(WHOLE, 100, 100, size)).toEqual(WHOLE);
    expect(centre(view, size)).toEqual({ x: 500, y: 200 });
  });

  it("zooms about a corner without leaving the drawing", () => {
    expect(zoomBy(WHOLE, 2, { x: 1000, y: 400 }, size)).toEqual({ x: 500, y: 200, k: 2 });
    expect(zoomBy(WHOLE, 2, { x: 0, y: 0 }, size)).toEqual({ x: 0, y: 0, k: 2 });
  });
});

describe("the view in the address", () => {
  it("is written as three numbers, and not at all for the whole drawing", () => {
    expect(viewParam({ x: 150.4, y: 60.6, k: 4 })).toBe("150,61,4");
    expect(viewParam({ x: 10, y: 0, k: 1.2549 })).toBe("10,0,1.25");
    expect(viewParam(WHOLE)).toBeNull();
  });

  it("is read back, and anything else is no view", () => {
    expect(viewOf("150,61,4")).toEqual({ x: 150, y: 61, k: 4 });
    expect(viewOf("0,0,99")).toEqual({ x: 0, y: 0, k: MAX_ZOOM });
    for (const junk of [
      null,
      "",
      "1,2",
      "1,2,3,4",
      "a,b,c",
      "0,0,1",
      "0,0,0.5",
      "-5,0,2",
      "0,-5,2",
    ])
      expect(viewOf(junk)).toBeUndefined();
  });
});
