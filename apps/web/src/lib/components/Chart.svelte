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
  import { place, rowAt, spanAt, step, valueText } from "$lib/chart-cursor.ts";
  import { pan, type Range, views, zoomAt } from "$lib/chart-view.svelte.ts";
  import { dayAndTime } from "$lib/format.ts";
  import { downsample, toggleHidden, windowed } from "$lib/series.ts";
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
    hidden,
    onhide,
    pin,
    onpin,
    pinHref,
    emphasis,
    onspan,
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
    // A mark with an id can be pointed at: see `emphasis` and `onspan`.
    spans?: { id?: string; from: number; to: number; label: string }[];
    height?: number;
    // The series that are not drawn, by their place in `series`. A page that
    // keeps them in the address gives them, and is told of a change; without
    // one the chart keeps them itself.
    hidden?: number[];
    onhide?: (hidden: number[]) => void;
    // An instant that is pinned, in unix seconds: its values stay in the
    // legend when the cursor has gone. A click on the plot, or Enter on the
    // cursor, asks the page to pin the point there, or to let it go.
    pin?: number;
    onpin?: (time: number | undefined) => void;
    // Where to look at the pinned instant in another way: a link beside it.
    pinHref?: (time: number) => { href: string; label: string };
    // The id of a mark to draw more heavily: the one a list points at.
    emphasis?: string;
    // Called with the id of the mark under the pointer, or with none.
    onspan?: (id: string | undefined) => void;
  } = $props();

  let host = $state<HTMLDivElement>();
  let asTable = $state(false);
  let plot: uPlot | undefined;
  let lib: typeof uPlot | undefined;
  let observer: ResizeObserver | undefined;
  // What the cursor is over, for the readout: the index of a point on the
  // canvas, and where the cursor is inside the plot, in CSS pixels.
  let hover = $state<number>();
  let cursorAt = $state<{ left: number; top: number }>();
  // The plot's place inside the chart, and its size: the room the readout has.
  let frame = $state({ left: 0, top: 0, width: 0, height: 0 });
  // Whether the pointer is over this chart, or the keyboard is moving its
  // cursor: only then does it float a readout. A chart that merely follows
  // another's cursor shows its values in its legend.
  let pointed = $state(false);
  let keyed = $state(false);
  // How many points are on the canvas, after downsampling, and their times.
  let drawn = $state(0);
  let drawnX = $state<number[]>([]);
  // The lines that mark now and the pinned instant: elements over the plot,
  // moved without a redraw.
  let nowLine: HTMLDivElement | undefined;
  let pinLine: HTMLDivElement | undefined;

  // The range on show: the page's, or the one a gesture is making, which the
  // charts that share a key show together until the page has it.
  const ownKey = views.own();
  const viewKey = $derived(syncKey ?? ownKey);
  const making = $derived(views.get(viewKey));
  const shown = $derived(making === undefined ? zoom : (making ?? undefined));
  const bounds = (): Range => [x[0] ?? 0, x.at(-1) ?? 1];
  const current = (): Range => shown ?? bounds();
  const same = (a: Range | undefined, b: Range | undefined) =>
    a === b || (!!a && !!b && a[0] === b[0] && a[1] === b[1]);

  // A gesture reached a range: the charts show it at once, and the page is
  // told when the gesture is over, or straight away when it is one step.
  let settle: ReturnType<typeof setTimeout> | undefined;
  function reach(next: Range | undefined, when: "now" | "soon") {
    if (!onzoom) return;
    clearTimeout(settle);
    const tell = () => {
      // The page already has it: there is nothing for it to answer.
      if (same(next, zoom)) views.clear(viewKey);
      else onzoom(next);
    };
    views.set(viewKey, next);
    if (when === "now") tell();
    else settle = setTimeout(tell, 200);
  }
  // The page has a range: what was in the making is done.
  $effect(() => {
    void zoom;
    untrack(() => views.clear(viewKey));
  });

  let ownHidden = $state<number[]>([]);
  const off = $derived(hidden ?? ownHidden);
  function toggle(index: number, alone: boolean) {
    const next = toggleHidden(off, index, series.length, alone);
    ownHidden = next;
    onhide?.(next);
  }

  const shape = $derived(series.map((s) => `${s.label}|${s.color}|${s.stepped ? 1 : 0}`).join(";"));

  // What goes on the canvas: the part of the series in view, thinned to what
  // the pixels can show. Zoomed in, the pixels go to what is in view.
  function data(width: number): uPlot.AlignedData {
    const inView = windowed(
      x,
      series.map((s) => s.values),
      shown,
    );
    const reduced = downsample(inView.x, inView.columns, width);
    drawn = reduced.x.length;
    drawnX = reduced.x;
    return [reduced.x, ...reduced.columns];
  }

  function range(): { min: number; max: number } {
    const [min, max] = current();
    return { min, max };
  }

  // Where the lines that mark now and the pinned instant stand: moved, not
  // drawn, so that the canvas is left alone as feeder time passes.
  function placeLines(u: uPlot) {
    for (const [line, time] of [
      [nowLine, now],
      [pinLine, pin],
    ] as const) {
      if (!line) continue;
      const left = time === undefined ? NaN : u.valToPos(time, "x");
      const inside = left >= 0 && left <= u.over.clientWidth;
      line.hidden = !inside;
      if (inside) line.style.transform = `translateX(${left}px)`;
    }
  }

  function build(uplot: typeof uPlot, el: HTMLDivElement): uPlot {
    // The colours are asked for at every draw, so a change of theme is a
    // redraw and not a new chart.
    const axis = () => token("--color-muted");
    const grid = () => token("--color-rule");
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
        ...series.map((s, i) => ({
          label: s.label,
          show: !off.includes(i),
          stroke: () => token(`--color-series-${s.color}`),
          width: s.color === "ref" ? 1.5 : 2,
          dash: DASH[s.color],
          spanGaps: false,
          points: { show: false },
          paths: s.stepped ? uplot.paths.stepped?.({ align: 1 }) : undefined,
        })),
      ],
      hooks: {
        setCursor: [
          (u) => {
            hover = u.cursor.idx ?? undefined;
            const { left, top } = u.cursor;
            cursorAt =
              left !== undefined && top !== undefined && left >= 0 ? { left, top } : undefined;
          },
        ],
        // The plot has a new size or a new range: the things laid over it move.
        setSize: [measure],
        setScale: [placeLines],
        ready: [
          (u) => {
            measure(u);
            placeLines(u);
          },
        ],
        setSelect: [
          (u) => {
            if (u.select.width < 4) return;
            // The click that ends a drag is not a click on a point.
            dragged = true;
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
              // The mark a list points at is heavier: more shade, more line.
              const strong = span.id !== undefined && span.id === emphasis;
              ctx.save();
              ctx.fillStyle = token("--color-critical");
              ctx.globalAlpha = strong ? 0.3 : 0.16;
              ctx.fillRect(from, u.bbox.top, width, u.bbox.height);
              ctx.globalAlpha = strong ? 1 : 0.7;
              ctx.strokeStyle = token("--color-critical");
              ctx.lineWidth = (strong ? 2 : 1) * devicePixelRatio;
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
        ],
      },
      // Room on the right for half of the last label of the time axis.
      padding: [14, 20, 0, 0],
    };
    const u = new uplot(options, data(width), el);
    // uPlot's own double-click resets to the data; here the page owns the
    // range, so it is told instead.
    u.over.addEventListener("dblclick", ondblclick);
    u.over.addEventListener("mouseenter", () => (pointed = true));
    u.over.addEventListener("mouseleave", () => (pointed = false));
    u.over.addEventListener("click", onclick);
    u.over.addEventListener("wheel", onwheel, { passive: false });
    u.over.addEventListener("touchstart", ontouch, { passive: true });
    u.over.addEventListener("touchmove", ontouch, { passive: true });
    u.over.addEventListener("touchend", ontouchend, { passive: true });
    // Before uPlot hears of it: with Shift a drag moves the range along, and
    // must not start a selection.
    el.addEventListener("mousedown", onpanstart, { capture: true });
    const line = (name: string, word: string) => {
      const mark = document.createElement("div");
      mark.className = name;
      mark.hidden = true;
      mark.append(Object.assign(document.createElement("span"), { textContent: word }));
      u.over.append(mark);
      return mark;
    };
    nowLine = line("now", "now");
    pinLine = line("pin", "pinned");
    measure(u);
    placeLines(u);
    return u;
  }

  // The time under a point of the screen, in the plot's own scale.
  const timeAt = (u: uPlot, clientX: number) =>
    u.posToVal(clientX - u.over.getBoundingClientRect().left, "x");

  // Ctrl or ⌘ with the wheel zooms about the pointer; a pinch on a trackpad
  // is the same event. A plain wheel is left to the page, which scrolls.
  function onwheel(event: WheelEvent) {
    if (!onzoom || !plot || !(event.ctrlKey || event.metaKey)) return;
    event.preventDefault();
    const factor = Math.exp(event.deltaY * 0.0025);
    reach(zoomAt(current(), timeAt(plot, event.clientX), factor, bounds()), "soon");
  }

  // Shift and a drag move the range along, the plot following the pointer.
  function onpanstart(event: MouseEvent) {
    if (!onzoom || !plot || !event.shiftKey || event.button !== 0) return;
    event.stopPropagation();
    event.preventDefault();
    // The plot's width to the fraction of a pixel: a drag of a third of it
    // is a third of the range.
    const [from, start] = [event.clientX, current()];
    const width = plot.over.getBoundingClientRect().width;
    let last: Range = start;
    const move = (e: MouseEvent) => {
      const seconds = (-(e.clientX - from) / width) * (start[1] - start[0]);
      last = pan(start, seconds, bounds());
      views.set(viewKey, last);
    };
    const end = () => {
      document.removeEventListener("mousemove", move);
      document.removeEventListener("mouseup", end);
      // The click that ends it is not a click on a point.
      dragged = true;
      reach(same(last, bounds()) ? undefined : last, "now");
    };
    document.addEventListener("mousemove", move);
    document.addEventListener("mouseup", end);
  }

  // A finger on the plot moves the cursor; two fingers pinch the range. The
  // plot takes sideways touches and leaves the page its scroll (touch-action).
  let pinch: { distance: number; range: Range; at: number } | undefined;
  const spread = (touches: TouchList) => Math.abs(touches[0]!.clientX - touches[1]!.clientX) || 1;
  function ontouch(event: TouchEvent) {
    if (!plot) return;
    const touches = event.touches;
    if (touches.length === 1) {
      pinch = undefined;
      const box = plot.over.getBoundingClientRect();
      pointed = true;
      (plot as unknown as Cursor).setCursor(
        { left: touches[0]!.clientX - box.left, top: touches[0]!.clientY - box.top },
        true,
        true,
      );
      return;
    }
    if (!onzoom || touches.length !== 2) return;
    const middle = (touches[0]!.clientX + touches[1]!.clientX) / 2;
    pinch ??= { distance: spread(touches), range: current(), at: timeAt(plot, middle) };
    reach(zoomAt(pinch.range, pinch.at, pinch.distance / spread(touches), bounds()), "soon");
  }
  function ontouchend(event: TouchEvent) {
    if (event.touches.length < 2) pinch = undefined;
  }

  // A click on a point pins it, and a click on the pinned point lets it go.
  // The first click of a double-click is taken back: that one resets the zoom.
  let dragged = false;
  let pinBefore: number | undefined;
  function onclick(event: MouseEvent) {
    if (dragged || event.detail > 1) {
      dragged = false;
      return;
    }
    pinBefore = pin;
    if (hoverTime !== undefined) togglePin(hoverTime);
  }
  function ondblclick() {
    onzoom?.(undefined);
    if (pin !== pinBefore) onpin?.(pinBefore);
  }
  // A touch somewhere else puts away the cursor that a finger left.
  $effect(() => {
    const outside = (event: TouchEvent) => {
      if (!pointed || host?.contains(event.target as Node)) return;
      pointed = false;
      moveTo(undefined);
    };
    document.addEventListener("touchstart", outside, { passive: true });
    return () => document.removeEventListener("touchstart", outside);
  });
  function togglePin(time: number) {
    onpin?.(time === pin ? undefined : time);
  }

  function measure(u: uPlot) {
    frame = {
      left: u.over.offsetLeft,
      top: u.over.offsetTop,
      width: u.over.clientWidth,
      height: u.over.clientHeight,
    };
  }

  // (Re)build when the canvas appears, or the set of series changes.
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

  // The theme changed: the same chart, drawn again in the new colours.
  $effect(() => {
    void theme.version;
    untrack(() => plot?.redraw(false, true));
  });

  // A series was hidden or shown.
  $effect(() => {
    const hiddenNow = off;
    untrack(() => {
      if (!plot) return;
      series.forEach((_, i) => {
        const show = !hiddenNow.includes(i);
        if (plot!.series[i + 1]?.show !== show) plot!.setSeries(i + 1, { show });
      });
    });
  });

  // New data or a new range: give the chart its data again.
  $effect(() => {
    void x;
    void series;
    void shown;
    untrack(() => {
      if (!plot || !host) return;
      plot.setData(data(host.clientWidth), false);
      plot.setScale("x", range());
    });
  });

  // A new "now" or a new pin: its line moves, and nothing is drawn.
  $effect(() => {
    void now;
    void pin;
    untrack(() => plot && placeLines(plot));
  });

  // New marks: the same data, drawn again. The marks are compared by what
  // they say, because a page makes the list anew as time passes.
  const marks = $derived(spans.map((s) => `${s.from}:${s.to}:${s.label}`).join(";"));
  $effect(() => {
    void marks;
    void emphasis;
    untrack(() => plot?.redraw(false));
  });

  onDestroy(() => {
    clearTimeout(settle);
    views.clear(viewKey);
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
  const hoverTime = $derived(hover === undefined ? undefined : drawnX[hover]);
  const hoverRow = $derived(hoverTime === undefined ? -1 : rowAt(x, hoverTime));
  // With no cursor, the legend says the values of the pinned instant.
  const pinRow = $derived(pin === undefined ? -1 : rowAt(x, pin));
  const legendRow = $derived(hoverRow >= 0 ? hoverRow : pinRow);
  // The mark under the pointer, for a list that wants to answer it.
  const under = $derived(pointed ? spanAt(spans, hoverTime) : undefined);
  $effect(() => {
    const id = under;
    untrack(() => onspan?.(id));
  });
  // The series that are drawn, each with its value under the cursor.
  const readout = $derived(
    hoverRow < 0
      ? []
      : series.flatMap((s, i) =>
          off.includes(i) ? [] : [{ ...s, value: cell(s.values[hoverRow]) }],
        ),
  );
  const tip = $derived(
    (pointed || keyed) && cursorAt && hoverRow >= 0 ? place(cursorAt, frame) : undefined,
  );
  const px = (value: number | undefined, offset: number) =>
    value === undefined ? undefined : `${value + offset}px`;

  // The cursor from the keyboard, as a slider is moved: a point, a tenth of
  // the chart, or to an end. The charts that share the cursor follow.
  type Cursor = {
    setCursor(at: { left: number; top: number }, fire?: boolean, pub?: boolean): void;
  };
  function moveTo(index: number | undefined) {
    if (!plot) return;
    const time = index === undefined ? undefined : drawnX[index];
    const at =
      time === undefined
        ? { left: -10, top: -10 }
        : { left: plot.valToPos(time, "x"), top: plot.over.clientHeight / 2 };
    (plot as unknown as Cursor).setCursor(at, true, true);
  }
  function onkeydown(event: KeyboardEvent) {
    if (event.key === "Escape") {
      keyed = false;
      moveTo(undefined);
      return;
    }
    // Enter or Space pins the point under the cursor, as a click does.
    if ((event.key === "Enter" || event.key === " ") && onpin && hoverTime !== undefined) {
      event.preventDefault();
      togglePin(hoverTime);
      return;
    }
    if (onzoom && zoomKey(event)) {
      event.preventDefault();
      return;
    }
    const next = step(drawnX.length, hover, event.key, now === undefined ? 0 : rowAt(drawnX, now));
    if (next === undefined) return;
    event.preventDefault();
    keyed = true;
    moveTo(next);
  }
  // The range from the keyboard: plus and minus zoom about the cursor, or
  // about the middle with none; 0 shows everything; Shift with an arrow moves
  // the range a quarter of itself along. True when the key was one of them.
  function zoomKey(event: KeyboardEvent): boolean {
    const [from, to] = current();
    const about = hoverTime ?? (from + to) / 2;
    switch (event.key) {
      case "+":
      case "=":
        reach(zoomAt(current(), about, 0.5, bounds()), "now");
        return true;
      case "-":
        reach(zoomAt(current(), about, 2, bounds()), "now");
        return true;
      case "0":
        reach(undefined, "now");
        return true;
    }
    if (!event.shiftKey || (event.key !== "ArrowLeft" && event.key !== "ArrowRight")) return false;
    const next = pan(current(), ((event.key === "ArrowLeft" ? -1 : 1) * (to - from)) / 4, bounds());
    reach(same(next, bounds()) ? undefined : next, "now");
    return true;
  }
  function onblur() {
    if (!keyed) return;
    keyed = false;
    moveTo(undefined);
  }
  const spoken = $derived(
    hoverRow < 0
      ? `${at(x[0]) ?? "No data"} to ${at(x.at(-1)) ?? "no data"}. No point chosen.`
      : valueText(
          at(hoverTime)!,
          readout.map((r) => ({ label: r.label, value: r.value })),
        ),
  );
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
    <div class="mt-2">
      <!-- The canvas is for sighted readers; the summary and the table say
           the same to everyone else. The keyboard moves a cursor through its
           points as it would a slider, and each point is said in words. -->
      <div
        class="plot relative"
        role="slider"
        tabindex="0"
        aria-label="{title}: a cursor through time. Arrow keys move it, Escape puts it away.{onzoom
          ? ' Plus and minus zoom, 0 shows everything, Shift with an arrow moves the range.'
          : ''}{onpin ? ' Enter pins a point.' : ''}"
        aria-valuemin="0"
        aria-valuemax={Math.max(0, drawn - 1)}
        aria-valuenow={hover ?? 0}
        aria-valuetext={spoken}
        {onkeydown}
        {onblur}
      >
        <div
          bind:this={host}
          style:height="{height}px"
          data-points={drawn}
          class="w-full overflow-hidden"
          aria-hidden="true"
        ></div>
        {#if tip}
          <!-- Beside the cursor, on the side with more room. It never takes
               the pointer, and it does not move in: it is where the cursor is. -->
          <div
            class="tip raised"
            aria-hidden="true"
            style:left={px(tip.left, frame.left)}
            style:right={px(tip.right, host ? host.clientWidth - frame.left - frame.width : 0)}
            style:max-width="{tip.maxWidth}px"
            style:top={px(tip.top, frame.top)}
            style:bottom={px(tip.bottom, host ? host.clientHeight - frame.top - frame.height : 0)}
          >
            <p class="text-muted text-label">{at(hoverTime)}</p>
            <ul class="mt-1 space-y-0.5">
              {#each readout as s (s.label)}
                <li class="text-label flex items-center gap-2">
                  <svg width="18" height="8" class="shrink-0">
                    <line
                      x1="0"
                      y1="4"
                      x2="18"
                      y2="4"
                      stroke="var(--color-series-{s.color})"
                      stroke-width={s.color === "ref" ? 1.5 : 2}
                      stroke-dasharray={DASH[s.color].join(" ")}
                    />
                  </svg>
                  <span class="min-w-0 flex-1">{s.label}</span>
                  <span class="font-semibold">{s.value}</span>
                </li>
              {/each}
            </ul>
          </div>
        {/if}
      </div>
      <!-- The legend: each series is a switch that hides it or shows it. -->
      <ul class="text-label mt-1 flex flex-wrap items-center gap-x-3 gap-y-1">
        {#each series as s, i (s.label)}
          {@const shown = !off.includes(i)}
          <li>
            <button
              type="button"
              class="legend"
              aria-pressed={shown}
              title="{shown ? 'Hide' : 'Show'} this series. With Alt, show it alone."
              onclick={(event) => toggle(i, event.altKey)}
            >
              <svg width="26" height="8" class="shrink-0" aria-hidden="true">
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
              <span class="name">{s.label}</span>
              {#if shown && legendRow >= 0}
                <span class="font-semibold">{cell(s.values[legendRow])}</span>
              {/if}
            </button>
          </li>
        {/each}
        <li class="text-muted ml-auto min-h-4" aria-hidden="true">
          {at(hoverTime) ?? (onzoom ? "Drag to zoom, double-click to reset" : "")}
        </li>
      </ul>
      {#if pin !== undefined && onpin}
        {@const link = pinHref?.(pin)}
        <!-- The pinned instant, and the ways on from it. -->
        <p class="text-label mt-1 flex flex-wrap items-center gap-x-3 gap-y-1">
          <span>Pinned at {at(pin)}: its values are in the legend.</span>
          {#if link}<a class="link" href={link.href}>{link.label}</a>{/if}
          <button type="button" class="btn min-h-6 px-2 py-0" onclick={() => onpin(undefined)}
            >Unpin</button
          >
        </p>
      {/if}
    </div>
  {/if}
</figure>

<style>
  /* The readout floats over the plot, beside the cursor. */
  .tip {
    position: absolute;
    z-index: 5;
    /* As wide as what it says, up to the room beside the cursor. */
    width: max-content;
    padding: 0.375rem 0.5rem;
    pointer-events: none;
  }
  /* A series of the legend: a switch, at least 24 px tall. Hidden, it is
     struck through and its line fades: not by colour alone. */
  .legend {
    display: inline-flex;
    min-height: 1.5rem;
    align-items: center;
    gap: 0.375rem;
    border-radius: var(--radius-control);
    padding: 0 0.25rem;
    cursor: pointer;
    transition: background-color var(--duration-fast);
  }
  .legend:hover {
    background: var(--color-sunken);
  }
  .legend[aria-pressed="false"] svg {
    opacity: 0.35;
  }
  .legend[aria-pressed="false"] .name {
    color: var(--color-muted);
    text-decoration: line-through;
  }
  /* The line that marks now: uPlot's plot holds it, so the rules are global,
     under the chart's own class. */
  /* The plot takes a sideways touch for its cursor, and leaves the page its
     scroll. */
  .plot :global(.u-over) {
    touch-action: pan-y;
  }
  .plot :global(.pin) {
    position: absolute;
    top: 0;
    bottom: 0;
    left: 0;
    width: 0;
    border-left: 2px solid var(--color-accent);
    pointer-events: none;
  }
  .plot :global(.pin span) {
    display: none;
  }
  .plot :global(.now) {
    position: absolute;
    top: 0;
    bottom: 0;
    left: 0;
    width: 0;
    border-left: 1px dashed var(--color-text);
    pointer-events: none;
  }
  .plot :global(.now span) {
    position: absolute;
    top: -14px;
    left: 0;
    transform: translateX(-50%);
    color: var(--color-text);
    font-size: var(--text-micro);
    line-height: 14px;
  }
</style>
