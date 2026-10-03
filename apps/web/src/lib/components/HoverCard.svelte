<script lang="ts">
  import { hovercard } from "$lib/hovercard.svelte.ts";
  import Sparkline from "./Sparkline.svelte";
  import StatusBadge from "./StatusBadge.svelte";

  // The one card of the app: what the mark under the pointer says. It is an
  // aid for the eye: every mark also has its words where a screen reader
  // finds them, so the card itself is hidden from one. It takes no pointer,
  // and it does not move in: it is where the pointer is.
  const card = $derived(hovercard.card);
  const at = $derived(card ? hovercard.placement : undefined);
  const px = (value: number | undefined) => (value === undefined ? undefined : `${value}px`);
  // A card with a plot is wide enough for the plot to say something, and
  // never wider than the room beside the pointer: on a phone that is less.

  // A card's plot is asked for when the card is shown; if the card goes
  // first, the question is taken back.
  const plot = $derived(card?.plot);
  $effect(() => {
    if (!plot) return;
    plot.ask();
    return () => plot.rest();
  });
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
    style:min-width={plot ? `min(13rem, ${at.maxWidth}px)` : undefined}
  >
    <p class="text-body font-semibold">{card.title}</p>
    {#if card.level}
      <p class="mt-1"><StatusBadge level={card.level} label={card.lines[0] ?? ""} /></p>
    {/if}
    {#if plot}
      <!-- The room is kept while the plot is on its way: the card does not
           grow under the pointer. -->
      <div class="mt-2">
        <Sparkline
          values={plot.values() ?? []}
          format={plot.format}
          label={plot.label}
          limit={plot.limit}
        />
        <p class="text-muted text-micro">{plot.label}</p>
      </div>
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
