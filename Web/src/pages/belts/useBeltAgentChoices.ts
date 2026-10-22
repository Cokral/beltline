import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { apiRequest } from "../../api/client";

import { apiQueryKey, resourcesForPath } from "../../api/resources";
import { useRuntime } from "../../app/runtime";
import type { AgentOption } from "./StageLine";
import type { AgentsResponse } from "../../api/wire.gen";

export interface BeltAgentChoices {
  /** The agents a new stage may copy, in the daemon's order; empty until the first stage picker asks for them. */
  agents: readonly AgentOption[];
  /** The chosen agent as `id@version`, or "" when no agent is listed. */
  choice: string;
  setChoice: (choice: string) => void;
  /** Asks for the agents list; the first stage picker that opens calls it. */
  needAgents: () => void;
}

/**
 * The agents a stage on the selected belt may copy. The daemon names them, so the editor only chooses among
 * them. The list is read once a stage picker asks for it, and never for an archived belt.
 */
export function useBeltAgentChoices(archived: boolean): BeltAgentChoices {
  const runtime = useRuntime();
  const [choice, setChoice] = useState("");
  const [wanted, setWanted] = useState(false);
  const agents = useQuery({
    queryKey: apiQueryKey("/api/v1/agents"),
    queryFn: ({ signal }) => apiRequest<AgentsResponse>("/api/v1/agents", { signal, onUnauthorized: runtime.requireSignIn }),
    enabled: runtime.canRead && !archived && wanted,
    retry: false,
    meta: { resourceDeps: resourcesForPath("/api/v1/agents") }
  });
  const listed = agents.data?.choices ?? [];
  const chosen = listed.find(agent => `${agent.id}@${agent.version}` === choice) ?? listed[0];
  return {
    agents: listed,
    choice: chosen ? `${chosen.id}@${chosen.version}` : "",
    setChoice,
    needAgents: () => setWanted(true)
  };
}
