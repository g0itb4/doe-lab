import { BindingConstraint } from "@doelab/gen/doelab/v1/common_pb.js";

// The minus sign, not a hyphen: it lines up with the digits.
const MINUS = "−";
// A no-break space keeps a number and its unit on one line.
const NBSP = " ";

function fixed(value: number, digits: number): string {
  const text = Math.abs(value).toFixed(digits);
  // No "-0.0".
  return value < 0 && Number(text) !== 0 ? MINUS + text : text;
}

// Power is always shown in kW, to one decimal place unless a caller has a
// reason: one precision across the app, so two numbers can be compared.
export function kw(watts: number | undefined, digits = 1): string {
  if (watts === undefined || !Number.isFinite(watts)) return "–";
  return `${fixed(watts / 1000, digits)}${NBSP}kW`;
}

export function kwh(value: number | undefined, digits = 1): string {
  if (value === undefined || !Number.isFinite(value)) return "–";
  return `${fixed(value, digits)}${NBSP}kWh`;
}

// A per-unit voltage as volts on the feeder's nominal voltage.
export function volts(pu: number | undefined, nominalV: number): string {
  if (pu === undefined || !Number.isFinite(pu) || pu === 0) return "–";
  return `${fixed(pu * nominalV, 1)}${NBSP}V`;
}

export function percent(value: number | undefined, digits = 0): string {
  if (value === undefined || !Number.isFinite(value)) return "–";
  return `${fixed(value, digits)}${NBSP}%`;
}

export function count(n: number): string {
  return n.toLocaleString("en-AU");
}

// A formatter costs far more to make than to use, and a list of alerts would
// make one per row per second: one is made for each zone, and kept.
function perZone(options: Intl.DateTimeFormatOptions): (zone: string) => Intl.DateTimeFormat {
  const made = new Map<string, Intl.DateTimeFormat>();
  return (zone) => {
    let format = made.get(zone);
    if (!format) {
      format = new Intl.DateTimeFormat("en-AU", { timeZone: zone, ...options });
      made.set(zone, format);
    }
    return format;
  };
}

const clockFormat = perZone({ hour: "2-digit", minute: "2-digit", hourCycle: "h23" });
const dayFormat = perZone({
  weekday: "short",
  day: "numeric",
  month: "short",
  hour: "2-digit",
  minute: "2-digit",
  hourCycle: "h23",
});
const zoneFormat = perZone({ timeZoneName: "short" });

// Times are shown in the feeder's zone, whatever the reader's own.
export function clockTime(at: Date | undefined, zone: string): string {
  if (!at) return "–";
  return clockFormat(zone).format(at);
}

export function dayAndTime(at: Date | undefined, zone: string): string {
  if (!at) return "–";
  // From the parts, not from format(): browsers disagree about the commas.
  const parts = dayFormat(zone).formatToParts(at);
  const part = (type: Intl.DateTimeFormatPartTypes) =>
    parts.find((p) => p.type === type)?.value ?? "";
  return `${part("weekday")} ${part("day")} ${part("month")}, ${part("hour")}:${part("minute")}`;
}

// The short name of the zone at an instant: "AEST" or "AEDT" for Sydney.
export function zoneName(at: Date, zone: string): string {
  const parts = zoneFormat(zone).formatToParts(at);
  return parts.find((p) => p.type === "timeZoneName")?.value ?? zone;
}

// How long ago, in the largest unit that fits. Both instants are feeder time.
export function ago(at: Date | undefined, now: Date): string {
  if (!at) return "–";
  const s = Math.round((now.getTime() - at.getTime()) / 1000);
  if (s < 0) return "in the future";
  if (s < 60) return "just now";
  if (s < 3600) return `${Math.floor(s / 60)} min ago`;
  if (s < 86400) return `${Math.floor(s / 3600)} h ago`;
  return `${Math.floor(s / 86400)} d ago`;
}

export function duration(seconds: number): string {
  if (seconds < 60) return `${Math.round(seconds)} s`;
  if (seconds < 3600) return `${Math.round(seconds / 60)} min`;
  return `${(seconds / 3600).toFixed(1)} h`;
}

// What limits an envelope, in an operator's words. The element is the name
// of the customer, line or transformer in the network model.
export function bindingWords(binding: BindingConstraint, element: string): string {
  const at = element ? ` at ${element}` : "";
  switch (binding) {
    case BindingConstraint.VOLTAGE_HIGH:
      return `high voltage${at}`;
    case BindingConstraint.VOLTAGE_LOW:
      return `low voltage${at}`;
    case BindingConstraint.TRANSFORMER:
      return "the transformer's rating";
    case BindingConstraint.LINE:
      return `the rating of line ${element || "(unnamed)"}`;
    case BindingConstraint.SITE_CAP:
      return "the site's own connection limit";
    default:
      return "nothing on the network";
  }
}

// One sentence that says why a site's export limit is what it is.
export function exportSentence(
  limitW: number,
  binding: BindingConstraint,
  element: string,
  backstop: boolean,
): string {
  if (backstop) return `Export held at ${kw(limitW)} by an operator's backstop.`;
  if (
    binding === BindingConstraint.SITE_CAP ||
    binding === BindingConstraint.NONE ||
    binding === BindingConstraint.UNSPECIFIED
  ) {
    return `Export up to ${kw(limitW)}: the network does not limit this site now, so its own connection limit applies.`;
  }
  return `Export limited to ${kw(limitW)} by ${bindingWords(binding, element)}.`;
}
