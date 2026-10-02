import { describe, expect, it } from "vitest";
import { LIMIT_BAND_W, useOf } from "./limit.ts";

describe("a reading against its limit", () => {
  it("is a share of the export limit, with what is left to spare", () => {
    expect(useOf(2700, 3000, 5000)).toEqual({
      direction: "export",
      usedW: 2700,
      limitW: 3000,
      share: 0.9,
      headroomW: 300,
      brief: "90 %",
      text: "Exporting 2.7 kW of 3.0 kW allowed: 90 %, 0.3 kW to spare.",
    });
    // A site that does nothing exports nothing of what it may.
    expect(useOf(0, 3000, undefined)).toMatchObject({ direction: "export", share: 0 });
  });

  it("is measured against the import limit when the power flows in", () => {
    expect(useOf(-1200, 3000, 4800)).toMatchObject({
      direction: "import",
      usedW: 1200,
      limitW: 4800,
      share: 0.25,
      headroomW: 3600,
      text: "Importing 1.2 kW of 4.8 kW allowed: 25 %, 3.6 kW to spare.",
    });
  });

  it("is at the limit within a band on either side of it", () => {
    expect(LIMIT_BAND_W).toBe(50);
    const under = useOf(2960, 3000, undefined)!;
    expect(under.brief).toBe("at the limit");
    expect(under.text).toBe("Exporting 3.0 kW of 3.0 kW allowed: at the limit.");
    expect(useOf(3050, 3000, undefined)!.brief).toBe("at the limit");
    expect(useOf(2949, 3000, undefined)!.brief).toBe("98 %");
  });

  it("is over the limit beyond the band, by how much", () => {
    expect(useOf(3400, 3000, undefined)).toMatchObject({
      headroomW: -400,
      brief: "0.4 kW over",
      text: "Exporting 3.4 kW of 3.0 kW allowed: 0.4 kW over.",
    });
    expect(useOf(3400, 3000, undefined)!.share).toBeCloseTo(1.133, 3);
  });

  it("has a share against a limit of nothing: all of it for a flow, none for none", () => {
    // A backstop of zero, and a site that exports all the same.
    expect(useOf(2000, 0, undefined)).toMatchObject({
      share: 1,
      brief: "2.0 kW over",
      text: "Exporting 2.0 kW of 0.0 kW allowed: 2.0 kW over.",
    });
    // A charger that may not export, and does not, is not at a limit.
    expect(useOf(0, 0, 7000)).toMatchObject({
      share: 0,
      brief: "none allowed",
      text: "Exporting nothing: no export is allowed.",
    });
    expect(useOf(-30, 5000, 0)!.text).toBe("Importing nothing: no import is allowed.");
  });

  it("is unset with no reading, or no limit on the way the power flows", () => {
    expect(useOf(undefined, 3000, 5000)).toBeUndefined();
    expect(useOf(1000, undefined, 5000)).toBeUndefined();
    expect(useOf(-1000, 3000, undefined)).toBeUndefined();
  });
});
