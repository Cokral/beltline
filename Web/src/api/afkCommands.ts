import type { QueryClient } from "@tanstack/react-query";
import type { AfkDecisionsResponse } from "./wire.gen";
import type { CommandRequest } from "./useCommand";

/** Marks AFK decisions as checked: the named ones, or every one still to check when `ids` is absent. */
export function afkReviewRequest(ids?: readonly string[]): CommandRequest<unknown, AfkDecisionsResponse> {
  return {
    path: "/api/v1/afk/review",
    body: ids ? { ids } : {},
    affects: ["afk", "state"],
    publish: (queryClient: QueryClient, _result: AfkDecisionsResponse) => {
      // Every filter's list changes when decisions are checked, so each is refetched.
      void queryClient.invalidateQueries({ queryKey: ["api", "/api/v1/afk/decisions"] });
    }
  };
}
