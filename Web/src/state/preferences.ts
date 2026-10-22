export const preferencesStorageKey = "beltline.web.preferences";
export const preferencesVersion = 1;

/** Light is the default in every browser, whatever the OS appearance is. */
export const appearances = ["light", "dark"] as const;
export type Appearance = (typeof appearances)[number];

export interface WebPreferences {
  version: typeof preferencesVersion;
  appearance: Appearance;
  floorPinnedBeltId: string | null;
  runsFolded: Record<string, string[]>;
  watchingFolded: Record<string, string[]>;
  navigationFolded: Record<string, boolean>;
  listFolded: Record<string, boolean>;
  navigationOpen: boolean;
  sessionsOpen: boolean;
  assistantOpen: boolean;
  navigationWidth: number;
  sessionsWidth: number | null;
  assistantWidth: number | null;
}

const isBoolean = (value: unknown): value is boolean => typeof value === "boolean";
const isNumber = (value: unknown): value is number => typeof value === "number";
const isRecordOf = <T>(value: unknown, isItem: (item: unknown) => item is T): value is Record<string, T> =>
  typeof value === "object" && value !== null && !Array.isArray(value) && Object.values(value).every(isItem);
const isStringList = (value: unknown): value is string[] => Array.isArray(value) && value.every(item => typeof item === "string");

/** Each stored field's shape. A record with a field missing, a field of another shape, or an extra field is not read. */
const storedShapes: { [K in keyof WebPreferences]: (value: unknown) => boolean } = {
  version: value => value === preferencesVersion,
  appearance: () => true, // An unknown appearance reads as Light, below, and keeps the rest.
  floorPinnedBeltId: value => value === null || typeof value === "string",
  runsFolded: value => isRecordOf(value, isStringList),
  watchingFolded: value => isRecordOf(value, isStringList),
  navigationFolded: value => isRecordOf(value, isBoolean),
  listFolded: value => isRecordOf(value, isBoolean),
  navigationOpen: isBoolean,
  sessionsOpen: isBoolean,
  assistantOpen: isBoolean,
  navigationWidth: isNumber,
  sessionsWidth: value => value === null || isNumber(value),
  assistantWidth: value => value === null || isNumber(value)
};

/** The stored record as preferences, or null when it does not have their shape. */
function readStored(value: unknown): WebPreferences | null {
  if (typeof value !== "object" || value === null || Array.isArray(value)) return null;
  const record = value as Record<string, unknown>;
  const fields = Object.keys(storedShapes);
  if (Object.keys(record).some(key => !fields.includes(key))) return null;
  for (const field of fields) {
    if (!storedShapes[field as keyof WebPreferences](record[field])) return null;
  }
  const appearance = appearances.includes(record.appearance as Appearance) ? record.appearance as Appearance : "light";
  return { ...(record as unknown as WebPreferences), appearance };
}

export const defaultPreferences: WebPreferences = {
  version: preferencesVersion,
  appearance: "light",
  floorPinnedBeltId: null,
  runsFolded: {},
  watchingFolded: {},
  navigationFolded: {},
  listFolded: {},
  navigationOpen: true,
  sessionsOpen: false,
  assistantOpen: false,
  navigationWidth: 220,
  sessionsWidth: null,
  assistantWidth: null
};

export function clampNavigationWidth(width: number): number {
  return Number.isFinite(width) ? Math.min(420, Math.max(180, width)) : defaultPreferences.navigationWidth;
}

export function clampSidebarWidth(width: number | null): number | null {
  if (width === null) return null;
  return Number.isFinite(width) ? Math.min(900, Math.max(320, width)) : null;
}

/** How far an arrow key moves a focused right sidebar's divider, in points. */
export const sidebarKeyboardStep = 16;

/** The drawn width of a right sidebar. `reserved` is what the page keeps for itself: a 420 px main column by default. */
export function drawnSidebarWidth(available: number, stored: number | null, reserved = 420): number {
  const chosen = stored ?? available / 2;
  return Math.max(0, Math.min(chosen, available - reserved));
}

export function parsePreferences(serialized: string | null): WebPreferences {
  if (!serialized) return { ...defaultPreferences };
  try {
    const stored = readStored(JSON.parse(serialized) as unknown);
    if (!stored) return { ...defaultPreferences };
    return {
      ...stored,
      navigationWidth: clampNavigationWidth(stored.navigationWidth),
      sessionsWidth: clampSidebarWidth(stored.sessionsWidth),
      assistantWidth: clampSidebarWidth(stored.assistantWidth)
    };
  } catch {
    return { ...defaultPreferences };
  }
}

export function loadPreferences(storage?: Pick<Storage, "getItem">): WebPreferences {
  return readPreferences(storage) ?? { ...defaultPreferences };
}

/** The stored preferences, or null when browser storage cannot be read at all, so a caller can keep what it has. */
export function readPreferences(storage?: Pick<Storage, "getItem">): WebPreferences | null {
  try { return parsePreferences((storage ?? window.localStorage).getItem(preferencesStorageKey)); }
  catch { return null; }
}

/** Returns false when storage refused the write; the preferences still apply for the current visit. */
export function savePreferences(preferences: WebPreferences, storage?: Pick<Storage, "setItem">): boolean {
  const normalized: WebPreferences = {
    ...preferences,
    version: preferencesVersion,
    navigationWidth: clampNavigationWidth(preferences.navigationWidth),
    sessionsWidth: clampSidebarWidth(preferences.sessionsWidth),
    assistantWidth: clampSidebarWidth(preferences.assistantWidth)
  };
  try {
    (storage ?? window.localStorage).setItem(preferencesStorageKey, JSON.stringify(normalized));
    return true;
  } catch {
    // Storage can be unavailable in private or restricted contexts.
    return false;
  }
}
