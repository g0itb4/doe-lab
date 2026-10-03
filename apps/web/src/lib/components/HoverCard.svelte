<script lang="ts">
  import { hovercard } from "$lib/hovercard.svelte.ts";
  import StatusBadge from "./StatusBadge.svelte";

  // The one card of the app: what the mark under the pointer says. It is an
  // aid for the eye: every mark also has its words where a screen reader
  // finds them, so the card itself is hidden from one. It takes no pointer,
  // and it does not move in: it is where the pointer is.
  const card = $derived(hovercard.card);
  const at = $derived(card ? hovercard.placement : undefined);
  const px = (value: number | undefined) => (value === undefined ? undefined : `${value}px`);
</script>

{#if card && at}
  <div
    class="card-over raised"
    aria-hidden="true"
    style:left={px(at.left)}
    style:right={px(at.right)}
    style:top={px(at.top)}
    style:bottom={px(at.bottom)}
    style:max-width="min(20rem, {at.maxWidth}px)"
  >
    <p class="text-body font-semibold">{card.title}</p>
    {#if card.level}
      <p class="mt-1"><StatusBadge level={card.level} label={card.lines[0] ?? ""} /></p>
    {/if}
    {#each card.level ? card.lines.slice(1) : card.lines as line (line)}
      <p class="text-label mt-1">{line}</p>
    {/each}
  </div>
{/if}

<style>
  .card-over {
    position: fixed;
    z-index: 60;
    width: max-content;
    padding: 0.5rem 0.625rem;
    pointer-events: none;
  }
</style>
