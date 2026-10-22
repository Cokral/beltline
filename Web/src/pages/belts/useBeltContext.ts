import { useEffect, useRef, useState } from "react";
import { apiRequest } from "../../api/client";
import type { Belt, BeltContext } from "../../api/wire.gen";
import { useRuntime } from "../../app/runtime";
import { contextRequest } from "./beltCommands";
import { useBeltDrafts } from "./beltDrafts";

export interface BeltContextRead {
  /** The daemon's context for the draft, or null until the first answer (or after a failed read). */
  context: BeltContext | null;
  /** Reads the context again, after a committed change. */
  refreshContext: () => void;
}

/**
 * The daemon's context for the selected belt's draft. It is read after mount, after each committed change
 * (`refreshContext`) and when the selected stage or the library revision changes, never while typing. Only the
 * newest read may apply: a late answer that a newer one has replaced is dropped.
 */
export function useBeltContext(key: string, belt: Belt, selectedStageId: string | null, snapshotRevision: unknown, shadow = false): BeltContextRead {
  const runtime = useRuntime();
  const drafts = useBeltDrafts();
  const [context, setContext] = useState<BeltContext | null>(null);
  const [revision, setRevision] = useState(0);
  const sequence = useRef(0);

  useEffect(() => {
    if (!runtime.canRead) return;
    const current = ++sequence.current;
    const latest = drafts.latest(key)?.belt ?? belt;
    const request = contextRequest(latest, selectedStageId, shadow);
    apiRequest<BeltContext>(request.path, { method: "POST", body: request.body, onUnauthorized: runtime.requireSignIn })
      .then(next => { if (current === sequence.current) setContext(next); })
      .catch(() => { if (current === sequence.current) setContext(null); });
    // The draft is read through `drafts.latest`, so the request follows the committed change, not the closure.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, revision, selectedStageId, runtime.canRead, snapshotRevision]);

  return { context, refreshContext: () => setRevision(value => value + 1) };
}
