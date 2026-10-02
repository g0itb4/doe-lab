import { AlertKind, EnvelopeSource } from "@doelab/gen/doelab/v1/common_pb.js";
import type { Feeder } from "@doelab/gen/doelab/v1/feeder_pb.js";
import type { Site } from "@doelab/gen/doelab/v1/site_pb.js";
import type { Substation } from "@doelab/gen/doelab/v1/substation_pb.js";
import type { GetFleetStateResponse, SiteState } from "@doelab/gen/doelab/v1/telemetry_pb.js";
import { severityLevel } from "../alerts.ts";
import { exportSentence, kw } from "../format.ts";
import type { Level, Status } from "../status.ts";

// What the map draws: the fleet as marks, each with a place and a status.
// Everything here is plain data worked out from what the API returned, so
// the map component only has to put it on the screen.

// The equipment behind a site.
export type Kind = "solar" | "battery" | "ev" | "hybrid";

export const KIND_WORDS: Record<Kind, string> = {
  solar: "Solar",
  battery: "Battery",
  ev: "EV charger",
  hybrid: "Solar and battery",
};

export function kindOf(site: Site): Kind {
  if (site.pvKw > 0) return site.hasBattery ? "hybrid" : "solar";
  return site.hasBattery ? "battery" : "ev";
}

export type SiteMark = {
  id: string;
  nmi: string;
  latitude: number;
  longitude: number;
  feederCode: string;
  // Empty for a site whose feeder hangs from no substation.
  substationCode: string;
  // Which of its substation's feeders the site is on, counted from 0 in code
  // order: the map draws each with its own line.
  feederIndex: number;
  kind: Kind;
  status: Status;
  // Net export now, in watts: unset for a site that is not reporting.
  exportW: number | undefined;
  // The export limit in force: unset for a site with no envelope.
  limitW: number | undefined;
  // How much of the limit the site uses, from 0 to 1.
  fill: number;
};

export type SubstationMark = {
  id: string;
  code: string;
  name: string;
  // The place alone, for a label on the map: see placeName.
  place: string;
  dnsp: string;
  state: string;
  latitude: number;
  longitude: number;
  status: Status;
  feeders: number;
  sites: number;
  reporting: number;
  // Export of the reporting sites and the sum of the limits in force, watts.
  exportW: number;
  limitW: number;
};

export type FleetView = { substations: SubstationMark[]; sites: SiteMark[] };

// A site within this many watts of its limit is at it: an inverter that
// curtails holds its export just under the limit, not on it.
export const AT_LIMIT_W = 50;

const RANK: Record<Level, number> = { ok: 0, info: 1, warn: 2, critical: 3 };

function worse(a: Level, b: Level): Level {
  return RANK[b] > RANK[a] ? b : a;
}

// What one site is doing, in a word and a sentence.
export function siteStatus(site: Site, state: SiteState | undefined): Status {
  if (!state) {
    return {
      level: "info",
      label: "No state",
      detail: "The API has said nothing about this site.",
    };
  }
  const envelope = state.envelope;
  const alert = state.openAlert;
  if (envelope?.source === EnvelopeSource.BACKSTOP) {
    return {
      level: "critical",
      label: "Backstop",
      detail: exportSentence(envelope.exportLimitW, envelope.exportBinding, "", true),
    };
  }
  if (alert?.kind === AlertKind.CONSTRAINT_BREACH || state.overLimit) {
    return {
      // Over the limit is at least a warning, whatever the alert says, and
      // also before the grace period has opened one.
      level: worse("warn", alert ? severityLevel(alert.severity) : "warn"),
      label: "Over limit",
      detail: `Exporting ${kw(state.netExportW)} against a limit of ${kw(envelope?.exportLimitW)}.`,
    };
  }
  if (alert?.kind === AlertKind.DEVICE_OFFLINE) {
    return {
      level: worse("warn", severityLevel(alert.severity)),
      label: "Offline",
      detail: "A device of the site has stopped reporting.",
    };
  }
  if (!state.reporting) {
    return {
      level: "info",
      label: "Not reporting",
      detail: "No device of the site has been heard recently.",
    };
  }
  if (!envelope) {
    return {
      level: "info",
      label: "No envelope",
      detail: "The engine has given this site no limit. It falls back to its default.",
    };
  }
  const why = exportSentence(
    envelope.exportLimitW,
    envelope.exportBinding,
    envelope.exportBindingElement,
    false,
  );
  const net = state.netExportW ?? 0;
  if (
    site.exportCapW > 0 &&
    envelope.exportLimitW < site.exportCapW &&
    net >= envelope.exportLimitW - AT_LIMIT_W
  ) {
    return { level: "warn", label: "At its limit", detail: why };
  }
  const flow = net >= 0 ? `Exporting ${kw(net)}` : `Importing ${kw(-net)}`;
  return { level: "ok", label: "Within limit", detail: `${flow}. ${why}` };
}

// How much of its export limit a site uses, from 0 to 1.
function fillOf(state: SiteState | undefined): number {
  const limit = state?.envelope?.exportLimitW ?? 0;
  const net = state?.netExportW ?? 0;
  if (limit <= 0 || net <= 0) return 0;
  return Math.min(1, net / limit);
}

function plural(n: number, one: string, many: string): string {
  return n === 1 ? `1 ${one}` : `${n} ${many}`;
}

// A substation in a word: its gravest site, and how many of them.
function substationStatus(
  sites: SiteMark[],
  backstop: boolean,
  ran: boolean,
  exportW: number,
  limitW: number,
): Status {
  const reporting = sites.filter((s) => s.exportW !== undefined).length;
  const detail = `${reporting} of ${plural(sites.length, "site", "sites")} reporting, exporting ${kw(exportW)} of ${kw(limitW)} allowed.`;
  if (backstop) return { level: "critical", label: "Backstop active", detail };
  if (!ran) return { level: "info", label: "No envelopes yet", detail };
  const count = (label: string) => sites.filter((s) => s.status.label === label).length;
  const over = count("Over limit");
  const offline = count("Offline");
  const atLimit = count("At its limit");
  const level = sites.reduce<Level>((worst, s) => worse(worst, s.status.level), "ok");
  if (over > 0) return { level, label: `${over} over limit`, detail };
  if (offline > 0) return { level, label: `${offline} offline`, detail };
  if (atLimit > 0)
    return { level, label: `${atLimit} at ${atLimit === 1 ? "its" : "their"} limit`, detail };
  return { level, label: level === "ok" ? "Normal" : "Not all reporting", detail };
}

// The place in a substation's name: "Ausgrid Crows Nest Zone" is the owner,
// the place and the kind, and on a map the place is the label. A name that
// is not of that shape is its own label.
export function placeName(name: string): string {
  const words = name.split(" ");
  if (words.length < 3 || words[words.length - 1] !== "Zone") return name;
  return words.slice(1, -1).join(" ");
}

// The fleet as marks. `state` is unset until the API has answered: the marks
// then have a place and no news.
export function fleetView(
  substations: Substation[],
  feeders: Feeder[],
  sites: Site[],
  state: GetFleetStateResponse | undefined,
): FleetView {
  const substationById = new Map(substations.map((s) => [s.id, s]));
  const feederById = new Map(feeders.map((f) => [f.id, f]));
  const stateBySite = new Map(state?.sites.map((s) => [s.siteId, s]));
  const summaryByFeeder = new Map(state?.feeders.map((f) => [f.feederId, f]));

  // The feeders of each substation, in code order.
  const feedersOf = new Map<string, Feeder[]>();
  for (const feeder of [...feeders].sort((a, b) => a.code.localeCompare(b.code))) {
    if (feeder.substationId === undefined) continue;
    feedersOf.set(feeder.substationId, [...(feedersOf.get(feeder.substationId) ?? []), feeder]);
  }

  const siteMarks: SiteMark[] = [];
  for (const site of sites) {
    if (site.latitudeDeg === undefined || site.longitudeDeg === undefined) continue;
    const feeder = feederById.get(site.feederId);
    const substation = substationById.get(feeder?.substationId ?? "");
    const siteState = stateBySite.get(site.id);
    siteMarks.push({
      id: site.id,
      nmi: site.nmi,
      latitude: site.latitudeDeg,
      longitude: site.longitudeDeg,
      feederCode: feeder?.code ?? "",
      substationCode: substation?.code ?? "",
      feederIndex: Math.max(0, (feedersOf.get(substation?.id ?? "") ?? []).indexOf(feeder!)),
      kind: kindOf(site),
      status: state ? siteStatus(site, siteState) : waiting,
      exportW: siteState?.reporting ? (siteState.netExportW ?? 0) : undefined,
      limitW: siteState?.envelope?.exportLimitW,
      fill: fillOf(siteState),
    });
  }

  const substationMarks = substations.map((substation): SubstationMark => {
    const own = feedersOf.get(substation.id) ?? [];
    const marks = siteMarks.filter((s) => s.substationCode === substation.code);
    const summaries = own.flatMap((f) => summaryByFeeder.get(f.id) ?? []);
    const exportW = marks.reduce((sum, s) => sum + Math.max(s.exportW ?? 0, 0), 0);
    const limitW = marks.reduce((sum, s) => sum + (s.limitW ?? 0), 0);
    return {
      id: substation.id,
      code: substation.code,
      name: substation.name,
      place: placeName(substation.name),
      dnsp: substation.dnsp,
      state: substation.state,
      latitude: substation.latitudeDeg,
      longitude: substation.longitudeDeg,
      status: state
        ? substationStatus(
            marks,
            summaries.some((s) => s.backstopEventId !== undefined),
            summaries.some((s) => s.latestRunId !== undefined),
            exportW,
            limitW,
          )
        : waiting,
      feeders: own.length,
      sites: marks.length,
      reporting: marks.filter((s) => s.exportW !== undefined).length,
      exportW,
      limitW,
    };
  });
  return { substations: substationMarks, sites: siteMarks };
}

const waiting: Status = {
  level: "info",
  label: "Loading",
  detail: "Waiting for the fleet's state.",
};

// A box on the map: south-west corner, then north-east.
export type Bounds = [[number, number], [number, number]];

// The smallest box around some places, or undefined for none.
export function boundsOf(places: { latitude: number; longitude: number }[]): Bounds | undefined {
  if (places.length === 0) return undefined;
  const lats = places.map((p) => p.latitude);
  const lngs = places.map((p) => p.longitude);
  return [
    [Math.min(...lats), Math.min(...lngs)],
    [Math.max(...lats), Math.max(...lngs)],
  ];
}

// The states that have a substation, in the order the fleet is largest: the
// regions the map can show.
export function regionsOf(view: FleetView): string[] {
  const sizes = new Map<string, number>();
  for (const s of view.substations) sizes.set(s.state, (sizes.get(s.state) ?? 0) + 1);
  return [...sizes.keys()].sort((a, b) => sizes.get(b)! - sizes.get(a)! || a.localeCompare(b));
}

// What the map frames: one substation and its sites, one state, or the lot.
export function frame(view: FleetView, region: string, substation: string): Bounds | undefined {
  if (substation) {
    return boundsOf([
      ...view.substations.filter((s) => s.code === substation),
      ...view.sites.filter((s) => s.substationCode === substation),
    ]);
  }
  const inRegion = view.substations.filter((s) => region === "all" || s.state === region);
  const codes = new Set(inRegion.map((s) => s.code));
  return boundsOf([...inRegion, ...view.sites.filter((s) => codes.has(s.substationCode))]);
}
