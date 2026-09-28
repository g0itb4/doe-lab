<script lang="ts">
  import { Code, ConnectError } from "@connectrpc/connect";
  import type { Alert } from "@doelab/gen/doelab/v1/alert_pb.js";
  import type { BackstopEvent } from "@doelab/gen/doelab/v1/backstop_pb.js";
  import { AlertKind, RunStatus } from "@doelab/gen/doelab/v1/common_pb.js";
  import type { EnvelopeRun } from "@doelab/gen/doelab/v1/envelope_run_pb.js";
  import type { Site } from "@doelab/gen/doelab/v1/site_pb.js";
  import { page } from "$app/state";
  import { alertTitle, severityLevel, severityWord, sortAlerts } from "$lib/alerts.ts";
  import { api, bearer } from "$lib/api.ts";
  import { clock } from "$lib/clock.svelte.ts";
  import BackstopControl from "$lib/components/BackstopControl.svelte";
  import EmptyState from "$lib/components/EmptyState.svelte";
  import ErrorState from "$lib/components/ErrorState.svelte";
  import Skeleton from "$lib/components/Skeleton.svelte";
  import StatusBadge from "$lib/components/StatusBadge.svelte";
  import { describeError } from "$lib/errors.ts";
  import { feeder } from "$lib/feeder.svelte.ts";
  import { ago, count, dayAndTime, kw } from "$lib/format.ts";
  import { operator } from "$lib/operator.svelte.ts";
  import { queryParam, withQuery } from "$lib/query.ts";
  import { Resource } from "$lib/resource.svelte.ts";
  import { enrolled } from "$lib/sites.ts";
  import type { Level } from "$lib/status.ts";
  import { date } from "$lib/time.ts";
  import { toasts } from "$lib/toast.svelte.ts";

  const zone = $derived(feeder.data?.timezone ?? "Australia/Sydney");

  // Which alerts: the open ones by default; the choice lives in the address.
  const FILTERS = { open: "Open", breaches: "Breaches", all: "All" } as const;
  type Filter = keyof typeof FILTERS;
  const filter = $derived.by((): Filter => {
    const value = queryParam(page.url, "alerts");
    return value !== null && value in FILTERS ? (value as Filter) : "open";
  });

  type Ops = { alerts: Alert[]; runs: EnvelopeRun[]; backstops: BackstopEvent[]; sites: Site[] };
  let ops = $state<Resource<Ops>>();
  let sites: Site[] | undefined;
  // How many alerts to ask for: a page at first, more on request.
  const PAGE = 25;
  let limit = $state(PAGE);

  $effect(() => {
    const id = feeder.data?.id;
    if (!id) return;
    const which = filter;
    const size = limit;
    const loaded = new Resource<Ops>(async (signal) => {
      // The sites do not change while the page is open: asked once.
      sites ??= (await api.sites.listSites({ feederId: id, pageSize: 500 }, { signal })).sites;
      const [alerts, runs, backstops] = await Promise.all([
        api.alerts.listAlerts(
          {
            feederId: id,
            openOnly: which === "open",
            kind: which === "breaches" ? AlertKind.CONSTRAINT_BREACH : undefined,
            pageSize: size,
          },
          { signal },
        ),
        api.runs.listEnvelopeRuns({ feederId: id, pageSize: 15 }, { signal }),
        api.backstops.listBackstopEvents({ feederId: id, pageSize: 10 }, { signal }),
      ]);
      return {
        alerts: alerts.alerts,
        runs: runs.envelopeRuns,
        backstops: backstops.backstopEvents,
        sites,
      };
    });
    ops = loaded;
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

  const alerts = $derived(ops?.data ? sortAlerts(ops.data.alerts) : []);
  const active = $derived(ops?.data?.backstops.find((b) => b.clearedAt === undefined));
  const nmiOf = $derived(new Map((ops?.data?.sites ?? []).map((s) => [s.id, s.nmi])));
  const enrolledSites = $derived((ops?.data?.sites ?? []).filter(enrolled).length);

  // Acknowledging says "someone has seen this"; it does not resolve it.
  let acknowledging = $state<string>();
  async function acknowledge(alert: Alert) {
    if (acknowledging) return;
    acknowledging = alert.id;
    try {
      await api.alerts.acknowledgeAlert({ id: alert.id }, bearer(operator.token));
      toasts.show("ok", "Alert acknowledged.");
      await ops?.load();
    } catch (e) {
      const code = ConnectError.from(e).code;
      toasts.show(
        "error",
        code === Code.Unauthenticated || code === Code.PermissionDenied
          ? "Acknowledging needs the operator token: enter it in the Backstop panel above."
          : describeError(e),
      );
    } finally {
      acknowledging = undefined;
    }
  }

  const runLevel: Partial<Record<RunStatus, Level>> = {
    [RunStatus.RUNNING]: "info",
    [RunStatus.COMPLETED]: "ok",
    [RunStatus.FAILED]: "critical",
  };
  const runWord: Partial<Record<RunStatus, string>> = {
    [RunStatus.RUNNING]: "Running",
    [RunStatus.COMPLETED]: "Completed",
    [RunStatus.FAILED]: "Failed",
  };
</script>

<svelte:head><title>Operations · doe-lab</title></svelte:head>

<div class="space-y-4">
  <h1 class="text-xl font-bold">Operations</h1>

  {#if (feeder.error && !feeder.data) || (ops?.error && !ops.data)}
    <ErrorState
      message={ops?.error ?? feeder.error ?? ""}
      onretry={() => (feeder.data ? ops?.load() : feeder.load())}
    />
  {:else if ops?.data && feeder.data}
    {#if ops.stale}
      <p class="text-muted text-sm">
        This page is the last that loaded; it may be out of date. It retries by itself.
      </p>
    {/if}

    <BackstopControl
      feederId={feeder.data.id}
      feederCode={feeder.data.code}
      sites={enrolledSites}
      {active}
      {zone}
      onchange={() => ops?.load()}
    />

    <section aria-labelledby="alerts-heading" class="space-y-2">
      <div class="flex flex-wrap items-center justify-between gap-2">
        <h2 id="alerts-heading" class="font-semibold">Alerts</h2>
        <nav aria-label="Which alerts">
          <ul class="border-control inline-flex overflow-hidden rounded-md border">
            {#each Object.entries(FILTERS) as [key, label] (key)}
              <li class="border-control border-l first:border-l-0">
                <a
                  href={withQuery(page.url, { alerts: key === "open" ? null : key })}
                  aria-current={key === filter ? "true" : undefined}
                  data-sveltekit-replacestate
                  data-sveltekit-noscroll
                  data-sveltekit-keepfocus
                  class="bg-surface text-muted hover:bg-sunken aria-[current=true]:bg-accent aria-[current=true]:text-accent-text inline-flex min-h-9 items-center px-3 text-sm font-medium"
                >
                  {label}
                </a>
              </li>
            {/each}
          </ul>
        </nav>
      </div>

      {#if alerts.length === 0}
        <EmptyState title={filter === "open" ? "No open alerts" : "No alerts"}>
          {filter === "open"
            ? "Every site is inside its limit and every device is reporting. Choose All to see the alerts that have been resolved."
            : "Nothing has been raised on this feeder yet."}
        </EmptyState>
      {:else}
        <ul class="card divide-rule divide-y">
          {#each alerts as alert (alert.id)}
            <li class="flex flex-wrap items-center gap-x-3 gap-y-1 px-3 py-2 text-sm">
              <StatusBadge
                level={severityLevel(alert.severity)}
                label={severityWord(alert.severity)}
              />
              <span class="min-w-0 flex-1">
                <span class="font-medium">{alertTitle(alert)}</span>
                at
                <a class="link" href="/sites/{nmiOf.get(alert.siteId) ?? ''}"
                  >{nmiOf.get(alert.siteId) ?? "a site"}</a
                >
                <span class="tabular text-muted block">
                  Opened {dayAndTime(date(alert.openedAt), zone)} ({ago(
                    date(alert.openedAt),
                    clock.now,
                  )}){#if alert.resolvedAt}, resolved {dayAndTime(
                      date(alert.resolvedAt),
                      zone,
                    )}{/if}{#if alert.acknowledgedAt}. Acknowledged by
                    {alert.acknowledgedBy}{/if}
                </span>
              </span>
              {#if !alert.acknowledgedAt && !alert.resolvedAt}
                <button
                  type="button"
                  class="btn"
                  disabled={acknowledging !== undefined}
                  onclick={() => acknowledge(alert)}
                >
                  {acknowledging === alert.id ? "Acknowledging…" : "Acknowledge"}
                </button>
              {/if}
            </li>
          {/each}
        </ul>
        {#if ops.data.alerts.length >= limit && limit < 500}
          <button type="button" class="btn" onclick={() => (limit += PAGE)}>Show {PAGE} more</button
          >
        {/if}
      {/if}
    </section>

    <section aria-labelledby="runs-heading" class="space-y-2">
      <h2 id="runs-heading" class="font-semibold">Engine runs</h2>
      {#if ops.data.runs.length === 0}
        <EmptyState title="The engine has not run">
          Start a run with <code>just engine</code>. Until then the sites have no envelopes and fall
          back to their default limit.
        </EmptyState>
      {:else}
        <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
        <div class="card overflow-x-auto" tabindex="0" role="region" aria-label="Engine runs">
          <table class="tabular w-full min-w-[640px] text-sm">
            <thead>
              <tr class="border-rule border-b text-left">
                <th scope="col" class="px-3 py-2 font-semibold">Horizon from</th>
                <th scope="col" class="px-3 py-2 font-semibold">Status</th>
                <th scope="col" class="px-3 py-2 text-right font-semibold">Sites</th>
                <th scope="col" class="px-3 py-2 text-right font-semibold">Envelopes</th>
                <th scope="col" class="px-3 py-2 text-right font-semibold">Took</th>
                <th scope="col" class="px-3 py-2 font-semibold">Ran</th>
              </tr>
            </thead>
            <tbody>
              {#each ops.data.runs as run (run.id)}
                <tr class="border-rule border-b align-top last:border-b-0">
                  <th scope="row" class="px-3 py-2 text-left font-normal whitespace-nowrap">
                    {dayAndTime(date(run.horizonFrom), zone)}
                  </th>
                  <td class="px-3 py-2">
                    <StatusBadge
                      level={runLevel[run.status] ?? "info"}
                      label={runWord[run.status] ?? "Unknown"}
                    />
                    {#if run.error}<p class="text-muted mt-1 max-w-md text-xs">{run.error}</p>{/if}
                  </td>
                  <td class="px-3 py-2 text-right">{count(run.siteCount)}</td>
                  <td class="px-3 py-2 text-right">{count(run.envelopeCount)}</td>
                  <td class="px-3 py-2 text-right"
                    >{run.durationMs === undefined ? "–" : `${count(run.durationMs)} ms`}</td
                  >
                  <td class="px-3 py-2 whitespace-nowrap">{ago(date(run.startedAt), new Date())}</td
                  >
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}
    </section>

    <section aria-labelledby="history-heading" class="space-y-2">
      <h2 id="history-heading" class="font-semibold">Backstop history</h2>
      {#if ops.data.backstops.length === 0}
        <EmptyState title="No backstop has been triggered"
          >The engine has been in control since the feeder was set up.</EmptyState
        >
      {:else}
        <ul class="card divide-rule divide-y">
          {#each ops.data.backstops as event (event.id)}
            <li class="px-3 py-2 text-sm">
              <span class="font-medium">{event.reason}</span>
              <span class="tabular text-muted block">
                Export held at {kw(event.exportLimitW)} from {dayAndTime(
                  date(event.triggeredAt),
                  zone,
                )} by {event.triggeredBy}{#if event.clearedAt}, cleared {dayAndTime(
                    date(event.clearedAt),
                    zone,
                  )} by {event.clearedBy}{:else}. Still active{/if}.
              </span>
            </li>
          {/each}
        </ul>
      {/if}
    </section>
  {:else}
    <Skeleton label="the backstop control" class="h-64 w-full" />
    <Skeleton label="the alerts" class="h-48 w-full" />
    <Skeleton label="the engine runs" class="h-64 w-full" />
  {/if}
</div>
