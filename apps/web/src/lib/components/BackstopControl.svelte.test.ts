import { Code, ConnectError, createRouterTransport, type Transport } from "@connectrpc/connect";
import { create } from "@bufbuild/protobuf";
import { BackstopEventSchema, BackstopService } from "@doelab/gen/doelab/v1/backstop_pb.js";
import { afterEach, describe, expect, it, vi } from "vitest";
import { userEvent } from "vitest/browser";
import { render } from "vitest-browser-svelte";
import { operator } from "$lib/operator.svelte.ts";
import { timestamp } from "$lib/time.ts";
import { toasts } from "$lib/toast.svelte.ts";
import BackstopControl from "./BackstopControl.svelte";

let router: Transport;
vi.mock("@connectrpc/connect-web", async () => {
  const { transportStub } = await import("$lib/transport.test-utils.ts");
  return transportStub(() => router);
});

// What the API was asked, and a hand on when it answers.
type Call = { method: string; token: string | null; body: unknown };
function backend(answer: () => Promise<void> | void = () => {}) {
  const calls: Call[] = [];
  router = createRouterTransport(({ service }) => {
    service(BackstopService, {
      createBackstopEvent: async (req, ctx) => {
        calls.push({ method: "create", token: ctx.requestHeader.get("Authorization"), body: req });
        await answer();
        return { backstopEvent: { id: "b-1" } };
      },
      clearBackstop: async (req, ctx) => {
        calls.push({ method: "clear", token: ctx.requestHeader.get("Authorization"), body: req });
        await answer();
        return { backstopEvent: { id: req.id } };
      },
    });
  });
  return calls;
}

const base = {
  feederId: "f-1",
  feederCode: "LV10",
  sites: 56,
  zone: "Australia/Sydney",
  active: undefined,
};

afterEach(() => {
  operator.token = "";
  toasts.clear();
});

async function fill(
  screen: Awaited<ReturnType<typeof render>>,
  token: string,
  reason: string,
  confirm: string,
) {
  await screen.getByLabelText("Operator token").fill(token);
  await screen.getByLabelText("Reason").fill(reason);
  await screen.getByLabelText(/Type the feeder's code/).fill(confirm);
}

describe("triggering a backstop", () => {
  it("says what will happen, and to how many sites, before anything is confirmed", async () => {
    backend();
    const screen = await render(BackstopControl, { ...base, onchange: vi.fn() });
    await expect
      .element(screen.getByText("This will: Set export to 0.0 kW for 56 sites."))
      .toBeVisible();
    await screen.getByLabelText("Export limit while active").fill("1.5");
    await expect
      .element(screen.getByText("This will: Set export to 1.5 kW for 56 sites."))
      .toBeVisible();
  });

  it("is disabled until the token, the reason and the typed confirmation are all there", async () => {
    backend();
    const screen = await render(BackstopControl, { ...base, onchange: vi.fn() });
    const button = screen.getByRole("button", { name: "Trigger the backstop" });
    await expect.element(button).toBeDisabled();
    await expect
      .element(screen.getByText("Needs the token, a reason and the confirmation."))
      .toBeVisible();

    await fill(screen, "token", "transformer alarm", "LV1");
    await expect.element(button).toBeDisabled();
    await screen.getByLabelText(/Type the feeder's code/).fill("lv10");
    await expect.element(button).toBeEnabled();
    await screen.getByLabelText("Reason").fill("   ");
    await expect.element(button).toBeDisabled();
  });

  it("is not triggered by Enter in a field", async () => {
    const calls = backend();
    const screen = await render(BackstopControl, { ...base, onchange: vi.fn() });
    await fill(screen, "token", "transformer alarm", "LV10");
    for (const label of [
      "Operator token",
      "Reason",
      /Type the feeder's code/,
      "Export limit while active",
    ]) {
      await screen.getByLabelText(label).click();
      await userEvent.keyboard("{Enter}");
    }
    expect(calls).toEqual([]);
  });

  it("sends the request once, with the token, and reports the result", async () => {
    let release = () => {};
    const calls = backend(() => new Promise<void>((r) => (release = r)));
    const onchange = vi.fn();
    const screen = await render(BackstopControl, { ...base, onchange });
    await fill(screen, "s3cret", " transformer alarm ", "LV10");
    await screen.getByLabelText("Export limit while active").fill("0.5");

    await screen.getByRole("button", { name: "Trigger the backstop" }).click();
    // In flight: the button says so and cannot be pressed again.
    const busy = screen.getByRole("button", { name: "Triggering…" });
    await expect.element(busy).toBeDisabled();
    (busy.element() as HTMLButtonElement).click();
    release();

    await vi.waitFor(() => expect(onchange).toHaveBeenCalledOnce());
    expect(calls).toHaveLength(1);
    expect(calls[0]).toMatchObject({
      method: "create",
      token: "Bearer s3cret",
      body: { backstopEvent: { feederId: "f-1", reason: "transformer alarm", exportLimitW: 500 } },
    });
    expect(toasts.items.map((t) => t.text)).toEqual([
      "Backstop triggered: set export to 0.5 kw for 56 sites.",
    ]);
    // The form is cleared: the next backstop is a new decision.
    await expect.element(screen.getByLabelText("Reason")).toHaveValue("");
    await expect.element(screen.getByLabelText(/Type the feeder's code/)).toHaveValue("");
  });

  it("shows a wrong token beside the token field, and keeps the form", async () => {
    backend(() => {
      throw new ConnectError("no", Code.PermissionDenied);
    });
    const onchange = vi.fn();
    const screen = await render(BackstopControl, { ...base, onchange });
    await fill(screen, "wrong", "transformer alarm", "LV10");
    await screen.getByRole("button", { name: "Trigger the backstop" }).click();

    const field = screen.getByLabelText("Operator token");
    await expect.element(field).toHaveAttribute("aria-invalid", "true");
    await expect
      .element(screen.getByText("That token is not accepted for this action.").first())
      .toBeVisible();
    await expect.element(screen.getByLabelText("Reason")).toHaveValue("transformer alarm");
    await expect
      .element(screen.getByRole("button", { name: "Trigger the backstop" }))
      .toBeEnabled();
    expect(onchange).not.toHaveBeenCalled();
  });

  it("shows any other refusal beside the button", async () => {
    backend(() => {
      throw new ConnectError(
        "a backstop is already active for feeder LV10",
        Code.FailedPrecondition,
      );
    });
    const screen = await render(BackstopControl, { ...base, onchange: vi.fn() });
    await fill(screen, "token", "again", "LV10");
    await screen.getByRole("button", { name: "Trigger the backstop" }).click();
    await expect
      .element(screen.getByRole("alert").first())
      .toHaveTextContent("A backstop is already active for feeder LV10.");
    await expect
      .element(screen.getByLabelText("Operator token"))
      .not.toHaveAttribute("aria-invalid");
  });

  it("treats a limit that is not a number as zero, and one site as one site", async () => {
    backend();
    const screen = await render(BackstopControl, { ...base, sites: 1, onchange: vi.fn() });
    await screen.getByLabelText("Export limit while active").fill("-3");
    await expect
      .element(screen.getByText("This will: Set export to 0.0 kW for 1 site."))
      .toBeVisible();
  });
});

describe("clearing a backstop", () => {
  const active = create(BackstopEventSchema, {
    id: "b-7",
    reason: "transformer alarm",
    exportLimitW: 0,
    triggeredAt: timestamp(Date.UTC(2026, 10, 3, 2, 30) / 1000),
  });

  it("says that one is active, since when and why, and is one button away", async () => {
    const calls = backend();
    const onchange = vi.fn();
    const screen = await render(BackstopControl, { ...base, active, onchange });
    await expect.element(screen.getByText("A backstop is active.")).toBeVisible();
    await expect
      .element(screen.getByText(/since Tue 3 Nov, 13:30\. Reason: transformer alarm\./))
      .toBeVisible();

    const button = screen.getByRole("button", { name: "Clear the backstop" });
    await expect.element(button).toBeDisabled();
    await screen.getByLabelText("Operator token").fill("s3cret");
    await userEvent.keyboard("{Enter}");
    expect(calls).toEqual([]);
    await button.click();

    await vi.waitFor(() => expect(onchange).toHaveBeenCalledOnce());
    expect(calls).toEqual([
      { method: "clear", token: "Bearer s3cret", body: expect.objectContaining({ id: "b-7" }) },
    ]);
    expect(toasts.items[0]!.text).toBe(
      "Backstop cleared. The engine's envelopes are back in force.",
    );
  });

  it("shows a wrong token beside the token field", async () => {
    backend(() => {
      throw new ConnectError("no", Code.Unauthenticated);
    });
    const screen = await render(BackstopControl, { ...base, active, onchange: vi.fn() });
    await screen.getByLabelText("Operator token").fill("wrong");
    await screen.getByRole("button", { name: "Clear the backstop" }).click();
    await expect
      .element(screen.getByLabelText("Operator token"))
      .toHaveAttribute("aria-invalid", "true");
    await expect
      .element(screen.getByText("The operator token is missing or not valid.").first())
      .toBeVisible();
  });
});
