<script lang="ts">
  // One figure of the fleet: a label, the number with its unit, and what it
  // is measured against.
  let { label, value, hint = "" }: { label: string; value: string; hint?: string } = $props();
</script>

<div class="card px-3 py-2">
  <dt class="text-muted text-xs font-medium">{label}</dt>
  <dd class="tabular mt-0.5 text-xl font-semibold">{value}</dd>
  <dd class="text-muted min-h-4 text-xs">{hint}</dd>
</div>
