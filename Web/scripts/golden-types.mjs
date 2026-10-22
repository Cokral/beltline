// Writes src/api/golden.gen.ts (gitignored), which `npm run typecheck` runs over: every golden of
// daemon/testdata/golden that src/api/goldenTypes.json maps to a response type, and every sample of
// src/api/goldenSamples.json, each as a literal `satisfies` its type. A field the type lacks, a
// missing field, a wrong kind or an enum value outside the union fails the typecheck.
// The Go test TestGoldenResponses keeps the two JSON inputs current with the Go wire types.

import { readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const webRoot = join(dirname(fileURLToPath(import.meta.url)), "..");
const apiDirectory = join(webRoot, "src", "api");
const goldenDirectory = join(webRoot, "..", "daemon", "testdata", "golden");
const outputPath = join(apiDirectory, "golden.gen.ts");

function readJSON(path) {
  return JSON.parse(readFileSync(path, "utf8"));
}

const wire = readFileSync(join(apiDirectory, "wire.gen.ts"), "utf8");
const goldenTypes = readJSON(join(apiDirectory, "goldenTypes.json"));
const samples = readJSON(join(apiDirectory, "goldenSamples.json"));

const typeNames = new Set();
const entries = [];

function addEntry(key, value, typeName) {
  if (!new RegExp(`export (interface|type) ${typeName}\\b`).test(wire)) {
    throw new Error(`${key} is typed ${typeName}, which wire.gen.ts does not export`);
  }
  typeNames.add(typeName);
  entries.push(`  ${JSON.stringify(key)}: ${JSON.stringify(value)} satisfies ${typeName},`);
}

for (const name of Object.keys(goldenTypes).sort()) {
  const path = join(goldenDirectory, name);
  let value;
  try {
    value = readJSON(path);
  } catch (error) {
    throw new Error(`golden ${name}: ${error.message}`);
  }
  addEntry(name, value, goldenTypes[name]);
}

for (const sample of samples) {
  addEntry(sample.name, sample.value, sample.type);
}

const output = [
  "// Generated from daemon/testdata/golden and src/api/goldenTypes.json by scripts/golden-types.mjs. Do not edit;",
  "// `npm run typecheck` writes it. It is gitignored.",
  "",
  `import type { ${[...typeNames].sort().join(", ")} } from "./wire.gen";`,
  "",
  "// Each golden is checked against its response type by `satisfies` (excess, missing and wrong fields, and enum values).",
  "export const goldenResponses = {",
  ...entries,
  "};",
  "",
].join("\n");

writeFileSync(outputPath, output);
