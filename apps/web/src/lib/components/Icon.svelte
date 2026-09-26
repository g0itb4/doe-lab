<script lang="ts" module>
  // A small set, drawn on a 20 px grid with one stroke weight. Each status
  // has its own shape, so the shape says what the colour says.
  const PATHS = {
    ok: "M10 2a8 8 0 1 0 0 16 8 8 0 0 0 0-16Zm-3.5 8.5 2.5 2.5 4.5-5",
    info: "M10 2a8 8 0 1 0 0 16 8 8 0 0 0 0-16Zm0 7v5m0-8v.5",
    warn: "M10 2.5 18 17H2L10 2.5Zm0 5.5v4m0 2.5v.5",
    critical: "M6.5 2h7L18 6.5v7L13.5 18h-7L2 13.5v-7L6.5 2ZM10 6v5m0 2.5v.5",
    sun: "M10 6.5a3.5 3.5 0 1 0 0 7 3.5 3.5 0 0 0 0-7ZM10 1.5v2m0 13v2M1.5 10h2m13 0h2M4 4l1.4 1.4m9.2 9.2L16 16M4 16l1.4-1.4m9.2-9.2L16 4",
    moon: "M16.5 12.5A7 7 0 0 1 7.5 3.5a7 7 0 1 0 9 9Z",
    auto: "M10 2a8 8 0 1 0 0 16 8 8 0 0 0 0-16Zm0 0v16",
    retry: "M16 10a6 6 0 1 1-2-4.5M16 3v3.5h-3.5",
    close: "M5 5l10 10M15 5 5 15",
    table: "M2.5 4.5h15v11h-15v-11Zm0 3.7h15m-15 3.6h15M7.5 4.5v11",
    chart: "M2.5 16.5h15M4 13l4-5 3 3 5-7",
    link: "M7.5 4.5h-3v11h11v-3M10 10l6.5-6.5m0 0H12m4.5 0V8",
  } as const;
  export type IconName = keyof typeof PATHS;
</script>

<script lang="ts">
  let { name, size = 16 }: { name: IconName; size?: number } = $props();
</script>

<!-- Decoration: the word beside it carries the meaning. -->
<svg
  width={size}
  height={size}
  viewBox="0 0 20 20"
  fill="none"
  stroke="currentColor"
  stroke-width="1.7"
  stroke-linecap="round"
  stroke-linejoin="round"
  aria-hidden="true"
  class="shrink-0"
>
  <path d={PATHS[name]} />
</svg>
