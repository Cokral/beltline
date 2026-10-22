import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { defaultPreferences, preferencesStorageKey } from "./preferences";
import { usePreferences } from "./usePreferences";

afterEach(() => {
  localStorage.clear();
  document.documentElement.removeAttribute("data-theme");
});

describe("usePreferences appearance", () => {
  it("applies a Dark choice to the page and stores it with the other preferences", () => {
    const { result } = renderHook(() => usePreferences());
    expect(result.current.preferences.appearance).toBe("light");
    act(() => result.current.update({ appearance: "dark" }));
    expect(document.documentElement.getAttribute("data-theme")).toBe("dark");
    expect(result.current.preferences.appearance).toBe("dark");
    expect(JSON.parse(localStorage.getItem(preferencesStorageKey)!).appearance).toBe("dark");
  });

  it("keeps the appearance through an unrelated preference update, in storage and on the page", () => {
    const { result } = renderHook(() => usePreferences());
    act(() => result.current.update({ appearance: "dark" }));
    act(() => result.current.update({ floorPinnedBeltId: "issues" }));
    expect(result.current.preferences.floorPinnedBeltId).toBe("issues");
    expect(result.current.preferences.appearance).toBe("dark");
    expect(document.documentElement.getAttribute("data-theme")).toBe("dark");
    expect(JSON.parse(localStorage.getItem(preferencesStorageKey)!)).toMatchObject({ appearance: "dark", floorPinnedBeltId: "issues" });
  });

  it("switches for the visit when storage refuses the write, and keeps it through reads that fail", () => {
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => { throw new Error("denied"); });
    const { result } = renderHook(() => usePreferences());
    act(() => result.current.update({ appearance: "dark" }));
    expect(document.documentElement.getAttribute("data-theme")).toBe("dark");
    expect(result.current.preferences.appearance).toBe("dark");
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => { throw new Error("denied"); });
    act(() => result.current.update({ floorPinnedBeltId: "issues" }));
    expect(result.current.preferences).toMatchObject({ appearance: "dark", floorPinnedBeltId: "issues" });
    expect(document.documentElement.getAttribute("data-theme")).toBe("dark");
  });

  it("starts from the page's appearance, so a remount in the visit keeps it", () => {
    document.documentElement.setAttribute("data-theme", "dark");
    const { result } = renderHook(() => usePreferences());
    expect(result.current.preferences.appearance).toBe("dark");
    expect(result.current.preferences).toMatchObject({ ...defaultPreferences, appearance: "dark" });
  });
});
