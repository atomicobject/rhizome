#!/usr/bin/env node
// The session-memory handler `rzm hook` runs (SPEC-0124 "Delivery",
// SPEC-0045 US5 AC6). It reads one JSON request on stdin and prints
// {"items": [{"id", "text"}]}: patternless shards at session and subagent
// start, and the shards for a tool call's paths or command afterwards. The
// dispatcher owns payload parsing, path containment, deadlines, and
// emitted-item state; any error here prints no items and exits 0.

import { createHash } from "node:crypto";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";
import { pathToFileURL } from "node:url";
import { approvedHash, loadShards, MAX_CHARS, SHARD_DIR, selectShards } from "./lib/shards.mjs";
import { readJson, writeJson, writeText } from "./lib/store.mjs";

export const HOOK_DIR = ".rhizome/session-memory/hook";
const MAX_INPUT = 1 << 20;
const PREFIX = "Context shard ";
const TOOLS = new Set(["read", "edit", "patch", "shell"]);

const hash = (s) => createHash("sha256").update(s).digest("hex").slice(0, 16);
const norm = (s) => String(s ?? "").replace(/\s+/g, " ").trim();
const warn = (what, err) => process.stderr.write(`session-memory hook: ${what}: ${err?.message ?? err}\n`);

export const moreLine = (n) => `${n} more context ${n === 1 ? "shard applies" : "shards apply"} everywhere; run node .agents/skills/session-memory/scripts/lookup.mjs to see them.`;

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

  if (ev.event === "session-start" || ev.event === "subagent-start") {
    let r = selectShards(base);
    if (!r.omitted) return { items: r.shards.map(({ id, text }) => ({ id, text })) };
    // Reserve room for the closing line; singular is the longer wording.
    const total = r.shards.length + r.omitted;
    const reserve = Math.max(Buffer.byteLength(moreLine(1)), Buffer.byteLength(moreLine(total)));
    r = selectShards({ ...base, maxChars: Math.max(0, maxChars - reserve), index: r.index });
    const items = r.shards.map(({ id, text }) => ({ id, text }));
    const more = { id: `session-memory:more@${hash(r.rest.join("\n"))}`, text: moreLine(r.omitted) };
    const used = items.reduce((n, i) => n + Buffer.byteLength(i.text), 0);
    if (!exclude.has(more.id) && used + Buffer.byteLength(more.text) <= maxChars) items.push(more);
    return { items };
  }
  if (ev.event === "post-tool-use" && TOOLS.has(ev.tool)) {
    const paths = (Array.isArray(ev.paths) ? ev.paths : []).filter((p) => typeof p === "string" && p);
    const command = ev.tool === "shell" ? ev.command : undefined;
    if (!paths.length && !command) return { items: [] };
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
  }
  stdout.write(JSON.stringify(out));
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) await main();
