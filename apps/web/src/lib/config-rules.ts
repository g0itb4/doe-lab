import { EnvelopePolicy } from "@doelab/gen/doelab/v1/common_pb.js";
import type { EnvelopeConfig } from "@doelab/gen/doelab/v1/envelope_config_pb.js";

// The fields of an envelope config that an operator sets, as the form holds
// them: in the units an operator thinks in (volts and kilowatts, not per-unit
// and watts).
export type ConfigForm = {
  policy: EnvelopePolicy;
  vMinV: number;
  vMaxV: number;
  transformerLimitPct: number;
  lineLimitPct: number;
  pvScale: number;
  staticLimitKw: number;
  intervalMinutes: number;
  horizonIntervals: number;
  breachGraceSeconds: number;
  offlineAfterSeconds: number;
  note: string;
};
export type NumberField = Exclude<keyof ConfigForm, "policy" | "note">;

// The ranges of the API, in the API's units. They mirror the protovalidate
// rules of EnvelopeConfig in proto/doelab/v1/envelope_config.proto, and
// config-rules.test.ts reads that file to make sure they still do.
export const RULES = {
  v_min_pu: { gte: 0.8, lte: 1.2 },
  v_max_pu: { gte: 0.8, lte: 1.2 },
  transformer_limit_pct: { gt: 0, lte: 200 },
  line_limit_pct: { gt: 0, lte: 200 },
  pv_scale: { gte: 0, lte: 20 },
  static_limit_w: { gte: 0 },
  interval_minutes: { in: [5, 15, 30, 60] },
  horizon_intervals: { gte: 1, lte: 288 },
  breach_grace_seconds: { gte: 0, lte: 3600 },
  offline_after_seconds: { gte: 10, lte: 86400 },
  note: { maxLen: 2000 },
} as const;

type Range = { gte?: number; gt?: number; lte?: number };

// How each number of the form is labelled, and what it may be.
export type FieldSpec = {
  key: NumberField;
  label: string;
  unit: string;
  help: string;
  step: number;
  whole: boolean;
  // The range in the form's units.
  range: Range;
};

export function fieldSpecs(nominalV: number): FieldSpec[] {
  const volts = (r: Range): Range => ({
    gte: round(r.gte! * nominalV),
    lte: round(r.lte! * nominalV),
  });
  return [
    {
      key: "vMinV",
      label: "Lowest voltage allowed",
      unit: "V",
      help: "At any customer, phase to neutral.",
      step: 0.5,
      whole: false,
      range: volts(RULES.v_min_pu),
    },
    {
      key: "vMaxV",
      label: "Highest voltage allowed",
      unit: "V",
      help: "At any customer, phase to neutral.",
      step: 0.5,
      whole: false,
      range: volts(RULES.v_max_pu),
    },
    {
      key: "transformerLimitPct",
      label: "Transformer loading allowed",
      unit: "%",
      help: "Of the transformer's rating.",
      step: 5,
      whole: false,
      range: RULES.transformer_limit_pct,
    },
    {
      key: "lineLimitPct",
      label: "Line loading allowed",
      unit: "%",
      help: "Of each line's current rating.",
      step: 5,
      whole: false,
      range: RULES.line_limit_pct,
    },
    {
      key: "pvScale",
      label: "Solar scale",
      unit: "×",
      help: "Multiplies the 2010 to 2013 solar profiles, to represent today's larger systems.",
      step: 0.1,
      whole: false,
      range: RULES.pv_scale,
    },
    {
      key: "staticLimitKw",
      label: "Fixed limit to compare with",
      unit: "kW",
      help: "The reports compare the envelopes with this limit for every site.",
      step: 0.5,
      whole: false,
      range: { gte: RULES.static_limit_w.gte / 1000 },
    },
    {
      key: "horizonIntervals",
      label: "Horizon",
      unit: "intervals",
      help: "How far ahead each engine run publishes envelopes.",
      step: 1,
      whole: true,
      range: RULES.horizon_intervals,
    },
    {
      key: "breachGraceSeconds",
      label: "Grace before a breach alert",
      unit: "s",
      help: "How long a site may exceed its limit before an alert opens.",
      step: 10,
      whole: true,
      range: RULES.breach_grace_seconds,
    },
    {
      key: "offlineAfterSeconds",
      label: "Silence before an offline alert",
      unit: "s",
      help: "How long a device may say nothing before an alert opens.",
      step: 10,
      whole: true,
      range: RULES.offline_after_seconds,
    },
  ];
}

// A volt value without float noise: 0.94 × 230 is 216.2, not 216.20000000000002.
function round(v: number): number {
  return Math.round(v * 1000) / 1000;
}

export function toForm(config: EnvelopeConfig, nominalV: number): ConfigForm {
  return {
    policy: config.policy,
    vMinV: round(config.vMinPu * nominalV),
    vMaxV: round(config.vMaxPu * nominalV),
    transformerLimitPct: config.transformerLimitPct,
    lineLimitPct: config.lineLimitPct,
    pvScale: config.pvScale,
    staticLimitKw: config.staticLimitW / 1000,
    intervalMinutes: config.intervalMinutes,
    horizonIntervals: config.horizonIntervals,
    breachGraceSeconds: config.breachGraceSeconds,
    offlineAfterSeconds: config.offlineAfterSeconds,
    note: "",
  };
}

// The fields of a new version, in the API's units.
export function fromForm(form: ConfigForm, feederId: string, nominalV: number) {
  return {
    feederId,
    policy: form.policy,
    vMinPu: form.vMinV / nominalV,
    vMaxPu: form.vMaxV / nominalV,
    transformerLimitPct: form.transformerLimitPct,
    lineLimitPct: form.lineLimitPct,
    pvScale: form.pvScale,
    staticLimitW: form.staticLimitKw * 1000,
    intervalMinutes: form.intervalMinutes,
    horizonIntervals: form.horizonIntervals,
    breachGraceSeconds: form.breachGraceSeconds,
    offlineAfterSeconds: form.offlineAfterSeconds,
    note: form.note.trim(),
  };
}

function rangeWords(range: Range, unit: string): string {
  const u = unit === "×" || unit === "intervals" ? "" : ` ${unit}`;
  const low = range.gt !== undefined ? `more than ${range.gt}` : `${range.gte}`;
  if (range.lte === undefined) return `${low}${u} or more`;
  return range.gt !== undefined
    ? `${low} and at most ${range.lte}${u}`
    : `from ${low} to ${range.lte}${u}`;
}

// What is wrong with one field, as an instruction: what to enter instead.
// Empty when the field is fine. The rules are the API's.
export function fieldError(spec: FieldSpec, form: ConfigForm): string {
  const value = form[spec.key];
  if (typeof value !== "number" || !Number.isFinite(value))
    return `Enter a number: ${rangeWords(spec.range, spec.unit)}.`;
  if (spec.whole && !Number.isInteger(value)) return "Enter a whole number.";
  const { gte, gt, lte } = spec.range;
  if (
    (gte !== undefined && value < gte) ||
    (gt !== undefined && value <= gt) ||
    (lte !== undefined && value > lte)
  ) {
    return `Enter ${rangeWords(spec.range, spec.unit)}.`;
  }
  // The band has a bottom and a top.
  if (spec.key === "vMinV" && Number.isFinite(form.vMaxV) && value >= form.vMaxV) {
    return `Enter a voltage below the highest allowed (${form.vMaxV} V).`;
  }
  if (spec.key === "vMaxV" && Number.isFinite(form.vMinV) && value <= form.vMinV) {
    return `Enter a voltage above the lowest allowed (${form.vMinV} V).`;
  }
  return "";
}

export function noteError(form: ConfigForm): string {
  return form.note.length > RULES.note.maxLen
    ? `Shorten the note to ${RULES.note.maxLen} characters or fewer.`
    : "";
}

// Every error of the form, by field.
export function formErrors(
  form: ConfigForm,
  nominalV: number,
): Partial<Record<keyof ConfigForm, string>> {
  const errors: Partial<Record<keyof ConfigForm, string>> = {};
  for (const spec of fieldSpecs(nominalV)) {
    const message = fieldError(spec, form);
    if (message) errors[spec.key] = message;
  }
  if (!(RULES.interval_minutes.in as readonly number[]).includes(form.intervalMinutes)) {
    errors.intervalMinutes = `Choose ${RULES.interval_minutes.in.join(", ")} minutes.`;
  }
  if (form.policy !== EnvelopePolicy.EQUAL && form.policy !== EnvelopePolicy.PROPORTIONAL) {
    errors.policy = "Choose how the headroom is shared.";
  }
  const note = noteError(form);
  if (note) errors.note = note;
  return errors;
}

export const POLICY_WORDS: Record<number, string> = {
  [EnvelopePolicy.EQUAL]: "Equal: every site gets the same limit",
  [EnvelopePolicy.PROPORTIONAL]: "Proportional: in step with each site's connection limit",
};

export type Change = { label: string; from: string; to: string };

// What differs between two sets of values, in words and units: the preview
// before a save, and the history of versions.
export function diffForms(before: ConfigForm, after: ConfigForm, nominalV: number): Change[] {
  const changes: Change[] = [];
  if (before.policy !== after.policy) {
    changes.push({
      label: "How headroom is shared",
      from: POLICY_WORDS[before.policy] ?? "–",
      to: POLICY_WORDS[after.policy] ?? "–",
    });
  }
  const show = (v: number, unit: string) =>
    Number.isFinite(v) ? `${v}${unit === "×" ? "×" : ` ${unit}`}` : "–";
  if (before.intervalMinutes !== after.intervalMinutes) {
    changes.push({
      label: "Interval",
      from: `${before.intervalMinutes} min`,
      to: `${after.intervalMinutes} min`,
    });
  }
  for (const spec of fieldSpecs(nominalV)) {
    const [a, b] = [before[spec.key], after[spec.key]];
    if (a !== b && !(Number.isNaN(a) && Number.isNaN(b))) {
      changes.push({ label: spec.label, from: show(a, spec.unit), to: show(b, spec.unit) });
    }
  }
  return changes;
}
