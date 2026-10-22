import { shellCases, sidebarState, type SnapshotCase } from "../caseTypes";
import { snapshotState } from "../stateFixture";

export const afkCases: SnapshotCase[] = [
  ...shellCases("afk", "/afk"),
  { id: "native-7g-afk", route: "/afk", sidebar: sidebarState({ navigationOpen: true }), readiness: "shell", readyMarker: '[data-column="afk-decisions"][data-ready="true"]' },
  { id: "native-7i-afk-on", route: "/afk", sidebar: sidebarState({ navigationOpen: true }), readiness: "shell", apiResponses: [{ path: "/api/v1/state", status: 200, body: snapshotState({ afk: true }) }], steps: [{ kind: "wait", target: { selector: '[role="switch"][aria-checked="true"]' }, state: "visible" }], readyMarker: '[data-column="afk-decisions"][data-ready="true"]' },
  { id: "native-7k-afk-decided", route: "/afk", sidebar: sidebarState({ navigationOpen: true }), readiness: "shell", readyMarker: '[data-column="afk-decisions"][data-ready="true"]' },
  { id: "native-7m-overrule-row", route: "/afk", sidebar: sidebarState({ navigationOpen: true }), readiness: "shell", steps: [{ kind: "select", target: { role: "combobox", name: "Decision filter" }, value: "all" }], readyMarker: '[data-column="afk-decisions"][data-ready="true"]' },
  {
    id: "afk-all",
    route: "/afk",
    sidebar: sidebarState({ navigationOpen: true }),
    readiness: "shell",
    steps: [{ kind: "select", target: { role: "combobox", name: "Decision filter" }, value: "all" }],
    readyMarker: '[data-column="afk-decisions"][data-ready="true"]'
  }
];
