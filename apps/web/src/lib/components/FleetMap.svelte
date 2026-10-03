<script lang="ts" module>
  import type { Kind } from "$lib/map/fleet.ts";
  import type { Level } from "$lib/status.ts";
  import { MARKS } from "./StatusMark.svelte";

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

  // The equipment of a site, on a 20 px grid: solid shapes, light on the
  // dark of the mark, so each reads at a glance over a busy street map. The
  // closed parts are filled and the open ones are strokes of the same colour.
  const KIND_PATHS: Record<Kind, string> = {
    // A sun.
    solar:
      "M10 6.6a3.4 3.4 0 1 0 0 6.8a3.4 3.4 0 0 0 0-6.8ZM10 1.9v1.5M10 16.6v1.5M1.9 10h1.5M16.6 10h1.5M4.3 4.3l1.05 1.05M14.65 14.65l1.05 1.05M4.3 15.7l1.05-1.05M14.65 5.35l1.05-1.05",
    // A battery, on its side.
    battery:
      "M3.4 6.9h10.9a.9.9 0 0 1 .9.9v4.4a.9.9 0 0 1-.9.9H3.4a.9.9 0 0 1-.9-.9V7.8a.9.9 0 0 1 .9-.9ZM17.3 8.8v2.4",
    // A bolt.
    ev: "M11.4 2.3 5.3 10.8h3.8l-.9 6.9 6.5-8.6h-3.8l.5-6.8Z",
    // The sun over the battery it fills.
    hybrid:
      "M3.4 10.9h10.9a.9.9 0 0 1 .9.9v3.6a.9.9 0 0 1-.9.9H3.4a.9.9 0 0 1-.9-.9v-3.6a.9.9 0 0 1 .9-.9ZM17.3 12.5v2.2M6.8 7.6a3.2 3.2 0 0 1 6.4 0ZM10 1.9v1.2M4.9 3.7l.85.95M15.1 3.7l-.85.95",
  };
  // A substation: the two rings of a transformer, as a network's own
  // drawings have it.
  const SUBSTATION_PATH =
    "M10 3.4a3.9 3.9 0 1 0 0 7.8a3.9 3.9 0 0 0 0-7.8ZM10 8.8a3.9 3.9 0 1 0 0 7.8a3.9 3.9 0 0 0 0-7.8Z";
  // The status marks of the rest of the app (StatusMark.svelte): the shape
  // says what the colour says.
  const LEVEL_PATHS = MARKS;

  const RING = 2 * Math.PI * 14;

  // For anything but "all is well", the status shape on a mark's shoulder.
  const badge = (level: Level) =>
    level === "ok"
      ? ""
      : `<svg class="fleet-badge" viewBox="0 0 12 12" width="14" height="14"><path fill-rule="evenodd" d="${LEVEL_PATHS[level]}"/></svg>`;

  // A site: its equipment on a dark disc, in a ring that fills as the site
  // uses its limit, with its status on its shoulder unless all is well.
  export function siteHtml(kind: Kind, level: Level, fill: number, selected: boolean): string {
    return (
      `<span class="fleet-site" data-level="${level}"${selected ? " data-selected" : ""}>` +
      `<svg class="fleet-body" viewBox="0 0 32 32" width="32" height="32">` +
      `<circle class="fleet-disc" cx="16" cy="16" r="14"/>` +
      `<circle class="fleet-ring" cx="16" cy="16" r="14" transform="rotate(-90 16 16)" stroke-dasharray="${(fill * RING).toFixed(1)} ${RING.toFixed(1)}"/>` +
      `<path class="fleet-glyph" transform="translate(7.5 7.5) scale(0.85)" d="${KIND_PATHS[kind]}"/>` +
      `</svg>${badge(level)}</span>`
    );
  }

  // A substation: a transformer's two rings on a dark square, in a border
  // that fills as its sites together use their export limits, with its
  // status on its shoulder unless all is well, and its name beneath. The
  // border is measured in hundredths of its own length.
  export function substationHtml(
    name: string,
    level: Level,
    fill: number,
    selected: boolean,
  ): string {
    const edge = 'x="1.5" y="1.5" width="33" height="33" rx="8"';
    return (
      `<span class="fleet-substation" data-level="${level}"${selected ? " data-selected" : ""}>` +
      `<svg class="fleet-body" viewBox="0 0 36 36" width="36" height="36">` +
      `<rect class="fleet-disc" ${edge}/>` +
      `<rect class="fleet-ring" ${edge} pathLength="100" stroke-dasharray="${(fill * 100).toFixed(1)} 100"/>` +
      `<path class="fleet-symbol" transform="translate(8 8)" d="${SUBSTATION_PATH}"/>` +
      `</svg>${badge(level)}</span><span class="fleet-name">${escape(name)}</span>`
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
  import { type Card, hint, type Plot } from "$lib/hovercard.svelte.ts";
  import type { Bounds, FleetView, SiteMark } from "$lib/map/fleet.ts";
  import { flowWords, KIND_WORDS } from "$lib/map/fleet.ts";
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
    trace,
  }: {
    view: FleetView;
    // What to frame. The map moves when this changes, and not otherwise.
    bounds: Bounds | undefined;
    // The NMI of the selected site, or the code of the selected substation.
    selected?: string;
    tiles?: Tiles;
    onselect: (kind: "site" | "substation", key: string) => void;
    // The plot for a site's card: what it has exported of late, against its
    // limit. The page knows where that comes from; the map only shows it.
    trace?: (site: SiteMark) => Plot;
  } = $props();

  let el: HTMLDivElement;
  let map = $state<L.Map>();
  const substationLayer = L.layerGroup();
  const siteLayer = L.layerGroup();
  // Each mark by its key, with the HTML it was last drawn with: a poll that
  // changes nothing touches nothing, so the keyboard focus stays where it is.
  // And what its card says, and the element the card is hung on.
  type Mark = {
    marker: L.Marker;
    html: string;
    label: string;
    card: Card;
    hinted?: { on: Element; action: ReturnType<typeof hint> };
  };
  const drawn = new Map<string, Mark>();
  const spokes = new Map<string, L.Polyline>();

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
  const night = $derived(theme.scheme === "dark");
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
    card: Card,
    focusable: boolean,
    onclick: () => void,
  ) {
    const had = drawn.get(key);
    if (had) had.card = card;
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
    const marker = L.marker(at, { icon, keyboard: focusable }).on("click", onclick);
    const mark: Mark = { marker, html, label, card };
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
    // And what it says to the pointer, or to the keyboard on a button: a card,
    // hung once on each element a mark has.
    if (el && mark.hinted?.on !== el) {
      mark.hinted?.action.destroy();
      mark.hinted = { on: el, action: hint(el, () => mark.card) };
    }
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
        // A word for its status, and how many of its sites are heard.
        {
          title: s.name,
          lines: [s.status.label, `${s.reporting} of ${s.sites} sites reporting`],
          level: s.status.level,
        },
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
        // Little to read: its status, the shape of its export against its
        // limit, and what it is doing now in a few words.
        {
          title: `${s.nmi}, ${KIND_WORDS[s.kind]}`,
          lines: [s.status.label, ...[flowWords(s)].filter((line) => line !== undefined)],
          level: s.status.level,
          plot: trace?.(s),
        },
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
    for (const [key, { marker, hinted }] of drawn) {
      if (keys.has(key)) continue;
      hinted?.action.destroy();
      marker.remove();
      drawn.delete(key);
      spokes.get(key)?.remove();
      spokes.delete(key);
    }
  });
</script>

<div
  bind:this={el}
  class="fleet-map border-rule rounded-card h-80 w-full border sm:h-[30rem]"
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
  /* A mark is dark with a light edge, as a pin on a map is: it reads over
     any street, and its shadow lifts it off them. The edge is the track of
     the ring that fills with the status colour. */
  .fleet-map :global(.fleet-body) {
    display: block;
    overflow: visible;
    filter: drop-shadow(0 1px 2px var(--color-shade-deep));
  }
  .fleet-map :global(.fleet-disc) {
    fill: var(--color-text);
    stroke: var(--color-surface);
    stroke-width: 3;
  }
  /* Square ends: a ring that is empty draws nothing at all. */
  .fleet-map :global(.fleet-ring) {
    fill: none;
    stroke: currentColor;
    stroke-width: 3;
  }
  /* Solid shapes in the colour of the page, on the dark of the mark. */
  .fleet-map :global(.fleet-glyph) {
    fill: var(--color-surface);
    stroke: var(--color-surface);
    stroke-width: 1.5;
    stroke-linecap: round;
    stroke-linejoin: round;
  }
  .fleet-map :global(.fleet-symbol) {
    fill: none;
    stroke: var(--color-surface);
    stroke-width: 1.9;
  }
  .fleet-map :global(.fleet-badge) {
    position: absolute;
    top: -4px;
    right: -5px;
    /* A solid mark, edged in the surface so it stands off the ring. */
    fill: currentColor;
    stroke: var(--color-surface);
    stroke-width: 2.5;
    stroke-linejoin: round;
    paint-order: stroke;
  }
  .fleet-map :global(.fleet-substation) {
    width: 36px;
    height: 36px;
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
