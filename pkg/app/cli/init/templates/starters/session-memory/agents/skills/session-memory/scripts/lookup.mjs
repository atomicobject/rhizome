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
import { byKind, git, loadShards, matchesAny, repoRoot, staleness } from "./lib/shards.mjs";

export const MAX_SHARDS = 5;
export const MAX_CHARS = 4000;

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

const norm = (s) => String(s ?? "").replace(/\s+/g, " ").trim();

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
  const tracked = git(root, ["ls-files", "-z"]).split("\0").filter(Boolean);
  const paths = files.map((f) => relativize(f, root, true)).filter(Boolean);
  if (command) {
    const p = shellPaths(command, root, true);
    paths.push(...p.reads, ...p.edits);
  }
  const ranked = task ? search(root, task) : [];
  const hits = [];
  const withheld = [];
  for (const s of loadShards(root)) {
    const reasons = [];
    if (!s.appliesTo.length && !s.commands.length) reasons.push("applies everywhere");
    const touched = paths.filter((p) => matchesAny(s.appliesTo, p));
    if (touched.length) reasons.push(`applies to ${touched.slice(0, 3).join(", ")}`);
    if (command && s.commands.some((c) => norm(command).includes(norm(c)))) reasons.push("matches the command");
    const rank = ranked.indexOf(s.path);
    if (rank >= 0) reasons.push("matches the task");
    if (!reasons.length) continue;
    const stale = staleness(root, s, tracked);
    if (stale) withheld.push({ path: s.path, reason: stale });
    else hits.push({ ...s, reasons, order: touched.length || reasons.includes("matches the command") ? 0 : rank >= 0 ? 1 + rank : 99 });
  }
  hits.sort((a, b) => byKind(a, b) || a.order - b.order || a.path.localeCompare(b.path));
  const shards = [];
  let chars = 0;
  for (const s of hits) {
    const text = render(s);
    if (shards.length === MAX_SHARDS || chars + text.length > MAX_CHARS) break;
    shards.push({ path: s.path, kind: s.kind, statement: s.statement, target: s.target, verifiedOn: s.verifiedOn, reasons: s.reasons, text });
    chars += text.length;
  }
  return { shards, omitted: hits.length - shards.length, withheld };
}

const render = (s) => `- [${s.kind.replace(/_/g, " ")}] ${s.statement}${s.target ? ` See ${s.target}.` : ""} (${s.path}, verified ${s.verifiedOn})`;

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
