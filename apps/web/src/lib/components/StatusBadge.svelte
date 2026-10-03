<script lang="ts">
  import type { Level } from "$lib/status.ts";
  import Icon from "./Icon.svelte";

  // A status is an icon, a word and a colour: never the colour alone.
  let { level, label, large = false }: { level: Level; label: string; large?: boolean } = $props();

  // The status's colour as text and border, on its own tint.
  const tone: Record<Level, string> = {
    ok: "text-ok border-ok bg-ok-soft",
    info: "text-accent border-accent bg-accent-soft",
    warn: "text-warn border-warn bg-warn-soft",
    critical: "text-critical border-critical bg-critical-soft",
  };
</script>

<span
  class="inline-flex items-center gap-1.5 rounded-full border font-semibold {tone[level]} {large
    ? 'px-3 py-1 text-base'
    : 'text-label px-2 py-0.5'}"
  data-level={level}
>
  <Icon name={level} size={large ? 18 : 14} />
  {label}
</span>
