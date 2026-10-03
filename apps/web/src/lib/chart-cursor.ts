// What a chart's cursor needs that is not drawing: which point a time is,
// where the readout goes, what a key does, and what to say of a point. Plain
// functions, so the component only has to put the answers on the screen.

// The index of the point nearest in time: a binary search, because a pointer
// asks on every move. -1 for a chart of nothing.
export function rowAt(x: readonly number[], time: number): number {
  if (x.length === 0) return -1;
  let lo = 0;
  let hi = x.length - 1;
  while (lo < hi) {
    const mid = (lo + hi) >> 1;
    if (x[mid]! < time) lo = mid + 1;
    else hi = mid;
  }
  // `lo` is the first point at or after the time; the one before may be nearer.
  return lo > 0 && time - x[lo - 1]! <= x[lo]! - time ? lo - 1 : lo;
}

// Where a readout sits beside the cursor, as the CSS of a box inside the
// plot: on the side with more room, so that it never leaves the plot and
// never covers the point it is about. Each of the four sides is set or left
// out, and `maxWidth` is the room there is on the side it was put: on a
// phone's chart a readout is narrower than it would like to be.
export type Placement = {
  left?: number;
  right?: number;
  top?: number;
  bottom?: number;
  maxWidth: number;
};

export function place(
  cursor: { left: number; top: number },
  plot: { width: number; height: number },
  gap = 12,
): Placement {
  const across =
    cursor.left <= plot.width / 2
      ? { left: cursor.left + gap, maxWidth: Math.max(0, plot.width - cursor.left - gap) }
      : { right: plot.width - cursor.left + gap, maxWidth: Math.max(0, cursor.left - gap) };
  const down =
    cursor.top <= plot.height / 2
      ? { top: Math.max(0, cursor.top + gap) }
      : { bottom: Math.max(0, plot.height - cursor.top + gap) };
  return { ...across, ...down };
}

// The keys that move a cursor through the points of a chart, as a slider's
// do: one point, a tenth of the chart, or to either end.
export const CURSOR_KEYS = [
  "ArrowLeft",
  "ArrowRight",
  "ArrowUp",
  "ArrowDown",
  "PageUp",
  "PageDown",
  "Home",
  "End",
] as const;

// The point a key moves the cursor to: undefined for a key that is not one
// of the cursor's, or for a chart of nothing. With no point chosen yet, the
// first move goes to where `from` says: the point nearest now.
export function step(
  count: number,
  index: number | undefined,
  key: string,
  from = 0,
): number | undefined {
  if (count === 0 || !(CURSOR_KEYS as readonly string[]).includes(key)) return undefined;
  const last = count - 1;
  const clamp = (i: number) => Math.min(last, Math.max(0, i));
  if (key === "Home") return 0;
  if (key === "End") return last;
  if (index === undefined) return clamp(from);
  const stride = Math.max(1, Math.round(count / 10));
  switch (key) {
    case "ArrowLeft":
    case "ArrowDown":
      return clamp(index - 1);
    case "ArrowRight":
    case "ArrowUp":
      return clamp(index + 1);
    case "PageDown":
      return clamp(index - stride);
    default:
      return clamp(index + stride);
  }
}

// A point in words, for a screen reader: the time, then each series that is
// shown with its value.
export function valueText(time: string, values: { label: string; value: string }[]): string {
  if (values.length === 0) return `${time}. No series is shown.`;
  return `${time}. ${values.map((v) => `${v.label} ${v.value}`).join(", ")}.`;
}

// The mark that holds a time, by its id: the first of those that do. Unset
// for a time outside every mark, or for a mark with no id to point at.
export function spanAt(
  spans: { id?: string; from: number; to: number }[],
  time: number | undefined,
): string | undefined {
  if (time === undefined) return undefined;
  return spans.find((s) => time >= s.from && time <= s.to)?.id;
}
