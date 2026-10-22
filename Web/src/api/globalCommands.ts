import type { QueryClient } from "@tanstack/react-query";
import type { EventResource, StateResponse } from "./wire.gen";
import type { GlobalActionId } from "./actions";
import { apiQueryKey } from "./resources";
import type { CommandRequest } from "./useCommand";

export const stateQueryKey = apiQueryKey("/api/v1/state");

/** Writes the command's returned state into the state cache, so the controls update before any event arrives. */
export function publishState(queryClient: QueryClient, state: StateResponse): void {
  queryClient.setQueryData(stateQueryKey, state);
}

/**
 * The reads a global command can change. Pause and Check now move the counts and the paused flag;
 * AFK also moves the settings the state reports and the work and run reads that follow it.
 */
export const globalCommandAffects: Record<GlobalActionId, readonly EventResource[]> = {
  pause: ["state"],
  afk: ["state", "settings"],
  "check-now": ["state", "intake"]
};

/**
 * Builds the single POST a global control sends. Pause sends the opposite of the latest loaded
 * `globallyPaused`, AFK the opposite of `settings.afkEnabled`, and Check now an empty body.
 */
export function globalCommandRequest(id: GlobalActionId, state: StateResponse): CommandRequest<unknown, StateResponse> {
  const common = { publish: publishState, affects: globalCommandAffects[id] };
  if (id === "pause") return { ...common, path: "/api/v1/pause", body: { paused: !state.globallyPaused } };
  if (id === "afk") return { ...common, path: "/api/v1/afk", body: { enabled: !state.settings.afkEnabled } };
  return { ...common, path: "/api/v1/check-now", body: {} };
}
