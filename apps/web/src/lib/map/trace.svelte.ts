import { SvelteMap } from "svelte/reactivity";
import { api } from "../api.ts";
import { clock } from "../clock.svelte.ts";
import { timestamp } from "../time.ts";

// What each site has exported over the last six hours of feeder time, for
// the small plot beside a site: on the card under the pointer, and in the
// panel of the site that is chosen. A plot is asked for when it is wanted and
// kept for a minute, so a pointer that crosses the map does not ask the API
// for every mark it passes.
export const TRACE_SECONDS = 6 * 3600;
const KEEP_MS = 60_000;
// How long a pointer rests on a mark before its plot is asked for.
const SETTLE_MS = 150;

class Traces {
  #kept = new SvelteMap<string, { at: number; values: number[] }>();
  #asking = new Set<string>();
  #pending: ReturnType<typeof setTimeout> | undefined;

  // The plot of a site, once it has arrived: watts, by the minute.
  of(siteId: string): number[] | undefined {
    return this.#kept.get(siteId)?.values;
  }

  // Asks for the plot of a site, unless a fresh one is kept: after a moment,
  // or at once for a site that was chosen and not merely passed over.
  want(siteId: string, settleMs = SETTLE_MS): void {
    clearTimeout(this.#pending);
    const kept = this.#kept.get(siteId);
    if (kept && Date.now() - kept.at < KEEP_MS) return;
    if (settleMs <= 0) void this.#load(siteId);
    else this.#pending = setTimeout(() => void this.#load(siteId), settleMs);
  }

  // The pointer has gone before it settled: nothing is asked for.
  rest(): void {
    clearTimeout(this.#pending);
  }

  async #load(siteId: string): Promise<void> {
    if (this.#asking.has(siteId)) return;
    this.#asking.add(siteId);
    try {
      const now = clock.nowSeconds();
      const res = await api.telemetry.getSiteSeries({
        siteId,
        from: timestamp(now - TRACE_SECONDS),
        to: timestamp(now),
      });
      this.#kept.set(siteId, { at: Date.now(), values: res.power.map((p) => p.avgNetExportW) });
    } catch {
      // A card with no plot is still a card: the next look asks again.
    } finally {
      this.#asking.delete(siteId);
    }
  }
}

export const traces = new Traces();
