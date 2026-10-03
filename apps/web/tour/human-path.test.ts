import { describe, expect, it } from "vitest";
import {
  type Box,
  difficulty,
  drag,
  HAND,
  landing,
  type Move,
  move,
  movementTime,
  type Point,
  press,
  progress,
  reading,
  rest,
  rng,
  type Sample,
  wheel,
} from "./human-path.ts";

// What makes a pointer look driven by a script, and what this checks is not
// there: the same time for every move whatever its length, one speed from
// end to end, a straight line, a landing dead on the centre, the same path
// twice. Each is measured over hundreds of seeded moves.

const viewport = { width: 832, height: 520 };
const N = 500;

// A move as a take makes them: from somewhere in the window to a control
// somewhere else in it.
function trial(seed: number): { from: Point; target: Box; made: Move } {
  const r = rng(seed);
  const from = { x: 20 + r() * 790, y: 20 + r() * 480 };
  const width = 16 + r() * 150;
  const height = 14 + r() * 26;
  const target = {
    x: r() * (viewport.width - width),
    y: r() * (viewport.height - height),
    width,
    height,
  };
  return { from, target, made: move(from, target, rng(seed + 10_000), { viewport }) };
}
const trials = Array.from({ length: N }, (_, i) => trial(i + 1));

const dist = (a: Point, b: Point) => Math.hypot(b.x - a.x, b.y - a.y);
const length = (samples: Sample[]) =>
  samples.slice(1).reduce((sum, s, i) => sum + dist(samples[i]!, s), 0);
const speeds = (samples: Sample[]) =>
  samples.slice(1).map((s, i) => dist(samples[i]!, s) / (s.t - samples[i]!.t));
const mean = (values: number[]) => values.reduce((a, b) => a + b, 0) / values.length;
const median = (values: number[]) =>
  [...values].sort((a, b) => a - b)[Math.floor(values.length / 2)]!;
const sd = (values: number[]) => Math.sqrt(mean(values.map((v) => (v - mean(values)) ** 2)));
function correlation(xs: number[], ys: number[]): number {
  const [mx, my] = [mean(xs), mean(ys)];
  const cov = mean(xs.map((x, i) => (x - mx) * (ys[i]! - my)));
  return cov / (sd(xs) * sd(ys));
}
// How far a path is from the straight line between its ends, at its
// furthest, with the side it is on: positive to the left of its direction.
function bow(samples: Sample[]): number {
  const [a, b] = [samples[0]!, samples.at(-1)!];
  const d = dist(a, b);
  let furthest = 0;
  for (const s of samples) {
    const off = ((b.y - a.y) * (s.x - a.x) - (b.x - a.x) * (s.y - a.y)) / d;
    if (Math.abs(off) > Math.abs(furthest)) furthest = off;
  }
  return furthest;
}

describe("the stream of random numbers", () => {
  it("is the same for the same seed, different for another, and inside 0 to 1", () => {
    const [a, b, c] = [rng(7), rng(7), rng(8)];
    const first = Array.from({ length: 1000 }, a);
    expect(Array.from({ length: 1000 }, b)).toEqual(first);
    expect(Array.from({ length: 1000 }, c)).not.toEqual(first);
    expect(Math.min(...first)).toBeGreaterThanOrEqual(0);
    expect(Math.max(...first)).toBeLessThan(1);
    // Spread over the range, not bunched.
    expect(mean(first)).toBeGreaterThan(0.45);
    expect(mean(first)).toBeLessThan(0.55);
  });
});

describe("a move of the pointer", () => {
  it("begins where the pointer is and ends on the target, a sample a frame, with time moving on", () => {
    for (const { from, target, made } of trials) {
      const { samples, end } = made;
      expect(samples[0]).toEqual({ ...from, t: 0 });
      expect(samples.at(-1)).toMatchObject(end);
      expect(end.x).toBeGreaterThanOrEqual(target.x);
      expect(end.x).toBeLessThanOrEqual(target.x + target.width);
      expect(end.y).toBeGreaterThanOrEqual(target.y);
      expect(end.y).toBeLessThanOrEqual(target.y + target.height);
      for (let i = 1; i < samples.length; i++) {
        const gap = samples[i]!.t - samples[i - 1]!.t;
        expect(gap).toBeGreaterThan(0);
        expect(gap).toBeLessThanOrEqual(1000 / HAND.hz + 1e-9);
      }
    }
  });

  it("takes longer the harder it is: Fitts' law, and not one time for every move", () => {
    const single = trials.filter((t) => !t.made.corrected);
    const bits = single.map((t) => t.made.bits);
    const times = single.map((t) => t.made.time);
    expect(correlation(bits, times)).toBeGreaterThan(0.8);
    // The slope of time on difficulty, in milliseconds a bit.
    const slope = (correlation(bits, times) * sd(times)) / sd(bits);
    expect(slope).toBeGreaterThan(HAND.tempo * 100);
    expect(slope).toBeLessThan(HAND.tempo * 180);
    // And two moves of the same difficulty do not take the same time.
    expect(new Set(times.map((t) => Math.round(t))).size).toBeGreaterThan(single.length / 3);
    expect(Math.min(...times)).toBeGreaterThanOrEqual(HAND.shortest);
    expect(Math.max(...times)).toBeLessThanOrEqual(HAND.longest);
  });

  it("has one bell of speed, steeper up than down, with its peak before the middle", () => {
    const peaks: number[] = [];
    for (const { made } of trials.filter((t) => !t.made.corrected && t.made.bits > 1.5)) {
      const v = speeds(made.samples);
      const top = v.indexOf(Math.max(...v));
      peaks.push(made.samples[top]!.t / made.time);
      // Nothing like a constant speed: it starts and ends near rest.
      expect(v[0]!).toBeLessThan(0.25 * Math.max(...v));
      expect(v.at(-1)!).toBeLessThan(0.25 * Math.max(...v));
      expect(sd(v) / mean(v)).toBeGreaterThan(0.4);
    }
    expect(peaks.length).toBeGreaterThan(200);
    expect(median(peaks)).toBeGreaterThan(0.3);
    expect(median(peaks)).toBeLessThan(0.45);
    expect(peaks.filter((p) => p > 0.25 && p < 0.5).length / peaks.length).toBeGreaterThan(0.95);
  });

  it("is a shallow curve: never a straight line, and never far from one", () => {
    const long = trials.filter((t) => !t.made.corrected && dist(t.from, t.made.end) > 150);
    const ratios = long.map((t) => length(t.made.samples) / dist(t.from, t.made.end));
    const bows = long.map((t) => Math.abs(bow(t.made.samples)) / dist(t.from, t.made.end));
    expect(long.length).toBeGreaterThan(150);
    // Longer than the straight line by a little.
    expect(Math.min(...ratios)).toBeGreaterThan(1);
    expect(Math.max(...ratios)).toBeLessThan(1.25);
    expect(median(ratios)).toBeGreaterThan(1.0015);
    expect(median(ratios)).toBeLessThan(1.08);
    // Off the line by a few hundredths of its length at the most.
    expect(median(bows)).toBeGreaterThan(0.015);
    expect(median(bows)).toBeLessThan(0.08);
    expect(Math.max(...bows)).toBeLessThan(0.2);
  });

  it("never makes the same step twice running", () => {
    for (const { made } of trials) {
      const steps = made.samples
        .slice(1)
        .map((s, i) => `${s.x - made.samples[i]!.x},${s.y - made.samples[i]!.y}`);
      for (let i = 1; i < steps.length; i++) expect(steps[i]).not.toBe(steps[i - 1]);
    }
  });

  it("is the same again from the same seed, and another move from another", () => {
    const [from, target] = [
      { x: 100, y: 400 },
      { x: 600, y: 80, width: 90, height: 32 },
    ];
    const once = move(from, target, rng(42), { viewport });
    expect(move(from, target, rng(42), { viewport })).toEqual(once);
    const other = move(from, target, rng(43), { viewport });
    expect(other.samples).not.toEqual(once.samples);
    expect(other.end).not.toEqual(once.end);
  });

  it("bows away from the wrist more often than towards it, and to the side it says", () => {
    const [from, target] = [
      { x: 120, y: 420 },
      { x: 560, y: 90, width: 80, height: 30 },
    ];
    const made = Array.from({ length: N }, (_, i) => move(from, target, rng(i), { viewport }));
    const sides = made.map((m) => m.side);
    const most = Math.max(
      sides.filter((s) => s === 1).length,
      sides.filter((s) => s === -1).length,
    );
    expect(most / N).toBeGreaterThan(0.62);
    expect(most / N).toBeLessThan(0.78);
    // The path is on the side the move says: to the left of its direction
    // for 1.
    const agree = made.filter((m) => !m.corrected && Math.sign(bow(m.samples)) === m.side);
    expect(agree.length / made.filter((m) => !m.corrected).length).toBeGreaterThan(0.9);
    // From the other side of the window the same hand bows the other way.
    const back = Array.from({ length: N }, (_, i) =>
      move({ x: 700, y: 60 }, { x: 80, y: 400, width: 80, height: 30 }, rng(i), { viewport }),
    );
    const backMost = back.filter(
      (m) => m.side === sides.find((s) => sides.filter((x) => x === s).length === most),
    ).length;
    expect(backMost / N).toBeLessThan(0.4);
  });

  it("misses a small, far target sometimes and puts it right; an easy one, never", () => {
    const from = { x: 40, y: 480 };
    const rate = (target: Box) => {
      const made = Array.from({ length: 400 }, (_, i) => move(from, target, rng(i), { viewport }));
      return { made, rate: made.filter((m) => m.corrected).length / made.length };
    };
    const easy = rate({ x: 100, y: 430, width: 200, height: 60 });
    const medium = rate({ x: 500, y: 200, width: 30, height: 24 });
    const hard = rate({ x: 760, y: 30, width: 14, height: 14 });
    expect(easy.rate).toBe(0);
    expect(medium.rate).toBeGreaterThan(0.1);
    expect(hard.rate).toBeGreaterThan(medium.rate + 0.15);
    expect(hard.rate).toBeLessThanOrEqual(0.85);
    for (const m of hard.made.filter((m) => m.corrected)) {
      // The second stroke begins before the first has ended, and neither is
      // cut short: the whole takes as long as the first alone at the least.
      const total = m.samples.at(-1)!.t;
      expect(total).toBeGreaterThanOrEqual(m.time);
      expect(total).toBeLessThan(m.time + 700);
      // No jump at the end: the last step is no longer than the ones before.
      const steps = m.samples.slice(1).map((s, i) => dist(m.samples[i]!, s));
      expect(steps.at(-1)!).toBeLessThan(Math.max(...steps.slice(0, -1)) + 0.5);
      expect(steps.at(-1)!).toBeLessThan(6);
      // It still lands where it was aimed.
      expect(m.samples.at(-1)).toMatchObject(m.end);
    }
    // More of them go past the target than stop short of it.
    const reach = (m: Move) => Math.max(...m.samples.map((s) => dist(from, s))) - dist(from, m.end);
    const past = hard.made.filter((m) => m.corrected && reach(m) > 1.5).length;
    expect(past / hard.made.filter((m) => m.corrected).length).toBeGreaterThan(0.5);
  });

  it("stays where it is for a target of no size under the pointer", () => {
    const at = { x: 300, y: 200 };
    const still = move(at, { ...at, width: 0, height: 0 }, rng(3), { viewport });
    expect(still.bits).toBe(0);
    expect(still.end).toEqual(at);
    for (const s of still.samples) expect(dist(at, s)).toBeLessThan(2);
    // With no window given, the tour's own is taken.
    expect(
      move(at, { x: 500, y: 100, width: 40, height: 20 }, rng(3)).samples.length,
    ).toBeGreaterThan(5);
  });
});

describe("where a move lands", () => {
  const target = { x: 400, y: 300, width: 80, height: 24 };
  const r = rng(5);
  const points = Array.from({ length: 2000 }, () => landing(target, r));

  it("is inside the middle of the target, and spread across it", () => {
    for (const p of points) {
      expect(Math.abs(p.x - 440)).toBeLessThanOrEqual(28);
      expect(Math.abs(p.y - 312)).toBeLessThanOrEqual(8.4);
    }
    expect(sd(points.map((p) => p.x))).toBeGreaterThan(0.1 * target.width);
    expect(sd(points.map((p) => p.y))).toBeGreaterThan(0.1 * target.height);
  });

  it("is seldom the centre", () => {
    const dead = points.filter((p) => Math.abs(p.x - 440) < 1 && Math.abs(p.y - 312) < 1);
    expect(dead.length / points.length).toBeLessThan(0.04);
  });
});

describe("how hard a move is, and how long it takes", () => {
  const target = { x: 400, y: 300, width: 80, height: 20 };

  it("is the distance against the size of the target the way the pointer comes", () => {
    // Level: the width counts. Upright: the height, which is less, so it is harder.
    const level = difficulty({ x: 0, y: 310 }, { x: 440, y: 310 }, target);
    const upright = difficulty({ x: 440, y: 750 }, { x: 440, y: 310 }, target);
    expect(level).toBeCloseTo(Math.log2(440 / 80 + 1));
    expect(upright).toBeCloseTo(Math.log2(440 / 20 + 1));
    // At a slant, the shorter of the two chords.
    const slant = difficulty({ x: 0, y: 750 }, { x: 440, y: 310 }, target);
    expect(slant).toBeCloseTo(Math.log2(Math.hypot(440, 440) / (20 * Math.SQRT2) + 1));
    expect(difficulty({ x: 440, y: 310 }, { x: 440, y: 310 }, target)).toBe(0);
    // A target of no size is taken as a pixel.
    expect(
      difficulty({ x: 0, y: 0 }, { x: 64, y: 0 }, { x: 64, y: 0, width: 0, height: 0 }),
    ).toBeCloseTo(Math.log2(65));
  });

  it("takes about a tenth of a second and an eighth more for each bit, a little more or less each time", () => {
    const r = rng(9);
    const times = Array.from({ length: 2000 }, () => movementTime(4, r));
    expect(median(times)).toBeGreaterThan(HAND.tempo * (HAND.a + 4 * HAND.b) * 0.95);
    expect(median(times)).toBeLessThan(HAND.tempo * (HAND.a + 4 * HAND.b) * 1.05);
    expect(sd(times) / mean(times)).toBeGreaterThan(0.1);
    expect(sd(times) / mean(times)).toBeLessThan(0.2);
    // Never quicker than a hand can be, nor slower than a tour can wait.
    expect(movementTime(0, r, 0.1)).toBe(HAND.shortest);
    expect(movementTime(40, r)).toBe(HAND.longest);
  });

  it("goes through a stroke slowly, then quickly, then slowly, further on by the middle than half", () => {
    expect(progress(0, 0.15)).toBe(0);
    expect(progress(1, 0.15)).toBe(1);
    expect(progress(-1, 0.15)).toBe(0);
    expect(progress(2, 0.15)).toBe(1);
    const steps = Array.from({ length: 101 }, (_, i) => progress(i / 100, 0.15));
    for (let i = 1; i < steps.length; i++) expect(steps[i]!).toBeGreaterThanOrEqual(steps[i - 1]!);
    // The quickest hundredth is before the middle, and more than half the
    // way is done by half the time.
    const gains = steps.slice(1).map((s, i) => s - steps[i]!);
    const quickest = gains.indexOf(Math.max(...gains));
    expect(quickest).toBeGreaterThan(30);
    expect(quickest).toBeLessThan(46);
    expect(steps[50]!).toBeGreaterThan(0.55);
    // More skew, an earlier peak.
    const skewed = Array.from({ length: 101 }, (_, i) => progress(i / 100, 0.22));
    const skewedGains = skewed.slice(1).map((s, i) => s - skewed[i]!);
    expect(skewedGains.indexOf(Math.max(...skewedGains))).toBeLessThan(quickest);
  });
});

describe("a drag", () => {
  const [from, to] = [
    { x: 200, y: 260 },
    { x: 560, y: 262 },
  ];
  const drags = Array.from({ length: 200 }, (_, i) => drag(from, to, rng(i), { viewport }));

  it("is slower and steadier than a move, and is never put right", () => {
    const moves = Array.from({ length: 200 }, (_, i) =>
      move(from, { x: to.x - 3, y: to.y - 3, width: 6, height: 6 }, rng(i), {
        viewport,
        correct: false,
      }),
    );
    expect(mean(drags.map((d) => d.time))).toBeGreaterThan(1.3 * mean(moves.map((m) => m.time)));
    expect(drags.every((d) => !d.corrected)).toBe(true);
    for (const d of drags) expect(dist(d.samples.at(-1)!, to)).toBeLessThan(4);
  });

  it("is never quite straight, and never far from straight", () => {
    const off = drags.map((d) => Math.abs(bow(d.samples)));
    expect(Math.min(...off)).toBeGreaterThan(0.3);
    expect(median(off)).toBeGreaterThan(1);
    expect(median(off)).toBeLessThan(8);
    expect(Math.max(...off)).toBeLessThan(16);
    // A level drag does not stay on one row of pixels.
    for (const d of drags)
      expect(new Set(d.samples.map((s) => Math.round(s.y))).size).toBeGreaterThan(1);
  });
});

describe("a press, a rest, a read and a turn of the wheel", () => {
  it("presses after a pause, for about a tenth of a second, and waits before moving on", () => {
    const r = rng(11);
    const presses = Array.from({ length: 1000 }, () => press(r));
    for (const p of presses) {
      expect(p.settle).toBeGreaterThanOrEqual(90);
      expect(p.settle).toBeLessThanOrEqual(450);
      expect(p.hold).toBeGreaterThanOrEqual(55);
      expect(p.hold).toBeLessThanOrEqual(180);
      expect(p.linger).toBeGreaterThanOrEqual(120);
      expect(p.linger).toBeLessThan(300);
    }
    // Never at once on arrival, and no two the same.
    expect(median(presses.map((p) => p.settle))).toBeGreaterThan(150);
    expect(median(presses.map((p) => p.settle))).toBeLessThan(215);
    expect(new Set(presses.map((p) => Math.round(p.hold))).size).toBeGreaterThan(40);
  });

  it("is never quite still at rest, and never far from where it rests", () => {
    const at = { x: 300, y: 200 };
    const samples = rest(at, 2000, rng(12));
    expect(samples[0]).toEqual({ ...at, t: 0 });
    expect(samples.at(-1)!.t).toBeLessThan(2000);
    expect(samples.length).toBeGreaterThan(110);
    for (let i = 1; i < samples.length; i++) {
      expect(dist(at, samples[i]!)).toBeLessThan(6);
      expect(dist(samples[i - 1]!, samples[i]!)).toBeGreaterThan(0);
    }
    expect(Math.max(...samples.map((s) => dist(at, s)))).toBeGreaterThan(0.5);
    // No time at all is the place alone.
    expect(rest(at, 0, rng(12))).toEqual([{ ...at, t: 0 }]);
  });

  it("reads for longer the more there is to read, up to a limit, and never for the same time twice", () => {
    const r = rng(13);
    const few = Array.from({ length: 500 }, () => reading(3, r));
    const many = Array.from({ length: 500 }, () => reading(20, r));
    const pages = Array.from({ length: 500 }, () => reading(500, r));
    expect(median(many)).toBeGreaterThan(1.8 * median(few));
    expect(median(pages)).toBeGreaterThan(2200);
    expect(median(pages)).toBeLessThan(2600);
    expect(new Set(few.map((ms) => Math.round(ms))).size).toBeGreaterThan(100);
  });

  it("turns the wheel in a burst of clicks that shrink and slow", () => {
    for (let seed = 0; seed < 200; seed++) {
      const clicks = wheel(600, rng(seed));
      expect(clicks.length).toBeGreaterThanOrEqual(3);
      expect(clicks.length).toBeLessThanOrEqual(7);
      expect(clicks.reduce((sum, c) => sum + c.delta, 0)).toBeCloseTo(600);
      for (let i = 1; i < clicks.length; i++) {
        expect(clicks[i]!.delta).toBeLessThan(clicks[i - 1]!.delta);
        expect(clicks[i]!.wait).toBeGreaterThan(clicks[i - 1]!.wait);
      }
      expect(clicks[0]!.wait).toBeGreaterThanOrEqual(45);
    }
    // Up as well as down.
    expect(wheel(-300, rng(1)).every((c) => c.delta < 0)).toBe(true);
  });
});
