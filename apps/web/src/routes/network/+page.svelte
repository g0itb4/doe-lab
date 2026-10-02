<script lang="ts">
  import type { Envelope } from "@doelab/gen/doelab/v1/envelope_pb.js";
  import type { GetFeederStateResponse } from "@doelab/gen/doelab/v1/telemetry_pb.js";
  import { goto } from "$app/navigation";
  import { page } from "$app/state";
  import { api } from "$lib/api.ts";
  import { clock } from "$lib/clock.svelte.ts";
  import EmptyState from "$lib/components/EmptyState.svelte";
  import ErrorState from "$lib/components/ErrorState.svelte";
  import FeederSchematic from "$lib/components/FeederSchematic.svelte";
  import Skeleton from "$lib/components/Skeleton.svelte";
  import StaleBanner from "$lib/components/StaleBanner.svelte";
  import StatusBadge from "$lib/components/StatusBadge.svelte";
  import { feeder } from "$lib/feeder.svelte.ts";
  import { clockTime, volts } from "$lib/format.ts";
  import { atOf, POINT_WORDS, pointOf, POINTS, schematic, stepFrom } from "$lib/map/schematic.ts";
  import { loadTopology, type Topology } from "$lib/map/topology.ts";
  import { queryParam, withQuery } from "$lib/query.ts";
  import { Resource } from "$lib/resource.svelte.ts";
  import { date, timestamp } from "$lib/time.ts";

  const zone = $derived(feeder.data?.timezone ?? "Australia/Sydney");
  const nominalV = $derived(feeder.data?.nominalVoltageV ?? 230);
  const point = $derived(pointOf(queryParam(page.url, "point")));
  const selected = $derived(queryParam(page.url, "mark") ?? "");
  // The instant on show, in the address: unset is now, and follows the clock.
  const at = $derived(atOf(queryParam(page.url, "at")));

  // The network, which does not change, and what the engine solved for the
  // interval in force, which is asked again as feeder time passes.
  // With it, the envelope in force at one site that takes part: every site of
  // an interval shares what binds it.
  type State = { feeder: GetFeederStateResponse; sample: Envelope | undefined };
  let topology = $state<Resource<Topology>>();
  let solved = $state<Resource<State>>();

  $effect(() => {
    const id = feeder.data?.id;
    if (!id) return;
    const network = new Resource<Topology>((signal) => loadTopology(id, signal));
    let sampleSite: string | undefined;
    const state = new Resource<State>(async (signal) => {
      const when = instant === undefined ? undefined : timestamp(instant);
      if (sampleSite === undefined) {
        const sites = await api.sites.listSites({ feederId: id, pageSize: 500 }, { signal });
        sampleSite = sites.sites.find((s) => s.exportCapW > 0)?.id ?? "";
      }
      const [feederState, current] = await Promise.all([
        api.telemetry.getFeederState({ feederId: id, at: when }, { signal }),
        sampleSite
          ? api.envelopes.getCurrentEnvelope(
              { site: { case: "siteId", value: sampleSite }, at: when },
              { signal },
            )
          : undefined,
      ]);
      return { feeder: feederState, sample: current?.envelope };
    });
    topology = network;
    solved = state;
    void network.load();
    // A minute of feeder time, and at most every five seconds.
    const every = Math.max(5000, 60_000 / clock.speed);
    const timer = setInterval(() => {
      if (document.visibilityState !== "hidden") void state.load();
    }, every);
    return () => {
      clearInterval(timer);
      network.cancel();
      state.cancel();
    };
  });

  // The state is asked for again when the instant changes. What is on screen
  // stays until the answer arrives: the drawing does not blink between two
  // half hours.
  let instant: number | undefined;
  $effect(() => {
    instant = at;
    void solved?.load();
  });

  const view = $derived(
    topology?.data
      ? schematic(
          topology.data.nodes,
          topology.data.lines,
          topology.data.sites,
          solved?.data?.feeder,
          solved?.data?.sample,
          point,
          nominalV,
        )
      : undefined,
  );
  const bus = $derived(view?.nodes.find((n) => n.id === selected));
  const line = $derived(view?.lines.find((l) => l.id === selected));
  const interval = $derived(solved?.data?.feeder.nodes[0]);

  function select(_kind: "bus" | "line", id: string) {
    void goto(withQuery(page.url, { mark: id }), { keepFocus: true, noScroll: true });
  }
</script>

<svelte:head><title>Network · doe-lab</title></svelte:head>

<div class="space-y-4">
  <div>
    <h1 class="text-2xl font-bold tracking-tight">
      Network{#if feeder.data}: {feeder.data.code}{/if}
    </h1>
    <p class="text-muted max-w-prose text-sm">
      The feeder as the engine solved it, from the transformer on the left. A bus sits as far to the
      right as there is cable to it, which is what decides its voltage.
    </p>
  </div>

  <StaleBanner stale={solved?.stale ?? false} what="The solved state" />

  {#if (feeder.error && !feeder.data) || (topology?.error && !topology.data)}
    <ErrorState
      message={topology?.error ?? feeder.error ?? ""}
      onretry={() => (feeder.data ? topology?.load() : feeder.load())}
    />
  {:else if solved?.error && !solved.data}
    <ErrorState message={solved.error} onretry={() => solved?.load()} />
  {/if}

  {#if view}
    <!-- What limits the feeder, in words, before the drawing. Always a line
         tall, so the drawing does not move when the state arrives. -->
    <p class="flex min-h-7 flex-wrap items-center gap-2 text-sm" aria-live="polite">
      {#if !solved?.data}
        &nbsp;
      {:else if !view.solved}
        <StatusBadge level="info" label="Not solved" />
        The engine has not solved this half hour for this feeder yet.
      {:else if view.binding}
        <StatusBadge level="warn" label="Constrained" />
        Export is limited by {view.binding.words}{view.binding.nodeId || view.binding.lineId
          ? ", ringed on the drawing"
          : ""}.
      {:else}
        <StatusBadge level="ok" label="Normal" />
        Nothing on the network limits export now.
      {/if}
      {#if interval}
        <span class="text-muted tabular">
          {clockTime(date(interval.validFrom), zone)} to {clockTime(date(interval.validTo), zone)}
        </span>
      {/if}
    </p>

    <nav aria-label="Time">
      <ul class="flex flex-wrap gap-1">
        <li>
          <a
            href={withQuery(page.url, { at: String(stepFrom(at, clock.nowSeconds(), -1)) })}
            class="btn"
            data-sveltekit-noscroll>30 minutes earlier</a
          >
        </li>
        <li>
          <a
            href={withQuery(page.url, { at: null })}
            aria-current={at === undefined ? "true" : undefined}
            class="btn aria-[current=true]:bg-sunken aria-[current=true]:font-semibold"
            data-sveltekit-noscroll>Now</a
          >
        </li>
        <li>
          <a
            href={withQuery(page.url, { at: String(stepFrom(at, clock.nowSeconds(), 1)) })}
            class="btn"
            data-sveltekit-noscroll>30 minutes later</a
          >
        </li>
      </ul>
    </nav>

    <nav aria-label="Operating point">
      <ul class="flex flex-wrap gap-1">
        {#each POINTS as key (key)}
          <li>
            <a
              href={withQuery(page.url, { point: key === "forecast" ? null : key })}
              aria-current={point === key ? "true" : undefined}
              class="btn aria-[current=true]:bg-sunken aria-[current=true]:font-semibold"
              data-sveltekit-noscroll
            >
              {POINT_WORDS[key]}
            </a>
          </li>
        {/each}
      </ul>
    </nav>

    <figure class="space-y-2">
      <FeederSchematic {view} {selected} onselect={select} />
      <figcaption class="text-muted space-y-1 text-xs">
        <p>
          A line is as heavy as the power it carries, with an arrow for its direction, and a bus
          with a ring is a site that takes part in envelopes. Green is inside the limits, amber is
          within {volts(0.01, nominalV)} of the voltage band's edge or above 80 % of a line's rating,
          red is past a limit, and grey is not solved.
        </p>
        <p>
          {POINT_WORDS.forecast} is every site at its forecast. {POINT_WORDS.envelope} is every site that
          takes part exporting at its limit at once, which is what a limit must be safe for.
          {POINT_WORDS.static} is what one fixed limit for everyone would do.
        </p>
      </figcaption>
    </figure>

    <div class="grid gap-4 lg:grid-cols-[minmax(0,1fr)_20rem]">
      <section aria-labelledby="closest" class="min-w-0 space-y-2">
        <h2 id="closest" class="text-lg font-semibold">Closest to a limit</h2>
        {#if view.worst.length === 0}
          <EmptyState title="Nothing to rank yet">
            The engine has not solved this half hour. Run <code>just engine</code>, or wait for its
            next run.
          </EmptyState>
        {:else}
          <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
          <div
            class="card overflow-x-auto"
            tabindex="0"
            role="region"
            aria-label="Closest to a limit"
          >
            <table class="tabular w-full min-w-[480px] text-sm">
              <thead>
                <tr class="border-rule border-b text-left">
                  <th scope="col" class="px-3 py-2 font-semibold">Bus or line</th>
                  <th scope="col" class="px-3 py-2 font-semibold">Status</th>
                  <th scope="col" class="px-3 py-2 font-semibold">Now</th>
                </tr>
              </thead>
              <tbody>
                {#each view.worst as row (row.id)}
                  <tr class="border-rule hover:bg-sunken border-b last:border-b-0">
                    <th scope="row" class="px-3 py-2 text-left font-medium">
                      <a
                        class="link inline-flex min-h-6 items-center"
                        href={withQuery(page.url, { mark: row.id })}
                        aria-current={row.id === selected ? "true" : undefined}
                        data-sveltekit-noscroll>{row.name}</a
                      >
                    </th>
                    <td class="px-3 py-2">
                      <StatusBadge
                        level={row.level}
                        label={row.level === "critical"
                          ? "Past its limit"
                          : row.level === "warn"
                            ? "Near its limit"
                            : "Inside"}
                      />
                    </td>
                    <td class="px-3 py-2">{row.label}</td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        {/if}
      </section>

      <!-- What is selected. Always there, so the table beside it does not move. -->
      <section
        class="card min-h-40 min-w-0 space-y-2 p-3"
        aria-live="polite"
        aria-labelledby="mark"
      >
        {#if bus}
          <h2 id="mark" class="text-base font-semibold">Bus {bus.name}</h2>
          <p class="text-sm">
            {#if bus.vPu}
              Phase to neutral: {bus.vPu.map((v) => volts(v, nominalV)).join(", ")}.
            {:else}
              Not solved for this half hour.
            {/if}
          </p>
          {#if bus.sites.length > 0}
            <p class="text-sm">
              {bus.sites.length === 1 ? "Site" : "Sites"} here:
              {#each bus.sites as nmi, i (nmi)}
                {i > 0 ? ", " : ""}<a class="link" href="/sites/{nmi}">{nmi}</a>
              {/each}
            </p>
          {/if}
        {:else if line}
          <h2 id="mark" class="text-base font-semibold">Line {line.name}</h2>
          <p class="text-sm">{line.label}.</p>
          {#if line.isSwitch}<p class="text-muted text-sm">A closed switch.</p>{/if}
        {:else}
          <h2 id="mark" class="text-base font-semibold">Nothing selected</h2>
          <p class="text-muted text-sm">
            Choose a bus or a line, on the drawing or in the table, to see its voltage or its flow.
          </p>
        {/if}
      </section>
    </div>
  {:else if !topology?.error && !(feeder.error && !feeder.data)}
    <Skeleton label="the network" class="h-96 w-full" />
  {/if}
</div>
