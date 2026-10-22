import { describe, expect, it } from "vitest";
import { formatContextTokens, formatDate, formatDateTime, formatDuration, formatMoney, formatTime } from "./format";

const fixed = { locale: "en-US", timeZone: "America/New_York" };

describe("dates and times", () => {
  it.each([null, undefined, "", "not a date", Number.NaN])("shows an em dash for %j", value => {
    expect(formatDateTime(value as null, fixed)).toBe("—");
    expect(formatDate(value as null, fixed)).toBe("—");
    expect(formatTime(value as null, fixed)).toBe("—");
  });

  it("formats midnight in the requested time zone", () => {
    expect(formatTime("2026-10-07T04:00:00Z", fixed)).toBe("12:00 AM");
    expect(formatDateTime("2026-10-07T04:00:00Z", fixed)).toBe("Oct 7, 2026, 12:00 AM");
  });

  it("formats both sides of the daylight-saving change in America/New_York on 2026-03-08", () => {
    expect(formatTime("2026-03-08T06:59:59Z", fixed)).toBe("1:59 AM");
    expect(formatTime("2026-03-08T07:00:00Z", fixed)).toBe("3:00 AM");
  });

  it("accepts a Date and an epoch number as well as an ISO string", () => {
    const iso = "2026-10-07T07:00:00Z";
    expect(formatTime(new Date(iso), fixed)).toBe(formatTime(iso, fixed));
    expect(formatTime(Date.parse(iso), fixed)).toBe(formatTime(iso, fixed));
  });

  it("uses the browser locale and time zone when none is given", () => {
    const expected = new Intl.DateTimeFormat(undefined, { timeStyle: "short" }).format(new Date("2026-10-07T07:00:00Z"));
    expect(formatTime("2026-10-07T07:00:00Z")).toBe(expected);
  });
});

describe("durations match Status.duration", () => {
  it.each([
    [-1, "0s"],
    [0, "0s"],
    [59.4, "59s"],
    [59.5, "1m 0s"],
    [60, "1m 0s"],
    [125, "2m 5s"],
    [3599.5, "1h 0m"],
    [3600, "1h 0m"],
    [7325, "2h 2m"]
  ])("formats %d seconds as %s", (seconds, expected) => {
    expect(formatDuration(seconds)).toBe(expected);
  });

  it("shows an em dash for nonfinite and absent durations", () => {
    expect(formatDuration(Number.NaN)).toBe("—");
    expect(formatDuration(Number.POSITIVE_INFINITY)).toBe("—");
    expect(formatDuration(null)).toBe("—");
    expect(formatDuration(undefined)).toBe("—");
  });
});

describe("money matches Cost.dollars", () => {
  it.each([
    [0, "$0.00"],
    [4_000_000, "$0.0040"],
    [9_999_999, "$0.0100"],
    [10_000_000, "$0.01"],
    [1_234_560_000_000, "$1,234.56"],
    [25_000_000_000, "$25.00"]
  ])("formats %d nanodollars as %s", (nanos, expected) => {
    expect(formatMoney(nanos)).toBe(expected);
  });

  it("shows cost not tracked for an unknown amount, never zero", () => {
    expect(formatMoney(null)).toBe("cost not tracked");
    expect(formatMoney(undefined)).toBe("cost not tracked");
  });

  it("shows an em dash for a nonfinite amount", () => {
    expect(formatMoney(Number.NaN)).toBe("—");
    expect(formatMoney(Number.POSITIVE_INFINITY)).toBe("—");
  });
});

describe("context sizes match ContextSize.label", () => {
  it.each([
    [999, "999"],
    [1500, "1.5k"],
    [250000, "250k"],
    [1000000, "1M"],
    [1048576, "1.048576M"]
  ])("formats %d tokens as %s", (tokens, expected) => {
    expect(formatContextTokens(tokens)).toBe(expected);
  });
});
