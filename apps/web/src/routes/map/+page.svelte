<script lang="ts">
  import type { Feeder } from "@doelab/gen/doelab/v1/feeder_pb.js";
  import type { Site } from "@doelab/gen/doelab/v1/site_pb.js";
  import type { Substation } from "@doelab/gen/doelab/v1/substation_pb.js";
  import type { GetFleetStateResponse } from "@doelab/gen/doelab/v1/telemetry_pb.js";
  import type { Component, ComponentProps } from "svelte";
  import { onMount } from "svelte";
  import { goto } from "$app/navigation";
  import { page } from "$app/state";
  import { api } from "$lib/api.ts";
  import { clock } from "$lib/clock.svelte.ts";
  import EmptyState from "$lib/components/EmptyState.svelte";
  import ErrorState from "$lib/components/ErrorState.svelte";
  import type FleetMap from "$lib/components/FleetMap.svelte";
  import LimitMeter from "$lib/components/LimitMeter.svelte";
  import MapLegend from "$lib/components/MapLegend.svelte";
  import Skeleton from "$lib/components/Skeleton.svelte";
  import StaleBanner from "$lib/components/StaleBanner.svelte";
  import StatusBadge from "$lib/components/StatusBadge.svelte";
  import { kw } from "$lib/format.ts";
  import { fleetView, frame, KIND_WORDS, regionsOf, type SiteMark } from "$lib/map/fleet.ts";
  import { queryParam, withQuery } from "$lib/query.ts";
  import { Resource } from "$lib/resource.svelte.ts";

  // Where everything is, which does not change, and what it is doing, which
  // is asked again as feeder time passes.
  type Places = { substations: Substation[]; feeders: Feeder[]; sites: Site[] };
  const places = new Resource<Places>(async (signal) => {
    const [substations, feeders, sites] = await Promise.all([
      api.substations.listSubstations({ pageSize: 500 }, { signal }),
      api.feeders.listFeeders({ pageSize: 500 }, { signal }),
      api.sites.listLocatedSites({ pageSize: 500 }, { signal }),
    ]);
    return { substations: substations.substations, feeders: feeders.feeders, sites: sites.sites };
  });
  const fleet = new Resource<GetFleetStateResponse>((signal) =>
    api.telemetry.getFleetState({}, { signal }),
  );

  // The map itself is fetched in the browser, when the page is: its library
  // is no part of any other page's first load.
  let MapView = $state<Component<ComponentProps<typeof FleetMap>>>();
  onMount(() => {
    void import("$lib/components/FleetMap.svelte").then((m) => (MapView = m.default));
    void places.load();
    void fleet.load();
    // A minute of feeder time, and at most every five seconds.
    const every = Math.max(5000, 60_000 / clock.speed);
    const timer = setInterval(() => {
      if (document.visibilityState !== "hidden") void fleet.load();
    }, every);
    return () => {
      clearInterval(timer);
      places.cancel();
      fleet.cancel();
    };
  });

  const view = $derived(
    places.data
      ? fleetView(places.data.substations, places.data.feeders, places.data.sites, fleet.data)
      : undefined,
  );
  const regions = $derived(view ? regionsOf(view) : []);

  // What is framed and what is selected live in the address.
  const substationCode = $derived(queryParam(page.url, "sub") ?? "");
  const siteNmi = $derived(queryParam(page.url, "site") ?? "");
  const region = $derived(queryParam(page.url, "region") ?? regions[0] ?? "all");
  // Framed from the places alone: the map does not move when the state does.
  const bounds = $derived(
    places.data
      ? frame(
          fleetView(places.data.substations, places.data.feeders, places.data.sites, undefined),
          region,
          substationCode,
        )
      : undefined,
  );

  const substation = $derived(view?.substations.find((s) => s.code === substationCode));
  const site = $derived(view?.sites.find((s) => s.nmi === siteNmi));
  // The sites of the table: those of the substation in view, or of the region.
  const listed = $derived(
    (view?.sites ?? []).filter((s) => {
      if (substationCode) return s.substationCode === substationCode;
      const of = view?.substations.find((sub) => sub.code === s.substationCode);
      return region === "all" || of?.state === region;
    }),
  );
  const shownSubstations = $derived(
    (view?.substations ?? []).filter((s) => region === "all" || s.state === region),
  );

  // Why a site has no comparison to show.
  const noUse = (s: SiteMark) => (s.exportW === undefined ? "No reading" : "No limit in force");

  function select(kind: "site" | "substation", key: string) {
    const changes: Record<string, string | null> =
      kind === "site" ? { site: key } : { sub: key, site: null };
    void goto(withQuery(page.url, changes), { keepFocus: true, noScroll: true });
  }
</script>

<svelte:head><title>Map · doe-lab</title></svelte:head>

<div class="space-y-4">
  <div>
    <h1 class="text-2xl font-bold tracking-tight">Map</h1>
    <p class="text-muted max-w-prose text-sm">
      Every substation, and around each the sites with solar, a battery or an EV charger. A site's
      ring fills as it uses its export limit, and a substation's border as its sites do together.
    </p>
  </div>

  <StaleBanner stale={fleet.stale} what="The fleet's state" />

  {#if places.error && !places.data}
    <ErrorState message={places.error} onretry={() => places.load()} />
  {:else if fleet.error && !fleet.data}
    <ErrorState message={fleet.error} onretry={() => fleet.load()} />
  {/if}

  {#if view && view.substations.length === 0}
    <EmptyState title="Nothing is on the map yet">
      No substation has been loaded. Run <code>just import</code> to load the fleet.
    </EmptyState>
  {:else if view}
    <nav aria-label="Region">
      <ul class="flex flex-wrap gap-1">
        {#each [...regions, "all"] as key (key)}
          <li>
            <a
              href={withQuery(page.url, { region: key, sub: null, site: null })}
              aria-current={region === key && !substationCode ? "true" : undefined}
              class="btn aria-[current=true]:bg-sunken aria-[current=true]:font-semibold"
              data-sveltekit-noscroll
            >
              {key === "all" ? "All regions" : key}
            </a>
          </li>
        {/each}
      </ul>
    </nav>

    <div class="grid gap-4 lg:grid-cols-[minmax(0,1fr)_20rem]">
      <figure class="min-w-0 space-y-2">
        {#if MapView}
          <MapView {view} {bounds} selected={siteNmi || substationCode} onselect={select} />
        {:else}
          <Skeleton label="the map" class="h-[30rem] w-full" />
        {/if}
        <figcaption><MapLegend /></figcaption>
      </figure>

      <!-- What is selected. Always there, so the map beside it does not move. -->
      <section
        class="card min-h-40 min-w-0 space-y-2 p-3"
        aria-live="polite"
        aria-labelledby="selected"
      >
        {#if site}
          <h2 id="selected" class="tabular text-base font-semibold">{site.nmi}</h2>
          <StatusBadge level={site.status.level} label={site.status.label} />
          <p class="text-sm">{site.status.detail}</p>
          <LimitMeter use={site.use} level={site.status.level} empty={noUse(site)} />
          <dl class="tabular grid grid-cols-2 gap-x-3 gap-y-1 text-sm">
            <dt class="text-muted">Equipment</dt>
            <dd>{KIND_WORDS[site.kind]}</dd>
            <dt class="text-muted">Feeder</dt>
            <dd>{site.feederCode}</dd>
          </dl>
          <p>
            <a class="link text-sm" href="/sites/{site.nmi}?feeder={site.feederCode}"
              >Open the site</a
            >
            ·
            <a class="link text-sm" href="/network?feeder={site.feederCode}">See its feeder</a>
          </p>
        {:else if substation}
          <h2 id="selected" class="text-base font-semibold">{substation.name}</h2>
          <StatusBadge level={substation.status.level} label={substation.status.label} />
          <p class="text-sm">{substation.status.detail}</p>
          <LimitMeter
            use={substation.use}
            level={substation.status.level}
            empty="No site to compare"
          />
          <dl class="tabular grid grid-cols-2 gap-x-3 gap-y-1 text-sm">
            <dt class="text-muted">Network</dt>
            <dd>{substation.dnsp}, {substation.state}</dd>
            <dt class="text-muted">Feeders</dt>
            <dd>{substation.feeders}</dd>
            <dt class="text-muted">Sites with DER</dt>
            <dd>{substation.sites}</dd>
          </dl>
        {:else}
          <h2 id="selected" class="text-base font-semibold">Nothing selected</h2>
          <p class="text-muted text-sm">
            Choose a substation or a site, on the map or in the lists below, to see what it is
            doing.
          </p>
        {/if}
      </section>
    </div>

    <section aria-labelledby="substations" class="space-y-2">
      <h2 id="substations" class="text-lg font-semibold">Substations</h2>
      <ul class="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
        {#each shownSubstations as s (s.code)}
          <li class="card flex flex-wrap items-center gap-2 px-3 py-2">
            <a
              class="link min-h-6 font-medium"
              href={withQuery(page.url, { sub: s.code, site: null })}
              aria-current={s.code === substationCode ? "true" : undefined}
              data-sveltekit-noscroll>{s.name}</a
            >
            <StatusBadge level={s.status.level} label={s.status.label} />
            <span class="text-muted tabular w-full text-xs">
              {s.reporting} of {s.sites} sites reporting{s.use
                ? ` · ${kw(s.use.usedW)} of ${kw(s.use.limitW)}`
                : ""}
            </span>
            <div class="w-full">
              <LimitMeter use={s.use} level={s.status.level} empty="No site to compare" />
            </div>
          </li>
        {/each}
      </ul>
    </section>

    <section aria-labelledby="sites" class="space-y-2">
      <h2 id="sites" class="text-lg font-semibold">
        Sites {substation ? `of ${substation.name}` : region === "all" ? "" : `in ${region}`}
      </h2>
      <!-- A wide table scrolls inside its own box; the page does not. -->
      <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
      <div class="card overflow-x-auto" tabindex="0" role="region" aria-label="Sites on the map">
        <table class="tabular w-full min-w-[800px] text-sm">
          <thead>
            <tr class="border-rule border-b text-left">
              <th scope="col" class="px-3 py-2 font-semibold">NMI</th>
              <th scope="col" class="px-3 py-2 font-semibold">Feeder</th>
              <th scope="col" class="px-3 py-2 font-semibold">Equipment</th>
              <th scope="col" class="px-3 py-2 font-semibold">Status</th>
              <th scope="col" class="px-3 py-2 text-right font-semibold">Net export</th>
              <th scope="col" class="px-3 py-2 text-right font-semibold">Export limit</th>
              <th scope="col" class="px-3 py-2 font-semibold">Use of limit</th>
            </tr>
          </thead>
          <tbody>
            {#each listed as s (s.id)}
              <tr class="border-rule hover:bg-sunken border-b last:border-b-0">
                <th scope="row" class="px-3 py-2 text-left font-medium">
                  <a
                    class="link inline-flex min-h-6 items-center"
                    href={withQuery(page.url, { site: s.nmi })}
                    aria-current={s.nmi === siteNmi ? "true" : undefined}
                    data-sveltekit-noscroll>{s.nmi}</a
                  >
                </th>
                <td class="px-3 py-2">{s.feederCode}</td>
                <td class="px-3 py-2">{KIND_WORDS[s.kind]}</td>
                <td class="px-3 py-2">
                  <StatusBadge level={s.status.level} label={s.status.label} />
                </td>
                <td class="px-3 py-2 text-right">{kw(s.exportW)}</td>
                <td class="px-3 py-2 text-right">{kw(s.limitW)}</td>
                <td class="w-48 px-3 py-2">
                  <LimitMeter use={s.use} level={s.status.level} empty={noUse(s)} />
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    </section>
  {:else if !places.error}
    <Skeleton label="the map" class="h-[30rem] w-full" />
  {/if}
</div>
