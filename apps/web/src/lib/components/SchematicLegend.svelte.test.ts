import { describe, expect, it } from "vitest";
import { render } from "vitest-browser-svelte";
import SchematicLegend from "./SchematicLegend.svelte";

describe("SchematicLegend", () => {
  it("says what each mark of the drawing is, in words beside its shape", async () => {
    const screen = await render(SchematicLegend, { near: "2.3 V" });
    const marks = screen.getByRole("list", { name: "The marks of the drawing" });
    const items = [...marks.element().querySelectorAll("li")];
    expect(items.map((li) => li.textContent?.replace(/\s+/g, " ").trim())).toEqual([
      "The transformer",
      "A site that takes part in envelopes",
      "Another site, or a junction",
      "A line: as heavy as the power in it, with an arrow for its direction",
      "What limits export now",
    ]);
    // Each has its shape, which a screen reader skips for the words.
    for (const li of items) expect(li.querySelector("svg[aria-hidden=true]")).not.toBeNull();
  });

  it("gives each status a shape of its own beside its colour, and says how near is near", async () => {
    const screen = await render(SchematicLegend, { near: "2.3 V" });
    const statuses = screen.getByRole("list", { name: "Status of a mark" });
    const items = [...statuses.element().querySelectorAll("li")];
    expect(items.map((li) => li.textContent?.trim())).toEqual([
      "Inside its limits",
      "Near a limit",
      "Past a limit",
      "Not solved",
    ]);
    const shapes = items.map((li) => li.querySelector("path")!.getAttribute("d"));
    expect(new Set(shapes).size).toBe(4);
    const colours = items.map((li) => getComputedStyle(li.querySelector("span")!).color);
    expect(new Set(colours).size).toBe(4);
    await expect
      .element(screen.getByText(/within 2\.3 V of the edge of the voltage band/))
      .toBeVisible();
  });

  it("says that the dashes move only while they do", async () => {
    const screen = await render(SchematicLegend, { near: "2.3 V", moving: true });
    await expect.element(screen.getByText(", and dashes that move the same way")).toBeVisible();
    await screen.rerender({ moving: false });
    await expect.element(screen.getByText(/dashes that move/)).not.toBeInTheDocument();
  });
});
