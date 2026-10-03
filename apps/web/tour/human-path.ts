// How a hand moves a pointer, for the recorded tour of the UI: a script that
// moved it in straight lines at one speed, every move the same length of
// time, looked like what it was.
//
// The model is the usual account of an aimed movement:
//
//   - How long it takes grows with its difficulty, which is the distance over
//     the size of the target (Fitts 1954), and varies from one move to the
//     next.
//   - Its speed is one bell, steeper up than down: a lognormal stroke
//     (Plamondon's kinematic theory), with its peak a little before the
//     middle. Nothing in it is at a constant speed.
//   - Its path is a shallow curve, more often bowed away from the wrist, and
//     never a straight line.
//   - A long move at a small target misses a little and is put right by a
//     second, short stroke that begins before the first has ended (Meyer et
//     al. 1988).
//   - It lands somewhere on the target, seldom at its centre.
//   - The hand trembles a little while it moves, and settles as it stops.
//
// Everything here is a pure function of its arguments and a seeded stream of
// random numbers: the same seed makes the same path, so the model can be
// tested, and a take can be made again.

export type Point = { x: number; y: number };
// A place and a time: milliseconds from the start of the move.
export type Sample = Point & { t: number };
export type Box = { x: number; y: number; width: number; height: number };
// A stream of numbers from 0 up to, not including, 1.
export type Rng = () => number;

// A seeded stream (mulberry32): small, quick, and good enough for a hand.
export function rng(seed: number): Rng {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

const between = (r: Rng, lo: number, hi: number) => lo + (hi - lo) * r();
const clamp = (value: number, lo: number, hi: number) => Math.min(hi, Math.max(lo, value));
// A draw from the bell curve (Box and Muller).
function normal(r: Rng, mean = 0, sd = 1): number {
  const u = Math.max(r(), 1e-12);
  return mean + sd * Math.sqrt(-2 * Math.log(u)) * Math.cos(2 * Math.PI * r());
}
// The share of the bell curve below z (Abramowitz and Stegun 26.2.17).
function below(z: number): number {
  const t = 1 / (1 + 0.2316419 * Math.abs(z));
  const tail =
    Math.exp((-z * z) / 2) *
    0.3989422804 *
    t *
    (0.31938153 + t * (-0.356563782 + t * (1.781477937 + t * (-1.821255978 + t * 1.330274429))));
  return z >= 0 ? 1 - tail : tail;
}

// The model's numbers. Each is in the range the literature gives for a mouse.
export const HAND = {
  // Fitts' law: milliseconds with no difficulty, and for each bit of it.
  a: 80,
  b: 130,
  // How unhurried the hand is: a tour is watched, so it takes its time.
  tempo: 1.25,
  // How much one move's time varies from the next, as a lognormal's sigma.
  timeSpread: 0.15,
  // The shortest and the longest a single move takes.
  shortest: 120,
  longest: 1800,
  // The skew of the stroke: its peak speed comes 34 to 40 % of the way in.
  skew: [0.12, 0.22] as const,
  // How far the path bows, as a share of its length, and the most in pixels.
  bow: 0.05,
  bowSpread: 0.025,
  bowMost: 40,
  // How often it bows away from the wrist, for a right hand.
  awayFromWrist: 0.7,
  // From this difficulty a move may miss and be put right.
  correctFrom: 3.5,
  // The tremor: pixels, and how many times a second.
  tremor: 0.3,
  // The rate the samples are made at: a display's.
  hz: 60,
};

// How much of the target a hand aims inside: the middle 70 %.
const AIMED = 0.7;

// Where on a target the pointer lands: near the middle and seldom on it.
export function landing(target: Box, r: Rng): Point {
  const draw = (start: number, size: number) => {
    const half = (size * AIMED) / 2;
    let offset = normal(r, 0, size / 6);
    // Drawn again, not clipped: clipping would pile landings on the edge.
    while (Math.abs(offset) > half) offset = normal(r, 0, size / 6);
    return start + size / 2 + offset;
  };
  return { x: draw(target.x, target.width), y: draw(target.y, target.height) };
}

// How hard a move is, in bits: the distance against the size of the target
// along the way the pointer comes at it.
export function difficulty(from: Point, to: Point, target: Box): number {
  const [dx, dy] = [to.x - from.x, to.y - from.y];
  const distance = Math.hypot(dx, dy);
  if (distance === 0) return 0;
  const across = Math.abs(dx) / distance;
  const down = Math.abs(dy) / distance;
  // The chord of the target along the approach: its width for a level move,
  // its height for an upright one.
  const size = Math.min(
    across > 1e-6 ? target.width / across : Infinity,
    down > 1e-6 ? target.height / down : Infinity,
  );
  return Math.log2(distance / Math.max(size, 1) + 1);
}

// How long a move of some difficulty takes, in milliseconds.
export function movementTime(bits: number, r: Rng, tempo = HAND.tempo): number {
  const mean = tempo * (HAND.a + HAND.b * bits);
  return clamp(mean * Math.exp(normal(r, 0, HAND.timeSpread)), HAND.shortest, HAND.longest);
}

// How far along a stroke the hand is at each share of its time, from 0 to 1:
// the lognormal's own S, cut at its 1st and 99th hundredths and stretched to
// fill the stroke. `skew` is the lognormal's sigma.
export function progress(share: number, skew: number): number {
  if (share <= 0) return 0;
  if (share >= 1) return 1;
  const reach = 2.326 * skew;
  const [first, last] = [Math.exp(-reach), Math.exp(reach)];
  const tau = first + share * (last - first);
  // Measured from the cut itself, so a stroke begins at nothing exactly and
  // ends at the whole.
  return clamp((below(Math.log(tau) / skew) - CUT) / (1 - 2 * CUT), 0, 1);
}
// The share of the bell curve below the cut: a hundredth, as near as the
// approximation has it.
const CUT = below(-2.326);

// A cubic Bézier walked by its length: where the path is at each share of
// the way along it, whatever the spacing of its own parameter.
function curve(p0: Point, p1: Point, p2: Point, p3: Point): (share: number) => Point {
  const at = (u: number): Point => {
    const v = 1 - u;
    const [a, b, c, d] = [v * v * v, 3 * v * v * u, 3 * v * u * u, u * u * u];
    return {
      x: a * p0.x + b * p1.x + c * p2.x + d * p3.x,
      y: a * p0.y + b * p1.y + c * p2.y + d * p3.y,
    };
  };
  const STEPS = 96;
  const points = Array.from({ length: STEPS + 1 }, (_, i) => at(i / STEPS));
  const lengths = [0];
  for (let i = 1; i <= STEPS; i++) {
    lengths.push(
      lengths[i - 1]! +
        Math.hypot(points[i]!.x - points[i - 1]!.x, points[i]!.y - points[i - 1]!.y),
    );
  }
  const total = lengths[STEPS]!;
  return (share) => {
    const want = clamp(share, 0, 1) * total;
    let i = 1;
    while (i < STEPS && lengths[i]! < want) i++;
    const span = lengths[i]! - lengths[i - 1]!;
    const part = span > 0 ? (want - lengths[i - 1]!) / span : 0;
    const [from, to] = [points[i - 1]!, points[i]!];
    return { x: from.x + part * (to.x - from.x), y: from.y + part * (to.y - from.y) };
  };
}

export type MoveOptions = {
  // The window, for which side the wrist is on.
  viewport?: { width: number; height: number };
  // More than 1 is slower: a drag is steadier than a move.
  tempo?: number;
  // False for a move that is not put right when it misses: a drag.
  correct?: boolean;
  // How far the path wanders off its line, in pixels: a drag's unsteadiness.
  wander?: number;
  // How much of its usual bow the path has: a drag is held to its line.
  bow?: number;
};

// What a move was, beside its samples: for the tests, and for a take's log.
export type Move = {
  samples: Sample[];
  // Where it was aimed to land, and how hard it was.
  end: Point;
  bits: number;
  // How long the first stroke took, and whether a second put it right.
  time: number;
  corrected: boolean;
  // Which way the path bows: 1 to the left of its direction, -1 to the right.
  side: 1 | -1;
};

// A move of the pointer from where it is to somewhere on a target.
export function move(from: Point, target: Box, r: Rng, options: MoveOptions = {}): Move {
  const viewport = options.viewport ?? { width: 832, height: 520 };
  const end = landing(target, r);
  const distance = Math.hypot(end.x - from.x, end.y - from.y);
  const bits = difficulty(from, end, target);
  const time = movementTime(bits, r, (options.tempo ?? 1) * HAND.tempo);
  const skew = between(r, HAND.skew[0], HAND.skew[1]);
  // Along the move, and across it to the left.
  const along =
    distance > 0
      ? { x: (end.x - from.x) / distance, y: (end.y - from.y) / distance }
      : { x: 1, y: 0 };
  const left = { x: along.y, y: -along.x };

  // Which way it bows: away from the wrist more often than not. The wrist of
  // a right hand is below and to the right of the window.
  const wrist = { x: viewport.width + 150, y: viewport.height + 350 };
  const middle = { x: (from.x + end.x) / 2, y: (from.y + end.y) / 2 };
  const wristIsLeft = (wrist.x - middle.x) * left.x + (wrist.y - middle.y) * left.y > 0;
  const away = r() < HAND.awayFromWrist;
  const side: 1 | -1 = wristIsLeft === away ? -1 : 1;

  // A hard move misses a little, along its line and across it, and a second
  // stroke puts it right.
  const chance = clamp(0.3 + 0.2 * (bits - HAND.correctFrom), 0, 0.8);
  const corrected = options.correct !== false && bits >= HAND.correctFrom && r() < chance;
  let aim = end;
  if (corrected) {
    const past = clamp(Math.abs(normal(r, 0.035 * distance, 0.02 * distance)), 3, 30);
    const off = clamp(normal(r, 0, 0.012 * distance), -12, 12);
    // Past the target more often than short of it.
    const sign = r() < 0.7 ? 1 : -1;
    aim = {
      x: end.x + sign * past * along.x + off * left.x,
      y: end.y + sign * past * along.y + off * left.y,
    };
  }

  const bow =
    (options.bow ?? 1) *
    Math.min(HAND.bowMost, Math.abs(normal(r, HAND.bow, HAND.bowSpread)) * distance);
  const [c1, c2] = [side * bow, side * bow * between(r, 0.4, 0.9)];
  const reach = Math.hypot(aim.x - from.x, aim.y - from.y);
  const toAim = reach > 0 ? { x: (aim.x - from.x) / reach, y: (aim.y - from.y) / reach } : along;
  const walk = curve(
    from,
    {
      x: from.x + 0.3 * reach * toAim.x + c1 * left.x,
      y: from.y + 0.3 * reach * toAim.y + c1 * left.y,
    },
    {
      x: from.x + 0.7 * reach * toAim.x + c2 * left.x,
      y: from.y + 0.7 * reach * toAim.y + c2 * left.y,
    },
    aim,
  );

  // The stroke that puts a miss right begins before the first has ended, and
  // is short: its own time by its own difficulty.
  const onset = corrected ? time * between(r, 0.85, 0.95) : time;
  const fix = corrected
    ? {
        time: movementTime(difficulty(aim, end, target), r, 0.8),
        skew: between(r, HAND.skew[0], HAND.skew[1]),
      }
    : undefined;
  // Both strokes run to their ends: a quick second one does not cut the
  // first short.
  const total = fix ? Math.max(time, onset + fix.time) : time;

  // The tremor, and a drag's wander: slow enough to be the arm's, not the
  // fingers'.
  const [fast, slow] = [between(r, 8, 12), between(r, 3, 5)];
  const phases = [r(), r(), r(), r(), r()].map((p) => 2 * Math.PI * p);
  const drift = between(r, 0.6, 1.2);
  const wander = options.wander ?? 0;

  const step = 1000 / HAND.hz;
  const count = Math.max(2, Math.ceil(total / step) + 1);
  const samples: Sample[] = [];
  for (let i = 0; i < count; i++) {
    const t = Math.min(total, i * step);
    const first = walk(progress(t / time, skew));
    const done = fix && t > onset ? progress((t - onset) / fix.time, fix.skew) : 0;
    // The tremor is the hand's while it moves, and goes as it settles; it is
    // nothing at the two ends, so the move begins and lands where it should.
    const settle = Math.sin(Math.PI * clamp(t / total, 0, 1));
    const s = t / 1000;
    const shake = HAND.tremor * settle;
    const stray = wander * settle * Math.sin(2 * Math.PI * drift * s + phases[4]!);
    samples.push({
      x:
        first.x +
        done * (end.x - aim.x) +
        shake *
          (Math.sin(2 * Math.PI * fast * s + phases[0]!) +
            0.6 * Math.sin(2 * Math.PI * slow * s + phases[1]!)) +
        stray * left.x,
      y:
        first.y +
        done * (end.y - aim.y) +
        shake *
          (Math.sin(2 * Math.PI * fast * s + phases[2]!) +
            0.6 * Math.sin(2 * Math.PI * slow * s + phases[3]!)) +
        stray * left.y,
      t,
    });
  }
  // Exactly where it began, and exactly where it was aimed.
  samples[0] = { ...from, t: 0 };
  samples[samples.length - 1] = { ...end, t: total };
  return { samples, end, bits, time, corrected, side };
}

// A drag from one point to another: slower and steadier than a move, never
// put right, and never quite straight.
export function drag(from: Point, to: Point, r: Rng, options: MoveOptions = {}): Move {
  // The far end is a place, not a target to hit: a few pixels of it will do.
  const target = { x: to.x - 3, y: to.y - 3, width: 6, height: 6 };
  return move(from, target, r, {
    ...options,
    tempo: between(r, 1.5, 2.2),
    correct: false,
    wander: between(r, 1.5, 4),
    bow: 0.25,
  });
}

// A press of the button: the pause before it, how long it is held, and the
// pause before the hand moves on. Milliseconds.
export function press(r: Rng): { settle: number; hold: number; linger: number } {
  return {
    settle: clamp(180 * Math.exp(normal(r, 0, 0.45)), 90, 450),
    hold: clamp(normal(r, 95, 25), 55, 180),
    linger: between(r, 120, 300),
  };
}

// The pointer while the hand rests on it: it is never quite still. A slow
// drift about the place, pulled back towards it, for some milliseconds.
export function rest(at: Point, ms: number, r: Rng): Sample[] {
  const step = 1000 / HAND.hz;
  const samples: Sample[] = [{ ...at, t: 0 }];
  let [dx, dy] = [0, 0];
  for (let t = step; t < ms; t += step) {
    dx += -0.08 * dx + normal(r, 0, 0.28);
    dy += -0.08 * dy + normal(r, 0, 0.28);
    samples.push({ x: at.x + dx, y: at.y + dy, t });
  }
  return samples;
}

// How long a reader looks at what has just appeared: longer for more words,
// never the same twice. Milliseconds.
export function reading(words: number, r: Rng): number {
  return Math.min(2400, 600 + 60 * words) * Math.exp(normal(r, 0, 0.2));
}

// A turn of the wheel: a burst of clicks that slow and shrink, as a finger's
// flick does. Each has its distance and the wait before the next.
export function wheel(distance: number, r: Rng): { delta: number; wait: number }[] {
  const count = Math.round(between(r, 3, 7));
  const fade = between(r, 0.7, 0.9);
  const sizes = Array.from({ length: count }, (_, i) => fade ** i);
  const sum = sizes.reduce((a, b) => a + b, 0);
  let wait = between(r, 45, 90);
  return sizes.map((size) => {
    const click = { delta: (distance * size) / sum, wait };
    wait *= 1.15;
    return click;
  });
}
