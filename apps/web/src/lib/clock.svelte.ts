import { api } from "./api.ts";
import { seconds } from "./time.ts";

// Feeder time: the clock of the simulation, which the API owns. The demo
// runs it faster than the wall clock, so the page asks once how it runs and
// keeps time itself from there.
class FeederClock {
  #anchorMs = $state<number>();
  #speed = $state(1);
  #tick = $state(Date.now());
  #timer: ReturnType<typeof setInterval> | undefined;

  // Asks the API how feeder time runs. Safe to call again: it asks once.
  async start(): Promise<void> {
    if (this.#timer !== undefined) return;
    this.#timer = setInterval(() => (this.#tick = Date.now()), 1000);
    try {
      const res = await api.clock.getClock({});
      this.#anchorMs = seconds(res.anchor) * 1000;
      this.#speed = res.speed;
    } catch {
      // Until the API answers, feeder time is taken to be the wall clock.
      clearInterval(this.#timer);
      this.#timer = undefined;
    }
  }

  get ready(): boolean {
    return this.#anchorMs !== undefined;
  }
  get speed(): number {
    return this.#speed;
  }
  // Feeder time now. Reading it in a template re-renders every second.
  get now(): Date {
    return new Date(this.at(this.#tick));
  }
  // Feeder time now, in unix seconds, without subscribing to the tick.
  nowSeconds(): number {
    return this.at(Date.now()) / 1000;
  }
  // The feeder time, in ms, of a wall-clock time in ms.
  at(wallMs: number): number {
    if (this.#anchorMs === undefined) return wallMs;
    return this.#anchorMs + this.#speed * (wallMs - this.#anchorMs);
  }
}

export const clock = new FeederClock();
