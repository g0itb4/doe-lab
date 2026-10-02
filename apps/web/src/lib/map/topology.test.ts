import { createRouterTransport, type Transport } from "@connectrpc/connect";
import { FeederService } from "@doelab/gen/doelab/v1/feeder_pb.js";
import { SiteService } from "@doelab/gen/doelab/v1/site_pb.js";
import { describe, expect, it, vi } from "vitest";
import { loadTopology } from "./topology.ts";

let router: Transport;
vi.mock("@connectrpc/connect-web", async () => {
  const { transportStub } = await import("$lib/transport.test-utils.ts");
  return transportStub(() => router);
});

describe("the topology of a feeder", () => {
  it("is every bus, line and site, read page by page", async () => {
    const asked: string[] = [];
    router = createRouterTransport(({ service }) => {
      service(FeederService, {
        // Two pages of buses.
        listFeederNodes: (req) => {
          asked.push(`nodes ${req.feederId} ${req.pageSize} "${req.pageToken}"`);
          return req.pageToken === ""
            ? { feederNodes: [{ id: "tx", name: "B1" }], nextPageToken: "more" }
            : { feederNodes: [{ id: "mid", name: "B2", parentNodeId: "tx" }] };
        },
        listFeederLines: (req) => {
          asked.push(`lines ${req.feederId}`);
          return { feederLines: [{ id: "l-1", name: "main", fromNodeId: "tx", toNodeId: "mid" }] };
        },
      });
      service(SiteService, {
        listSites: (req) => {
          asked.push(`sites ${req.feederId}`);
          return { sites: [{ id: "s-1", nmi: "NMI00000017", nodeId: "mid" }] };
        },
      });
    });
    const got = await loadTopology("f-1", new AbortController().signal);
    expect(got.nodes.map((n) => n.name)).toEqual(["B1", "B2"]);
    expect(got.lines.map((l) => l.name)).toEqual(["main"]);
    expect(got.sites.map((s) => s.nmi)).toEqual(["NMI00000017"]);
    expect(asked.sort()).toEqual([
      "lines f-1",
      'nodes f-1 500 ""',
      'nodes f-1 500 "more"',
      "sites f-1",
    ]);
  });
});
