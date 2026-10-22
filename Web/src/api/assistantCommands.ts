import type { QueryClient } from "@tanstack/react-query";
import { apiQueryKey } from "./resources";
import type { AssistantConfigurationResponse, AssistantResponse } from "./wire.gen";

import type { CommandRequest } from "./useCommand";

export const assistantPath = "/api/v1/assistant";
export const assistantQueryKey = apiQueryKey(assistantPath);

export type AssistantCommand = "configuration" | "start" | "stop";

/** All assistant writes use the shared command path and publish the daemon's validated resource. */
export function assistantCommandRequest(action: AssistantCommand, body: AssistantConfigurationResponse | { request: string } | Record<string, never>): CommandRequest<unknown, AssistantResponse> {
  return {
    path: `${assistantPath}/${action}`,
    body,
    affects: ["assistant", "settings", "models"],
    publish: (queryClient: QueryClient, result: AssistantResponse) => {
      queryClient.setQueryData(assistantQueryKey, result);
    }
  };
}
