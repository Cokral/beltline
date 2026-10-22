import { formatContextTokens } from "../format";
import type { ModelChoice } from "../api/wire.gen";

export type ModelPickerState = "ready" | "loading" | "failed" | "frozen" | "readonly";

export interface ModelPickerProps {
  id: string;
  label: string;
  /** The runner's display name, used in the empty-choices note. */
  runnerLabel: string;
  /** Exactly the choices the daemon offers for this runner. Only these are selectable. */
  choices: readonly ModelChoice[];
  /** The models the daemon allows for this runner. A selection outside the choices says why it is not offered. */
  allowedModels?: readonly string[];
  /** The opaque choice id, or "" for the runner default. It is returned and displayed unchanged. */
  value: string;
  /** The daemon's reason the selection is not offered; null when it is. Absent, the picker reads the lists it was given. */
  staleReason?: string | null;
  state?: ModelPickerState;
  errorMessage?: string | null;
  /** Visually hide the label but keep it associated with the select, for a column heading that names it. */
  labelHidden?: boolean;
  onChange?: (choiceId: string) => void;
}

function choiceLabel(choice: ModelChoice): string {
  return choice.contextTokens ? `${choice.name} - ${formatContextTokens(choice.contextTokens)}` : choice.name;
}

/**
 * A model choice for a runner. The picker never adds a choice of its own and never changes the id it was
 * given: a selection that is no longer offered stays visible, labelled, until the person picks another one.
 */
export function ModelPicker({ id, label, runnerLabel, choices, allowedModels, value, staleReason: daemonReason, state = "ready", errorMessage, labelHidden, onChange }: ModelPickerProps) {
  const selected = choices.find(choice => choice.id === value);
  const computedReason = value && !selected
    ? (allowedModels ?? []).includes(value) ? "unavailable in catalog" : "not allowed"
    : null;
  const staleReason = daemonReason !== undefined ? daemonReason : computedReason;

  if (state === "readonly") {
    const shown = selected ?? (value ? { id: value, name: value } : null);
    return <div className="model-picker" data-model-state="readonly">
      <span className="model-label">{label}</span>
      <code className="model-readonly-id">{shown ? shown.id : "Runner default"}</code>
      {selected?.contextTokens && <span className="model-context">{formatContextTokens(selected.contextTokens)}</span>}
    </div>;
  }

  const disabled = state !== "ready" || (choices.length === 0 && !staleReason);
  const dataState = state === "ready" && choices.length === 0 ? "empty" : state;
  return <div className="model-picker" data-model-state={dataState}>
    <label htmlFor={id} className={labelHidden ? "model-label screen-reader-only" : "model-label"}>{label}</label>
    <select id={id} className="model-select" value={value} disabled={disabled}
      onChange={event => {
        const next = event.currentTarget.value;
        if (choices.some(choice => choice.id === next)) onChange?.(next);
      }}>
      {!value && <option value="" disabled>Runner default</option>}
      {staleReason && <option value={value} disabled>{`${value} (${staleReason})`}</option>}
      {choices.map(choice => <option key={choice.id} value={choice.id}>{choiceLabel(choice)}</option>)}
    </select>
    {state === "loading" && <p role="status" className="model-note">Loading models…</p>}
    {state === "failed" && <p role="alert" className="model-note model-error">{errorMessage || "Models could not be loaded."}</p>}
    {state === "frozen" && <p className="model-note">Choices are frozen for this run.</p>}
    {state === "ready" && choices.length === 0 && <p className="model-note">No models reported for {runnerLabel}.</p>}
    {staleReason && <p className="model-note"><span className="model-stale">{staleReason}</span></p>}
  </div>;
}
