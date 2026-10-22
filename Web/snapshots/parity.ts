import { beltsParity, type ParityEntry } from "./belts-parity";
import { overviewParity } from "./overview-parity";

/**
 * Every capture the retired native app's snapshot run saved, frozen when that app was removed, with the web case that replaces it or the reason none does. parity.test.ts
 * checks that every name has exactly one entry, that a captured entry names a registered case, and that every other
 * entry gives its reason. A state the web app could not reach at all would have been a parity gap, not a reason.
 */
export const nativeCaptures = [
  "1-overview", "2-floor", "2-floor-hover", "2-floor-hover-other", "2-floor-pinned", "2a-factory-station", "2b-copy",
  "2m-metro", "2m-metro-hover", "2m-metro-pinned", "2n-nested-floor",
  "3-work", "3b-work-grouped", "3c-work-folded", "3d-work-status", "3e-work-epic", "3f-work-watching", "3g-work-epic-watching",
  "3h-work-watcher-only",
  "4-run", "4-run-wide", "4b-run-waiting", "4b2-run-afk", "4b3-run-afk-sessions", "4b4-run-afk-restored", "4b5-run-parked",
  "4b6-run-parked-sessions", "4b7-run-unparked", "4c-run-review", "4d-run-watch", "4d2-run-multi-watch", "4d3-run-epic",
  "4e-run-route", "4f-run-wrapping-1440-closed", "4f-run-wrapping-1440-open", "4f-run-wrapping-2560-closed",
  "4f-run-wrapping-2560-open", "4n-nested-run", "4o-open-links", "4p-pause-incomplete", "4p-run-paused", "4p-run-paused-narrow",
  "4p-run-paused-sessions",
  "5-run-sessions", "5-run-sessions-wide", "5b-run-held", "5c-cursor-adjust", "5c-run-resized", "5c-stage-adjust",
  "5d-navigation-rail", "5d-stage-adjusted", "5e-run-constrained", "5e-send-to", "5f-run-width-restored", "5f-send-to-again",
  "5g-run-default-widths", "5g-send-to-gate", "5h-run-estimated", "5h-send-to-watch", "5i-previous-attempt",
  "5j-running-attempt", "5k-copilot-continued",
  "6-belts", "6b-belt-stage", "6c-belt-stage", "6c-context-model", "6c-cursor-stage", "6c-model-pool", "6c2-belt-watch",
  "6c3-belt-route", "6c4-belt-epic", "6c5-belt-epic-watch", "6d-trigger-reviews", "6e-trigger-jira", "6f-trigger-schedule",
  "6g-trigger-manual", "6h-belts-agent", "6h-cursor-assistant", "6i-belts-agent-running", "6j-belts-agent-stage",
  "6k-agents-agent", "6l-agents-agent-stopped", "6n-nested-belt", "6n-nested-narrow", "6n-nested-restore", "6n-nested-stage",
  "6n-nested-wide", "6w-paused", "6w-paused-narrow", "6w-restore-size", "6w-setup-edit", "6w-setup-new", "6w-warned-edit",
  "6w-warned-new",
  "7-agents", "7-cursor-agent", "7b-agents-archived", "7c-agents-revealed", "7d-agents-back-hidden", "7e-settings",
  "7f-settings-factories", "7g-afk", "7g-settings-paused", "7h-afk-run", "7h-overview-paused", "7i-afk-on", "7i-work-paused",
  "7j-afk-deciding-run", "7j-settings-continued", "7k-afk-decided", "7l-overrule", "7m-overrule-row",
  "8-markdown-528", "8-markdown-1128", "8-markdown-2248", "8-outcomes-approve", "8-outcomes-ship", "8-outcomes-triage",
  "8-outcomes-verify", "8-outcomes-watch", "8b-belts-archived", "8b-outcomes-both-routes", "8c-belts-revealed",
  "8d-belts-back-hidden", "8e-floor-archived-idle", "8f-floor-archived-active", "8g-work-archived", "8h-run-archived",
  "8i-belts-restore",
  "9-machines", "9-placeholder-help-agent", "9-placeholder-help-belt", "9-placeholder-help-run",
  "10-codex-adjust-extra-high", "10-codex-adjust-max", "10-codex-agent-max", "10-codex-assistant", "10-codex-belt-max",
  "10-market", "10-market-preview", "10-market-preview-agent", "10-market-preview-details", "10-market-preview-loading",
  "10-market-preview-stage", "10-market-settings", "10-market-share", "10-market-share-belt", "10-market-update",
  "10-market-updated-agent", "10-market-updated-agent-narrow", "10-market-updated-belt"
] as const;

/** Commands that change the one demo daemon every case and size shares stay out of the matrix. */
const savedCommand = (what: string, tests: string) =>
  `${what} is a saved command, so it would change the one demo daemon every case and size shares. ${tests}`;
const archive = savedCommand("Archiving and restoring", "The archive section, its fold and Restore are covered by BeltsPage.test.tsx and AgentsPage.test.tsx.");
const windowSize = "A window-size and column-width step of the native run. Column widths are browser preferences; the drag, clamp and "
  + "double-click reset are covered by RunPage.test.tsx and preferences.test.ts, and every case is captured at 1080, 1440 and 2560 wide.";
const housekeeping = "A housekeeping step between native captures (it resets the window or the selection), not a state of its own.";

const pageEntries: readonly ParityEntry[] = [
  { native: "2b-copy", status: "not-captured", reason: savedCommand("Copy belt", "Copying is covered by BeltsCommands.test.tsx and BeltsDraftLifecycle.test.tsx.") },

  { native: "3-work", status: "captured", caseId: "work-open" },
  { native: "3b-work-grouped", status: "captured", caseId: "work-grouped" },
  { native: "3c-work-folded", status: "not-captured", reason: "A fold is a browser preference toggled by a group header; folding and its persistence are covered by Work.test.tsx." },
  { native: "3d-work-status", status: "captured", caseId: "work-status" },
  { native: "3e-work-epic", status: "captured", caseId: "work-epic" },
  { native: "3f-work-watching", status: "captured", caseId: "work-watching" },
  { native: "3g-work-epic-watching", status: "captured", caseId: "work-epic", note: "Grouped by epic, with the Watching table below." },
  { native: "3h-work-watcher-only", status: "not-captured", reason: "The native case filtered the demo down to its watch runs in memory; the daemon's demo has active runs, so a filter cannot empty the main table. Work.test.tsx covers the watcher-only table." },

  { native: "4-run", status: "captured", caseId: "run-open" },
  { native: "4-run-wide", status: "captured", caseId: "run-open", note: "The 2560x1440 capture." },
  { native: "4b-run-waiting", status: "captured", caseId: "run-waiting" },
  { native: "4b2-run-afk", status: "not-captured", reason: savedCommand("A run's AFK mode", "No web test covers the run AFK sheet yet.") },
  { native: "4b3-run-afk-sessions", status: "not-captured", reason: savedCommand("A run's AFK mode", "The Sessions panel is captured by run-wide.") },
  { native: "4b4-run-afk-restored", status: "not-captured", reason: savedCommand("A run's AFK mode", "Restoring it is the same command.") },
  { native: "4b5-run-parked", status: "captured", caseId: "run-parked", note: "The demo's seeded parked run." },
  { native: "4b6-run-parked-sessions", status: "captured", caseId: "run-parked-sessions" },
  { native: "4b7-run-unparked", status: "not-captured", reason: savedCommand("Unpark", "No web test covers park and unpark yet; run-parked captures the parked state.") },
  { native: "4c-run-review", status: "captured", caseId: "run-review" },
  { native: "4d-run-watch", status: "captured", caseId: "run-watch" },
  { native: "4d2-run-multi-watch", status: "captured", caseId: "run-multi-watch" },
  { native: "4d3-run-epic", status: "captured", caseId: "run-watch", note: "The epic's ticket watch is the demo's first running watch." },
  { native: "4e-run-route", status: "captured", caseId: "run-route" },
  { native: "4f-run-wrapping-1440-closed", status: "captured", caseId: "run-wrapping-closed" },
  { native: "4f-run-wrapping-1440-open", status: "captured", caseId: "run-wrapping-open" },
  { native: "4f-run-wrapping-2560-closed", status: "captured", caseId: "run-wrapping-closed" },
  { native: "4f-run-wrapping-2560-open", status: "captured", caseId: "run-wrapping-open" },
  { native: "4n-nested-run", status: "captured", caseId: "run-nested" },
  { native: "4o-open-links", status: "captured", caseId: "run-open-links" },
  { native: "4p-pause-incomplete", status: "captured", caseId: "run-pause-incomplete" },
  { native: "4p-run-paused", status: "captured", caseId: "run-paused" },
  { native: "4p-run-paused-narrow", status: "captured", caseId: "run-paused", note: "The 1080x680 capture." },
  { native: "4p-run-paused-sessions", status: "captured", caseId: "run-paused-sessions" },

  { native: "5-run-sessions", status: "captured", caseId: "run-sessions-click" },
  { native: "5-run-sessions-wide", status: "captured", caseId: "run-wide" },
  { native: "5b-run-held", status: "captured", caseId: "run-held" },
  { native: "5c-cursor-adjust", status: "not-captured", reason: "The demo has no Cursor stage; the Adjust stage sheet is captured by run-stage-adjust and its runner choices come from the run's model admission, covered by RunControls.test.tsx." },
  { native: "5c-run-resized", status: "not-captured", reason: windowSize },
  { native: "5c-stage-adjust", status: "captured", caseId: "run-stage-adjust" },
  { native: "5d-navigation-rail", status: "captured", caseId: "run-closed", note: "The navigation hidden." },
  { native: "5d-stage-adjusted", status: "not-captured", reason: savedCommand("Adjusting a stage", "RunControls.test.tsx covers the sheet and the command it sends.") },
  { native: "5e-run-constrained", status: "not-captured", reason: windowSize },
  { native: "5e-send-to", status: "captured", caseId: "run-send-to" },
  { native: "5f-run-width-restored", status: "not-captured", reason: windowSize },
  { native: "5f-send-to-again", status: "not-captured", reason: "Run <stage> again opens the same sheet as Send to… with the current stage chosen; RunControls.test.tsx covers it." },
  { native: "5g-run-default-widths", status: "not-captured", reason: windowSize },
  { native: "5g-send-to-gate", status: "not-captured", reason: "A gate destination is another option in the Send to… sheet that run-send-to captures; RunControls.test.tsx covers changing the destination." },
  { native: "5h-run-estimated", status: "not-captured", reason: "The estimated cost line is the daemon's; SessionsSidebar.test.tsx covers an attempt's token and estimate detail and the Run header shows the run's cost in every run case." },
  { native: "5h-send-to-watch", status: "not-captured", reason: "A watch destination is another option in the Send to… sheet that run-send-to captures; RunControls.test.tsx covers changing the destination." },
  { native: "5i-previous-attempt", status: "not-captured", reason: "An earlier attempt is chosen by URL; RunPage.test.tsx covers keeping a selected historical attempt, and run-wrapping-* capture a chosen attempt." },
  { native: "5j-running-attempt", status: "not-captured", reason: "The demo's running agents finish in the simulated runner before a capture; No web test covers a running attempt's panel yet." },
  { native: "5k-copilot-continued", status: "not-captured", reason: "Needs a real Copilot session continued in tmux; SessionsSidebar.test.tsx covers Continue and the writable terminal it attaches to." },

  { native: "6n-nested-restore", status: "not-captured", reason: housekeeping },
  { native: "6w-restore-size", status: "not-captured", reason: housekeeping },
  { native: "6w-setup-edit", status: "not-captured", reason: housekeeping },
  { native: "6w-setup-new", status: "not-captured", reason: housekeeping },

  { native: "7-agents", status: "captured", caseId: "agents-open" },
  { native: "7-cursor-agent", status: "captured", caseId: "agents-cursor" },
  { native: "7b-agents-archived", status: "captured", caseId: "agents-archived" },
  { native: "7c-agents-revealed", status: "captured", caseId: "agents-revealed" },
  { native: "7d-agents-back-hidden", status: "captured", caseId: "agents-back-hidden" },
  { native: "7e-settings", status: "captured", caseId: "native-7e-settings" },
  { native: "7f-settings-factories", status: "captured", caseId: "native-7f-settings-factories" },
  { native: "7g-afk", status: "captured", caseId: "native-7g-afk" },
  { native: "7g-settings-paused", status: "captured", caseId: "native-7g-settings-paused" },
  { native: "7h-afk-run", status: "captured", caseId: "run-afk-approved" },
  { native: "7i-afk-on", status: "captured", caseId: "native-7i-afk-on" },
  { native: "7i-work-paused", status: "captured", caseId: "work-paused" },
  { native: "7j-afk-deciding-run", status: "not-captured", reason: "Needs a live AFK agent session deciding; the demo's decisions are already made. No web test covers the deciding state yet." },
  { native: "7j-settings-continued", status: "captured", caseId: "native-7j-settings-continued" },
  { native: "7k-afk-decided", status: "captured", caseId: "native-7k-afk-decided" },
  { native: "7l-overrule", status: "not-captured", reason: "The daemon offers Overrule AFK only on a run whose latest AFK decision can still be overruled, which the demo does not seed; RunControls.test.tsx opens the sheet and sends the overrule." },
  { native: "7m-overrule-row", status: "captured", caseId: "native-7m-overrule-row" },

  { native: "8-markdown-528", status: "captured", caseId: "run-wrapping-closed", note: "The long Markdown output, checked for overflow at every size." },
  { native: "8-markdown-1128", status: "captured", caseId: "run-wrapping-closed" },
  { native: "8-markdown-2248", status: "captured", caseId: "run-wrapping-open" },
  { native: "8-outcomes-approve", status: "captured", caseId: "belts-stage-form" },
  { native: "8-outcomes-ship", status: "captured", caseId: "belts-outcomes-ship" },
  { native: "8-outcomes-triage", status: "captured", caseId: "belts-outcomes-triage" },
  { native: "8-outcomes-verify", status: "captured", caseId: "belts-outcomes-verify" },
  { native: "8-outcomes-watch", status: "captured", caseId: "belts-outcomes-watch" },
  { native: "8b-outcomes-both-routes", status: "captured", caseId: "belts-outcomes-both-routes" },
  { native: "8b-belts-archived", status: "not-captured", reason: archive },
  { native: "8c-belts-revealed", status: "not-captured", reason: archive },
  { native: "8d-belts-back-hidden", status: "not-captured", reason: archive },
  { native: "8g-work-archived", status: "not-captured", reason: archive },
  { native: "8h-run-archived", status: "not-captured", reason: archive },
  { native: "8i-belts-restore", status: "not-captured", reason: archive },

  { native: "9-machines", status: "captured", caseId: "gallery" },
  { native: "9-placeholder-help-agent", status: "captured", caseId: "agents-open", note: "The agent editor lists its placeholders." },
  { native: "9-placeholder-help-run", status: "captured", caseId: "run-stage-adjust", note: "The Adjust stage sheet lists the run's placeholders." },

  { native: "10-codex-adjust-extra-high", status: "not-captured", reason: "The demo has no Codex stage to adjust; Codex's thinking levels come from the daemon's catalog and are captured by belts-codex-stage and agents-codex." },
  { native: "10-codex-adjust-max", status: "not-captured", reason: "The demo has no Codex stage to adjust; Codex's thinking levels come from the daemon's catalog and are captured by belts-codex-stage and agents-codex." },
  { native: "10-codex-agent-max", status: "captured", caseId: "agents-codex" },
  { native: "10-codex-assistant", status: "captured", caseId: "belts-cursor-assistant", note: "The assistant's runner and model picker; its choices come from the daemon's catalog for the chosen runner." },
  { native: "10-codex-belt-max", status: "captured", caseId: "belts-codex-stage" },

  { native: "10-market", status: "captured", caseId: "library-open", note: "The Market is the Library page now." },
  { native: "10-market-preview", status: "captured", caseId: "library-compare", note: "A shared item's files beside your own copy replace the package preview." },
  { native: "10-market-preview-agent", status: "captured", caseId: "library-compare", note: "The compare sheet shows an agent's file the same way." },
  { native: "10-market-preview-details", status: "captured", caseId: "library-compare", note: "The compare sheet lists every file of the item." },
  { native: "10-market-preview-loading", status: "not-captured", reason: "A transient state; LibraryPage.test.tsx covers the sections while they load." },
  { native: "10-market-preview-stage", status: "not-captured", reason: "A package preview's stage line has no Library counterpart; the Belts editor draws each saved belt's stages." },
  { native: "10-market-settings", status: "captured", caseId: "native-7e-settings", note: "The library repositories are set in Settings." },
  { native: "10-market-share", status: "captured", caseId: "library-share-belt", note: "The same share sheet opens for agents; ShareSheet.test.tsx covers both." },
  { native: "10-market-share-belt", status: "captured", caseId: "library-share-belt" },
  { native: "10-market-update", status: "not-captured", reason: savedCommand("Installing a shared item", "LibraryPage.test.tsx covers the install and its answer.") },
  { native: "10-market-updated-agent", status: "not-captured", reason: savedCommand("Installing a shared item", "LibraryPage.test.tsx covers the install and its answer.") },
  { native: "10-market-updated-agent-narrow", status: "not-captured", reason: savedCommand("Installing a shared item", "LibraryPage.test.tsx covers the install and its answer.") },
  { native: "10-market-updated-belt", status: "not-captured", reason: savedCommand("Installing a shared item", "LibraryPage.test.tsx covers the install and its answer.") }
];

const listed = new Set<string>(nativeCaptures);

/** One entry per native capture. The Overview and Belts manifests keep their own entries for the names they share. */
export const parity: readonly ParityEntry[] = [
  ...overviewParity.filter(entry => listed.has(entry.native)),
  ...beltsParity.filter(entry => listed.has(entry.native)),
  ...pageEntries
];
