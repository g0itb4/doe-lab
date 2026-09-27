import { create } from "@bufbuild/protobuf";
import { EnvelopeSchema } from "@doelab/gen/doelab/v1/envelope_pb.js";
import { GetSiteSeriesResponseSchema } from "@doelab/gen/doelab/v1/telemetry_pb.js";
import { describe, expect, it } from "vitest";
import { envelopeAt, siteChart } from "./site.ts";
import { timestamp } from "./time.ts";

// 3 November 2026, 11:00 in Sydney.
const t0 = Date.UTC(2026, 10, 3) / 1000;
const zone = "Australia/Sydney";
const envelope = (slot: number, exportLimitW: number, superseded = false) =>
  create(EnvelopeSchema, {
    id: `e-${slot}-${exportLimitW}`,
    validFrom: timestamp(t0 + slot * 1800),
    validTo: timestamp(t0 + (slot + 1) * 1800),
    exportLimitW,
    importLimitW: 7000,
    supersededAt: superseded ? timestamp(t0) : undefined,
  });

const series = create(GetSiteSeriesResponseSchema, {
  power: [
    { bucket: timestamp(t0), avgNetExportW: 1900 },
    { bucket: timestamp(t0 + 60), avgNetExportW: 2000 },
    { bucket: timestamp(t0 + 1800), avgNetExportW: 1000 },
  ],
  forecast: [
    { ts: timestamp(t0), loadW: 500, pvW: 3500 },
    { ts: timestamp(t0 + 1800), loadW: 600, pvW: 3000 },
  ],
});

describe("the chart of a site", () => {
  const chart = siteChart(
    [envelope(1, 1000), envelope(0, 2000), envelope(0, 9999, true)],
    series,
    zone,
  );

  it("is drawn on the union of the three grids, in time order", () => {
    expect(chart.x).toEqual([t0, t0 + 60, t0 + 1800, t0 + 3600]);
  });

  it("repeats a limit at every time inside its interval, and ignores a superseded envelope", () => {
    const [limit, measured, free, importLimit] = chart.series;
    expect(limit).toMatchObject({
      label: "Export limit",
      values: [2000, 2000, 1000, null],
      stepped: true,
    });
    expect(measured!.values).toEqual([1900, 2000, 1000, null]);
    expect(free!.values).toEqual([3000, 3000, 2400, null]);
    // Import is drawn below zero, on the same axis as export.
    expect(importLimit!.values).toEqual([-7000, -7000, -7000, null]);
  });

  it("says what it shows", () => {
    expect(chart.summary).toBe(
      "The export limit moves between 1.0 kW and 2.0 kW. Measured net export peaks at 2.0 kW at 11:01.",
    );
    expect(
      siteChart([envelope(0, 1500)], create(GetSiteSeriesResponseSchema, {}), zone).summary,
    ).toBe("The export limit is 1.5 kW throughout. No telemetry for this range.");
    expect(siteChart([], create(GetSiteSeriesResponseSchema, {}), zone)).toMatchObject({
      x: [],
      summary: "No envelope for this range. No telemetry for this range.",
    });
  });

  it("leaves a gap where an interval has no envelope", () => {
    const gapped = siteChart(
      [envelope(0, 2000), envelope(2, 1000)],
      create(GetSiteSeriesResponseSchema, {}),
      zone,
    );
    expect(gapped.x).toEqual([t0, t0 + 1800, t0 + 3600, t0 + 5400]);
    expect(gapped.series[0]!.values).toEqual([2000, null, 1000, null]);
  });
});

describe("the envelope in force", () => {
  const list = [envelope(0, 2000), envelope(1, 9999, true), envelope(1, 1000)];

  it("is the active one whose interval holds the instant", () => {
    expect(envelopeAt(list, t0 + 10)?.exportLimitW).toBe(2000);
    expect(envelopeAt(list, t0 + 1800)?.exportLimitW).toBe(1000);
    expect(envelopeAt(list, t0 + 3600)).toBeUndefined();
    expect(envelopeAt(list, t0 - 1)).toBeUndefined();
  });
});
