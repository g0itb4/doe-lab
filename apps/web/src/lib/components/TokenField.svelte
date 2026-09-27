<script lang="ts">
  import { operator } from "$lib/operator.svelte.ts";

  // The operator's token: one field, shared by every action that needs it.
  // `error` is what the API said about the token the last time it was used.
  let { id, error = "" }: { id: string; error?: string } = $props();
</script>

<div>
  <label for={id} class="mb-1 block text-sm font-medium">Operator token</label>
  <input
    {id}
    type="password"
    class="field"
    autocomplete="off"
    spellcheck="false"
    aria-invalid={error ? "true" : undefined}
    aria-describedby="{id}-help"
    value={operator.token}
    oninput={(e) => (operator.token = e.currentTarget.value)}
  />
  <p id="{id}-help" class="mt-1 text-xs {error ? 'text-critical' : 'text-muted'}">
    {error || "Changes need it; reading does not. It is kept for this tab only."}
  </p>
</div>
