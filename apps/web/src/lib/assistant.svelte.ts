import { Code, ConnectError } from "@connectrpc/connect";
import {
  AnswerEnd,
  AssistantTool,
  AssistantUnavailable,
  type AssistantLookup,
} from "@doelab/gen/doelab/v1/assistant_pb.js";
import { api } from "./api.ts";
import { isAbort } from "./errors.ts";

// Whether a question would be taken: not known yet, yes, no because the
// server has no assistant, or no because the day's budget is spent.
export type Availability = "unknown" | "available" | "off" | "spent";

// One lookup the assistant made, in words.
export type Lookup = { label: string; found: boolean };

// An answer as it forms. `done` is set when the server said how it ended;
// `note` says so when it did not end well; `error` when it did not end.
export type Answer = {
  question: string;
  text: string;
  lookups: Lookup[];
  done: boolean;
  note?: string;
  error?: string;
};

export const SPENT = "The assistant has answered all it can today. It is back at midnight UTC.";

const END_NOTES: Record<AnswerEnd, string | undefined> = {
  [AnswerEnd.UNSPECIFIED]: undefined,
  [AnswerEnd.COMPLETE]: undefined,
  [AnswerEnd.CUT_SHORT]: "The answer reached its length limit and stops here.",
  [AnswerEnd.DECLINED]: "The assistant declined to answer that.",
  [AnswerEnd.TOO_MANY_LOOKUPS]:
    "The question needed more lookups than one answer may make. Ask something narrower.",
};

const capital = (s: string) => (s === "" ? s : s[0]!.toUpperCase() + s.slice(1));

// What a lookup was, for the list above an answer.
export function describeLookup(lookup: AssistantLookup): Lookup {
  const label = {
    [AssistantTool.UNSPECIFIED]: `A lookup that does not exist: ${lookup.subject}`,
    [AssistantTool.GET_ENVELOPE]: `Envelope of ${lookup.subject}`,
    [AssistantTool.GET_BINDING_CONSTRAINT]: `What limits ${lookup.subject}`,
    [AssistantTool.LIST_BREACHES]: capital(lookup.subject),
    [AssistantTool.GET_CONFIG]: capital(lookup.subject),
  }[lookup.tool];
  return { label: lookup.found ? label : `${label}: nothing found`, found: lookup.found };
}

// The assistant as the drawer sees it: whether it can be asked, and the one
// answer on screen. A question stands alone; the server keeps no
// conversation, so neither does this.
class Assistant {
  availability = $state<Availability>("unknown");
  maxChars = $state(500);
  answer = $state<Answer>();
  asking = $state(false);
  #inFlight: AbortController | undefined;

  // Asks the server whether it has an assistant. A server that cannot say
  // has none, as far as the page is concerned.
  async check(): Promise<void> {
    try {
      const status = await api.assistant.getAssistantStatus({});
      this.maxChars = status.maxQuestionChars || 500;
      if (status.available) this.availability = "available";
      else
        this.availability =
          status.unavailable === AssistantUnavailable.BUDGET_SPENT ? "spent" : "off";
    } catch {
      this.availability = "off";
    }
  }

  // Asks a question and fills `answer` as the steps arrive. A question asked
  // while another is in flight replaces it.
  async ask(feederCode: string, question: string, nmi?: string): Promise<void> {
    this.#inFlight?.abort();
    const controller = new AbortController();
    this.#inFlight = controller;
    this.answer = { question, text: "", lookups: [], done: false };
    const answer = this.answer;
    this.asking = true;
    try {
      const steps = api.assistant.ask({ feederCode, question, nmi }, { signal: controller.signal });
      for await (const { step } of steps) {
        if (step.case === "text") answer.text += step.value;
        else if (step.case === "lookup") answer.lookups.push(describeLookup(step.value));
        else if (step.case === "end") {
          answer.note = END_NOTES[step.value];
          answer.done = true;
        }
      }
    } catch (e) {
      if (controller.signal.aborted || isAbort(e)) return;
      answer.error = await this.#explain(e);
    } finally {
      if (this.#inFlight === controller) {
        this.#inFlight = undefined;
        this.asking = false;
      }
    }
  }

  // Why a question was not answered, in words the asker can act on.
  async #explain(e: unknown): Promise<string> {
    switch (ConnectError.from(e).code) {
      case Code.ResourceExhausted:
        // One of two rations refused it; the status says which.
        await this.check();
        return this.availability === "spent"
          ? SPENT
          : "That is a lot of questions. Wait a minute and ask again.";
      case Code.FailedPrecondition:
        this.availability = "off";
        return "The assistant is off on this server.";
      case Code.NotFound:
        return "The assistant cannot find that feeder or site. It may have been removed.";
      case Code.InvalidArgument:
        return `Ask a question of at most ${this.maxChars} characters.`;
      default:
        return "The assistant could not answer just now. Try again.";
    }
  }

  // Abandons the answer in flight, and keeps what has arrived.
  stop(): void {
    if (!this.#inFlight) return;
    this.#inFlight.abort();
    this.#inFlight = undefined;
    this.asking = false;
    if (this.answer) this.answer.note = "Stopped.";
  }

  // Forgets the answer: the drawer is back to its empty state.
  clear(): void {
    this.stop();
    this.answer = undefined;
  }
}

export const assistant = new Assistant();
