import { describeError, isAbort } from "./errors.ts";

// One piece of data fetched from the API, with every state a view has to
// draw: loading (nothing yet), ready, and failed. A reload keeps the data it
// has while it fetches, and after a failure: old data marked as old is more
// use to an operator than a spinner.
//
// A reload that brings back what is already here changes nothing: with
// `same`, the data on screen stays the object it was, and nothing that was
// worked out from it is worked out again.
export type ResourceOptions<T> = {
  // True when a new answer says what the one on screen says.
  same?: (shown: T, next: T) => boolean;
};

export class Resource<T> {
  #data = $state<T>();
  #error = $state<string>();
  #loading = $state(false);
  #fetch: (signal: AbortSignal) => Promise<T>;
  #inFlight: AbortController | undefined;
  #same: ResourceOptions<T>["same"];

  constructor(fetch: (signal: AbortSignal) => Promise<T>, options: ResourceOptions<T> = {}) {
    this.#fetch = fetch;
    this.#same = options.same;
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
      if (this.#data === undefined || !this.#same?.(this.#data, data)) this.#data = data;
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
