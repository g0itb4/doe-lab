<script lang="ts">
  import type { Level } from "$lib/status.ts";
  import Icon from "./Icon.svelte";

  // A status is an icon, a word and a colour: never the colour alone.
  let { level, label, large = false }: { level: Level; label: string; large?: boolean } = $props();

  const tone: Record<Level, string> = {
    ok: "text-ok border-ok",
    info: "text-accent border-accent",
    warn: "text-warn border-warn",
    critical: "text-critical border-critical",
  };
</script>

<span
  class="bg-surface inline-flex items-center gap-1.5 rounded-full border font-semibold {tone[
    level
  ]} {large ? 'px-3 py-1 text-base' : 'px-2 py-0.5 text-xs'}"
  data-level={level}
>
  <Icon name={level} size={large ? 18 : 14} />
  {label}
</span>
