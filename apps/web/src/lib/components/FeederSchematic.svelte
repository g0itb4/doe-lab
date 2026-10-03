<script lang="ts">
  import { untrack } from "svelte";
  import { hint } from "$lib/hovercard.svelte.ts";
  import { LEVEL_WORDS, type Schematic } from "$lib/map/schematic.ts";
  import { boxOf, centre, MAX_ZOOM, panBy, type View, WHOLE, zoomBy } from "$lib/map/viewbox.ts";
  import Icon from "./Icon.svelte";

  // A feeder drawn as a schematic: the transformer on the left, each bus as
  // far to the right as there is cable to it, each branch in a lane. A line is
  // as heavy as the power it carries and coloured by how near its rating it
  // is; a bus is coloured by how near the voltage band's edge it is. Dashes
  // move along a line the way its power flows. A click on a mark selects it;
  // the page shows what it is. The drawing zooms and moves: with the buttons
  // on it, with the keys, with Ctrl and the wheel, and by a drag.
  let {
    view,
    selected = "",
    animate = true,
    onselect,
    zoom,
    onview,
  }: {
    view: Schematic;
    // The id of the selected bus or line.
    selected?: string;
    // False stops the dashes: motion a reader can switch off. The arrows say
    // the direction without them.
    animate?: boolean;
    onselect: (kind: "bus" | "line", id: string) => void;
    // The part of the drawing on show. A page that keeps it in the address
    // gives it, and is told of a change; without one the drawing keeps it.
    zoom?: View;
    onview?: (view: View) => void;
  } = $props();

  // A flow worth showing, as a share of the busiest line's power: less than
  // this and a line has no arrow and no moving dashes.
  const SHOWN_FROM = 0.08;
  // How long the dashes take to move on by one, in seconds: the more a line
  // carries, the quicker.
  const pace = (weight: number) => (2.4 - 1.8 * weight).toFixed(2);
  // How many lines have moving dashes at most: the busiest. Every frame of
  // every one is painted, and past a few dozen the eye gains nothing.
  const MOVING = 24;
  // The lines whose dashes move. A switch is dashed already: its arrow alone
  // says the direction.
  const flows = $derived(
    animate
      ? view.lines
          .filter((l) => l.powerW !== undefined && l.weight > SHOWN_FROM && !l.isSwitch)
          .sort((a, b) => b.weight - a.weight)
          .slice(0, MOVING)
      : [],
  );
  const root = $derived(view.nodes[0]);
  const scaleY = $derived(view.height - 8);

  // The part on show: the page's, or the one a gesture is making until the
  // page has it.
  let making = $state<View>();
  const shown = $derived(making ?? zoom ?? WHOLE);
  const size = $derived({ width: view.width, height: view.height });
  const box = $derived(boxOf(shown, size));
  $effect(() => {
    void zoom;
    untrack(() => (making = undefined));
  });
  // The page is told when a gesture is over, or at once for one step.
  let settle: ReturnType<typeof setTimeout> | undefined;
  function reach(next: View, when: "now" | "soon") {
    making = next;
    clearTimeout(settle);
    if (when === "now") onview?.(next);
    else settle = setTimeout(() => onview?.(next), 200);
  }

  let sheet = $state<HTMLDivElement>();
  // A point of the screen in the drawing's own units, and a distance.
  const scale = () => size.width / shown.k / sheet!.getBoundingClientRect().width;
  function pointAt(event: MouseEvent) {
    const rect = sheet!.getBoundingClientRect();
    return {
      x: shown.x + (event.clientX - rect.left) * scale(),
      y: shown.y + (event.clientY - rect.top) * scale(),
    };
  }

  // Ctrl or ⌘ with the wheel zooms about the pointer; a plain wheel is the
  // page's. Registered by hand: the handler must be able to stop the page.
  $effect(() => {
    const el = sheet;
    if (!el) return;
    const onwheel = (event: WheelEvent) => {
      if (!(event.ctrlKey || event.metaKey)) return;
      event.preventDefault();
      reach(zoomBy(shown, Math.exp(-event.deltaY * 0.0025), pointAt(event), size), "soon");
    };
    el.addEventListener("wheel", onwheel, { passive: false });
    return () => {
      el.removeEventListener("wheel", onwheel);
      clearTimeout(settle);
    };
  });

  // A drag moves the drawing; the click that ends one selects nothing.
  let dragged = false;
  function onmousedown(event: MouseEvent) {
    if (event.button !== 0) return;
    const [fromX, fromY, start, perPixel] = [event.clientX, event.clientY, shown, scale()];
    dragged = false;
    const move = (e: MouseEvent) => {
      const [dx, dy] = [e.clientX - fromX, e.clientY - fromY];
      if (!dragged && Math.hypot(dx, dy) < 4) return;
      dragged = true;
      making = panBy(start, -dx * perPixel, -dy * perPixel, size);
    };
    const end = () => {
      document.removeEventListener("mousemove", move);
      document.removeEventListener("mouseup", end);
      if (dragged && making) reach(making, "now");
    };
    document.addEventListener("mousemove", move);
    document.addEventListener("mouseup", end);
  }
  function swallow(event: MouseEvent) {
    if (!dragged) return;
    dragged = false;
    event.stopPropagation();
  }

  // From the buttons and the keys: about the middle of what is on show.
  const zoomStep = (factor: number) =>
    reach(zoomBy(shown, factor, centre(shown, size), size), "now");
  function onkeydown(event: KeyboardEvent) {
    const [w, h] = [size.width / shown.k / 5, size.height / shown.k / 5];
    const moves: Record<string, () => void> = {
      "+": () => zoomStep(1.5),
      "=": () => zoomStep(1.5),
      "-": () => zoomStep(1 / 1.5),
      "0": () => reach(WHOLE, "now"),
      ArrowLeft: () => reach(panBy(shown, -w, 0, size), "now"),
      ArrowRight: () => reach(panBy(shown, w, 0, size), "now"),
      ArrowUp: () => reach(panBy(shown, 0, -h, size), "now"),
      ArrowDown: () => reach(panBy(shown, 0, h, size), "now"),
    };
    const move = moves[event.key];
    // A key pressed on one of the buttons is the button's.
    if (!move || event.target !== event.currentTarget) return;
    event.preventDefault();
    move();
  }
</script>

<!-- The drawing is as wide as its box at any width, and zooms inside it: the
     page never scrolls sideways for it. The box takes the keyboard, so the
     keys can move and zoom what is in it. -->
<!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions -->
<div
  class="card relative overflow-hidden"
  tabindex="0"
  role="region"
  aria-label="Schematic of the feeder"
  aria-describedby="schematic-keys"
  {onkeydown}
>
  <p id="schematic-keys" class="sr-only">
    Plus and minus zoom the drawing, 0 shows the whole feeder, and the arrow keys move it.
  </p>
  <div class="controls">
    <button
      type="button"
      class="btn"
      aria-label="Zoom in"
      disabled={shown.k >= MAX_ZOOM}
      onclick={() => zoomStep(1.5)}><Icon name="plus" /></button
    >
    <button
      type="button"
      class="btn"
      aria-label="Zoom out"
      disabled={shown.k <= 1}
      onclick={() => zoomStep(1 / 1.5)}><Icon name="minus" /></button
    >
    <button
      type="button"
      class="btn"
      aria-label="Show the whole feeder"
      disabled={shown.k <= 1}
      onclick={() => reach(WHOLE, "now")}>Fit</button
    >
  </div>
  <!-- One drawing in three sheets, one over the other: the lines, the dashes
       that move along them, and the buses. The dashes are painted on every
       frame; on a sheet of their own the browser paints them alone, and
       leaves the two thousand marks of the others as they are. -->
  <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
  <div
    bind:this={sheet}
    class="schematic relative"
    class:zoomed={shown.k > 1}
    role="img"
    {onmousedown}
    onclickcapture={swallow}
    aria-label="The feeder, from the transformer on the left to its furthest bus, {Math.round(
      view.lengthM,
    )} metres of cable away. The table below lists the buses and lines closest to their limits."
  >
    <svg viewBox={box} class="block h-auto w-full" aria-hidden="true">
      {#if view.binding?.lineId}
        {@const held = view.lines.find((l) => l.id === view.binding?.lineId)}
        {#if held}<polyline class="halo" points={held.path} />{/if}
      {/if}

      {#each view.lines as line (line.id)}
        <g
          class="line"
          data-level={line.level}
          data-selected={selected === line.id ? "" : undefined}
          data-line={line.name}
          use:hint={() => ({
            title: `Line ${line.name}`,
            lines: [LEVEL_WORDS[line.level], line.label],
            level: line.level,
          })}
        >
          <polyline
            class="wire"
            class:switch={line.isSwitch}
            points={line.path}
            stroke-width={1.5 + 5 * line.weight}
          />
          {#if line.powerW !== undefined && line.weight > SHOWN_FROM}
            <!-- Which way the power goes: left is back towards the transformer. -->
            <path
              class="arrow"
              d={line.powerW < 0 ? "M4 -5 -4 0 4 5" : "M-4 -5 4 0 -4 5"}
              transform="translate({line.arrow.x} {line.arrow.y})"
            />
          {/if}
          <!-- A wider target than the wire. -->
          <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
          <polyline class="hit" points={line.path} onclick={() => onselect("line", line.id)} />
        </g>
      {/each}
    </svg>

    <svg viewBox={box} class="sheet moving" aria-hidden="true">
      {#each flows as line (line.id)}
        <polyline
          class="flow"
          data-flow={line.name}
          data-direction={line.powerW! < 0 ? "back" : "out"}
          points={line.path}
          stroke-width={Math.max(1, 0.35 * (1.5 + 5 * line.weight))}
          style:--pace="{pace(line.weight)}s"
        />
      {/each}
    </svg>

    <svg viewBox={box} class="sheet" aria-hidden="true">
      {#if view.binding?.nodeId}
        {@const held = view.nodes.find((n) => n.id === view.binding?.nodeId)}
        {#if held}<circle class="halo" cx={held.x} cy={held.y} r="14" />{/if}
      {/if}

      {#each view.nodes as node (node.id)}
        <g
          class="bus"
          data-level={node.level}
          data-selected={selected === node.id ? "" : undefined}
          data-bus={node.name}
          use:hint={() => ({
            title: `Bus ${node.name}`,
            lines: [
              LEVEL_WORDS[node.level],
              node.label,
              ...(node.sites.length > 0 ? [`Sites here: ${node.sites.join(", ")}`] : []),
            ],
            level: node.level,
          })}
        >
          {#if node === root}
            <!-- The transformer. -->
            <rect class="dot" x={node.x - 8} y={node.y - 8} width="16" height="16" rx="3" />
          {:else}
            <circle
              class="dot"
              class:enrolled={node.enrolled}
              cx={node.x}
              cy={node.y}
              r={node.enrolled ? 6.5 : node.sites.length > 0 ? 4 : 2.5}
            />
          {/if}
          <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
          <circle
            class="hit"
            cx={node.x}
            cy={node.y}
            r="12"
            onclick={() => onselect("bus", node.id)}
          />
        </g>
      {/each}

      <!-- The scale: cable from the transformer. -->
      {#if root}
        <text class="scale" x={root.x} y={scaleY}>0 m</text>
        <text class="scale" x={view.width - 28} y={scaleY} text-anchor="end"
          >{Math.round(view.lengthM)} m of cable from the transformer</text
        >
      {/if}
    </svg>
  </div>
</div>

<style>
  .schematic {
    color: var(--color-ok);
  }
  /* Zoomed in, the drawing can be dragged along. */
  .schematic.zoomed {
    cursor: grab;
  }
  .schematic.zoomed:active {
    cursor: grabbing;
  }
  /* The zoom's buttons, in a row of their own above the drawing: over it,
     they would hide the far end of a small feeder. */
  .controls {
    display: flex;
    justify-content: flex-end;
    gap: 0.25rem;
    border-bottom: 1px solid var(--color-rule);
    padding: 0.375rem 0.5rem;
  }
  .controls :global(.btn) {
    min-width: 2.25rem;
    padding-inline: 0.5rem;
  }
  /* A sheet lies exactly over the first, and lets the pointer through to
     what is under it except where it has a mark of its own. */
  .sheet {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    pointer-events: none;
  }
  .sheet .hit {
    pointer-events: auto;
  }
  /* The sheet of dashes is a layer to the browser: painted alone. */
  .moving {
    will-change: transform;
  }
  [data-level="info"] {
    color: var(--color-control);
  }
  [data-level="warn"] {
    color: var(--color-warn);
  }
  [data-level="critical"] {
    color: var(--color-critical);
  }
  .wire {
    fill: none;
    stroke: currentColor;
    stroke-linejoin: round;
    stroke-linecap: round;
  }
  .switch {
    stroke-dasharray: 2 3;
  }
  /* The dashes run from the transformer outwards, or back towards it. They
     are drawn in the colour of the page, as gaps that move along the wire. */
  .flow {
    fill: none;
    stroke: var(--color-surface);
    stroke-linecap: round;
    stroke-dasharray: 3 11;
    pointer-events: none;
    animation: flow-out var(--pace) linear infinite;
  }
  .flow[data-direction="back"] {
    animation-name: flow-back;
  }
  @keyframes flow-out {
    to {
      stroke-dashoffset: -14;
    }
  }
  @keyframes flow-back {
    to {
      stroke-dashoffset: 14;
    }
  }
  /* Dashes that stand still would say "switch", not "flow". */
  @media (prefers-reduced-motion: reduce) {
    .flow {
      display: none;
    }
  }
  .arrow {
    fill: none;
    stroke: var(--color-text);
    stroke-width: 1.5;
    stroke-linecap: round;
    stroke-linejoin: round;
  }
  .dot {
    fill: currentColor;
    stroke: var(--color-surface);
    stroke-width: 1;
  }
  /* A site that takes part in envelopes: a ring, not a spot. */
  .dot.enrolled {
    fill: var(--color-surface);
    stroke: currentColor;
    stroke-width: 3;
  }
  .hit {
    fill: transparent;
    stroke: transparent;
    stroke-width: 14;
    cursor: pointer;
  }
  polyline.hit {
    fill: none;
  }
  .halo {
    fill: none;
    stroke: var(--color-accent);
    stroke-width: 2;
    stroke-dasharray: 4 3;
  }
  polyline.halo {
    stroke-width: 12;
    stroke-opacity: 0.35;
    stroke-dasharray: none;
    stroke-linejoin: round;
  }
  [data-selected] .dot,
  [data-selected] .wire {
    stroke: var(--color-accent);
  }
  [data-selected] .wire {
    stroke-width: 6;
  }
  [data-selected] .dot {
    stroke-width: 3;
  }
  .scale {
    fill: var(--color-muted);
    font-size: 12px;
  }
</style>
