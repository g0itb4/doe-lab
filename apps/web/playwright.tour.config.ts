import { defineConfig, devices } from "@playwright/test";

// The recorded tour of the UI for the README: one take, against a real stack
// that scripts/tour.sh has brought up on ports of its own (`just tour`). It
// starts no server and mocks nothing, and it is no part of `just e2e`.
//
// The window is a small desktop's, and tall enough that each page shows what
// the tour is there for without scrolling: in a GIF every frame of a scroll
// is the whole window again, and a few of them weigh more than all the rest.
export const TOUR = {
  viewport: { width: 1024, height: 768 },
  // Where the frames and the take's notes go; scripts/tour.sh reads them.
  out: "test-results/tour",
};

export default defineConfig({
  testDir: "tour",
  testMatch: "tour.spec.ts",
  workers: 1,
  retries: 0,
  // A take waits for feeder time to reach midday, which can be minutes.
  timeout: 20 * 60_000,
  reporter: "list",
  outputDir: "test-results/tour-run",
  use: {
    baseURL: process.env.TOUR_URL ?? "http://localhost:5373",
    ...devices["Desktop Chrome"],
    viewport: TOUR.viewport,
    colorScheme: "light",
    locale: "en-AU",
    timezoneId: "Australia/Sydney",
  },
});
