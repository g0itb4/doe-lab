<script lang="ts">
  import type { Schematic } from "$lib/map/schematic.ts";

  // A feeder drawn as a schematic: the transformer on the left, each bus as
  // far to the right as there is cable to it, each branch in a lane. A line is
  // as heavy as the power it carries and coloured by how near its rating it
  // is; a bus is coloured by how near the voltage band's edge it is. A click
  // on a mark selects it; the page shows what it is.
  let {
    view,
    selected = "",
    onselect,
  }: {
    view: Schematic;
    // The id of the selected bus or line.
    selected?: string;
    onselect: (kind: "bus" | "line", id: string) => void;
  } = $props();

  const path = (points: [number, number][]) => points.map(([x, y]) => `${x},${y}`).join(" ");
  // Where a line's arrow sits: the middle of its last, level stretch.
  const arrowAt = (points: [number, number][]) => {
    const [a, b] = [points[points.length - 2]!, points[points.length - 1]!];
    return { x: (a[0] + b[0]) / 2, y: b[1] };
  };
  const root = $derived(view.nodes[0]);
  const scaleY = $derived(view.height - 8);
</script>

<!-- A wide drawing scrolls inside its own box; the page does not. -->
<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
<div class="card overflow-x-auto" tabindex="0" role="region" aria-label="Schematic of the feeder">
  <svg
    viewBox="0 0 {view.width} {view.height}"
    class="schematic block h-auto w-full min-w-[720px]"
    role="img"
    aria-label="The feeder, from the transformer on the left to its furthest bus, {Math.round(
      view.lengthM,
    )} metres of cable away. The table below lists the buses and lines closest to their limits."
  >
    {#if view.binding?.lineId}
      {@const held = view.lines.find((l) => l.id === view.binding?.lineId)}
      {#if held}<polyline class="halo" points={path(held.points)} />{/if}
    {/if}

    {#each view.lines as line (line.id)}
      <g
        class="line"
        data-level={line.level}
        data-selected={selected === line.id ? "" : undefined}
        data-line={line.name}
      >
        <title>{line.name}: {line.label}</title>
        <polyline
          class="wire"
          class:switch={line.isSwitch}
          points={path(line.points)}
          stroke-width={1.5 + 5 * line.weight}
        />
        {#if line.powerW !== undefined && line.weight > 0.08}
          {@const at = arrowAt(line.points)}
          <!-- Which way the power goes: left is back towards the transformer. -->
          <path
            class="arrow"
            d={line.powerW < 0 ? "M4 -5 -4 0 4 5" : "M-4 -5 4 0 -4 5"}
            transform="translate({at.x} {at.y})"
          />
        {/if}
        <!-- A wider target than the wire. -->
        <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
        <polyline
          class="hit"
          points={path(line.points)}
          onclick={() => onselect("line", line.id)}
        />
      </g>
    {/each}

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
      >
        <title
          >{node.name}{node.sites.length > 0 ? ` (${node.sites.join(", ")})` : ""}: {node.label}</title
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

<style>
  .schematic {
    color: var(--color-ok);
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
