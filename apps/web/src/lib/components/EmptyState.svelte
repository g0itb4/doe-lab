<script lang="ts">
  import type { Snippet } from "svelte";

  // Nothing to show: say why, and what to do about it.
  let { title, children }: { title: string; children: Snippet } = $props();
</script>

<div class="border-control rounded-lg border border-dashed px-4 py-6 text-center">
  <p class="font-semibold">{title}</p>
  <div class="text-muted mx-auto mt-1 max-w-prose text-sm">{@render children()}</div>
</div>
