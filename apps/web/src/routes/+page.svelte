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
  import Kpi from "$lib/components/Kpi.svelte";
  import LimitMeter from "$lib/components/LimitMeter.svelte";
  import MapLegend from "$lib/components/MapLegend.svelte";
  import Skeleton from "$lib/components/Skeleton.svelte";
  import SortHeader from "$lib/components/SortHeader.svelte";
  import Sparkline from "$lib/components/Sparkline.svelte";
  import StaleBanner from "$lib/components/StaleBanner.svelte";
  import StatusBadge from "$lib/components/StatusBadge.svelte";
  import { count, kw } from "$lib/format.ts";
  import {
    attention,
    fleetTotals,
    fleetView,
    frame,
    KIND_WORDS,
    regionsOf,
    type SiteMark,
  } from "$lib/map/fleet.ts";
  import { poll } from "$lib/poll.ts";
  import { queryParam, withQuery } from "$lib/query.ts";
  import { Resource } from "$lib/resource.svelte.ts";
  import { type Sort, sortOf, sortRows } from "$lib/sort.ts";
  import { timestamp } from "$lib/time.ts";

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

  const REPO_URL: string | undefined = import.meta.env.VITE_REPO_URL;
  const INTRO_KEY = "doelab.intro";

  // The panel is in the prerendered page, so a first-time visitor reads it at
  // first paint. For one who dismissed it, app.html hides it before paint.
  let intro = $state(true);
  function dismissIntro() {
    intro = false;
    document.documentElement.dataset.intro = "dismissed";
    try {
      localStorage.setItem(INTRO_KEY, "dismissed");
    } catch {
      // It stays dismissed for this visit.
    }
  }

  // The map itself is fetched in the browser, when the page is: its library
  // is no part of any page's first load.
  let MapView = $state<Component<ComponentProps<typeof FleetMap>>>();
  onMount(() => {
    try {
      intro = localStorage.getItem(INTRO_KEY) === null;
    } catch {
      // Storage is blocked: the panel shows on every visit.
    }
    void import("$lib/components/FleetMap.svelte").then((m) => (MapView = m.default));
    void places.load();
    void fleet.load();
    return () => {
      places.cancel();
      fleet.cancel();
    };
  });
  // The state again every minute of feeder time, and at most every five
  // seconds: once the clock has said how fast that is.
  $effect(() => {
    if (clock.settled) return poll(() => void fleet.load(), 60_000);
  });

  const view = $derived(
    places.data
      ? fleetView(places.data.substations, places.data.feeders, places.data.sites, fleet.data)
      : undefined,
  );
  const regions = $derived(view ? regionsOf(view) : []);
  // The fleet in a few figures, and the marks that ask for an operator's eye:
  // both wait for the state, so that neither says "all is well" too early.
  const totals = $derived(fleet.data ? fleetTotals(fleet.data.feeders) : undefined);
  const asking = $derived(view && fleet.data ? attention(view) : undefined);
  // How many of them the panel lists: the table below has every site.
  const LISTED = 8;

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

  // What the chosen site has exported over the last six hours of feeder time:
  // asked for when a site is chosen, and not again until another is.
  const TRACE_SECONDS = 6 * 3600;
  const siteId = $derived(site?.id);
  let trace = $state<Resource<number[]>>();
  $effect(() => {
    const id = siteId;
    // Not before the clock has settled: the window is in feeder time.
    if (!id || !clock.settled) {
      trace = undefined;
      return;
    }
    const loaded = new Resource(async (signal) => {
      const now = clock.nowSeconds();
      const res = await api.telemetry.getSiteSeries(
        { siteId: id, from: timestamp(now - TRACE_SECONDS), to: timestamp(now) },
        { signal },
      );
      return res.power.map((p) => p.avgNetExportW);
    });
    trace = loaded;
    void loaded.load();
    return () => loaded.cancel();
  });
  // The sites of the table: those of the substation in view, or of the region.
  const listed = $derived(
    (view?.sites ?? []).filter((s) => {
      if (substationCode) return s.substationCode === substationCode;
      const of = view?.substations.find((sub) => sub.code === s.substationCode);
      return region === "all" || of?.state === region;
    }),
  );
  // The order of the table's rows, in the address: by NMI with nothing asked.
  // By status, the gravest is first when the order runs downwards.
  const BY_NMI: Sort = { key: "nmi", dir: "asc" };
  const GRAVITY = { ok: 0, info: 1, warn: 2, critical: 3 };
  const columns = {
    nmi: (s: SiteMark) => s.nmi,
    feeder: (s: SiteMark) => s.feederCode,
    kind: (s: SiteMark) => KIND_WORDS[s.kind],
    status: (s: SiteMark) => GRAVITY[s.status.level],
    export: (s: SiteMark) => s.exportW,
    limit: (s: SiteMark) => s.limitW,
    use: (s: SiteMark) => s.use?.share,
  };
  const sort = $derived(
    sortOf(queryParam(page.url, "sort"), queryParam(page.url, "dir"), Object.keys(columns), BY_NMI),
  );
  const rows = $derived(sortRows(listed, sort, columns));
  const shownSubstations = $derived(
    (view?.substations ?? []).filter((s) => region === "all" || s.state === region),
  );

  // Why a site has no comparison to show.
  const noUse = (s: SiteMark) => (s.exportW === undefined ? "No reading" : "No limit in force");

  // What selecting a mark does to the address. A site brings its substation
  // with it, so the map frames where it is.
  function selection(kind: "site" | "substation", key: string): Record<string, string | null> {
    if (kind === "substation") return { sub: key, site: null };
    const of = view?.sites.find((s) => s.nmi === key)?.substationCode;
    return of ? { site: key, sub: of } : { site: key };
  }
  function select(kind: "site" | "substation", key: string) {
    void goto(withQuery(page.url, selection(kind, key)), { keepFocus: true, noScroll: true });
  }

  // For a screen reader: the figures in a sentence, at most every fifteen
  // seconds, and a new alert at once.
  let spoken = $state("");
  let spokenAt = 0;
  let alertText = $state("");
  let knownAlerts: number | undefined;
  $effect(() => {
    if (!totals) return;
    if (knownAlerts !== undefined && totals.openAlerts > knownAlerts) {
      alertText = `A new alert has opened. ${totals.openAlerts} alerts are open across the fleet.`;
    }
    knownAlerts = totals.openAlerts;
    if (Date.now() - spokenAt < 15_000) return;
    spokenAt = Date.now();
    spoken = `${totals.status.label}. ${totals.status.detail} ${totals.openAlerts} open alerts.`;
  });
</script>

<svelte:head><title>Fleet · doe-lab</title></svelte:head>

<div class="space-y-4">
  <div>
    <h1 class="h-page">Fleet</h1>
    <p class="text-muted text-body max-w-prose">
      Every feeder at once: whether the fleet is inside its limits, what asks for attention, and
      where it is.
    </p>
  </div>

  {#if intro}
    <aside
      id="intro"
      class="card border-accent flex flex-wrap items-start gap-3 p-3"
      aria-label="About this page"
    >
      <p class="text-body min-w-0 flex-1">
        <strong>What you are looking at.</strong> A model of low-voltage feeders, each below a zone
        substation: homes behind one transformer, many with rooftop solar, a battery or an EV
        charger. For every interval an engine solves each feeder's network and gives each
        participating site an <em>operating envelope</em>: the most it may export without pushing
        the street's voltage or the transformer past their limits. Simulated inverters obey the
        envelopes, and a few do not, so you can see the alerts. This is a simulation, not a real
        network.
        {#if REPO_URL}<a class="link" href={REPO_URL}>Read how it works</a>.{/if}
      </p>
      <button type="button" class="btn" onclick={dismissIntro}>Got it</button>
    </aside>
  {/if}

  <!-- The answer first: is the fleet inside its limits now? -->
  <section aria-labelledby="status-heading" class="card p-card">
    <h2 id="status-heading" class="sr-only">Status now</h2>
    {#if totals}
      <!-- As tall as its skeleton at least, so nothing below moves when it arrives. -->
      <div class="flex min-h-24 flex-wrap items-center gap-x-4 gap-y-2 sm:min-h-9">
        <StatusBadge level={totals.status.level} label={totals.status.label} large />
        <p class="text-body min-w-0 flex-1">{totals.status.detail}</p>
      </div>
    {:else if !fleet.error}
      <Skeleton label="the fleet's status" class="h-24 w-full sm:h-9" />
    {:else}
      <p class="text-muted text-body flex min-h-24 items-center sm:min-h-9">
        The fleet's state has not loaded.
      </p>
    {/if}
  </section>

  <StaleBanner stale={fleet.stale} what="The fleet's state" />
  <p class="sr-only" aria-live="polite">{spoken}</p>
  <p class="sr-only" role="alert">{alertText}</p>

  <section aria-labelledby="figures-heading">
    <h2 id="figures-heading" class="h-section mb-2">The fleet now</h2>
    {#if totals}
      <!-- Every tile as tall as the one with a bar, as the skeletons are. -->
      <dl class="gap-gutter grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-6 [&>div]:min-h-[6.75rem]">
        <!-- Like against like: only the sites that have a limit are in either
             number. What every site exports is the next tile. -->
        <Kpi
          label="Controlled export"
          value={kw(totals.controlledExportW)}
          hint="of {kw(totals.exportLimitW)} allowed"
        >
          <LimitMeter use={totals.use} empty="No limit in force" />
        </Kpi>
        <Kpi
          label="Export, all sites"
          value={kw(totals.exportW)}
          hint="import {kw(totals.importW)}"
        />
        <Kpi
          label="Sites reporting"
          value="{count(totals.reportingSites)} of {count(totals.enrolledSites)}"
          hint="{count(totals.devicesOnline)} of {count(totals.devices)} devices online"
        />
        <Kpi
          label="Sites over limit"
          value={count(totals.sitesOverLimit)}
          hint={totals.sitesOverLimit > 0
            ? `on ${totals.feedersOverLimit} of ${totals.feeders} feeders`
            : "all inside their limits"}
        />
        <Kpi
          label="Open alerts"
          value={count(totals.openAlerts)}
          hint={totals.openAlerts > 0
            ? `on ${totals.feedersWithAlerts} of ${totals.feeders} feeders`
            : "nothing to do"}
        />
        <Kpi
          label="Backstops"
          value={totals.backstops > 0 ? `${totals.backstops} active` : "None"}
          hint={totals.backstops > 0
            ? "envelopes overridden there"
            : `engine in control of ${totals.feeders === 1 ? "the feeder" : `all ${totals.feeders} feeders`}`}
        />
      </dl>
    {:else if !fleet.error}
      <div class="gap-gutter grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-6">
        {#each { length: 6 }, i (i)}
          <Skeleton label="the fleet's figures" class="h-[6.75rem] w-full" />
        {/each}
      </div>
    {/if}
  </section>

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

    <div class="grid gap-4 lg:grid-cols-[minmax(0,1fr)_22rem]">
      <figure class="min-w-0 space-y-2">
        <h2 class="h-section">Where it is</h2>
        {#if MapView}
          <MapView {view} {bounds} selected={siteNmi || substationCode} onselect={select} />
        {:else}
          <Skeleton label="the map" class="h-80 w-full sm:h-[30rem]" />
        {/if}
        <figcaption><MapLegend /></figcaption>
      </figure>

      <!-- What is selected. Always there, so the map beside it does not move. -->
      <!-- A choice is announced; the list that stands in for one is not, or
           every change of the fleet would be read out. -->
      <section
        class="card p-card min-h-40 min-w-0 space-y-2"
        aria-live={site || substation ? "polite" : "off"}
        aria-labelledby="selected"
      >
        {#if site}
          <h2 id="selected" class="h-panel">{site.nmi}</h2>
          <StatusBadge level={site.status.level} label={site.status.label} />
          <p class="text-sm">{site.status.detail}</p>
          <LimitMeter use={site.use} level={site.status.level} empty={noUse(site)} />
          <!-- Its net export through the last six hours: the room is kept, so
               the panel does not grow when the line arrives. -->
          <div>
            <p class="text-muted text-label">Net export, last 6 hours</p>
            <Sparkline values={trace?.data ?? []} format={(v) => kw(v)} label="Net export" />
          </div>
          <dl class="tabular grid grid-cols-2 gap-x-3 gap-y-1 text-sm">
            <dt class="text-muted">Equipment</dt>
            <dd>{KIND_WORDS[site.kind]}</dd>
            <dt class="text-muted">Feeder</dt>
            <dd>{site.feederCode}</dd>
          </dl>
          <p class="text-body">
            <a class="link" href="/sites/{site.nmi}?feeder={site.feederCode}">Open the site</a>
            ·
            <a class="link" href="/feeder?feeder={site.feederCode}">Open its feeder</a>
            ·
            <a class="link" href="/network?feeder={site.feederCode}">See its network</a>
          </p>
          <p class="text-body">
            <a
              class="link"
              href={withQuery(page.url, { site: null, sub: null })}
              data-sveltekit-noscroll>Back to what needs attention</a
            >
          </p>
        {:else if substation}
          <h2 id="selected" class="h-panel">{substation.name}</h2>
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
          <!-- Each of its feeders, and the way to its own pages. -->
          <ul class="text-body space-y-1">
            {#each substation.feederCodes as code (code)}
              <li>
                <span class="font-medium">{code}</span>:
                <a class="link" href="/feeder?feeder={code}" aria-label="Open feeder {code}"
                  >open the feeder</a
                >
                ·
                <a class="link" href="/network?feeder={code}" aria-label="See the network of {code}"
                  >see its network</a
                >
              </li>
            {/each}
          </ul>
          <p class="text-body">
            <a
              class="link"
              href={withQuery(page.url, { site: null, sub: null })}
              data-sveltekit-noscroll>Back to what needs attention</a
            >
          </p>
        {:else}
          <!-- Nothing chosen: what an operator should look at first. -->
          <h2 id="selected" class="h-panel">Needs attention</h2>
          {#if !asking}
            <Skeleton label="what needs attention" class="h-24 w-full" />
          {:else if asking.length === 0}
            <p class="text-muted text-body">
              Nothing does: no site is over or at its limit, no device has gone quiet and no
              backstop is in force. Choose a substation or a site, on the map or in the lists below,
              to see what it is doing.
            </p>
          {:else}
            <ul class="divide-rule -mx-1 divide-y">
              {#each asking.slice(0, LISTED) as row (row.kind + row.key)}
                <li class="px-1 py-2">
                  <div class="flex flex-wrap items-center gap-x-2 gap-y-1">
                    <a
                      class="link min-h-6 font-medium"
                      href={withQuery(page.url, selection(row.kind, row.key))}
                      data-sveltekit-noscroll>{row.name}</a
                    >
                    <StatusBadge level={row.status.level} label={row.status.label} />
                  </div>
                  <p class="text-muted text-label">{row.where}</p>
                  <LimitMeter use={row.use} level={row.status.level} empty="No reading" />
                </li>
              {/each}
            </ul>
            {#if asking.length > LISTED}
              <p class="text-muted text-label">
                And {asking.length - LISTED} more: every site is in the table below.
              </p>
            {/if}
          {/if}
        {/if}
      </section>
    </div>

    <section aria-labelledby="substations" class="space-y-2">
      <h2 id="substations" class="h-section">Substations</h2>
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
      <h2 id="sites" class="h-section">
        Sites {substation ? `of ${substation.name}` : region === "all" ? "" : `in ${region}`}
      </h2>
      <!-- A wide or long table scrolls inside its own box, under a heading
           that stays; the page does not scroll sideways. -->
      <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
      <div
        class="card max-h-[70vh] overflow-auto"
        tabindex="0"
        role="region"
        aria-label="Sites on the map"
      >
        <table class="data-table min-w-[800px]">
          <thead>
            <tr>
              <SortHeader key="nmi" label="NMI" {sort} fallback={BY_NMI} />
              <SortHeader key="feeder" label="Feeder" {sort} fallback={BY_NMI} />
              <SortHeader key="kind" label="Equipment" {sort} fallback={BY_NMI} />
              <SortHeader key="status" label="Status" {sort} fallback={BY_NMI} />
              <SortHeader key="export" label="Net export" {sort} fallback={BY_NMI} align="right" />
              <SortHeader key="limit" label="Export limit" {sort} fallback={BY_NMI} align="right" />
              <SortHeader key="use" label="Use of limit" {sort} fallback={BY_NMI} />
            </tr>
          </thead>
          <tbody>
            {#each rows as s (s.id)}
              <tr>
                <th scope="row" class="text-left font-medium">
                  <a
                    class="link inline-flex min-h-6 items-center"
                    href={withQuery(page.url, { site: s.nmi })}
                    aria-current={s.nmi === siteNmi ? "true" : undefined}
                    data-sveltekit-noscroll>{s.nmi}</a
                  >
                </th>
                <td>{s.feederCode}</td>
                <td>{KIND_WORDS[s.kind]}</td>
                <td>
                  <StatusBadge level={s.status.level} label={s.status.label} />
                </td>
                <td class="text-right">{kw(s.exportW)}</td>
                <td class="text-right">{kw(s.limitW)}</td>
                <td class="w-48">
                  <LimitMeter use={s.use} level={s.status.level} empty={noUse(s)} />
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    </section>
  {:else if !places.error}
    <Skeleton label="the map" class="h-80 w-full sm:h-[30rem]" />
  {/if}
</div>
