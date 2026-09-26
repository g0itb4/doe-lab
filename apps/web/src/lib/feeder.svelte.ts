import type { Feeder } from "@doelab/gen/doelab/v1/feeder_pb.js";
import { api } from "./api.ts";
import { Resource } from "./resource.svelte.ts";

// The feeder the app shows: the demo has one. Every page needs it, so it is
// loaded once and shared.
export const feeder = new Resource<Feeder>(async (signal) => {
  const res = await api.feeders.listFeeders({ pageSize: 1 }, { signal });
  const first = res.feeders[0];
  if (!first) throw new Error("no feeder");
  return first;
});

export function ensureFeeder(): void {
  if (feeder.data === undefined && !feeder.loading) void feeder.load();
}
