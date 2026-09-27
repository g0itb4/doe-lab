import { create } from "@bufbuild/protobuf";
import { EnvelopePolicy } from "@doelab/gen/doelab/v1/common_pb.js";
import { EnvelopeConfigSchema } from "@doelab/gen/doelab/v1/envelope_config_pb.js";
import { describe, expect, it } from "vitest";
// The source of the rules: the browser's generated code leaves them out.
import proto from "../../../../proto/doelab/v1/envelope_config.proto?raw";
import {
  diffForms,
  fieldError,
  fieldSpecs,
  formErrors,
  fromForm,
  noteError,
  RULES,
  toForm,
  type ConfigForm,
} from "./config-rules.ts";

const config = create(EnvelopeConfigSchema, {
  feederId: "f-1",
  version: 3,
  policy: EnvelopePolicy.EQUAL,
  vMinPu: 0.94,
  vMaxPu: 1.1,
  transformerLimitPct: 100,
  lineLimitPct: 100,
  pvScale: 3,
  staticLimitW: 5000,
  intervalMinutes: 30,
  horizonIntervals: 48,
  breachGraceSeconds: 60,
  offlineAfterSeconds: 300,
  note: "the note of version 3",
});
const form = (changes: Partial<ConfigForm> = {}): ConfigForm => ({
  ...toForm(config, 230),
  ...changes,
});
const spec = (key: string) => fieldSpecs(230).find((s) => s.key === key)!;

// The rule block of one field of the EnvelopeConfig message, as written in
// the .proto file: from its declaration to the end of its options.
function protoRule(field: string): Record<string, number | number[]> {
  const message = proto.slice(
    proto.indexOf("message EnvelopeConfig {"),
    proto.indexOf("message GetEnvelopeConfigRequest"),
  );
  const start = message.search(new RegExp(`\\b${field} = \\d+`));
  expect(start, `field ${field} in the proto file`).toBeGreaterThan(-1);
  const end = message.indexOf("];", start);
  const block = message.slice(start, end);
  const rule: Record<string, number | number[]> = {};
  for (const m of block.matchAll(/\b(gte|gt|lte|lt|max_len)\b:?\s*=?\s*(-?[\d.]+)/g))
    rule[m[1] === "max_len" ? "maxLen" : m[1]!] = Number(m[2]);
  const list = block.match(/in: \[([^\]]+)\]/);
  if (list) rule.in = list[1]!.split(",").map((v) => Number(v.trim()));
  return rule;
}

describe("the form's rules", () => {
  it.each(Object.keys(RULES))("are the API's for %s", (field) => {
    expect(RULES[field as keyof typeof RULES]).toEqual(protoRule(field));
  });

  it("cover every field that the operator sets", () => {
    const numbers = fieldSpecs(230).map((s) => s.key);
    expect(new Set([...numbers, "policy", "intervalMinutes", "note"])).toEqual(
      new Set(Object.keys(form())),
    );
  });
});

describe("a config in the form's units", () => {
  it("shows volts and kilowatts, without float noise, and starts with an empty note", () => {
    expect(toForm(config, 230)).toEqual({
      policy: EnvelopePolicy.EQUAL,
      vMinV: 216.2,
      vMaxV: 253,
      transformerLimitPct: 100,
      lineLimitPct: 100,
      pvScale: 3,
      staticLimitKw: 5,
      intervalMinutes: 30,
      horizonIntervals: 48,
      breachGraceSeconds: 60,
      offlineAfterSeconds: 300,
      note: "",
    });
  });

  it("goes back to the API's units", () => {
    const sent = fromForm(
      form({ vMaxV: 250, staticLimitKw: 1.5, note: "  tighter band " }),
      "f-1",
      230,
    );
    expect(sent).toMatchObject({
      feederId: "f-1",
      staticLimitW: 1500,
      note: "tighter band",
      intervalMinutes: 30,
    });
    expect(sent.vMaxPu).toBeCloseTo(250 / 230, 12);
    expect(sent.vMinPu).toBeCloseTo(0.94, 12);
  });

  it("gives the ranges in the form's units", () => {
    expect(spec("vMinV").range).toEqual({ gte: 184, lte: 276 });
    expect(spec("staticLimitKw").range).toEqual({ gte: 0 });
  });
});

describe("validation", () => {
  it("passes the active config", () => {
    expect(formErrors(form(), 230)).toEqual({});
  });

  it("says what to enter instead, with the unit", () => {
    expect(fieldError(spec("vMaxV"), form({ vMaxV: 300 }))).toBe("Enter from 184 to 276 V.");
    expect(fieldError(spec("vMinV"), form({ vMinV: 100 }))).toBe("Enter from 184 to 276 V.");
    expect(fieldError(spec("transformerLimitPct"), form({ transformerLimitPct: 0 }))).toBe(
      "Enter more than 0 and at most 200 %.",
    );
    expect(fieldError(spec("transformerLimitPct"), form({ transformerLimitPct: 201 }))).toBe(
      "Enter more than 0 and at most 200 %.",
    );
    expect(fieldError(spec("pvScale"), form({ pvScale: 21 }))).toBe("Enter from 0 to 20.");
    expect(fieldError(spec("staticLimitKw"), form({ staticLimitKw: -1 }))).toBe(
      "Enter 0 kW or more.",
    );
    expect(fieldError(spec("horizonIntervals"), form({ horizonIntervals: 0 }))).toBe(
      "Enter from 1 to 288.",
    );
    expect(fieldError(spec("offlineAfterSeconds"), form({ offlineAfterSeconds: 5 }))).toBe(
      "Enter from 10 to 86400 s.",
    );
  });

  it("accepts the edges of a range", () => {
    expect(fieldError(spec("pvScale"), form({ pvScale: 0 }))).toBe("");
    expect(fieldError(spec("pvScale"), form({ pvScale: 20 }))).toBe("");
    expect(fieldError(spec("transformerLimitPct"), form({ transformerLimitPct: 200 }))).toBe("");
    expect(fieldError(spec("breachGraceSeconds"), form({ breachGraceSeconds: 0 }))).toBe("");
  });

  it("wants a number, and a whole one where the API takes an integer", () => {
    expect(fieldError(spec("pvScale"), form({ pvScale: Number.NaN }))).toBe(
      "Enter a number: from 0 to 20.",
    );
    expect(fieldError(spec("horizonIntervals"), form({ horizonIntervals: 47.5 }))).toBe(
      "Enter a whole number.",
    );
  });

  it("keeps the voltage band the right way up", () => {
    expect(fieldError(spec("vMinV"), form({ vMinV: 255 }))).toBe(
      "Enter a voltage below the highest allowed (253 V).",
    );
    expect(fieldError(spec("vMaxV"), form({ vMaxV: 216.2 }))).toBe(
      "Enter a voltage above the lowest allowed (216.2 V).",
    );
    const both = formErrors(form({ vMinV: 250, vMaxV: 240 }), 230);
    expect(Object.keys(both)).toEqual(["vMinV", "vMaxV"]);
  });

  it("checks the interval, the policy and the note", () => {
    expect(formErrors(form({ intervalMinutes: 7 }), 230)).toEqual({
      intervalMinutes: "Choose 5, 15, 30, 60 minutes.",
    });
    expect(formErrors(form({ policy: EnvelopePolicy.UNSPECIFIED }), 230)).toEqual({
      policy: "Choose how the headroom is shared.",
    });
    expect(noteError(form({ note: "x".repeat(2000) }))).toBe("");
    expect(formErrors(form({ note: "x".repeat(2001) }), 230)).toEqual({
      note: "Shorten the note to 2000 characters or fewer.",
    });
  });
});

describe("the difference between two versions", () => {
  it("is nothing when nothing changed", () => {
    expect(diffForms(form(), form({ note: "only a note" }), 230)).toEqual([]);
  });

  it("names each change with its unit", () => {
    const changes = diffForms(
      form(),
      form({
        policy: EnvelopePolicy.PROPORTIONAL,
        vMaxV: 250,
        pvScale: 2.5,
        intervalMinutes: 15,
        staticLimitKw: 3,
      }),
      230,
    );
    expect(changes).toEqual([
      {
        label: "How headroom is shared",
        from: "Equal: every site gets the same limit",
        to: "Proportional: in step with each site's connection limit",
      },
      { label: "Interval", from: "30 min", to: "15 min" },
      { label: "Highest voltage allowed", from: "253 V", to: "250 V" },
      { label: "Solar scale", from: "3×", to: "2.5×" },
      { label: "Fixed limit to compare with", from: "5 kW", to: "3 kW" },
    ]);
  });

  it("shows a field that was emptied as a dash, and does not see two empties as a change", () => {
    expect(diffForms(form(), form({ pvScale: Number.NaN }), 230)).toEqual([
      { label: "Solar scale", from: "3×", to: "–" },
    ]);
    expect(diffForms(form({ pvScale: Number.NaN }), form({ pvScale: Number.NaN }), 230)).toEqual(
      [],
    );
    expect(diffForms(form({ policy: 9 as EnvelopePolicy }), form(), 230)[0]).toMatchObject({
      from: "–",
    });
  });
});
