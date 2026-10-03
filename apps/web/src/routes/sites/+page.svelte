<script lang="ts">
  import type { Site } from "@doelab/gen/doelab/v1/site_pb.js";
  import { goto } from "$app/navigation";
  import { page } from "$app/state";
  import { api } from "$lib/api.ts";
  import EmptyState from "$lib/components/EmptyState.svelte";
  import ErrorState from "$lib/components/ErrorState.svelte";
  import Skeleton from "$lib/components/Skeleton.svelte";
  import SortHeader from "$lib/components/SortHeader.svelte";
  import { feeder } from "$lib/feeder.svelte.ts";
  import { kw } from "$lib/format.ts";
  import { queryParam, withQuery } from "$lib/query.ts";
  import { Resource } from "$lib/resource.svelte.ts";
  import { enrolled, equipment, filterSites, phaseName } from "$lib/sites.ts";
  import { type Sort, sortOf, sortRows } from "$lib/sort.ts";

  // The filter lives in the address, so a filtered list can be shared.
  const query = $derived(queryParam(page.url, "q") ?? "");
  const enrolledOnly = $derived(queryParam(page.url, "enrolled") === "1");

  let sites = $state<Resource<Site[]>>();
  $effect(() => {
    const id = feeder.data?.id;
    if (!id) return;
    const list = new Resource<Site[]>(async (signal) => {
      const all: Site[] = [];
      for (let pageToken = ""; ;) {
        const res = await api.sites.listSites(
          { feederId: id, pageSize: 500, pageToken },
          { signal },
        );
        all.push(...res.sites);
        pageToken = res.nextPageToken;
        if (pageToken === "") return all;
      }
    });
    sites = list;
    void list.load();
    return () => list.cancel();
  });

  // The order of the rows is in the address too: by NMI with nothing asked.
  const BY_NMI: Sort = { key: "nmi", dir: "asc" };
  const columns = {
    nmi: (s: Site) => s.nmi,
    name: (s: Site) => s.name,
    phase: (s: Site) => s.phase,
    enrolled: (s: Site) => (enrolled(s) ? "Takes part" : "Passive"),
    // A site with no limit of its own has nothing to compare: it comes last.
    cap: (s: Site) => (s.exportCapW > 0 ? s.exportCapW : undefined),
  };
  const sort = $derived(
    sortOf(queryParam(page.url, "sort"), queryParam(page.url, "dir"), Object.keys(columns), BY_NMI),
  );
  const shown = $derived(
    sites?.data ? sortRows(filterSites(sites.data, query, enrolledOnly), sort, columns) : [],
  );

  function set(changes: Record<string, string | null>) {
    void goto(withQuery(page.url, changes), {
      replaceState: true,
      keepFocus: true,
      noScroll: true,
    });
  }
</script>

<svelte:head><title>Sites · doe-lab</title></svelte:head>

<div class="space-y-4">
  <h1 class="h-page">Sites</h1>

  <form class="flex flex-wrap items-end gap-3" role="search" onsubmit={(e) => e.preventDefault()}>
    <div class="min-w-0 flex-1 sm:max-w-xs">
      <label for="site-search" class="mb-1 block text-sm font-medium"
        >Find a site by NMI or name</label
      >
      <input
        id="site-search"
        type="search"
        class="field"
        autocomplete="off"
        value={query}
        oninput={(e) => set({ q: e.currentTarget.value || null })}
      />
    </div>
    <label class="flex min-h-9 items-center gap-2 text-sm">
      <input
        type="checkbox"
        class="accent-accent size-6"
        checked={enrolledOnly}
        onchange={(e) => set({ enrolled: e.currentTarget.checked ? "1" : null })}
      />
      Only sites that take part in envelopes
    </label>
  </form>

  {#if (feeder.error && !feeder.data) || sites?.error}
    <ErrorState
      message={sites?.error ?? feeder.error ?? ""}
      onretry={() => (feeder.data ? sites?.load() : feeder.load())}
    />
  {/if}

  <!-- Always there, so the list below does not move when the count arrives. -->
  <p class="text-muted text-sm" aria-live="polite">
    {#if sites?.data}{shown.length} of {sites.data.length} sites{:else}&nbsp;{/if}
  </p>

  {#if sites?.data}
    {#if shown.length === 0}
      <EmptyState title="No site matches">
        Nothing on this feeder has "{query}" in its NMI or name. Clear the search to see every site.
      </EmptyState>
    {:else}
      <!-- A wide or long table scrolls inside its own box, under a heading
           that stays; the page does not scroll sideways. -->
      <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
      <div class="card max-h-[70vh] overflow-auto" tabindex="0" role="region" aria-label="Sites">
        <table class="data-table min-w-[640px]">
          <thead>
            <tr>
              <SortHeader key="nmi" label="NMI" {sort} fallback={BY_NMI} />
              <SortHeader key="name" label="Name" {sort} fallback={BY_NMI} />
              <SortHeader key="phase" label="Phase" {sort} fallback={BY_NMI} />
              <th scope="col">Behind the meter</th>
              <SortHeader key="enrolled" label="Envelopes" {sort} fallback={BY_NMI} />
              <SortHeader
                key="cap"
                label="Connection limit, export"
                {sort}
                fallback={BY_NMI}
                align="right"
              />
            </tr>
          </thead>
          <tbody>
            {#each shown as site (site.id)}
              <tr>
                <th scope="row" class="text-left font-medium">
                  <a class="link inline-flex min-h-6 items-center" href="/sites/{site.nmi}"
                    >{site.nmi}</a
                  >
                </th>
                <td>{site.name}</td>
                <td>{phaseName(site.phase)}</td>
                <td>{equipment(site)}</td>
                <td>{enrolled(site) ? "Takes part" : "Passive"}</td>
                <td class="text-right">{site.exportCapW > 0 ? kw(site.exportCapW) : "–"}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {/if}
  {:else if !sites?.error && !(feeder.error && !feeder.data)}
    <Skeleton label="the sites" class="h-96 w-full" />
  {/if}
</div>
