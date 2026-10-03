<script lang="ts">
  import type { Level } from "$lib/status.ts";
  import StatusMark from "./StatusMark.svelte";

  // A status is a mark, a word and a colour: never the colour alone. It is
  // quiet while all is well, a mark and a word in a thin outline, and takes a
  // tint only when it wants attention: on a page of forty sites the eye goes
  // to the three that need it. The word keeps the colour of text, so it reads
  // as well on a tint as off one.
  let { level, label, large = false }: { level: Level; label: string; large?: boolean } = $props();

  // The colour of the mark, and what is behind the word.
  const tone: Record<Level, { mark: string; box: string }> = {
    ok: { mark: "text-ok", box: "border-rule" },
    info: { mark: "text-accent", box: "border-rule" },
    warn: { mark: "text-warn", box: "bg-warn-soft border-transparent" },
    critical: { mark: "text-critical", box: "bg-critical-soft border-transparent" },
  };
</script>

<span
  class="rounded-control text-text inline-flex items-center border font-medium {tone[level]
    .box} {large
    ? 'gap-2 px-2.5 py-1 text-base font-semibold'
    : 'text-label gap-1.5 px-1.5 py-0.5 whitespace-nowrap'}"
  data-level={level}
>
  <span class="inline-flex {tone[level].mark}"><StatusMark {level} size={large ? 12 : 10} /></span>
  {label}
</span>
