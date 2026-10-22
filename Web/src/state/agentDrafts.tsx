import { createContext, useContext, useMemo, useRef, useState, type PropsWithChildren } from "react";
import type { Agent } from "../api/wire.gen";

interface AgentDraftsValue {
  drafts: Readonly<Record<string, Agent>>;
  /** Stores a draft. `basis` is the saved agent the draft started from; the first one given for a key is kept. */
  putDraft: (key: string, agent: Agent, basis?: Agent) => void;
  discardDraft: (key: string) => void;
  /** The saved agent a draft started from, or undefined for a new draft or one that started nowhere. */
  basis: (key: string) => Agent | undefined;
  revision: (key: string) => number;
  generation: (key: string) => number;
}

const AgentDraftsContext = createContext<AgentDraftsValue | null>(null);

/** Keeps unsent agent edits while the user browses away and returns. */
export function AgentDraftsProvider({ children }: PropsWithChildren) {
  const [drafts, setDrafts] = useState<Record<string, Agent>>({});
  const revisions = useRef<Record<string, number>>({});
  const generations = useRef<Record<string, number>>({});
  const bases = useRef<Record<string, Agent>>({});
  const value = useMemo<AgentDraftsValue>(() => ({
    drafts,
    revision: key => revisions.current[key] ?? 0,
    generation: key => generations.current[key] ?? 0,
    basis: key => bases.current[key],
    putDraft: (key, agent, basis) => {
      revisions.current[key] = (revisions.current[key] ?? 0) + 1;
      if (basis !== undefined && bases.current[key] === undefined) bases.current[key] = basis;
      setDrafts(previous => ({ ...previous, [key]: agent }));
    },
    discardDraft: key => {
      generations.current[key] = (generations.current[key] ?? 0) + 1;
      revisions.current[key] = (revisions.current[key] ?? 0) + 1;
      delete bases.current[key];
      setDrafts(previous => {
        if (!(key in previous)) return previous;
        const next = { ...previous };
        delete next[key];
        return next;
      });
    }
  }), [drafts]);
  return <AgentDraftsContext.Provider value={value}>{children}</AgentDraftsContext.Provider>;
}

export function useAgentDrafts() {
  const value = useContext(AgentDraftsContext);
  if (!value) throw new Error("useAgentDrafts must be used inside AgentDraftsProvider");
  return value;
}

/**
 * The draft a runner switch's answer lands in when the user kept typing while it ran, or undefined when the draft no
 * longer matches what the switch was sent for. `base` is the agent the switch was sent for, `switched` the daemon's
 * normalized answer. Only the settings the switch changed are applied, so unrelated typing is kept.
 */
export function runnerSwitchDraft(latest: Agent | undefined, base: Agent, switched: Agent, alternateIndex?: number): Agent | undefined {
  if (!latest || latest.version !== base.version) return undefined;
  if (alternateIndex === undefined) {
    if (latest.runner !== base.runner || latest.model !== base.model || latest.thinking !== base.thinking) return undefined;
    return { ...latest, runner: switched.runner, model: switched.model, thinking: switched.thinking };
  }
  if (JSON.stringify(latest.alternates) !== JSON.stringify(base.alternates)) return undefined;
  return { ...latest, alternates: switched.alternates };
}

export function newAgentDraftKey(): string {
  return `new-${globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random().toString(36).slice(2)}`}`;
}
