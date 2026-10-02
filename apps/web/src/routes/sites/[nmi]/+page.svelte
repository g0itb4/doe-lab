<script lang="ts">
  import { Code, ConnectError } from "@connectrpc/connect";
  import type { Alert } from "@doelab/gen/doelab/v1/alert_pb.js";
  import { BindingConstraint, DerType, EnvelopeSource } from "@doelab/gen/doelab/v1/common_pb.js";
  import type { Device } from "@doelab/gen/doelab/v1/device_pb.js";
  import type { Envelope } from "@doelab/gen/doelab/v1/envelope_pb.js";
  import type { Site } from "@doelab/gen/doelab/v1/site_pb.js";
  import type { GetSiteSeriesResponse } from "@doelab/gen/doelab/v1/telemetry_pb.js";
  import { goto } from "$app/navigation";
  import { page } from "$app/state";
  import { alertTitle, breachSpans, severityLevel, severityWord, sortAlerts } from "$lib/alerts.ts";
  import { api } from "$lib/api.ts";
  import { clock } from "$lib/clock.svelte.ts";
  import Chart from "$lib/components/Chart.svelte";
  import EmptyState from "$lib/components/EmptyState.svelte";
  import ErrorState from "$lib/components/ErrorState.svelte";
  import Kpi from "$lib/components/Kpi.svelte";
  import LimitMeter from "$lib/components/LimitMeter.svelte";
  import RangePicker from "$lib/components/RangePicker.svelte";
  import Skeleton from "$lib/components/Skeleton.svelte";
  import StatusBadge from "$lib/components/StatusBadge.svelte";
  import { feeder } from "$lib/feeder.svelte.ts";
  import { bindingWords, dayAndTime, exportSentence, kw } from "$lib/format.ts";
  import { useOf } from "$lib/limit.ts";
  import { queryParam, withQuery } from "$lib/query.ts";
  import { Resource } from "$lib/resource.svelte.ts";
  import { rangeKey, windowOf, zoomOf } from "$lib/series.ts";
  import { envelopeAt, siteChart } from "$lib/site.ts";
  import { enrolled, equipment, phaseName } from "$lib/sites.ts";
  import { date, timestamp } from "$lib/time.ts";

  const nmi = $derived(page.params.nmi ?? "");
  const range = $derived(rangeKey(queryParam(page.url, "range")));
  const zoom = $derived(zoomOf(queryParam(page.url, "from"), queryParam(page.url, "to")));
  const zone = $derived(feeder.data?.timezone ?? "Australia/Sydney");

  // The site itself; "missing" when the feeder has no such NMI.
  let site = $state<Resource<Site | "missing">>();
  type Detail = {
    envelopes: Envelope[];
    series: GetSiteSeriesResponse;
    alerts: Alert[];
    devices: Device[];
  };
  let detail = $state<Resource<Detail>>();

  $effect(() => {
    const wanted = nmi;
    const found = new Resource<Site | "missing">(async (signal) => {
      try {
        const res = await api.sites.getSite({ key: { case: "nmi", value: wanted } }, { signal });
        return res.site ?? "missing";
      } catch (e) {
        // Not on the feeder, or not an NMI at all: the same thing to a reader.
        const code = ConnectError.from(e).code;
        if (code === Code.NotFound || code === Code.InvalidArgument) return "missing";
        throw e;
      }
    });
    site = found;
    detail = undefined;
    void found.load();
    return () => found.cancel();
  });

  $effect(() => {
    const s = site?.data;
    if (!s || s === "missing") return;
    const key = range;
    const loaded = new Resource<Detail>(async (signal) => {
      const w = windowOf(key, clock.nowSeconds());
      const [from, to] = [timestamp(w.from), timestamp(w.to)];
      const [envelopes, series, alerts, devices] = await Promise.all([
        api.envelopes.listEnvelopes({ siteId: s.id, from, to, pageSize: 2000 }, { signal }),
        api.telemetry.getSiteSeries({ siteId: s.id, from, to }, { signal }),
        api.alerts.listAlerts({ feederId: s.feederId, siteId: s.id, pageSize: 100 }, { signal }),
        api.devices.listDevices({ siteId: s.id, pageSize: 50 }, { signal }),
      ]);
      return {
        envelopes: envelopes.envelopes,
        series,
        alerts: alerts.alerts,
        devices: devices.devices,
      };
    });
    detail = loaded;
    void loaded.load();
    const every = Math.max(5000, 300_000 / clock.speed);
    const timer = setInterval(() => {
      if (document.visibilityState !== "hidden") void loaded.load();
    }, every);
    return () => {
      clearInterval(timer);
      loaded.cancel();
    };
  });

  // Every minute of feeder time is enough for the chart's "now" line.
  const nowSeconds = $derived(Math.floor(clock.now.getTime() / 60_000) * 60);
  const chart = $derived(
    detail?.data ? siteChart(detail.data.envelopes, detail.data.series, zone) : undefined,
  );
  const current = $derived(
    detail?.data ? envelopeAt(detail.data.envelopes, nowSeconds) : undefined,
  );
  const spans = $derived(detail?.data ? breachSpans(detail.data.alerts, nowSeconds) : []);
  const alerts = $derived(detail?.data ? sortAlerts(detail.data.alerts) : []);
  const latest = $derived(detail?.data?.series.power.at(-1));
  // The last reading against the limit in force now.
  const use = $derived(useOf(latest?.avgNetExportW, current?.exportLimitW, current?.importLimitW));
  const active = $derived(
    (detail?.data?.envelopes ?? []).filter((e) => e.supersededAt === undefined),
  );

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

  const deviceWords: Partial<Record<DerType, string>> = {
    [DerType.SOLAR]: "Solar inverter",
    [DerType.BATTERY]: "Battery",
    [DerType.EV]: "EV charger",
  };
  const limitedBy = (e: Envelope) =>
    e.source === EnvelopeSource.BACKSTOP
      ? "an operator's backstop"
      : e.exportBinding === BindingConstraint.UNSPECIFIED
        ? "–"
        : bindingWords(e.exportBinding, e.exportBindingElement);
</script>

<svelte:head><title>Site {nmi} · doe-lab</title></svelte:head>

<div class="space-y-4">
  <p class="text-sm"><a class="link" href="/sites">All sites</a></p>

  {#if site?.error && !site.data}
    <h1 class="text-xl font-bold">Site {nmi}</h1>
    <ErrorState message={site.error} onretry={() => site?.load()} />
  {:else if site?.data === "missing"}
    <h1 class="text-xl font-bold">Site {nmi}</h1>
    <EmptyState title="There is no site with this NMI">
      "{nmi}" is not a connection point of this feeder. Check the NMI, or
      <a class="link" href="/sites">find the site in the list</a>.
    </EmptyState>
  {:else if site?.data}
    {@const s = site.data}
    <div>
      <h1 class="text-xl font-bold">Site {s.nmi}</h1>
      <p class="text-muted text-sm">
        {s.name}, phase {phaseName(s.phase)}. {equipment(s)}.
        {enrolled(s) ? "Takes part in envelopes." : "Passive: it is forecast, not controlled."}
      </p>
    </div>

    <!-- The answer first: what may this site do now, and why. -->
    <section aria-labelledby="now-heading" class="card p-3">
      <h2 id="now-heading" class="sr-only">Now</h2>
      {#if detail?.data}
        {#if current}
          <p class="text-base font-semibold">
            {exportSentence(
              current.exportLimitW,
              current.exportBinding,
              current.exportBindingElement,
              current.source === EnvelopeSource.BACKSTOP,
            )}
          </p>
          <p class="text-muted mt-1 text-sm">
            Until {dayAndTime(date(current.validTo), zone)}. Import up to
            <abbr title="CSIP-AUS opModImpLimW">{kw(current.importLimitW)}</abbr>; the export limit
            is
            <abbr title="CSIP-AUS opModExpLimW">opModExpLimW</abbr> on the wire.
          </p>
        {:else if enrolled(s)}
          <p class="text-base font-semibold">No envelope is in force for this site now.</p>
          <p class="text-muted mt-1 text-sm">
            Its device falls back to its default export limit until the engine runs.
          </p>
        {:else}
          <p class="text-base font-semibold">This site is not given envelopes.</p>
          <p class="text-muted mt-1 text-sm">
            The engine counts its forecast load and solar when it sets the limits of the others.
          </p>
        {/if}
      {:else if detail?.error}
        <ErrorState message={detail.error} onretry={() => detail?.load()} />
      {:else}
        <Skeleton label="the site's envelope" class="h-12 w-full" />
      {/if}
    </section>

    {#if detail?.data}
      <!-- The track of the bar ends at the connection's own limit, that way. -->
      {@const capW = use?.direction === "import" ? s.importCapW : s.exportCapW}
      <dl class="grid grid-cols-2 gap-2 lg:grid-cols-4">
        <Kpi
          class="col-span-2"
          label={use ? `Net ${use.direction} against its limit` : "Net export, last reading"}
          value={use
            ? `${kw(use.usedW)} of ${kw(use.limitW)}`
            : latest
              ? kw(latest.avgNetExportW)
              : "–"}
          hint="{latest
            ? dayAndTime(date(latest.bucket), zone)
            : 'no telemetry in this range'} · connection limit {kw(capW)}"
        >
          <LimitMeter {use} {capW} empty={latest ? "No limit in force" : "No reading"} />
        </Kpi>
        <Kpi
          label="Solar"
          value={s.pvKw > 0 ? `${s.pvKw.toFixed(1)} kW` : "–"}
          hint="{kw(s.inverterKva * 1000, 1)} inverter"
        />
        <Kpi
          label="Open alerts"
          value={String(alerts.filter((a) => a.resolvedAt === undefined).length)}
          hint="{alerts.length} in total"
        />
      </dl>
    {/if}

    <section aria-labelledby="chart-heading" class="space-y-3">
      <div class="flex flex-wrap items-center justify-between gap-2">
        <h2 id="chart-heading" class="font-semibold">The site through time</h2>
        <div class="flex flex-wrap items-center gap-2">
          {#if zoom}
            <button type="button" class="btn" onclick={() => setZoom(undefined)}>Reset zoom</button>
          {/if}
          <RangePicker current={range} />
        </div>
      </div>
      {#if chart && chart.x.length > 0}
        {#if detail?.stale}
          <p class="text-muted text-sm">
            This chart is the last that loaded; it may be out of date.
          </p>
        {/if}
        <Chart
          title="Envelope, forecast and telemetry"
          summary="{chart.summary}{spans.length > 0
            ? ` ${spans.length === 1 ? '1 breach is' : `${spans.length} breaches are`} marked.`
            : ''}"
          x={chart.x}
          series={chart.series}
          format={(v) => kw(v)}
          axisFormat={(v) => kw(v, 0)}
          {zone}
          {zoom}
          onzoom={setZoom}
          now={nowSeconds}
          {spans}
          height={280}
        />
      {:else if chart}
        <EmptyState title="Nothing to chart for this range">
          This site has no envelope, forecast or telemetry in this range. Choose a range that
          includes now, or run the engine with <code>just engine</code>.
        </EmptyState>
      {:else if !detail?.error}
        <Skeleton label="the chart" class="h-[400px] w-full" />
      {/if}
    </section>

    {#if detail?.data}
      <section aria-labelledby="alerts-heading">
        <h2 id="alerts-heading" class="mb-2 font-semibold">Alerts at this site</h2>
        {#if alerts.length === 0}
          <EmptyState title="No alerts"
            >This site has stayed inside its limits and its devices have kept reporting.</EmptyState
          >
        {:else}
          <ul class="card divide-rule divide-y">
            {#each alerts as alert (alert.id)}
              <li class="flex flex-wrap items-center gap-x-3 gap-y-1 px-3 py-2 text-sm">
                <StatusBadge
                  level={severityLevel(alert.severity)}
                  label={severityWord(alert.severity)}
                />
                <span class="min-w-0 flex-1 font-medium">{alertTitle(alert)}</span>
                <span class="tabular text-muted">
                  {dayAndTime(date(alert.openedAt), zone)} to
                  {alert.resolvedAt ? dayAndTime(date(alert.resolvedAt), zone) : "now (open)"}
                </span>
              </li>
            {/each}
          </ul>
        {/if}
      </section>

      <!-- Detail, one click away. -->
      <details class="card p-3">
        <summary class="cursor-pointer font-semibold"
          >Envelopes in this range ({active.length})</summary
        >
        <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
        <div
          class="mt-2 max-h-96 overflow-auto"
          tabindex="0"
          role="region"
          aria-label="Envelopes in this range"
        >
          <table class="tabular w-full min-w-[560px] text-sm">
            <thead class="bg-surface sticky top-0">
              <tr class="border-rule border-b text-left">
                <th scope="col" class="py-1 pr-3 font-semibold">From</th>
                <th scope="col" class="py-1 pr-3 text-right font-semibold">Export limit</th>
                <th scope="col" class="py-1 pr-3 font-semibold">Limited by</th>
                <th scope="col" class="py-1 pr-3 text-right font-semibold">Import limit</th>
              </tr>
            </thead>
            <tbody>
              {#each active as e (e.id)}
                <tr class="border-rule border-b">
                  <th scope="row" class="py-1 pr-3 text-left font-normal whitespace-nowrap"
                    >{dayAndTime(date(e.validFrom), zone)}</th
                  >
                  <td class="py-1 pr-3 text-right">{kw(e.exportLimitW)}</td>
                  <td class="py-1 pr-3">{limitedBy(e)}</td>
                  <td class="py-1 pr-3 text-right">{kw(e.importLimitW)}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      </details>

      <section aria-labelledby="devices-heading">
        <h2 id="devices-heading" class="mb-2 font-semibold">Devices</h2>
        {#if detail.data.devices.length === 0}
          <EmptyState title="No devices"
            >Nothing at this site reports telemetry or takes an envelope.</EmptyState
          >
        {:else}
          <ul class="flex flex-wrap gap-2">
            {#each detail.data.devices as device (device.id)}
              <li class="card px-3 py-2 text-sm">
                <span class="font-medium">{deviceWords[device.derType] ?? "Device"}</span><span
                  class="tabular text-muted">, rated {kw(device.ratedW)}</span
                >
              </li>
            {/each}
          </ul>
        {/if}
      </section>
    {/if}
  {:else}
    <h1 class="text-xl font-bold">Site {nmi}</h1>
    <Skeleton label="the site" class="h-40 w-full" />
  {/if}
</div>
