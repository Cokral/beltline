import { createContext, useCallback, useContext, useMemo, useRef, useState, type PropsWithChildren } from "react";
import type { Belt, Stage } from "../../api/wire.gen";

/**
 * An unsent belt in the browser, in the API's own shape. Saved belts are keyed by their id; new and copied
 * belts, which have no library entry until Save, by a `draft:` key. The daemon never holds this state.
 */
export interface BeltDraft {
  key: string;
  belt: Belt;
  /** True for a new or copied belt that has not been saved yet. */
  unsaved: boolean;
  /** The draft a copy was made from; Discard returns to it. */
  sourceKey: string | null;
  /**
   * For a saved belt's draft, the saved draft it was first edited from. A draft that still equals it, with no typed
   * input, holds nothing unsent, so a newer saved version replaces it. Absent when that is not known, which keeps the draft.
   */
  basis?: Belt;
}

/** The trigger settings typed for each kind of one trigger entry, kept for the session so switching kinds back restores them. */
export interface TriggerStash {
  entry: string;
  triggers: Record<string, unknown>;
}

/** Input typed into one form and not yet applied, by field. Only fields the person changed are here. */
export type TypedInput = Readonly<Record<string, unknown>>;

interface BeltDraftsState {
  drafts: Readonly<Record<string, BeltDraft>>;
  /** Typed, unapplied form input per belt key, then per form (such as `trigger:1` or `stage:build`). */
  inputs: Readonly<Record<string, Readonly<Record<string, TypedInput>>>>;
  /** The stage whose form is open, per belt key. */
  selectedStages: Readonly<Record<string, string | null>>;
  /** The warnings the daemon last returned for each belt key, in its order. */
  warnings: Readonly<Record<string, readonly string[]>>;
  /** The trigger stash per belt key. */
  stashes: Readonly<Record<string, TriggerStash>>;
}

export interface BeltDrafts extends BeltDraftsState {
  /** Replaces one draft, or with `undefined` drops it. The updater sees the latest draft, so late responses and fast typing never overwrite each other. */
  update: (key: string, change: (current: BeltDraft | undefined) => BeltDraft | undefined) => void;
  /** As `update`, but the result moves to another key in the same step, for a new draft that Save has given its id. */
  move: (from: string, to: string, change: (current: BeltDraft | undefined) => BeltDraft | undefined) => void;
  setWarnings: (key: string, warnings: readonly string[]) => void;
  setStash: (key: string, stash: TriggerStash) => void;
  /** Sets one typed field of one form; `undefined` drops it. */
  type: (key: string, form: string, field: string, value: unknown) => void;
  /** Drops typed fields of one form that still hold the values that were applied, keeping anything typed since. Without `applied`, drops them all. */
  settleTyped: (key: string, form: string, applied?: TypedInput) => void;
  /** Drops every typed field of a belt, for Discard. */
  dropTyped: (key: string) => void;
  selectStage: (key: string, stage: string | null) => void;
  /** The draft as it is now, for a request that was started earlier and is about to send. */
  latest: (key: string) => BeltDraft | undefined;
  nextKey: () => string;
}

const BeltDraftsContext = createContext<BeltDrafts | null>(null);

/** Holds the browser's unsent belt drafts above the routes, so they survive navigation within the app session. Reloading clears them. */
export function BeltDraftsProvider({ children }: PropsWithChildren) {
  const [state, setState] = useState<BeltDraftsState>({ drafts: {}, warnings: {}, stashes: {}, inputs: {}, selectedStages: {} });
  const latestState = useRef(state);
  latestState.current = state;
  const counter = useRef(0);
  const move = useCallback((from: string, to: string, change: (current: BeltDraft | undefined) => BeltDraft | undefined) => {
    setState(current => {
      const next = change(current.drafts[from]);
      const drafts = { ...current.drafts };
      delete drafts[from];
      if (next) drafts[to] = { ...next, key: to };
      if (from === to) return { ...current, drafts };
      // A new belt that Save has given its id keeps its typed input and open stage under the new key.
      const inputs = { ...current.inputs };
      const selectedStages = { ...current.selectedStages };
      if (from in inputs) { inputs[to] = inputs[from]!; delete inputs[from]; }
      if (from in selectedStages) { selectedStages[to] = selectedStages[from]!; delete selectedStages[from]; }
      return { ...current, drafts, inputs, selectedStages };
    });
  }, []);
  const update = useCallback((key: string, change: (current: BeltDraft | undefined) => BeltDraft | undefined) => move(key, key, change), [move]);
  const setWarnings = useCallback((key: string, warnings: readonly string[]) => {
    setState(current => ({ ...current, warnings: { ...current.warnings, [key]: warnings } }));
  }, []);
  const setStash = useCallback((key: string, stash: TriggerStash) => {
    setState(current => ({ ...current, stashes: { ...current.stashes, [key]: stash } }));
  }, []);
  const type = useCallback((key: string, form: string, field: string, value: unknown) => {
    setState(current => {
      const forms = { ...(current.inputs[key] ?? {}) };
      const fields: Record<string, unknown> = { ...(forms[form] ?? {}) };
      if (value === undefined) delete fields[field];
      else fields[field] = value;
      if (Object.keys(fields).length === 0) delete forms[form];
      else forms[form] = fields;
      return { ...current, inputs: { ...current.inputs, [key]: forms } };
    });
  }, []);
  const settleTyped = useCallback((key: string, form: string, applied?: TypedInput) => {
    setState(current => {
      const forms = { ...(current.inputs[key] ?? {}) };
      const fields: Record<string, unknown> = { ...(forms[form] ?? {}) };
      for (const field of Object.keys(fields)) {
        if (!applied || (field in applied && sameBelt(applied[field], fields[field]))) delete fields[field];
      }
      if (Object.keys(fields).length === 0) delete forms[form];
      else forms[form] = fields;
      return { ...current, inputs: { ...current.inputs, [key]: forms } };
    });
  }, []);
  const dropTyped = useCallback((key: string) => {
    setState(current => {
      const inputs = { ...current.inputs };
      delete inputs[key];
      return { ...current, inputs };
    });
  }, []);
  const selectStage = useCallback((key: string, stage: string | null) => {
    setState(current => ({ ...current, selectedStages: { ...current.selectedStages, [key]: stage } }));
  }, []);
  const latest = useCallback((key: string) => latestState.current.drafts[key], []);
  const nextKey = useCallback(() => {
    counter.current += 1;
    return `draft:${counter.current}`;
  }, []);
  const value = useMemo(() => ({ ...state, update, move, setWarnings, setStash, type, settleTyped, dropTyped, selectStage, latest, nextKey }),
    [state, update, move, setWarnings, setStash, type, settleTyped, dropTyped, selectStage, latest, nextKey]);
  return <BeltDraftsContext.Provider value={value}>{children}</BeltDraftsContext.Provider>;
}

export function useBeltDrafts(): BeltDrafts {
  const value = useContext(BeltDraftsContext);
  if (!value) throw new Error("useBeltDrafts must be used within AppProviders");
  return value;
}

/** Structural equality over the JSON the daemon sent, independent of key order. */
export function sameBelt(left: unknown, right: unknown): boolean {
  return canonical(left) === canonical(right);
}

function canonical(value: unknown): string {
  if (Array.isArray(value)) return `[${value.map(canonical).join(",")}]`;
  if (value && typeof value === "object") {
    return `{${Object.keys(value).sort().map(key => `${JSON.stringify(key)}:${canonical((value as Record<string, unknown>)[key])}`).join(",")}}`;
  }
  return JSON.stringify(value);
}

/** Stage fields the daemon decides, which a typed value never overrides. */
const structuralStageFields = new Set(["id", "route", "outcomes", "agent"]);

/**
 * The daemon's answer to a structural edit, with the input typed while the edit was pending kept. Any belt
 * or stage field the person changed since the edit was sent wins over the answer; everything else is the answer.
 */
export function rebaseTyped(answer: Belt, current: Belt, submitted: Belt): Belt {
  if (sameBelt(current, submitted)) return answer;
  const next: Belt = { ...answer };
  for (const field of ["name", "base", "afk"] as const) {
    if (!sameBelt(current[field], submitted[field])) next[field] = current[field] as never;
  }
  next.stages = answer.stages.map(stage => {
    const now = current.stages.find(item => item.id === stage.id);
    const sent = submitted.stages.find(item => item.id === stage.id);
    if (!now || !sent) return stage;
    const changed: Partial<Stage> = {};
    for (const field of Object.keys(now) as Array<keyof Stage & string>) {
      if (structuralStageFields.has(field)) continue;
      if (!sameBelt(now[field], sent[field])) Object.assign(changed, { [field]: now[field] });
    }
    return { ...stage, ...changed };
  });
  return next;
}
