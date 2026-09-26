import { BindingConstraint } from "@doelab/gen/doelab/v1/common_pb.js";
import { describe, expect, it } from "vitest";
import {
  ago,
  bindingWords,
  clockTime,
  count,
  dayAndTime,
  duration,
  exportSentence,
  kw,
  kwh,
  percent,
  volts,
  zoneName,
} from "./format.ts";

describe("numbers", () => {
  it("shows power in kW to one place, with a real minus sign", () => {
    expect(kw(2140)).toBe("2.1 kW");
    expect(kw(-3050)).toBe("−3.0 kW");
    expect(kw(1234, 2)).toBe("1.23 kW");
    expect(kw(0)).toBe("0.0 kW");
  });

  it("never shows a negative zero", () => {
    expect(kw(-4)).toBe("0.0 kW");
  });

  it("shows a dash for a number that is not there", () => {
    expect(kw(undefined)).toBe("–");
    expect(kw(Number.NaN)).toBe("–");
    expect(kwh(undefined)).toBe("–");
    expect(percent(Infinity)).toBe("–");
    expect(volts(undefined, 230)).toBe("–");
    expect(volts(0, 230)).toBe("–");
  });

  it("puts a unit on everything", () => {
    expect(kwh(12.34)).toBe("12.3 kWh");
    expect(percent(84.6)).toBe("85 %");
    expect(percent(84.6, 1)).toBe("84.6 %");
    expect(volts(1.1, 230)).toBe("253.0 V");
    expect(count(12345)).toBe("12,345");
  });
});

describe("times", () => {
  // 02:30 UTC is 12:30 in Sydney in winter, 13:30 in summer. The tests run
  // in London, so a time formatted in the reader's zone would be wrong here.
  const winter = new Date("2026-07-01T02:30:00Z");
  const summer = new Date("2026-11-03T02:30:00Z");

  it("shows the feeder's zone, not the reader's", () => {
    expect(clockTime(winter, "Australia/Sydney")).toBe("12:30");
    expect(clockTime(summer, "Australia/Sydney")).toBe("13:30");
    expect(dayAndTime(summer, "Australia/Sydney")).toBe("Tue 3 Nov, 13:30");
  });

  it("names the zone as it is on the day", () => {
    expect(zoneName(winter, "Australia/Sydney")).toBe("AEST");
    expect(zoneName(summer, "Australia/Sydney")).toBe("AEDT");
  });

  it("shows a dash for no time", () => {
    expect(clockTime(undefined, "Australia/Sydney")).toBe("–");
    expect(dayAndTime(undefined, "Australia/Sydney")).toBe("–");
    expect(ago(undefined, summer)).toBe("–");
  });

  it("says how long ago in the largest unit that fits", () => {
    const now = new Date("2026-11-03T12:00:00Z");
    const before = (s: number) => new Date(now.getTime() - s * 1000);
    expect(ago(before(20), now)).toBe("just now");
    expect(ago(before(150), now)).toBe("2 min ago");
    expect(ago(before(2 * 3600 + 5), now)).toBe("2 h ago");
    expect(ago(before(3 * 86400), now)).toBe("3 d ago");
    expect(ago(before(-60), now)).toBe("in the future");
  });

  it("shows a duration in a unit that suits it", () => {
    expect(duration(45)).toBe("45 s");
    expect(duration(300)).toBe("5 min");
    expect(duration(5400)).toBe("1.5 h");
  });
});

describe("the binding constraint in words", () => {
  it("names what limits and where", () => {
    expect(bindingWords(BindingConstraint.VOLTAGE_HIGH, "Ld14")).toBe("high voltage at Ld14");
    expect(bindingWords(BindingConstraint.VOLTAGE_LOW, "")).toBe("low voltage");
    expect(bindingWords(BindingConstraint.TRANSFORMER, "T1")).toBe("the transformer's rating");
    expect(bindingWords(BindingConstraint.LINE, "L7")).toBe("the rating of line L7");
    expect(bindingWords(BindingConstraint.LINE, "")).toBe("the rating of line (unnamed)");
    expect(bindingWords(BindingConstraint.SITE_CAP, "")).toBe("the site's own connection limit");
    expect(bindingWords(BindingConstraint.NONE, "")).toBe("nothing on the network");
  });

  it("makes one sentence of an export limit", () => {
    expect(exportSentence(2100, BindingConstraint.VOLTAGE_HIGH, "Ld14", false)).toBe(
      "Export limited to 2.1 kW by high voltage at Ld14.",
    );
    expect(exportSentence(5000, BindingConstraint.SITE_CAP, "", false)).toContain(
      "its own connection limit applies",
    );
    expect(exportSentence(5000, BindingConstraint.NONE, "", false)).toContain(
      "does not limit this site",
    );
    expect(exportSentence(5000, BindingConstraint.UNSPECIFIED, "", false)).toContain(
      "does not limit this site",
    );
    expect(exportSentence(0, BindingConstraint.VOLTAGE_HIGH, "Ld14", true)).toBe(
      "Export held at 0.0 kW by an operator's backstop.",
    );
  });
});
