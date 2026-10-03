import { createRawSnippet } from "svelte";
import { afterEach, describe, expect, it, vi } from "vitest";
import { render } from "vitest-browser-svelte";
import { theme } from "$lib/theme.svelte.ts";
import { toasts } from "$lib/toast.svelte.ts";
import EmptyState from "./EmptyState.svelte";
import ErrorState from "./ErrorState.svelte";
import Kpi from "./Kpi.svelte";
import Skeleton from "./Skeleton.svelte";
import StaleBanner from "./StaleBanner.svelte";
import StatusBadge from "./StatusBadge.svelte";
import ThemeToggle from "./ThemeToggle.svelte";
import Toasts from "./Toasts.svelte";

afterEach(() => {
  toasts.clear();
  theme.set("system");
});

describe("Skeleton", () => {
  it("tells a screen reader what is loading, and holds the final size", async () => {
    const screen = await render(Skeleton, { label: "fleet figures", class: "h-20 w-40" });
    const status = screen.getByRole("status");
    await expect.element(status).toHaveTextContent("Loading fleet figures…");
    const box = status.element().getBoundingClientRect();
    expect([box.width, box.height]).toEqual([160, 80]);
  });
});

describe("EmptyState", () => {
  it("says why there is nothing, and what to do", async () => {
    const screen = await render(EmptyState, {
      title: "No envelopes yet",
      children: createRawSnippet(() => ({
        render: () => "<span>Run the engine: <code>just engine</code></span>",
      })),
    });
    await expect.element(screen.getByText("No envelopes yet")).toBeVisible();
    await expect.element(screen.getByText("just engine")).toBeVisible();
  });
});

describe("ErrorState", () => {
  it("says what failed and offers a retry", async () => {
    const onretry = vi.fn();
    const screen = await render(ErrorState, { message: "The API cannot be reached.", onretry });
    await expect.element(screen.getByRole("alert")).toHaveTextContent("The API cannot be reached.");
    await screen.getByRole("button", { name: "Try again" }).click();
    expect(onretry).toHaveBeenCalledOnce();
  });

  it("has no retry button when there is nothing to retry", async () => {
    const screen = await render(ErrorState, { message: "Not found." });
    expect(screen.getByRole("button").elements()).toHaveLength(0);
  });
});

describe("StaleBanner", () => {
  it("is silent while the stream is live, and speaks when it drops", async () => {
    const screen = await render(StaleBanner, { stale: false });
    const region = screen.getByRole("status");
    await expect.element(region).toHaveTextContent("");
    await expect.element(region).toHaveAttribute("aria-live", "polite");

    await screen.rerender({ stale: true, what: "Fleet figures" });
    await expect.element(region).toHaveTextContent("Fleet figures paused, reconnecting…");
  });
});

describe("Toasts", () => {
  it("announces a success politely and a failure at once", async () => {
    const screen = await render(Toasts);
    toasts.show("ok", "Backstop cleared");
    toasts.show("error", "That token is not accepted for this action.");
    await expect.element(screen.getByRole("status")).toHaveTextContent("Backstop cleared");
    await expect.element(screen.getByRole("alert")).toHaveTextContent("That token is not accepted");
  });

  it("can be dismissed", async () => {
    const screen = await render(Toasts);
    toasts.show("error", "Refused");
    await screen.getByRole("button", { name: "Dismiss" }).click();
    expect(screen.getByRole("alert").elements()).toHaveLength(0);
  });
});

describe("StatusBadge", () => {
  it.each(["ok", "info", "warn", "critical"] as const)(
    "says %s with an icon and a word, not only a colour",
    async (level) => {
      const screen = await render(StatusBadge, { level, label: `Label for ${level}` });
      const badge = screen.getByText(`Label for ${level}`);
      await expect.element(badge).toHaveAttribute("data-level", level);
      expect(badge.element().querySelector("svg[aria-hidden=true]")).not.toBeNull();
    },
  );

  it("has a large form for the headline", async () => {
    const screen = await render(StatusBadge, { level: "ok", label: "Normal", large: true });
    await expect.element(screen.getByText("Normal")).toHaveClass("text-base");
  });

  it("gives each status a mark of its own shape, in its own colour, beside a word in the colour of text", async () => {
    theme.set("light");
    const shapes = new Set<string>();
    const colours = new Set<string>();
    for (const level of ["ok", "info", "warn", "critical"] as const) {
      const screen = await render(StatusBadge, { level, label: `Word for ${level}` });
      const badge = screen.getByText(`Word for ${level}`).element();
      const mark = badge.querySelector(`svg[data-mark=${level}]`)!;
      shapes.add(mark.querySelector("path")!.getAttribute("d")!);
      colours.add(getComputedStyle(mark).color);
      // A solid shape, not an outline with a sign inside it.
      expect(getComputedStyle(mark).fill).toBe(getComputedStyle(mark).color);
      // The word reads the same on every status.
      expect(getComputedStyle(badge).color).toBe(getComputedStyle(document.body).color);
    }
    expect(shapes.size).toBe(4);
    expect(colours.size).toBe(4);
  });

  it("is quiet while all is well, and tinted only when it wants attention", async () => {
    theme.set("light");
    const fill = async (level: "ok" | "info" | "warn" | "critical") => {
      const screen = await render(StatusBadge, { level, label: `Tint of ${level}` });
      return getComputedStyle(screen.getByText(`Tint of ${level}`).element()).backgroundColor;
    };
    const clear = "rgba(0, 0, 0, 0)";
    const [ok, info, warn, critical] = [
      await fill("ok"),
      await fill("info"),
      await fill("warn"),
      await fill("critical"),
    ];
    expect(ok).toBe(clear);
    expect(info).toBe(clear);
    expect(warn).not.toBe(clear);
    expect(critical).not.toBe(clear);
    expect(warn).not.toBe(critical);
  });
});

describe("Kpi", () => {
  it("shows a label, a value and what it is measured against", async () => {
    const screen = await render(Kpi, {
      label: "Export now",
      value: "12.3 kW",
      hint: "of 40.0 kW allowed",
    });
    await expect.element(screen.getByText("Export now")).toBeVisible();
    await expect.element(screen.getByText("12.3 kW")).toBeVisible();
    await expect.element(screen.getByText("of 40.0 kW allowed")).toBeVisible();
  });

  it("can hold a picture of its figure, and take the room of two tiles", async () => {
    const screen = await render(Kpi, {
      label: "Export against its limit",
      value: "2.7 kW of 3.0 kW",
      class: "col-span-2",
      children: createRawSnippet(() => ({ render: () => "<span>a bar</span>" })),
    });
    await expect.element(screen.getByText("a bar")).toBeVisible();
    expect(screen.getByText("a bar").element().closest(".card")!.classList).toContain("col-span-2");
  });
});

describe("ThemeToggle", () => {
  it("names the theme in force and the one a click gives", async () => {
    const screen = await render(ThemeToggle);
    const button = screen.getByRole("button");
    await expect.element(button).toHaveAccessibleName("Theme: System. Switch to Light.");
    await button.click();
    await expect.element(button).toHaveAccessibleName("Theme: Light. Switch to Dark.");
    expect(document.documentElement.dataset.theme).toBe("light");
    await button.click();
    await expect.element(button).toHaveAccessibleName("Theme: Dark. Switch to System.");
    await button.click();
    expect(document.documentElement.dataset.theme).toBeUndefined();
  });

  it("is a target of at least 24 px", async () => {
    const screen = await render(ThemeToggle);
    const box = screen.getByRole("button").element().getBoundingClientRect();
    expect(Math.min(box.width, box.height)).toBeGreaterThanOrEqual(24);
  });
});
