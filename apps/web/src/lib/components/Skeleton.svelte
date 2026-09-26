<script lang="ts">
  // A placeholder with the size of what will replace it, so nothing moves
  // when the data arrives.
  let { label, class: klass = "h-6 w-full" }: { label: string; class?: string } = $props();
</script>

<div role="status" class="animate-pulse-soft bg-sunken rounded-md {klass}">
  <span class="sr-only">Loading {label}…</span>
</div>
