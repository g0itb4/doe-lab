import { create } from "@bufbuild/protobuf";
import { AlertSchema } from "@doelab/gen/doelab/v1/alert_pb.js";
import {
  AlertKind,
  AlertSeverity,
  BindingConstraint,
  EnvelopeSource,
} from "@doelab/gen/doelab/v1/common_pb.js";
import { EnvelopeSchema } from "@doelab/gen/doelab/v1/envelope_pb.js";
import { FeederSchema } from "@doelab/gen/doelab/v1/feeder_pb.js";
import { SiteSchema } from "@doelab/gen/doelab/v1/site_pb.js";
import { SubstationSchema } from "@doelab/gen/doelab/v1/substation_pb.js";
import {
  FleetSummarySchema,
  GetFleetStateResponseSchema,
  SiteStateSchema,
} from "@doelab/gen/doelab/v1/telemetry_pb.js";
import { describe, expect, it } from "vitest";
import { boundsOf, fleetView, frame, kindOf, placeName, regionsOf, siteStatus } from "./fleet.ts";

type Init<S> = Parameters<typeof create<S extends Parameters<typeof create>[0] ? S : never>>[1];

const site = (fields: Init<typeof SiteSchema> = {}) =>
  create(SiteSchema, {
    id: "s-1",
    nmi: "NMI00000017",
    feederId: "f-lv10",
    pvKw: 5,
    exportCapW: 5000,
    importCapW: 14000,
    latitudeDeg: -33.85,
    longitudeDeg: 151.07,
    ...fields,
  });
const envelope = (exportLimitW: number, fields: Init<typeof EnvelopeSchema> = {}) =>
  create(EnvelopeSchema, {
    source: EnvelopeSource.ENGINE,
    exportLimitW,
    exportBinding: BindingConstraint.VOLTAGE_HIGH,
    exportBindingElement: "NMI00000098",
    ...fields,
  });
const stateOf = (fields: Init<typeof SiteStateSchema> = {}) =>
  create(SiteStateSchema, { siteId: "s-1", reporting: true, netExportW: 1000, ...fields });
const alert = (kind: AlertKind, severity: AlertSeverity) => create(AlertSchema, { kind, severity });

describe("the kind of a site", () => {
  it("is read from what the site has", () => {
    expect(kindOf(site())).toBe("solar");
    expect(kindOf(site({ hasBattery: true, batteryKwh: 10 }))).toBe("hybrid");
    expect(kindOf(site({ pvKw: 0, hasBattery: true, batteryKwh: 10 }))).toBe("battery");
    expect(kindOf(site({ pvKw: 0, hasEv: true }))).toBe("ev");
  });
});

describe("the status of a site", () => {
  it("is within its limit when it exports less than it may, and says why the limit is what it is", () => {
    expect(siteStatus(site(), stateOf({ envelope: envelope(3000) }))).toEqual({
      level: "ok",
      label: "Within limit",
      detail:
        "Exporting 1.0\u00a0kW. Export limited to 3.0\u00a0kW by high voltage at NMI00000098.",
    });
    const importing = siteStatus(site(), stateOf({ envelope: envelope(3000), netExportW: -2500 }));
    expect(importing.detail).toMatch(/^Importing 2\.5\u00a0kW\. /);
  });

  it("is at its limit when the network holds it there", () => {
    const held = siteStatus(site(), stateOf({ envelope: envelope(3000), netExportW: 2960 }));
    expect(held).toMatchObject({ level: "warn", label: "At its limit" });
    // At its own connection limit is as much as it could ever do: not held.
    const free = siteStatus(site(), stateOf({ envelope: envelope(5000), netExportW: 4990 }));
    expect(free.label).toBe("Within limit");
    // A site that may not export at all is not "at" a limit of nothing.
    const charger = siteStatus(
      site({ pvKw: 0, exportCapW: 0, hasEv: true }),
      stateOf({ envelope: envelope(0), netExportW: 0 }),
    );
    expect(charger.label).toBe("Within limit");
  });

  it("is over its limit as soon as it is, and as grave as its alert", () => {
    const early = siteStatus(
      site(),
      stateOf({ envelope: envelope(1000), overLimit: true, netExportW: 2500 }),
    );
    expect(early).toEqual({
      level: "warn",
      label: "Over limit",
      detail: "Exporting 2.5\u00a0kW against a limit of 1.0\u00a0kW.",
    });
    const critical = stateOf({
      envelope: envelope(1000),
      openAlert: alert(AlertKind.CONSTRAINT_BREACH, AlertSeverity.CRITICAL),
    });
    expect(siteStatus(site(), critical)).toMatchObject({ level: "critical", label: "Over limit" });
    // An alert of little weight is still a warning on the map.
    const info = stateOf({ openAlert: alert(AlertKind.CONSTRAINT_BREACH, AlertSeverity.INFO) });
    expect(siteStatus(site(), info).level).toBe("warn");
  });

  it("is offline when an alert says so, and not reporting before one does", () => {
    const offline = stateOf({
      reporting: false,
      netExportW: undefined,
      openAlert: alert(AlertKind.DEVICE_OFFLINE, AlertSeverity.WARNING),
    });
    expect(siteStatus(site(), offline)).toMatchObject({ level: "warn", label: "Offline" });
    const quiet = stateOf({ reporting: false, netExportW: undefined });
    expect(siteStatus(site(), quiet)).toMatchObject({ level: "info", label: "Not reporting" });
  });

  it("says when there is no envelope, a backstop, or no word at all", () => {
    expect(siteStatus(site(), stateOf())).toMatchObject({ level: "info", label: "No envelope" });
    const backstop = stateOf({ envelope: envelope(0, { source: EnvelopeSource.BACKSTOP }) });
    expect(siteStatus(site(), backstop)).toEqual({
      level: "critical",
      label: "Backstop",
      detail: "Export held at 0.0\u00a0kW by an operator's backstop.",
    });
    expect(siteStatus(site(), undefined)).toMatchObject({ level: "info", label: "No state" });
  });
});

// Two substations: Lidcombe with two feeders and three sites, Footscray with
// one feeder and one site. A fourth feeder hangs from nothing.
const substations = [
  create(SubstationSchema, {
    id: "sub-1",
    code: "SUB-001",
    name: "Ausgrid Lidcombe Zone",
    dnsp: "Ausgrid",
    state: "NSW",
    latitudeDeg: -33.8524,
    longitudeDeg: 151.0621,
  }),
  create(SubstationSchema, {
    id: "sub-7",
    code: "SUB-007",
    name: "Jemena Footscray Zone",
    dnsp: "Jemena",
    state: "VIC",
    latitudeDeg: -37.8048,
    longitudeDeg: 144.9011,
  }),
];
const feeders = [
  create(FeederSchema, { id: "f-b", code: "SUB-001-LV2", substationId: "sub-1" }),
  create(FeederSchema, { id: "f-a", code: "LV10", substationId: "sub-1" }),
  create(FeederSchema, { id: "f-v", code: "SUB-007-LV1", substationId: "sub-7" }),
  create(FeederSchema, { id: "f-x", code: "LOOSE" }),
];
const sites = [
  site({
    id: "s-1",
    nmi: "NMI00000017",
    feederId: "f-a",
    latitudeDeg: -33.85,
    longitudeDeg: 151.07,
  }),
  site({
    id: "s-2",
    nmi: "NMI00000025",
    feederId: "f-b",
    latitudeDeg: -33.86,
    longitudeDeg: 151.05,
  }),
  site({
    id: "s-3",
    nmi: "NMI00000033",
    feederId: "f-b",
    latitudeDeg: -33.84,
    longitudeDeg: 151.06,
  }),
  site({
    id: "s-4",
    nmi: "NMI00000674",
    feederId: "f-v",
    latitudeDeg: -37.79,
    longitudeDeg: 144.9,
  }),
  site({ id: "s-5", nmi: "NMI00000900", feederId: "f-x", latitudeDeg: -30, longitudeDeg: 150 }),
  site({ id: "s-6", nmi: "NMI00000901", feederId: "f-gone", latitudeDeg: -31, longitudeDeg: 150 }),
  // No place: not on the map.
  site({
    id: "s-7",
    nmi: "XDLAB000014",
    feederId: "f-a",
    latitudeDeg: undefined,
    longitudeDeg: undefined,
  }),
];
const summary = (feederId: string, fields: Init<typeof FleetSummarySchema> = {}) =>
  create(FleetSummarySchema, { feederId, latestRunId: "run-1", ...fields });
const state = (
  siteStates: Init<typeof SiteStateSchema>[],
  summaries = [summary("f-a"), summary("f-b"), summary("f-v")],
) =>
  create(GetFleetStateResponseSchema, {
    feeders: summaries,
    sites: siteStates.map((s) => stateOf(s)),
  });

const busy = state([
  { siteId: "s-1", envelope: envelope(3000), netExportW: 1500 },
  { siteId: "s-2", envelope: envelope(2000), netExportW: 2500, overLimit: true },
  { siteId: "s-3", envelope: envelope(2000), reporting: false, netExportW: undefined },
  { siteId: "s-4", envelope: envelope(4000), netExportW: -700 },
]);

describe("the fleet as marks", () => {
  it("places every located site, with its feeder and its substation", () => {
    const view = fleetView(substations, feeders, sites, busy);
    expect(view.sites.map((s) => s.nmi)).toEqual([
      "NMI00000017",
      "NMI00000025",
      "NMI00000033",
      "NMI00000674",
      "NMI00000900",
      "NMI00000901",
    ]);
    expect(view.sites[0]).toMatchObject({
      latitude: -33.85,
      longitude: 151.07,
      feederCode: "LV10",
      substationCode: "SUB-001",
      feederIndex: 0,
      kind: "solar",
      exportW: 1500,
      limitW: 3000,
      fill: 0.5,
    });
    // The feeders of a substation are counted in code order.
    expect(view.sites[1]).toMatchObject({ feederCode: "SUB-001-LV2", feederIndex: 1, fill: 1 });
    // Not reporting: no flow, no fill, and the limit still known.
    expect(view.sites[2]).toMatchObject({ exportW: undefined, limitW: 2000, fill: 0 });
    // Importing uses none of an export limit.
    expect(view.sites[3]).toMatchObject({ exportW: -700, fill: 0, substationCode: "SUB-007" });
    // A feeder that hangs from nothing, and a feeder that is gone.
    expect(view.sites[4]).toMatchObject({
      feederCode: "LOOSE",
      substationCode: "",
      feederIndex: 0,
    });
    expect(view.sites[5]).toMatchObject({ feederCode: "", substationCode: "" });
    expect(view.sites[5]!.status.label).toBe("No state");
  });

  it("sums a substation from its sites, and names its gravest", () => {
    const [lidcombe, footscray] = fleetView(substations, feeders, sites, busy).substations;
    expect(lidcombe).toMatchObject({
      code: "SUB-001",
      place: "Lidcombe",
      state: "NSW",
      feeders: 2,
      sites: 3,
      reporting: 2,
      exportW: 4000,
      limitW: 7000,
    });
    expect(lidcombe!.status).toEqual({
      level: "warn",
      label: "1 over limit",
      detail: "2 of 3 sites reporting, exporting 4.0\u00a0kW of 7.0\u00a0kW allowed.",
    });
    expect(footscray!.status).toMatchObject({ level: "ok", label: "Normal" });
    expect(footscray!.status.detail).toBe(
      "1 of 1 site reporting, exporting 0.0\u00a0kW of 4.0\u00a0kW allowed.",
    );
  });

  it("words each state of a substation", () => {
    const label = (s: ReturnType<typeof state>) =>
      fleetView(substations, feeders, sites, s).substations[0]!.status;
    const offline = {
      reporting: false,
      netExportW: undefined,
      openAlert: alert(AlertKind.DEVICE_OFFLINE, AlertSeverity.WARNING),
    };
    expect(
      label(
        state([
          { siteId: "s-1", ...offline },
          { siteId: "s-2", envelope: envelope(2000) },
        ]),
      ),
    ).toMatchObject({
      level: "warn",
      label: "1 offline",
    });
    const held = { envelope: envelope(2000), netExportW: 1990 };
    expect(label(state([{ siteId: "s-1", ...held }])).label).toBe("1 at its limit");
    expect(
      label(
        state([
          { siteId: "s-1", ...held },
          { siteId: "s-2", ...held },
        ]),
      ).label,
    ).toBe("2 at their limit");
    // A site that only says nothing is no alarm, and not "normal" either.
    expect(
      label(
        state([
          { siteId: "s-1", envelope: envelope(3000) },
          { siteId: "s-2", envelope: envelope(3000) },
          { siteId: "s-3", reporting: false, netExportW: undefined },
        ]),
      ),
    ).toMatchObject({ level: "info", label: "Not all reporting" });
    expect(
      label(state([], [summary("f-a", { backstopEventId: "b-1" }), summary("f-b")])),
    ).toMatchObject({
      level: "critical",
      label: "Backstop active",
    });
    expect(label(state([], [summary("f-a", { latestRunId: undefined })]))).toMatchObject({
      level: "info",
      label: "No envelopes yet",
    });
  });

  it("has places and no news until the state arrives", () => {
    const view = fleetView(substations, feeders, sites, undefined);
    expect(view.sites).toHaveLength(6);
    expect(view.sites[0]!.status.label).toBe("Loading");
    expect(view.substations[0]!.status.label).toBe("Loading");
    expect(view.sites[0]).toMatchObject({ exportW: undefined, limitW: undefined, fill: 0 });
  });
});

describe("a substation's place", () => {
  it("is the middle of owner, place and kind", () => {
    expect(placeName("Ausgrid Crows Nest Zone")).toBe("Crows Nest");
    expect(placeName("Jemena Footscray Zone")).toBe("Footscray");
  });
  it("is the whole name when the name is not of that shape", () => {
    expect(placeName("Lidcombe")).toBe("Lidcombe");
    expect(placeName("North Zone")).toBe("North Zone");
    expect(placeName("Lidcombe Terminal Station")).toBe("Lidcombe Terminal Station");
  });
});

describe("what the map frames", () => {
  const view = fleetView(substations, feeders, sites, undefined);

  it("lists the regions, the largest first", () => {
    const three = fleetView(
      [
        ...substations,
        create(SubstationSchema, { id: "sub-9", code: "SUB-009", name: "Coburg", state: "VIC" }),
      ],
      feeders,
      sites,
      undefined,
    );
    expect(regionsOf(three)).toEqual(["VIC", "NSW"]);
    // Equal in size: by name.
    expect(regionsOf(view)).toEqual(["NSW", "VIC"]);
  });

  it("is a box around a region's substations and their sites", () => {
    expect(frame(view, "VIC", "")).toEqual([
      [-37.8048, 144.9],
      [-37.79, 144.9011],
    ]);
    expect(frame(view, "all", "")).toEqual([
      [-37.8048, 144.9],
      [-33.84, 151.07],
    ]);
    expect(frame(view, "WA", "")).toBeUndefined();
  });

  it("is a box around one substation when one is chosen, whatever the region", () => {
    expect(frame(view, "VIC", "SUB-001")).toEqual([
      [-33.86, 151.05],
      [-33.84, 151.07],
    ]);
    expect(frame(view, "all", "SUB-404")).toBeUndefined();
  });

  it("has no box around nothing", () => {
    expect(boundsOf([])).toBeUndefined();
  });
});
