import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { sidebarState, type SnapshotCase, type RunSelector } from "../caseTypes";

const sessionsPanel = '[data-column="sessions"]';
const sessionsReady = '[data-column="sessions"][data-ready="true"]';

const issues = (number: number): RunSelector => ({ beltId: "issues", number });
const runRoute = (runId: string) => `/runs/${encodeURIComponent(runId)}`;
const stageRoute = (runId: string, stage: string, attempt?: string) =>
  `/runs/${encodeURIComponent(runId)}?stage=${encodeURIComponent(stage)}${attempt ? `&attempt=${encodeURIComponent(attempt)}` : ""}`;

/** The golden run view of acme/web#61, which shipped on the simple route, so its metro line has no current station. */
interface MetroViewGolden {
  run: { state: string; stageId: string };
  stages: { id: string; step: string; status: { face: string; look: string; text: string }; compactStatus: { face: string; look: string; text: string } }[];
  metro: {
    segments: { state: string; path: { subpaths: { x: number; y: number }[][] } }[];
    stations: { id: string; x: number; state: string; look: string; status: string }[];
  };
}

const metroGolden = JSON.parse(readFileSync(resolve(process.cwd(), "../daemon/testdata/golden/views-run-metro.json"), "utf8")) as MetroViewGolden;
const metroRun: RunSelector = { beltId: "issues", number: 61 };
const metroViewPath = `/api/v1/views/run/${encodeURIComponent("issues:acme/web#61")}`;

/** The golden view with verify running: the stations and line after it are still to come, and it is the current station. */
function runningMetroView(): string {
  const view = structuredClone(metroGolden);
  const verify = view.metro.stations.find(station => station.id === "verify");
  if (!verify) throw new Error("the metro golden has no verify station");
  view.run.state = "running";
  view.run.stageId = "verify";
  for (const station of view.metro.stations) {
    if (station.state === "done" && station.x > verify.x) Object.assign(station, { state: "todo", look: "plain", status: "" });
  }
  Object.assign(verify, { state: "current", look: "running", status: "RUNNING 2m" });
  for (const segment of view.metro.segments) {
    if (segment.state === "done" && segment.path.subpaths[0]![0]!.x >= verify.x) segment.state = "todo";
  }
  for (const panel of view.stages) {
    if (panel.id === "verify") {
      panel.step = "current";
      Object.assign(panel.status, { look: "running", text: "RUNNING 2m" });
      Object.assign(panel.compactStatus, { look: "running", text: "RUNNING 2m" });
    }
  }
  return JSON.stringify(view);
}

/**
 * The Run page draws the daemon-provided belt (boxes by default), stage panel, output and artifacts, so its cases wait on
 * the Run content itself rather than the old resource scaffold. The default demo run is the seeded
 * `acme/platform#139` run; a case can name a different seeded run with `run` and the harness resolves
 * it from `/api/v1/runs`. Its last saved agent output is `spec-1`, which the demo seeds with long
 * Markdown when the snapshot daemon starts.
 */
export const runCases: SnapshotCase[] = [
  { id: "run-open", route: runRoute, sidebar: sidebarState({ navigationOpen: true, sessionsOpen: false }), readiness: "run" },
  {
    // The metro look with a finished station selected: its glyph is blue, and the button draws no wash.
    id: "run-metro-selected",
    route: (runId: string) => stageRoute(runId, "triage"),
    run: metroRun,
    sidebar: sidebarState({ navigationOpen: true, sessionsOpen: false }),
    readiness: "run",
    reducedMotion: "reduce",
    apiResponses: [{ path: metroViewPath, status: 200, body: JSON.stringify(metroGolden) }],
    metroCheck: { stage: "triage", current: false }
  },
  {
    // The metro look with the running current station selected: it keeps its gold glyph, halo and pulse.
    id: "run-metro-current-selected",
    route: (runId: string) => stageRoute(runId, "verify"),
    run: metroRun,
    sidebar: sidebarState({ navigationOpen: true, sessionsOpen: false }),
    readiness: "run",
    reducedMotion: "reduce",
    apiResponses: [{ path: metroViewPath, status: 200, body: runningMetroView() }],
    metroCheck: { stage: "verify", current: true }
  },
  {
    // The machines look: the same belt as machines on a track, from the machines golden at any width.
    id: "run-machines",
    route: runRoute,
    run: metroRun,
    sidebar: sidebarState({ navigationOpen: true, sessionsOpen: false }),
    readiness: "run",
    apiResponses: [{ path: metroViewPath, status: 200, body: readFileSync(resolve(process.cwd(), "../daemon/testdata/golden/views-run-machines.json"), "utf8") }]
  },
  { id: "run-closed", route: runRoute, sidebar: sidebarState({ navigationOpen: false, sessionsOpen: false }), readiness: "run" },
  {
    // A new snapshot starts with Sessions closed, clicks the real button, and captures the panel once it is ready.
    id: "run-sessions-click",
    route: runRoute,
    sidebar: sidebarState({ navigationOpen: true, sessionsOpen: false }),
    readiness: "run",
    steps: [
      { kind: "click", target: { role: "button", name: "Sessions", exact: true } },
      { kind: "wait", target: { selector: sessionsReady }, state: "visible" }
    ],
    readyMarker: sessionsReady,
    expectedPreferences: { sessionsOpen: true },
    visibleColumns: [sessionsPanel]
  },
  {
    id: "run-wide",
    route: runRoute,
    sidebar: sidebarState({ navigationOpen: true, sessionsOpen: true }),
    readiness: "run",
    readyMarker: sessionsReady
  },
  {
    // The long Markdown fixture, with Sessions closed. capture.spec.ts also runs it, and the open case below,
    // at 1080×680, 1440×900 and 2560×1440 for overflow assertions without a screenshot.
    id: "run-wrapping-closed",
    route: (runId: string) => stageRoute(runId, "spec", "spec-1"),
    sidebar: sidebarState({ navigationOpen: true, sessionsOpen: false }),
    readiness: "run"
  },
  {
    id: "run-wrapping-open",
    route: (runId: string) => stageRoute(runId, "spec", "spec-1"),
    sidebar: sidebarState({ navigationOpen: true, sessionsOpen: true }),
    readiness: "run",
    readyMarker: sessionsReady
  },
  {
    // The floating run menu (controls, then links), so its bounds are visible next to the page it floats over.
    id: "run-controls-open",
    route: runRoute,
    sidebar: sidebarState({ navigationOpen: true, sessionsOpen: false }),
    readiness: "run",
    steps: [
      { kind: "click", target: { role: "button", name: "Run actions", exact: true } },
      { kind: "wait", target: { selector: ".run-menu-card" }, state: "visible" }
    ],
    readyMarker: ".run-menu-card"
  },
  {
    id: "run-reduced-motion",
    route: runRoute,
    sidebar: sidebarState({ navigationOpen: true, sessionsOpen: false }),
    readiness: "run",
    reducedMotion: "reduce"
  },
  // Distinct native Run states. Each names its seeded demo run so the capture fails rather than
  // silently replacing a fixture.
  {
    // 4p-run-paused: the paused mid-triage run #152.
    id: "run-paused",
    route: runRoute,
    run: issues(152),
    sidebar: sidebarState({ navigationOpen: true, sessionsOpen: false }),
    readiness: "run"
  },
  {
    // 4p-run-paused-sessions: the same paused run with the Sessions panel open.
    id: "run-paused-sessions",
    route: runRoute,
    run: issues(152),
    sidebar: sidebarState({ navigationOpen: true, sessionsOpen: true }),
    readiness: "run",
    readyMarker: sessionsReady
  },
  {
    // 4p-pause-incomplete: the security run #93 whose pause did not confirm.
    id: "run-pause-incomplete",
    route: runRoute,
    run: { beltId: "security", number: 93 },
    sidebar: sidebarState({ navigationOpen: true, sessionsOpen: false }),
    readiness: "run"
  },
  {
    // 4b-run-waiting: the waiting run #88 (the pause seed's waiting state).
    id: "run-waiting",
    route: runRoute,
    run: { beltId: "security", number: 88 },
    sidebar: sidebarState({ navigationOpen: true, sessionsOpen: false }),
    readiness: "run"
  },
  {
    // 4b5-run-parked / 4b6-run-parked-sessions: the demo's parked run #153. The native cases park a
    // waiting run through the coordinator; the browser capture reads the seeded parked run instead.
    id: "run-parked",
    route: runRoute,
    run: issues(153),
    sidebar: sidebarState({ navigationOpen: true, sessionsOpen: false }),
    readiness: "run"
  },
  {
    id: "run-parked-sessions",
    route: runRoute,
    run: issues(153),
    sidebar: sidebarState({ navigationOpen: true, sessionsOpen: true }),
    readiness: "run",
    readyMarker: sessionsReady
  },
  {
    // 5b-run-held: the held run #151.
    id: "run-held",
    route: runRoute,
    run: issues(151),
    sidebar: sidebarState({ navigationOpen: true, sessionsOpen: false }),
    readiness: "run"
  },
  {
    // 4c-run-review: the pull-request review run #160.
    id: "run-review",
    route: runRoute,
    run: { beltId: "reviews", number: 160 },
    sidebar: sidebarState({ navigationOpen: true, sessionsOpen: false }),
    readiness: "run"
  },
  {
    // 4d-run-watch and 4d3-run-epic both resolve to the epic's ticket watch (PAY-40), the first
    // running watch in the daemon's createdAt order.
    id: "run-watch",
    route: runRoute,
    run: { beltId: "epics", key: "PAY-40" },
    sidebar: sidebarState({ navigationOpen: true, sessionsOpen: false }),
    readiness: "run"
  },
  {
    // 4d2-run-multi-watch: the jira run PAY-34, whose watch shows two pull requests.
    id: "run-multi-watch",
    route: runRoute,
    run: { beltId: "jira", key: "PAY-34" },
    sidebar: sidebarState({ navigationOpen: true, sessionsOpen: false }),
    readiness: "run"
  },
  {
    // 4n-nested-run: the demo's run #180 on the belt whose route splits again.
    id: "run-nested",
    route: runRoute,
    run: { beltId: "nested", number: 180 },
    sidebar: sidebarState({ navigationOpen: true, sessionsOpen: false }),
    readiness: "run"
  },
  {
    // 7h-afk-run: run #142 at Approve, the stage the demo's AFK decision approved.
    id: "run-afk-approved",
    route: (runId: string) => stageRoute(runId, "approve"),
    run: issues(142),
    sidebar: sidebarState({ navigationOpen: true, sessionsOpen: false }),
    readiness: "run"
  },
  {
    // 4o-open-links: the run menu of the default run, opened at its links section with the ticket and worktree entries.
    id: "run-open-links",
    route: runRoute,
    sidebar: sidebarState({ navigationOpen: true, sessionsOpen: false }),
    readiness: "run",
    steps: [
      { kind: "click", target: { role: "button", name: "Run actions", exact: true } },
      { kind: "wait", target: { selector: ".run-menu-card" }, state: "visible" }
    ],
    readyMarker: ".run-menu-card"
  },
  {
    // 5c-stage-adjust: the Adjust stage sheet for the default run's Spec stage, opened from the stage panel's Adjust.
    id: "run-stage-adjust",
    route: (runId: string) => stageRoute(runId, "spec", "spec-1"),
    sidebar: sidebarState({ navigationOpen: true, sessionsOpen: false }),
    readiness: "run",
    steps: [
      { kind: "click", target: { role: "button", name: "Adjust", exact: true } },
      { kind: "wait", target: { selector: '[role="dialog"]' }, state: "visible" }
    ],
    readyMarker: '[role="dialog"]'
  },
  {
    // 5e-send-to: the Send to… sheet of the held run #151, opened from the run menu.
    id: "run-send-to",
    route: runRoute,
    run: issues(151),
    sidebar: sidebarState({ navigationOpen: true, sessionsOpen: false }),
    readiness: "run",
    steps: [
      { kind: "click", target: { role: "button", name: "Run actions", exact: true } },
      { kind: "click", target: { role: "button", name: "Send to…", exact: true } },
      { kind: "wait", target: { selector: '[role="dialog"]' }, state: "visible" }
    ],
    readyMarker: '[role="dialog"]'
  },
  {
    // 4e-run-route: the run #145 the demo estimates on a split route.
    id: "run-route",
    route: runRoute,
    run: issues(145),
    sidebar: sidebarState({ navigationOpen: true, sessionsOpen: false }),
    readiness: "run"
  }
];