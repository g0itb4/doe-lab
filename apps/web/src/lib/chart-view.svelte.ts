// The range of time a chart shows, while a reader is changing it. The page
// owns the range and keeps it in the address; but a wheel or a drag makes
// many ranges a second, and the address is told only of the last. Until
// then the range in the making lives here, by the key that charts share, so
// that the charts of a page move together.

export type Range = [number, number];

// The shortest range a chart zooms in to, in seconds.
export const MIN_SPAN = 300;

// A range zoomed about one instant, which stays where it is on the screen:
// a factor below 1 zooms in, above 1 out. It never leaves the bounds and is
// never shorter than the least span. Undefined is the whole of the bounds:
// no zoom at all.
export function zoomAt(
  range: Range,
  at: number,
  factor: number,
  bounds: Range,
  minSpan = MIN_SPAN,
): Range | undefined {
  const whole = bounds[1] - bounds[0];
  const span = Math.min(whole, Math.max(Math.min(minSpan, whole), (range[1] - range[0]) * factor));
  if (span >= whole) return undefined;
  // Where the instant sits in the range, from 0 at its start to 1 at its end.
  const share = range[1] > range[0] ? (at - range[0]) / (range[1] - range[0]) : 0.5;
  const from = at - Math.min(1, Math.max(0, share)) * span;
  return clamp([from, from + span], bounds);
}

// A range moved along by some seconds, and stopped at the bounds.
export function pan(range: Range, seconds: number, bounds: Range): Range {
  return clamp([range[0] + seconds, range[1] + seconds], bounds);
}

// A range of the same length, moved inside the bounds; whole seconds, because
// that is what the address holds.
export function clamp(range: Range, bounds: Range): Range {
  const span = Math.min(range[1] - range[0], bounds[1] - bounds[0]);
  const from = Math.min(bounds[1] - span, Math.max(bounds[0], range[0]));
  return [Math.round(from), Math.round(from + span)];
}

// The ranges in the making, by key: a range, or null for "the whole of it",
// which a reader zooming all the way out is on the way to.
class Views {
  #live = $state<Record<string, Range | null>>({});
  #next = 0;

  // A key for a chart that shares its range with no other.
  own(): string {
    return `chart-${this.#next++}`;
  }
  // The range in the making: undefined when none is, null for the whole.
  get(key: string): Range | null | undefined {
    return this.#live[key];
  }
  set(key: string, range: Range | undefined): void {
    this.#live[key] = range ?? null;
  }
  // The page has the range now, or the gesture came to nothing.
  clear(key: string): void {
    delete this.#live[key];
  }
}

export const views = new Views();
