import type { FeederLine, FeederNode } from "@doelab/gen/doelab/v1/feeder_pb.js";
import type { Site } from "@doelab/gen/doelab/v1/site_pb.js";
import { api } from "../api.ts";

// A feeder's network model: every bus, every line and every site.
export type Topology = { nodes: FeederNode[]; lines: FeederLine[]; sites: Site[] };

// Reads every page of a list.
async function all<T>(page: (token: string) => Promise<{ rows: T[]; next: string }>): Promise<T[]> {
  const out: T[] = [];
  for (let token = ""; ;) {
    const { rows, next } = await page(token);
    out.push(...rows);
    if (!next) return out;
    token = next;
  }
}

export async function loadTopology(feederId: string, signal: AbortSignal): Promise<Topology> {
  const pageSize = 500;
  const [nodes, lines, sites] = await Promise.all([
    all(async (pageToken) => {
      const res = await api.feeders.listFeederNodes({ feederId, pageSize, pageToken }, { signal });
      return { rows: res.feederNodes, next: res.nextPageToken };
    }),
    all(async (pageToken) => {
      const res = await api.feeders.listFeederLines({ feederId, pageSize, pageToken }, { signal });
      return { rows: res.feederLines, next: res.nextPageToken };
    }),
    all(async (pageToken) => {
      const res = await api.sites.listSites({ feederId, pageSize, pageToken }, { signal });
      return { rows: res.sites, next: res.nextPageToken };
    }),
  ]);
  return { nodes, lines, sites };
}
