<script lang="ts">
  import type { Envelope } from "@doelab/gen/doelab/v1/envelope_pb.js";
  import { RunStatus } from "@doelab/gen/doelab/v1/common_pb.js";
  import type {
    FleetSummary,
    GetDailyReportResponse,
    GetFeederSeriesResponse,
  } from "@doelab/gen/doelab/v1/telemetry_pb.js";
  import { goto } from "$app/navigation";
  import { page } from "$app/state";
  import { api } from "$lib/api.ts";
  import { clock } from "$lib/clock.svelte.ts";
  import Chart from "$lib/components/Chart.svelte";
  import EmptyState from "$lib/components/EmptyState.svelte";
  import ErrorState from "$lib/components/ErrorState.svelte";
  import Kpi from "$lib/components/Kpi.svelte";
  import LimitMeter from "$lib/components/LimitMeter.svelte";
  import RangePicker from "$lib/components/RangePicker.svelte";
  import Skeleton from "$lib/components/Skeleton.svelte";
  import StaleBanner from "$lib/components/StaleBanner.svelte";
  import Sparkline from "$lib/components/Sparkline.svelte";
  import StatusBadge from "$lib/components/StatusBadge.svelte";
  import { feeder } from "$lib/feeder.svelte.ts";
  import { ago, count, kw, kwh, percent, volts } from "$lib/format.ts";
  import { useOf } from "$lib/limit.ts";
  import { Live } from "$lib/live.svelte.ts";
  import { overviewCharts } from "$lib/overview.ts";
  import { poll } from "$lib/poll.ts";
  import { queryParam, withQuery } from "$lib/query.ts";
  import { Resource } from "$lib/resource.svelte.ts";
  import { hiddenOf, rangeKey, windowOf, withHidden, zoomOf } from "$lib/series.ts";
  import { upToLast } from "$lib/sparkline.ts";
  import { feederStatus } from "$lib/status.ts";
  import { date, timestamp } from "$lib/time.ts";

  const range = $derived(rangeKey(queryParam(page.url, "range")));
  const zoom = $derived(zoomOf(queryParam(page.url, "from"), queryParam(page.url, "to")));
  const zone = $derived(feeder.data?.timezone ?? "Australia/Sydney");
  const nominalV = $derived(feeder.data?.nominalVoltageV ?? 230);

  // The live figures of the fleet.
  let fleet = $state<Live<FleetSummary>>();
  // The feeder through time, and the envelope in force at one enrolled site:
  // every site of an interval shares what binds it.
  type Series = { series: GetFeederSeriesResponse; sample: Envelope | undefined };
  let series = $state<Resource<Series>>();
  let report = $state<Resource<GetDailyReportResponse>>();
  let sampleSite: string | undefined;

  $effect(() => {
    const id = feeder.data?.id;
    // Not before the clock has settled: the window is in feeder time.
    if (!id || !clock.settled) return;
    const key = range;

    const live = new Live<FleetSummary>(async function* (signal) {
      for await (const res of api.telemetry.watchFleet({ feederId: id }, { signal })) {
        if (res.summary) yield res.summary;
      }
    });
    const feederSeries = new Resource<Series>(async (signal) => {
      const w = windowOf(key, clock.nowSeconds());
      if (sampleSite === undefined) {
        const sites = await api.sites.listSites({ feederId: id, pageSize: 500 }, { signal });
        sampleSite = sites.sites.find((s) => s.exportCapW > 0)?.id ?? "";
      }
      const [res, current] = await Promise.all([
        api.telemetry.getFeederSeries(
          { feederId: id, from: timestamp(w.from), to: timestamp(w.to) },
          { signal },
        ),
        sampleSite
          ? api.envelopes.getCurrentEnvelope(
              { site: { case: "siteId", value: sampleSite } },
              { signal },
            )
          : undefined,
      ]);
      return { series: res, sample: current?.envelope };
    });
    const daily = new Resource<GetDailyReportResponse>((signal) =>
      api.telemetry.getDailyReport(
        { feederId: id, day: timestamp(clock.nowSeconds()) },
        { signal },
      ),
    );
    fleet = live;
    series = feederSeries;
    report = daily;

    const stop = live.start();
    void feederSeries.load();
    void daily.load();
    // The charts follow feeder time: every five minutes of it, and at most
    // every five seconds.
    const stopPolling = poll(() => {
      void feederSeries.load();
      void daily.load();
    }, 300_000);
    return () => {
      stop();
      stopPolling();
      feederSeries.cancel();
      daily.cancel();
    };
  });

  const summary = $derived(fleet?.value);
  // The export of the sites that have a limit, against the sum of those
  // limits: the sites that take no part are in neither.
  const use = $derived(
    summary ? useOf(summary.controlledExportW, summary.exportLimitW, undefined) : undefined,
  );
  // The status waits for the envelope that says what binds: shown a moment
  // earlier it would say "Normal" and then change under the reader's eyes.
  const status = $derived(
    summary && (series?.data || series?.error)
      ? feederStatus(summary, series?.data?.sample)
      : undefined,
  );
  // What the fleet has exported over the range on show, up to now: the shape
  // of the figure beside it.
  const measured = $derived(
    upToLast(series?.data?.series.points.map((p) => p.measuredExportW) ?? []),
  );
  const charts = $derived(
    series?.data ? overviewCharts(series.data.series, nominalV, zone) : undefined,
  );
  // One "now" for the three charts. A line on a chart of a day does not move
  // by the second: every five minutes of feeder time is enough.
  const nowSeconds = $derived(Math.floor(clock.now.getTime() / 300_000) * 300);

  // The series a reader has hidden, chart by chart, in the address like the
  // range and the zoom: a view that is shared is the view that was made.
  const hide = $derived(queryParam(page.url, "hide"));
  function setHidden(chart: string, hidden: number[]) {
    void goto(withQuery(page.url, { hide: withHidden(hide, chart, hidden) }), {
      replaceState: true,
      keepFocus: true,
      noScroll: true,
    });
  }

  // An instant pinned on the charts, in the address: its values stay in the
  // legends, and a link leads to the network as it was then.
  const pin = $derived.by(() => {
    const value = Number(queryParam(page.url, "pin"));
    return Number.isInteger(value) && value > 0 ? value : undefined;
  });
  function setPin(time: number | undefined) {
    void goto(withQuery(page.url, { pin: time === undefined ? null : String(time) }), {
      replaceState: true,
      keepFocus: true,
      noScroll: true,
    });
  }
  const networkAt = (time: number) => ({
    href: `/network?feeder=${feeder.data?.code ?? ""}&at=${Math.floor(time / 1800) * 1800}`,
    label: "See the network then",
  });
  // Two hours either side of now, on whole minutes.
  function aroundNow() {
    const minute = Math.floor(clock.nowSeconds() / 60) * 60;
    setZoom([minute - 7200, minute + 7200]);
  }

  function setZoom(next: [number, number] | undefined) {
    const changes = next
      ? { from: String(next[0]), to: String(next[1]) }
      : { from: null, to: null };
    void goto(withQuery(page.url, changes), {
      replaceState: true,
      keepFocus: true,
      noScroll: true,
    });
  }

  // For a screen reader: the figures in a sentence, at most every fifteen
  // seconds, and a new alert at once.
  let spoken = $state("");
  let spokenAt = 0;
  let alertText = $state("");
  let knownAlerts: number | undefined;
  $effect(() => {
    if (!summary || !status) return;
    if (knownAlerts !== undefined && summary.openAlerts > knownAlerts) {
      alertText = `A new alert has opened. ${summary.openAlerts} alerts are open.`;
    }
    knownAlerts = summary.openAlerts;
    if (Date.now() - spokenAt < 15_000) return;
    spokenAt = Date.now();
    spoken = `${status.label}. Export ${kw(summary.controlledExportW)} of ${kw(summary.exportLimitW)} allowed. ${summary.reportingSites} of ${summary.enrolledSites} sites reporting. ${summary.openAlerts} open alerts.`;
  });

  const runWords: Partial<Record<RunStatus, string>> = {
    [RunStatus.RUNNING]: "running",
    [RunStatus.COMPLETED]: "completed",
    [RunStatus.FAILED]: "failed",
  };
  const share = (part: number, whole: number) => (whole > 0 ? percent((part / whole) * 100) : "–");
</script>

<svelte:head><title>Feeder overview · doe-lab</title></svelte:head>

<!-- A part of a whole, as a bar: the hint under it has the number. -->
{#snippet shareBar(part: number, whole: number)}
  <div class="bg-sunken border-rule h-2 overflow-hidden rounded-full border" aria-hidden="true">
    <div
      class="bg-accent h-full rounded-full"
      style:width="{whole > 0 ? Math.min(100, (100 * part) / whole) : 0}%"
    ></div>
  </div>
{/snippet}

<div class="space-y-4">
  <div class="flex flex-wrap items-baseline justify-between gap-2">
    <h1 class="h-page">
      Feeder overview{#if feeder.data}<span class="text-muted font-normal"
          >, {feeder.data.code}</span
        >{/if}
    </h1>
  </div>

  {#if feeder.error && !feeder.data}
    <ErrorState message={feeder.error} onretry={() => feeder.load()} />
  {:else}
    <!-- The answer first: is the feeder safe now? -->
    <section aria-labelledby="status-heading" class="card p-3">
      <h2 id="status-heading" class="sr-only">Status now</h2>
      {#if status && summary}
        <!-- As tall as its skeleton at least, so nothing below moves when it arrives. -->
        <div class="min-h-[11.5rem] sm:min-h-16">
          <div class="flex flex-wrap items-center gap-x-4 gap-y-2">
            <StatusBadge level={status.level} label={status.label} large />
            <p class="min-w-0 flex-1 text-sm">{status.detail}</p>
          </div>
          <p class="text-muted mt-2 text-sm">
            {#if summary.latestRunAt}
              Last engine run {ago(date(summary.latestRunAt), new Date())}
              ({runWords[summary.latestRunStatus ?? RunStatus.UNSPECIFIED] ?? "unknown"}).
            {:else}
              The engine has not run yet.
            {/if}
            <a class="link" href="/operations">
              {summary.openAlerts === 1
                ? "1 open alert"
                : `${count(summary.openAlerts)} open alerts`}
            </a>
          </p>
        </div>
      {:else}
        <Skeleton label="the feeder's status" class="h-[11.5rem] w-full sm:h-16" />
      {/if}
    </section>

    <StaleBanner stale={fleet?.stale ?? false} />
    <p class="sr-only" aria-live="polite">{spoken}</p>
    <p class="sr-only" role="alert">{alertText}</p>

    <section aria-labelledby="fleet-heading">
      <h2 id="fleet-heading" class="h-section mb-2">The fleet now</h2>
      {#if summary}
        <!-- Every tile as tall as the one with a bar, as the skeletons are. -->
        <dl class="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-6 [&>div]:min-h-[7.5rem]">
          <!-- Like against like: only the sites that have a limit are in
               either number. What every site exports is the next tile. -->
          <Kpi
            label="Controlled export"
            value={kw(summary.controlledExportW)}
            hint="of {kw(summary.exportLimitW)} allowed"
          >
            <LimitMeter {use} />
          </Kpi>
          <Kpi
            label="Export, all sites"
            value={kw(summary.exportW)}
            hint="import {kw(summary.importW)}"
          >
            <!-- Always there, with a line once the series has loaded: the tile
                 does not grow when it does. -->
            <Sparkline values={measured} format={(v) => kw(v)} label="Measured export" />
          </Kpi>
          <Kpi
            label="Sites reporting"
            value="{summary.reportingSites} of {summary.enrolledSites}"
            hint="{summary.devicesOnline} of {summary.devices} devices online"
          />
          <Kpi
            label="Sites over limit"
            value={count(summary.sitesOverLimit)}
            hint={summary.sitesOverLimit > 0
              ? "exporting above the limit"
              : "all inside their limits"}
          />
          <Kpi
            label="Open alerts"
            value={count(summary.openAlerts)}
            hint={summary.openAlerts > 0 ? "see Operations" : "nothing to do"}
          />
          <Kpi
            label="Backstop"
            value={summary.backstopEventId ? "Active" : "Off"}
            hint={summary.backstopEventId ? "envelopes overridden" : "engine in control"}
          />
        </dl>
      {:else}
        <div class="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-6">
          {#each { length: 6 }, i (i)}
            <Skeleton label="fleet figures" class="h-[7.5rem] w-full" />
          {/each}
        </div>
      {/if}
    </section>

    <section aria-labelledby="charts-heading" class="space-y-3">
      <div class="flex flex-wrap items-center justify-between gap-2">
        <h2 id="charts-heading" class="h-section">The feeder through time</h2>
        <div class="flex flex-wrap items-center gap-2">
          {#if zoom}
            <button type="button" class="btn" onclick={() => setZoom(undefined)}>Reset zoom</button>
          {/if}
          <button type="button" class="btn" onclick={aroundNow}>Now ± 2 h</button>
          <RangePicker current={range} />
        </div>
      </div>

      {#if series?.error}
        <ErrorState message={series.error} onretry={() => series?.load()} />
      {/if}
      {#if charts && charts.load.x.length > 0}
        {#if series?.stale}
          <p class="text-muted text-sm">
            These charts are the last that loaded; they may be out of date.
          </p>
        {/if}
        <div class="grid gap-3 lg:grid-cols-2">
          <div class="lg:col-span-2">
            <Chart
              title="Export: allowed and measured"
              summary={charts.exports.summary}
              x={charts.exports.x}
              series={charts.exports.series}
              hidden={hiddenOf(hide, "export")}
              onhide={(hidden) => setHidden("export", hidden)}
              format={(v) => kw(v)}
              axisFormat={(v) => kw(v, 0)}
              {zone}
              syncKey="feeder"
              {zoom}
              onzoom={setZoom}
              now={nowSeconds}
              {pin}
              onpin={setPin}
              pinHref={networkAt}
            />
          </div>
          <Chart
            title="Customer voltage"
            summary={charts.voltage.summary}
            x={charts.voltage.x}
            series={charts.voltage.series}
            hidden={hiddenOf(hide, "voltage")}
            onhide={(hidden) => setHidden("voltage", hidden)}
            format={(v) => volts(v / nominalV, nominalV)}
            axisFormat={(v) => `${Math.round(v)}\u00a0V`}
            {zone}
            syncKey="feeder"
            {zoom}
            onzoom={setZoom}
            now={nowSeconds}
            {pin}
            onpin={setPin}
            pinHref={networkAt}
          />
          <Chart
            title="Power through the transformer"
            summary={charts.load.summary}
            x={charts.load.x}
            series={charts.load.series}
            hidden={hiddenOf(hide, "load")}
            onhide={(hidden) => setHidden("load", hidden)}
            format={(v) => kw(v)}
            axisFormat={(v) => kw(v, 0)}
            {zone}
            syncKey="feeder"
            {zoom}
            onzoom={setZoom}
            now={nowSeconds}
            {pin}
            onpin={setPin}
            pinHref={networkAt}
          />
        </div>
      {:else if charts}
        <EmptyState title="No engine run covers this range">
          The charts are drawn from the engine's runs. Start one with <code>just engine</code>, or
          choose a range that includes now.
        </EmptyState>
      {:else if !series?.error}
        <div class="grid gap-3 lg:grid-cols-2">
          <div class="lg:col-span-2"><Skeleton label="charts" class="h-[330px] w-full" /></div>
          <Skeleton label="charts" class="h-[330px] w-full" />
          <Skeleton label="charts" class="h-[330px] w-full" />
        </div>
      {/if}
    </section>

    <section aria-labelledby="day-heading">
      <h2 id="day-heading" class="h-section">Today, by the forecast</h2>
      <p class="text-muted mb-2 text-sm">
        What the solar homes could export over the feeder's day, and how much of it each kind of
        limit lets out.
      </p>
      {#if report?.data}
        {@const r = report.data}
        <dl class="grid grid-cols-2 gap-2 lg:grid-cols-4 [&>div]:min-h-[6.75rem]">
          <Kpi
            label="Could be exported"
            value={kwh(r.potentialExportKwh)}
            hint="with no limit at all"
          />
          <!-- Each kind of limit as a share of what could be exported: a bar
               beside the words that say the same. -->
          <Kpi
            label="Let out by the envelopes"
            value={kwh(r.envelopeExportKwh)}
            hint="{share(
              r.envelopeExportKwh,
              r.potentialExportKwh,
            )} of it, inside the network's limits"
          >
            {@render shareBar(r.envelopeExportKwh, r.potentialExportKwh)}
          </Kpi>
          <Kpi
            label="Let out by a fixed {kw(r.staticLimitW)} limit"
            value={kwh(r.staticExportKwh)}
            hint="{share(r.staticExportKwh, r.potentialExportKwh)} of it"
          >
            {@render shareBar(r.staticExportKwh, r.potentialExportKwh)}
          </Kpi>
          <Kpi
            label="Intervals a fixed limit breaks a limit"
            value="{r.staticViolationIntervals} of {r.intervals}"
            hint="if every site exported at it"
          />
        </dl>
      {:else if report?.error}
        <ErrorState message={report.error} onretry={() => report?.load()} />
      {:else}
        <div class="grid grid-cols-2 gap-2 lg:grid-cols-4">
          {#each { length: 4 }, i (i)}
            <Skeleton label="today's figures" class="h-[6.75rem] w-full" />
          {/each}
        </div>
      {/if}
    </section>
  {/if}
</div>
