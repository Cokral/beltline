import { describe, expect, it } from "vitest";
import { clampNavigationWidth, clampSidebarWidth, defaultPreferences, drawnSidebarWidth, loadPreferences, parsePreferences, preferencesStorageKey, readPreferences, savePreferences, type WebPreferences } from "./preferences";

class MemoryStorage {
  values = new Map<string, string>();
  getItem(key: string): string | null { return this.values.get(key) ?? null; }
  setItem(key: string, value: string): void { this.values.set(key, value); }
}

describe("typed browser preferences", () => {
  it("persists Floor, independent folds, nav state, and explicit widths across reload", () => {
    const storage = new MemoryStorage();
    const value: WebPreferences = {
      ...defaultPreferences,
      floorPinnedBeltId: "nightly",
      runsFolded: { "belt:nightly": ["group:main"] },
      watchingFolded: { "belt:nightly": ["group:watching"] },
      navigationFolded: { "workspace:agents": true },
      listFolded: { "belts:archived": true },
      navigationOpen: false,
      sessionsOpen: true,
      assistantOpen: true,
      navigationWidth: 300,
      sessionsWidth: 500,
      assistantWidth: 420
    };
    savePreferences(value, storage);
    const restored = loadPreferences(storage);
    expect(restored).toEqual(value);
    expect(restored.runsFolded).not.toEqual(restored.watchingFolded);
    expect([...storage.values.keys()]).toEqual([preferencesStorageKey]);
    expect(storage.values.get(preferencesStorageKey)).not.toContain("token");
  });

  it("uses defaults for absent, malformed, blocked, or unsupported storage", () => {
    const storage = new MemoryStorage();
    expect(loadPreferences(storage)).toEqual(defaultPreferences);
    expect(parsePreferences("not-json")).toEqual(defaultPreferences);
    expect(parsePreferences(JSON.stringify({ ...defaultPreferences, version: 99 }))).toEqual(defaultPreferences);
    expect(loadPreferences({ getItem() { throw new Error("blocked"); } })).toEqual(defaultPreferences);
    expect(() => savePreferences(defaultPreferences, { setItem() { throw new Error("blocked"); } })).not.toThrow();
  });

  it("clamps stored widths and keeps the saved sidebar preference separate from the drawn width", () => {
    expect(clampNavigationWidth(100)).toBe(180);
    expect(clampNavigationWidth(999)).toBe(420);
    expect(clampNavigationWidth(Number.NaN)).toBe(defaultPreferences.navigationWidth);
    expect(clampSidebarWidth(100)).toBe(320);
    expect(clampSidebarWidth(9999)).toBe(900);
    expect(clampSidebarWidth(Number.POSITIVE_INFINITY)).toBeNull();
    expect(drawnSidebarWidth(900, 700)).toBe(480);
    expect(drawnSidebarWidth(900, null)).toBe(450);
    expect(drawnSidebarWidth(500, 800)).toBe(80);

    const restored = parsePreferences(JSON.stringify({ ...defaultPreferences, navigationWidth: 700, sessionsWidth: 1200, assistantWidth: 100 }));
    expect(restored.navigationWidth).toBe(420);
    expect(restored.sessionsWidth).toBe(900);
    expect(restored.assistantWidth).toBe(320);
  });

  it("round-trips Sessions visibility and width, and a reset to the default width", () => {
    const storage = new MemoryStorage();
    savePreferences({ ...defaultPreferences, sessionsOpen: true, sessionsWidth: 640 }, storage);
    expect(loadPreferences(storage)).toMatchObject({ sessionsOpen: true, sessionsWidth: 640 });
    savePreferences({ ...loadPreferences(storage), sessionsWidth: null }, storage);
    expect(loadPreferences(storage)).toMatchObject({ sessionsOpen: true, sessionsWidth: null });
  });

  it("clamps the Sessions width to 320–900 px at both boundaries and keeps a reset as null", () => {
    expect(clampSidebarWidth(319.9)).toBe(320);
    expect(clampSidebarWidth(320)).toBe(320);
    expect(clampSidebarWidth(900)).toBe(900);
    expect(clampSidebarWidth(900.1)).toBe(900);
    expect(clampSidebarWidth(null)).toBeNull();
    expect(parsePreferences(JSON.stringify({ ...defaultPreferences, sessionsWidth: null })).sessionsWidth).toBeNull();
  });

  it("resets to half the available workspace when no width was chosen", () => {
    expect(drawnSidebarWidth(1200, null)).toBe(600);
    expect(drawnSidebarWidth(1400, null)).toBe(700);
    // A reset is half the workspace, even where that exceeds the 900 px a chosen width may reach.
    expect(drawnSidebarWidth(1900, null)).toBe(950);
  });

  it("shrinks the drawn sidebar to keep a 420 px main column without overwriting the chosen width", () => {
    const chosen = 800;
    expect(drawnSidebarWidth(1000, chosen)).toBe(580);
    expect(drawnSidebarWidth(700, chosen)).toBe(280);
    expect(drawnSidebarWidth(400, chosen)).toBe(0);
    // Expanding the workspace restores the width that was chosen, not the shrunken one.
    expect(drawnSidebarWidth(1400, chosen)).toBe(800);
    expect(parsePreferences(JSON.stringify({ ...defaultPreferences, sessionsWidth: chosen })).sessionsWidth).toBe(chosen);
  });

  it("round-trips Assistant visibility and its chosen width through browser preferences", () => {
    const storage = new MemoryStorage();
    savePreferences({ ...defaultPreferences, assistantOpen: true, assistantWidth: 640 }, storage);
    expect(loadPreferences(storage)).toMatchObject({ assistantOpen: true, assistantWidth: 640 });
    savePreferences({ ...loadPreferences(storage), assistantOpen: false, assistantWidth: null }, storage);
    expect(loadPreferences(storage)).toMatchObject({ assistantOpen: false, assistantWidth: null });
  });

  it("clamps the Assistant's chosen width, defaults it to half, and restores it after a temporary shrink", () => {
    expect(clampSidebarWidth(319.9)).toBe(320);
    expect(clampSidebarWidth(320)).toBe(320);
    expect(clampSidebarWidth(900)).toBe(900);
    expect(clampSidebarWidth(900.1)).toBe(900);
    expect(parsePreferences(JSON.stringify({ ...defaultPreferences, assistantWidth: 1200 })).assistantWidth).toBe(900);
    expect(drawnSidebarWidth(1200, null)).toBe(600);

    const chosen = 800;
    expect(drawnSidebarWidth(1000, chosen)).toBe(580);
    expect(drawnSidebarWidth(700, chosen)).toBe(280);
    expect(drawnSidebarWidth(1400, chosen)).toBe(chosen);
    expect(parsePreferences(JSON.stringify({ ...defaultPreferences, assistantWidth: chosen })).assistantWidth).toBe(chosen);
  });

  it("keeps the controls usable when browser storage is blocked", () => {
    const blocked = { getItem() { throw new Error("blocked"); }, setItem() { throw new Error("blocked"); } };
    expect(readPreferences(blocked)).toBeNull();
    expect(loadPreferences(blocked)).toEqual(defaultPreferences);
    expect(savePreferences({ ...defaultPreferences, sessionsOpen: true }, blocked)).toBe(false);
    expect(savePreferences(defaultPreferences, new MemoryStorage())).toBe(true);
  });
});

describe("Overview belt pin persistence", () => {
  it("round-trips the pin under the preferences key and restores it on a fresh read", () => {
    const storage = new MemoryStorage();
    savePreferences({ ...defaultPreferences, floorPinnedBeltId: "issues" }, storage);
    expect(JSON.parse(storage.getItem(preferencesStorageKey)!).floorPinnedBeltId).toBe("issues");
    expect(loadPreferences(storage).floorPinnedBeltId).toBe("issues");
  });

  it("falls back to the defaults for a partial or malformed stored record", () => {
    expect(parsePreferences(JSON.stringify({ version: 1, floorPinnedBeltId: "issues" }))).toEqual(defaultPreferences);
    expect(parsePreferences("{not json")).toEqual(defaultPreferences);
  });

  it("reads as the defaults and keeps working when storage throws", () => {
    const throwing = {
      getItem: () => { throw new Error("denied"); },
      setItem: () => { throw new Error("denied"); }
    };
    expect(loadPreferences(throwing)).toEqual(defaultPreferences);
    expect(() => savePreferences({ ...defaultPreferences, floorPinnedBeltId: "issues" }, throwing)).not.toThrow();
  });

  it("keeps what a page reserves beside a right sidebar", () => {
    expect(drawnSidebarWidth(860, null)).toBe(430);
    expect(drawnSidebarWidth(860, null, 560)).toBe(300);
    expect(drawnSidebarWidth(860, 640, 560)).toBe(300);
    expect(drawnSidebarWidth(1600, 640, 560)).toBe(640);
    expect(drawnSidebarWidth(500, null, 560)).toBe(0);
  });
});

describe("Appearance preference", () => {
  it("round-trips both Light and Dark through browser storage", () => {
    const storage = new MemoryStorage();
    savePreferences({ ...defaultPreferences, appearance: "dark" }, storage);
    expect(loadPreferences(storage).appearance).toBe("dark");
    savePreferences({ ...defaultPreferences, appearance: "light" }, storage);
    expect(loadPreferences(storage).appearance).toBe("light");
  });

  it("opens in Light by default, whatever the OS appearance", () => {
    expect(defaultPreferences.appearance).toBe("light");
    expect(loadPreferences(new MemoryStorage()).appearance).toBe("light");
  });

  it("reads a version-1 record saved before Appearance existed as Light and keeps its other preferences", () => {
    const { appearance: _unused, ...legacy } = { ...defaultPreferences, floorPinnedBeltId: "nightly", sessionsOpen: true, sessionsWidth: 500 };
    const parsed = parsePreferences(JSON.stringify(legacy));
    expect(parsed.appearance).toBe("light");
    expect(parsed).toEqual({ ...legacy, appearance: "light" });
  });

  it("falls back to Light for an invalid appearance without discarding the other valid preferences", () => {
    for (const appearance of ["system", "DARK", 1, null, true]) {
      const parsed = parsePreferences(JSON.stringify({ ...defaultPreferences, floorPinnedBeltId: "issues", appearance }));
      expect(parsed.appearance, JSON.stringify(appearance)).toBe("light");
      expect(parsed.floorPinnedBeltId).toBe("issues");
      expect(parsed.navigationWidth).toBe(defaultPreferences.navigationWidth);
    }
  });

  it("uses Light for malformed JSON, absent data and storage that cannot be read, and reports refused writes", () => {
    expect(parsePreferences("{\"version\":1,\"appearance\":\"dark\"")).toEqual(defaultPreferences);
    expect(readPreferences(new MemoryStorage())?.appearance).toBe("light");
    expect(loadPreferences({ getItem() { throw new Error("denied"); } }).appearance).toBe("light");
    expect(() => savePreferences({ ...defaultPreferences, appearance: "dark" }, { setItem() { throw new Error("denied"); } })).not.toThrow();
    expect(savePreferences({ ...defaultPreferences, appearance: "dark" }, { setItem() { throw new Error("denied"); } })).toBe(false);
  });
});
