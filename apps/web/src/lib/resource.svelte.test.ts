import { Code, ConnectError } from "@connectrpc/connect";
import { describe, expect, it } from "vitest";
import { Resource } from "./resource.svelte.ts";

// A fetch that a test settles by hand.
function deferred<T>() {
  const calls: { resolve: (v: T) => void; reject: (e: unknown) => void; signal: AbortSignal }[] =
    [];
  const fetch = (signal: AbortSignal) =>
    new Promise<T>((resolve, reject) => {
      calls.push({ resolve, reject, signal });
    });
  return { calls, fetch };
}

describe("a resource", () => {
  it("starts with nothing, then has its data", async () => {
    const { calls, fetch } = deferred<string>();
    const r = new Resource(fetch);
    expect(r.pending).toBe(true);
    expect(r.loading).toBe(false);

    const done = r.load();
    expect(r.loading).toBe(true);
    calls[0]!.resolve("first");
    await done;
    expect(r).toMatchObject({
      data: "first",
      pending: false,
      loading: false,
      stale: false,
      error: undefined,
    });
  });

  it("says what failed, in plain words", async () => {
    const r = new Resource<string>(() =>
      Promise.reject(new ConnectError("down", Code.Unavailable)),
    );
    await r.load();
    expect(r.error).toContain("The API cannot be reached");
    expect(r.pending).toBe(false);
    expect(r.stale).toBe(false);
    expect(r.loading).toBe(false);
  });

  it("keeps its data when a reload fails, and marks it stale", async () => {
    let fail = false;
    const r = new Resource<string>(() =>
      fail ? Promise.reject(new Error("boom")) : Promise.resolve("kept"),
    );
    await r.load();
    fail = true;
    await r.load();
    expect(r).toMatchObject({ data: "kept", stale: true });
    expect(r.error).toBeDefined();

    // And recovers.
    fail = false;
    await r.load();
    expect(r).toMatchObject({ data: "kept", stale: false, error: undefined });
  });

  it("lets a newer load replace one in flight", async () => {
    const { calls, fetch } = deferred<string>();
    const r = new Resource(fetch);
    const first = r.load();
    const second = r.load();
    expect(calls[0]!.signal.aborted).toBe(true);

    // The older answer arrives late, and is dropped.
    calls[0]!.resolve("old");
    await first;
    expect(r.data).toBeUndefined();
    expect(r.loading).toBe(true);

    calls[1]!.resolve("new");
    await second;
    expect(r).toMatchObject({ data: "new", loading: false });
  });

  it("keeps the object it has when a reload brings the same thing back", async () => {
    let version = 1;
    const r = new Resource(() => Promise.resolve({ version, at: Math.random() }), {
      same: (shown, next) => shown.version === next.version,
    });
    await r.load();
    const first = r.data;
    await r.load();
    expect(r.data).toBe(first);

    // Something new replaces it.
    version = 2;
    await r.load();
    expect(r.data).not.toBe(first);
    expect(r.data?.version).toBe(2);
  });

  it("clears a failure when a reload brings the same thing back", async () => {
    let fail = false;
    const r = new Resource(() => (fail ? Promise.reject(new Error("boom")) : Promise.resolve(1)), {
      same: (shown, next) => shown === next,
    });
    await r.load();
    fail = true;
    await r.load();
    expect(r.stale).toBe(true);
    fail = false;
    await r.load();
    expect(r).toMatchObject({ data: 1, stale: false, error: undefined });
  });

  it("drops the failure of a load that was replaced or cancelled", async () => {
    const { calls, fetch } = deferred<string>();
    const r = new Resource(fetch);
    const first = r.load();
    r.cancel();
    expect(r.loading).toBe(false);
    calls[0]!.reject(new Error("late"));
    await first;
    expect(r.error).toBeUndefined();

    // A call that the transport reports as cancelled is not a failure either.
    const second = r.load();
    calls[1]!.reject(new ConnectError("gone", Code.Canceled));
    await second;
    expect(r.error).toBeUndefined();
  });
});
