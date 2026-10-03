import { defineConfig, devices } from "@playwright/test";

// The run-time measurements of e2e/perf.spec.ts: the production build, one
// browser at a time so that nothing else takes the processor, at a desktop's
// size. `just perf` runs it; it is no part of `just e2e`, because a timing is
// not a thing to fail a commit on.
export default defineConfig({
  testDir: "e2e",
  testMatch: "perf.spec.ts",
  workers: 1,
  fullyParallel: false,
  reporter: "list",
  use: {
    baseURL: "http://localhost:4273",
    timezoneId: "Europe/London",
    locale: "en-GB",
    ...devices["Desktop Chrome"],
    viewport: { width: 1440, height: 900 },
  },
  webServer: {
    command: "bun run preview",
    url: "http://localhost:4273",
    reuseExistingServer: !process.env.CI,
    timeout: 60_000,
  },
});
