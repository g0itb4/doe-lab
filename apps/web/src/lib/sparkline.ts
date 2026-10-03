// A series as a small line with no axes: the shape of a figure through time,
// beside the figure itself. Plain functions, so the component only has to
// put the answer on the screen.

// The line as the `d` of an SVG path, in a box `width` by `height`: the
// lowest value at the bottom, the highest at the top, and a break wherever a
// value is missing. A series that never changes is a level line across the
// middle; one with no value at all is no line.
export function sparkPath(
  values: readonly (number | null | undefined)[],
  width: number,
  height: number,
  pad = 1.5,
): string {
  const known = values.filter((v): v is number => v !== null && v !== undefined);
  if (known.length === 0) return "";
  const [lo, hi] = [Math.min(...known), Math.max(...known)];
  const x = (i: number) => (values.length === 1 ? width / 2 : (i / (values.length - 1)) * width);
  const y = (v: number) =>
    hi === lo ? height / 2 : height - pad - ((v - lo) / (hi - lo)) * (height - 2 * pad);
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
