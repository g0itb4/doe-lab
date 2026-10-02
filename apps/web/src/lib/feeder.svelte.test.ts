import { Code, ConnectError, createRouterTransport, type Transport } from "@connectrpc/connect";
import { FeederService } from "@doelab/gen/doelab/v1/feeder_pb.js";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { chooseFeeder, feeder, feeders } from "./feeder.svelte.ts";

let router: Transport;
vi.mock("@connectrpc/connect-web", async () => {
  const { transportStub } = await import("$lib/transport.test-utils.ts");
  return transportStub(() => router);
});

const all = [
  { id: "f-1", code: "LV10", timezone: "Australia/Sydney" },
  { id: "f-2", code: "SUB-007-LV1", timezone: "Australia/Melbourne" },
];
// What the API was asked: "list 1", "list 500", "get CODE".
let asked: string[] = [];
function api(list = all) {
  asked = [];
  router = createRouterTransport(({ service }) => {
    service(FeederService, {
      listFeeders: (req) => {
        asked.push(`list ${req.pageSize}`);
        return { feeders: list.slice(0, req.pageSize) };
      },
      getFeeder: (req) => {
        asked.push(`get ${req.key.value}`);
        const found = list.find((f) => f.code === req.key.value);
        if (!found) throw new ConnectError("no such feeder", Code.NotFound);
        return { feeder: found };
      },
    });
  });
}

beforeEach(() => localStorage.clear());

describe("the feeder", () => {
  it("fails in plain words when the API has none", async () => {
    api([]);
    await feeder.load();
    expect(feeder.data).toBeUndefined();
    expect(feeder.error).toBeDefined();
  });

  it("is the first feeder of the API until one is chosen, loaded once", async () => {
    api();
    chooseFeeder(null);
    chooseFeeder(null); // while the first is in flight
    await vi.waitFor(() => expect(feeder.data?.code).toBe("LV10"));
    chooseFeeder(null); // and after
    expect(asked).toEqual(["list 1"]);
  });

  it("is the one the address names, which is remembered when the address names none", async () => {
    api();
    chooseFeeder("SUB-007-LV1");
    await vi.waitFor(() => expect(feeder.data?.code).toBe("SUB-007-LV1"));
    expect(asked).toEqual(["get SUB-007-LV1"]);
    expect(localStorage.getItem("doelab.feeder")).toBe("SUB-007-LV1");
    // Another page, with no feeder in its address: the same feeder, and no
    // new request.
    chooseFeeder(null);
    expect(asked).toEqual(["get SUB-007-LV1"]);
    chooseFeeder("LV10");
    await vi.waitFor(() => expect(feeder.data?.code).toBe("LV10"));
    expect(asked).toEqual(["get SUB-007-LV1", "get LV10"]);
  });

  it("falls back to the first when the one chosen has gone, and forgets it", async () => {
    api();
    chooseFeeder("LV99");
    await vi.waitFor(() => expect(asked).toEqual(["get LV99", "list 1"]));
    await vi.waitFor(() => expect(feeder.data?.code).toBe("LV10"));
    expect(localStorage.getItem("doelab.feeder")).toBeNull();
  });

  it("works where storage is blocked: the choice holds for the visit", async () => {
    api();
    const blocked = () => {
      throw new Error("blocked");
    };
    const get = vi.spyOn(Storage.prototype, "getItem").mockImplementation(blocked);
    const set = vi.spyOn(Storage.prototype, "setItem").mockImplementation(blocked);
    const remove = vi.spyOn(Storage.prototype, "removeItem").mockImplementation(blocked);
    try {
      chooseFeeder("SUB-007-LV1");
      await vi.waitFor(() => expect(feeder.data?.code).toBe("SUB-007-LV1"));
      chooseFeeder("LV98");
      await vi.waitFor(() => expect(feeder.data?.code).toBe("LV10"));
    } finally {
      get.mockRestore();
      set.mockRestore();
      remove.mockRestore();
    }
  });

  it("lists every feeder for the picker", async () => {
    api();
    await feeders.load();
    expect(feeders.data?.map((f) => f.code)).toEqual(["LV10", "SUB-007-LV1"]);
    expect(asked).toEqual(["list 500"]);
  });
});
