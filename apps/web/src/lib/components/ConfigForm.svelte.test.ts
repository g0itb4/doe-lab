import { create } from "@bufbuild/protobuf";
import { Code, ConnectError, createRouterTransport, type Transport } from "@connectrpc/connect";
import { EnvelopePolicy } from "@doelab/gen/doelab/v1/common_pb.js";
import {
  EnvelopeConfigSchema,
  EnvelopeConfigService,
  type CreateEnvelopeConfigRequest,
} from "@doelab/gen/doelab/v1/envelope_config_pb.js";
import { afterEach, describe, expect, it, vi } from "vitest";
import { render } from "vitest-browser-svelte";
import { operator } from "$lib/operator.svelte.ts";
import { toasts } from "$lib/toast.svelte.ts";
import ConfigForm from "./ConfigForm.svelte";

let router: Transport;
vi.mock("@connectrpc/connect-web", async () => {
  const { transportStub } = await import("$lib/transport.test-utils.ts");
  return transportStub(() => router);
});

const active = create(EnvelopeConfigSchema, {
  id: "c-3",
  feederId: "f-1",
  version: 3,
  policy: EnvelopePolicy.EQUAL,
  vMinPu: 0.94,
  vMaxPu: 1.1,
  transformerLimitPct: 100,
  lineLimitPct: 100,
  pvScale: 3,
  staticLimitW: 5000,
  intervalMinutes: 30,
  horizonIntervals: 48,
  breachGraceSeconds: 60,
  offlineAfterSeconds: 300,
});

type Call = { token: string | null; request: CreateEnvelopeConfigRequest };
function backend(fail?: ConnectError) {
  const calls: Call[] = [];
  router = createRouterTransport(({ service }) => {
    service(EnvelopeConfigService, {
      createEnvelopeConfig: (request, ctx) => {
        calls.push({ token: ctx.requestHeader.get("Authorization"), request });
        if (fail) throw fail;
        return { envelopeConfig: create(EnvelopeConfigSchema, { id: "c-4", version: 4 }) };
      },
    });
  });
  return calls;
}

afterEach(() => {
  operator.token = "";
  toasts.clear();
});

const props = (onsaved = vi.fn()) => ({ active, nominalV: 230, onsaved });

describe("ConfigForm", () => {
  it("starts from the version in force, in volts and kilowatts, each field with a label and a unit", async () => {
    backend();
    const screen = await render(ConfigForm, props());
    await expect.element(screen.getByLabelText("Highest voltage allowed")).toHaveValue(253);
    await expect.element(screen.getByLabelText("Lowest voltage allowed")).toHaveValue(216.2);
    await expect.element(screen.getByLabelText("Fixed limit to compare with")).toHaveValue(5);
    await expect.element(screen.getByLabelText("Interval")).toHaveValue("30");
    await expect
      .element(screen.getByLabelText("How headroom is shared"))
      .toHaveValue(String(EnvelopePolicy.EQUAL));
    // Every input has a label; every number has a unit beside it.
    for (const input of screen.container.querySelectorAll("input, select, textarea")) {
      expect(input.id, "an input without an id").not.toBe("");
      expect(
        screen.container.querySelector(`label[for="${input.id}"]`),
        `a label for ${input.id}`,
      ).not.toBeNull();
    }
    for (const input of screen.container.querySelectorAll('input[type="number"]')) {
      expect(input.nextElementSibling?.textContent?.trim(), `a unit for ${input.id}`).toMatch(
        /^(V|%|×|kW|intervals|s)$/,
      );
    }
  });

  it("validates a field when the reader leaves it, with the API's rule, and says what to enter", async () => {
    backend();
    const screen = await render(ConfigForm, props());
    const field = screen.getByLabelText("Highest voltage allowed");
    await field.fill("300");
    // Not while typing.
    await expect.element(field).not.toHaveAttribute("aria-invalid");
    await screen.getByLabelText("Solar scale").click();
    await expect.element(field).toHaveAttribute("aria-invalid", "true");
    const help = screen.container.querySelector("#config-vMaxV-help")!;
    expect(help.textContent?.trim()).toBe("Enter from 184 to 276 V.");
    expect(field.element().getAttribute("aria-describedby")).toBe("config-vMaxV-help");

    // Fixed: the hint comes back.
    await field.fill("250");
    await expect.element(field).not.toHaveAttribute("aria-invalid");
    expect(help.textContent?.trim()).toBe("At any customer, phase to neutral.");
  });

  it("previews what a save would change", async () => {
    backend();
    const screen = await render(ConfigForm, props());
    const save = screen.getByRole("button", { name: "Save as version 4" });
    await expect.element(screen.getByRole("heading", { name: "No changes yet" })).toBeVisible();
    await expect.element(save).toBeDisabled();

    await screen.getByLabelText("Highest voltage allowed").fill("250");
    await screen
      .getByLabelText("How headroom is shared")
      .selectOptions(String(EnvelopePolicy.PROPORTIONAL));
    await screen.getByLabelText("Interval").selectOptions("15");
    await expect
      .element(screen.getByRole("heading", { name: "3 changes from version 3" }))
      .toBeVisible();
    const lines = [...screen.container.querySelectorAll("section li")].map((li) =>
      li.textContent?.replace(/\s+/g, " ").trim(),
    );
    expect(lines).toEqual([
      "How headroom is shared: from Equal: every site gets the same limit to Proportional: in step with each site's connection limit",
      "Interval: from 30 min to 15 min",
      "Highest voltage allowed: from 253 V to 250 V",
    ]);
    await expect.element(save).toBeEnabled();

    await screen.getByRole("button", { name: "Discard changes" }).click();
    await expect.element(screen.getByRole("heading", { name: "No changes yet" })).toBeVisible();
    await expect.element(screen.getByLabelText("Highest voltage allowed")).toHaveValue(253);
  });

  it("does not save a form with an error: it shows every error and goes to the first", async () => {
    const calls = backend();
    operator.token = "s3cret";
    const screen = await render(ConfigForm, props());
    await screen.getByLabelText("Solar scale").fill("99");
    await screen.getByLabelText("Silence before an offline alert").fill("");
    await screen.getByRole("button", { name: "Save as version 4" }).click();

    await expect.element(screen.getByText("Enter from 0 to 20.")).toBeVisible();
    await expect.element(screen.getByText("Enter a number: from 10 to 86400 s.")).toBeVisible();
    await vi.waitFor(() => expect(document.activeElement?.id).toBe("config-pvScale"));
    expect(calls).toEqual([]);
  });

  it("asks for the token before it calls the API", async () => {
    const calls = backend();
    const screen = await render(ConfigForm, props());
    await screen.getByLabelText("Solar scale").fill("2.5");
    await screen.getByRole("button", { name: "Save as version 4" }).click();
    await expect.element(screen.getByText("Enter the operator token to save.")).toBeVisible();
    await expect
      .element(screen.getByLabelText("Operator token"))
      .toHaveAttribute("aria-invalid", "true");
    expect(calls).toEqual([]);
  });

  it("saves a new version in the API's units, with the token, and says what happens next", async () => {
    const calls = backend();
    const onsaved = vi.fn();
    const screen = await render(ConfigForm, props(onsaved));
    await screen.getByLabelText("Operator token").fill("s3cret");
    await screen.getByLabelText("Highest voltage allowed").fill("250");
    await screen.getByLabelText("Fixed limit to compare with").fill("3.5");
    await screen.getByLabelText("Note for this version").fill(" tighter band for summer ");
    await screen.getByRole("button", { name: "Save as version 4" }).click();

    await vi.waitFor(() => expect(onsaved).toHaveBeenCalledOnce());
    expect(calls).toHaveLength(1);
    expect(calls[0]!.token).toBe("Bearer s3cret");
    const sent = calls[0]!.request.envelopeConfig!;
    expect(sent).toMatchObject({
      feederId: "f-1",
      id: "",
      version: 0,
      policy: EnvelopePolicy.EQUAL,
      staticLimitW: 3500,
      intervalMinutes: 30,
      note: "tighter band for summer",
    });
    expect(sent.vMaxPu).toBeCloseTo(250 / 230, 12);
    expect(onsaved.mock.calls[0]![0]).toMatchObject({ id: "c-4", version: 4 });
    expect(toasts.items.map((t) => t.text)).toEqual([
      "Saved as version 4. The next engine run uses it.",
    ]);
  });

  it("starts again from a new version in force", async () => {
    backend();
    const screen = await render(ConfigForm, props());
    await screen.getByLabelText("Solar scale").fill("99");
    await screen.getByLabelText("Interval").click();
    await expect.element(screen.getByText("Enter from 0 to 20.")).toBeVisible();

    await screen.rerender({
      active: create(EnvelopeConfigSchema, { ...active, id: "c-4", version: 4, pvScale: 2 }),
    });
    await expect.element(screen.getByLabelText("Solar scale")).toHaveValue(2);
    await expect.element(screen.getByRole("button", { name: "Save as version 5" })).toBeDisabled();
    expect(screen.getByText("Enter from 0 to 20.").elements()).toHaveLength(0);
  });

  it("shows a refused token beside the token field", async () => {
    backend(new ConnectError("no", Code.PermissionDenied));
    const onsaved = vi.fn();
    const screen = await render(ConfigForm, props(onsaved));
    await screen.getByLabelText("Operator token").fill("wrong");
    await screen.getByLabelText("Solar scale").fill("2.5");
    await screen.getByRole("button", { name: "Save as version 4" }).click();
    await expect
      .element(screen.getByLabelText("Operator token"))
      .toHaveAttribute("aria-invalid", "true");
    await expect
      .element(screen.getByText("That token is not accepted for this action.").first())
      .toBeVisible();
    // What was typed is kept.
    await expect.element(screen.getByLabelText("Solar scale")).toHaveValue(2.5);
    expect(onsaved).not.toHaveBeenCalled();
  });

  it("shows any other refusal beside the button", async () => {
    backend(new ConnectError("v_min_pu must be below v_max_pu", Code.InvalidArgument));
    const screen = await render(ConfigForm, props());
    await screen.getByLabelText("Operator token").fill("s3cret");
    await screen.getByLabelText("Solar scale").fill("2.5");
    await screen.getByRole("button", { name: "Save as version 4" }).click();
    await expect
      .element(screen.getByRole("alert").first())
      .toHaveTextContent("V_min_pu must be below v_max_pu.");
  });

  it("checks the note when the reader leaves it", async () => {
    backend();
    const screen = await render(ConfigForm, props());
    const note = screen.getByLabelText("Note for this version");
    await note.fill("x".repeat(2001));
    await screen.getByLabelText("Solar scale").click();
    await expect.element(note).toHaveAttribute("aria-invalid", "true");
    await expect
      .element(screen.getByText("Shorten the note to 2000 characters or fewer."))
      .toBeVisible();
  });
});
