<script lang="ts">
  import Icon from "./Icon.svelte";

  // A failure in plain words, with the way out beside it.
  let { message, onretry }: { message: string; onretry?: () => void } = $props();
</script>

<div
  role="alert"
  class="border-critical bg-surface flex flex-wrap items-center gap-3 rounded-lg border px-4 py-3"
>
  <span class="text-critical"><Icon name="critical" size={18} /></span>
  <p class="min-w-0 flex-1 text-sm">{message}</p>
  {#if onretry}
    <button type="button" class="btn" onclick={onretry}>
      <Icon name="retry" />
      Try again
    </button>
  {/if}
</div>
