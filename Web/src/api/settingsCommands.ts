import type { QueryClient } from "@tanstack/react-query";
import { apiQueryKey } from "./resources";
import type { EventResource, SettingsResponse } from "./wire.gen";
import type { CommandRequest } from "./useCommand";

export const settingsQueryKey = apiQueryKey("/api/v1/settings");

/**
 * One partial settings patch. The daemon validates and returns the saved
 * settings; success writes that response into the settings cache before any
 * dependent read refetches, and failure publishes nothing.
 */
export function settingsPatchRequest(body: unknown, affects: readonly EventResource[] = ["settings", "state"]): CommandRequest<unknown, SettingsResponse> {
  return {
    path: "/api/v1/settings/patch",
    body,
    affects,
    publish: (queryClient: QueryClient, result: SettingsResponse) => {
      queryClient.setQueryData(settingsQueryKey, result);
    }
  };
}
