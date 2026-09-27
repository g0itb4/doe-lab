<script lang="ts">
  import { Code, ConnectError } from "@connectrpc/connect";
  import { EnvelopePolicy } from "@doelab/gen/doelab/v1/common_pb.js";
  import type { EnvelopeConfig } from "@doelab/gen/doelab/v1/envelope_config_pb.js";
  import { api, bearer } from "$lib/api.ts";
  import {
    diffForms,
    fieldSpecs,
    formErrors,
    fromForm,
    POLICY_WORDS,
    RULES,
    toForm,
    type ConfigForm,
  } from "$lib/config-rules.ts";
  import { describeError } from "$lib/errors.ts";
  import { operator } from "$lib/operator.svelte.ts";
  import { toasts } from "$lib/toast.svelte.ts";
  import TokenField from "./TokenField.svelte";

  let {
    active,
    nominalV,
    dirty = $bindable(false),
    onsaved,
  }: {
    // The version in force: the form starts from it.
    active: EnvelopeConfig;
    nominalV: number;
    // True while the form differs from the version in force.
    dirty?: boolean;
    onsaved: (saved: EnvelopeConfig | undefined) => void;
  } = $props();

  // svelte-ignore state_referenced_locally
  let values = $state<ConfigForm>(toForm(active, nominalV));
  // A field's error shows once the reader has left the field, or tried to save.
  let touched = $state<Partial<Record<keyof ConfigForm, boolean>>>({});
  let busy = $state(false);
  let tokenError = $state("");
  let formError = $state("");
  let form = $state<HTMLFormElement>();

  // A new version in force (after a save, or someone else's): start again.
  // svelte-ignore state_referenced_locally
  let shownId = active.id;
  $effect(() => {
    if (active.id === shownId) return;
    shownId = active.id;
    values = toForm(active, nominalV);
    touched = {};
  });

  const specs = $derived(fieldSpecs(nominalV));
  const errors = $derived(formErrors(values, nominalV));
  const changes = $derived(diffForms(toForm(active, nominalV), values, nominalV));
  const shown = (key: keyof ConfigForm) => (touched[key] ? (errors[key] ?? "") : "");
  $effect(() => {
    dirty = changes.length > 0 || values.note.trim() !== "";
  });

  async function save(event: SubmitEvent) {
    event.preventDefault();
    if (busy) return;
    tokenError = "";
    formError = "";
    // Every error shows now, and the first field that has one gets the focus.
    for (const key of Object.keys(values) as (keyof ConfigForm)[]) touched[key] = true;
    if (Object.keys(errors).length > 0) {
      form?.querySelector<HTMLElement>('[aria-invalid="true"]')?.focus();
      // The attribute is set by the render that follows; ask again after it.
      queueMicrotask(() => form?.querySelector<HTMLElement>('[aria-invalid="true"]')?.focus());
      return;
    }
    if (operator.token === "") {
      tokenError = "Enter the operator token to save.";
      return;
    }
    busy = true;
    try {
      const res = await api.configs.createEnvelopeConfig(
        { envelopeConfig: fromForm(values, active.feederId, nominalV) },
        bearer(operator.token),
      );
      toasts.show(
        "ok",
        `Saved as version ${res.envelopeConfig?.version ?? ""}. The next engine run uses it.`,
      );
      onsaved(res.envelopeConfig);
    } catch (e) {
      const code = ConnectError.from(e).code;
      const message = describeError(e);
      if (code === Code.PermissionDenied || code === Code.Unauthenticated) tokenError = message;
      else formError = message;
      toasts.show("error", message);
    } finally {
      busy = false;
    }
  }

  function reset() {
    values = toForm(active, nominalV);
    touched = {};
    formError = "";
  }
</script>

<form bind:this={form} class="space-y-4" novalidate onsubmit={save}>
  <div class="grid gap-x-4 gap-y-3 sm:grid-cols-2 lg:grid-cols-3">
    <div class="sm:col-span-2 lg:col-span-3">
      <label for="config-policy" class="mb-1 block text-sm font-medium"
        >How headroom is shared</label
      >
      <select
        id="config-policy"
        class="field sm:max-w-md"
        value={values.policy}
        onchange={(e) => (values.policy = Number(e.currentTarget.value) as EnvelopePolicy)}
      >
        {#each [EnvelopePolicy.EQUAL, EnvelopePolicy.PROPORTIONAL] as policy (policy)}
          <option value={policy}>{POLICY_WORDS[policy]}</option>
        {/each}
      </select>
    </div>

    {#each specs as spec (spec.key)}
      {@const error = shown(spec.key)}
      <div>
        <label for="config-{spec.key}" class="mb-1 block text-sm font-medium">{spec.label}</label>
        <div class="flex items-center gap-2">
          <input
            id="config-{spec.key}"
            type="number"
            class="field"
            step={spec.step}
            inputmode={spec.whole ? "numeric" : "decimal"}
            aria-invalid={error ? "true" : undefined}
            aria-describedby="config-{spec.key}-help"
            value={Number.isFinite(values[spec.key]) ? values[spec.key] : ""}
            oninput={(e) => (values[spec.key] = e.currentTarget.valueAsNumber)}
            onblur={() => (touched[spec.key] = true)}
          />
          <span class="w-16 shrink-0 text-sm">{spec.unit}</span>
        </div>
        <!-- One place for the hint and the error: the error replaces the hint,
             and says what to enter. -->
        <p
          id="config-{spec.key}-help"
          class="mt-1 text-xs {error ? 'text-critical' : 'text-muted'}"
        >
          {error || spec.help}
        </p>
      </div>
    {/each}

    <div>
      <label for="config-interval" class="mb-1 block text-sm font-medium">Interval</label>
      <div class="flex items-center gap-2">
        <select
          id="config-interval"
          class="field"
          value={values.intervalMinutes}
          onchange={(e) => (values.intervalMinutes = Number(e.currentTarget.value))}
        >
          {#each RULES.interval_minutes.in as minutes (minutes)}
            <option value={minutes}>{minutes}</option>
          {/each}
        </select>
        <span class="w-16 shrink-0 text-sm">min</span>
      </div>
      <p class="text-muted mt-1 text-xs">The length of one envelope.</p>
    </div>

    <div class="sm:col-span-2 lg:col-span-3">
      <label for="config-note" class="mb-1 block text-sm font-medium">Note for this version</label>
      <textarea
        id="config-note"
        class="field"
        rows="2"
        aria-invalid={shown("note") ? "true" : undefined}
        aria-describedby="config-note-help"
        bind:value={values.note}
        onblur={() => (touched.note = true)}></textarea>
      <p
        id="config-note-help"
        class="mt-1 text-xs {shown('note') ? 'text-critical' : 'text-muted'}"
      >
        {shown("note") || "Why the change: it is shown in the version history."}
      </p>
    </div>
  </div>

  <!-- The preview: what a save would change, before it is saved. -->
  <section
    aria-labelledby="config-changes"
    aria-live="polite"
    class="border-control rounded-md border px-3 py-2"
  >
    <h3 id="config-changes" class="text-sm font-semibold">
      {changes.length === 0
        ? "No changes yet"
        : `${changes.length} ${changes.length === 1 ? "change" : "changes"} from version ${active.version}`}
    </h3>
    {#if changes.length === 0}
      <p class="text-muted text-sm">Edit a field to see what a new version would change.</p>
    {:else}
      <ul class="mt-1 space-y-1 text-sm">
        {#each changes as change (change.label)}
          <li>
            <span class="font-medium">{change.label}:</span>
            from <del class="text-muted">{change.from}</del> to
            <ins class="font-semibold no-underline">{change.to}</ins>
          </li>
        {/each}
      </ul>
    {/if}
  </section>

  <div class="grid gap-3 sm:grid-cols-[minmax(0,20rem)_auto] sm:items-start">
    <TokenField id="config-token" error={tokenError} />
    <div class="flex flex-wrap items-center gap-3 sm:pt-6">
      <button type="submit" class="btn btn-primary" disabled={busy || changes.length === 0}>
        {busy ? "Saving…" : `Save as version ${active.version + 1}`}
      </button>
      <button type="button" class="btn" disabled={busy || !dirty} onclick={reset}
        >Discard changes</button
      >
      {#if formError}
        <p class="text-critical text-sm" role="alert">{formError}</p>
      {/if}
    </div>
  </div>
</form>
