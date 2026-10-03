import { Code, ConnectError, createRouterTransport, type Transport } from "@connectrpc/connect";
import { TelemetryService } from "@doelab/gen/doelab/v1/telemetry_pb.js";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { seconds } from "../time.ts";
import { TRACE_SECONDS, traces } from "./trace.svelte.ts";

let router: Transport;
vi.mock("@connectrpc/connect-web", async () => {
  const { transportStub } = await import("$lib/transport.test-utils.ts");
  return transportStub(() => router);
});

// What the API was asked, and whether it answers.
let asked: { siteId: string; span: number }[] = [];
let down = false;
beforeEach(() => {
  asked = [];
  down = false;
  router = createRouterTransport(({ service }) => {
    service(TelemetryService, {
      getSiteSeries: (req) => {
        asked.push({ siteId: req.siteId, span: seconds(req.to) - seconds(req.from) });
        if (down) throw new ConnectError("down", Code.Unavailable);
        return { power: [{ avgNetExportW: 900 }, { avgNetExportW: 1500 }] };
      },
    });
  });
  vi.useFakeTimers();
});
afterEach(() => vi.useRealTimers());

describe("the plot of a site's last six hours", () => {
  it("is asked for once the pointer has rested on the site, and then is there", async () => {
    expect(traces.of("s-1")).toBeUndefined();
    traces.want("s-1");
    expect(asked).toEqual([]);
    await vi.advanceTimersByTimeAsync(150);
    await vi.waitFor(() => expect(traces.of("s-1")).toEqual([900, 1500]));
    expect(asked).toEqual([{ siteId: "s-1", span: TRACE_SECONDS }]);
  });

  it("is not asked for when the pointer only passes over", async () => {
    traces.want("s-2");
    traces.rest();
    traces.want("s-3");
    traces.want("s-4");
    await vi.advanceTimersByTimeAsync(1000);
    // Only the mark the pointer stayed on.
    await vi.waitFor(() => expect(traces.of("s-4")).toBeDefined());
    expect(asked.map((a) => a.siteId)).toEqual(["s-4"]);
    expect(traces.of("s-2")).toBeUndefined();
  });

  it("is kept for a minute, and then asked for again", async () => {
    traces.want("s-5", 0);
    await vi.waitFor(() => expect(traces.of("s-5")).toBeDefined());
    traces.want("s-5", 0);
    await vi.advanceTimersByTimeAsync(59_000);
    traces.want("s-5", 0);
    expect(asked).toHaveLength(1);
    await vi.advanceTimersByTimeAsync(2000);
    traces.want("s-5", 0);
    await vi.waitFor(() => expect(asked).toHaveLength(2));
  });

  it("is asked for at once for a site that was chosen, and once while the answer is on its way", async () => {
    traces.want("s-6", 0);
    traces.want("s-6", 0);
    await vi.waitFor(() => expect(traces.of("s-6")).toBeDefined());
    expect(asked.map((a) => a.siteId)).toEqual(["s-6"]);
  });

  it("is simply not there when the API fails, and is asked for again on the next look", async () => {
    down = true;
    traces.want("s-7", 0);
    await vi.waitFor(() => expect(asked).toHaveLength(1));
    await vi.advanceTimersByTimeAsync(10);
    expect(traces.of("s-7")).toBeUndefined();
    down = false;
    traces.want("s-7", 0);
    await vi.waitFor(() => expect(traces.of("s-7")).toEqual([900, 1500]));
  });
});
