<script lang="ts">
  import { toasts } from "$lib/toast.svelte.ts";
  import Icon from "./Icon.svelte";
</script>

<!-- The result of an action, said once. A failure interrupts; a success
     waits its turn. -->
<div class="pointer-events-none fixed inset-x-0 bottom-0 z-50 flex flex-col items-center gap-2 p-4">
  {#each toasts.items as toast (toast.id)}
    <div
      role={toast.kind === "error" ? "alert" : "status"}
      class="bg-surface pointer-events-auto flex max-w-md items-start gap-2 rounded-lg border px-3 py-2 text-sm shadow-lg {toast.kind ===
      'error'
        ? 'border-critical'
        : 'border-ok'}"
    >
      <span class="mt-0.5 {toast.kind === 'error' ? 'text-critical' : 'text-ok'}">
        <Icon name={toast.kind === "error" ? "critical" : "ok"} />
      </span>
      <p class="min-w-0 flex-1">{toast.text}</p>
      <button
        type="button"
        class="text-muted hover:bg-sunken -m-1 grid size-7 cursor-pointer place-items-center rounded"
        aria-label="Dismiss"
        onclick={() => toasts.dismiss(toast.id)}
      >
        <Icon name="close" size={14} />
      </button>
    </div>
  {/each}
</div>
