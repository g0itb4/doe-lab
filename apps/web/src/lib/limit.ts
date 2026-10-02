import { kw, percent } from "./format.ts";

// A reading against the limit it is held to: one comparison, worked out in one
// place, so that every page says the same thing about the same numbers.

// A reading within this many watts of a limit is at it: an inverter that
// curtails holds its export just under the limit, not on it. The API counts a
// site as over its limit from the same distance above it.
export const LIMIT_BAND_W = 50;

export type Use = {
  // The way the power flows, and so which limit it is measured against.
  direction: "export" | "import";
  // The flow that way and the limit on it, in watts: neither is negative.
  usedW: number;
  limitW: number;
  // The flow as a share of the limit, not capped: above 1 is over the limit.
  // Against a limit of nothing it is 1 for a flow and 0 for none.
  share: number;
  // What is left of the limit, in watts: negative when the flow is over it.
  headroomW: number;
  // Where the flow stands, in a word or two: "90 %", "at the limit",
  // "0.4 kW over".
  brief: string;
  // The whole comparison as a sentence.
  text: string;
};

// A net export against the limits in force. Unset when there is no reading,
// or no limit on the way the power flows.
export function useOf(
  netExportW: number | undefined,
  exportLimitW: number | undefined,
  importLimitW: number | undefined,
): Use | undefined {
  if (netExportW === undefined) return undefined;
  const direction = netExportW < 0 ? "import" : "export";
  const limitW = direction === "import" ? importLimitW : exportLimitW;
  if (limitW === undefined) return undefined;
  const usedW = Math.abs(netExportW);
  const headroomW = limitW - usedW;
  const flow = direction === "import" ? "Importing" : "Exporting";
  const against = `${flow} ${kw(usedW)} of ${kw(limitW)} allowed`;

  if (headroomW < -LIMIT_BAND_W) {
    const brief = `${kw(-headroomW)} over`;
    const share = limitW > 0 ? usedW / limitW : 1;
    return { direction, usedW, limitW, share, headroomW, brief, text: `${against}: ${brief}.` };
  }
  if (limitW === 0) {
    // Not "at" a limit of nothing: a charger that may not export, and does not.
    const brief = "none allowed";
    const text = `${flow} nothing: no ${direction} is allowed.`;
    return { direction, usedW, limitW, share: 0, headroomW, brief, text };
  }
  const share = usedW / limitW;
  if (headroomW <= LIMIT_BAND_W) {
    const brief = "at the limit";
    return { direction, usedW, limitW, share, headroomW, brief, text: `${against}: ${brief}.` };
  }
  const brief = percent(share * 100);
  const text = `${against}: ${brief}, ${kw(headroomW)} to spare.`;
  return { direction, usedW, limitW, share, headroomW, brief, text };
}
