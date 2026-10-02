import { BindingConstraint } from "@doelab/gen/doelab/v1/common_pb.js";
import type { FeederLineState, FeederNodeState } from "@doelab/gen/doelab/v1/envelope_run_pb.js";
import type { FeederLine, FeederNode } from "@doelab/gen/doelab/v1/feeder_pb.js";
import type { Site } from "@doelab/gen/doelab/v1/site_pb.js";
import type { Envelope } from "@doelab/gen/doelab/v1/envelope_pb.js";
import type { GetFeederStateResponse } from "@doelab/gen/doelab/v1/telemetry_pb.js";
import { bindingWords, kw, percent, volts } from "../format.ts";
import type { Level } from "../status.ts";
import { layout } from "./layout.ts";

// A feeder as a drawing: every bus and line placed, and judged against the
// limits at one of the three operating points the engine solves.

// The operating points: every site at its forecast, every enrolled site
// exporting at its envelope, and every enrolled site exporting at the fixed
// limit.
export const POINTS = ["forecast", "envelope", "static"] as const;
export type Point = (typeof POINTS)[number];
export const POINT_WORDS: Record<Point, string> = {
  forecast: "Forecast",
  envelope: "At the envelopes",
  static: "At the fixed limit",
};

export function pointOf(value: string | null): Point {
  return POINTS.includes(value as Point) ? (value as Point) : "forecast";
}

// The grain of the instants the drawing can show: a half hour, which every
// interval length of a config divides.
export const STEP_SECONDS = 1800;

// The instant in the address, in unix seconds: unset, or anything that is not
// a time, means now.
export function atOf(value: string | null): number | undefined {
  const at = Number(value);
  return value !== null && value !== "" && Number.isInteger(at) && at > 0 ? at : undefined;
}

// The instant some half hours before or after the one on show, on a half-hour
// boundary. `at` unset is the half hour that holds now.
export function stepFrom(at: number | undefined, nowSeconds: number, steps: number): number {
  const from = Math.floor((at ?? nowSeconds) / STEP_SECONDS) * STEP_SECONDS;
  return from + steps * STEP_SECONDS;
}

// A voltage this close to a limit, per unit, is a warning; so is a line
// carrying this share of its rating.
export const NEAR_PU = 0.01;
export const NEAR_LOADING_PCT = 80;

export type NodeMark = {
  id: string;
  name: string;
  x: number;
  y: number;
  leaf: boolean;
  // The voltage of each phase, per unit; unset until the engine has solved
  // the interval.
  vPu: number[] | undefined;
  level: Level;
  label: string;
  // The sites connected at this bus, by NMI.
  sites: string[];
  // True when a site here has flexible DER: the sites the envelopes are for.
  enrolled: boolean;
};

export type LineMark = {
  id: string;
  name: string;
  // The corners of the line as drawn: out of the parent along its lane's
  // height, then across.
  points: [number, number][];
  isSwitch: boolean;
  // Share of the line's rating in use, as a percentage of the config's limit;
  // unset for a line with no rating, or not solved.
  loadingPct: number | undefined;
  // Power towards the far end, in watts: negative flows back to the
  // transformer.
  powerW: number | undefined;
  // How heavy to draw the line, from 0 to 1: its power against the busiest
  // line's.
  weight: number;
  level: Level;
  label: string;
};

export type Schematic = {
  nodes: NodeMark[];
  lines: LineMark[];
  // The size of the drawing, in its own units, and the cable it spans.
  width: number;
  height: number;
  lengthM: number;
  // False until the engine has solved the interval on show.
  solved: boolean;
  // What limits the feeder's export now, in words, and the mark it is at:
  // unset when nothing on the network does.
  binding: { words: string; nodeId?: string; lineId?: string } | undefined;
  // The marks closest to their limits, the worst first, for the table.
  worst: { kind: "bus" | "line"; id: string; name: string; level: Level; label: string }[];
};

// The drawing's units: a lane's height, the margins, and the width a feeder
// is fitted to.
const LANE = 44;
const MARGIN = 28;
const WIDTH = 1000;

const RANK: Record<Level, number> = { ok: 0, info: 1, warn: 2, critical: 3 };

function pick<T extends FeederNodeState | FeederLineState, K extends string>(
  state: T | undefined,
  point: Point,
  field: (p: Point) => K,
): T[K & keyof T] | undefined {
  return state?.[field(point) as K & keyof T];
}

// Judges a bus by its phase furthest outside, or nearest to, the band.
function nodeStatus(
  vPu: number[] | undefined,
  vMin: number,
  vMax: number,
  nominalV: number,
): { level: Level; label: string } {
  if (!vPu || vPu.length === 0) return { level: "info", label: "Not solved" };
  const high = Math.max(...vPu);
  const low = Math.min(...vPu);
  // The phase that matters is the one nearest a limit.
  const worst = vMax - high < low - vMin ? high : low;
  const label = volts(worst, nominalV);
  if (vMax > 0 && (high > vMax || low < vMin)) return { level: "critical", label };
  if (vMax > 0 && (high > vMax - NEAR_PU || low < vMin + NEAR_PU)) return { level: "warn", label };
  return { level: "ok", label };
}

// `sample` is the envelope in force at one site that takes part: every site
// of an interval shares what binds it.
export function schematic(
  nodes: FeederNode[],
  lines: FeederLine[],
  sites: Site[],
  state: GetFeederStateResponse | undefined,
  sample: Envelope | undefined,
  point: Point,
  nominalV: number,
): Schematic {
  const placed = layout(
    nodes.map((n) => ({ id: n.id, name: n.name, parentId: n.parentNodeId })),
    lines.map((l) => ({ id: l.id, toNodeId: l.toNodeId, lengthM: l.lengthM })),
  );
  const scale = placed.lengthM > 0 ? (WIDTH - 2 * MARGIN) / placed.lengthM : 0;
  const at = new Map(
    placed.nodes.map((n) => [
      n.id,
      { x: MARGIN + n.distanceM * scale, y: MARGIN + (n.lane + 0.5) * LANE },
    ]),
  );
  const nodeState = new Map(state?.nodes.map((s) => [s.nodeId, s]));
  const lineState = new Map(state?.lines.map((s) => [s.lineId, s]));
  const vMin = state?.vMinPu ?? 0;
  const vMax = state?.vMaxPu ?? 0;
  const lineLimit = (state?.lineLimitPct ?? 100) / 100;

  const sitesAt = new Map<string, Site[]>();
  for (const site of sites) sitesAt.set(site.nodeId, [...(sitesAt.get(site.nodeId) ?? []), site]);

  const nodeMarks = placed.nodes.map((n): NodeMark => {
    const vPu = pick(nodeState.get(n.id), point, (p) => `${p}VPu` as const);
    const here = sitesAt.get(n.id) ?? [];
    return {
      id: n.id,
      name: n.name,
      ...at.get(n.id)!,
      leaf: n.leaf,
      vPu,
      ...nodeStatus(vPu, vMin, vMax, nominalV),
      sites: here.map((s) => s.nmi),
      enrolled: here.some((s) => s.exportCapW > 0 || s.importCapW > 0),
    };
  });

  const powers = lines.map((l) =>
    Math.abs(pick(lineState.get(l.id), point, (p) => `${p}PowerW` as const) ?? 0),
  );
  const busiest = Math.max(0, ...powers);
  const lineMarks = lines.flatMap((l, i): LineMark[] => {
    const from = at.get(l.fromNodeId);
    const to = at.get(l.toNodeId);
    if (!from || !to) return [];
    const s = lineState.get(l.id);
    const current = pick(s, point, (p) => `${p}CurrentA` as const);
    const powerW = pick(s, point, (p) => `${p}PowerW` as const);
    const loadingPct =
      current && l.ampacityA !== undefined && l.ampacityA > 0
        ? (100 * Math.max(...current)) / (l.ampacityA * lineLimit)
        : undefined;
    let level: Level = "ok";
    if (!s) level = "info";
    else if (loadingPct !== undefined && loadingPct > 100) level = "critical";
    else if (loadingPct !== undefined && loadingPct > NEAR_LOADING_PCT) level = "warn";
    const flow = powerW === undefined ? "Not solved" : kw(Math.abs(powerW));
    const direction = powerW !== undefined && powerW < 0 ? " towards the transformer" : "";
    const rated = loadingPct === undefined ? "" : `, ${percent(loadingPct)} of its rating`;
    return [
      {
        id: l.id,
        name: l.name,
        // Down or up the parent's side to the far end's height, then across.
        points: [
          [from.x, from.y],
          [from.x, to.y],
          [to.x, to.y],
        ],
        isSwitch: l.isSwitch,
        loadingPct,
        powerW,
        weight: busiest > 0 ? powers[i]! / busiest : 0,
        level,
        label: `${flow}${direction}${rated}`,
      },
    ];
  });

  return {
    nodes: nodeMarks,
    lines: lineMarks,
    width: WIDTH,
    height: 2 * MARGIN + Math.max(placed.lanes, 1) * LANE,
    lengthM: placed.lengthM,
    solved: (state?.nodes.length ?? 0) > 0,
    binding: bindingOf(sample, nodeMarks, lineMarks, nodes),
    worst: worstOf(nodeMarks, lineMarks),
  };
}

// What holds the feeder's export limits down: the element that the envelope
// in force names, when it is something on the network.
function bindingOf(
  envelope: Envelope | undefined,
  nodeMarks: NodeMark[],
  lineMarks: LineMark[],
  nodes: FeederNode[],
): Schematic["binding"] {
  if (
    !envelope ||
    ![
      BindingConstraint.VOLTAGE_HIGH,
      BindingConstraint.VOLTAGE_LOW,
      BindingConstraint.TRANSFORMER,
      BindingConstraint.LINE,
    ].includes(envelope.exportBinding)
  ) {
    return undefined;
  }
  const words = bindingWords(envelope.exportBinding, envelope.exportBindingElement);
  if (envelope.exportBinding === BindingConstraint.TRANSFORMER) {
    return { words, nodeId: nodes.find((n) => n.parentNodeId === undefined)?.id };
  }
  if (envelope.exportBinding === BindingConstraint.LINE) {
    return { words, lineId: lineMarks.find((l) => l.name === envelope.exportBindingElement)?.id };
  }
  // A voltage limit binds at a customer: the element is the site's NMI.
  return {
    words,
    nodeId: nodeMarks.find((n) => n.sites.includes(envelope.exportBindingElement))?.id,
  };
}

// How many marks the table lists.
const WORST = 8;

function worstOf(nodeMarks: NodeMark[], lineMarks: LineMark[]): Schematic["worst"] {
  // How far past, or how near, its limit a mark is: the larger the worse.
  const voltage = (n: NodeMark) => (n.vPu ? Math.max(...n.vPu.map((v) => Math.abs(v - 1))) : 0);
  const rows = [
    ...nodeMarks
      // A customer's bus, or a bus in trouble: the junctions between say
      // nothing a customer's bus does not.
      .filter((n) => n.vPu && (n.sites.length > 0 || n.level !== "ok"))
      .map((n) => ({
        kind: "bus" as const,
        id: n.id,
        name: n.sites.length > 0 ? `${n.name} (${n.sites.join(", ")})` : n.name,
        level: n.level,
        label: n.label,
        score: voltage(n),
      })),
    ...lineMarks
      .filter((l) => l.loadingPct !== undefined)
      .map((l) => ({
        kind: "line" as const,
        id: l.id,
        name: l.name,
        level: l.level,
        label: l.label,
        // On the scale of a voltage's distance from nominal: a line at its
        // rating ranks with a bus a tenth off.
        score: l.loadingPct! / 1000,
      })),
  ];
  return rows
    .sort(
      (a, b) => RANK[b.level] - RANK[a.level] || b.score - a.score || a.name.localeCompare(b.name),
    )
    .slice(0, WORST)
    .map(({ kind, id, name, level, label }) => ({ kind, id, name, level, label }));
}
