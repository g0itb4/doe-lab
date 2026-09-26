import { describe, expect, it, vi } from "vitest";
import { render } from "vitest-browser-svelte";
import RangePicker from "./RangePicker.svelte";

// A fixed address for the whole file: a mock of $app/state cannot vary
// between tests in browser mode.
vi.mock("$app/state", () => ({
  page: { url: new URL("https://example.test/?range=6h&from=100&to=200&feeder=LV10") },
}));

describe("RangePicker", () => {
  it("offers each range as a link that keeps the rest of the address and drops the zoom", async () => {
    const screen = await render(RangePicker, { current: "6h" });
    const nav = screen.getByRole("navigation", { name: "Time range" });
    await expect.element(nav).toBeVisible();
    const links = [...nav.element().querySelectorAll("a")];
    expect(links.map((a) => a.textContent?.trim())).toEqual(["6 hours", "24 hours", "3 days"]);
    expect(links.map((a) => a.getAttribute("href"))).toEqual([
      "/?range=6h&feeder=LV10",
      "/?range=24h&feeder=LV10",
      "/?range=3d&feeder=LV10",
    ]);
  });

  it("marks the range in force", async () => {
    const screen = await render(RangePicker, { current: "24h" });
    const marked = [...screen.container.querySelectorAll("a[aria-current=true]")];
    expect(marked.map((a) => a.textContent?.trim())).toEqual(["24 hours"]);
  });

  it("has targets of at least 24 px", async () => {
    const screen = await render(RangePicker, { current: "24h" });
    for (const a of screen.container.querySelectorAll("a")) {
      const box = a.getBoundingClientRect();
      expect(Math.min(box.width, box.height)).toBeGreaterThanOrEqual(24);
    }
  });
});
