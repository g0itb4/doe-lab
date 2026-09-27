<script lang="ts">
  import { Code, ConnectError } from "@connectrpc/connect";
  import type { BackstopEvent } from "@doelab/gen/doelab/v1/backstop_pb.js";
  import { api, bearer } from "$lib/api.ts";
  import { describeError } from "$lib/errors.ts";
  import { dayAndTime, kw } from "$lib/format.ts";
  import { operator } from "$lib/operator.svelte.ts";
  import { date } from "$lib/time.ts";
  import { toasts } from "$lib/toast.svelte.ts";
  import Icon from "./Icon.svelte";
  import TokenField from "./TokenField.svelte";

  let {
    feederId,
    feederCode,
    sites,
    active,
    zone,
    onchange,
  }: {
    feederId: string;
    feederCode: string;
    // How many sites a backstop covers: the enrolled sites of the feeder.
    sites: number;
    // The backstop in force, if there is one.
    active: BackstopEvent | undefined;
    zone: string;
    // Called after a backstop was triggered or cleared.
    onchange: () => void;
  } = $props();

  let reason = $state("");
  let limitKw = $state(0);
  let confirm = $state("");
  let busy = $state(false);
  let tokenError = $state("");
  let formError = $state("");

  const limitW = $derived(Number.isFinite(limitKw) && limitKw >= 0 ? limitKw * 1000 : 0);
  const impact = $derived(
    `Set export to ${kw(limitW)} for ${sites} ${sites === 1 ? "site" : "sites"}`,
  );
  // Every part of the decision is on the page before the button works.
  const ready = $derived(
    !busy &&
      operator.token !== "" &&
      reason.trim() !== "" &&
      confirm.trim().toUpperCase() === feederCode.toUpperCase(),
  );

  async function run(action: () => Promise<unknown>, done: string) {
    // The flag is the guard: a second click finds the button disabled, and a
    // second call finds this.
    if (busy) return;
    busy = true;
    tokenError = "";
    formError = "";
    try {
      await action();
      toasts.show("ok", done);
      reason = "";
      confirm = "";
      onchange();
    } catch (e) {
      const code = ConnectError.from(e).code;
      const message = describeError(e);
      // The token's fault goes beside the token; anything else beside the button.
      if (code === Code.PermissionDenied || code === Code.Unauthenticated) tokenError = message;
      else formError = message;
      toasts.show("error", message);
    } finally {
      busy = false;
    }
  }

  const trigger = () =>
    run(
      () =>
        api.backstops.createBackstopEvent(
          { backstopEvent: { feederId, reason: reason.trim(), exportLimitW: limitW } },
          bearer(operator.token),
        ),
      `Backstop triggered: ${impact.toLowerCase()}.`,
    );
  const clear = () =>
    run(
      () => api.backstops.clearBackstop({ id: active?.id ?? "" }, bearer(operator.token)),
      "Backstop cleared. The engine's envelopes are back in force.",
    );
</script>

<section aria-labelledby="backstop-heading" class="card p-3 {active ? 'border-critical' : ''}">
  <h2 id="backstop-heading" class="font-semibold">Backstop</h2>

  {#if active}
    <p class="mt-1 flex items-start gap-2 text-sm">
      <span class="text-critical mt-0.5"><Icon name="critical" /></span>
      <span>
        <strong>A backstop is active.</strong> Export is held at {kw(active.exportLimitW)} for the sites
        it covers, since {dayAndTime(date(active.triggeredAt), zone)}. Reason: {active.reason}. The
        engine's runs are refused until it is cleared.
      </span>
    </p>
    <!-- Not a submit: Enter in the token field does nothing. -->
    <form
      class="mt-3 grid gap-3 sm:grid-cols-[minmax(0,20rem)_auto] sm:items-start"
      onsubmit={(e) => e.preventDefault()}
    >
      <TokenField id="backstop-token" error={tokenError} />
      <div class="sm:pt-6">
        <button
          type="button"
          class="btn btn-primary"
          disabled={busy || operator.token === ""}
          onclick={clear}
        >
          {busy ? "Clearing…" : "Clear the backstop"}
        </button>
      </div>
    </form>
  {:else}
    <p class="text-muted mt-1 text-sm">
      An emergency override: it replaces every envelope of the feeder with one fixed export limit
      until you clear it. Use it when the network is at risk and the engine's limits cannot be
      trusted.
    </p>
    <form class="mt-3 grid gap-3 sm:grid-cols-2" onsubmit={(e) => e.preventDefault()}>
      <TokenField id="backstop-token" error={tokenError} />
      <div>
        <label for="backstop-limit" class="mb-1 block text-sm font-medium"
          >Export limit while active</label
        >
        <div class="flex items-center gap-2">
          <input
            id="backstop-limit"
            type="number"
            class="field"
            min="0"
            max="100"
            step="0.5"
            inputmode="decimal"
            bind:value={limitKw}
          />
          <span class="text-sm">kW</span>
        </div>
      </div>
      <div class="sm:col-span-2">
        <label for="backstop-reason" class="mb-1 block text-sm font-medium">Reason</label>
        <input
          id="backstop-reason"
          type="text"
          class="field"
          maxlength="500"
          autocomplete="off"
          aria-describedby="backstop-reason-help"
          bind:value={reason}
        />
        <p id="backstop-reason-help" class="text-muted mt-1 text-xs">
          Recorded with the event, for whoever reads the log later.
        </p>
      </div>
      <div class="border-warn rounded-md border px-3 py-2 sm:col-span-2">
        <p class="flex items-center gap-2 text-sm font-semibold">
          <span class="text-warn"><Icon name="warn" /></span>
          This will: {impact}.
        </p>
        <label for="backstop-confirm" class="mt-2 mb-1 block text-sm">
          Type the feeder's code, <strong>{feederCode}</strong>, to confirm
        </label>
        <input
          id="backstop-confirm"
          type="text"
          class="field sm:max-w-40"
          autocomplete="off"
          autocapitalize="characters"
          spellcheck="false"
          bind:value={confirm}
        />
      </div>
      <div class="flex flex-wrap items-center gap-3 sm:col-span-2">
        <!-- Not a submit: Enter in a field never triggers a backstop. -->
        <button type="button" class="btn btn-danger" disabled={!ready} onclick={trigger}>
          {busy ? "Triggering…" : "Trigger the backstop"}
        </button>
        {#if formError}
          <p class="text-critical text-sm" role="alert">{formError}</p>
        {:else if !ready && !busy}
          <p class="text-muted text-sm">Needs the token, a reason and the confirmation.</p>
        {/if}
      </div>
    </form>
  {/if}
</section>
