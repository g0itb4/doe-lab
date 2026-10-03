import { afterEach, describe, expect, it, vi } from "vitest";
import { render } from "vitest-browser-svelte";
import HoverCard from "./components/HoverCard.svelte";
import { type Card, hint, hovercard } from "./hovercard.svelte.ts";

afterEach(() => hovercard.hide());

function mark(content: () => Card | undefined) {
  const node = document.createElement("button");
  node.textContent = "a mark";
  node.style.cssText = "position: fixed; left: 40px; top: 30px;";
  document.body.append(node);
  const action = hint(node, content);
  return { node, action };
}
const mouse = (node: Element, type: string, clientX = 0, clientY = 0) =>
  node.dispatchEvent(new MouseEvent(type, { clientX, clientY }));

describe("a mark's card", () => {
  it("is shown beside the pointer while it is over the mark, and follows it", () => {
    const { node, action } = mark(() => ({ title: "B3", lines: ["259.9 V"] }));
    expect(hovercard.card).toBeUndefined();
    mouse(node, "mouseenter", 100, 80);
    expect(hovercard.card).toEqual({ title: "B3", lines: ["259.9 V"] });
    expect(hovercard.placement).toMatchObject({ left: 112, top: 92 });
    mouse(node, "mousemove", 140, 90);
    expect(hovercard.placement).toMatchObject({ left: 152, top: 102 });
    mouse(node, "mouseleave");
    expect(hovercard.card).toBeUndefined();
    action.destroy();
    node.remove();
  });

  it("goes to the side of the window with more room", () => {
    const { node, action } = mark(() => ({ title: "far", lines: [] }));
    mouse(node, "mouseenter", window.innerWidth - 10, window.innerHeight - 10);
    expect(hovercard.placement).toMatchObject({ right: 22, bottom: 22 });
    expect(hovercard.placement.left).toBeUndefined();
    action.destroy();
    node.remove();
  });

  it("is shown under the mark for the keyboard, and put away on Escape or when the focus leaves", () => {
    const { node, action } = mark(() => ({ title: "Lidcombe", lines: ["Normal"], level: "ok" }));
    node.focus();
    expect(hovercard.card?.title).toBe("Lidcombe");
    const box = node.getBoundingClientRect();
    expect(hovercard.placement).toMatchObject({ left: box.left + 12, top: box.bottom + 12 });
    node.dispatchEvent(new KeyboardEvent("keydown", { key: "a" }));
    expect(hovercard.card).toBeDefined();
    node.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape" }));
    expect(hovercard.card).toBeUndefined();
    node.focus();
    node.blur();
    expect(hovercard.card).toBeUndefined();
    action.destroy();
    node.remove();
  });

  it("says what the mark says now, and nothing when the mark has nothing to say", () => {
    let label = "first";
    const { node, action } = mark(() => ({ title: label, lines: [] }));
    mouse(node, "mouseenter", 10, 10);
    expect(hovercard.card?.title).toBe("first");
    label = "second";
    mouse(node, "mousemove", 12, 10);
    expect(hovercard.card?.title).toBe("second");
    // A mark given something else to say, and then nothing.
    action.update(() => ({ title: "third", lines: [] }));
    mouse(node, "mousemove", 12, 10);
    expect(hovercard.card?.title).toBe("third");
    action.update(() => undefined);
    mouse(node, "mousemove", 12, 10);
    expect(hovercard.card).toBeUndefined();
    // A mark that goes takes its card with it, and listens no more.
    action.update(() => ({ title: "fourth", lines: [] }));
    mouse(node, "mouseenter", 10, 10);
    action.destroy();
    expect(hovercard.card).toBeUndefined();
    mouse(node, "mouseenter", 10, 10);
    expect(hovercard.card).toBeUndefined();
    node.remove();
  });
});

describe("HoverCard", () => {
  it("draws the card over the page, for the eye only, and takes no pointer", async () => {
    const screen = await render(HoverCard);
    expect(screen.container.querySelector(".card-over")).toBeNull();
    hovercard.show(
      { title: "NMI00000033, Solar", lines: ["Over limit", "Exporting 2.7 kW."], level: "warn" },
      60,
      40,
    );
    await expect.element(screen.getByText("NMI00000033, Solar")).toBeVisible();
    const card = screen.container.querySelector(".card-over") as HTMLElement;
    expect(card.getAttribute("aria-hidden")).toBe("true");
    expect(getComputedStyle(card).pointerEvents).toBe("none");
    expect(getComputedStyle(card).position).toBe("fixed");
    expect(card.getBoundingClientRect().left).toBe(72);
    expect(card.getBoundingClientRect().top).toBe(52);
    // Its status is a badge: a shape and a word as well as a colour.
    expect(card.querySelector("[data-level=warn]")!.textContent).toContain("Over limit");
    expect(card.textContent).toContain("Exporting 2.7 kW.");

    // Without a status, every line is a line.
    hovercard.show({ title: "L_far", lines: ["3.0 kW towards the transformer"] }, 60, 40);
    await expect.element(screen.getByText("3.0 kW towards the transformer")).toBeVisible();
    expect(screen.container.querySelector("[data-level]")).toBeNull();
    // A status with no word still has its badge.
    hovercard.show({ title: "bare", lines: [], level: "ok" }, 60, 40);
    await expect.element(screen.getByText("bare")).toBeVisible();
    expect(screen.container.querySelector("[data-level=ok]")).not.toBeNull();

    hovercard.hide();
    await expect.element(screen.getByText("bare")).not.toBeInTheDocument();
  });

  it("draws a card's plot against its limit, asks for it when the card is shown and not before", async () => {
    let values: number[] | undefined;
    const [ask, rest] = [vi.fn(), vi.fn()];
    const plot = {
      label: "Net export, last 6 hours",
      values: () => values,
      limit: 4000,
      format: (watts: number) => `${(watts / 1000).toFixed(1)} kW`,
      ask,
      rest,
    };
    const screen = await render(HoverCard);
    expect(ask).not.toHaveBeenCalled();
    hovercard.show(
      {
        title: "NMI00000033, Solar",
        lines: ["Within limit", "Exporting 2.0 kW of 4.0 kW"],
        level: "ok",
        plot,
      },
      60,
      40,
    );
    await expect
      .element(screen.getByText("Net export, last 6 hours", { exact: true }))
      .toBeVisible();
    await vi.waitFor(() => expect(ask).toHaveBeenCalledOnce());
    const card = screen.container.querySelector(".card-over") as HTMLElement;
    // The room for the plot is there before the plot is: the limit's rule,
    // and no line yet.
    const height = card.getBoundingClientRect().height;
    expect(card.querySelector("line.limit")).not.toBeNull();
    expect(card.querySelector(".spark path")).toBeNull();
    expect(card.getBoundingClientRect().width).toBeGreaterThanOrEqual(13 * 16);
    // Its words are few: the status, and one line.
    expect([...card.querySelectorAll("p.text-label")].map((p) => p.textContent)).toEqual([
      "Exporting 2.0 kW of 4.0 kW",
    ]);

    // The card goes before its plot came: the question is taken back.
    hovercard.hide();
    await vi.waitFor(() => expect(rest).toHaveBeenCalledOnce());

    // With its values, the line is drawn, and the card is no taller.
    values = [1000, 2000, 2000];
    hovercard.show(
      { title: "NMI00000033, Solar", lines: ["Within limit"], level: "ok", plot: { ...plot } },
      60,
      40,
    );
    await vi.waitFor(() =>
      expect(screen.container.querySelector(".spark path")?.getAttribute("d")).toBe(
        "M0 17.3L60 12L120 12",
      ),
    );
    expect(
      screen.container.querySelector(".card-over")!.getBoundingClientRect().height,
    ).toBeLessThanOrEqual(height);
    hovercard.hide();
  });
});
