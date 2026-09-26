export type LiveOptions = {
  // How long without a message before the stream counts as stale.
  staleAfterMs?: number;
  // The first wait before a reconnect, and the longest. It doubles between.
  retryMs?: number;
  maxRetryMs?: number;
  // The clock and the frame scheduler, replaceable in tests.
  sleep?: (ms: number, signal: AbortSignal) => Promise<void>;
  frame?: (run: () => void) => void;
};

const sleepFor = (ms: number, signal: AbortSignal) =>
  new Promise<void>((resolve) => {
    const timer = setTimeout(resolve, ms);
    signal.addEventListener("abort", () => {
      clearTimeout(timer);
      resolve();
    });
  });

// A server stream that stays open: it reconnects with backoff when it drops,
// and says so while it is down. The last value stays on screen, marked as
// stale; it is not replaced by a spinner.
//
// Messages are applied once per animation frame, the latest winning, so a
// burst from the server costs one render.
export class Live<T> {
  #value = $state<T>();
  #stale = $state(false);
  #open: (signal: AbortSignal) => AsyncIterable<T>;
  #options: Required<LiveOptions>;
  #controller: AbortController | undefined;
  #pending: { value: T } | undefined;
  #lastAt = 0;
  #watchdog: ReturnType<typeof setInterval> | undefined;

  constructor(open: (signal: AbortSignal) => AsyncIterable<T>, options: LiveOptions = {}) {
    this.#open = open;
    this.#options = {
      staleAfterMs: 6000,
      retryMs: 1000,
      maxRetryMs: 15000,
      sleep: sleepFor,
      frame: (run) => requestAnimationFrame(run),
      ...options,
    };
  }

  get value(): T | undefined {
    return this.#value;
  }
  // True while the stream is down, or silent for too long.
  get stale(): boolean {
    return this.#stale;
  }

  // Opens the stream and keeps it open. Returns the function that stops it.
  start(): () => void {
    this.stop();
    const controller = new AbortController();
    this.#controller = controller;
    this.#lastAt = Date.now();
    this.#watchdog = setInterval(() => {
      if (Date.now() - this.#lastAt > this.#options.staleAfterMs) this.#stale = true;
    }, 1000);
    void this.#run(controller.signal);
    return () => this.stop();
  }

  stop(): void {
    this.#controller?.abort();
    this.#controller = undefined;
    clearInterval(this.#watchdog);
  }

  async #run(signal: AbortSignal): Promise<void> {
    let wait = this.#options.retryMs;
    while (!signal.aborted) {
      let heard = false;
      try {
        for await (const message of this.#open(signal)) {
          heard = true;
          wait = this.#options.retryMs;
          this.#lastAt = Date.now();
          this.#apply(message);
        }
      } catch {
        // Whatever broke it, the answer is the same: say so, and reconnect.
      }
      if (signal.aborted) return;
      // A stream that ended after it had spoken is reopened at once: a proxy
      // may close a long response, and that is not an outage.
      if (!heard) {
        this.#stale = true;
        await this.#options.sleep(wait, signal);
        wait = Math.min(wait * 2, this.#options.maxRetryMs);
      }
    }
  }

  #apply(message: T): void {
    const first = this.#pending === undefined;
    this.#pending = { value: message };
    if (!first) return;
    this.#options.frame(() => {
      const pending = this.#pending;
      this.#pending = undefined;
      if (pending) {
        this.#value = pending.value;
        this.#stale = false;
      }
    });
  }
}
