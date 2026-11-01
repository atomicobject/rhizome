#!/usr/bin/env node
// The compounding handler `rzm hook` runs (SPEC-0124 "Delivery",
// SPEC-0045 US5 AC6). It reads one JSON request on stdin and prints
// {"items": [{"id", "text"}]}: patternless shards at session and subagent
// start, the shards for a tool call's paths or command afterwards, and both
// together on a tool call that brings due start items ("startDue"). The
// dispatcher owns payload parsing, path containment, deadlines, and
// emitted-item state. An error here prints no items, names the error in
// "error" so the dispatcher keeps start items due, and exits 0.

import { createHash } from "node:crypto";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, sep } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { approvedHash, loadShards, MAX_CHARS, SHARD_DIR, selectShards } from "./lib/shards.mjs";
import { readJson, writeJson, writeText } from "./lib/store.mjs";

export const HOOK_DIR = ".rhizome/compounding/hook";
const MAX_INPUT = 1 << 20;
const PREFIX = "Context shard ";
const TOOLS = new Set(["read", "edit", "patch", "shell"]);

const hash = (s) => createHash("sha256").update(s).digest("hex").slice(0, 16);
const norm = (s) => String(s ?? "").replace(/\s+/g, " ").trim();
const warn = (what, err) => process.stderr.write(`compounding hook: ${what}: ${err?.message ?? err}\n`);

// The lookup command that shows the matches past the first `skip`, naming
// this skill's lookup.mjs by absolute path, so it runs from any directory
// the agent is in, and quoted for the agent's shell: POSIX quoting off
// Windows, PowerShell quoting for Codex on Windows. Claude Code on Windows
// runs commands in Git Bash or PowerShell, which the hook cannot tell
// apart, so it gets both forms, each labelled with its shell.
export function lookupCommands(skip, { harness, platform = process.platform } = {}) {
  const script = fileURLToPath(new URL("./lookup.mjs", import.meta.url)).split(sep).join("/");
  const posix = { shell: "Git Bash", command: `node ${posixQuote(script)} --skip ${skip}` };
  const powershell = { shell: "PowerShell", command: `node ${powershellQuote(script)} --skip ${skip}` };
  if (platform !== "win32") return [posix];
  return harness === "codex" ? [powershell] : [posix, powershell];
}

export function lookupCommand(skip, options) {
  const forms = lookupCommands(skip, options);
  if (forms.length === 1) return forms[0].command;
  return `${forms.map((f) => `${f.command} in ${f.shell}`).join(", or ")},`;
}

const posixQuote = (s) => (/^[\w@%+=:,./-]+$/.test(s) ? s : `'${s.replaceAll("'", "'\\''")}'`);
// PowerShell splits an unquoted argument at a comma, and also ends a
// single-quoted string at a typographic single quote.
const powershellQuote = (s) => (/^[\w%+=:./-]+$/.test(s) ? s : `'${s.replace(/['\u2018\u2019\u201A\u201B]/g, "$&$&")}'`);

export const moreLine = (n, command) => `${n} more context ${n === 1 ? "shard applies" : "shards apply"} everywhere; run ${command} to see them.`;

// Names, nanosecond modification times, and sizes of the shard notes.
function listing(root) {
  const dir = join(root, SHARD_DIR);
  let names;
  try {
    names = readdirSync(dir).filter((f) => f.endsWith(".md")).sort();
  } catch (err) {
    if (err.code === "ENOENT") return "";
    throw err;
  }
  return names.map((f) => {
    const st = statSync(join(dir, f), { bigint: true });
    return `${f}\t${st.mtimeNs}\t${st.size}`;
  }).join("\n");
}

// Parsed shards, cached under the listing they were read at. The cache is
// written only when the listing did not change while the files were read, so
// it never pairs a key with a torn read.
export function cachedShards(root, load = loadShards) {
  const file = join(root, HOOK_DIR, "shard-cache.json");
  const before = listing(root);
  const key = hash(before);
  let cache = null;
  try {
    cache = readJson(file, null);
  } catch {}
  if (cache?.key === key && Array.isArray(cache.shards)) return cache.shards;
  const shards = load(root);
  if (listing(root) === before) {
    try {
      writeJson(file, { key, shards });
    } catch (err) {
      warn("writing the shard cache", err);
    }
  }
  return shards;
}

// The prefixes `rzm hook` matches shell commands against before it starts
// this handler, rewritten only when they change.
export function publishPrefixes(root, shards) {
  const prefixes = [...new Set(shards.filter((s) => approvedHash(s) === s.approvedHash).flatMap((s) => s.commands.map(norm)).filter(Boolean))].sort();
  const file = join(root, HOOK_DIR, "command-prefixes.json");
  const text = JSON.stringify(prefixes);
  let old = null;
  try {
    old = readFileSync(file, "utf8");
  } catch {}
  if (old !== text) writeText(file, text);
  return prefixes;
}

// `startedAt` is when the handler's deadline began, in performance.now()
// time; the CLI passes 0, the process start.
export function handle(input, { startedAt = performance.now() } = {}) {
  if (input?.version !== 1) throw new Error(`unsupported request version ${input?.version}`);
  const ev = input.event ?? {};
  const root = ev.root || process.cwd();
  const deadline = Number(input.deadlineMs) > 0 ? Number(input.deadlineMs) : 2000;
  const timeoutMs = Math.max(50, Math.floor(deadline - (performance.now() - startedAt) - 250));
  const budget = Number(input.budget);
  const maxChars = Number.isFinite(budget) && budget >= 0 ? Math.min(MAX_CHARS, budget) : MAX_CHARS;
  const exclude = new Set(Array.isArray(input.emitted) ? input.emitted : []);

  const shards = cachedShards(root);
  try {
    publishPrefixes(root, shards);
  } catch (err) {
    warn("publishing command prefixes", err);
  }
  const base = { root, shards, exclude, maxChars, timeoutMs, prefix: PREFIX };
  const tool = ev.event === "post-tool-use" && TOOLS.has(ev.tool);
  const paths = tool && Array.isArray(ev.paths) ? ev.paths.filter((p) => typeof p === "string" && p) : [];
  const command = tool && ev.tool === "shell" ? ev.command : undefined;

  // A tool call that also brings due start items selects from the shards
  // that apply everywhere and the call's own together, rules first, within
  // the one limit; its file shards that do not fit arrive with later calls.
  if (ev.event === "session-start" || ev.event === "subagent-start" || (ev.event === "post-tool-use" && ev.startDue === true)) {
    let r = selectShards({ ...base, paths, command });
    if (!r.omitted) return { items: r.shards.map(({ id, text }) => ({ id, text })) };
    // Lookup pages through every shard that applies everywhere, emitted ones
    // included, so the closing line skips to the first omitted one in that
    // order.
    const order = selectShards({ ...base, exclude: new Set(), maxShards: Infinity, maxChars: Infinity, index: r.index }).shards.map((s) => s.id);
    const omitted = (rest) => rest.filter((id) => order.includes(id));
    if (!omitted(r.rest).length) return { items: r.shards.map(({ id, text }) => ({ id, text })) };
    const line = (n, skip) => moreLine(n, lookupCommand(skip, { harness: ev.harness }));
    // Reserve room for the closing line; singular is the longer wording.
    const reserve = Math.max(Buffer.byteLength(line(1, order.length)), Buffer.byteLength(line(order.length, order.length)));
    r = selectShards({ ...base, paths, command, maxChars: Math.max(0, maxChars - reserve), index: r.index });
    const items = r.shards.map(({ id, text }) => ({ id, text }));
    const rest = omitted(r.rest);
    const more = { id: `compounding:more@${hash(rest.join("\n"))}`, text: line(rest.length, order.indexOf(rest[0])) };
    const used = items.reduce((n, i) => n + Buffer.byteLength(i.text), 0);
    if (!exclude.has(more.id) && used + Buffer.byteLength(more.text) <= maxChars) items.push(more);
    return { items };
  }
  if (tool && (paths.length || command)) {
    const r = selectShards({ ...base, paths, command, everywhere: false });
    return { items: r.shards.map(({ id, text }) => ({ id, text })) };
  }
  return { items: [] };
}

export async function main(stdin = process.stdin, stdout = process.stdout) {
  let out = { items: [] };
  try {
    const chunks = [];
    let size = 0;
    for await (const chunk of stdin) {
      size += chunk.length;
      if (size > MAX_INPUT) throw new Error("request exceeds 1 MiB");
      chunks.push(chunk);
    }
    out = handle(JSON.parse(Buffer.concat(chunks).toString("utf8")), { startedAt: 0 });
  } catch (err) {
    warn("no items", err);
    out = { items: [], error: String(err?.message ?? err) };
  }
  stdout.write(JSON.stringify(out));
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) await main();
