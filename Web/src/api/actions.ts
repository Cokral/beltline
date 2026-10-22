/** The global controls the shell knows how to submit. The daemon may offer others, which stay disabled. */
export const globalActionIds = ["pause", "afk", "check-now"] as const;
export type GlobalActionId = (typeof globalActionIds)[number];

export function isGlobalActionId(id: string): id is GlobalActionId {
  return (globalActionIds as readonly string[]).includes(id);
}

/** The part of a daemon action the shell reads to decide whether to offer a command: its id and whether the daemon enables it. */
export interface OfferedAction {
  readonly id: string;
  readonly enabled: boolean;
}

/** Whether the daemon's action list offers the named action and enables it; a missing action is not offered. */
export function isActionEnabled(actions: readonly OfferedAction[], id: string): boolean {
  return actions.find(action => action.id === id)?.enabled === true;
}
