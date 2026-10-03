import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError, createRouterTransport, type Transport } from "@connectrpc/connect";
import { ClockService } from "@doelab/gen/doelab/v1/clock_pb.js";
import { describe, expect, it, vi } from "vitest";
import { bearer } from "./api.ts";
import { clock } from "./clock.svelte.ts";

let router: Transport;
vi.mock("@connectrpc/connect-web", async () => {
  const { transportStub } = await import("$lib/transport.test-utils.ts");
  return transportStub(() => router);
});

const anchor = new Date("2026-10-01T00:00:00Z");

// One file, in order: the clock is one object for the whole app.
describe("feeder time", () => {
  it("is the wall clock until the API has answered", async () => {
    router = createRouterTransport(({ service }) => {
      service(ClockService, {
        getClock: () => {
          throw new ConnectError("down", Code.Unavailable);
        },
      });
    });
    expect(clock.settled).toBe(false);
    await clock.start();
    expect(clock.ready).toBe(false);
    // It has its answer all the same: a page need not wait any longer.
    expect(clock.settled).toBe(true);
    expect(clock.at(1234)).toBe(1234);
    expect(Math.abs(clock.now.getTime() - Date.now())).toBeLessThan(2000);
  });

  it("runs at the API's speed from the API's anchor", async () => {
    let asked = 0;
    router = createRouterTransport(({ service }) => {
      service(ClockService, {
        getClock: () => {
          asked++;
          return { anchor: timestampFromDate(anchor), speed: 60 };
        },
      });
    });
    await clock.start();
    expect(clock.ready).toBe(true);
    expect(clock.settled).toBe(true);
    expect(clock.speed).toBe(60);
    // Ten seconds on the wall are ten minutes on the feeder.
    expect(clock.at(anchor.getTime() + 10_000)).toBe(anchor.getTime() + 600_000);
    expect(clock.nowSeconds()).toBeCloseTo(clock.at(Date.now()) / 1000, 0);
    expect(Math.abs(clock.now.getTime() - clock.at(Date.now()))).toBeLessThan(120_000);

    // It asks once.
    await clock.start();
    expect(asked).toBe(1);
  });
});

describe("the operator's token", () => {
  it("goes out as a bearer header", () => {
    expect(bearer("s3cret")).toEqual({ headers: { Authorization: "Bearer s3cret" } });
  });
});
