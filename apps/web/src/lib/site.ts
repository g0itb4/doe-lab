import type { Envelope } from "@doelab/gen/doelab/v1/envelope_pb.js";
import type { GetSiteSeriesResponse } from "@doelab/gen/doelab/v1/telemetry_pb.js";
import type { ChartSeries } from "./components/Chart.svelte";
import { clockTime, kw } from "./format.ts";
import { seconds } from "./time.ts";

// The chart of one site: its envelope, what it was forecast to do with no
// limit, and what it did.
//
// The three come on different grids (the envelope by the interval, the
// forecast by the half hour, the telemetry by the minute), so the chart is
// drawn on the union of their times, and a value that holds for a period is
// repeated at every time inside it.
export function siteChart(
  envelopes: Envelope[],
  series: GetSiteSeriesResponse,
  zone: string,
): { x: number[]; series: ChartSeries[]; summary: string } {
  const active = envelopes
    .filter((e) => e.supersededAt === undefined)
    .map((e) => ({
      from: seconds(e.validFrom),
      to: seconds(e.validTo),
      exportW: e.exportLimitW,
      importW: e.importLimitW,
    }))
    .sort((a, b) => a.from - b.from);
  const forecast = series.forecast.map((f) => ({
    from: seconds(f.ts),
    to: seconds(f.ts) + 1800,
    netW: f.pvW - f.loadW,
  }));
  const measured = new Map(series.power.map((p) => [seconds(p.bucket), p.avgNetExportW]));

  const times = new Set<number>();
  for (const e of active) times.add(e.from).add(e.to);
  for (const f of forecast) times.add(f.from);
  for (const t of measured.keys()) times.add(t);
  const x = [...times].sort((a, b) => a - b);

  // Both lists are in time order, and so is x: one pass each.
  const during = <T extends { from: number; to: number }>(periods: T[], pick: (p: T) => number) => {
    let i = 0;
    return x.map((t) => {
      while (i < periods.length && periods[i]!.to <= t) i++;
      const p = periods[i];
      return p && p.from <= t ? pick(p) : null;
    });
  };
  const exportLimit = during(active, (e) => e.exportW);
  const importLimit = during(active, (e) => -e.importW);
  const free = during(forecast, (f) => f.netW);
  const net = x.map((t) => measured.get(t) ?? null);

  const time = (at: number) => clockTime(new Date(at * 1000), zone);
  let summary = "No envelope for this range.";
  if (active.length > 0) {
    const limits = active.map((e) => e.exportW);
    const [lo, hi] = [Math.min(...limits), Math.max(...limits)];
    summary =
      lo === hi
        ? `The export limit is ${kw(lo)} throughout.`
        : `The export limit moves between ${kw(lo)} and ${kw(hi)}.`;
  }
  let peak: { value: number; at: number } | undefined;
  for (const [at, value] of measured) if (!peak || value > peak.value) peak = { value, at };
  summary += peak
    ? ` Measured net export peaks at ${kw(peak.value)} at ${time(peak.at)}.`
    : " No telemetry for this range.";

  return {
    x,
    series: [
      { label: "Export limit", values: exportLimit, color: 1, stepped: true },
      { label: "Measured net export", values: net, color: 3 },
      { label: "Forecast, with no limit", values: free, color: 4, stepped: true },
      { label: "Import limit, as export", values: importLimit, color: "ref", stepped: true },
    ],
    summary,
  };
}

// The envelope in force at an instant, if the list has it.
export function envelopeAt(envelopes: Envelope[], at: number): Envelope | undefined {
  return envelopes.find(
    (e) => e.supersededAt === undefined && seconds(e.validFrom) <= at && at < seconds(e.validTo),
  );
}
