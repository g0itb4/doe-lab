<script lang="ts" module>
  export type ChartSeries = {
    label: string;
    values: (number | null)[];
    // Which series token colours it: 1 to 4, or "ref" for a limit or a rating.
    color: 1 | 2 | 3 | 4 | "ref";
    // Drawn as steps: a limit holds for its interval, it does not slope.
    stepped?: boolean;
  };

  // A dash pattern for each colour, so that two lines differ by more than hue.
  const DASH: Record<ChartSeries["color"], number[]> = {
    1: [],
    2: [9, 4],
    3: [2, 3],
    4: [10, 3, 2, 3],
    ref: [4, 4],
  };

  // Charts are built one per task, in turn: three built in one task block the
  // page for as long as the three together.
  let turn = Promise.resolve();
  function nextTurn(): Promise<void> {
    turn = turn.then(() => new Promise<void>((resolve) => setTimeout(resolve)));
    return turn;
  }
</script>

<script lang="ts">
  import "uplot/dist/uPlot.min.css";
  import { onDestroy, untrack } from "svelte";
  import type uPlot from "uplot";
  import { dayAndTime } from "$lib/format.ts";
  import { downsample } from "$lib/series.ts";
  import { theme, token } from "$lib/theme.svelte.ts";
  import Icon from "./Icon.svelte";

  let {
    title,
    summary,
    x,
    series,
    format,
    axisFormat,
    zone,
    syncKey,
    zoom,
    onzoom,
    now,
    spans = [],
    height = 220,
  }: {
    title: string;
    // What the chart shows, in a sentence: a canvas says nothing to a screen
    // reader.
    summary: string;
    // Unix seconds, ascending.
    x: number[];
    series: ChartSeries[];
    // A value with its unit, for the readout and the table; and a shorter
    // form for the axis, where there is less room.
    format: (value: number) => string;
    axisFormat?: (value: number) => string;
    // The zone that times are shown in.
    zone: string;
    // Charts with the same key share a cursor.
    syncKey?: string;
    // The visible range, in unix seconds; unset shows everything.
    zoom?: [number, number];
    // Called when the reader drags a range (or double-clicks, for none).
    onzoom?: (range: [number, number] | undefined) => void;
    // Feeder time now, marked with a line.
    now?: number;
    // Periods to mark, in unix seconds: shaded, hatched and labelled, so
    // that the mark does not depend on its colour.
    spans?: { from: number; to: number; label: string }[];
    height?: number;
  } = $props();

  let host = $state<HTMLDivElement>();
  let asTable = $state(false);
  let plot: uPlot | undefined;
  let lib: typeof uPlot | undefined;
  let observer: ResizeObserver | undefined;
  // What the cursor is over, for the readout under the chart.
  let hover = $state<number>();
  // How many points are on the canvas, after downsampling.
  let drawn = $state(0);

  const shape = $derived(series.map((s) => `${s.label}|${s.color}|${s.stepped ? 1 : 0}`).join(";"));

  function data(width: number): uPlot.AlignedData {
    const reduced = downsample(
      x,
      series.map((s) => s.values),
      width,
    );
    drawn = reduced.x.length;
    return [reduced.x, ...reduced.columns];
  }

  function range(): { min: number; max: number } {
    if (zoom) return { min: zoom[0], max: zoom[1] };
    return { min: x[0] ?? 0, max: x.at(-1) ?? 1 };
  }

  function build(uplot: typeof uPlot, el: HTMLDivElement): uPlot {
    const axis = token("--color-muted");
    const grid = token("--color-rule");
    const width = el.clientWidth;
    const options: uPlot.Options = {
      width,
      height,
      tzDate: (ts) => uplot.tzDate(new Date(ts * 1000), zone),
      legend: { show: false },
      cursor: {
        sync: syncKey ? { key: syncKey } : undefined,
        // A drag selects a range; the page decides what the range does.
        drag: { x: true, y: false, setScale: false },
      },
      scales: { x: { time: true, range: () => [range().min, range().max] } },
      axes: [
        {
          stroke: axis,
          grid: { stroke: grid, width: 1 },
          ticks: { stroke: grid, width: 1 },
          // The 24-hour clock, and the day before the month.
          values: [
            [3600 * 24 * 365, "{YYYY}", null, null, null, null, null, null, 1],
            [3600 * 24 * 28, "{MMM}", "\n{YYYY}", null, null, null, null, null, 1],
            [3600 * 24, "{D} {MMM}", null, null, null, null, null, null, 1],
            [60, "{HH}:{mm}", "\n{D} {MMM}", null, "\n{D} {MMM}", null, null, null, 1],
            [1, "{HH}:{mm}:{ss}", "\n{D} {MMM}", null, "\n{D} {MMM}", null, null, null, 1],
          ],
        },
        {
          stroke: axis,
          grid: { stroke: grid, width: 1 },
          ticks: { stroke: grid, width: 1 },
          size: 64,
          values: (_u, splits) => splits.map((v) => (axisFormat ?? format)(v)),
        },
      ],
      series: [
        {},
        ...series.map((s) => ({
          label: s.label,
          stroke: token(`--color-series-${s.color}`),
          width: s.color === "ref" ? 1.5 : 2,
          dash: DASH[s.color],
          spanGaps: false,
          points: { show: false },
          paths: s.stepped ? uplot.paths.stepped?.({ align: 1 }) : undefined,
        })),
      ],
      hooks: {
        setCursor: [(u) => (hover = u.cursor.idx ?? undefined)],
        setSelect: [
          (u) => {
            if (u.select.width < 4) return;
            const from = u.posToVal(u.select.left, "x");
            const to = u.posToVal(u.select.left + u.select.width, "x");
            u.setSelect({ left: 0, top: 0, width: 0, height: 0 }, false);
            onzoom?.([Math.round(from), Math.round(to)]);
          },
        ],
        draw: [
          (u) => {
            const ctx = u.ctx;
            const [left, right] = [u.bbox.left, u.bbox.left + u.bbox.width];
            // Where the last label ended: spans that are close share one.
            let labelled = -Infinity;
            for (const span of spans) {
              const from = Math.max(left, u.valToPos(span.from, "x", true));
              const to = Math.min(right, u.valToPos(span.to, "x", true));
              if (to <= left || from >= right) continue;
              // At least a few pixels wide: a breach of a minute must show
              // on a chart of a day.
              const width = Math.max(to - from, 3 * devicePixelRatio);
              ctx.save();
              ctx.fillStyle = token("--color-critical");
              ctx.globalAlpha = 0.16;
              ctx.fillRect(from, u.bbox.top, width, u.bbox.height);
              ctx.globalAlpha = 0.7;
              ctx.strokeStyle = token("--color-critical");
              ctx.lineWidth = devicePixelRatio;
              ctx.beginPath();
              ctx.rect(from, u.bbox.top, width, u.bbox.height);
              ctx.clip();
              for (let d = -u.bbox.height; d < width; d += 6 * devicePixelRatio) {
                ctx.moveTo(from + d, u.bbox.top + u.bbox.height);
                ctx.lineTo(from + d + u.bbox.height, u.bbox.top);
              }
              ctx.stroke();
              ctx.restore();
              if (from < labelled) continue;
              ctx.save();
              ctx.fillStyle = token("--color-critical");
              ctx.font = `${11 * devicePixelRatio}px ${token("--font-sans")}`;
              ctx.textAlign = "left";
              ctx.fillText(
                span.label,
                from + 2 * devicePixelRatio,
                u.bbox.top + 12 * devicePixelRatio,
              );
              labelled = from + ctx.measureText(span.label).width + 6 * devicePixelRatio;
              ctx.restore();
            }
          },
          (u) => {
            if (now === undefined) return;
            const left = u.valToPos(now, "x", true);
            if (left < u.bbox.left || left > u.bbox.left + u.bbox.width) return;
            const ctx = u.ctx;
            ctx.save();
            ctx.strokeStyle = token("--color-text");
            ctx.lineWidth = devicePixelRatio;
            ctx.setLineDash([2 * devicePixelRatio, 4 * devicePixelRatio]);
            ctx.beginPath();
            ctx.moveTo(left, u.bbox.top);
            ctx.lineTo(left, u.bbox.top + u.bbox.height);
            ctx.stroke();
            ctx.fillStyle = token("--color-text");
            ctx.font = `${11 * devicePixelRatio}px ${token("--font-sans")}`;
            ctx.textAlign = "center";
            ctx.fillText("now", left, u.bbox.top - 4 * devicePixelRatio);
            ctx.restore();
          },
        ],
      },
      padding: [14, 8, 0, 0],
    };
    const u = new uplot(options, data(width), el);
    // uPlot's own double-click resets to the data; here the page owns the
    // range, so it is told instead.
    u.over.addEventListener("dblclick", () => onzoom?.(undefined));
    return u;
  }

  // (Re)build when the canvas appears, the set of series changes, or the
  // theme does: the colours are read from the tokens at build time.
  // True once the chart has come near the viewport. A chart below the fold
  // costs nothing until the reader scrolls towards it.
  let near = $state(false);
  $effect(() => {
    const el = host;
    if (!el || near) return;
    const watcher = new IntersectionObserver(
      (entries) => {
        if (entries.some((entry) => entry.isIntersecting)) near = true;
      },
      { rootMargin: "300px" },
    );
    watcher.observe(el);
    return () => watcher.disconnect();
  });

  $effect(() => {
    void shape;
    void theme.version;
    const el = host;
    if (!el || !near) return;
    let cancelled = false;
    void (async () => {
      lib ??= (await import("uplot")).default;
      await nextTurn();
      if (cancelled) return;
      untrack(() => {
        plot?.destroy();
        plot = build(lib!, el);
        observer?.disconnect();
        observer = new ResizeObserver(() => {
          if (!plot || el.clientWidth === 0 || el.clientWidth === plot.width) return;
          plot.setSize({ width: el.clientWidth, height });
          plot.setData(data(el.clientWidth));
        });
        observer.observe(el);
      });
    })();
    return () => {
      cancelled = true;
    };
  });

  // New data or a new range: give the chart its data again.
  $effect(() => {
    void x;
    void series;
    void zoom;
    untrack(() => {
      if (!plot || !host) return;
      plot.setData(data(host.clientWidth), false);
      plot.setScale("x", range());
    });
  });

  // A new "now" or new marks: the same data, drawn again.
  $effect(() => {
    void now;
    void spans;
    untrack(() => plot?.redraw(false));
  });

  onDestroy(() => {
    observer?.disconnect();
    plot?.destroy();
    plot = undefined;
  });

  const cell = (value: number | null | undefined) =>
    value === null || value === undefined ? "–" : format(value);
  const at = (seconds: number | undefined) =>
    seconds === undefined ? undefined : dayAndTime(new Date(seconds * 1000), zone);
  // The readout follows the cursor on the canvas, whose points may have been
  // thinned: look the time up, not the index.
  const hoverTime = $derived(hover === undefined ? undefined : plot?.data[0]?.[hover]);
  const hoverRow = $derived(hoverTime === undefined ? -1 : x.indexOf(hoverTime));
</script>

<figure class="card min-w-0 p-3">
  <figcaption class="flex flex-wrap items-start justify-between gap-2">
    <div class="min-w-0">
      <h2 class="font-semibold">{title}</h2>
      <p class="text-muted text-sm">{summary}</p>
    </div>
    <button type="button" class="btn" aria-pressed={asTable} onclick={() => (asTable = !asTable)}>
      <Icon name={asTable ? "chart" : "table"} />
      {asTable ? "View as chart" : "View as table"}
    </button>
  </figcaption>

  {#if asTable}
    <!-- Focusable, so that a keyboard can scroll it. -->
    <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
    <div
      class="mt-2 overflow-auto"
      style:max-height="{height + 40}px"
      tabindex="0"
      role="region"
      aria-label="{title}, as a table"
    >
      <table class="tabular w-full text-sm">
        <caption class="sr-only">{title}</caption>
        <thead class="bg-surface sticky top-0">
          <tr class="border-rule border-b text-left">
            <th scope="col" class="py-1 pr-3 font-semibold">Time</th>
            {#each series as s (s.label)}
              <th scope="col" class="py-1 pr-3 text-right font-semibold">{s.label}</th>
            {/each}
          </tr>
        </thead>
        <tbody>
          {#each x as seconds, i (seconds)}
            <tr class="border-rule border-b">
              <th scope="row" class="py-1 pr-3 text-left font-normal whitespace-nowrap"
                >{at(seconds)}</th
              >
              {#each series as s (s.label)}
                <td class="py-1 pr-3 text-right whitespace-nowrap">{cell(s.values[i])}</td>
              {/each}
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {:else}
    <!-- The canvas is for sighted readers; the summary and the table say the
         same to everyone else. -->
    <div class="mt-2" aria-hidden="true">
      <div
        bind:this={host}
        style:height="{height}px"
        data-points={drawn}
        class="w-full overflow-hidden"
      ></div>
      <ul class="mt-1 flex flex-wrap gap-x-4 gap-y-1 text-xs">
        {#each series as s (s.label)}
          <li class="flex items-center gap-1.5">
            <svg width="26" height="8" class="shrink-0">
              <line
                x1="0"
                y1="4"
                x2="26"
                y2="4"
                stroke="var(--color-series-{s.color})"
                stroke-width={s.color === "ref" ? 1.5 : 2}
                stroke-dasharray={DASH[s.color].join(" ")}
              />
            </svg>
            <span>{s.label}</span>
            {#if hoverRow >= 0}
              <span class="tabular font-semibold">{cell(s.values[hoverRow])}</span>
            {/if}
          </li>
        {/each}
        <li class="tabular text-muted ml-auto min-h-4">
          {at(hoverTime) ?? (onzoom ? "Drag to zoom, double-click to reset" : "")}
        </li>
      </ul>
    </div>
  {/if}
</figure>
