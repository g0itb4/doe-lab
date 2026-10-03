import { describe, expect, it, vi } from "vitest";
import { render } from "vitest-browser-svelte";
import SortHeader from "./SortHeader.svelte";

// A fixed address for the whole file: a mock of $app/state cannot vary
// between tests in browser mode.
vi.mock("$app/state", () => ({
  page: { url: new URL("https://example.test/sites?q=ld&sort=cap&dir=asc") },
}));

const fallback = { key: "nmi", dir: "asc" } as const;
const cell = (container: Element) => container.querySelector("th")!;

describe("SortHeader", () => {
  it("is a column heading whose link orders the table by it, and keeps the rest of the address", async () => {
    const screen = await render(SortHeader, {
      key: "name",
      label: "Name",
      sort: { key: "cap", dir: "asc" },
      fallback,
    });
    const th = cell(screen.container);
    expect(th.getAttribute("scope")).toBe("col");
    expect(th.getAttribute("aria-sort")).toBe("none");
    const link = screen.getByRole("link", { name: "Name" });
    await expect.element(link).toHaveAttribute("href", "/sites?q=ld&sort=name&dir=asc");
    // No arrow on a column that does not order the table.
    expect(th.querySelector("svg")).toBeNull();
    expect(link.element().getBoundingClientRect().height).toBeGreaterThanOrEqual(24);
  });

  it("says which way the table runs, with an arrow and to a screen reader, and offers the other way", async () => {
    const screen = await render(SortHeader, {
      key: "cap",
      label: "Connection limit",
      sort: { key: "cap", dir: "asc" },
      fallback,
      align: "right",
    });
    const th = cell(screen.container);
    expect(th.getAttribute("aria-sort")).toBe("ascending");
    expect(th.classList.contains("text-right")).toBe(true);
    const up = th.querySelector("svg path")!.getAttribute("d");
    await expect
      .element(screen.getByRole("link", { name: "Connection limit" }))
      .toHaveAttribute("href", "/sites?q=ld&sort=cap&dir=desc");

    await screen.rerender({ sort: { key: "cap", dir: "desc" } });
    expect(th.getAttribute("aria-sort")).toBe("descending");
    expect(th.querySelector("svg path")!.getAttribute("d")).not.toBe(up);
  });

  it("drops the order from the address when the next one is the table's own", async () => {
    const screen = await render(SortHeader, {
      key: "nmi",
      label: "NMI",
      sort: { key: "nmi", dir: "desc" },
      fallback,
    });
    await expect
      .element(screen.getByRole("link", { name: "NMI" }))
      .toHaveAttribute("href", "/sites?q=ld");
  });
});
