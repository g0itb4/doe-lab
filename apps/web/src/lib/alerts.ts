import type { Alert } from "@doelab/gen/doelab/v1/alert_pb.js";
import { AlertKind, AlertSeverity } from "@doelab/gen/doelab/v1/common_pb.js";
import { kw } from "./format.ts";
import type { Level } from "./status.ts";
import { seconds } from "./time.ts";

export function severityLevel(severity: AlertSeverity): Level {
  if (severity === AlertSeverity.CRITICAL) return "critical";
  if (severity === AlertSeverity.WARNING) return "warn";
  return "info";
}

export function severityWord(severity: AlertSeverity): string {
  if (severity === AlertSeverity.CRITICAL) return "Critical";
  if (severity === AlertSeverity.WARNING) return "Warning";
  return "Info";
}

// What an alert is about, in an operator's words.
export function alertTitle(alert: Alert): string {
  if (alert.kind === AlertKind.CONSTRAINT_BREACH) {
    return `Exporting ${kw(alert.peakW)} against a limit of ${kw(alert.limitW)}`;
  }
  if (alert.kind === AlertKind.DEVICE_OFFLINE) return "Device stopped reporting";
  return "Alert";
}

// What needs attention first: open before resolved, the most severe first,
// and among equals the one that has waited longest.
export function sortAlerts(alerts: Alert[]): Alert[] {
  return [...alerts].sort(
    (a, b) =>
      Number(a.resolvedAt !== undefined) - Number(b.resolvedAt !== undefined) ||
      b.severity - a.severity ||
      seconds(a.openedAt) - seconds(b.openedAt),
  );
}

// The periods of the breaches, for a chart: from when each opened to when it
// was resolved, or to now.
export function breachSpans(
  alerts: Alert[],
  nowSeconds: number,
): { from: number; to: number; label: string }[] {
  return alerts
    .filter((a) => a.kind === AlertKind.CONSTRAINT_BREACH)
    .map((a) => ({
      from: seconds(a.openedAt),
      to: a.resolvedAt ? seconds(a.resolvedAt) : nowSeconds,
      label: "breach",
    }))
    .sort((a, b) => a.from - b.from);
}
