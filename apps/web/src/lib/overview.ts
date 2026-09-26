import type { GetFeederSeriesResponse } from "@doelab/gen/doelab/v1/telemetry_pb.js";
import type { ChartSeries } from "./components/Chart.svelte";
import { clockTime, count, kw, percent, volts } from "./format.ts";
import { seconds } from "./time.ts";

export type ChartData = { x: number[]; series: ChartSeries[]; summary: string };
export type OverviewCharts = { load: ChartData; voltage: ChartData; exports: ChartData };

// Where a column peaks: the value and its time.
function peak(
  x: number[],
  values: (number | null)[],
  sign: 1 | -1 = 1,
): { value: number; at: number } | undefined {
  let best: { value: number; at: number } | undefined;
  values.forEach((v, i) => {
    if (v !== null && (best === undefined || sign * v > sign * best.value))
      best = { value: v, at: x[i]! };
  });
  return best;
}

// The three charts of the overview, from one series of the feeder: the data
// of each, and a sentence that says what it shows.
export function overviewCharts(
  res: GetFeederSeriesResponse,
  nominalV: number,
  zone: string,
): OverviewCharts {
  const points = res.points;
  const x = points.map((p) => seconds(p.validFrom));
  const time = (at: number) => clockTime(new Date(at * 1000), zone);
  const ratingW = res.transformerKva * 1000;

  // 1. Power through the transformer.
  const load = points.map((p) => p.forecastNetLoadW);
  const loadSeries: ChartSeries[] = [{ label: "Forecast", values: load, color: 1 }];
  const top = peak(x, load);
  const bottom = peak(x, load, -1);
  // The rating is drawn only when the flow comes near it: on a lightly
  // loaded feeder it would flatten the line that matters.
  if (
    ratingW > 0 &&
    Math.max(Math.abs(top?.value ?? 0), Math.abs(bottom?.value ?? 0)) >= ratingW / 2
  ) {
    loadSeries.push(
      { label: "Rating, supply", values: x.map(() => ratingW), color: "ref" },
      { label: "Rating, reverse flow", values: x.map(() => -ratingW), color: "ref" },
    );
  }
  let loadSummary = "No forecast for this range.";
  if (top && bottom) {
    const share =
      ratingW > 0
        ? `, ${percent((Math.abs(top.value) / ratingW) * 100)} of the ${count(res.transformerKva)} kVA rating`
        : "";
    loadSummary = `Forecast supply peaks at ${kw(top.value)} at ${time(top.at)}${share}.`;
    if (bottom.value < 0)
      loadSummary += ` Solar pushes ${kw(-bottom.value)} back through the transformer at ${time(bottom.at)}.`;
  }

  // 2. Customer voltage: where it would go with no limits, where a fixed
  // limit would take it, and where the envelopes hold it.
  const free = points.map((p) => p.forecastVMaxPu * nominalV);
  const low = points.map((p) => p.forecastVMinPu * nominalV);
  const fixedHigh = points.map((p) => p.staticVMaxPu * nominalV);
  // Unset for an interval from before the engine recorded it: a gap.
  const held = points.map((p) =>
    p.envelopeVMaxPu === undefined ? null : p.envelopeVMaxPu * nominalV,
  );
  const upper = res.vMaxPu * nominalV;
  const lower = res.vMinPu * nominalV;
  const inV = (v: number) => volts(v / nominalV, nominalV);
  const over = fixedHigh.filter((v) => v > upper).length;
  const highest = peak(x, held) ?? peak(x, free);
  let voltageSummary = "No forecast for this range.";
  if (highest) {
    const what = peak(x, held)
      ? "With every site at its envelope the highest voltage"
      : "The highest forecast voltage";
    voltageSummary = `${what} is ${inV(highest.value)} at ${time(highest.at)}; the limit is ${inV(upper)}.`;
    voltageSummary +=
      over > 0
        ? ` If every site exported at a fixed ${kw(res.staticLimitW)} it would pass the limit in ${over} of ${points.length} intervals.`
        : ` A fixed ${kw(res.staticLimitW)} export limit would stay inside it.`;
  }

  // 3. Export: allowed and measured.
  const allowed = points.map((p) => p.exportLimitTotalW);
  const fixed = points.map((p) => p.staticLimitTotalW);
  const measured = points.map((p) => p.measuredExportW ?? null);
  const least = peak(x, allowed, -1);
  const most = peak(x, measured);
  let exportSummary = "No envelopes for this range.";
  if (least) {
    exportSummary = `The envelopes allow at least ${kw(least.value)} of export in total (at ${time(least.at)}); a fixed limit would allow ${kw(fixed[0] ?? 0)} throughout.`;
    exportSummary += most
      ? ` The fleet's measured export peaks at ${kw(most.value)} at ${time(most.at)}.`
      : " No telemetry yet.";
  }

  return {
    load: { x, series: loadSeries, summary: loadSummary },
    voltage: {
      x,
      series: [
        { label: "Highest, at the envelopes", values: held, color: 1 },
        { label: "Highest, at a fixed limit", values: fixedHigh, color: 2 },
        { label: "Highest, with no limits", values: free, color: 4 },
        { label: "Lowest, forecast", values: low, color: 3 },
        { label: "Upper limit", values: x.map(() => upper), color: "ref" },
        { label: "Lower limit", values: x.map(() => lower), color: "ref" },
      ],
      summary: voltageSummary,
    },
    exports: {
      x,
      series: [
        { label: "Allowed by the envelopes", values: allowed, color: 1, stepped: true },
        { label: "Allowed by a fixed limit", values: fixed, color: 2, stepped: true },
        { label: "Measured export", values: measured, color: 3 },
      ],
      summary: exportSummary,
    },
  };
}
