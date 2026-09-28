import { afterEach, describe, expect, it, vi } from "vitest";
import { Live } from "./live.svelte.ts";

// A stream that a test feeds by hand.
class Feed<T> {
  opened = 0;
  #queue: (IteratorResult<T> | Error)[] = [];
  #waiting: ((r: IteratorResult<T> | Error) => void) | undefined;

  open = (signal: AbortSignal): AsyncIterable<T> => {
    this.opened++;
    signal.addEventListener("abort", () => this.#push(new Error("aborted")));
    const next = async (): Promise<IteratorResult<T>> => {
      const item =
        this.#queue.shift() ??
        (await new Promise<IteratorResult<T> | Error>((r) => (this.#waiting = r)));
      if (item instanceof Error) throw item;
      return item;
    };
    return { [Symbol.asyncIterator]: () => ({ next }) };
  };
  send(value: T) {
    this.#push({ value, done: false });
  }
  end() {
    this.#push({ value: undefined, done: true });
  }
  fail() {
    this.#push(new Error("network"));
  }
  #push(item: IteratorResult<T> | Error) {
    const waiting = this.#waiting;
    this.#waiting = undefined;
    if (waiting) waiting(item);
    else this.#queue.push(item);
  }
}

const settle = () => new Promise((r) => setTimeout(r, 0));

function harness() {
  const feed = new Feed<number>();
  const sleeps: number[] = [];
  let wake: (() => void) | undefined;
  const frames: (() => void)[] = [];
  const live = new Live<number>(feed.open, {
    retryMs: 100,
    maxRetryMs: 350,
    reopenMs: 7,
    sleep: (ms) => {
      sleeps.push(ms);
      return new Promise<void>((r) => (wake = r));
    },
    frame: (run) => frames.push(run),
  });
  const frame = () => frames.splice(0).forEach((run) => run());
  return { feed, live, sleeps, frame, wake: () => wake?.() };
}

let stop: (() => void) | undefined;
afterEach(() => {
  stop?.();
  vi.useRealTimers();
});

describe("a live stream", () => {
  it("applies the latest message once per frame", async () => {
    const h = harness();
    stop = h.live.start();
    h.feed.send(1);
    h.feed.send(2);
    h.feed.send(3);
    await settle();
    // Nothing until the frame, then the latest only.
    expect(h.live.value).toBeUndefined();
    h.frame();
    expect(h.live.value).toBe(3);
    expect(h.live.stale).toBe(false);
  });

  it("reopens a stream that ended after it had spoken, after a moment and without going stale", async () => {
    const h = harness();
    stop = h.live.start();
    h.feed.send(1);
    await settle();
    h.frame();
    h.feed.end();
    await settle();
    // The short pause, not the backoff.
    expect(h.sleeps).toEqual([7]);
    expect(h.feed.opened).toBe(1);
    h.wake();
    await settle();
    expect(h.feed.opened).toBe(2);
    expect(h.live.stale).toBe(false);
    expect(h.live.value).toBe(1);
  });

  it("goes stale and backs off when the stream fails, and keeps its value", async () => {
    const h = harness();
    stop = h.live.start();
    h.feed.send(7);
    await settle();
    h.frame();

    // The stream breaks: reopened after the short pause. The reconnect then
    // fails three times.
    h.feed.fail();
    await settle();
    expect(h.sleeps).toEqual([7]);
    expect(h.live.stale).toBe(false);
    h.wake();
    await settle();
    for (const want of [100, 200, 350]) {
      h.feed.fail();
      await settle();
      expect(h.sleeps.at(-1)).toBe(want);
      expect(h.live.stale).toBe(true);
      expect(h.live.value).toBe(7);
      h.wake();
      await settle();
    }
    // 350 is the ceiling.
    h.feed.fail();
    await settle();
    expect(h.sleeps.at(-1)).toBe(350);
    h.wake();
    await settle();

    // It comes back: fresh again, and the backoff starts over.
    h.feed.send(8);
    await settle();
    h.frame();
    expect(h.live).toMatchObject({ value: 8, stale: false });
    h.feed.fail();
    await settle();
    h.wake();
    await settle();
    h.feed.fail();
    await settle();
    expect(h.sleeps.at(-1)).toBe(100);
  });

  it("goes stale when the stream is open but silent", async () => {
    vi.useFakeTimers();
    const feed = new Feed<number>();
    const live = new Live<number>(feed.open, { staleAfterMs: 3000, frame: (run) => run() });
    stop = live.start();
    feed.send(1);
    await vi.advanceTimersByTimeAsync(2500);
    expect(live.stale).toBe(false);
    await vi.advanceTimersByTimeAsync(2000);
    expect(live.stale).toBe(true);
    // The next message clears it.
    feed.send(2);
    await vi.advanceTimersByTimeAsync(1);
    expect(live).toMatchObject({ value: 2, stale: false });
  });

  it("stops: no reconnect, and a pending frame changes nothing more", async () => {
    const h = harness();
    stop = h.live.start();
    h.feed.send(1);
    await settle();
    h.live.stop();
    await settle();
    expect(h.feed.opened).toBe(1);
    expect(h.sleeps).toEqual([]);
  });

  it("restarts cleanly: a second start replaces the first", async () => {
    const h = harness();
    h.live.start();
    stop = h.live.start();
    await settle();
    expect(h.feed.opened).toBe(2);
    h.feed.send(5);
    await settle();
    h.frame();
    expect(h.live.value).toBe(5);
  });

  it("waits with real timers by default, and wakes when stopped", async () => {
    const feed = new Feed<number>();
    const live = new Live<number>(feed.open, { retryMs: 20 });
    stop = live.start();
    feed.fail();
    await new Promise((r) => setTimeout(r, 60));
    expect(feed.opened).toBeGreaterThanOrEqual(2);
    feed.send(4);
    await new Promise((r) => requestAnimationFrame(() => r(undefined)));
    await settle();
    expect(live.value).toBe(4);

    // Stopped in the middle of a wait.
    const slow = new Live<number>(feed.open, { retryMs: 60_000 });
    const opened = feed.opened;
    const stopSlow = slow.start();
    feed.fail();
    await settle();
    stopSlow();
    await settle();
    expect(feed.opened).toBe(opened + 1);
  });
});
