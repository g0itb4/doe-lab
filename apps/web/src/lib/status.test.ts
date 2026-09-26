import { create } from "@bufbuild/protobuf";
import { BindingConstraint, EnvelopeSource } from "@doelab/gen/doelab/v1/common_pb.js";
import { EnvelopeSchema } from "@doelab/gen/doelab/v1/envelope_pb.js";
import { FleetSummarySchema } from "@doelab/gen/doelab/v1/telemetry_pb.js";
import { describe, expect, it } from "vitest";
import { feederStatus } from "./status.ts";

const summary = (fields: Parameters<typeof create<typeof FleetSummarySchema>>[1] = {}) =>
  create(FleetSummarySchema, { latestRunId: "run-1", ...fields });
const envelope = (binding: BindingConstraint, source = EnvelopeSource.ENGINE) =>
  create(EnvelopeSchema, { source, exportBinding: binding, exportBindingElement: "Ld14" });

describe("the feeder's status", () => {
  it("is normal when nothing binds", () => {
    expect(feederStatus(summary(), envelope(BindingConstraint.SITE_CAP))).toMatchObject({
      level: "ok",
      label: "Normal",
    });
    expect(feederStatus(summary(), undefined).level).toBe("ok");
  });

  it("names the constraint that binds", () => {
    expect(feederStatus(summary(), envelope(BindingConstraint.VOLTAGE_HIGH))).toMatchObject({
      level: "warn",
      label: "Constrained: high voltage at Ld14",
    });
    for (const binding of [
      BindingConstraint.VOLTAGE_LOW,
      BindingConstraint.TRANSFORMER,
      BindingConstraint.LINE,
    ]) {
      expect(feederStatus(summary(), envelope(binding)).level).toBe("warn");
    }
  });

  it("puts a site over its limit before the constraint", () => {
    const one = feederStatus(
      summary({ sitesOverLimit: 1 }),
      envelope(BindingConstraint.VOLTAGE_HIGH),
    );
    expect(one).toMatchObject({
      level: "warn",
      label: "Limit exceeded",
      detail: "1 site is exporting above the limit.",
    });
    expect(feederStatus(summary({ sitesOverLimit: 3 }), undefined).detail).toBe(
      "3 sites are exporting above the limit.",
    );
  });

  it("puts a backstop before everything", () => {
    const status = feederStatus(
      summary({ backstopEventId: "b-1", sitesOverLimit: 2 }),
      envelope(BindingConstraint.VOLTAGE_HIGH, EnvelopeSource.BACKSTOP),
    );
    expect(status).toMatchObject({ level: "critical", label: "Backstop active" });
  });

  it("says so when the engine has never run", () => {
    expect(feederStatus(create(FleetSummarySchema, {}), undefined)).toMatchObject({
      level: "info",
      label: "No envelopes yet",
    });
  });

  it("does not call a backstop's envelope a network constraint", () => {
    expect(
      feederStatus(summary(), envelope(BindingConstraint.VOLTAGE_HIGH, EnvelopeSource.BACKSTOP))
        .level,
    ).toBe("ok");
  });
});
