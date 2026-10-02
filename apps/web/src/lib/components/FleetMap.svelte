<script lang="ts" module>
  import type { Kind } from "$lib/map/fleet.ts";
  import type { Level } from "$lib/status.ts";

  // Where the streets come from: the raster tiles of OpenStreetMap, which
  // need no key. The host is the one exception in the app's content security
  // policy (svelte.config.js). There is one style, drawn for a light page;
  // in the dark theme the stylesheet below inverts it.
  export type Tiles = { url: string; attribution: string };
  export const TILES: Tiles = {
    url: "https://tile.openstreetmap.org/{z}/{x}/{y}.png",
    attribution:
      '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors',
  };

  // Below this zoom a substation's sites are a smudge around it, so only the
  // substations are drawn.
  export const SITES_FROM_ZOOM = 12;

  // The equipment of a site, on the 20 px grid of Icon.svelte.
  const KIND_PATHS: Record<Kind, string> = {
    solar:
      "M10 6.5a3.5 3.5 0 1 0 0 7 3.5 3.5 0 0 0 0-7ZM10 1.5v2m0 13v2M1.5 10h2m13 0h2M4 4l1.4 1.4m9.2 9.2L16 16M4 16l1.4-1.4m9.2-9.2L16 4",
    battery: "M3 6.5h12v7H3v-7Zm12 2h2v3h-2M6 10h2m3 0h2m-1-1v2",
    ev: "M11.5 2 5 11h4.5l-1 7L15 9h-4.5l1-7Z",
    hybrid:
      "M3 10.5h12v6.5H3v-6.5Zm12 2h2v2.5h-2M6.5 7.5a3.5 3.5 0 0 1 7 0M10 1.5v1.5M4.5 3.5l1 1.2m10-1.2-1 1.2",
  };
  // The status shapes of Icon.svelte: the shape says what the colour says.
  const LEVEL_PATHS: Record<Level, string> = {
    ok: "M10 2a8 8 0 1 0 0 16 8 8 0 0 0 0-16Zm-3.5 8.5 2.5 2.5 4.5-5",
    info: "M10 2a8 8 0 1 0 0 16 8 8 0 0 0 0-16Zm0 7v5m0-8v.5",
    warn: "M10 2.5 18 17H2L10 2.5Zm0 5.5v4m0 2.5v.5",
    critical: "M6.5 2h7L18 6.5v7L13.5 18h-7L2 13.5v-7L6.5 2ZM10 6v5m0 2.5v.5",
  };

  const RING = 2 * Math.PI * 14;

  // A site: its equipment in a ring that fills as the site uses its limit,
  // and for anything but "all is well" the status shape on its shoulder.
  export function siteHtml(kind: Kind, level: Level, fill: number, selected: boolean): string {
    const badge =
      level === "ok"
        ? ""
        : `<svg class="fleet-badge" viewBox="0 0 20 20" width="14" height="14"><path d="${LEVEL_PATHS[level]}"/></svg>`;
    return (
      `<span class="fleet-site" data-level="${level}"${selected ? " data-selected" : ""}>` +
      `<svg viewBox="0 0 32 32" width="32" height="32">` +
      `<circle class="fleet-disc" cx="16" cy="16" r="14"/>` +
      `<circle class="fleet-ring" cx="16" cy="16" r="14" transform="rotate(-90 16 16)" stroke-dasharray="${(fill * RING).toFixed(1)} ${RING.toFixed(1)}"/>` +
      `<path class="fleet-glyph" transform="translate(7 7) scale(0.9)" d="${KIND_PATHS[kind]}"/>` +
      `</svg>${badge}</span>`
    );
  }

  // A substation: its status shape in a border that fills as its sites
  // together use their export limits, and its name beneath. The border is
  // measured in hundredths of its own length.
  export function substationHtml(
    name: string,
    level: Level,
    fill: number,
    selected: boolean,
  ): string {
    const edge = 'x="1.5" y="1.5" width="33" height="33" rx="6.5"';
    return (
      `<span class="fleet-substation" data-level="${level}"${selected ? " data-selected" : ""}>` +
      `<svg class="fleet-edge" viewBox="0 0 36 36" width="36" height="36">` +
      `<rect class="fleet-disc" ${edge}/>` +
      `<rect class="fleet-ring" ${edge} pathLength="100" stroke-dasharray="${(fill * 100).toFixed(1)} 100"/>` +
      `</svg>` +
      `<svg class="fleet-status" viewBox="0 0 20 20" width="22" height="22"><path d="${LEVEL_PATHS[level]}"/></svg>` +
      `</span><span class="fleet-name">${escape(name)}</span>`
    );
  }

  function escape(text: string): string {
    return text.replace(/[&<>"']/g, (c) => `&#${c.charCodeAt(0)};`);
  }
</script>

<script lang="ts">
  import "leaflet/dist/leaflet.css";
  import L from "leaflet";
  import { onMount } from "svelte";
  import type { Bounds, FleetView } from "$lib/map/fleet.ts";
  import { KIND_WORDS } from "$lib/map/fleet.ts";
  import { theme } from "$lib/theme.svelte.ts";

  // The fleet on a street map: substations, and from a closer zoom the sites
  // around each, tied to it by a line per feeder. A click on a mark selects
  // it; the page shows what it is.
  let {
    view,
    bounds,
    selected = "",
    tiles = TILES,
    onselect,
  }: {
    view: FleetView;
    // What to frame. The map moves when this changes, and not otherwise.
    bounds: Bounds | undefined;
    // The NMI of the selected site, or the code of the selected substation.
    selected?: string;
    tiles?: Tiles;
    onselect: (kind: "site" | "substation", key: string) => void;
  } = $props();

  let el: HTMLDivElement;
  let map = $state<L.Map>();
  const substationLayer = L.layerGroup();
  const siteLayer = L.layerGroup();
  // Each mark by its key, with the HTML it was last drawn with: a poll that
  // changes nothing touches nothing, so the keyboard focus stays where it is.
  type Mark = { marker: L.Marker; html: string; label: string };
  const drawn = new Map<string, Mark>();
  const spokes = new Map<string, L.Polyline>();

  function dark(): boolean {
    return (
      theme.choice === "dark" ||
      (theme.choice === "system" && matchMedia("(prefers-color-scheme: dark)").matches)
    );
  }

  onMount(() => {
    const still = matchMedia("(prefers-reduced-motion: reduce)").matches;
    const m = L.map(el, {
      zoomAnimation: !still,
      fadeAnimation: !still,
      markerZoomAnimation: !still,
      // The whole of the demo's fleet is in south-east Australia.
      center: [-35.5, 146],
      zoom: 5,
      minZoom: 4,
      maxZoom: 17,
    });
    m.attributionControl.setPrefix(false);
    substationLayer.addTo(m);
    const showSites = () => {
      if (m.getZoom() >= SITES_FROM_ZOOM) siteLayer.addTo(m);
      else siteLayer.remove();
    };
    m.on("zoomend", showSites);
    showSites();
    map = m;
    return () => {
      m.remove();
      map = undefined;
    };
  });

  // The streets, and whether the theme asks for them dark.
  let night = $state(false);
  $effect(() => {
    void theme.version;
    night = dark();
  });
  $effect(() => {
    if (!map) return;
    const streets = L.tileLayer(tiles.url, { attribution: tiles.attribution, maxZoom: 19 });
    streets.addTo(map).bringToBack();
    return () => streets.remove();
  });

  $effect(() => {
    if (map && bounds) map.fitBounds(bounds, { padding: [48, 48], maxZoom: 14 });
  });

  function place(
    layer: L.LayerGroup,
    key: string,
    at: L.LatLngExpression,
    html: string,
    size: number,
    label: string,
    focusable: boolean,
    onclick: () => void,
  ) {
    const had = drawn.get(key);
    if (had?.html === html) {
      // The same picture can have a new name: "Loading" and "Not reporting"
      // look alike.
      if (had.label !== label) {
        had.label = label;
        name(had);
      }
      return;
    }
    const icon = L.divIcon({
      html,
      className: "fleet-mark",
      iconSize: [size, size],
      iconAnchor: [size / 2, size / 2],
    });
    if (had) {
      had.html = html;
      had.label = label;
      had.marker.setIcon(icon);
      name(had);
      return;
    }
    const marker = L.marker(at, { icon, keyboard: focusable, title: label }).on("click", onclick);
    const mark = { marker, html, label };
    // A mark has an element only while its layer is on the map: the sites
    // come and go with the zoom.
    marker.on("add", () => name(mark));
    drawn.set(key, mark);
    marker.addTo(layer);
  }

  // What a screen reader calls a mark: what it is, and its status.
  function name(mark: Mark) {
    const el = mark.marker.getElement();
    // A mark the keyboard reaches is a button, by Leaflet's hand. The others
    // are pictures with a name: a name on a plain element is not allowed.
    if (el && !el.hasAttribute("role")) el.setAttribute("role", "img");
    el?.setAttribute("aria-label", mark.label);
    el?.setAttribute("title", mark.label);
  }

  // The marks, kept in step with the view.
  $effect(() => {
    if (!map) return;
    const keys = new Set<string>();
    const substations = new Map(view.substations.map((s) => [s.code, s]));
    for (const s of view.substations) {
      const key = `substation:${s.code}`;
      keys.add(key);
      place(
        substationLayer,
        key,
        [s.latitude, s.longitude],
        substationHtml(s.place, s.status.level, s.fill, selected === s.code),
        36,
        `${s.name}: ${s.status.label}`,
        true,
        () => onselect("substation", s.code),
      );
    }
    for (const s of view.sites) {
      const key = `site:${s.nmi}`;
      keys.add(key);
      // Not in the tab order: there are too many. The table under the map is
      // the way to a site from the keyboard.
      place(
        siteLayer,
        key,
        [s.latitude, s.longitude],
        siteHtml(s.kind, s.status.level, s.fill, selected === s.nmi),
        32,
        `${s.nmi}, ${KIND_WORDS[s.kind]}: ${s.status.label}`,
        false,
        () => onselect("site", s.nmi),
      );
      const substation = substations.get(s.substationCode);
      if (substation && !spokes.has(key)) {
        spokes.set(
          key,
          L.polyline(
            [
              [substation.latitude, substation.longitude],
              [s.latitude, s.longitude],
            ],
            { className: `fleet-spoke fleet-spoke-${s.feederIndex % 3}`, interactive: false },
          ).addTo(siteLayer),
        );
      }
    }
    for (const [key, { marker }] of drawn) {
      if (keys.has(key)) continue;
      marker.remove();
      drawn.delete(key);
      spokes.get(key)?.remove();
      spokes.delete(key);
    }
  });
</script>

<div
  bind:this={el}
  class="fleet-map border-rule h-[30rem] w-full rounded-lg border"
  data-night={night ? "" : undefined}
  role="region"
  aria-label="Map of the fleet"
></div>

<style>
  /* Leaflet draws the marks outside Svelte, so the rules are global, under
     the map's own class. Every colour is a design token. */
  .fleet-map {
    /* Under the page's own layers: the header, a dialog, a toast. */
    isolation: isolate;
    background: var(--color-sunken);
  }
  .fleet-map :global(.leaflet-container),
  .fleet-map:global(.leaflet-container) {
    font: inherit;
  }
  /* The tiles are drawn for a light page. At night they are inverted, and
     turned back through the colour wheel so that water stays blue and parks
     green. */
  .fleet-map[data-night] :global(.leaflet-tile-pane) {
    filter: invert(1) hue-rotate(180deg) brightness(0.95) contrast(0.9);
  }
  .fleet-map :global(.fleet-mark) {
    background: none;
    border: 0;
  }
  .fleet-map :global(.fleet-site),
  .fleet-map :global(.fleet-substation) {
    position: relative;
    display: block;
    color: var(--color-ok);
  }
  .fleet-map :global([data-level="info"]) {
    color: var(--color-accent);
  }
  .fleet-map :global([data-level="warn"]) {
    color: var(--color-warn);
  }
  .fleet-map :global([data-level="critical"]) {
    color: var(--color-critical);
  }
  .fleet-map :global(.fleet-disc) {
    fill: var(--color-surface);
    stroke: var(--color-control);
    stroke-width: 1.5;
  }
  .fleet-map :global(.fleet-ring) {
    fill: none;
    stroke: currentColor;
    stroke-width: 3;
  }
  .fleet-map :global(.fleet-glyph) {
    fill: none;
    stroke: var(--color-text);
    stroke-width: 1.7;
    stroke-linecap: round;
    stroke-linejoin: round;
  }
  .fleet-map :global(.fleet-badge) {
    position: absolute;
    top: -3px;
    right: -5px;
    fill: var(--color-surface);
    stroke: currentColor;
    stroke-width: 2;
    stroke-linecap: round;
    stroke-linejoin: round;
  }
  .fleet-map :global(.fleet-substation) {
    display: grid;
    width: 36px;
    height: 36px;
    place-items: center;
  }
  .fleet-map :global(.fleet-edge) {
    position: absolute;
    inset: 0;
  }
  .fleet-map :global(.fleet-status) {
    position: relative;
    fill: none;
    stroke: currentColor;
    stroke-width: 1.7;
    stroke-linecap: round;
    stroke-linejoin: round;
  }
  .fleet-map :global([data-selected]) {
    outline: 3px solid var(--color-accent);
    outline-offset: 2px;
    border-radius: 9999px;
  }
  .fleet-map :global(.fleet-substation[data-selected]) {
    border-radius: 8px;
  }
  .fleet-map :global(.fleet-name) {
    position: absolute;
    top: 40px;
    left: 50%;
    padding: 0 6px;
    transform: translateX(-50%);
    border: 1px solid var(--color-rule);
    border-radius: 4px;
    background: var(--color-surface);
    color: var(--color-text);
    font-size: 12px;
    font-weight: 600;
    line-height: 18px;
    white-space: nowrap;
  }
  /* A feeder's sites are tied to their substation by one kind of line: told
     apart by dash as well as by colour. */
  .fleet-map :global(.fleet-spoke) {
    fill: none;
    stroke: var(--color-series-1);
    stroke-width: 1.5;
    stroke-opacity: 0.8;
  }
  .fleet-map :global(.fleet-spoke-1) {
    stroke: var(--color-series-2);
    stroke-dasharray: 6 4;
  }
  .fleet-map :global(.fleet-spoke-2) {
    stroke: var(--color-series-3);
    stroke-dasharray: 2 4;
  }
  /* Leaflet's own controls, in the app's colours. */
  .fleet-map :global(.leaflet-bar a),
  .fleet-map :global(.leaflet-control-attribution) {
    background: var(--color-surface);
    color: var(--color-text);
  }
  .fleet-map :global(.leaflet-bar a) {
    border-color: var(--color-rule);
  }
  .fleet-map :global(.leaflet-control-attribution) {
    font-size: 11px;
  }
  /* A target of 24 px, like every other link that stands on its own. */
  .fleet-map :global(.leaflet-control-attribution a) {
    display: inline-block;
    min-height: 24px;
    line-height: 24px;
  }
  .fleet-map :global(.leaflet-control-attribution a) {
    color: var(--color-accent);
    text-decoration: underline;
  }
</style>
