/**
 * Resolves a snapshot case's run selector against the seeded demo runs.
 *
 * A selector names a fixture by its daemon-provided belt, issue or state. Matching is deliberately
 * strict: a case whose fixture moved fails with its own id instead of capturing a different run.
 * This module is pure so the resolver can be unit-tested without a browser or daemon.
 */

/** Selects one seeded demo run for a case. Every named field must match the run or its issue. */
export interface RunSelector {
  beltId?: string;
  number?: number;
  repo?: string;
  key?: string;
  state?: string;
}

/** The parts of a demo run a selector can match; the daemon returns these on `/api/v1/runs`. */
export interface DemoRun {
  id: string;
  beltId?: string;
  state?: string;
  issue?: { number?: number; repo?: string; key?: string };
}

/** The seeded acme/platform#139 run the Run cases draw unless they name another. */
export const defaultRunSelector: RunSelector = { beltId: "issues", number: 139, repo: "acme/platform" };

export function matchesRun(run: DemoRun, selector: RunSelector): boolean {
  const issue = run.issue ?? {};
  if (selector.beltId !== undefined && run.beltId !== selector.beltId) return false;
  if (selector.state !== undefined && run.state !== selector.state) return false;
  if (selector.number !== undefined && issue.number !== selector.number) return false;
  if (selector.repo !== undefined && issue.repo !== selector.repo) return false;
  if (selector.key !== undefined && issue.key !== selector.key) return false;
  return true;
}

/** The id of the only run a selector names, or a failure naming the case and selector. */
export function resolveRunSelector(caseId: string, selector: RunSelector | undefined, runs: DemoRun[]): string {
  const wanted = selector ?? defaultRunSelector;
  const match = runs.find(run => matchesRun(run, wanted));
  if (!match) throw new Error(`snapshot case ${caseId}: no demo run matches ${JSON.stringify(wanted)}`);
  return match.id;
}