// A series as a small line with no axes: the shape of a figure through time,
// beside the figure itself. Plain functions, so the component only has to
// put the answer on the screen.

// The line as the `d` of an SVG path, in a box `width` by `height`: the
// lowest value at the bottom, the highest at the top, and a break wherever a
// value is missing. A series that never changes is a level line across the
// middle; one with no value at all is no line.
//
// With a `domain`, the bottom and the top are the domain's and not the
// series' own: a line drawn against a limit shares its scale with the limit.
export function sparkPath(
  values: readonly (number | null | undefined)[],
  width: number,
  height: number,
  pad = 1.5,
  domain?: readonly [number, number],
): string {
  const known = values.filter((v): v is number => v !== null && v !== undefined);
  if (known.length === 0) return "";
  const scale = domain ?? ([Math.min(...known), Math.max(...known)] as const);
  const x = (i: number) => (values.length === 1 ? width / 2 : (i / (values.length - 1)) * width);
  const y = (v: number) => sparkY(v, scale, height, pad);
  const round = (n: number) => Math.round(n * 10) / 10;
  let path = "";
  let pen = false;
  values.forEach((v, i) => {
    if (v === null || v === undefined) {
      pen = false;
      return;
    }
    path += `${pen ? "L" : "M"}${round(x(i))} ${round(y(v))}`;
    pen = true;
  });
  // One value on its own would draw nothing: a short level stroke stands for it.
  if (known.length === 1) path += `h0.1`;
  return path;
}

// How far down the box a value is drawn, for a scale from its lowest to its
// highest: the middle, when the two are the same.
export function sparkY(
  value: number,
  [lo, hi]: readonly [number, number],
  height: number,
  pad = 1.5,
): number {
  return hi === lo ? height / 2 : height - pad - ((value - lo) / (hi - lo)) * (height - 2 * pad);
}

// The scale of a line drawn against a limit: from nothing, or from the
// deepest import, up to the limit, or to the highest value when that is over
// it. So a line that hugs the top is at its limit, and one above the mark of
// the limit is over it.
export function limitDomain(
  values: readonly (number | null | undefined)[],
  limit: number,
): [number, number] {
  const known = values.filter((v): v is number => v !== null && v !== undefined);
  return [Math.min(0, ...known), Math.max(limit, ...known)];
}

// The same series in a few words, for a reader who cannot see the line: where
// it began, where it is, and its highest. `format` gives a value its unit.
export function trendWords(
  values: readonly (number | null | undefined)[],
  format: (value: number) => string,
): string {
  const known = values.filter((v): v is number => v !== null && v !== undefined);
  if (known.length === 0) return "No readings in this range.";
  const [first, last, peak] = [known[0]!, known.at(-1)!, Math.max(...known)];
  if (known.length === 1 || format(first) === format(last)) {
    return peak > last && format(peak) !== format(last)
      ? `Now ${format(last)}, as at the start of the range; its highest was ${format(peak)}.`
      : `Steady at ${format(last)} over the range.`;
  }
  const way = last > first ? "Up" : "Down";
  const top =
    format(peak) === format(Math.max(first, last)) ? "" : `; its highest was ${format(peak)}`;
  return `${way} from ${format(first)} to ${format(last)} over the range${top}.`;
}

// A series up to its last value: a window that runs on past now has nothing
// yet for the part ahead, and a line should fill its room with what there is.
export function upToLast<T>(values: readonly (T | null | undefined)[]): (T | null | undefined)[] {
  let end = values.length;
  while (end > 0 && (values[end - 1] === null || values[end - 1] === undefined)) end--;
  return values.slice(0, end);
}
