import { useMutation, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { useRef, useState } from "react";
import { apiRequest } from "./client";
import type { EventResource } from "./wire.gen";
import { queryResources } from "./resources";
import { useRuntime } from "../app/runtime";

/** One POST to the daemon. Commands are never retried automatically: a failed command waits for an explicit retry. */
export interface CommandRequest<TBody, TResult> {
  path: string;
  body: TBody;
  /** Applies unsent form input through daemon draft commands before the final POST. */
  prepare?: (body: TBody) => Promise<TBody>;
  /** Cached reads that depend on any of these resources are refetched after a success. */
  affects: readonly EventResource[];
  /** Writes the validated result into the cache before dependent reads refetch. */
  publish: (queryClient: QueryClient, result: TResult) => void;
}

export interface CommandController {
  /** Sends the request unless one is already in flight; returns false when it was refused as a duplicate. */
  submit: <TBody, TResult>(request: CommandRequest<TBody, TResult>) => boolean;
  /** Sends the most recent request again. Only a user's explicit retry calls this. */
  retry: () => void;
  clearError: () => void;
  pending: boolean;
  error: unknown;
  lastRequest: CommandRequest<unknown, unknown> | null;
}

/**
 * Sends daemon commands for the shell. A second submission while one is pending is refused before it
 * reaches the network. Success publishes the validated result and refetches only the reads that depend
 * on the affected resources; failure publishes nothing, so the last successful state stays on screen.
 */
export function useCommand(): CommandController {
  const queryClient = useQueryClient();
  const runtime = useRuntime();
  const inFlight = useRef(false);
  const [lastRequest, setLastRequest] = useState<CommandRequest<unknown, unknown> | null>(null);
  const mutation = useMutation({
    retry: false,
    mutationFn: async (request: CommandRequest<unknown, unknown>) => apiRequest<unknown>(request.path, {
      method: "POST",
      body: request.prepare ? await request.prepare(request.body) : request.body,
      onUnauthorized: runtime.requireSignIn
    }),
    onSuccess: (result, request) => {
      request.publish(queryClient, result);
      void queryClient.invalidateQueries({
        predicate: query => queryResources(query.meta).some(resource => request.affects.includes(resource))
      });
    }
  });

  const send = (request: CommandRequest<unknown, unknown>) => {
    if (inFlight.current) return false;
    inFlight.current = true;
    setLastRequest(request);
    mutation.mutate(request, { onSettled: () => { inFlight.current = false; } });
    return true;
  };

  return {
    submit: request => send(request as CommandRequest<unknown, unknown>),
    retry: () => { if (lastRequest) send(lastRequest); },
    clearError: () => mutation.reset(),
    pending: mutation.isPending,
    error: mutation.error,
    lastRequest
  };
}
