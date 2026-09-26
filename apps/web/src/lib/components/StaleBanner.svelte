<script lang="ts">
  import Icon from "./Icon.svelte";

  // Shown while a live stream is down. What is on screen stays, and this
  // says how far to trust it.
  let { stale, what = "Live updates" }: { stale: boolean; what?: string } = $props();
</script>

<!-- Always in the page, so a screen reader hears the change in its text. -->
<div role="status" aria-live="polite">
  {#if stale}
    <p
      class="border-warn bg-surface text-warn flex items-center gap-2 rounded-md border px-3 py-2 text-sm"
    >
      <Icon name="warn" />
      <span class="text-text"
        >{what} paused, reconnecting… The figures below are the last received.</span
      >
    </p>
  {/if}
</div>
