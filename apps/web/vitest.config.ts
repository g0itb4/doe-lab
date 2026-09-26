import { sveltekit } from "@sveltejs/kit/vite";
import tailwindcss from "@tailwindcss/vite";
import { playwright } from "@vitest/browser-playwright";
import { defineConfig } from "vitest/config";

// One project, in a real browser, for every test: the components are tested
// where they run, with the real stylesheet, and coverage comes from one map.
export default defineConfig({
  // tailwindcss as well as sveltekit: without it app.css is imported but never
  // compiled, no utility exists, and every assertion about layout or colour
  // measures an unstyled page.
  plugins: [tailwindcss(), sveltekit()],
  test: {
    include: ["src/**/*.test.ts"],
    setupFiles: ["src/test-setup.ts"],
    browser: {
      enabled: true,
      headless: true,
      // Not the feeder's zone, on purpose: a time that is formatted in the
      // reader's zone instead of the feeder's then fails a test.
      provider: playwright({ contextOptions: { timezoneId: "Europe/London", locale: "en-GB" } }),
      instances: [{ browser: "chromium" }],
    },
    coverage: {
      // istanbul, not v8: the v8 provider reads coverage from the runtime's
      // inspector, which Bun does not expose.
      provider: "istanbul",
      // The library is held to the thresholds here. The pages in src/routes
      // are exercised by the Playwright suite in e2e/, against the built app.
      include: ["src/lib/**/*.{ts,svelte}"],
      exclude: ["src/**/*.test.ts", "src/**/*.test-utils.ts"],
      reporter: ["text-summary", "html", "json"],
      // Floors, as in apps/api/coverage.json: they only go up.
      thresholds: { lines: 98, functions: 97, statements: 98, branches: 90 },
    },
  },
});
