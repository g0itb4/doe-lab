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
