<script lang="ts">
  import { assistant, SPENT } from "$lib/assistant.svelte.ts";
  import Icon from "./Icon.svelte";

  let {
    open = $bindable(false),
    feederCode,
    nmi,
  }: {
    open: boolean;
    feederCode: string;
    // The site on screen, so that "this site" means something.
    nmi?: string;
  } = $props();

  let dialog = $state<HTMLDialogElement>();
  let field = $state<HTMLTextAreaElement>();
  let question = $state("");

  // A modal dialog: the browser holds the focus inside it, closes it on
  // Escape, and gives the focus back to the button that opened it.
  $effect(() => {
    if (!dialog) return;
    if (open && !dialog.open) dialog.showModal();
    else if (!open && dialog.open) dialog.close();
  });

  const suggestions = $derived([
    ...(nmi ? [`Why is ${nmi} limited now?`, `What may ${nmi} export at midday today?`] : []),
    "Which sites are over their limit now?",
    "How is capacity shared between the sites?",
  ]);
  const available = $derived(assistant.availability === "available");
  const left = $derived(assistant.maxChars - question.length);

  function ask(text: string) {
    const q = text.trim();
    if (q === "" || assistant.asking || !available) return;
    question = "";
    // The button that was pressed may be about to go; the field stays.
    field?.focus();
    void assistant.ask(feederCode, q, nmi);
  }

  function onkeydown(e: KeyboardEvent) {
    if (e.key !== "Enter" || e.shiftKey || e.isComposing) return;
    e.preventDefault();
    ask(question);
  }
</script>

<!-- A click on the dialog itself is a click on the backdrop: the panel
     inside covers the rest. Escape is the keyboard's way out. -->
<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_noninteractive_element_interactions -->
<dialog
  bind:this={dialog}
  aria-labelledby="assistant-heading"
  class="bg-surface text-text border-rule backdrop:bg-scrim m-0 ml-auto h-dvh max-h-none w-full max-w-md border-l p-0"
  onclose={() => {
    open = false;
    assistant.stop();
  }}
  onclick={(e) => {
    if (e.target === dialog) dialog.close();
  }}
>
  <div class="flex h-full flex-col">
    <header class="border-rule flex items-center gap-2 border-b px-4 py-3">
      <h2 id="assistant-heading" class="flex-1 text-base font-semibold">Ask about {feederCode}</h2>
      <button
        type="button"
        class="text-muted hover:bg-sunken grid size-9 cursor-pointer place-items-center rounded"
        aria-label="Close"
        onclick={() => dialog?.close()}
      >
        <Icon name="close" />
      </button>
    </header>

    <div class="min-h-0 flex-1 overflow-y-auto px-4 py-3">
      <p class="text-muted text-sm">
        A language model writes the answer from this feeder's data. It can read and nothing else.
        Check a figure before you act on it.
      </p>

      {#if assistant.answer}
        {@const answer = assistant.answer}
        <!-- Announced once, when the answer is whole: not piece by piece. -->
        <section aria-label="Answer" aria-live="polite" aria-busy={assistant.asking} class="mt-4">
          <p class="text-sm font-semibold">{answer.question}</p>
          {#if answer.lookups.length > 0}
            <ul class="text-muted mt-2 space-y-1 text-xs" aria-label="Looked up">
              {#each answer.lookups as lookup, i (i)}
                <li class="flex items-start gap-1.5">
                  <span class="mt-px"><Icon name={lookup.found ? "ok" : "info"} size={14} /></span>
                  {lookup.label}
                </li>
              {/each}
            </ul>
          {/if}
          {#if answer.text !== ""}
            <p class="mt-2 text-sm whitespace-pre-wrap">{answer.text}</p>
          {:else if assistant.asking}
            <p class="text-muted animate-pulse-soft mt-2 text-sm">Looking things up…</p>
          {/if}
          {#if answer.note}
            <p class="text-muted mt-2 flex items-start gap-1.5 text-sm">
              <span class="mt-0.5"><Icon name="info" /></span>
              {answer.note}
            </p>
          {/if}
          {#if answer.error}
            <div role="alert" class="border-critical mt-2 rounded-md border px-3 py-2 text-sm">
              <p class="flex items-start gap-1.5">
                <span class="text-critical mt-0.5"><Icon name="critical" /></span>
                {answer.error}
              </p>
              {#if available}
                <button type="button" class="btn mt-2" onclick={() => ask(answer.question)}>
                  <Icon name="retry" /> Ask again
                </button>
              {/if}
            </div>
          {/if}
        </section>
      {:else}
        <div class="mt-4">
          <p class="text-sm font-medium">Questions to start from</p>
          <ul class="mt-2 space-y-2">
            {#each suggestions as suggestion (suggestion)}
              <li>
                <button
                  type="button"
                  class="btn w-full justify-start text-left"
                  disabled={!available}
                  onclick={() => ask(suggestion)}
                >
                  {suggestion}
                </button>
              </li>
            {/each}
          </ul>
        </div>
      {/if}
    </div>

    <form
      class="border-rule border-t px-4 py-3"
      onsubmit={(e) => {
        e.preventDefault();
        ask(question);
      }}
    >
      {#if assistant.availability === "spent"}
        <p
          role="status"
          class="border-warn mb-3 flex items-start gap-1.5 rounded-md border px-3 py-2 text-sm"
        >
          <span class="text-warn mt-0.5"><Icon name="warn" /></span>
          {SPENT}
        </p>
      {/if}
      <label for="assistant-question" class="mb-1 block text-sm font-medium">Your question</label>
      <!-- svelte-ignore a11y_autofocus -->
      <textarea
        id="assistant-question"
        bind:this={field}
        bind:value={question}
        class="field"
        rows="3"
        maxlength={assistant.maxChars}
        aria-describedby="assistant-help"
        disabled={!available}
        autofocus
        {onkeydown}></textarea>
      <div class="mt-2 flex items-center gap-3">
        {#if assistant.asking}
          <button type="button" class="btn" onclick={() => assistant.stop()}>
            <Icon name="stop" /> Stop
          </button>
        {:else}
          <button
            type="submit"
            class="btn btn-primary"
            disabled={!available || question.trim() === ""}
          >
            Ask
          </button>
        {/if}
        <p id="assistant-help" class="text-muted text-xs">
          Enter to ask, Shift+Enter for a new line. <span class="tabular">{left}</span> characters left.
        </p>
      </div>
    </form>
  </div>
</dialog>
