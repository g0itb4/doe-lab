import { untrack } from "svelte";
import { clock } from "./clock.svelte.ts";

// The shortest wait between two polls, whatever the speed of feeder time.
export const POLL_FLOOR_MS = 5000;

// Calls `run` again and again, a stretch of feeder time apart and never
// closer than the floor. Each wait is worked out when it starts, from the
// speed feeder time runs at then, so a clock that arrives late still sets the
// pace. A round is skipped while the tab is hidden. Returns what stops it.
//
// The speed is read without subscribing: an effect that starts a poll does
// not start over when the speed changes.
export function poll(run: () => void, feederMs: number, floorMs = POLL_FLOOR_MS): () => void {
  let timer: ReturnType<typeof setTimeout>;
  const wait = () => Math.max(floorMs, feederMs / untrack(() => clock.speed));
  const round = () => {
    if (document.visibilityState !== "hidden") run();
    timer = setTimeout(round, wait());
  };
  timer = setTimeout(round, wait());
  return () => clearTimeout(timer);
}
