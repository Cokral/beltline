import { readFileSync } from "node:fs";
import { resolve } from "node:path";

interface SnapshotState {
  globallyPaused: boolean;
  settings: { afkEnabled: boolean };
  actions: Array<{ id: string; label: string }>;
}

const fixture = JSON.parse(readFileSync(resolve(process.cwd(), "../daemon/testdata/golden/state.json"), "utf8")) as SnapshotState;

/** A valid server-state golden with only the requested visible global state adjusted. */
export function snapshotState({ paused, afk }: { paused?: boolean; afk?: boolean }): string {
  const state = structuredClone(fixture);
  if (paused !== undefined) {
    state.globallyPaused = paused;
    const action = state.actions.find(item => item.id === "pause");
    if (action) action.label = paused ? "Resume" : "Pause";
  }
  if (afk !== undefined) {
    state.settings.afkEnabled = afk;
    const action = state.actions.find(item => item.id === "afk");
    // The daemon's label says the state it is in, as the state golden's "GLOBAL AFK OFF" does.
    if (action) action.label = afk ? "GLOBAL AFK ON" : "GLOBAL AFK OFF";
  }
  return JSON.stringify(state);
}
