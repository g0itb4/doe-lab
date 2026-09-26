import { create } from "@bufbuild/protobuf";
import { GetFeederSeriesResponseSchema } from "@doelab/gen/doelab/v1/telemetry_pb.js";
import { describe, expect, it } from "vitest";
import { overviewCharts } from "./overview.ts";
import { timestamp } from "./time.ts";

// 3 November 2026, 11:00 in Sydney, by the half hour.
const start = Date.UTC(2026, 10, 3) / 1000;
const zone = "Australia/Sydney";

function response(points: Parameters<typeof create<typeof GetFeederSeriesResponseSchema>>[1] = {}) {
  const net = [40_000, -20_000, 10_000];
  const base = {
    vMinPu: 0.94,
    vMaxPu: 1.1,
    transformerKva: 500,
    staticLimitW: 5000,
    points: net.map((forecastNetLoadW, i) => ({
      validFrom: timestamp(start + i * 1800),
      validTo: timestamp(start + (i + 1) * 1800),
      forecastNetLoadW,
      forecastLoadingPct: 8,
      forecastVMinPu: 1.0 + i / 100,
      forecastVMaxPu: 1.05 + i / 100,
      exportLimitTotalW: [30_000, 12_000, 40_000][i],
      importLimitTotalW: 100_000,
      staticLimitTotalW: 280_000,
      staticVMaxPu: [1.08, 1.13, 1.12][i],
      envelopeVMaxPu: [1.06, 1.09, 1.094][i],
      measuredExportW: i === 2 ? undefined : [9000, 11_000][i],
    })),
  };
  return create(GetFeederSeriesResponseSchema, { ...base, ...points });
}

describe("the overview's charts", () => {
  const charts = overviewCharts(response(), 230, zone);

  it("plot every interval against its start", () => {
    expect(charts.load.x).toEqual([start, start + 1800, start + 3600]);
    expect(charts.voltage.x).toEqual(charts.load.x);
    expect(charts.exports.x).toEqual(charts.load.x);
  });

  it("show the forecast flow, and leave the rating out when the flow is far from it", () => {
    expect(charts.load.series.map((s) => s.label)).toEqual(["Forecast"]);
    expect(charts.load.series[0]!.values).toEqual([40_000, -20_000, 10_000]);
    expect(charts.load.summary).toBe(
      "Forecast supply peaks at 40.0 kW at 11:00, 8 % of the 500 kVA rating. Solar pushes 20.0 kW back through the transformer at 11:30.",
    );
  });

  it("draw the rating when the flow comes within half of it", () => {
    const near = overviewCharts(response({ transformerKva: 60 }), 230, zone);
    expect(near.load.series.map((s) => s.label)).toEqual([
      "Forecast",
      "Rating, supply",
      "Rating, reverse flow",
    ]);
    expect(near.load.series[1]!.values).toEqual([60_000, 60_000, 60_000]);
    expect(near.load.series[2]!.values).toEqual([-60_000, -60_000, -60_000]);
  });

  it("show voltage in volts: at the envelopes, at a fixed limit and with no limits, against the band", () => {
    const [held, fixed, free, low, upper, lower] = charts.voltage.series;
    expect(charts.voltage.series.map((s) => s.label)).toEqual([
      "Highest, at the envelopes",
      "Highest, at a fixed limit",
      "Highest, with no limits",
      "Lowest, forecast",
      "Upper limit",
      "Lower limit",
    ]);
    expect(held!.values.map((v) => Math.round(v! * 10) / 10)).toEqual([243.8, 250.7, 251.6]);
    expect(fixed!.values[1]).toBeCloseTo(259.9);
    expect(free!.values.map((v) => Math.round(v! * 10) / 10)).toEqual([241.5, 243.8, 246.1]);
    expect(low!.values[0]).toBeCloseTo(230);
    expect(upper!.values[0]).toBeCloseTo(253);
    expect(lower!.values[0]).toBeCloseTo(216.2);
    expect(charts.voltage.summary).toBe(
      "With every site at its envelope the highest voltage is 251.6\u00a0V at 12:00; the limit is 253.0\u00a0V. If every site exported at a fixed 5.0\u00a0kW it would pass the limit in 2 of 3 intervals.",
    );
  });

  it("say so when a fixed limit would do no harm", () => {
    const calm = response();
    calm.points.forEach((p) => (p.staticVMaxPu = 1.06));
    expect(overviewCharts(calm, 230, zone).voltage.summary).toContain(
      "A fixed 5.0\u00a0kW export limit would stay inside it.",
    );
  });

  it("fall back to the forecast for intervals from before the engine recorded the envelope voltage", () => {
    const old = response();
    old.points.forEach((p) => (p.envelopeVMaxPu = undefined));
    const voltage = overviewCharts(old, 230, zone).voltage;
    expect(voltage.series[0]!.values).toEqual([null, null, null]);
    expect(voltage.summary).toContain("The highest forecast voltage is 246.1\u00a0V at 12:00");
  });

  it("show what the envelopes allow against a fixed limit and what was measured", () => {
    const [allowed, fixed, measured] = charts.exports.series;
    expect(allowed).toMatchObject({ values: [30_000, 12_000, 40_000], stepped: true });
    expect(fixed!.values).toEqual([280_000, 280_000, 280_000]);
    // An interval with no telemetry is a gap, not a zero.
    expect(measured!.values).toEqual([9000, 11_000, null]);
    expect(charts.exports.summary).toBe(
      "The envelopes allow at least 12.0 kW of export in total (at 11:30); a fixed limit would allow 280.0 kW throughout. The fleet's measured export peaks at 11.0 kW at 11:30.",
    );
  });

  it("say when there is no telemetry yet", () => {
    const quiet = response();
    quiet.points.forEach((p) => (p.measuredExportW = undefined));
    expect(overviewCharts(quiet, 230, zone).exports.summary).toContain("No telemetry yet.");
  });

  it("say when there is nothing for the range", () => {
    const empty = overviewCharts(response({ points: [] }), 230, zone);
    expect(empty.load).toMatchObject({ x: [], summary: "No forecast for this range." });
    expect(empty.voltage.summary).toBe("No forecast for this range.");
    expect(empty.exports.summary).toBe("No envelopes for this range.");
  });

  it("leave the share of the rating out for a feeder with no rating", () => {
    const summary = overviewCharts(response({ transformerKva: 0 }), 230, zone).load.summary;
    expect(summary).toContain("peaks at 40.0 kW at 11:00.");
    expect(summary).not.toContain("rating");
  });

  it("do not speak of reverse flow when there is none", () => {
    const supply = response();
    supply.points.forEach((p) => (p.forecastNetLoadW = Math.abs(p.forecastNetLoadW)));
    expect(overviewCharts(supply, 230, zone).load.summary).not.toContain("Solar pushes");
  });
});
