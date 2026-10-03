<script lang="ts" module>
  import type { Level } from "$lib/status.ts";

  // The mark of each status: a small solid shape on a 12 px grid, each with
  // an outline of its own, so the shape says what the colour says. A dot is
  // well, a ring has nothing to report, a triangle wants a look, a diamond
  // wants a hand. The map draws the same shapes on its marks.
  export const MARKS: Record<Level, string> = {
    ok: "M6 2.25a3.75 3.75 0 1 0 0 7.5a3.75 3.75 0 0 0 0-7.5Z",
    info: "M6 1.75a4.25 4.25 0 1 0 0 8.5a4.25 4.25 0 0 0 0-8.5Zm0 2.1a2.15 2.15 0 1 1 0 4.3a2.15 2.15 0 0 1 0-4.3Z",
    warn: "M6 2 10.4 9.75H1.6Z",
    critical: "M6 1.4 10.6 6 6 10.6 1.4 6Z",
  };
</script>

<script lang="ts">
  // In the colour of the text around it: whoever shows a status gives it its
  // colour, and its word.
  let { level, size = 10 }: { level: Level; size?: number } = $props();
</script>

<!-- Decoration: the word beside it carries the meaning. -->
<svg
  width={size}
  height={size}
  viewBox="0 0 12 12"
  fill="currentColor"
  fill-rule="evenodd"
  stroke="currentColor"
  stroke-width="1"
  stroke-linejoin="round"
  aria-hidden="true"
  class="shrink-0"
  data-mark={level}
>
  <path d={MARKS[level]} />
</svg>
