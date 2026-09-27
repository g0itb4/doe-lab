import { create } from "@bufbuild/protobuf";
import { AlertSchema } from "@doelab/gen/doelab/v1/alert_pb.js";
import { AlertKind, AlertSeverity } from "@doelab/gen/doelab/v1/common_pb.js";
import { describe, expect, it } from "vitest";
import { alertTitle, breachSpans, severityLevel, severityWord, sortAlerts } from "./alerts.ts";
import { timestamp } from "./time.ts";

const alert = (
  id: string,
  severity: AlertSeverity,
  opened: number,
  resolved?: number,
  kind = AlertKind.CONSTRAINT_BREACH,
) =>
  create(AlertSchema, {
    id,
    kind,
    severity,
    openedAt: timestamp(opened),
    resolvedAt: resolved === undefined ? undefined : timestamp(resolved),
    limitW: 1500,
    peakW: 2740,
  });

describe("alerts", () => {
  it("come in the order they need attention", () => {
    const sorted = sortAlerts([
      alert("resolved-critical", AlertSeverity.CRITICAL, 100, 200),
      alert("open-info", AlertSeverity.INFO, 50),
      alert("open-warning-new", AlertSeverity.WARNING, 300),
      alert("open-critical", AlertSeverity.CRITICAL, 400),
      alert("open-warning-old", AlertSeverity.WARNING, 100),
    ]);
    expect(sorted.map((a) => a.id)).toEqual([
      "open-critical",
      "open-warning-old",
      "open-warning-new",
      "open-info",
      "resolved-critical",
    ]);
  });

  it("say what they are about", () => {
    expect(alertTitle(alert("a", AlertSeverity.WARNING, 0))).toBe(
      "Exporting 2.7 kW against a limit of 1.5 kW",
    );
    expect(alertTitle(alert("b", AlertSeverity.INFO, 0, undefined, AlertKind.DEVICE_OFFLINE))).toBe(
      "Device stopped reporting",
    );
    expect(alertTitle(alert("c", AlertSeverity.INFO, 0, undefined, AlertKind.UNSPECIFIED))).toBe(
      "Alert",
    );
  });

  it("have a level and a word for each severity", () => {
    expect(
      [AlertSeverity.CRITICAL, AlertSeverity.WARNING, AlertSeverity.INFO].map(severityLevel),
    ).toEqual(["critical", "warn", "info"]);
    expect(
      [AlertSeverity.CRITICAL, AlertSeverity.WARNING, AlertSeverity.INFO].map(severityWord),
    ).toEqual(["Critical", "Warning", "Info"]);
  });

  it("give a chart the periods of the breaches, an open one up to now", () => {
    const spans = breachSpans(
      [
        alert("open", AlertSeverity.WARNING, 500),
        alert("offline", AlertSeverity.INFO, 50, 60, AlertKind.DEVICE_OFFLINE),
        alert("resolved", AlertSeverity.WARNING, 100, 200),
      ],
      900,
    );
    expect(spans).toEqual([
      { from: 100, to: 200, label: "breach" },
      { from: 500, to: 900, label: "breach" },
    ]);
  });
});
