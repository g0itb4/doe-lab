import { describeError, isAbort } from "./errors.ts";

// One piece of data fetched from the API, with every state a view has to
// draw: loading (nothing yet), ready, and failed. A reload keeps the data it
// has while it fetches, and after a failure: old data marked as old is more
// use to an operator than a spinner.
export class Resource<T> {
  #data = $state<T>();
  #error = $state<string>();
  #loading = $state(false);
  #fetch: (signal: AbortSignal) => Promise<T>;
  #inFlight: AbortController | undefined;

  constructor(fetch: (signal: AbortSignal) => Promise<T>) {
    this.#fetch = fetch;
  }

  get data(): T | undefined {
    return this.#data;
  }
  // What went wrong with the latest load, in plain words; unset when it
  // worked.
  get error(): string | undefined {
    return this.#error;
  }
  get loading(): boolean {
    return this.#loading;
  }
  // True while there is nothing to show yet.
  get pending(): boolean {
    return this.#data === undefined && this.#error === undefined;
  }
  // True when the data on screen is older than the latest attempt to load.
  get stale(): boolean {
    return this.#data !== undefined && this.#error !== undefined;
  }

  // Loads, replacing a load that is still in flight.
  async load(): Promise<void> {
    this.#inFlight?.abort();
    const controller = new AbortController();
    this.#inFlight = controller;
    this.#loading = true;
    try {
      const data = await this.#fetch(controller.signal);
      if (controller.signal.aborted) return;
      this.#data = data;
      this.#error = undefined;
    } catch (e) {
      if (controller.signal.aborted || isAbort(e)) return;
      this.#error = describeError(e);
    } finally {
      if (this.#inFlight === controller) this.#loading = false;
    }
  }

  // Abandons a load in flight: the page that asked has gone.
  cancel(): void {
    this.#inFlight?.abort();
    this.#inFlight = undefined;
    this.#loading = false;
  }
}
