import { describe, expect, it } from "vitest";
import { date, seconds, timestamp } from "./time.ts";

describe("timestamps", () => {
  it("round-trips unix seconds", () => {
    const ts = timestamp(1_790_000_000.5);
    expect(seconds(ts)).toBe(1_790_000_000.5);
    expect(date(ts)?.toISOString()).toBe(new Date(1_790_000_000_500).toISOString());
  });

  it("reads an unset timestamp as nothing", () => {
    expect(seconds(undefined)).toBe(0);
    expect(date(undefined)).toBeUndefined();
  });
});
