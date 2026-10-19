#!/usr/bin/env node
// Find this repository's Claude Code and Codex sessions and turn them into
// redacted episodes.
//
//   sessions.mjs survey  [--since D] [--until D]           content-free counts
//   sessions.mjs extract [--since D] [--until D] --out F   episodes as JSONL
//
// Dates are ISO dates (YYYY-MM-DD) and select families by their root
// session's start time. Run from the repository root.

import { execFileSync } from "node:child_process";
import { closeSync, existsSync, mkdirSync, openSync, readdirSync, readFileSync, readSync, statSync, writeFileSync } from "node:fs";
import { homedir } from "node:os";
import { dirname, join } from "node:path";
import { pathToFileURL } from "node:url";
import { completeThrough, episodesFor, familyOf, openingTask } from "./lib/episodes.mjs";
import { parseClaude, parseCodex } from "./lib/transcripts.mjs";

const IDLE_MS = 15 * 60 * 1000;

function args(argv) {
  const out = { _: [] };
  for (let k = 0; k < argv.length; k++) {
    const a = argv[k];
    if (a.startsWith("--")) out[a.slice(2)] = argv[k + 1]?.startsWith("--") || argv[k + 1] === undefined ? true : argv[++k];
    else out._.push(a);
  }
  return out;
}

function git(root, ...a) {
  try {
    return execFileSync("git", ["-C", root, ...a], { encoding: "utf8", stdio: ["ignore", "pipe", "ignore"] }).trim();
  } catch {
    return "";
  }
}

function globToRegExp(glob) {
  const g = glob.replace(/^~(?=\/)/, homedir());
  let re = "";
  for (let k = 0; k < g.length; k++) {
    const c = g[k];
    if (c === "*" && g[k + 1] === "*") {
      re += ".*";
      k++;
    } else if (c === "*") re += "[^/]*";
    else if (c === "?") re += "[^/]";
    else re += c.replace(/[.+^${}()|[\]\\]/g, "\\$&");
  }
  return new RegExp(`^${re}(/.*)?$`);
}

function normalizeRemote(url) {
  return String(url ?? "")
    .trim()
    .replace(/\.git$/, "")
    .replace(/^git@([^:]+):/, "https://$1/")
    .replace(/^ssh:\/\/git@/, "https://")
    .toLowerCase();
}

function repoMatcher(root) {
  const configPath = join(root, ".rhizome/session-memory/config.json");
  const config = existsSync(configPath) ? JSON.parse(readFileSync(configPath, "utf8")) : {};
  const roots = new Set([root]);
  for (const line of git(root, "worktree", "list", "--porcelain").split("\n")) {
    if (line.startsWith("worktree ")) roots.add(line.slice(9));
  }
  const patterns = [...roots].map((r) => globToRegExp(r)).concat((config.extraPaths ?? []).map(globToRegExp));
  const remote = normalizeRemote(git(root, "remote", "get-url", "origin"));
  const extraDepths = (config.extraPaths ?? []).map((g) => ({ re: globToRegExp(g), depth: g.replace(/^~(?=\/)/, homedir()).split("/").length }));
  return {
    config,
    // The repository root for a session's working directory, so paths from
    // sessions started in a subdirectory are still repository-relative.
    rootOf(cwd) {
      if (!cwd) return cwd;
      for (const r of roots) if (cwd === r || cwd.startsWith(r + "/")) return r;
      for (const { re, depth } of extraDepths) {
        const candidate = cwd.split("/").slice(0, depth).join("/");
        if (re.test(candidate)) return candidate;
      }
      return cwd;
    },
    matches(cwd, repoUrl) {
      if (repoUrl && remote && normalizeRemote(repoUrl) === remote) return true;
      return !!cwd && patterns.some((re) => re.test(cwd));
    },
  };
}

// Read just enough of a file to find what identifies its session.
function head(file, bytes = 256 * 1024) {
  const fd = openSync(file, "r");
  try {
    const buf = Buffer.alloc(bytes);
    const n = readSync(fd, buf, 0, bytes, 0);
    return buf.subarray(0, n).toString("utf8");
  } finally {
    closeSync(fd);
  }
}

function claudeCwd(file) {
  for (const line of head(file, 64 * 1024).split("\n")) {
    const m = line.match(/"cwd":"((?:\\.|[^"\\])*)"/);
    if (m) return JSON.parse(`"${m[1]}"`);
  }
  return null;
}

function codexMeta(file) {
  const first = head(file).split("\n")[0];
  try {
    const p = JSON.parse(first).payload ?? {};
    return { cwd: p.cwd, repoUrl: p.git?.repository_url };
  } catch {
    const cwd = first.match(/"cwd":"((?:\\.|[^"\\])*)"/)?.[1];
    const url = first.match(/"repository_url":"((?:\\.|[^"\\])*)"/)?.[1];
    return { cwd: cwd ? JSON.parse(`"${cwd}"`) : null, repoUrl: url ? JSON.parse(`"${url}"`) : null };
  }
}

function* walk(dir, depth) {
  if (!existsSync(dir)) return;
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const p = join(dir, entry.name);
    if (entry.isDirectory() && depth > 0) yield* walk(p, depth - 1);
    else if (entry.isFile() && entry.name.endsWith(".jsonl")) yield p;
  }
}

export function discover(root, { since, until, now = Date.now() }) {
  const matcher = repoMatcher(root);
  const sinceMs = since ? Date.parse(since) : 0;
  // Parents can start long before their children; read back far enough to
  // resolve families before filtering by family start.
  const scanFromMs = sinceMs ? sinceMs - 30 * 24 * 3600 * 1000 : 0;
  const report = { matched: 0, skippedActive: 0, skippedGuardian: 0, unreadable: 0 };
  const files = [];
  const claudeHome = join(homedir(), ".claude/projects");
  for (const file of walk(claudeHome, 3)) {
    const st = statSync(file);
    if (st.mtimeMs < scanFromMs) continue;
    const cwd = claudeCwd(file);
    if (!matcher.matches(cwd, null)) continue;
    files.push({ harness: "claude", file, mtime: st.mtimeMs, root: matcher.rootOf(cwd) });
  }
  for (const file of walk(join(homedir(), ".codex/sessions"), 4)) {
    const st = statSync(file);
    if (st.mtimeMs < scanFromMs) continue;
    const meta = codexMeta(file);
    if (!matcher.matches(meta.cwd, meta.repoUrl)) continue;
    files.push({ harness: "codex", file, mtime: st.mtimeMs, root: matcher.rootOf(meta.cwd) });
  }
  const sessions = [];
  for (const f of files) {
    if (now - f.mtime < IDLE_MS) {
      report.skippedActive++;
      continue;
    }
    let s;
    try {
      s = f.harness === "claude" ? parseClaude(f.file, { root: f.root }) : parseCodex(f.file, { root: f.root });
    } catch {
      report.unreadable++;
      continue;
    }
    if (s.kind === "guardian") {
      report.skippedGuardian++;
      continue;
    }
    s.completeThrough = completeThrough(s);
    sessions.push(s);
    report.matched++;
  }
  report.linked = linkSpawned(sessions);
  const byId = new Map(sessions.map((s) => [s.id, s]));
  for (const s of sessions) s.family = familyOf(s, byId);
  // A family's start is its earliest member, which is the root session.
  const familyStart = new Map();
  for (const s of sessions) {
    const t = s.start ? Date.parse(s.start) : s.mtime;
    familyStart.set(s.family, Math.min(familyStart.get(s.family) ?? Infinity, t));
  }
  const untilMs = until ? Date.parse(until) + 24 * 3600 * 1000 : Infinity;
  const selected = sessions.filter((s) => {
    const t = familyStart.get(s.family);
    return t >= sinceMs && t < untilMs;
  });
  report.inWindow = selected.length;
  return { sessions: selected, report, familyStart };
}

const normalize = (t) => String(t ?? "").replace(/\s+/g, " ").trim().slice(0, 300);

// Sessions started by another session (a delegated task, a spawned worker, a
// `codex exec` or `claude -p` run) join their starter's family. Without this,
// one investigation and its delegates count as independent recurrences.
function linkSpawned(sessions) {
  const counts = { delegated: 0, cliSpawn: 0 };
  const byText = new Map();
  const cliSpawns = [];
  for (const s of sessions) {
    for (const sp of s.spawns ?? []) {
      if (sp.text && normalize(sp.text).length >= 40) byText.set(normalize(sp.text), { from: s.id, ts: Date.parse(sp.ts) });
      if (sp.cli) cliSpawns.push({ from: s.id, ts: Date.parse(sp.ts) });
    }
  }
  for (const s of sessions) {
    if (s.parentId) continue;
    const opening = normalize(openingTask(s));
    const start = Date.parse(s.start ?? "") || s.mtime;
    let link = null;
    if (opening.length >= 40) {
      for (const [text, sp] of byText) {
        if (sp.from === s.id) continue;
        if ((opening.startsWith(text.slice(0, 120)) || text.startsWith(opening.slice(0, 120)) || opening.includes(text.slice(0, 120))) && sp.ts <= start + 5 * 60 * 1000) {
          link = { parentId: sp.from, basis: "delegated-task" };
          break;
        }
      }
    }
    if (!link && (s.kind === "exec" || /^sdk-/.test(s.entrypoint ?? ""))) {
      let best = null;
      for (const sp of cliSpawns) {
        if (sp.from === s.id) continue;
        const dt = start - sp.ts;
        if (dt >= -5000 && dt <= 60000 && (!best || dt < best.dt)) best = { ...sp, dt };
      }
      if (best) link = { parentId: best.from, basis: "cli-spawn-time" };
    }
    if (link) {
      s.parentId = link.parentId;
      s.linkBasis = link.basis;
      counts[link.basis === "delegated-task" ? "delegated" : "cliSpawn"]++;
    }
  }
  return counts;
}

function survey(root, opts) {
  const { sessions, report } = discover(root, opts);
  const count = (f) => sessions.reduce((acc, s) => ((acc[f(s)] = (acc[f(s)] ?? 0) + 1), acc), {});
  const origins = {};
  for (const s of sessions) {
    for (const m of s.messages) {
      if (m.role !== "user" || m.kind !== "text") continue;
      const key = `${s.harness}:${m.origin}:${m.confidence}`;
      origins[key] = (origins[key] ?? 0) + 1;
    }
  }
  return {
    window: { since: opts.since ?? null, until: opts.until ?? null },
    report,
    sessionsByKind: count((s) => `${s.harness}:${s.kind}`),
    families: new Set(sessions.map((s) => s.family)).size,
    userTextByOrigin: origins,
  };
}

function extract(root, opts) {
  const { sessions, report } = discover(root, opts);
  const lines = [];
  let redactions = 0;
  const byLens = {};
  for (const s of sessions) {
    const { episodes, redactions: r } = episodesFor(s, s.family, { through: s.completeThrough });
    redactions += r;
    for (const e of episodes) {
      byLens[e.lens] = (byLens[e.lens] ?? 0) + 1;
      lines.push(JSON.stringify(e));
    }
  }
  mkdirSync(dirname(opts.out), { recursive: true, mode: 0o700 });
  writeFileSync(opts.out, lines.join("\n") + (lines.length ? "\n" : ""), { mode: 0o600 });
  return { out: opts.out, report, families: new Set(sessions.map((s) => s.family)).size, episodes: lines.length, byLens, redactions };
}

const isMain = process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href;
if (isMain) {
  const a = args(process.argv.slice(2));
  const root = git(process.cwd(), "rev-parse", "--show-toplevel") || process.cwd();
  const opts = { since: a.since, until: a.until, out: a.out };
  const cmd = a._[0];
  let result;
  if (cmd === "survey") result = survey(root, opts);
  else if (cmd === "extract") {
    if (!opts.out) {
      console.error("extract needs --out <file>");
      process.exit(2);
    }
    result = extract(root, opts);
  } else {
    console.error("usage: sessions.mjs survey|extract [--since YYYY-MM-DD] [--until YYYY-MM-DD] [--out file]");
    process.exit(2);
  }
  process.stdout.write(JSON.stringify(result, null, 2) + "\n");
}
