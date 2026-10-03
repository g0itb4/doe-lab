<script lang="ts">
  import type { Snippet } from "svelte";

  // One figure of the fleet: a label, the number with its unit, and what it
  // is measured against. A tile can hold a picture of the figure between the
  // number and the hint, and can be given the room of two.
  let {
    label,
    value,
    hint = "",
    class: klass = "",
    children,
  }: {
    label: string;
    value: string;
    hint?: string;
    class?: string;
    children?: Snippet;
  } = $props();
</script>

<div class="card px-card min-h-[6rem] py-2.5 {klass}">
  <dt class="text-muted text-label font-medium">{label}</dt>
  <dd class="text-figure mt-0.5 font-semibold tracking-tight">{value}</dd>
  {#if children}<dd class="mt-1">{@render children()}</dd>{/if}
  <dd class="text-muted text-label min-h-4">{hint}</dd>
</div>
