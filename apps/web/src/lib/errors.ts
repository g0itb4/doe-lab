import { Code, ConnectError } from "@connectrpc/connect";

// What went wrong, in words an operator can act on. Never a stack trace, and
// never the name of an exception.
export function describeError(e: unknown): string {
  const err = ConnectError.from(e);
  switch (err.code) {
    case Code.Unavailable:
    case Code.DeadlineExceeded:
    case Code.Unknown:
      return "The API cannot be reached. Check your connection and try again.";
    case Code.Unauthenticated:
      return "This needs the operator token.";
    case Code.PermissionDenied:
      return "That token is not accepted for this action.";
    case Code.NotFound:
      return "Not found. It may have been removed.";
    case Code.ResourceExhausted:
      return "Too many requests. Wait a moment and try again.";
    case Code.InvalidArgument:
    case Code.FailedPrecondition:
    case Code.AlreadyExists:
      // The API's own sentence: it names the field or the rule.
      return sentence(err.rawMessage);
    default:
      return "Something went wrong on the server. Try again.";
  }
}

function sentence(s: string): string {
  const text = s.trim();
  if (text === "") return "The request was refused.";
  const capital = text[0]!.toUpperCase() + text.slice(1);
  return /[.!?]$/.test(capital) ? capital : capital + ".";
}

// True when a call was abandoned on purpose: a newer one replaced it, or the
// page went away. That is not an error to show.
export function isAbort(e: unknown): boolean {
  return ConnectError.from(e).code === Code.Canceled;
}
