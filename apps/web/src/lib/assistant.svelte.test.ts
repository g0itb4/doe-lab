import { Code, ConnectError, type Transport } from "@connectrpc/connect";
import { create } from "@bufbuild/protobuf";
import {
  AnswerEnd,
  AssistantLookupSchema,
  AssistantTool,
  AssistantUnavailable,
} from "@doelab/gen/doelab/v1/assistant_pb.js";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { assistant, describeLookup, SPENT } from "./assistant.svelte.ts";
import { assistantBackend, type Backend } from "./assistant.test-utils.ts";

let router: Transport;
vi.mock("@connectrpc/connect-web", async () => {
  const { transportStub } = await import("$lib/transport.test-utils.ts");
  return transportStub(() => router);
});

let backend: Backend;
beforeEach(() => {
  backend = assistantBackend();
  router = backend.router;
});
afterEach(() => {
  assistant.clear();
  assistant.availability = "unknown";
  assistant.maxChars = 500;
});

const settle = () => new Promise((r) => setTimeout(r, 0));

describe("whether the assistant can be asked", () => {
  it("is not known until the server has said", () => {
    expect(assistant.availability).toBe("unknown");
  });

  it("is what the server says: available, spent, or off", async () => {
    backend.status = { available: true, maxQuestionChars: 300 };
    await assistant.check();
    expect(assistant.availability).toBe("available");
    expect(assistant.maxChars).toBe(300);

    backend.status = { available: false, unavailable: AssistantUnavailable.BUDGET_SPENT };
    await assistant.check();
    expect(assistant.availability).toBe("spent");
    // A server that names no bound gets the default.
    expect(assistant.maxChars).toBe(500);

    backend.status = { available: false, unavailable: AssistantUnavailable.OFF };
    await assistant.check();
    expect(assistant.availability).toBe("off");
  });

  it("is off when the server cannot say", async () => {
    backend.status = new ConnectError("no such service", Code.Unimplemented);
    await assistant.check();
    expect(assistant.availability).toBe("off");
  });
});

describe("an answer", () => {
  it("forms from the steps, in order: lookups, text, and the end", async () => {
    backend.scripts.push([
      {
        lookup: {
          tool: AssistantTool.GET_BINDING_CONSTRAINT,
          subject: "XDLAB000014 at 12:30 on 10 Nov",
          found: true,
        },
      },
      { text: "Voltage " },
      { text: "at XDLAB000022." },
      { end: AnswerEnd.COMPLETE },
    ]);
    await assistant.ask("LV10", "Why is this site limited?", "XDLAB000014");

    expect(backend.asked).toHaveLength(1);
    expect(backend.asked[0]).toMatchObject({
      feederCode: "LV10",
      question: "Why is this site limited?",
      nmi: "XDLAB000014",
    });
    expect(assistant.asking).toBe(false);
    expect(assistant.answer).toEqual({
      question: "Why is this site limited?",
      text: "Voltage at XDLAB000022.",
      lookups: [{ label: "What limits XDLAB000014 at 12:30 on 10 Nov", found: true }],
      done: true,
      note: undefined,
    });
  });

  it("says so when it did not end well", async () => {
    for (const [end, note] of [
      [AnswerEnd.CUT_SHORT, "The answer reached its length limit and stops here."],
      [AnswerEnd.DECLINED, "The assistant declined to answer that."],
      [
        AnswerEnd.TOO_MANY_LOOKUPS,
        "The question needed more lookups than one answer may make. Ask something narrower.",
      ],
    ] as const) {
      backend.scripts.push([{ text: "Well" }, { end }]);
      await assistant.ask("LV10", "?");
      expect(assistant.answer).toMatchObject({ text: "Well", done: true, note });
    }
  });

  it("is not done when the stream ends with no end", async () => {
    backend.scripts.push([{ text: "Half an" }]);
    await assistant.ask("LV10", "?");
    expect(assistant.answer).toMatchObject({ text: "Half an", done: false });
    expect(assistant.asking).toBe(false);
  });

  it("describes each lookup in words", () => {
    const lookup = (tool: AssistantTool, subject: string, found = true) =>
      describeLookup(create(AssistantLookupSchema, { tool, subject, found }));
    expect(lookup(AssistantTool.GET_ENVELOPE, "XDLAB000014 at 12:30 on 10 Nov")).toEqual({
      label: "Envelope of XDLAB000014 at 12:30 on 10 Nov",
      found: true,
    });
    expect(lookup(AssistantTool.LIST_BREACHES, "open alerts of XDLAB000022").label).toBe(
      "Open alerts of XDLAB000022",
    );
    expect(lookup(AssistantTool.GET_CONFIG, "config version 2").label).toBe("Config version 2");
    expect(lookup(AssistantTool.GET_ENVELOPE, "", false)).toEqual({
      label: "Envelope of : nothing found",
      found: false,
    });
    expect(lookup(AssistantTool.LIST_BREACHES, "", false).label).toBe(": nothing found");
    expect(lookup(AssistantTool.UNSPECIFIED, "clear_backstop", false)).toEqual({
      label: "A lookup that does not exist: clear_backstop: nothing found",
      found: false,
    });
  });
});

describe("a question that is not answered", () => {
  const failing = (code: Code) => [new ConnectError("the server's own words", code)];

  it("says to wait when the asker has asked too much", async () => {
    await assistant.check();
    backend.scripts.push(failing(Code.ResourceExhausted));
    await assistant.ask("LV10", "?");
    expect(assistant.answer?.error).toBe(
      "That is a lot of questions. Wait a minute and ask again.",
    );
    expect(assistant.availability).toBe("available");
    expect(assistant.asking).toBe(false);
  });

  it("says when it is back when the day's budget is spent", async () => {
    await assistant.check();
    backend.scripts.push(failing(Code.ResourceExhausted));
    backend.status = { available: false, unavailable: AssistantUnavailable.BUDGET_SPENT };
    await assistant.ask("LV10", "?");
    expect(assistant.answer?.error).toBe(SPENT);
    expect(assistant.availability).toBe("spent");
  });

  it("explains every other refusal without the server's words", async () => {
    for (const [code, message] of [
      [Code.FailedPrecondition, "The assistant is off on this server."],
      [Code.NotFound, "The assistant cannot find that feeder or site. It may have been removed."],
      [Code.InvalidArgument, "Ask a question of at most 500 characters."],
      [Code.Unavailable, "The assistant could not answer just now. Try again."],
      [Code.Internal, "The assistant could not answer just now. Try again."],
    ] as const) {
      backend.scripts.push(failing(code));
      await assistant.ask("LV10", "?");
      expect(assistant.answer?.error).toBe(message);
    }
  });

  it("is off once the server has said that it is", async () => {
    await assistant.check();
    backend.scripts.push(failing(Code.FailedPrecondition));
    await assistant.ask("LV10", "?");
    expect(assistant.availability).toBe("off");
  });

  it("keeps what arrived before the failure", async () => {
    backend.scripts.push([{ text: "So far" }, ...failing(Code.Unavailable)]);
    await assistant.ask("LV10", "?");
    expect(assistant.answer).toMatchObject({ text: "So far", done: false });
    expect(assistant.answer?.error).toBeDefined();
  });
});

describe("stopping", () => {
  it("abandons the answer in flight and keeps what has arrived", async () => {
    backend.scripts.push([{ text: "One moment" }, "hold", { text: ", never sent" }]);
    const asked = assistant.ask("LV10", "?");
    await vi.waitFor(() => expect(assistant.answer?.text).toBe("One moment"));
    expect(assistant.asking).toBe(true);

    assistant.stop();
    expect(assistant.asking).toBe(false);
    await asked;
    await settle();
    expect(assistant.answer).toMatchObject({ text: "One moment", done: false, note: "Stopped." });
    expect(assistant.answer?.error).toBeUndefined();
  });

  it("does nothing when nothing is in flight", async () => {
    backend.scripts.push([{ text: "Done." }, { end: AnswerEnd.COMPLETE }]);
    await assistant.ask("LV10", "?");
    assistant.stop();
    expect(assistant.answer?.note).toBeUndefined();
  });

  it("is what a new question does to the one before it", async () => {
    backend.scripts.push(
      [{ text: "The first" }, "hold"],
      [{ text: "The second" }, { end: AnswerEnd.COMPLETE }],
    );
    const first = assistant.ask("LV10", "first?");
    await vi.waitFor(() => expect(assistant.answer?.text).toBe("The first"));
    await assistant.ask("LV10", "second?");
    await first;
    expect(assistant.answer).toMatchObject({ question: "second?", text: "The second", done: true });
    expect(assistant.asking).toBe(false);
  });

  it("clears the answer, in flight or not", async () => {
    backend.scripts.push([{ text: "One moment" }, "hold"]);
    const asked = assistant.ask("LV10", "?");
    await vi.waitFor(() => expect(assistant.answer?.text).toBe("One moment"));
    assistant.clear();
    await asked;
    expect(assistant.answer).toBeUndefined();
    expect(assistant.asking).toBe(false);
  });
});
