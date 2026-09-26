<script lang="ts">
  import { page } from "$app/state";
  import { withQuery } from "$lib/query.ts";
  import { RANGES, type RangeKey } from "$lib/series.ts";

  // The time range, as links: the range is part of the address, so a view
  // can be shared and the back button undoes a change.
  let { current }: { current: RangeKey } = $props();

  const words: Record<RangeKey, string> = { "6h": "6 hours", "24h": "24 hours", "3d": "3 days" };
</script>

<nav aria-label="Time range">
  <ul class="border-control inline-flex overflow-hidden rounded-md border">
    {#each Object.keys(RANGES) as RangeKey[] as key (key)}
      <li class="border-control border-l first:border-l-0">
        <a
          href={withQuery(page.url, { range: key, from: null, to: null })}
          aria-current={key === current ? "true" : undefined}
          data-sveltekit-replacestate
          data-sveltekit-noscroll
          data-sveltekit-keepfocus
          class="bg-surface text-muted hover:bg-sunken aria-[current=true]:bg-accent aria-[current=true]:text-accent-text inline-flex min-h-9 items-center px-3 text-sm font-medium"
        >
          {words[key]}
        </a>
      </li>
    {/each}
  </ul>
</nav>
