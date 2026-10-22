/**
 * The Belts parity case-definition manifest. Each entry is a state the approved parity specifications name. A "captured"
 * entry has an executable case definition in cases/belts.ts; "owned" is a state another ticket owns, with that ticket named; and
 * "not-captured" is a capture this change does not produce, with the reason. Sizes are the default matrix:
 * 1080x680 is the narrow size and 2560x1440 the wide one. This run writes no PNG captures.
 */
export type ParityEntry =
  | { native: string; status: "captured"; caseId: string; note?: string }
  | { native: string; status: "owned"; owner: string; reason: string }
  | { native: string; status: "not-captured"; reason: string };

/** Why the archive states are not captured. The owner chose to keep them out of the matrix rather than start a daemon per case. */
const archiveReason = "Archiving and restoring are saved commands, so they change the one demo daemon every case and size shares. "
  + "These states stay out of the matrix instead of the harness starting a fresh daemon per case; "
  + "the archive section, its fold and Restore are covered by the page tests.";

export const beltsParity: readonly ParityEntry[] = [
  { native: "6-belts", status: "captured", caseId: "belts-open" },
  { native: "6b-belt-stage", status: "captured", caseId: "belts-stage-form", note: "An approval stage's form." },
  { native: "6c-belt-stage", status: "captured", caseId: "belts-agent-stage-form" },
  { native: "6c-cursor-stage", status: "captured", caseId: "belts-cursor-stage", note: "Triage switched to the Cursor runner in the form; the demo has no Cursor stage." },
  { native: "6c-context-model", status: "captured", caseId: "belts-context-model", note: "Pi's catalog with context sizes added, as the demo catalog has none." },
  { native: "6c-model-pool", status: "captured", caseId: "belts-model-pool" },
  { native: "6c2-belt-watch", status: "captured", caseId: "belts-watch-stage" },
  { native: "6c3-belt-route", status: "captured", caseId: "belts-route-stage", note: "Spec on the Complex route, with its route menu." },
  { native: "6n-nested-belt", status: "captured", caseId: "belts-nested-forks" },
  { native: "6n-nested-stage", status: "captured", caseId: "belts-nested-stage", note: "Glance, on a route of the nested fork." },
  { native: "6n-nested-narrow", status: "captured", caseId: "belts-nested-forks", note: "The 1080x680 capture of the nested belt." },
  { native: "6n-nested-wide", status: "captured", caseId: "belts-nested-forks", note: "The 2560x1440 capture of the nested belt." },
  { native: "6c4-belt-epic", status: "captured", caseId: "belts-epic" },
  { native: "6c5-belt-epic-watch", status: "captured", caseId: "belts-epic-watch" },
  { native: "6d-trigger-reviews", status: "captured", caseId: "belts-trigger-reviews" },
  { native: "6e-trigger-jira", status: "captured", caseId: "belts-trigger-jira" },
  { native: "6f-trigger-schedule", status: "captured", caseId: "belts-trigger-schedule" },
  { native: "6g-trigger-manual", status: "captured", caseId: "belts-trigger-manual" },
  { native: "6w-warned-new", status: "captured", caseId: "belts-warned-new" },
  { native: "6w-warned-edit", status: "captured", caseId: "belts-warned-edit", note: "Docs fixes with its only trigger removed from the draft." },
  { native: "6w-paused", status: "captured", caseId: "belts-paused" },
  { native: "6w-paused-narrow", status: "captured", caseId: "belts-paused", note: "The 1080x680 capture of the paused belt." },
  { native: "archive, revealed", status: "not-captured", reason: archiveReason },
  { native: "archive, back-hidden", status: "not-captured", reason: archiveReason },
  { native: "archive, restore", status: "not-captured", reason: archiveReason },
  { native: "triggers-inlet", status: "captured", caseId: "belts-trigger-jira", note: "The trigger section of a belt's editor." },
  { native: "triggers-editor", status: "captured", caseId: "belts-trigger-manual", note: "The trigger editor of a manual belt." },
  { native: "task-sheet", status: "captured", caseId: "belts-task-sheet" },
  { native: "outcomes: triage", status: "captured", caseId: "belts-outcomes-triage" },
  { native: "outcomes: approve", status: "captured", caseId: "belts-stage-form" },
  { native: "outcomes: verify", status: "captured", caseId: "belts-outcomes-verify" },
  { native: "outcomes: ship", status: "captured", caseId: "belts-outcomes-ship" },
  { native: "outcomes: watch", status: "captured", caseId: "belts-outcomes-watch" },
  { native: "outcomes: both-routes", status: "captured", caseId: "belts-outcomes-both-routes", note: "Triage's first outcome set to take Complex and Simple in the draft." },
  { native: "9-placeholder-help-belt", status: "captured", caseId: "belts-placeholder-help" },
  { native: "disabled Save", status: "captured", caseId: "belts-save-disabled" },
  { native: "save 422", status: "captured", caseId: "belts-save-422", note: "The daemon's refusal is a fixture, so the demo is not saved." },
  { native: "model error", status: "captured", caseId: "belts-model-error" },
  // These six native assistant states now have deterministic case definitions; this work does not execute PNG captures.
  { native: "6h-belts-agent", status: "captured", caseId: "belts-assistant-stopped" },
  { native: "6h-cursor-assistant", status: "captured", caseId: "belts-cursor-assistant", note: "The daemon fixture selects Cursor and supplies Cursor model choices." },
  { native: "6i-belts-agent-running", status: "captured", caseId: "belts-assistant-running-demo", note: "The deterministic running fixture has command:null, so no terminal socket opens." },
  { native: "6j-belts-agent-stage", status: "captured", caseId: "belts-assistant-stage-open", note: "The Approve stage form stays beside the Assistant sidebar." },
  { native: "6k-agents-agent", status: "captured", caseId: "agents-assistant-running", note: "The deterministic Agents fixture shows the running demo conversation." },
  { native: "6l-agents-agent-stopped", status: "captured", caseId: "agents-assistant-stopped" },
  { native: "marketplace share", status: "captured", caseId: "library-share-belt", note: "Share opens the library's share sheet for the saved version." },
];
