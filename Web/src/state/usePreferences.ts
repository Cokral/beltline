import { useRef, useState } from "react";
import { defaultPreferences, readPreferences, savePreferences, type WebPreferences } from "./preferences";
import { applyAppearance, pageAppearance } from "./theme";

/**
 * The browser preferences as one component sees them. An update merges into what is stored now, not into
 * this component's copy, so a change made by another part of the page is not written back over. When the
 * browser refuses to read storage, the update merges into the copy this page holds, so the controls keep
 * working for the current visit. The appearance always follows the page, so a refused write still holds.
 */
export function usePreferences(): { preferences: WebPreferences; update: (changes: Partial<WebPreferences>) => void } {
  const [preferences, setPreferences] = useState<WebPreferences>(() => ({
    ...(readPreferences() ?? defaultPreferences),
    appearance: pageAppearance()
  }));
  const current = useRef(preferences);
  const update = (changes: Partial<WebPreferences>) => {
    const next = { ...(readPreferences() ?? current.current), appearance: pageAppearance(), ...changes };
    applyAppearance(next.appearance);
    savePreferences(next);
    current.current = next;
    setPreferences(next);
  };
  return { preferences, update };
}
