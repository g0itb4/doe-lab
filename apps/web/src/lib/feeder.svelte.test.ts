import { createRouterTransport, type Transport } from "@connectrpc/connect";
import { FeederService } from "@doelab/gen/doelab/v1/feeder_pb.js";
import { describe, expect, it, vi } from "vitest";
import { ensureFeeder, feeder } from "./feeder.svelte.ts";

let router: Transport;
vi.mock("@connectrpc/connect-web", async () => {
  const { transportStub } = await import("$lib/transport.test-utils.ts");
  return transportStub(() => router);
});

describe("the feeder", () => {
  it("fails in plain words when the API has none", async () => {
    router = createRouterTransport(({ service }) => {
      service(FeederService, { listFeeders: () => ({ feeders: [] }) });
    });
    await feeder.load();
    expect(feeder.data).toBeUndefined();
    expect(feeder.error).toBeDefined();
  });

  it("is the first feeder of the API, loaded once", async () => {
    let asked = 0;
    router = createRouterTransport(({ service }) => {
      service(FeederService, {
        listFeeders: (req) => {
          asked++;
          expect(req.pageSize).toBe(1);
          return { feeders: [{ id: "f-1", code: "LV10", timezone: "Australia/Sydney" }] };
        },
      });
    });
    ensureFeeder();
    ensureFeeder(); // while the first is in flight
    await vi.waitFor(() => expect(feeder.data?.code).toBe("LV10"));
    ensureFeeder(); // and after
    expect(asked).toBe(1);
  });
});
