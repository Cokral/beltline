import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { shellCases, sidebarState, type PreCaptureStep, type SnapshotCase } from "../caseTypes";

/** The demo's Issues to pull requests belt, whose Triage leads into the Complex and Simple routes. The contract golden
 *  archives it, so the first live belt there is a different one; the snapshot demo keeps it live. */
const editorBeltId = "issues";
const editorRoute = `/belts?belt=${encodeURIComponent(editorBeltId)}`;

/** Editor captures open through an explicit deep link; the bare Belts route now shows the browser. */
const editorCases: SnapshotCase[] = shellCases("belts", editorRoute).map(item => ({ ...item, readyMarker: ".belt-editor" }));

/**
 * A capture on the Belts page: the steps run against the demo daemon's first live belt, Issues to pull requests,
 * unless they pick another. Every step only changes the browser's draft or opens a form; none saves, so the shared
 * demo daemon is the same for every case and size.
 */
function beltsCase(id: string, steps: PreCaptureStep[], readyMarker: string, extra: Partial<SnapshotCase> = {}): SnapshotCase {
  return { id, route: editorRoute, sidebar: sidebarState({ navigationOpen: true }), readiness: "shell", steps, readyMarker, ...extra };
}

function browserCase(id: string): SnapshotCase {
  return { id, route: "/belts", sidebar: sidebarState({ navigationOpen: true }), readiness: "shell", steps: [], readyMarker: ".library-browser" };
}

const pick = (belt: string): PreCaptureStep => ({ kind: "click", target: { role: "button", name: `Open ${belt}`, exact: true } });
const open = (stage: string): PreCaptureStep => ({ kind: "click", target: { role: "button", name: stage } });
/** A stage form whose daemon context has arrived. */
const stageReady = '.stage-form[data-ready="true"]';

/** A belt chosen from the library by its name, then whatever more the case needs before its marker is checked. */
function beltCase(id: string, belt: string, steps: PreCaptureStep[] = [], readyMarker = ".belt-editor"): SnapshotCase {
  return beltsCase(id, [pick(belt), ...steps], readyMarker, { route: "/belts" });
}

/** Pi's catalog from the server's golden, with context sizes, for the model picker's context labels. */
function catalogWithContext(): string {
  const golden = JSON.parse(readFileSync(resolve(process.cwd(), "../daemon/testdata/golden/models.json"), "utf8")) as { choices: Array<Record<string, unknown>> };
  const sizes = [200_000, 1_000_000, 1_000_000, 250_000];
  return JSON.stringify({ ...golden, choices: golden.choices.map((choice, index) => ({ ...choice, contextTokens: sizes[index % sizes.length] })) });
}

const assistantGolden = JSON.parse(readFileSync(resolve(process.cwd(), "../daemon/testdata/golden/assistant.json"), "utf8")) as Record<string, any>;
const modelsGolden = JSON.parse(readFileSync(resolve(process.cwd(), "../daemon/testdata/golden/models.json"), "utf8")) as Record<string, any>;
const beltsAssistantColumns = ['[data-column="belts-editor"]', '[data-column="assistant"]'];

function assistantResponse(changes: Record<string, unknown> = {}): string {
  return JSON.stringify({ ...assistantGolden, ...changes });
}

function cursorModels(): string {
  return JSON.stringify({ ...modelsGolden, runner: "cursor", executable: "cursor-agent",
    catalog: modelsGolden.catalog ? { ...modelsGolden.catalog, runner: "cursor", executable: "cursor-agent" } : null });
}

const runningAssistant = assistantResponse({
  running: true,
  command: null,
  actions: [
    { id: "configuration", enabled: false, label: "Change configuration", reason: "Stop the assistant before changing its configuration." },
    { id: "start", enabled: false, label: "Start", reason: "The assistant is already running." },
    { id: "stop", enabled: true, label: "Stop", reason: null }
  ]
});

/** A save the daemon refuses, with problems located at the belt's name, its first trigger and an outcome, and one about the whole belt. */
const refusedSave = JSON.stringify({
  message: "The belt was not saved.",
  problems: ["Give the belt a name.", "Trigger id 1 is used more than once.", "An outcome goes to a stage that is not on the belt.", "The library changed while you were editing; read it again."],
  locations: [{ kind: "belt", field: "name" }, { kind: "trigger", id: "1" }, { kind: "outcome", id: "triage", field: "missing" }, null]
});

export const beltsCases: SnapshotCase[] = [
  browserCase("belts-browser"),
  ...editorCases,
  // Stage forms: approval, agent, Cursor runner, context labels, model pool, watch, a stage on a route, a nested stage.
  beltsCase("belts-stage-form", [open("Approve")], stageReady),
  beltsCase("belts-agent-stage-form", [open("Triage")], stageReady),
  beltsCase("belts-cursor-stage", [open("Triage"), { kind: "select", target: { role: "combobox", name: "Harness" }, value: "cursor" }], stageReady),
  // Codex's thinking levels, up to extra high and max, come from the demo daemon's Codex catalog.
  beltsCase("belts-codex-stage", [open("Triage"), { kind: "select", target: { role: "combobox", name: "Harness" }, value: "codex" }], stageReady),
  beltsCase("belts-context-model", [open("Triage")], '.stage-form .model-picker[data-model-state="ready"]',
    { apiResponses: [{ path: "/api/v1/models/pi", status: 200, body: catalogWithContext() }] }),
  beltsCase("belts-model-pool", [open("Triage"), { kind: "click", target: { role: "button", name: "Add a model" } }], ".alternate-row"),
  beltsCase("belts-watch-stage", [open("Merge")], ".watch-note"),
  // The editor's default belt has Spec on the line, not on a route, so the route case opens the nested belt, where Spec sits on a fork route.
  { ...beltCase("belts-route-stage", "Nested forks", [open("Spec")], '.stage-form select[aria-label="Route"]'), formAtTop: true },
  beltCase("belts-nested-stage", "Nested forks", [open("Glance")], stageReady),
  beltCase("belts-epic-watch", "Epics to tickets", [open("Watch the ticket")], ".watch-note"),
  beltsCase("belts-model-error", [open("Triage")], ".stage-form .model-error",
    { apiResponses: [{ path: "/api/v1/models/pi", status: 503, body: '{"message":"Catalog unavailable","problems":[]}' }], expectedText: "Catalog unavailable" }),
  beltsCase("belts-placeholder-help", [open("Triage")], ".stage-form .placeholder-chips"),
  // The "+" menu open on the first gap, with every agent in the demo library listed.
  beltsCase("belts-add-menu", [{ kind: "click", target: { selector: ".stage-line-row > .stage-line-piece:nth-of-type(1) .add-menu-plus" } }], ".add-menu-card"),

  beltsCase("belts-assistant-stopped", [], '[data-column="assistant"]', {
    sidebar: sidebarState({ navigationOpen: true, assistantOpen: true }),
    apiResponses: [
      { path: "/api/v1/assistant", status: 200, body: assistantResponse() },
      { path: "/api/v1/models/pi", status: 200, body: JSON.stringify(modelsGolden) }
    ],
    visibleColumns: beltsAssistantColumns
  }),
  beltsCase("belts-cursor-assistant", [], '[data-column="assistant"]', {
    sidebar: sidebarState({ navigationOpen: true, assistantOpen: true }),
    apiResponses: [
      { path: "/api/v1/assistant", status: 200, body: assistantResponse({ configuration: { backend: "cursor", model: "fixture-model", thinking: null } }) },
      { path: "/api/v1/models/cursor", status: 200, body: cursorModels() }
    ],
    visibleColumns: beltsAssistantColumns
  }),
  beltsCase("belts-assistant-running-demo", [], '[data-column="assistant"]', {
    sidebar: sidebarState({ navigationOpen: true, assistantOpen: true }),
    apiResponses: [
      { path: "/api/v1/assistant", status: 200, body: runningAssistant },
      { path: "/api/v1/models/pi", status: 200, body: JSON.stringify(modelsGolden) }
    ],
    expectedText: "Simulated conversation. No real agent is launched in demo mode.",
    visibleColumns: beltsAssistantColumns
  }),
  beltsCase("belts-assistant-running-terminal", [], '[data-column="assistant"] .terminal-screen', {
    sidebar: sidebarState({ navigationOpen: true, assistantOpen: true }),
    apiResponses: [
      { path: "/api/v1/assistant", status: 200, body: JSON.stringify({ ...JSON.parse(runningAssistant), command: ["pi"] }) },
      { path: "/api/v1/models/pi", status: 200, body: JSON.stringify(modelsGolden) }
    ],
    visibleColumns: beltsAssistantColumns
  }),
  beltsCase("belts-assistant-stage-open", [open("Approve")], stageReady, {
    sidebar: sidebarState({ navigationOpen: true, assistantOpen: true }),
    apiResponses: [
      { path: "/api/v1/assistant", status: 200, body: assistantResponse() },
      { path: "/api/v1/models/pi", status: 200, body: JSON.stringify(modelsGolden) }
    ],
    visibleColumns: beltsAssistantColumns
  }),

  // The outcomes of each kind of stage, and an outcome that takes both routes of the fork after Triage.
  beltsCase("belts-outcomes-triage", [open("Triage")], ".stage-form .outcomes-editor"),
  beltsCase("belts-outcomes-verify", [open("Verify")], ".stage-form .outcomes-editor"),
  beltsCase("belts-outcomes-ship", [open("Ship")], ".stage-form .outcomes-editor"),
  beltsCase("belts-outcomes-watch", [open("Merge")], ".stage-form .outcomes-editor"),
  beltsCase("belts-outcomes-both-routes", [
    open("Triage"),
    { kind: "select", target: { role: "combobox", name: "Outcome 1 then" }, value: "route" },
    { kind: "click", target: { role: "checkbox", name: "Complex" } },
    { kind: "click", target: { role: "checkbox", name: "Simple" } }
  ], ".stage-form .route-targets"),

  // Triggers.
  beltCase("belts-trigger-reviews", "Pull request reviews", [], ".belt-triggers"),
  beltCase("belts-trigger-jira", "Payments tickets", [], ".belt-triggers"),
  // Draft-only fills: the overrides and the shared base are typed, never saved, so the demo daemon is left as it was.
  beltCase("belts-repository-bases", "Payments tickets", [
    { kind: "fill", target: { role: "textbox", name: "Base branch" }, value: "develop" },
    { kind: "fill", target: { role: "textbox", name: "acme/billing" }, value: "release/billing" },
    { kind: "fill", target: { role: "textbox", name: "acme/infra" }, value: "release/infra" }
  ], ".belt-repository-bases"),
  beltCase("belts-trigger-schedule", "Nightly test sweep", [], ".schedule-settings"),
  beltCase("belts-trigger-manual", "Tasks", [], ".belt-triggers"),
  beltsCase("belts-task-sheet", [pick("Tasks"), { kind: "click", target: { role: "button", name: "Start a task…" } }], '[role="dialog"]', { route: "/belts" }),

  // Belts as a whole: paused, warned while new and while edited, nested forks, the epic and the policies.
  beltCase("belts-paused", "Dependency bumps", [], ".belt-editor"),
  beltsCase("belts-warned-new", [{ kind: "click", target: { role: "button", name: "New belt", exact: false } }], ".belt-warnings",
    { route: "/belts", expectedText: "Saving will pause this belt until its warnings are resolved." }),
  beltCase("belts-warned-edit", "Docs fixes", [{ kind: "click", target: { role: "button", name: "Remove trigger 1 GitHub issues" } }], ".belt-warnings"),
  beltCase("belts-nested-forks", "Nested forks", [], ".stage-line-card"),
  beltCase("belts-epic", "Epics to tickets", [], ".stage-line-card"),
  beltCase("belts-policies", "Issues to pull requests", [], ".belt-policies"),

  // Save: unavailable with its reason, and refused with located problems.
  beltsCase("belts-save-disabled", [], ".belt-editor", { expectedText: "Shared by every trigger" }),
  beltsCase("belts-save-422", [
    { kind: "fill", target: { role: "textbox", name: "Belt name" }, value: "" },
    { kind: "click", target: { role: "button", name: "Save" } }
  ], ".belt-problem", { apiResponses: [{ path: "/api/v1/belts/save", status: 422, body: refusedSave }], expectedText: "The belt was not saved." })
];
