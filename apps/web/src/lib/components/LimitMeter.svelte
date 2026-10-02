<script lang="ts">
  import { LIMIT_BAND_W, type Use } from "$lib/limit.ts";
  import type { Level } from "$lib/status.ts";

  // A reading against its limit, as a bar: the bar is the reading, the tick is
  // the limit, and the track ends at the most the connection could ever carry.
  // What runs past the tick is hatched as well as coloured. Beside the bar is
  // where the reading stands in a word or two, and for a screen reader the
  // whole comparison as a sentence: the bar is never the only way to it.
  let {
    use,
    capW,
    level,
    empty = "No reading",
  }: {
    use: Use | undefined;
    // Where the track ends, in watts: the connection's own limit. Without it
    // the track ends at the limit, or at the reading when that is over it.
    capW?: number;
    // The colour of the bar: the status the page shows beside it. Without one
    // a reading inside its limit is well and one over it is a warning.
    level?: Level;
    // What to say when there is nothing to compare.
    empty?: string;
  } = $props();

  const scale = $derived(use ? Math.max(capW ?? 0, use.limitW, use.usedW) || 1 : 1);
  const pct = (watts: number) => (100 * watts) / scale;
  const over = $derived(use ? use.usedW > use.limitW : false);
  const tone = $derived(level ?? (use && use.headroomW < -LIMIT_BAND_W ? "warn" : "ok"));
</script>

<!-- Positioned, so that the sentence for a screen reader stays inside a table
     that scrolls sideways instead of widening the page. -->
<div class="meter relative flex items-center gap-2" data-level={tone} title={use?.text}>
  <div class="bar" class:none={!use} aria-hidden="true">
    {#if use}
      <div class="track">
        <div
          class="fill"
          class:end={!over}
          style:width="{pct(Math.min(use.usedW, use.limitW))}%"
        ></div>
        {#if over}
          <div
            class="fill over end"
            style:left="{pct(use.limitW)}%"
            style:width="{pct(use.usedW - use.limitW)}%"
          ></div>
        {/if}
      </div>
      <div
        class="tick"
        style:left="clamp(0px, calc({pct(use.limitW)}% - 1px), calc(100% - 2px))"
      ></div>
    {:else}
      <div class="track"></div>
    {/if}
  </div>
  {#if !use}
    <p class="text-muted text-xs whitespace-nowrap">{empty}</p>
  {:else}
    <p class="tabular text-xs whitespace-nowrap">
      <span aria-hidden="true">{use.brief}</span>
      <span class="sr-only">{use.text}</span>
    </p>
  {/if}
</div>

<style>
  /* The bar wears the status colour; the words keep the colour of text. The
     tick stands taller than the track. */
  .bar {
    color: var(--color-ok);
    position: relative;
    height: 14px;
    min-width: 4rem;
    flex: 1 1 4rem;
  }
  [data-level="info"] .bar {
    color: var(--color-accent);
  }
  [data-level="warn"] .bar {
    color: var(--color-warn);
  }
  [data-level="critical"] .bar {
    color: var(--color-critical);
  }
  .track {
    position: absolute;
    inset: 3px 0;
    overflow: hidden;
    border: 1px solid var(--color-rule);
    border-radius: 4px;
    background: var(--color-sunken);
  }
  .none .track {
    border-style: dashed;
    border-color: var(--color-control);
    background: none;
  }
  .fill {
    position: absolute;
    top: 0;
    bottom: 0;
    left: 0;
    background: currentColor;
  }
  .fill.end {
    border-radius: 0 4px 4px 0;
  }
  /* Past the limit: stripes, so that the eye needs no colour to see it. */
  .fill.over {
    background: repeating-linear-gradient(45deg, currentColor 0 2px, var(--color-surface) 2px 4px);
  }
  .tick {
    position: absolute;
    top: 0;
    bottom: 0;
    width: 2px;
    border-radius: 1px;
    background: var(--color-text);
  }
</style>
