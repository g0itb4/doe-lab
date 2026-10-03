<script lang="ts">
  import { sparkPath, trendWords } from "$lib/sparkline.ts";

  // The shape of a figure through time, beside the figure: a line with no
  // axes. It is a picture of what its words say, so the words are there for
  // a screen reader and the line is hidden from one.
  let {
    values,
    format,
    label,
  }: {
    values: readonly (number | null | undefined)[];
    // A value with its unit, for the words.
    format: (value: number) => string;
    // What the line is of: "Measured export".
    label: string;
  } = $props();

  // The line is drawn in a box of its own units and stretched to its room.
  const [WIDTH, HEIGHT] = [120, 24];
  const d = $derived(sparkPath(values, WIDTH, HEIGHT));
</script>

<div class="spark">
  <svg viewBox="0 0 {WIDTH} {HEIGHT}" preserveAspectRatio="none" aria-hidden="true">
    {#if d}<path {d} />{/if}
  </svg>
  <span class="sr-only">{label}: {trendWords(values, format)}</span>
</div>

<style>
  /* Positioned, so that the words for a screen reader stay inside the tile. */
  .spark {
    position: relative;
    color: var(--color-series-1);
  }
  svg {
    display: block;
    width: 100%;
    height: 1.5rem;
  }
  path {
    fill: none;
    stroke: currentColor;
    stroke-width: 1.5;
    stroke-linecap: round;
    stroke-linejoin: round;
    /* The box is stretched; the line keeps its weight. */
    vector-effect: non-scaling-stroke;
  }
</style>
