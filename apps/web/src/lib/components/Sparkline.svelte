<script lang="ts">
  import { limitDomain, sparkPath, sparkY, trendWords } from "$lib/sparkline.ts";

  // The shape of a figure through time, beside the figure: a line with no
  // axes. With a limit, the line is drawn against it: the box runs from
  // nothing up to the limit, which is a dashed rule across it, so the eye
  // sees how much of the limit was used. It is a picture of what its words
  // say, so the words are there for a screen reader and the line is hidden
  // from one.
  let {
    values,
    format,
    label,
    limit,
  }: {
    values: readonly (number | null | undefined)[];
    // A value with its unit, for the words.
    format: (value: number) => string;
    // What the line is of: "Measured export".
    label: string;
    // The limit the values are held to, in their own unit.
    limit?: number;
  } = $props();

  // The line is drawn in a box of its own units and stretched to its room.
  const [WIDTH, HEIGHT] = [120, 24];
  const domain = $derived(limit === undefined ? undefined : limitDomain(values, limit));
  const d = $derived(sparkPath(values, WIDTH, HEIGHT, 1.5, domain));
  const rule = $derived(limit === undefined || !domain ? undefined : sparkY(limit, domain, HEIGHT));
</script>

<div class="spark">
  <svg viewBox="0 0 {WIDTH} {HEIGHT}" preserveAspectRatio="none" aria-hidden="true">
    {#if rule !== undefined}<line class="limit" x1="0" x2={WIDTH} y1={rule} y2={rule} />{/if}
    {#if d}<path {d} />{/if}
  </svg>
  <span class="sr-only"
    >{label}: {trendWords(values, format)}{limit === undefined
      ? ""
      : ` The limit is ${format(limit)}.`}</span
  >
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
  path,
  .limit {
    fill: none;
    stroke: currentColor;
    stroke-width: 1.5;
    stroke-linecap: round;
    stroke-linejoin: round;
    /* The box is stretched; the line keeps its weight. */
    vector-effect: non-scaling-stroke;
  }
  /* The limit: a dashed rule, told from the line by its dashes as well as
     its colour. */
  .limit {
    stroke: var(--color-control);
    stroke-width: 1;
    stroke-dasharray: 3 3;
    stroke-linecap: butt;
  }
</style>
