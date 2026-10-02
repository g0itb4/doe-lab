import type { Feeder } from "@doelab/gen/doelab/v1/feeder_pb.js";
import { api } from "./api.ts";
import { Resource } from "./resource.svelte.ts";

const KEY = "doelab.feeder";

function stored(): string | undefined {
  try {
    return localStorage.getItem(KEY) ?? undefined;
  } catch {
    // Storage can be blocked; the first feeder is then the one shown.
    return undefined;
  }
}

function remember(code: string | undefined): void {
  try {
    if (code === undefined) localStorage.removeItem(KEY);
    else localStorage.setItem(KEY, code);
  } catch {
    // The choice holds for this visit.
  }
}

// The code of the feeder that was asked for; unset for the first one.
let wanted: string | undefined;
let chosen = false;

// The feeder the app shows. Every page needs it, so it is loaded once and
// shared. It is the one that was chosen, and the first of the API when none
// was, or when the one that was chosen is no longer there.
export const feeder = new Resource<Feeder>(async (signal) => {
  if (wanted !== undefined) {
    try {
      const res = await api.feeders.getFeeder({ key: { case: "code", value: wanted } }, { signal });
      if (res.feeder) return res.feeder;
    } catch (e) {
      if (signal.aborted) throw e;
    }
    // A feeder that has gone, as after a new import: forget it.
    wanted = undefined;
    remember(undefined);
  }
  const res = await api.feeders.listFeeders({ pageSize: 1 }, { signal });
  const first = res.feeders[0];
  if (!first) throw new Error("no feeder");
  return first;
});

// Every feeder, for the picker in the header.
export const feeders = new Resource<Feeder[]>(async (signal) => {
  const res = await api.feeders.listFeeders({ pageSize: 500 }, { signal });
  return res.feeders;
});

// Shows the feeder that the address names with `?feeder=`, and remembers it;
// with none in the address, the one remembered, or else the first. It loads
// only when the answer changes.
export function chooseFeeder(fromAddress: string | null): void {
  if (fromAddress) remember(fromAddress);
  const code = fromAddress ?? stored();
  if (chosen && code === wanted) return;
  chosen = true;
  wanted = code;
  void feeder.load();
}
