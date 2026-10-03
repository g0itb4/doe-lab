import type { Page, Route } from "@playwright/test";

// A stand-in for the API, in the browser: every request to /rpc is answered
// here, in the JSON that the Connect protocol puts on the wire. It holds a
// little state, so that a backstop that was triggered is then active and a
// config that was saved is then in force.
//
// Feeder time is the wall clock here (speed 1) unless a test asks for the
// demo's speed, so the fixtures are built around now.

const FEEDER = "0199c0de-0000-7000-8000-000000000001";
const SITES = [
  {
    id: "0199c0de-0000-7000-8000-0000000000a1",
    nmi: "XDLAB000014",
    name: "Ld1_LOAD_A",
    phase: 1,
    pvKw: 5,
    inverterKva: 5,
    exportCapW: 5000,
    importCapW: 7000,
    hasBattery: true,
    batteryKwh: 13.5,
  },
  {
    id: "0199c0de-0000-7000-8000-0000000000a2",
    nmi: "XDLAB000022",
    name: "Ld2_LOAD_B",
    phase: 2,
    pvKw: 6.5,
    inverterKva: 5,
    exportCapW: 5000,
    importCapW: 7000,
    hasEv: true,
  },
  {
    id: "0199c0de-0000-7000-8000-0000000000a3",
    nmi: "XDLAB000030",
    name: "Ld3_LOAD_C",
    phase: 3,
    pvKw: 3,
  },
].map((s) => ({ feederId: FEEDER, nodeId: FEEDER, exportCapW: 0, ...s }));

// The fleet map: two substations, and three sites with a place around the
// first, on the feeder above.
const SUBSTATIONS = [
  {
    id: "0199c0de-0000-7000-8000-000000000501",
    code: "SUB-001",
    name: "Ausgrid Lidcombe Zone",
    dnsp: "Ausgrid",
    state: "NSW",
    latitudeDeg: -33.8524,
    longitudeDeg: 151.0621,
  },
  {
    id: "0199c0de-0000-7000-8000-000000000507",
    code: "SUB-007",
    name: "Jemena Footscray Zone",
    dnsp: "Jemena",
    state: "VIC",
    latitudeDeg: -37.8048,
    longitudeDeg: 144.9011,
  },
];
const SECOND_FEEDER = "0199c0de-0000-7000-8000-000000000002";
const NODES = {
  mid: "0199c0de-0000-7000-8000-000000000702",
  far: "0199c0de-0000-7000-8000-000000000703",
};
const LINES = {
  main: "0199c0de-0000-7000-8000-000000000801",
  far: "0199c0de-0000-7000-8000-000000000802",
};
const LOCATED = [
  {
    nmi: "NMI00000017",
    pvKw: 0,
    hasBattery: true,
    batteryKwh: 10.8,
    lat: -33.850025,
    lng: 151.078926,
  },
  { nmi: "NMI00000033", pvKw: 3.3, lat: -33.86503, lng: 151.061588 },
  { nmi: "NMI00000025", pvKw: 0, hasEv: true, exportCapW: 0, lat: -33.830587, lng: 151.078069 },
].map(({ lat, lng, ...s }, i) => ({
  id: `0199c0de-0000-7000-8000-0000000006a${i + 1}`,
  feederId: FEEDER,
  nodeId: FEEDER,
  name: `Ld${i + 1}`,
  phase: 1,
  exportCapW: 5000,
  importCapW: 14000,
  latitudeDeg: lat,
  longitudeDeg: lng,
  ...s,
}));
export const MAP = { within: LOCATED[0]!.nmi, over: LOCATED[1]!.nmi, charger: LOCATED[2]!.nmi };

export const OPERATOR_TOKEN = "operator-token-for-e2e";
export const NMI = { enrolled: SITES[0]!.nmi, breaching: SITES[1]!.nmi, passive: SITES[2]!.nmi };

const iso = (ms: number) => new Date(ms).toISOString();
const HALF_HOUR = 1800_000;

type Row = Record<string, unknown>;
// The JSON of a request: whatever the page sent.
// eslint-disable-next-line @typescript-eslint/no-explicit-any
type Message = any;

export type Mock = {
  // The calls that changed something, in order: "Service/Method".
  writes: string[];
  // When set, streams fail: the API is "down" for live updates.
  streamsDown: boolean;
  // Extra milliseconds before every answer.
  delayMs: number;
  // Whether the server has an assistant.
  assistant: "available" | "off";
  // Every question the assistant was asked.
  asked: Message[];
  // How many feeders the API has: with more than one, the header offers a
  // choice.
  feeders: 1 | 2;
  // How many tiles of the map were asked for.
  tiles: number;
  // How many times each procedure was called: "Service/Method".
  calls: Record<string, number>;
  // How fast feeder time runs. The demo runs at 60.
  speed: number;
  // Extra milliseconds before the clock's answer: the page then has its
  // feeder before it knows how feeder time runs.
  clockDelayMs: number;
  // The feeder's network: three buses, or a tree the size of the real one.
  network: "small" | "large";
  // The located sites: three, or as many as the demo's fleet has.
  fleet: "small" | "large";
};

// The size of the real feeder, and of the demo's fleet.
const LARGE_BUSES = 223;
const LARGE_SITES = 76;

// The same numbers on every run: a test must see the same network twice.
function sequence(seed: number): () => number {
  let state = seed;
  return () => (state = (state * 1103515245 + 12345) % 2147483648) / 2147483648;
}

// A tree of buses the size of the real feeder: runs of buses with branches
// off them, each line with a length, and a load at every bus so that the
// power in a line is that of everything beyond it.
function largeNetwork() {
  const rand = sequence(7);
  const hex = (kind: string, i: number) =>
    `0199c0de-0000-7000-8000-${kind}${String(i).padStart(11, "0")}`;
  const parent: number[] = [-1];
  for (let i = 1; i < LARGE_BUSES; i++) {
    parent.push(Math.max(0, i - 1 - Math.floor(rand() * rand() * rand() * i)));
  }
  const depth = parent.map(() => 0);
  for (let i = 1; i < LARGE_BUSES; i++) depth[i] = depth[parent[i]!]! + 1;
  const deepest = Math.max(...depth);
  // Everything beyond a bus, itself included: children come after parents.
  const beyond = parent.map(() => 1);
  for (let i = LARGE_BUSES - 1; i > 0; i--) beyond[parent[i]!]! += beyond[i]!;
  const nodeId = (i: number) => (i === 0 ? FEEDER : hex("a", i));
  const nodes = parent.map((p, i) => ({
    id: nodeId(i),
    feederId: FEEDER,
    name: `B${i + 1}`,
    ...(p >= 0 ? { parentNodeId: nodeId(p) } : {}),
  }));
  const lines = parent.slice(1).map((p, k) => ({
    id: hex("b", k + 1),
    feederId: FEEDER,
    linecode: "e2e",
    name: `L${k + 1}`,
    fromNodeId: nodeId(p),
    toNodeId: nodeId(k + 1),
    lengthM: Math.round(15 + 40 * rand()),
    ampacityA: 200,
  }));
  return { nodes, lines, depth, deepest, beyond };
}

// As many located sites as the demo's fleet has, around the first
// substation: some within their limit, some over it, some silent.
function largeFleet() {
  const rand = sequence(11);
  return Array.from({ length: LARGE_SITES }, (_, i) => ({
    id: `0199c0de-0000-7000-8000-c${String(i).padStart(11, "0")}`,
    feederId: FEEDER,
    nodeId: FEEDER,
    nmi: `NMI${String(10_000 + i).padStart(8, "0")}`,
    name: `Ld${i + 1}`,
    phase: 1 + (i % 3),
    pvKw: i % 4 === 0 ? 0 : 3 + (i % 5),
    hasBattery: i % 4 === 0,
    exportCapW: 5000,
    importCapW: 14000,
    latitudeDeg: SUBSTATIONS[0]!.latitudeDeg + (rand() - 0.5) * 0.05,
    longitudeDeg: SUBSTATIONS[0]!.longitudeDeg + (rand() - 0.5) * 0.06,
  }));
}

export async function mockApi(page: Page): Promise<Mock> {
  const mock: Mock = {
    writes: [],
    streamsDown: false,
    delayMs: 0,
    assistant: "available",
    asked: [],
    feeders: 1,
    tiles: 0,
    calls: {},
    speed: 1,
    clockDelayMs: 0,
    network: "small",
    fleet: "small",
  };
  let large: ReturnType<typeof largeNetwork> | undefined;
  const network = () => (large ??= largeNetwork());
  let manySites: ReturnType<typeof largeFleet> | undefined;
  const located = () => (mock.fleet === "large" ? (manySites ??= largeFleet()) : LOCATED);
  const start = Math.floor(Date.now() / HALF_HOUR) * HALF_HOUR;

  let backstop: Row | undefined;
  const backstops: Row[] = [];
  const configs: Row[] = [
    {
      id: "0199c0de-0000-7000-8000-0000000000c1",
      feederId: FEEDER,
      version: 1,
      policy: "ENVELOPE_POLICY_EQUAL",
      vMinPu: 0.94,
      vMaxPu: 1.1,
      transformerLimitPct: 100,
      lineLimitPct: 100,
      pvScale: 3,
      staticLimitW: 5000,
      intervalMinutes: 30,
      horizonIntervals: 48,
      breachGraceSeconds: 60,
      offlineAfterSeconds: 300,
      note: "created by the import",
      createdBy: "import",
      createdAt: iso(start - 86400_000),
    },
  ];
  const alerts: Row[] = [
    {
      id: "0199c0de-0000-7000-8000-0000000000e1",
      siteId: SITES[1]!.id,
      feederId: FEEDER,
      kind: "ALERT_KIND_CONSTRAINT_BREACH",
      severity: "ALERT_SEVERITY_WARNING",
      openedAt: iso(start - 2 * HALF_HOUR),
      limitW: 1500,
      peakW: 2740,
      detail: "Net export above the 1500 W limit for more than 1m0s.",
    },
    {
      id: "0199c0de-0000-7000-8000-0000000000e2",
      siteId: SITES[0]!.id,
      feederId: FEEDER,
      deviceId: "0199c0de-0000-7000-8000-0000000000d1",
      kind: "ALERT_KIND_DEVICE_OFFLINE",
      severity: "ALERT_SEVERITY_INFO",
      openedAt: iso(start - 8 * HALF_HOUR),
      resolvedAt: iso(start - 7 * HALF_HOUR),
      detail: "No reading from the solar device for more than 5m0s.",
    },
  ];

  const feeders = () =>
    [
      {
        id: FEEDER,
        code: "LV10",
        name: "lv10_223bus",
        nominalVoltageV: 230,
        transformerKva: 500,
        timezone: "Australia/Sydney",
        substationId: SUBSTATIONS[0]!.id,
      },
      {
        id: SECOND_FEEDER,
        code: "SUB-007-LV1",
        name: "lv22_80bus",
        nominalVoltageV: 230,
        transformerKva: 500,
        timezone: "Australia/Melbourne",
        substationId: SUBSTATIONS[1]!.id,
      },
    ].slice(0, mock.feeders);

  const envelope = (site: { id: string }, from: number) => ({
    id: `0199c0de-0000-7000-8000-${String(from / HALF_HOUR).padStart(12, "0")}`,
    siteId: site.id,
    validFrom: iso(from),
    validTo: iso(from + HALF_HOUR),
    opModExpLimW: backstop ? 0 : 1500 + 250 * (new Date(from).getUTCHours() % 6),
    opModImpLimW: 7000,
    source: backstop ? "ENVELOPE_SOURCE_BACKSTOP" : "ENVELOPE_SOURCE_ENGINE",
    exportBinding: "BINDING_CONSTRAINT_VOLTAGE_HIGH",
    exportBindingElement: "XDLAB000022",
    importBinding: "BINDING_CONSTRAINT_SITE_CAP",
    createdAt: iso(start),
  });
  const summary = () => ({
    feederId: FEEDER,
    at: iso(Date.now()),
    enrolledSites: 2,
    reportingSites: 2,
    devices: 3,
    devicesOnline: 3,
    exportW: backstop ? 0 : 2840,
    importW: 310,
    exportLimitW: backstop ? 0 : 3500,
    // One site that takes no part exports 0.3 kW of the 2.84.
    controlledExportW: backstop ? 0 : 2540,
    sitesOverLimit: 0,
    openAlerts: alerts.filter((a) => !a.resolvedAt).length,
    backstopEventId: backstop?.id,
    latestRunId: "0199c0de-0000-7000-8000-0000000000f1",
    latestRunAt: iso(Date.now() - 40_000),
    latestRunStatus: "RUN_STATUS_COMPLETED",
  });

  const smallFleet = () => [
    {
      siteId: LOCATED[0]!.id,
      envelope: envelope(LOCATED[0]!, start),
      reporting: true,
      netExportW: 400,
    },
    {
      siteId: LOCATED[1]!.id,
      envelope: envelope(LOCATED[1]!, start),
      reporting: true,
      netExportW: 2740,
      overLimit: true,
      openAlert: { ...alerts[0], siteId: LOCATED[1]!.id },
    },
    { siteId: LOCATED[2]!.id, envelope: envelope(LOCATED[2]!, start), reporting: false },
  ];

  const unary: Record<string, (req: Message, token: string | null) => unknown> = {
    "ClockService/GetClock": () => ({
      now: iso(Date.now()),
      anchor: iso(Date.now()),
      speed: mock.speed,
      wallNow: iso(Date.now()),
    }),
    "FeederService/ListFeeders": (req) => ({ feeders: feeders().slice(0, req.pageSize || 100) }),
    "FeederService/GetFeeder": (req) => {
      const feeder = feeders().find((f) => f.code === req.code || f.id === req.id);
      if (!feeder) throw new RpcError("not_found", `feeder ${req.code} not found`, 404);
      return { feeder };
    },
    // The network of the feeder: the transformer's bus, where the fixture's
    // sites are, a junction 300 m out and a far end 200 m beyond it.
    "FeederService/ListFeederNodes": () => ({
      feederNodes:
        mock.network === "large"
          ? network().nodes
          : [
              { id: FEEDER, feederId: FEEDER, name: "B1" },
              { id: NODES.mid, feederId: FEEDER, name: "B2", parentNodeId: FEEDER },
              { id: NODES.far, feederId: FEEDER, name: "B3", parentNodeId: NODES.mid },
            ],
    }),
    "FeederService/ListFeederLines": () => ({
      feederLines:
        mock.network === "large"
          ? network().lines
          : [
              {
                id: LINES.main,
                name: "L_main",
                fromNodeId: FEEDER,
                toNodeId: NODES.mid,
                lengthM: 300,
                ampacityA: 100,
              },
              {
                id: LINES.far,
                name: "L_far",
                fromNodeId: NODES.mid,
                toNodeId: NODES.far,
                lengthM: 200,
                ampacityA: 50,
              },
            ].map((l) => ({ feederId: FEEDER, linecode: "e2e", ...l })),
    }),
    "TelemetryService/GetFeederState": (req) => {
      // The half hour that holds the instant asked for; now when none is.
      const from = req.at ? Math.floor(Date.parse(req.at) / HALF_HOUR) * HALF_HOUR : start;
      const interval = { validFrom: iso(from), validTo: iso(from + HALF_HOUR) };
      const node = (nodeId: string, forecast: number, envelope: number, fixed: number) => ({
        ...interval,
        nodeId,
        forecastVPu: [forecast, 1.03, 1.03],
        envelopeVPu: [envelope, 1.03, 1.03],
        staticVPu: [fixed, 1.03, 1.03],
      });
      const line = (lineId: string, amps: number, watts: number) => ({
        ...interval,
        lineId,
        forecastCurrentA: [amps, 1, 1, amps],
        envelopeCurrentA: [2 * amps, 1, 1, 2 * amps],
        staticCurrentA: [3 * amps, 1, 1, 3 * amps],
        forecastPowerW: watts,
        envelopePowerW: -watts,
        staticPowerW: -2 * watts,
      });
      // On the large network the voltage rises with the distance from the
      // transformer, and a line carries the load of everything beyond it.
      const big = mock.network === "large" ? network() : undefined;
      return {
        at: iso(Date.now()),
        nodes: big
          ? big.nodes.map((n, i) => {
              const rise = (0.05 * big.depth[i]!) / big.deepest;
              return node(n.id, 1.03 + rise, 1.04 + rise, 1.05 + 1.4 * rise);
            })
          : [
              node(FEEDER, 1.04, 1.05, 1.06),
              node(NODES.mid, 1.05, 1.08, 1.11),
              node(NODES.far, 1.06, 1.095, 1.13),
            ],
        lines: big
          ? big.lines.map((l, k) => line(l.id, 0.6 * big.beyond[k + 1]!, 400 * big.beyond[k + 1]!))
          : [line(LINES.main, 30, 6000), line(LINES.far, 14, 3000)],
        vMinPu: 0.94,
        vMaxPu: 1.1,
        lineLimitPct: 100,
        transformerLimitPct: 100,
      };
    },
    "SubstationService/ListSubstations": () => ({ substations: SUBSTATIONS }),
    "SiteService/ListLocatedSites": () => ({ sites: located() }),
    "TelemetryService/GetFleetState": () => ({
      at: iso(Date.now()),
      feeders: [summary()],
      sites:
        mock.fleet === "large"
          ? located().map((site, i) => ({
              siteId: site.id,
              envelope: envelope(site, start),
              // One in nine is silent, and one in seven over its limit.
              reporting: i % 9 !== 0,
              ...(i % 9 !== 0 ? { netExportW: i % 7 === 0 ? 2740 : 300 + 20 * (i % 40) } : {}),
              ...(i % 9 !== 0 && i % 7 === 0 ? { overLimit: true } : {}),
            }))
          : smallFleet(),
    }),
    "SiteService/ListSites": () => ({ sites: SITES }),
    "SiteService/GetSite": (req) => {
      const site = SITES.find((s) => s.nmi === req.nmi || s.id === req.id);
      if (!site) throw new RpcError("not_found", `site ${req.nmi} not found`, 404);
      return { site };
    },
    "DeviceService/ListDevices": (req) => ({
      devices:
        req.siteId === SITES[2]!.id
          ? []
          : [
              {
                id: "0199c0de-0000-7000-8000-0000000000d1",
                siteId: req.siteId,
                derType: "DER_TYPE_SOLAR",
                ratedW: 5000,
              },
            ],
    }),
    "EnvelopeService/GetCurrentEnvelope": (req) => {
      const site = SITES.find((s) => s.id === req.siteId || s.nmi === req.nmi)!;
      return site.exportCapW ? { envelope: envelope(site, start) } : {};
    },
    "EnvelopeService/ListEnvelopes": (req) => {
      const site = SITES.find((s) => s.id === req.siteId)!;
      if (!site.exportCapW) return { envelopes: [] };
      return {
        envelopes: Array.from({ length: 49 }, (_, i) =>
          envelope(site, start + (i - 24) * HALF_HOUR),
        ),
      };
    },
    "TelemetryService/GetFleetSummary": () => ({ summary: summary() }),
    "TelemetryService/GetFeederSeries": () => ({
      vMinPu: 0.94,
      vMaxPu: 1.1,
      transformerKva: 500,
      staticLimitW: 5000,
      points: Array.from({ length: 49 }, (_, i) => {
        const from = start + (i - 24) * HALF_HOUR;
        const sun = Math.max(0, Math.sin(((i % 48) / 48) * Math.PI));
        return {
          validFrom: iso(from),
          validTo: iso(from + HALF_HOUR),
          forecastNetLoadW: 60_000 - 180_000 * sun,
          forecastLoadingPct: 12 + 20 * sun,
          forecastVMinPu: 1.01,
          forecastVMaxPu: 1.05 + 0.06 * sun,
          exportLimitTotalW: 9000 - 5000 * sun,
          importLimitTotalW: 14_000,
          staticLimitTotalW: 10_000,
          staticVMaxPu: 1.09 + 0.05 * sun,
          staticBinding: sun > 0.2 ? "BINDING_CONSTRAINT_VOLTAGE_HIGH" : "BINDING_CONSTRAINT_NONE",
          staticBindingElement: sun > 0.2 ? "XDLAB000022" : "",
          envelopeVMaxPu: 1.04 + 0.055 * sun,
          ...(i <= 24 ? { measuredExportW: 3000 * sun, measuredImportW: 400 } : {}),
        };
      }),
    }),
    "TelemetryService/GetSiteSeries": () => ({
      power: Array.from({ length: 240 }, (_, i) => ({
        bucket: iso(start - (240 - i) * 60_000),
        avgNetExportW: 900 + 600 * Math.sin(i / 30),
        maxNetExportW: 1700,
      })),
      forecast: Array.from({ length: 49 }, (_, i) => ({
        ts: iso(start + (i - 24) * HALF_HOUR),
        loadW: 500,
        pvW: 500 + 60 * (i % 24),
      })),
    }),
    "TelemetryService/GetDailyReport": () => ({
      from: iso(start - 12 * 3600_000),
      to: iso(start + 12 * 3600_000),
      enrolledSites: 2,
      intervals: 48,
      potentialExportKwh: 41.8,
      envelopeExportKwh: 35.4,
      envelopeCurtailedKwh: 6.4,
      staticExportKwh: 40.7,
      staticCurtailedKwh: 1.1,
      staticViolationIntervals: 19,
      staticLimitW: 5000,
      constraintBreaches: 1,
      deviceOfflineAlerts: 1,
    }),
    "AlertService/ListAlerts": (req) => ({
      alerts: alerts.filter(
        (a) =>
          (!req.openOnly || !a.resolvedAt) &&
          (!req.siteId || a.siteId === req.siteId) &&
          (!req.kind || a.kind === req.kind),
      ),
    }),
    "AlertService/AcknowledgeAlert": (req, token) => {
      operatorOnly(token);
      const alert = alerts.find((a) => a.id === req.id)!;
      Object.assign(alert, { acknowledgedAt: iso(Date.now()), acknowledgedBy: "operator" });
      return { alert };
    },
    "EnvelopeRunService/ListEnvelopeRuns": () => ({
      envelopeRuns: [0, 1, 2].map((i) => ({
        id: `0199c0de-0000-7000-8000-0000000000f${i + 1}`,
        feederId: FEEDER,
        envelopeConfigId: configs[0]!.id,
        status: i === 2 ? "RUN_STATUS_FAILED" : "RUN_STATUS_COMPLETED",
        horizonFrom: iso(start - i * 2 * HALF_HOUR),
        horizonTo: iso(start + 86400_000),
        startedAt: iso(Date.now() - 40_000 - i * 3600_000),
        durationMs: 290 + i,
        siteCount: 2,
        intervalCount: 48,
        envelopeCount: i === 2 ? 0 : 96,
        engineVersion: "e2e",
        ...(i === 2
          ? {
              error:
                "publish batch 1 of 1: failed_precondition: site XDLAB000014 is under a backstop",
            }
          : {}),
      })),
    }),
    "EnvelopeRunService/ExportEnvelopeRun": (req, token) => {
      operatorOnly(token);
      return {
        url: `https://objects.example/exports/runs/LV10/${req.id}.csv?signature=e2e`,
        expiresAt: iso(Date.now() + 900_000),
        rows: 96,
        objectKey: `exports/runs/LV10/${req.id}.csv`,
      };
    },
    "BackstopService/ListBackstopEvents": () => ({ backstopEvents: [...backstops].reverse() }),
    "BackstopService/CreateBackstopEvent": (req, token) => {
      operatorOnly(token);
      backstop = {
        id: `0199c0de-0000-7000-8000-00000000b${String(backstops.length).padStart(3, "0")}`,
        feederId: FEEDER,
        reason: req.backstopEvent.reason,
        exportLimitW: req.backstopEvent.exportLimitW ?? 0,
        triggeredBy: "operator",
        triggeredAt: iso(Date.now()),
      };
      backstops.push(backstop);
      return { backstopEvent: backstop, siteIds: [SITES[0]!.id, SITES[1]!.id] };
    },
    "BackstopService/ClearBackstop": (_req, token) => {
      operatorOnly(token);
      Object.assign(backstop!, { clearedBy: "operator", clearedAt: iso(Date.now()) });
      const cleared = backstop;
      backstop = undefined;
      return { backstopEvent: cleared };
    },
    "AssistantService/GetAssistantStatus": () =>
      mock.assistant === "available"
        ? { available: true, maxQuestionChars: 500 }
        : { unavailable: "ASSISTANT_UNAVAILABLE_OFF", maxQuestionChars: 500 },
    "EnvelopeConfigService/ListEnvelopeConfigs": () => ({
      envelopeConfigs: [...configs].reverse(),
    }),
    "EnvelopeConfigService/CreateEnvelopeConfig": (req, token) => {
      operatorOnly(token);
      const saved = {
        ...req.envelopeConfig,
        id: `0199c0de-0000-7000-8000-0000000000c${configs.length + 1}`,
        version: configs.length + 1,
        createdBy: "operator",
        createdAt: iso(Date.now()),
      };
      configs.push(saved);
      return { envelopeConfig: saved };
    },
  };

  // The street tiles of the map: one transparent pixel each, so that the
  // suite asks no other host for anything.
  await page.route("https://tile.openstreetmap.org/**", (route) => {
    mock.tiles++;
    return route.fulfill({
      status: 200,
      contentType: "image/gif",
      body: Buffer.from("R0lGODlhAQABAAAAACH5BAEKAAEALAAAAAABAAEAAAICTAEAOw==", "base64"),
    });
  });

  // One handler per page: a test that asks for the mock again gets the first.
  await page.unroute("**/rpc/doelab.v1.*/*");
  await page.route("**/rpc/doelab.v1.*/*", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const procedure = url.pathname.replace("/rpc/doelab.v1.", "");
    mock.calls[procedure] = (mock.calls[procedure] ?? 0) + 1;
    if (mock.delayMs > 0) await new Promise((r) => setTimeout(r, mock.delayMs));
    if (procedure === "ClockService/GetClock" && mock.clockDelayMs > 0)
      await new Promise((r) => setTimeout(r, mock.clockDelayMs));

    // The one server stream the pages use: a summary, then a clean end. The
    // page reopens it after a moment, which is how the figures stay live.
    if (procedure === "TelemetryService/WatchFleet") {
      if (mock.streamsDown) return route.abort("connectionrefused");
      return route.fulfill({
        status: 200,
        contentType: "application/connect+json",
        body: Buffer.concat([frame(0, { summary: summary() }), frame(2, {})]),
      });
    }

    // An answer: two lookups, the text in two pieces, and the end. The
    // question arrives as one frame of a stream.
    if (procedure === "AssistantService/Ask") {
      const question = JSON.parse(request.postDataBuffer()!.subarray(5).toString("utf8"));
      mock.asked.push(question);
      const site = question.nmi ?? NMI.enrolled;
      return route.fulfill({
        status: 200,
        contentType: "application/connect+json",
        body: Buffer.concat([
          frame(0, {
            lookup: {
              tool: "ASSISTANT_TOOL_GET_BINDING_CONSTRAINT",
              subject: `${site} at 12:30 on 10 Nov`,
              found: true,
            },
          }),
          frame(0, {
            lookup: { tool: "ASSISTANT_TOOL_GET_CONFIG", subject: "config version 1", found: true },
          }),
          frame(0, { text: `${site} is limited to 1.5 kW by voltage ` }),
          frame(0, { text: "at XDLAB000022, which would pass 253 V." }),
          frame(0, { end: "ANSWER_END_COMPLETE" }),
          frame(2, {}),
        ]),
      });
    }

    const handler = unary[procedure];
    if (!handler)
      return fail(route, new RpcError("unimplemented", `the mock has no ${procedure}`, 404));
    try {
      const message =
        request.method() === "GET" ? fromQuery(url) : JSON.parse(request.postData() || "{}");
      if (request.method() !== "GET") mock.writes.push(procedure);
      const token = request.headers()["authorization"] ?? null;
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(handler(message, token)),
      });
    } catch (e) {
      if (e instanceof RpcError) return fail(route, e);
      throw e;
    }
  });
  return mock;
}

class RpcError extends Error {
  readonly code: string;
  readonly status: number;
  constructor(code: string, message: string, status: number) {
    super(message);
    this.code = code;
    this.status = status;
  }
}

function operatorOnly(token: string | null) {
  if (token !== `Bearer ${OPERATOR_TOKEN}`) {
    throw new RpcError("unauthenticated", "valid credentials are required", 401);
  }
}

function fail(route: Route, e: RpcError) {
  return route.fulfill({
    status: e.status,
    contentType: "application/json",
    body: JSON.stringify({ code: e.code, message: e.message }),
  });
}

// A Connect GET carries its message in the query, as JSON or as base64 of it.
function fromQuery(url: URL): unknown {
  const message = url.searchParams.get("message") ?? "{}";
  if (url.searchParams.get("base64") === "1") {
    return JSON.parse(
      Buffer.from(message.replace(/-/g, "+").replace(/_/g, "/"), "base64").toString("utf8"),
    );
  }
  return JSON.parse(message);
}

// One frame of a Connect stream: a flag byte, a length, and the JSON.
function frame(flags: number, message: unknown): Buffer {
  const body = Buffer.from(JSON.stringify(message), "utf8");
  const head = Buffer.alloc(5);
  head.writeUInt8(flags, 0);
  head.writeUInt32BE(body.length, 1);
  return Buffer.concat([head, body]);
}
