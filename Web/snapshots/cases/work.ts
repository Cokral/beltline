import { shellCases, sidebarState, type SnapshotCase } from "../caseTypes";
import { snapshotState } from "../stateFixture";

// Each grouping is reached by its URL, which the page reads as its query, so no pre-capture steps are needed.
const groupedCase = (id: string, groupBy: string): SnapshotCase => ({
  id,
  route: `/work?groupBy=${groupBy}`,
  sidebar: sidebarState({ navigationOpen: true }),
  readiness: "shell"
});

export const workCases: SnapshotCase[] = [
  ...shellCases("work", "/work"),
  groupedCase("work-grouped", "repo"),
  groupedCase("work-status", "status"),
  groupedCase("work-epic", "epic"),
  groupedCase("work-watching", "none"),
  {
    // 7i-work-paused: the paused state is a fixture, so the one demo daemon is never paused for the other cases.
    id: "work-paused",
    route: "/work",
    sidebar: sidebarState({ navigationOpen: true }),
    readiness: "shell",
    apiResponses: [{ path: "/api/v1/state", status: 200, body: snapshotState({ paused: true }) }]
  }
];
