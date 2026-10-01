import type { Page, Route } from "@playwright/test";

// A stand-in for the API, in the browser: every request to /rpc is answered
// here, in the JSON that the Connect protocol puts on the wire. It holds a
// little state, so that a backstop that was triggered is then active and a
// config that was saved is then in force.
//
// Feeder time is the wall clock here (speed 1), so the fixtures are built
// around now.

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
};

export async function mockApi(page: Page): Promise<Mock> {
  const mock: Mock = {
    writes: [],
    streamsDown: false,
    delayMs: 0,
    assistant: "available",
    asked: [],
  };
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

  const envelope = (site: (typeof SITES)[number], from: number) => ({
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
    sitesOverLimit: 0,
    openAlerts: alerts.filter((a) => !a.resolvedAt).length,
    backstopEventId: backstop?.id,
    latestRunId: "0199c0de-0000-7000-8000-0000000000f1",
    latestRunAt: iso(Date.now() - 40_000),
    latestRunStatus: "RUN_STATUS_COMPLETED",
  });

  const unary: Record<string, (req: Message, token: string | null) => unknown> = {
    "ClockService/GetClock": () => ({
      now: iso(Date.now()),
      anchor: iso(Date.now()),
      speed: 1,
      wallNow: iso(Date.now()),
    }),
    "FeederService/ListFeeders": () => ({
      feeders: [
        {
          id: FEEDER,
          code: "LV10",
          name: "lv10_223bus",
          nominalVoltageV: 230,
          transformerKva: 500,
          timezone: "Australia/Sydney",
        },
      ],
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

  // One handler per page: a test that asks for the mock again gets the first.
  await page.unroute("**/rpc/doelab.v1.*/*");
  await page.route("**/rpc/doelab.v1.*/*", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const procedure = url.pathname.replace("/rpc/doelab.v1.", "");
    if (mock.delayMs > 0) await new Promise((r) => setTimeout(r, mock.delayMs));

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
