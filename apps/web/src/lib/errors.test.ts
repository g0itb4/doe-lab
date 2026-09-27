import { Code, ConnectError } from "@connectrpc/connect";
import { describe, expect, it } from "vitest";
import { describeError, isAbort } from "./errors.ts";

describe("an error in plain words", () => {
  it.each([
    [Code.Unavailable, "The API cannot be reached"],
    [Code.DeadlineExceeded, "The API cannot be reached"],
    [Code.Unauthenticated, "token is missing or not valid"],
    [Code.PermissionDenied, "token is not accepted"],
    [Code.NotFound, "Not found"],
    [Code.ResourceExhausted, "Too many requests"],
    [Code.Internal, "Something went wrong on the server"],
  ])("code %s", (code, text) => {
    expect(describeError(new ConnectError("pq: relation does not exist", code))).toContain(text);
  });

  it("never shows the server's internals", () => {
    expect(
      describeError(new ConnectError("pq: relation does not exist", Code.Internal)),
    ).not.toContain("pq");
  });

  it("treats a failed fetch as the API being out of reach", () => {
    expect(describeError(new TypeError("Failed to fetch"))).toContain("The API cannot be reached");
  });

  it("passes on the API's own sentence for a refused request, as a sentence", () => {
    expect(
      describeError(new ConnectError("v_max_pu must be above v_min_pu", Code.InvalidArgument)),
    ).toBe("V_max_pu must be above v_min_pu.");
    expect(
      describeError(new ConnectError("a backstop is already active.", Code.FailedPrecondition)),
    ).toBe("A backstop is already active.");
    expect(describeError(new ConnectError("  ", Code.AlreadyExists))).toBe(
      "The request was refused.",
    );
  });

  it("knows a call that was abandoned on purpose", () => {
    expect(isAbort(new ConnectError("gone", Code.Canceled))).toBe(true);
    expect(isAbort(new ConnectError("gone", Code.Unavailable))).toBe(false);
  });
});
