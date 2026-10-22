import { validateSnapshotCase, type SnapshotCase } from "./caseTypes";
import { agentsCases } from "./cases/agents";
import { appearanceCases } from "./cases/appearance";
import { afkCases } from "./cases/afk";
import { beltsCases } from "./cases/belts";
import { galleryCases } from "./cases/gallery";
import { libraryCases } from "./cases/library";
import { overviewCases } from "./cases/overview";
import { runCases } from "./cases/run";
import { settingsCases } from "./cases/settings";
import { workCases } from "./cases/work";

export type { SnapshotCase, SidebarState, PreCaptureStep, SnapshotFixture, StepTarget } from "./caseTypes";

/** Every page case file, in capture order. cases.test.ts checks this list against the files on disk. */
export const snapshotPageFiles = ["overview", "work", "run", "belts", "agents", "library", "afk", "settings", "gallery", "appearance"] as const;

export const snapshotCases: SnapshotCase[] = [
  ...overviewCases, ...workCases, ...runCases, ...beltsCases, ...agentsCases, ...libraryCases, ...afkCases, ...settingsCases, ...galleryCases,
  ...appearanceCases
];

for (const item of snapshotCases) validateSnapshotCase(item);
if (new Set(snapshotCases.map(item => item.id)).size !== snapshotCases.length) throw new Error("snapshot case ids must be unique");

export const snapshotSizes = [
  { width: 1080, height: 680 },
  { width: 1440, height: 900 },
  { width: 2560, height: 1440 }
] as const;

export function selectCases(filter: string | undefined): SnapshotCase[] {
  if (filter === undefined || filter === "") return snapshotCases;
  const prefixes = filter.split(",").map(value => value.trim()).filter(Boolean);
  if (!prefixes.length) throw new Error(`BELTLINE_SNAPSHOT_ONLY matched no cases: ${filter}`);
  const selected = snapshotCases.filter(item => prefixes.some(prefix => item.id.startsWith(prefix)));
  if (!selected.length) throw new Error(`BELTLINE_SNAPSHOT_ONLY matched no cases: ${prefixes.join(", ")}`);
  return selected;
}

export function parseSnapshotSize(value: string | undefined): { width: number; height: number }[] {
  if (!value) return [...snapshotSizes];
  const match = /^(\d+)x(\d+)$/.exec(value.trim());
  if (!match || Number(match[1]) <= 0 || Number(match[2]) <= 0) {
    throw new Error(`BELTLINE_SNAPSHOT_SIZE must be a positive WIDTHxHEIGHT value (received ${value})`);
  }
  return [{ width: Number(match[1]), height: Number(match[2]) }];
}
