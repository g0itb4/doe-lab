import { ConnectError, createRouterTransport, type Transport } from "@connectrpc/connect";
import {
  AssistantService,
  type AnswerEnd,
  type AskRequest,
  type AssistantTool,
  type AssistantUnavailable,
} from "@doelab/gen/doelab/v1/assistant_pb.js";

// One step of a scripted answer: a piece of text, a lookup, an end, a
// failure, or "hold", which waits until the test lets go (or the asker
// leaves).
export type ScriptStep =
  | { text: string }
  | { lookup: { tool: AssistantTool; subject: string; found: boolean } }
  | { end: AnswerEnd }
  | ConnectError
  | "hold";

export type Backend = {
  router: Transport;
  // Every question the server was asked.
  asked: AskRequest[];
  // What the status call answers, or fails with.
  status:
    | { available: boolean; unavailable?: AssistantUnavailable; maxQuestionChars?: number }
    | ConnectError;
  // The scripts of the answers to come, in order.
  scripts: ScriptStep[][];
  // Lets every held answer go on.
  release: () => void;
};

// An assistant service, in memory, that plays scripts.
export function assistantBackend(): Backend {
  let release = () => {};
  let held = new Promise<void>((r) => (release = r));
  const backend: Backend = {
    asked: [],
    status: { available: true, maxQuestionChars: 500 },
    scripts: [],
    release: () => {
      release();
      held = new Promise<void>((r) => (release = r));
    },
    router: createRouterTransport(({ service }) => {
      service(AssistantService, {
        getAssistantStatus: () => {
          if (backend.status instanceof ConnectError) throw backend.status;
          return backend.status;
        },
        async *ask(req, ctx) {
          backend.asked.push(req);
          for (const step of backend.scripts.shift() ?? []) {
            if (step instanceof ConnectError) throw step;
            if (step === "hold") {
              await Promise.race([
                held,
                new Promise<void>((r) => ctx.signal.addEventListener("abort", () => r())),
              ]);
              ctx.signal.throwIfAborted();
            } else if ("text" in step) yield { step: { case: "text" as const, value: step.text } };
            else if ("lookup" in step)
              yield { step: { case: "lookup" as const, value: step.lookup } };
            else yield { step: { case: "end" as const, value: step.end } };
          }
        },
      });
    }),
  };
  return backend;
}
