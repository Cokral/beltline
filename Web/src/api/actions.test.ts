import { describe, expect, it } from "vitest";
import { isActionEnabled } from "./actions";

describe("isActionEnabled", () => {
  const actions = [
    { id: "save", enabled: true },
    { id: "reset", enabled: false }
  ];

  it("offers an action the daemon enables", () => {
    expect(isActionEnabled(actions, "save")).toBe(true);
  });

  it("does not offer an action the daemon disables", () => {
    expect(isActionEnabled(actions, "reset")).toBe(false);
  });

  it("does not offer an action the daemon did not list", () => {
    expect(isActionEnabled(actions, "archive")).toBe(false);
    expect(isActionEnabled([], "save")).toBe(false);
  });
});
