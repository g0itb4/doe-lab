<script lang="ts">
  import { LEVEL_WORDS } from "$lib/map/schematic.ts";
  import type { Level } from "$lib/status.ts";
  import Icon from "./Icon.svelte";

  // What the marks of the feeder's drawing mean: each shape with its words,
  // and each status with its own shape beside its colour.
  let {
    near,
    moving = false,
  }: {
    // How near a limit is "near", in the reader's units: "2.3 V".
    near: string;
    // Whether dashes move along the lines.
    moving?: boolean;
  } = $props();

  const statuses: { level: Level; tone: string }[] = [
    { level: "ok", tone: "text-ok" },
    { level: "warn", tone: "text-warn" },
    { level: "critical", tone: "text-critical" },
    { level: "info", tone: "text-control" },
  ];
</script>

<div class="text-muted text-label space-y-1">
  <ul class="flex flex-wrap gap-x-4 gap-y-1" aria-label="The marks of the drawing">
    <li class="inline-flex items-center gap-1.5">
      <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true" class="mark">
        <rect x="2" y="2" width="12" height="12" rx="3" fill="currentColor" />
      </svg>
      The transformer
    </li>
    <li class="inline-flex items-center gap-1.5">
      <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true" class="mark">
        <circle cx="8" cy="8" r="5" fill="none" stroke="currentColor" stroke-width="3" />
      </svg>
      A site that takes part in envelopes
    </li>
    <li class="inline-flex items-center gap-1.5">
      <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true" class="mark">
        <circle cx="8" cy="8" r="3.5" fill="currentColor" />
      </svg>
      Another site, or a junction
    </li>
    <li class="inline-flex items-center gap-1.5">
      <svg width="34" height="16" viewBox="0 0 34 16" aria-hidden="true" class="mark">
        <path
          d="M2 8h30"
          fill="none"
          stroke="currentColor"
          stroke-width="5"
          stroke-linecap="round"
        />
        <path d="M13 3l6 5-6 5" fill="none" stroke="var(--color-text)" stroke-width="1.5" />
      </svg>
      <!-- A reader who asked for reduced motion has no dashes to read of. -->
      <span>
        A line: as heavy as the power in it, with an arrow for its direction{#if moving}<span
            class="motion-reduce:hidden">, and dashes that move the same way</span
          >{/if}
      </span>
    </li>
    <li class="inline-flex items-center gap-1.5">
      <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true">
        <circle
          cx="8"
          cy="8"
          r="6"
          fill="none"
          stroke="var(--color-accent)"
          stroke-width="2"
          stroke-dasharray="4 3"
        />
      </svg>
      What limits export now
    </li>
  </ul>
  <ul class="flex flex-wrap gap-x-4 gap-y-1" aria-label="Status of a mark">
    {#each statuses as s (s.level)}
      <li class="inline-flex items-center gap-1">
        <span class={s.tone}><Icon name={s.level} size={14} /></span>
        {LEVEL_WORDS[s.level]}
      </li>
    {/each}
  </ul>
  <p>
    Near a limit is within {near} of the edge of the voltage band, or above 80 % of a line's rating.
  </p>
</div>

<style>
  .mark {
    color: var(--color-ok);
    flex-shrink: 0;
  }
</style>
