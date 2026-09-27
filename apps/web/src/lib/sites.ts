import type { Site } from "@doelab/gen/doelab/v1/site_pb.js";

// A site takes part in envelopes when it has a limit to be given.
export function enrolled(site: Site): boolean {
  return site.exportCapW > 0 || site.importCapW > 0;
}

// The sites that match what was typed, by NMI or name, in NMI order.
export function filterSites(sites: Site[], query: string, enrolledOnly: boolean): Site[] {
  const q = query.trim().toLowerCase();
  return sites
    .filter((s) => !enrolledOnly || enrolled(s))
    .filter((s) => q === "" || s.nmi.toLowerCase().includes(q) || s.name.toLowerCase().includes(q))
    .sort((a, b) => a.nmi.localeCompare(b.nmi));
}

const PHASES = ["", "A", "B", "C"];

export function phaseName(phase: number): string {
  return PHASES[phase] ?? String(phase);
}

// What is behind the meter, in a few words.
export function equipment(site: Site): string {
  const parts: string[] = [];
  if (site.pvKw > 0) parts.push(`${site.pvKw.toFixed(1)} kW solar`);
  if (site.hasBattery)
    parts.push(site.batteryKwh ? `${site.batteryKwh.toFixed(1)} kWh battery` : "battery");
  if (site.hasEv) parts.push("EV charger");
  return parts.length > 0 ? parts.join(", ") : "no solar";
}
