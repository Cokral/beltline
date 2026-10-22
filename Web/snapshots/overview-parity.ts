import type { ParityEntry } from "./belts-parity";

/**
 * The Overview capture parity manifest. Each entry is a capture the native app used to take. A "captured"
 * entry is produced by a case in cases/overview.ts; a "not-captured" entry is not produced here, with the reason.
 * The Floor's hover is not reachable: caseTypes has no hover step, and a belt's emphasis follows the pointer only, so a
 * keyboard focus cannot stand in for it. The Floor's looks are one setting: the demo's default is the boxes look.
 * The Floor hover, archive and nested cases are covered by the page and map unit tests.
 */
const hoverReason = "caseTypes has no hover step, and a belt's emphasis on the Floor follows the pointer only, so a keyboard focus cannot "
  + "stand in for it; the hover emphasis, its exit and its clearing on mode change are covered by FactoryMap.test.tsx.";
const archivedReason = "Archived belts need a daemon whose library has an archived belt; the shared demo has none, and an API-response "
  + "override would replace the whole floor. The floor's eligibility is covered by OverviewPresentationTests and the page tests.";
const nestedReason = "The demo floor has no nested fork, and a fixture harness is not introduced for one. The nested drawing is covered by "
  + "FactoryLayoutTests and the FactoryMap test that renders the nested golden.";
const metroLookReason = "The Metro is the belt look Settings picks, not a tab. The shared demo draws boxes, and a case cannot change "
  + "the look without changing that one daemon; the metro's drawing, dots and pins are covered by MetroMap.test.tsx.";
const stationReason = "Opening a station navigates to Work with a machine query. That navigation is covered by the Overview navigation tests, "
  + "and no Work capture in this matrix is filtered to a machine.";

export const overviewParity: readonly ParityEntry[] = [
  { native: "overview-open", status: "captured", caseId: "overview-open" },
  { native: "overview-closed", status: "captured", caseId: "overview-closed" },
  { native: "1-overview", status: "captured", caseId: "overview-open", note: "The Summary, which the Overview lands on." },
  { native: "2-floor", status: "captured", caseId: "overview-floor-open" },
  { native: "2-floor-hover", status: "not-captured", reason: hoverReason },
  { native: "2-floor-hover-other", status: "not-captured", reason: hoverReason },
  { native: "2-floor-pinned", status: "captured", caseId: "overview-floor-pinned" },
  { native: "2m-metro", status: "not-captured", reason: metroLookReason },
  { native: "2m-metro-hover", status: "not-captured", reason: metroLookReason },
  { native: "2m-metro-pinned", status: "not-captured", reason: metroLookReason },
  { native: "2a-factory-station", status: "not-captured", reason: stationReason },
  { native: "2n-nested-floor", status: "not-captured", reason: nestedReason },
  { native: "8e-floor-archived-idle", status: "not-captured", reason: archivedReason },
  { native: "8f-floor-archived-active", status: "not-captured", reason: archivedReason },
  { native: "7h-overview-paused", status: "captured", caseId: "overview-paused", note: "The paused state is an API fixture, as for Settings." }
];
