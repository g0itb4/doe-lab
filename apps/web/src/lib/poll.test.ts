import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { poll } from "./poll.ts";

let speed = 1;
vi.mock("./clock.svelte.ts", () => ({
  clock: {
    get speed() {
      return speed;
    },
  },
}));

function visibility(state: "visible" | "hidden") {
  Object.defineProperty(document, "visibilityState", { value: state, configurable: true });
}

beforeEach(() => {
  vi.useFakeTimers();
  speed = 1;
});
afterEach(() => {
  vi.useRealTimers();
  visibility("visible");
});

describe("a poll", () => {
  it("runs a stretch of feeder time apart, and stops when told to", () => {
    const run = vi.fn();
    const stop = poll(run, 60_000);
    vi.advanceTimersByTime(59_999);
    expect(run).not.toHaveBeenCalled();
    vi.advanceTimersByTime(1);
    expect(run).toHaveBeenCalledTimes(1);
    vi.advanceTimersByTime(60_000);
    expect(run).toHaveBeenCalledTimes(2);

    stop();
    vi.advanceTimersByTime(600_000);
    expect(run).toHaveBeenCalledTimes(2);
  });

  it("is never closer than its floor, however fast feeder time runs", () => {
    speed = 60;
    const run = vi.fn();
    const stop = poll(run, 60_000);
    // A minute of feeder time is a second here: the floor holds it to five.
    vi.advanceTimersByTime(4999);
    expect(run).not.toHaveBeenCalled();
    vi.advanceTimersByTime(1);
    expect(run).toHaveBeenCalledTimes(1);
    stop();

    const quick = vi.fn();
    const stopQuick = poll(quick, 60_000, 250);
    vi.advanceTimersByTime(1000);
    expect(quick).toHaveBeenCalledTimes(1);
    stopQuick();
  });

  it("takes its pace from the speed at each round, so a clock that arrives late still counts", () => {
    const run = vi.fn();
    const stop = poll(run, 300_000);
    // The first wait was set at speed 1: five minutes.
    speed = 60;
    vi.advanceTimersByTime(300_000);
    expect(run).toHaveBeenCalledTimes(1);
    // The next is five minutes of feeder time: five seconds.
    vi.advanceTimersByTime(5000);
    expect(run).toHaveBeenCalledTimes(2);
    stop();
  });

  it("skips a round while the tab is hidden, and goes on when it shows again", () => {
    const run = vi.fn();
    const stop = poll(run, 10_000);
    visibility("hidden");
    vi.advanceTimersByTime(20_000);
    expect(run).not.toHaveBeenCalled();
    visibility("visible");
    vi.advanceTimersByTime(10_000);
    expect(run).toHaveBeenCalledTimes(1);
    stop();
  });
});
