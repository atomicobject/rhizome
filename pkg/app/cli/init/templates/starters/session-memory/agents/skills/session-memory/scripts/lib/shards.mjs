// Approved context shards (SPEC-0124 "Tracked data"): one note per shard in
// docs/memory/context-shards/, decisions as JSON events beside them. Only
// shards.mjs writes these files, and every write is one atomic replacement.

import { execFileSync } from "node:child_process";
import { createHash, randomBytes } from "node:crypto";
import { existsSync, mkdirSync, readdirSync, readFileSync, renameSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";

export const SHARD_DIR = "docs/memory/context-shards";
export const DECISION_DIR = `${SHARD_DIR}/decisions`;
const KIND_ORDER = { rule: 0, fact: 1, where_to_find: 2 };

const hex = (v) => createHash("sha256").update(typeof v === "string" ? v : JSON.stringify(v)).digest("hex").slice(0, 16);
const norm = (s) => String(s ?? "").replace(/\s+/g, " ").trim();
export const today = () => new Date().toISOString().slice(0, 10);
export const byKind = (a, b) => (KIND_ORDER[a.kind] ?? 9) - (KIND_ORDER[b.kind] ?? 9);

// One pattern dialect for lookup and verify mode: literal paths, `*` within
// a segment, `?` for one character, and `**` for any number of segments.
// Rhizome's doublestar matcher must agree; patterns.json holds the shared
// case table.
export function globRegExp(pattern) {
  let re = "";
  for (let k = 0; k < pattern.length; k++) {
    const ch = pattern[k];
    if (ch === "*" && pattern[k + 1] === "*") {
      const before = k === 0 || pattern[k - 1] === "/";
      const after = k + 2 === pattern.length || pattern[k + 2] === "/";
      if (before && after) {
        if (pattern[k + 2] === "/") {
          re += "(?:.*/)?";
          k += 2;
        } else {
          re = re.endsWith("/") ? `${re.slice(0, -1)}(?:/.*)?` : `${re}.*`;
          k += 1;
        }
        continue;
      }
      re += "[^/]*";
      k += 1;
    } else if (ch === "*") re += "[^/]*";
    else if (ch === "?") re += "[^/]";
    else re += ch.replace(/[.+^${}()|[\]\\]/g, "\\$&");
  }
  return new RegExp(`^${re}$`);
}

export const matchesAny = (patterns, path) => (patterns ?? []).some((p) => globRegExp(p).test(path));

// The approved hash covers what delivery depends on, so a hand edit to any of
// it withholds the shard until it is revised.
export const approvedHash = (s) => hex({ kind: s.kind, statement: norm(s.statement), appliesTo: [...(s.appliesTo ?? [])].sort(), commands: [...(s.commands ?? [])].sort(), target: s.target ?? null });
export const learningHash = (s) => hex(`${s.kind}\n${norm(s.statement).toLowerCase()}`);
export const contentHash = (text) => hex(text);

export const git = (root, args, timeout) => execFileSync("git", args, { cwd: root, encoding: "utf8", maxBuffer: 256 << 20, ...(timeout ? { timeout, killSignal: "SIGKILL" } : {}) });
export const repoRoot = (cwd = process.cwd()) => git(cwd, ["rev-parse", "--show-toplevel"]).trim();

// Every tracked file a shard depends on: files its patterns match, its
// target, and the locations its verification checked.
export function shardFiles(root, shard, tracked = git(root, ["ls-files", "-z"]).split("\0").filter(Boolean)) {
  const set = new Set(tracked);
  // Shard notes and decisions are not what a shard describes; counting them
  // would make a shard on docs/** stale as soon as it was committed.
  const files = new Set(tracked.filter((f) => !f.startsWith(`${SHARD_DIR}/`) && matchesAny(shard.appliesTo, f)));
  for (const p of [shard.target, ...(shard.checked ?? [])]) if (p && set.has(p)) files.add(p);
  return [...files].sort();
}

// A content-addressed fingerprint of those files' index entries, which
// survives squash merges and branch switches. Null when there is nothing to
// fingerprint.
export function fingerprint(root, files) {
  if (!files.length) return null;
  return hex(git(root, ["ls-files", "-s", "-z", "--", ...files]));
}

// The whole index in one call: the tracked list, and each path's
// `ls-files -s -z` records in index order (several for an unmerged path), so
// fingerprints for many shards need no further Git calls.
export function readIndex(root, timeout) {
  const records = git(root, ["ls-files", "-s", "-z"], timeout).split("\0").filter(Boolean).map((r) => [r.slice(r.indexOf("\t") + 1), r]);
  return { tracked: [...new Set(records.map(([path]) => path))], records };
}

// The bytes `git ls-files -s -z -- <files>` prints, hashed as fingerprint() does.
export function indexFingerprint(index, files) {
  if (!files.length) return null;
  const want = new Set(files);
  return hex(index.records.filter(([path]) => want.has(path)).map(([, r]) => `${r}\0`).join(""));
}

// Files with uncommitted changes or unresolved merge stages.
export function dirtyFiles(root, files) {
  if (!files.length) return [];
  return git(root, ["status", "--porcelain", "-z", "--", ...files]).split("\0").filter(Boolean).map((l) => l.slice(3));
}

export function slugify(text) {
  return norm(text).toLowerCase().replace(/`/g, "").replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "").split("-").slice(0, 8).join("-") || "shard";
}

// A short title: the statement's first sentence, cut at a word.
export function titleOf(statement) {
  const first = norm(statement).split(/(?<=[.;:])\s/)[0].replace(/[.;:]$/, "");
  if (first.length <= 80) return first;
  return first.slice(0, 80).replace(/\s+\S*$/, "");
}

const q = (s) => JSON.stringify(String(s));

export function renderShard(s) {
  const fm = ["---", `kind: ${s.kind}`, `summary: ${q(s.statement)}`];
  const list = (key, xs) => xs?.length && fm.push(`${key}:`, ...xs.map((x) => `  - ${q(x)}`));
  list("applies-to", s.appliesTo);
  list("commands", s.commands);
  if (s.target) fm.push(`target: ${q(s.target)}`);
  fm.push(`reviewed-by: ${q(s.reviewedBy)}`, `reviewed-on: ${s.reviewedOn}`, `approved-hash: ${approvedHash(s)}`, `verified-on: ${s.verifiedOn}`);
  if (s.verifiedFingerprint) fm.push(`verified-fingerprint: ${s.verifiedFingerprint}`);
  fm.push(`families: ${s.families ?? 0}`, `tool-calls: ${s.toolCalls ?? 0}`);
  if (s.appliesTo?.length) fm.push("code-anchors:", "  any:", ...s.appliesTo.map((g) => `    - glob: ${q(g)}`));
  fm.push("---", "");
  const body = [`# ${titleOf(s.statement)}`, "", s.statement, "", "## Why it matters", "", s.why || "(not recorded)", "", "## Verification", ""];
  for (const v of s.verification ?? []) body.push(`- \`${v.path}\`: ${norm(v.checked)}`);
  body.push("", "## Evidence", "", ...(s.evidenceLines ?? []), "");
  return [...fm, ...body].join("\n");
}

export function evidenceLines(evidence = []) {
  return evidence.map((e) => `- ${e.harness} session \`${e.session}\`, message \`${e.episode}\`, ${String(e.ts ?? "").slice(0, 10)}, ${e.lens}, ${e.toolCalls ?? 0} tool calls`);
}

// Reads the frontmatter subset shards use: scalars, quoted strings, and
// lists of scalars. Nested maps (code-anchors) are read as lists of their
// `glob:` values.
export function parseShard(text, path) {
  const m = text.replace(/\r\n/g, "\n").match(/^---\n([\s\S]*?)\n---\n?([\s\S]*)$/);
  if (!m) return null;
  const fm = {};
  let key = null;
  for (const line of m[1].split("\n")) {
    const top = line.match(/^([\w-]+):\s*(.*)$/);
    if (top) {
      key = top[1];
      fm[key] = top[2] === "" ? [] : scalar(top[2]);
      continue;
    }
    const item = line.match(/^\s*-\s*(?:glob:\s*)?(.*)$/);
    if (key && Array.isArray(fm[key]) && item) fm[key].push(scalar(item[1]));
  }
  const body = m[2];
  const section = (name) => body.split(/\n(?=## )/).find((s) => s.startsWith(`## ${name}`))?.replace(/^## .*\n+/, "").trim() ?? "";
  const checked = [...section("Verification").matchAll(/^- `([^`]+)`/gm)].map((x) => x[1]);
  const shard = {
    path,
    kind: fm.kind,
    statement: fm.summary,
    appliesTo: list(fm["applies-to"]),
    commands: list(fm.commands),
    target: fm.target || null,
    reviewedBy: fm["reviewed-by"],
    reviewedOn: fm["reviewed-on"],
    approvedHash: fm["approved-hash"],
    verifiedOn: fm["verified-on"],
    verifiedFingerprint: fm["verified-fingerprint"] || null,
    families: Number(fm.families ?? 0),
    toolCalls: Number(fm["tool-calls"] ?? 0),
    anchors: list(fm["code-anchors"]),
    why: section("Why it matters"),
    checked,
    evidenceSection: section("Evidence"),
    contentHash: contentHash(text),
  };
  return shard;
}

const list = (v) => (Array.isArray(v) ? v.map(String) : v ? [String(v)] : []);

function scalar(raw) {
  const s = raw.trim();
  if (s.startsWith('"')) {
    try {
      return JSON.parse(s);
    } catch {
      return s.slice(1, -1);
    }
  }
  if (s.startsWith("'")) return s.slice(1, -1).replace(/''/g, "'");
  if (s === "[]") return [];
  return s;
}

export function loadShards(root) {
  const dir = join(root, SHARD_DIR);
  if (!existsSync(dir)) return [];
  return readdirSync(dir)
    .filter((f) => f.endsWith(".md"))
    .sort()
    .map((f) => parseShard(readFileSync(join(dir, f), "utf8"), `${SHARD_DIR}/${f}`))
    .filter(Boolean);
}

// Why a shard should not be delivered, or null when it is current.
export function staleness(root, shard, index = readIndex(root)) {
  if (approvedHash(shard) !== shard.approvedHash) return "content differs from the approved hash";
  const files = shardFiles(root, shard, index.tracked);
  if (shard.appliesTo.length && !files.some((f) => matchesAny(shard.appliesTo, f))) return "its patterns match no file";
  if (shard.target && !files.includes(shard.target)) return `its target ${shard.target} is missing`;
  if (indexFingerprint(index, files) !== (shard.verifiedFingerprint ?? null)) return "files it depends on changed since verification";
  return null;
}

export const MAX_SHARDS = 5;
export const MAX_CHARS = 4000;
export const patternless = (s) => !s.appliesTo.length && !s.commands.length;

// The command-prefix rule `rzm hook` also applies before starting the
// handler for a shell call; the two must stay identical.
export const commandSegments = (command) => String(command ?? "").split(/&&|\|\||;|\||\n/).map(norm).filter(Boolean);
export const matchesCommand = (prefixes, segments) => prefixes.some((c) => (c = norm(c)) && segments.some((s) => s.startsWith(c)));

// A directory operand (`rg TODO pkg/store`) matches a shard when a tracked
// file beneath it does. One holding more than 50 tracked files and more than
// a tenth of them, the breadth approval questions, matches nothing, so a
// search over most of the repository does not select every shard.
function filesUnder(index, dir) {
  const files = index.tracked.filter((f) => f.startsWith(`${dir}/`));
  return files.length > 50 && files.length > index.tracked.length / 10 ? null : files;
}

// Whether a file beneath dir could match the pattern, judged from the
// pattern's literal prefix, so most shards need no index read.
function mayMatchUnder(pattern, dir) {
  const wild = pattern.search(/[*?]/);
  const literal = wild < 0 ? pattern : pattern.slice(0, wild);
  return literal.startsWith(`${dir}/`) || `${dir}/`.startsWith(literal);
}

export const shardLine = (s, prefix = "- ") => `${prefix}[${s.kind.replace(/_/g, " ")}] ${s.statement}${s.target ? ` See ${s.target}.` : ""} (${s.path}, verified ${s.verifiedOn})`;

// The one selection lookup and the hook handler share: current shards that
// match the paths, the command, or the ranked task results (and, with
// `everywhere`, shards with no patterns), rules first, within the limits.
// Excluded IDs are dropped before the limits so they never hide later
// matches, and `skip` drops that many leading matches, so a caller can page
// past what it already showed. The index is read at most once, and only when
// something matches or a directory operand might.
export function selectShards({ root, paths = [], command, ranked = [], everywhere = true, exclude = new Set(), skip = 0, maxShards = MAX_SHARDS, maxChars = MAX_CHARS, shards, index, timeoutMs, prefix }) {
  const segments = commandSegments(command);
  const hits = [];
  const withheld = [];
  const dirs = new Map();
  for (const s of shards ?? loadShards(root)) {
    const id = `${s.path}@${s.contentHash}`;
    if (exclude.has(id)) continue;
    const reasons = [];
    if (everywhere && patternless(s)) reasons.push("applies everywhere");
    const under = (p) => {
      index ??= readIndex(root, timeoutMs);
      if (!dirs.has(p)) dirs.set(p, filesUnder(index, p));
      return dirs.get(p);
    };
    const touched = paths.filter((p) => {
      // A folder that matches a pattern itself (pkg matches pkg/**) is still
      // a folder: the breadth limit applies. A file, or a new file nothing
      // is tracked under, matches directly.
      if (matchesAny(s.appliesTo, p)) return under(p) !== null;
      const pats = s.appliesTo.filter((g) => mayMatchUnder(g, p)).map(globRegExp);
      if (!pats.length) return false;
      return (under(p) ?? []).some((f) => !f.startsWith(`${SHARD_DIR}/`) && pats.some((re) => re.test(f)));
    });
    if (touched.length) reasons.push(`applies to ${touched.slice(0, 3).join(", ")}`);
    const byCommand = matchesCommand(s.commands, segments);
    if (byCommand) reasons.push("matches the command");
    const rank = ranked.indexOf(s.path);
    if (rank >= 0) reasons.push("matches the task");
    if (!reasons.length) continue;
    const stale = staleness(root, s, (index ??= readIndex(root, timeoutMs)));
    if (stale) withheld.push({ path: s.path, reason: stale });
    else hits.push({ ...s, id, reasons, order: touched.length || byCommand ? 0 : rank >= 0 ? 1 + rank : 99 });
  }
  hits.sort((a, b) => byKind(a, b) || a.order - b.order || a.path.localeCompare(b.path));
  hits.splice(0, Math.max(0, Math.floor(Number(skip)) || 0));
  const picked = [];
  let chars = 0;
  for (const s of hits) {
    const text = shardLine(s, prefix);
    const size = Buffer.byteLength(text);
    if (picked.length === maxShards || chars + size > maxChars) break;
    picked.push({ id: s.id, path: s.path, kind: s.kind, statement: s.statement, target: s.target, verifiedOn: s.verifiedOn, reasons: s.reasons, text });
    chars += size;
  }
  const rest = hits.slice(picked.length).map((s) => s.id);
  return { shards: picked, omitted: rest.length, rest, withheld, index };
}

// Decision events from the repository and the local store; the latest event
// for a learning wins.
export function loadDecisions(root, localDir) {
  const events = [];
  for (const dir of [join(root, DECISION_DIR), localDir && join(localDir, "decisions")].filter(Boolean)) {
    if (!existsSync(dir)) continue;
    for (const f of readdirSync(dir).filter((f) => f.endsWith(".json"))) {
      try {
        events.push(JSON.parse(readFileSync(join(dir, f), "utf8")));
      } catch {}
    }
  }
  const latest = new Map();
  for (const e of events.sort((a, b) => String(a.at).localeCompare(String(b.at)))) latest.set(e.learning, e);
  return [...latest.values()];
}

// Tracked files keep ordinary permissions, unlike the owner-only local store.
export function writeAtomic(file, text) {
  mkdirSync(dirname(file), { recursive: true });
  const tmp = `${file}.${process.pid}.${randomBytes(4).toString("hex")}.tmp`;
  writeFileSync(tmp, text);
  renameSync(tmp, file);
}
