import { create } from "@bufbuild/protobuf";
import { BindingConstraint, EnvelopeSource } from "@doelab/gen/doelab/v1/common_pb.js";
import { EnvelopeSchema } from "@doelab/gen/doelab/v1/envelope_pb.js";
import {
  FeederLineStateSchema,
  FeederNodeStateSchema,
} from "@doelab/gen/doelab/v1/envelope_run_pb.js";
import { FeederLineSchema, FeederNodeSchema } from "@doelab/gen/doelab/v1/feeder_pb.js";
import { SiteSchema } from "@doelab/gen/doelab/v1/site_pb.js";
import { GetFeederStateResponseSchema } from "@doelab/gen/doelab/v1/telemetry_pb.js";
import { describe, expect, it } from "vitest";
import { timestamp } from "../time.ts";
import { atOf, colour, place, pointOf, schematic, solvedKey, stepFrom } from "./schematic.ts";

// tx ── mid ── far, with a house off mid on a switch-less service.
const nodes = [
  create(FeederNodeSchema, { id: "tx", name: "B1" }),
  create(FeederNodeSchema, { id: "mid", name: "B2", parentNodeId: "tx" }),
  create(FeederNodeSchema, { id: "far", name: "B3", parentNodeId: "mid" }),
  create(FeederNodeSchema, { id: "house", name: "B4", parentNodeId: "mid" }),
];
const lines = [
  create(FeederLineSchema, {
    id: "l-main",
    name: "main",
    fromNodeId: "tx",
    toNodeId: "mid",
    lengthM: 300,
    ampacityA: 100,
  }),
  create(FeederLineSchema, {
    id: "l-far",
    name: "L_far",
    fromNodeId: "mid",
    toNodeId: "far",
    lengthM: 200,
    ampacityA: 50,
  }),
  create(FeederLineSchema, {
    id: "l-house",
    name: "Switch_1",
    fromNodeId: "mid",
    toNodeId: "house",
    lengthM: 20,
    isSwitch: true,
  }),
];
const sites = [
  create(SiteSchema, { id: "s-1", nmi: "NMI00000017", nodeId: "far", exportCapW: 5000 }),
  create(SiteSchema, { id: "s-2", nmi: "XDLAB000014", nodeId: "house" }),
  create(SiteSchema, { id: "s-3", nmi: "XDLAB000022", nodeId: "house" }),
];
const nodeState = (nodeId: string, forecast: number[], envelope = forecast, fixed = envelope) =>
  create(FeederNodeStateSchema, {
    nodeId,
    forecastVPu: forecast,
    envelopeVPu: envelope,
    staticVPu: fixed,
  });
const lineState = (lineId: string, amps: number, watts: number) =>
  create(FeederLineStateSchema, {
    lineId,
    forecastCurrentA: [amps, 1, 1, amps],
    envelopeCurrentA: [2 * amps, 1, 1, 2 * amps],
    staticCurrentA: [3 * amps, 1, 1, 3 * amps],
    forecastPowerW: watts,
    envelopePowerW: -watts,
    staticPowerW: -2 * watts,
  });
const state = create(GetFeederStateResponseSchema, {
  nodes: [
    nodeState("tx", [1.04, 1.04, 1.04]),
    nodeState("mid", [1.05, 1.04, 1.03], [1.092, 1.04, 1.03], [1.12, 1.04, 1.03]),
    nodeState("far", [1.06, 1.03, 0.93], [1.095, 1.03, 1.0], [1.13, 1.03, 1.0]),
    // The house is not solved.
  ],
  lines: [lineState("l-main", 30, 6000), lineState("l-far", 22, 3000)],
  vMinPu: 0.94,
  vMaxPu: 1.1,
  lineLimitPct: 100,
  transformerLimitPct: 100,
});
const bound = (binding: BindingConstraint, element: string) =>
  create(EnvelopeSchema, {
    source: EnvelopeSource.ENGINE,
    exportBinding: binding,
    exportBindingElement: element,
  });

const draw = (
  point: Parameters<typeof schematic>[5] = "forecast",
  sample: Parameters<typeof schematic>[4] = undefined,
  solved: Parameters<typeof schematic>[3] = state,
) => schematic(nodes, lines, sites, solved, sample, point, 230);

describe("the operating point", () => {
  it("is read from the address, and is the forecast when the address says nothing sensible", () => {
    expect(pointOf("envelope")).toBe("envelope");
    expect(pointOf("static")).toBe("static");
    expect(pointOf(null)).toBe("forecast");
    expect(pointOf("yesterday")).toBe("forecast");
  });
});

describe("the instant on show", () => {
  it("is read from the address, and is now when the address says nothing sensible", () => {
    expect(atOf("1791600000")).toBe(1791600000);
    for (const value of [null, "", "soon", "-5", "0", "1.5"]) expect(atOf(value)).toBeUndefined();
  });

  it("steps by half hours, from the half hour that holds it", () => {
    // A half-hour boundary, and ten minutes past it.
    const start = 995334 * 1800;
    const now = start + 600;
    expect(stepFrom(undefined, now, 0)).toBe(start);
    expect(stepFrom(undefined, now, 1)).toBe(start + 1800);
    expect(stepFrom(undefined, now, -1)).toBe(start - 1800);
    // From an instant in the address, whatever the clock says.
    expect(stepFrom(start + 1800, now, 2)).toBe(start + 5400);
    expect(stepFrom(start + 1805, now, -1)).toBe(start);
  });
});

describe("a feeder as a drawing", () => {
  it("places the buses by cable length, and draws each line as a corner from its parent", () => {
    const view = draw();
    expect(view).toMatchObject({ width: 1000, lengthM: 500, solved: true });
    const at = Object.fromEntries(view.nodes.map((n) => [n.name, [n.x, n.y]]));
    // 28 units of margin, and 944 for 500 m of cable; one lane, 44 units tall.
    expect(at.B1).toEqual([28, 50]);
    expect(at.B2![0]).toBeCloseTo(28 + 944 * 0.6);
    expect(at.B3).toEqual([972, 50]);
    // The house hangs off the lane.
    expect(at.B4![1]).toBeCloseTo(50 - 0.36 * 44);
    expect(view.height).toBe(2 * 28 + 44);
    const service = view.lines.find((l) => l.name === "Switch_1")!;
    expect(service.points).toEqual([at.B2, [at.B2![0], at.B4![1]], at.B4]);
    expect(service.isSwitch).toBe(true);
  });

  it("judges a bus by its phase nearest the band's edge", () => {
    const view = draw();
    const level = (name: string) => {
      const n = view.nodes.find((x) => x.name === name)!;
      return [n.level, n.label];
    };
    expect(level("B1")).toEqual(["ok", "239.2 V"]);
    // 0.93 pu is under the 0.94 limit: the low phase is the one that counts.
    expect(level("B3")).toEqual(["critical", "213.9 V"]);
    expect(level("B4")).toEqual(["info", "Not solved"]);
    // Within 0.01 pu of the limit is a warning; past it is critical.
    expect(draw("envelope").nodes.find((n) => n.name === "B2")).toMatchObject({
      level: "warn",
      label: "251.2 V",
    });
    expect(draw("static").nodes.find((n) => n.name === "B2")!.level).toBe("critical");
    expect(draw("envelope").nodes.find((n) => n.name === "B3")!.level).toBe("warn");
  });

  it("says which buses have sites, and which of them take part", () => {
    const view = draw();
    expect(view.nodes.find((n) => n.name === "B3")).toMatchObject({
      sites: ["NMI00000017"],
      enrolled: true,
    });
    expect(view.nodes.find((n) => n.name === "B4")).toMatchObject({
      sites: ["XDLAB000014", "XDLAB000022"],
      enrolled: false,
    });
    expect(view.nodes.find((n) => n.name === "B2")).toMatchObject({ sites: [], enrolled: false });
  });

  it("judges a line by its busiest conductor against its rating, and weighs it by its power", () => {
    const line = (point: Parameters<typeof draw>[0], name: string) =>
      draw(point).lines.find((l) => l.name === name)!;
    expect(line("forecast", "main")).toMatchObject({
      level: "ok",
      loadingPct: 30,
      powerW: 6000,
      weight: 1,
      label: "6.0 kW, 30 % of its rating",
    });
    expect(line("forecast", "L_far")).toMatchObject({ level: "ok", loadingPct: 44, weight: 0.5 });
    // Twice the current: 88 % of 50 A is a warning. Three times is past it.
    expect(line("envelope", "L_far")).toMatchObject({
      level: "warn",
      powerW: -3000,
      label: "3.0 kW towards the transformer, 88 % of its rating",
    });
    expect(line("static", "L_far").level).toBe("critical");
    // No rating: not judged. Not solved: grey, and no weight.
    expect(line("forecast", "Switch_1")).toMatchObject({
      level: "info",
      loadingPct: undefined,
      powerW: undefined,
      weight: 0,
      label: "Not solved",
    });
  });

  it("judges lines against the config's share of their rating", () => {
    const strict = create(GetFeederStateResponseSchema, { ...state, lineLimitPct: 50 });
    const main = draw("forecast", undefined, strict).lines.find((l) => l.name === "main")!;
    expect(main.loadingPct).toBe(60);
  });

  it("is grey all over until the engine has solved the interval", () => {
    const view = schematic(nodes, lines, sites, undefined, undefined, "forecast", 230);
    expect(view.solved).toBe(false);
    expect(new Set(view.nodes.map((n) => n.level))).toEqual(new Set(["info"]));
    expect(new Set(view.lines.map((l) => l.level))).toEqual(new Set(["info"]));
    expect(view.worst).toEqual([]);
    // With no limits to judge by, a voltage is inside them.
    const unjudged = create(GetFeederStateResponseSchema, { nodes: state.nodes });
    expect(draw("forecast", undefined, unjudged).nodes.find((n) => n.name === "B3")!.level).toBe(
      "ok",
    );
  });

  it("names what limits the feeder, and where that is on the drawing", () => {
    expect(draw().binding).toBeUndefined();
    // A site at its own connection limit is limited by nothing on the network.
    expect(draw("forecast", bound(BindingConstraint.SITE_CAP, "")).binding).toBeUndefined();
    expect(draw("forecast", bound(BindingConstraint.VOLTAGE_HIGH, "XDLAB000014")).binding).toEqual({
      words: "high voltage at XDLAB000014",
      nodeId: "house",
    });
    expect(draw("forecast", bound(BindingConstraint.VOLTAGE_LOW, "NOWHERE")).binding).toEqual({
      words: "low voltage at NOWHERE",
      nodeId: undefined,
    });
    expect(draw("forecast", bound(BindingConstraint.LINE, "L_far")).binding).toEqual({
      words: "the rating of line L_far",
      lineId: "l-far",
    });
    expect(draw("forecast", bound(BindingConstraint.TRANSFORMER, "transformer")).binding).toEqual({
      words: "the transformer's rating",
      nodeId: "tx",
    });
  });

  it("lists what is closest to a limit, the worst first", () => {
    expect(draw("static").worst).toEqual([
      // Past its limit: the line a third over its rating, then the buses,
      // the one furthest from nominal first.
      {
        kind: "line",
        id: "l-far",
        name: "L_far",
        level: "critical",
        label: expect.stringContaining("132"),
      },
      {
        kind: "bus",
        id: "far",
        name: "B3 (NMI00000017)",
        level: "critical",
        label: "259.9\u00a0V",
      },
      { kind: "bus", id: "mid", name: "B2", level: "critical", label: "257.6\u00a0V" },
      {
        kind: "line",
        id: "l-main",
        name: "main",
        level: "warn",
        label: expect.stringContaining("90"),
      },
    ]);
    // In the forecast only the customer's bus and the rated lines are listed:
    // a junction that is inside its limits says nothing.
    expect(draw().worst.map((w) => w.name)).toEqual(["B3 (NMI00000017)", "L_far", "main"]);
  });

  it("gives each line its corners as a polyline's points, and the place of its arrow", () => {
    const view = draw();
    const service = view.lines.find((l) => l.name === "Switch_1")!;
    expect(service.path).toBe(service.points.map(([x, y]) => `${x},${y}`).join(" "));
    // The middle of the last, level stretch.
    const [, corner, end] = service.points;
    expect(service.arrow).toEqual({ x: (corner![0] + end![0]) / 2, y: end![1] });
  });

  it("is placed once and judged many times: the same drawing, and the same corners", () => {
    const placed = place(nodes, lines, sites);
    expect(placed).toMatchObject({ rootId: "tx", width: 1000, height: 100, lengthM: 500 });
    expect(placed.lineIds).toEqual(["l-main", "l-far", "l-house"]);
    for (const point of ["forecast", "envelope", "static"] as const) {
      const sample = bound(BindingConstraint.TRANSFORMER, "");
      expect(colour(placed, state, sample, point, 230)).toEqual(draw(point, sample));
    }
    // Judged again, a line keeps the corners it was placed with.
    const [first, second] = [
      colour(placed, state, undefined, "forecast", 230),
      colour(placed, undefined, undefined, "forecast", 230),
    ];
    expect(second.lines[0]!.points).toBe(first.lines[0]!.points);
    expect(second.solved).toBe(false);
  });

  it("knows a state it has seen: the same run, interval, limits and binding", () => {
    const again = create(GetFeederStateResponseSchema, state);
    const sample = bound(BindingConstraint.VOLTAGE_HIGH, "NMI00000017");
    expect(solvedKey(again, sample)).toBe(solvedKey(state, sample));
    // Another run, another interval, another limit or another envelope is new.
    const run = (id: string) =>
      create(GetFeederStateResponseSchema, {
        ...state,
        nodes: [create(FeederNodeStateSchema, { ...state.nodes[0]!, envelopeRunId: id })],
      });
    expect(solvedKey(run("r-2"), sample)).not.toBe(solvedKey(run("r-1"), sample));
    const later = create(GetFeederStateResponseSchema, {
      ...state,
      nodes: [
        create(FeederNodeStateSchema, { ...state.nodes[0]!, validFrom: timestamp(1800) }),
        ...state.nodes.slice(1),
      ],
    });
    expect(solvedKey(later, sample)).not.toBe(solvedKey(state, sample));
    const tighter = create(GetFeederStateResponseSchema, { ...state, vMaxPu: 1.08 });
    expect(solvedKey(tighter, sample)).not.toBe(solvedKey(state, sample));
    expect(solvedKey(state, create(EnvelopeSchema, { id: "e-2" }))).not.toBe(
      solvedKey(state, create(EnvelopeSchema, { id: "e-1" })),
    );
    // Nothing solved, and no envelope: still a key.
    expect(solvedKey(create(GetFeederStateResponseSchema, {}), undefined)).toBe("||0|0|0|0|0|0|");
  });

  it("skips a line whose ends the model does not have", () => {
    const orphan = create(FeederLineSchema, {
      id: "x",
      name: "x",
      fromNodeId: "gone",
      toNodeId: "far",
    });
    const view = schematic(nodes, [...lines, orphan], sites, state, undefined, "forecast", 230);
    expect(view.lines.map((l) => l.name)).toEqual(["main", "L_far", "Switch_1"]);
    // And a feeder of nothing at all is an empty drawing.
    expect(schematic([], [], [], undefined, undefined, "forecast", 230)).toMatchObject({
      nodes: [],
      lines: [],
      height: 100,
      lengthM: 0,
    });
  });
});
