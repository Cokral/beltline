import { describe, expect, it } from "vitest";
import { snapshotCases } from "./cases";
import { nativeCaptures, parity } from "./parity";

describe("retired native snapshot parity", () => {
  it("gives every native capture exactly one entry", () => {
    expect(parity.map(entry => entry.native).sort()).toEqual([...nativeCaptures].sort());
  });
  it("names a registered case or gives a reason", () => {
    const ids = new Set(snapshotCases.map(item => item.id));
    for (const entry of parity) {
      if (entry.status === "captured") expect(ids.has(entry.caseId), entry.native).toBe(true);
      else expect(entry.reason.trim().length, entry.native).toBeGreaterThan(20);
    }
  });
});
