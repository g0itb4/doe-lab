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

<div class="card min-h-[5.5rem] px-3 py-2 {klass}">
  <dt class="text-muted text-xs font-medium">{label}</dt>
  <dd class="tabular mt-0.5 text-xl font-semibold">{value}</dd>
  {#if children}<dd class="mt-1">{@render children()}</dd>{/if}
  <dd class="text-muted min-h-4 text-xs">{hint}</dd>
</div>
