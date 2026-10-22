import type { AfkMode, Belt } from "../../api/wire.gen";
import { unchangedField } from "./draftEditor";
import type { DraftOperands, DraftOperation } from "./beltCommands";

export function afkHelp(mode: AfkMode, on: boolean): string {
  if (mode === "always") return "New runs on this belt start in AFK mode, whatever the global switch.";
  if (mode === "never") return "New runs on this belt wait for you, whatever the global switch.";
  return `New runs on this belt follow the global AFK switch, which is ${on ? "on" : "off"}.`;
}

/**
 * The draft edit that applies one typed form, as that form's Enter or Apply sends it, or null when there is
 * nothing to send: an empty or unchanged value, input the daemon already declined, or an entry no longer there.
 */
export function typedOperation(draft: Belt, form: string, typed: Record<string, unknown>): { op: DraftOperation; operands: DraftOperands } | null {
  if (unchangedField in typed) return null;
  const [kind, id, triggerKind] = form.split(":");
  if (kind === "stage" && id) {
    const update = { ...typed };
    if (update.slots === "") delete update.slots;
    else if ("slots" in update) update.slots = Number(update.slots);
    if (update.thinking === "") { delete update.thinking; update.clearThinking = true; }
    if (update.runner === "") delete update.runner;
    if (update.workspace === "") delete update.workspace;
    return { op: "stage-update", operands: { stageId: form.slice("stage:".length), update } };
  }
  if (kind === "route" && id) {
    const name = String(typed.name ?? "").trim();
    return name === "" ? null : { op: "route-update", operands: { routeId: id, routeName: String(typed.name) } };
  }
  const entry = draft.triggers.find(item => item.id === id);
  if (!entry) return null;
  if (kind === "trigger") {
    if (entry.trigger.kind !== triggerKind) return null;
    const next: Record<string, unknown> = { ...entry.trigger };
    for (const [field, value] of Object.entries(typed)) next[field] = field === "poll" ? Number(value) : value;
    return { op: "trigger-update", operands: { entryId: entry.id, trigger: next } };
  }
  if (kind === "schedule") {
    const current = entry.trigger as unknown as { schedule?: Record<string, unknown>; title?: string; task?: string };
    const schedule = { ...current.schedule };
    for (const field of ["weekday", "hour", "minute"]) if (field in typed) schedule[field] = Number(typed[field]);
    if ("days" in typed) schedule.days = typed.days;
    const next = { ...entry.trigger, schedule, title: "title" in typed ? typed.title : current.title, task: "task" in typed ? typed.task : current.task };
    return { op: "trigger-update", operands: { entryId: entry.id, trigger: next } };
  }
  if (kind === "repository") {
    const text = String(typed.text ?? "");
    return text.trim() === "" ? null : { op: "repository-add", operands: { entryId: entry.id, repository: text } };
  }
  return null;
}
