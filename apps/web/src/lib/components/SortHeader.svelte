<script lang="ts">
  import { page } from "$app/state";
  import { withQuery } from "$lib/query.ts";
  import { ariaSort, nextSort, type Sort } from "$lib/sort.ts";
  import Icon from "./Icon.svelte";

  // The heading of a column that can order its table. It is a link: the
  // order is part of the address, so an ordered table can be shared and the
  // back button undoes a change. An arrow says which way the table runs, as
  // `aria-sort` does to a screen reader.
  let {
    key,
    label,
    sort,
    fallback,
    align = "left",
  }: {
    key: string;
    label: string;
    // The order the table is in, and the order it is in with nothing asked.
    sort: Sort;
    fallback: Sort;
    align?: "left" | "right";
  } = $props();

  const next = $derived(nextSort(sort, key));
  const isFallback = $derived(next.key === fallback.key && next.dir === fallback.dir);
  const state = $derived(ariaSort(sort, key));
</script>

<th scope="col" aria-sort={state} class={align === "right" ? "text-right" : ""}>
  <a
    href={withQuery(
      page.url,
      isFallback ? { sort: null, dir: null } : { sort: next.key, dir: next.dir },
    )}
    class="hover:text-text inline-flex min-h-6 items-center gap-1 {state === 'none'
      ? ''
      : 'text-text'}"
    data-sveltekit-replacestate
    data-sveltekit-noscroll
    data-sveltekit-keepfocus
  >
    {label}
    {#if state !== "none"}
      <Icon name={state === "ascending" ? "up" : "down"} size={12} />
    {/if}
  </a>
</th>
