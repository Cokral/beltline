import { render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { apiRequest } from "../api/client";
import { ContractError, NetworkError, SignInRequiredError, ValidationError } from "../api/errors";

import { ErrorProblems, ProblemList } from "./ProblemList";
import { goldenText } from "../test/golden";
import type { ErrorResponse, VersionResponse } from "../api/wire.gen";

/** Runs one request against a canned response, the way the shell does. */
async function failWith(response: () => Response | Promise<Response>, options: { onUnauthorized?: () => void } = {}): Promise<unknown> {
  const fetcher = vi.fn<typeof fetch>(async () => response());
  try {
    await apiRequest<VersionResponse>("/api/v1/pause", { method: "POST", body: {}, fetcher, onUnauthorized: options.onUnauthorized });
  } catch (error) {
    return error;
  }
  throw new Error("the request unexpectedly succeeded");
}

const status = (body: string, code: number) => () => new Response(body, { status: code, headers: { "content-type": "application/json" } });

describe("422 handling", () => {
  it("shows the daemon message from error-failure.json with no invented problems", async () => {
    const error = await failWith(status(goldenText("error-failure.json"), 422));
    expect(error).toBeInstanceOf(ValidationError);
    expect((error as ValidationError).problems).toEqual([]);
    render(<ErrorProblems error={error} />);
    expect(screen.getByRole("alert").textContent).toBe("The branch is gone");
    expect(screen.queryByRole("list")).toBeNull();
  });

  it("shows every problem from error-problems.json in the order the daemon sent them", async () => {
    const error = await failWith(status(goldenText("error-problems.json"), 422));
    render(<ErrorProblems error={error} />);
    const alert = screen.getByRole("alert");
    const items = within(alert).getAllByRole("listitem").map(item => item.textContent);
    expect(items).toEqual(["belt a has no stages", "agent b is missing"]);
  });

  it("keeps duplicate and empty problems exactly as they arrived", () => {
    const parsed: ErrorResponse = { message: "Rejected", problems: ["same", "same", ""] };
    render(<ProblemList message={parsed.message} problems={parsed.problems} />);
    const items = screen.getAllByRole("listitem").map(item => item.textContent);
    expect(items).toEqual(["same", "same", ""]);
  });

  it("falls back to the rejected-request message for a malformed JSON body", async () => {
    const error = await failWith(() => new Response("{not json", { status: 422 }));
    expect(error).toBeInstanceOf(ValidationError);
    expect((error as ValidationError).message).toBe("The daemon rejected this request.");
    expect((error as ValidationError).problems).toEqual([]);
  });

  it("falls back to the same message for a 422 body that is not an error response", async () => {
    const error = await failWith(status('{"message":3,"problems":"nope"}', 422));
    expect((error as ValidationError).message).toBe("The daemon rejected this request.");
    expect((error as ValidationError).problems).toEqual([]);
  });

  it("shows text with markup as text, never as HTML", () => {
    render(<ProblemList message={'<img src="x" onerror="alert(1)">Denied'} problems={["<b>bold</b>"]} />);
    expect(document.querySelector("img")).toBeNull();
    expect(document.querySelector("b")).toBeNull();
    expect(screen.getByText('<img src="x" onerror="alert(1)">Denied')).toBeTruthy();
    expect(screen.getByText("<b>bold</b>")).toBeTruthy();
  });
});

describe("other failures", () => {
  it("passes a 401 to sign-in and reports sign-in rather than a message", async () => {
    const onUnauthorized = vi.fn();
    const error = await failWith(status(goldenText("error-unauthorized.json"), 401), { onUnauthorized });
    expect(error).toBeInstanceOf(SignInRequiredError);
    expect(onUnauthorized).toHaveBeenCalledTimes(1);
  });

  it("reports a network failure as a visible, retryable kind", async () => {
    const error = await failWith(() => { throw new TypeError("offline"); });
    expect(error).toBeInstanceOf(NetworkError);
    expect((error as NetworkError).kind).toBe("network");
  });

  it("reports a non-JSON success body as an invalid-JSON contract failure", async () => {
    const error = await failWith(() => new Response("<html>", { status: 200 }));
    expect(error).toBeInstanceOf(ContractError);
    expect((error as ContractError).message).toMatch(/invalid JSON/);
  });
});
