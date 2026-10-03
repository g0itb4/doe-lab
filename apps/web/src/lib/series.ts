// The time ranges the overview offers. A range is centred on now: the
// forecast and the envelopes run ahead of it, the telemetry behind.
export const RANGES = {
  "6h": 6 * 3600,
  "24h": 24 * 3600,
  "3d": 72 * 3600,
} as const;
export type RangeKey = keyof typeof RANGES;
export const DEFAULT_RANGE: RangeKey = "24h";

export function rangeKey(value: string | null): RangeKey {
  return value !== null && value in RANGES ? (value as RangeKey) : DEFAULT_RANGE;
}

// The window of a range around now, on half-hour boundaries so that a refresh
// a second later asks for the same window.
export function windowOf(key: RangeKey, nowSeconds: number): { from: number; to: number } {
  const half = RANGES[key] / 2;
  const anchor = Math.floor(nowSeconds / 1800) * 1800;
  return { from: anchor - half, to: anchor + half + 1800 };
}

// A zoom from the URL: two unix seconds, in order. Anything else is no zoom.
export function zoomOf(from: string | null, to: string | null): [number, number] | undefined {
  const a = Number(from);
  const b = Number(to);
  if (from === null || to === null || !Number.isFinite(a) || !Number.isFinite(b) || b <= a)
    return undefined;
  return [a, b];
}

export type Column = (number | null)[];

// Reduces a series to about two points per pixel: each bucket keeps its
// lowest and its highest point, so a spike survives. A chart cannot show
// more than its pixels, and drawing ten thousand points to fill six hundred
// costs time on every frame.
export function downsample(
  x: number[],
  columns: Column[],
  width: number,
): { x: number[]; columns: Column[] } {
  const buckets = Math.max(1, Math.floor(width));
  if (x.length <= buckets * 2) return { x, columns };

  const size = x.length / buckets;
  const keep = new Set<number>([0, x.length - 1]);
  for (let b = 0; b < buckets; b++) {
    const start = Math.floor(b * size);
    const end = Math.min(x.length, Math.floor((b + 1) * size));
    for (const column of columns) {
      let lo = -1;
      let hi = -1;
      for (let i = start; i < end; i++) {
        const v = column[i];
        if (v === null || v === undefined) continue;
        if (lo < 0 || v < (column[lo] as number)) lo = i;
        if (hi < 0 || v > (column[hi] as number)) hi = i;
      }
      if (lo >= 0) keep.add(lo).add(hi);
    }
  }
  const index = [...keep].sort((a, b) => a - b);
  return {
    x: index.map((i) => x[i]!),
    columns: columns.map((column) => index.map((i) => column[i] ?? null)),
  };
}

// The part of a series that a range shows: its points inside the range, and
// one either side so that a line runs to the edge of the plot. A chart that
// is zoomed in then spends its pixels on what is in view, and not on the
// whole series it was cut from. `x` is ascending.
export function windowed(
  x: number[],
  columns: Column[],
  range: [number, number] | undefined,
): { x: number[]; columns: Column[] } {
  if (!range || x.length === 0) return { x, columns };
  let from = 0;
  while (from < x.length && x[from]! < range[0]) from++;
  let to = x.length;
  while (to > 0 && x[to - 1]! > range[1]) to--;
  from = Math.max(0, from - 1);
  to = Math.min(x.length, to + 1);
  if (from === 0 && to === x.length) return { x, columns };
  return { x: x.slice(from, to), columns: columns.map((column) => column.slice(from, to)) };
}

// Which series of which chart are hidden, from the address: "export.1,load.0"
// hides the second series of the chart "export" and the first of "load".
// Anything that is not of that shape is ignored.
export function hiddenOf(value: string | null, chart: string): number[] {
  const hidden = new Set<number>();
  for (const part of (value ?? "").split(",")) {
    const [id, index, ...rest] = part.split(".");
    const i = Number(index);
    if (id === chart && rest.length === 0 && index !== "" && Number.isInteger(i) && i >= 0)
      hidden.add(i);
  }
  return [...hidden].sort((a, b) => a - b);
}

// The address's value with one chart's hidden series replaced: null when
// nothing at all is hidden, so that the parameter goes.
export function withHidden(value: string | null, chart: string, hidden: number[]): string | null {
  const others = (value ?? "")
    .split(",")
    .filter((part) => part !== "" && part.split(".")[0] !== chart);
  const own = [...new Set(hidden)].sort((a, b) => a - b).map((i) => `${chart}.${i}`);
  return [...others, ...own].join(",") || null;
}

// What a click on a series of the legend does to the hidden set. A plain
// click hides the series, or shows it again. With `alone`, it shows that
// series alone; and when it is alone already, everything again.
export function toggleHidden(
  hidden: number[],
  index: number,
  count: number,
  alone = false,
): number[] {
  if (!alone) {
    return hidden.includes(index)
      ? hidden.filter((i) => i !== index)
      : [...hidden, index].sort((a, b) => a - b);
  }
  const others = Array.from({ length: count }, (_, i) => i).filter((i) => i !== index);
  const isAlone = !hidden.includes(index) && others.every((i) => hidden.includes(i));
  return isAlone ? [] : others;
}
