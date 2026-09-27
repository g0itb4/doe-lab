<script lang="ts">
  import type { Site } from "@doelab/gen/doelab/v1/site_pb.js";
  import { goto } from "$app/navigation";
  import { page } from "$app/state";
  import { api } from "$lib/api.ts";
  import EmptyState from "$lib/components/EmptyState.svelte";
  import ErrorState from "$lib/components/ErrorState.svelte";
  import Skeleton from "$lib/components/Skeleton.svelte";
  import { feeder } from "$lib/feeder.svelte.ts";
  import { kw } from "$lib/format.ts";
  import { withQuery } from "$lib/query.ts";
  import { Resource } from "$lib/resource.svelte.ts";
  import { enrolled, equipment, filterSites, phaseName } from "$lib/sites.ts";

  // The filter lives in the address, so a filtered list can be shared.
  const query = $derived(page.url.searchParams.get("q") ?? "");
  const enrolledOnly = $derived(page.url.searchParams.get("enrolled") === "1");

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

  const shown = $derived(sites?.data ? filterSites(sites.data, query, enrolledOnly) : []);

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
  <h1 class="text-xl font-bold">Sites</h1>

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
        class="accent-accent size-5"
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

  {#if sites?.data}
    <p class="text-muted text-sm" aria-live="polite">
      {shown.length} of {sites.data.length} sites
    </p>
    {#if shown.length === 0}
      <EmptyState title="No site matches">
        Nothing on this feeder has "{query}" in its NMI or name. Clear the search to see every site.
      </EmptyState>
    {:else}
      <!-- A wide table scrolls inside its own box; the page does not. -->
      <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
      <div class="card overflow-x-auto" tabindex="0" role="region" aria-label="Sites">
        <table class="tabular w-full min-w-[640px] text-sm">
          <thead>
            <tr class="border-rule border-b text-left">
              <th scope="col" class="px-3 py-2 font-semibold">NMI</th>
              <th scope="col" class="px-3 py-2 font-semibold">Name</th>
              <th scope="col" class="px-3 py-2 font-semibold">Phase</th>
              <th scope="col" class="px-3 py-2 font-semibold">Behind the meter</th>
              <th scope="col" class="px-3 py-2 font-semibold">Envelopes</th>
              <th scope="col" class="px-3 py-2 text-right font-semibold"
                >Connection limit, export</th
              >
            </tr>
          </thead>
          <tbody>
            {#each shown as site (site.id)}
              <tr class="border-rule hover:bg-sunken border-b last:border-b-0">
                <th scope="row" class="px-3 py-2 text-left font-medium">
                  <a class="link inline-flex min-h-6 items-center" href="/sites/{site.nmi}"
                    >{site.nmi}</a
                  >
                </th>
                <td class="px-3 py-2">{site.name}</td>
                <td class="px-3 py-2">{phaseName(site.phase)}</td>
                <td class="px-3 py-2">{equipment(site)}</td>
                <td class="px-3 py-2">{enrolled(site) ? "Takes part" : "Passive"}</td>
                <td class="px-3 py-2 text-right"
                  >{site.exportCapW > 0 ? kw(site.exportCapW) : "–"}</td
                >
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
