import { readdirSync, readFileSync } from "node:fs";
import { resolve } from "node:path";

const goldenDirectory = resolve(process.cwd(), "../daemon/testdata/golden");

/** The name of every golden response, sorted. */
export const goldenFiles = (): string[] => readdirSync(goldenDirectory).sort();

/** A golden response's text, exactly as the daemon's contract tests wrote it. */
export const goldenText = (name: string): string => readFileSync(resolve(goldenDirectory, name), "utf8");

/** A golden response parsed as JSON. Untyped by default, since tests often edit a golden to break it. */
export const golden = <T = any>(name: string): T => JSON.parse(goldenText(name)) as T;
