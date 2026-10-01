import { Code, ConnectError, type Transport } from "@connectrpc/connect";
import { AnswerEnd, AssistantTool } from "@doelab/gen/doelab/v1/assistant_pb.js";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { userEvent } from "vitest/browser";
import { render } from "vitest-browser-svelte";
import { assistant, SPENT } from "$lib/assistant.svelte.ts";
import { assistantBackend, type Backend } from "$lib/assistant.test-utils.ts";
import AssistantDrawer from "./AssistantDrawer.svelte";

let router: Transport;
vi.mock("@connectrpc/connect-web", async () => {
  const { transportStub } = await import("$lib/transport.test-utils.ts");
  return transportStub(() => router);
});

let backend: Backend;
beforeEach(() => {
  backend = assistantBackend();
  router = backend.router;
  assistant.availability = "available";
});
afterEach(() => {
  assistant.clear();
  assistant.availability = "unknown";
});

const answer = [
  {
    lookup: {
      tool: AssistantTool.GET_BINDING_CONSTRAINT,
      subject: "XDLAB000014 at 12:30 on 10 Nov",
      found: true,
    },
  },
  { lookup: { tool: AssistantTool.GET_CONFIG, subject: "the config", found: false } },
  { text: "Voltage at XDLAB000022.\n" },
  { text: "The limit is 2.1 kW." },
  { end: AnswerEnd.COMPLETE },
];

const dialog = () => document.querySelector("dialog")!;

describe("the assistant drawer", () => {
  it("opens as a modal dialog with a name, and the focus in the question", async () => {
    const screen = await render(AssistantDrawer, { open: true, feederCode: "LV10" });
    await expect.element(screen.getByRole("dialog", { name: "Ask about LV10" })).toBeVisible();
    expect(dialog().matches(":modal")).toBe(true);
    await expect.element(screen.getByLabelText("Your question")).toHaveFocus();
    // It says what writes the answers, before any is asked for.
    await expect.element(screen.getByText(/A language model writes the answer/)).toBeVisible();
  });

  it("stays closed until it is opened", async () => {
    const screen = await render(AssistantDrawer, { open: false, feederCode: "LV10" });
    expect(dialog().open).toBe(false);
    await screen.rerender({ open: true });
    await vi.waitFor(() => expect(dialog().open).toBe(true));
    await screen.rerender({ open: false });
    await vi.waitFor(() => expect(dialog().open).toBe(false));
  });

  it("offers questions to start from, about the site on screen when there is one", async () => {
    const screen = await render(AssistantDrawer, { open: true, feederCode: "LV10" });
    await expect
      .element(screen.getByRole("button", { name: "Which sites are over their limit now?" }))
      .toBeVisible();
    await expect.element(screen.getByText(/Why is .* limited now/)).not.toBeInTheDocument();

    await screen.rerender({ nmi: "XDLAB000014" });
    await expect
      .element(screen.getByRole("button", { name: "Why is XDLAB000014 limited now?" }))
      .toBeVisible();
  });

  it("asks a suggested question, and shows the lookups and the answer", async () => {
    backend.scripts.push(answer);
    const screen = await render(AssistantDrawer, {
      open: true,
      feederCode: "LV10",
      nmi: "XDLAB000014",
    });
    await screen.getByRole("button", { name: "Why is XDLAB000014 limited now?" }).click();

    const region = screen.getByRole("region", { name: "Answer" });
    await expect.element(region).toHaveTextContent("The limit is 2.1 kW.");
    await expect.element(region).toHaveTextContent("Why is XDLAB000014 limited now?");
    await expect.element(region).toHaveAttribute("aria-live", "polite");
    await expect.element(region).toHaveAttribute("aria-busy", "false");
    const lookups = screen.getByRole("list", { name: "Looked up" }).getByRole("listitem");
    await expect
      .element(lookups.nth(0))
      .toHaveTextContent("What limits XDLAB000014 at 12:30 on 10 Nov");
    await expect.element(lookups.nth(1)).toHaveTextContent("The config: nothing found");
    // The line break of the answer is kept.
    const text = screen.getByText(/Voltage at XDLAB000022/).element();
    expect(getComputedStyle(text).whiteSpace).toBe("pre-wrap");

    expect(backend.asked[0]).toMatchObject({
      feederCode: "LV10",
      question: "Why is XDLAB000014 limited now?",
      nmi: "XDLAB000014",
    });
    // The focus is where the next question is typed.
    await expect.element(screen.getByLabelText("Your question")).toHaveFocus();
  });

  it("asks what was typed on Enter, and not on Shift+Enter or with nothing typed", async () => {
    backend.scripts.push(answer);
    const screen = await render(AssistantDrawer, { open: true, feederCode: "LV10" });
    const field = screen.getByLabelText("Your question");
    const ask = screen.getByRole("button", { name: "Ask", exact: true });
    await expect.element(ask).toBeDisabled();
    await expect.element(screen.getByText("500 characters left.", { exact: false })).toBeVisible();

    await userEvent.keyboard("{Enter}");
    await field.fill("   ");
    await userEvent.keyboard("{Enter}");
    await expect.element(ask).toBeDisabled();
    expect(backend.asked).toHaveLength(0);

    await field.fill("Is the feeder constrained?");
    await expect.element(screen.getByText("474 characters left.", { exact: false })).toBeVisible();
    await userEvent.keyboard("{Shift>}{Enter}{/Shift}");
    expect(backend.asked).toHaveLength(0);
    await field.fill("Is the feeder constrained?");
    await userEvent.keyboard("{Enter}");

    await expect
      .element(screen.getByRole("region", { name: "Answer" }))
      .toHaveTextContent("The limit is 2.1 kW.");
    expect(backend.asked[0]).toMatchObject({ question: "Is the feeder constrained?" });
    expect(backend.asked[0]!.nmi).toBeUndefined();
    // The field is empty again, for the next one.
    await expect.element(field).toHaveValue("");
  });

  it("asks with the button", async () => {
    backend.scripts.push(answer);
    const screen = await render(AssistantDrawer, { open: true, feederCode: "LV10" });
    await screen.getByLabelText("Your question").fill("How is capacity shared?");
    await screen.getByRole("button", { name: "Ask", exact: true }).click();
    await expect
      .element(screen.getByRole("region", { name: "Answer" }))
      .toHaveTextContent("The limit is 2.1 kW.");
  });

  it("says that it is working, and can be stopped", async () => {
    backend.scripts.push(["hold", { text: "never sent" }]);
    const screen = await render(AssistantDrawer, { open: true, feederCode: "LV10" });
    await screen.getByLabelText("Your question").fill("Why?");
    await userEvent.keyboard("{Enter}");

    await expect.element(screen.getByText("Looking things up…")).toBeVisible();
    await expect
      .element(screen.getByRole("region", { name: "Answer" }))
      .toHaveAttribute("aria-busy", "true");
    // One question at a time.
    await expect
      .element(screen.getByRole("button", { name: "Ask", exact: true }))
      .not.toBeInTheDocument();
    await screen.getByLabelText("Your question").fill("And another?");
    await userEvent.keyboard("{Enter}");
    expect(backend.asked).toHaveLength(1);

    await screen.getByRole("button", { name: "Stop" }).click();
    await expect.element(screen.getByText("Stopped.")).toBeVisible();
    await expect.element(screen.getByText("Looking things up…")).not.toBeInTheDocument();
    await expect.element(screen.getByRole("button", { name: "Ask", exact: true })).toBeVisible();
  });

  it("shows a failure in plain words, with a way to ask again", async () => {
    backend.scripts.push([new ConnectError("upstream said no", Code.Unavailable)], answer);
    const screen = await render(AssistantDrawer, { open: true, feederCode: "LV10" });
    await screen.getByRole("button", { name: "Which sites are over their limit now?" }).click();

    const alert = screen.getByRole("alert");
    await expect
      .element(alert)
      .toHaveTextContent("The assistant could not answer just now. Try again.");
    await expect.element(alert).not.toHaveTextContent("upstream");

    await screen.getByRole("button", { name: "Ask again" }).click();
    await expect
      .element(screen.getByRole("region", { name: "Answer" }))
      .toHaveTextContent("The limit is 2.1 kW.");
    expect(backend.asked.map((q) => q.question)).toEqual([
      "Which sites are over their limit now?",
      "Which sites are over their limit now?",
    ]);
  });

  it("says when the answer did not end well", async () => {
    backend.scripts.push([{ text: "It is" }, { end: AnswerEnd.CUT_SHORT }]);
    const screen = await render(AssistantDrawer, { open: true, feederCode: "LV10" });
    await screen.getByRole("button", { name: "Which sites are over their limit now?" }).click();
    await expect
      .element(screen.getByText("The answer reached its length limit and stops here."))
      .toBeVisible();
  });

  it("takes no question when the day's budget is spent, and says when it is back", async () => {
    assistant.availability = "spent";
    const screen = await render(AssistantDrawer, { open: true, feederCode: "LV10" });
    await expect.element(screen.getByRole("status")).toHaveTextContent(SPENT);
    await expect.element(screen.getByLabelText("Your question")).toBeDisabled();
    await expect
      .element(screen.getByRole("button", { name: "Which sites are over their limit now?" }))
      .toBeDisabled();
    await expect.element(screen.getByRole("button", { name: "Ask", exact: true })).toBeDisabled();
  });

  it("offers no second try once the budget ran out under a question", async () => {
    backend.scripts.push([new ConnectError("over", Code.ResourceExhausted)]);
    const screen = await render(AssistantDrawer, { open: true, feederCode: "LV10" });
    backend.status = { available: false, unavailable: 2 };
    await screen.getByRole("button", { name: "Which sites are over their limit now?" }).click();
    await expect.element(screen.getByRole("alert")).toHaveTextContent(SPENT);
    await expect.element(screen.getByRole("button", { name: "Ask again" })).not.toBeInTheDocument();
  });

  it("closes with its button, with Escape and with a click outside it, and stops the answer", async () => {
    backend.scripts.push(["hold"]);
    const screen = await render(AssistantDrawer, { open: true, feederCode: "LV10" });
    await screen.getByRole("button", { name: "Which sites are over their limit now?" }).click();
    await vi.waitFor(() => expect(assistant.asking).toBe(true));

    await screen.getByRole("button", { name: "Close" }).click();
    await vi.waitFor(() => expect(dialog().open).toBe(false));
    expect(assistant.asking).toBe(false);

    await screen.rerender({ open: true });
    await vi.waitFor(() => expect(dialog().open).toBe(true));
    await userEvent.keyboard("{Escape}");
    await vi.waitFor(() => expect(dialog().open).toBe(false));

    await screen.rerender({ open: true });
    await vi.waitFor(() => expect(dialog().open).toBe(true));
    // Inside the panel: it stays.
    await screen.getByRole("heading", { name: "Ask about LV10" }).click();
    expect(dialog().open).toBe(true);
    // On the backdrop: it goes.
    dialog().dispatchEvent(new MouseEvent("click", { bubbles: true }));
    await vi.waitFor(() => expect(dialog().open).toBe(false));
  });
});
