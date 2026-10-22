import { describe, expect, it } from "vitest";
import { revealLabelFor } from "./runtime";

describe("revealLabelFor", () => {
  it("names Finder on macOS, the folder on Linux, and Finder until the version has loaded", () => {
    expect(revealLabelFor("macos")).toBe("Show in Finder");
    expect(revealLabelFor("linux")).toBe("Show in folder");
    expect(revealLabelFor(undefined)).toBe("Show in Finder");
  });
});
