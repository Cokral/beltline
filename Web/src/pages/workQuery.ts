// The Work URL is the query. These helpers only move its parameters between the address bar and the API
// request; they never decide which rows match, how they group or in what order they show. An unsupported
// value is passed on unchanged, so the daemon's rejection is what the page shows.

export const workApiPath = "/api/v1/views/work";

export const workParamNames = ["filter", "belt", "stage", "search", "groupBy"] as const;
export type WorkParamName = (typeof workParamNames)[number];

/** Each parameter's value as the URL holds it: "" means the default, so the request omits it. */
export type WorkUrlQuery = Record<WorkParamName, string>;

const defaults: WorkUrlQuery = { filter: "active", belt: "all", stage: "", search: "", groupBy: "none" };

/** The Work query the URL holds. Omitted and empty parameters read as their defaults. */
export function readWorkQuery(params: URLSearchParams): WorkUrlQuery {
  return Object.fromEntries(workParamNames.map(name => [name, params.get(name) ?? ""])) as WorkUrlQuery;
}

/**
 * Sets or clears parameters. A value equal to its default is removed, so the default query has no parameters.
 * Other parameters keep their values; the caller decides whether the change adds a history entry.
 */
export function withWorkQuery(params: URLSearchParams, changes: Partial<WorkUrlQuery>): URLSearchParams {
  const next = new URLSearchParams(params);
  for (const name of workParamNames) {
    if (!(name in changes)) continue;
    const value = changes[name] ?? "";
    if (value === "" || value === defaults[name]) next.delete(name);
    else next.set(name, value);
  }
  return next;
}

/** The API request for a query: each nonempty parameter, under the name the daemon reads. */
export function workRequestPath(query: WorkUrlQuery): string {
  const params = new URLSearchParams();
  for (const name of workParamNames) {
    if (query[name] !== "") params.set(name, query[name]);
  }
  const encoded = params.toString();
  return encoded ? `${workApiPath}?${encoded}` : workApiPath;
}

/**
 * The folded group values for one dimension. Folds are kept per dimension, so one value never collides with
 * another dimension's, and Runs and Watching each have their own record.
 */
export function foldedValues(record: Record<string, string[]>, dimension: string): ReadonlySet<string> {
  return new Set(Object.prototype.hasOwnProperty.call(record, dimension) ? record[dimension] : []);
}

/** Flips one group value in a dimension's folds, keeping the order the values were folded in. */
export function toggleFolded(record: Record<string, string[]>, dimension: string, value: string): Record<string, string[]> {
  const current = foldedValues(record, dimension);
  const next = current.has(value) ? [...current].filter(item => item !== value) : [...current, value];
  return { ...record, [dimension]: next };
}
