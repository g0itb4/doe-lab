import { mkdirSync, rmSync, writeFileSync } from "node:fs";
import { writeFile } from "node:fs/promises";
import { join } from "node:path";
import { expect, test, type Locator, type Page } from "@playwright/test";
import { TOUR } from "../playwright.tour.config.ts";
import { cursorOverlay } from "./cursor-overlay.ts";
import {
  type Box,
  drag,
  HAND,
  move,
  type MoveOptions,
  type Point,
  press,
  reading,
  rest,
  rng,
  type Sample,
} from "./human-path.ts";

// The tour of the UI that the README shows, recorded from a real stack
// (scripts/tour.sh brings one up; `just tour` runs it all). The pointer is
// moved as a hand moves one (human-path.ts) through real mouse events, so
// every hover card, readout and drag on the recording is the app's own.
//
// The frames come from the browser's screencast: one for each change of the
// page, with its time, written to test-results/tour with the lists that
// ffmpeg reads. It is one take and two clips, each short enough to watch and
// light enough to keep: the fleet and one of its feeders through time, and
// then that feeder's network. The take is checked before it is kept: the moves the page received
// must have the shape the model gave them.

const SEED = Number(process.env.TOUR_SEED ?? 20261003);
// A reader of the README watches; they do not read every word. The pauses
// are a share of what reading the page would take.
const PACE = 0.45;
const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

// The hand on the mouse: where the pointer is, and how it gets anywhere.
class Hand {
  at: Point = { x: -14, y: TOUR.viewport.height * 0.58 };
  readonly r = rng(SEED);
  constructor(readonly page: Page) {}

  // Sends the samples of a move at the times they are for. A sample that is
  // late is dropped, never sent in a bunch: the page must see a steady hand.
  async play(samples: Sample[]) {
    const start = performance.now();
    for (const [i, s] of samples.entries()) {
      const wait = start + s.t - performance.now();
      if (wait > 1) await sleep(wait);
      else if (wait < -10 && i < samples.length - 1) continue;
      // Half pixels: finer than the page can show.
      await this.page.mouse.move(Math.round(s.x * 2) / 2, Math.round(s.y * 2) / 2);
    }
    const last = samples.at(-1)!;
    this.at = { x: last.x, y: last.y };
  }

  async box(target: Locator | Box): Promise<Box> {
    if (!("boundingBox" in target)) return target;
    await expect(target).toBeVisible();
    // Where it is now: a live page moves under a hand.
    return (await target.boundingBox())!;
  }

  // Moves the pointer onto a target.
  async to(target: Locator | Box, options: MoveOptions = {}) {
    const made = move(this.at, await this.box(target), this.r, {
      viewport: TOUR.viewport,
      ...options,
    });
    await this.play(made.samples);
  }

  // The hand at rest for a while: still, but for the odd small shift.
  async pause(ms: number) {
    let left = ms;
    while (left > 900 && this.r() < 0.35) {
      const before = 300 + this.r() * (left - 600);
      await sleep(before);
      const size = 3 + this.r() * 9;
      const angle = this.r() * 2 * Math.PI;
      const near = {
        x: this.at.x + size * Math.cos(angle) - 1,
        y: this.at.y + size * Math.sin(angle) - 1,
        width: 2,
        height: 2,
      };
      await this.to(near, { correct: false });
      left -= before;
    }
    await sleep(Math.max(0, left));
  }
  // A look at what has just appeared, for about as long as it has words.
  read(words: number) {
    return this.pause(PACE * reading(words, this.r));
  }

  async click(target: Locator | Box) {
    await this.to(target);
    const { settle, hold, linger } = press(this.r);
    // The hand tightens on the button before it presses.
    await this.play(rest(this.at, settle, this.r));
    await this.page.mouse.down();
    await sleep(hold);
    await this.page.mouse.up();
    await sleep(linger);
  }

  // Presses at one point and lets go at another: a brush, a pan, a slider.
  async drag(from: Point, to: Point) {
    await this.to({ x: from.x - 2, y: from.y - 2, width: 4, height: 4 });
    const { settle } = press(this.r);
    await this.play(rest(this.at, settle, this.r));
    await this.page.mouse.down();
    await sleep(80 + this.r() * 80);
    await this.play(drag(this.at, to, this.r, { viewport: TOUR.viewport }).samples);
    await sleep(100 + this.r() * 150);
    await this.page.mouse.up();
    await sleep(150 + this.r() * 150);
  }
}

// A point of a box, by shares of its width and height.
const within = (box: Box, across: number, down: number): Point => ({
  x: box.x + box.width * across,
  y: box.y + box.height * down,
});
const around = (point: Point, size = 6): Box => ({
  x: point.x - size / 2,
  y: point.y - size / 2,
  width: size,
  height: size,
});

test("the tour of the UI, with a hand on the mouse", async ({ page, context }) => {
  const out = TOUR.out;
  rmSync(out, { recursive: true, force: true });
  mkdirSync(out, { recursive: true });
  await context.addInitScript(cursorOverlay);
  // A reader who has been here before: no introduction over the page.
  await context.addInitScript(() => localStorage.setItem("doelab.intro", "dismissed"));

  // The stack is up; wait for its midday, when the sun is on the feeders and
  // there is something to limit. Feeder time runs at sixty times the clock.
  await page.goto("/");
  const clock = page.locator("header p > span").first();
  await expect(clock).toHaveText(/^\d\d:\d\d /, { timeout: 60_000 });
  const minutes = async () => {
    const [h, m] = (await clock.innerText()).split(/[: ]/).map(Number);
    return h! * 60 + m!;
  };
  const start = Number(process.env.TOUR_FROM_MINUTES ?? 11 * 60 + 40);
  expect(await minutes(), "feeder time is past the hours the tour is for").toBeLessThan(15 * 60);
  await expect
    .poll(minutes, { timeout: 15 * 60_000, intervals: [1000] })
    .toBeGreaterThanOrEqual(start);

  // A fresh page for the take, loaded and at rest before the first frame. The
  // map is asked to jump to what it frames, not to glide there: a glide over
  // street tiles is a dozen whole frames of a GIF, and says nothing.
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.goto("/");
  await expect(page.getByRole("button", { name: /Zone:/ }).first()).toBeVisible();
  await expect(page.getByRole("heading", { name: "Needs attention" })).toBeVisible();
  await expect(page.locator(".leaflet-tile-loaded").first()).toBeVisible();
  await page.waitForLoadState("networkidle");

  // The frames: one for each change of the page, with the time it was drawn.
  const cdp = await context.newCDPSession(page);
  const frames: { file: string; t: number }[] = [];
  const writes: Promise<void>[] = [];
  const scenes: { name: string; frame: number }[] = [];
  const scene = (name: string) => scenes.push({ name, frame: frames.length });
  cdp.on("Page.screencastFrame", (frame) => {
    const file = `f-${String(frames.length).padStart(5, "0")}.png`;
    frames.push({ file, t: frame.metadata.timestamp ?? 0 });
    writes.push(writeFile(join(out, file), Buffer.from(frame.data, "base64")));
    void cdp.send("Page.screencastFrameAck", { sessionId: frame.sessionId });
  });
  await cdp.send("Page.startScreencast", { format: "png", everyNthFrame: 1 });

  const hand = new Hand(page);
  const header = page.getByRole("navigation", { name: "Main" });

  // ── The fleet: how it is, what needs attention, and where. ────────────────
  scene("fleet");
  await sleep(700);
  await hand.to(
    page.locator("section", { has: page.getByRole("heading", { name: "Status now" }) }),
  );
  await hand.read(10);
  const map = page.getByRole("region", { name: "Map of the fleet" });
  const panel = page.locator("section", {
    has: page.getByRole("heading", { level: 2, name: "Needs attention" }),
  });
  const rows = panel.getByRole("listitem");
  if ((await rows.count()) > 0) {
    // The first thing that needs attention: its reading against its limit,
    // and then the site itself.
    const row = rows.first();
    await hand.to(row.locator(".meter"));
    await hand.read(10);
    await hand.click(row.getByRole("link").first());
  } else {
    // A quiet fleet: a substation instead.
    await hand.click(page.getByRole("button", { name: /Zone:/ }).first());
  }
  await expect(page).toHaveURL(/[?&]sub=/);
  await expect(map.locator(".fleet-site").first()).toBeVisible();
  await hand.read(8);
  // What a mark on the map says: its export against its limit, in a card.
  // One that is wholly in the window: the pointer must not leave the frame.
  const frame = (await map.boundingBox())!;
  let inView: Box | undefined;
  for (const site of await map.locator(".fleet-site").all()) {
    const box = await site.boundingBox();
    if (
      box &&
      box.y > frame.y + 30 &&
      box.y + box.height < TOUR.viewport.height - 110 &&
      box.x > frame.x + 60 &&
      box.x + box.width < frame.x + frame.width - 60
    ) {
      inView = box;
      break;
    }
  }
  if (inView) {
    await hand.to(inView);
    await expect(page.locator(".card-over .spark")).toBeVisible();
    await hand.read(9);
  }

  // ── Its feeder, through time. ─────────────────────────────────────────────
  scene("feeder");
  const toFeeder = page.getByRole("link", { name: /^Open (its feeder|feeder )/ }).first();
  await hand.click(toFeeder);
  await expect(page).toHaveURL(/\/feeder/);
  // From here on things may move: the dashes of the network do.
  await page.emulateMedia({ reducedMotion: "no-preference" });
  const chart = page.locator("figure", { hasText: "Export: allowed and measured" });
  await expect(page.getByText("The feeder through time")).toBeVisible();
  await expect(chart.locator("canvas")).toBeVisible();
  await hand.read(5);
  const plot = (await chart.locator(".u-over").boundingBox())!;
  // Along the morning of the chart, reading the values as they go by: what
  // was allowed, and what the fleet did.
  // A short stretch of it: every frame of a readout that moves is a good
  // part of the window again.
  await hand.to(around(within(plot, 0.08, 0.55)));
  await hand.to(around(within(plot, 0.24, 0.42)), {
    tempo: 1.7,
    correct: false,
    wander: 3,
    bow: 0.4,
  });
  await hand.read(4);
  // A closer look at the hours around now: a range dragged across the plot.
  await hand.drag(within(plot, 0.12, 0.5), within(plot, 0.44, 0.53));
  await expect(page).toHaveURL(/[?&]from=/);
  await hand.read(4);
  // One line less.
  await hand.click(chart.getByRole("button", { name: /^Allowed by a fixed limit/ }));
  await hand.read(3);
  // The first clip ends here, on the chart.
  const firstEnds = frames.length;

  // ── Its network, as the engine solved it. ─────────────────────────────────
  scene("network");
  await hand.click(header.getByRole("link", { name: "Network" }));
  await expect(page.locator(".schematic .bus").first()).toBeVisible();
  await expect(page.getByRole("slider", { name: "The instant on show" })).toBeVisible();
  await expect(page.getByText("Constrained").or(page.getByText("Normal")).first()).toBeVisible();
  await sleep(400);
  // The second clip begins here, with the network drawn: from the frame that
  // is on the screen now.
  const secondBegins = Math.max(firstEnds, frames.length - 1);
  await hand.read(5);
  // What one fixed limit for everyone would do.
  await hand.click(page.getByRole("link", { name: "At the fixed limit" }));
  await hand.read(6);
  // Half a day at a touch: the slider, a few hours on.
  const slider = (await page.getByRole("slider", { name: "The instant on show" }).boundingBox())!;
  await hand.drag(within(slider, 0.5, 0.5), within(slider, 0.62, 0.5));
  await hand.read(5);
  await hand.click(page.getByRole("button", { name: "Zoom in" }));
  const sheet = (await page.locator(".schematic").boundingBox())!;
  const seen = {
    ...sheet,
    height: Math.min(sheet.height, TOUR.viewport.height - sheet.y - 20),
  };
  await hand.drag(within(seen, 0.62, 0.45), within(seen, 0.34, 0.55));
  // What a bus says of itself.
  const buses = page.locator(".schematic .bus .hit");
  const shown: Box[] = [];
  for (const bus of await buses.all()) {
    const box = await bus.boundingBox();
    if (
      box &&
      box.x > seen.x + 80 &&
      box.x < seen.x + seen.width - 200 &&
      box.y > seen.y + 40 &&
      box.y < seen.y + seen.height - 60
    )
      shown.push(box);
  }
  if (shown.length > 0) {
    await hand.to(shown[Math.floor(shown.length / 2)]!);
    await hand.read(8);
  }

  // ── What to do about it. ──────────────────────────────────────────────────
  scene("operations");
  await hand.click(header.getByRole("link", { name: "Operations" }));
  await expect(page.getByRole("heading", { name: "Engine runs" })).toBeVisible();
  await hand.read(10);
  scene("end");
  await sleep(600);

  await cdp.send("Page.stopScreencast");
  await Promise.all(writes);

  // The lists ffmpeg reads: each frame of a clip, and how long it stood. The
  // last frame of a clip stands a while, so the eye has it before the loop.
  const list = (name: string, from: number, to: number) => {
    const lines = ["ffconcat version 1.0"];
    for (let i = from; i < to; i++) {
      const seconds = i + 1 < to ? Math.max(0.01, frames[i + 1]!.t - frames[i]!.t) : 1.6;
      lines.push(`file '${frames[i]!.file}'`, `duration ${seconds.toFixed(4)}`);
    }
    // The last frame is named again: ffmpeg gives a duration to what follows it.
    lines.push(`file '${frames[to - 1]!.file}'`);
    writeFileSync(join(out, `${name}.ffconcat`), lines.join("\n") + "\n");
    return frames[to - 1]!.t - frames[from]!.t + 1.6;
  };
  const clips = {
    "tour-fleet": list("tour-fleet", 0, firstEnds),
    "tour-network": list("tour-network", secondBegins, frames.length),
  };
  // And the whole take in one, for the video that a reviewer looks at.
  list("frames", 0, frames.length);

  // ── Was it a hand? The moves the page received, against the model. ────────
  const moves = await page.evaluate(
    () => (window as unknown as { __tour: { moves: [number, number, number][] } }).__tour.moves,
  );
  // One stretch of movement after another: a gap is a pause.
  const strokes: [number, number, number][][] = [];
  for (const sample of moves) {
    const last = strokes.at(-1)?.at(-1);
    if (!last || sample[2] - last[2] > 120) strokes.push([]);
    strokes.at(-1)!.push(sample);
  }
  const dist = (a: number[], b: number[]) => Math.hypot(b[0]! - a[0]!, b[1]! - a[1]!);
  const measured = strokes
    .map((stroke) => {
      const [first, last] = [stroke[0]!, stroke.at(-1)!];
      const time = last[2] - first[2];
      const chord = dist(first, last);
      const path = stroke.slice(1).reduce((sum, s, i) => sum + dist(stroke[i]!, s), 0);
      // Speed over a few samples, so that one late frame is not a peak.
      let [peak, peakAt] = [0, 0];
      for (let i = 3; i < stroke.length; i++) {
        const speed = dist(stroke[i - 3]!, stroke[i]!) / (stroke[i]![2] - stroke[i - 3]![2]);
        if (speed > peak) [peak, peakAt] = [speed, (stroke[i - 1]![2] - first[2]) / time];
      }
      return { time, chord, path, peakAt, mean: path / time, peak };
    })
    // The moves across the page: not the small shifts of a hand at rest.
    .filter((m) => m.chord > 120 && m.time > 150);
  const median = (values: number[]) =>
    [...values].sort((a, b) => a - b)[Math.floor(values.length / 2)]!;
  const frameGaps = frames.slice(1).map((f, i) => 1000 * (f.t - frames[i]!.t));
  const notes = {
    seed: SEED,
    frames: frames.length,
    seconds: frames.at(-1)!.t - frames[0]!.t,
    "clip seconds": clips,
    scenes,
    "median frame gap ms": median(frameGaps),
    moves: measured.length,
    "median peak at": median(measured.map((m) => m.peakAt)),
    "peak over mean speed": median(measured.map((m) => m.peak / m.mean)),
    "path over chord": median(measured.map((m) => m.path / m.chord)),
    "times ms": measured.map((m) => Math.round(m.time)),
  };
  writeFileSync(join(out, "notes.json"), JSON.stringify(notes, null, 2) + "\n");
  console.log(JSON.stringify(notes));

  // The picture of it, for whoever reviews the take: every move on the
  // window, and the speed of each through its time.
  const sketch = await context.newPage();
  const [w, h] = [TOUR.viewport.width, TOUR.viewport.height];
  const paths = strokes
    .map((s) => `<polyline points="${s.map((p) => `${p[0]},${p[1]}`).join(" ")}"/>`)
    .join("");
  const profiles = strokes
    .filter((s) => dist(s[0]!, s.at(-1)!) > 120)
    .map((s) => {
      const time = s.at(-1)![2] - s[0]![2];
      const speeds = s.slice(1).map((p, i) => dist(s[i]!, p) / (p[2] - s[i]![2] || 1));
      const top = Math.max(...speeds);
      return `<polyline points="${speeds
        .map(
          (v, i) =>
            `${(((s[i + 1]![2] - s[0]![2]) / time) * w).toFixed(1)},${(h + 190 - (v / top) * 170).toFixed(1)}`,
        )
        .join(" ")}"/>`;
    })
    .join("");
  await sketch.setViewportSize({ width: w, height: h + 200 });
  await sketch.setContent(
    `<body style="margin:0;background:#fff"><svg width="${w}" height="${h + 200}" fill="none" stroke="#0b5cad" stroke-width="1" stroke-opacity=".7">` +
      `<rect width="${w}" height="${h}" stroke="#999"/>${paths}<g stroke="#c2410c" stroke-opacity=".45">${profiles}</g>` +
      `<text x="8" y="${h + 16}" fill="#333" stroke="none" font-family="system-ui" font-size="12">Speed of each move across the page, against its own time: one bell, its peak before the middle.</text></svg>`,
  );
  await sketch.screenshot({ path: join(out, "debug.png") });
  await sketch.close();

  // A take is kept only if it looks like a hand's.
  expect(measured.length, "moves across the page").toBeGreaterThan(8);
  // The peak of speed is before the middle of a move, on the whole.
  expect(notes["median peak at"]).toBeGreaterThan(0.2);
  expect(notes["median peak at"]).toBeLessThan(0.55);
  // Nothing like one speed from end to end, and nothing like a straight line.
  expect(notes["peak over mean speed"]).toBeGreaterThan(1.5);
  expect(notes["path over chord"]).toBeGreaterThan(1.001);
  // No two moves the same length of time: the old tour's every move was.
  const times = measured.map((m) => m.time);
  const spread = Math.sqrt(
    times.reduce((sum, t) => sum + (t - times.reduce((a, b) => a + b, 0) / times.length) ** 2, 0) /
      times.length,
  );
  expect(spread / (times.reduce((a, b) => a + b, 0) / times.length)).toBeGreaterThan(0.2);
  // And the recording kept up with it: a frame every few hundredths of a
  // second while the page was changing.
  expect(notes["median frame gap ms"]).toBeLessThan(50 + 1000 / HAND.hz);
});
