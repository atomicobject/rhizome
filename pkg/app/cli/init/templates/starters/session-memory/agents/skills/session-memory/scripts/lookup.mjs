#!/usr/bin/env node
// Find the approved context shards for a task, files, or a command
// (SPEC-0124 US3). Only current shards are returned: a shard whose content no
// longer matches its approved hash, or whose files changed since it was
// verified, is withheld until it is renewed or revised.
//
//   lookup.mjs [--files PATH...] [--command "CMD"] [--task "TEXT"] [--json]
//
// At most five shards (about 4,000 characters), rules first. Shards with no
// patterns always apply. Run from anywhere inside the repository.

import { execFileSync } from "node:child_process";
import { existsSync } from "node:fs";
import { join } from "node:path";
import { pathToFileURL } from "node:url";
import { relativize, shellPaths } from "./lib/paths.mjs";
import { repoRoot, selectShards } from "./lib/shards.mjs";

export { MAX_CHARS, MAX_SHARDS } from "./lib/shards.mjs";

function args(argv) {
  const out = { files: [] };
  for (let k = 0; k < argv.length; k++) {
    const a = argv[k];
    if (a === "--files") while (argv[k + 1] && !argv[k + 1].startsWith("--")) out.files.push(argv[++k]);
    else if (a === "--json") out.json = true;
    else if (a.startsWith("--")) out[a.slice(2)] = argv[++k];
  }
  return out;
}

// Shard paths Rhizome search ranks for a task; empty when search fails.
function searchShards(root, task) {
  const rzm = process.env.RZM || (existsSync(join(root, "bin/rzm")) ? join(root, "bin/rzm") : "rzm");
  try {
    const out = execFileSync(rzm, ["search", "-n", "20", "--raw", "--", task], { cwd: root, encoding: "utf8", timeout: 20000 });
    return [...out.matchAll(/\((docs\/memory\/context-shards\/[^)]+\.md)\)/g)].map((m) => m[1]);
  } catch {
    return [];
  }
}

export function lookup({ root, files = [], command, task, search = searchShards }) {
  const paths = files.map((f) => relativize(f, root, true)).filter(Boolean);
  if (command) {
    const p = shellPaths(command, root, true);
    paths.push(...p.reads, ...p.edits);
  }
  const ranked = task ? search(root, task) : [];
  const { shards, omitted, withheld } = selectShards({ root, paths, command, ranked });
  return { shards, omitted, withheld };
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const a = args(process.argv.slice(2));
  const result = lookup({ root: repoRoot(), files: a.files, command: a.command, task: a.task });
  if (a.json) console.log(JSON.stringify(result, null, 2));
  else {
    console.log(result.shards.length ? result.shards.map((s) => s.text).join("\n") : "No current context shards match.");
    if (result.omitted) console.log(`(${result.omitted} more matched; narrow the files or task to see them)`);
    if (result.withheld.length) console.log(`(${result.withheld.length} matching shard(s) withheld until re-verified; see \`shards.mjs check\`)`);
  }
}
