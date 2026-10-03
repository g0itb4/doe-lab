import { defineConfig, devices } from "@playwright/test";

// The end-to-end suite runs the production build, served by `vite preview`,
// in two sizes: a small phone and a desktop. The API is not needed: every
// call to /rpc is answered in the browser by e2e/mock.ts.
export default defineConfig({
  testDir: "e2e",
  // The timings have a config of their own: playwright.perf.config.ts.
  testIgnore: "perf.spec.ts",
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [["list"], ["html", { open: "never" }]] : "list",
  use: {
    baseURL: "http://localhost:4273",
    timezoneId: "Europe/London",
    locale: "en-GB",
    trace: "retain-on-failure",
  },
  projects: [
    { name: "phone", use: { ...devices["Desktop Chrome"], viewport: { width: 360, height: 740 } } },
    {
      name: "desktop",
      use: { ...devices["Desktop Chrome"], viewport: { width: 1440, height: 900 } },
    },
  ],
  webServer: {
    command: "bun run preview",
    url: "http://localhost:4273",
    reuseExistingServer: !process.env.CI,
    timeout: 60_000,
  },
});
