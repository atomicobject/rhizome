// Where compounding's private state lives (SPEC-0124 US8): once per
// repository, in the main checkout's .rhizome/compounding/, so every
// worktree and every task run reads the same proposals and decisions. A bare
// repository has no main checkout; its state goes to a per-user folder keyed
// by the Git common directory, as task state does. Releases before this kept
// one store per worktree; the first run after upgrading migrates them.

import { execFileSync } from "node:child_process";
import { cpSync, existsSync, readdirSync, realpathSync, statSync } from "node:fs";
import { homedir } from "node:os";
import { basename, dirname, isAbsolute, join, relative, resolve, sep } from "node:path";
import { learningHash } from "./shards.mjs";
import { Refused, openStore, sha, writeJson } from "./store.mjs";

const STATE = ".rhizome/compounding";

// The real path of a file that may not exist yet: its nearest existing
// parent resolved, the rest appended.
function real(path) {
  const rest = [];
  let dir = resolve(path);
  while (!existsSync(dir) && dirname(dir) !== dir) {
    rest.unshift(basename(dir));
    dir = dirname(dir);
  }
  return join(realpathSync(dir), ...rest);
}

// Go's os.UserConfigDir, which task state uses for the same fallback.
function userConfigDir() {
  if (process.platform === "darwin") return join(homedir(), "Library", "Application Support");
  if (process.platform === "win32") return process.env.APPDATA;
  return process.env.XDG_CONFIG_HOME || join(homedir(), ".config");
}

export function repoState(cwd) {
  const git = (...a) => execFileSync("git", a, { cwd, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] }).trim();
  const common = real(git("rev-parse", "--path-format=absolute", "--git-common-dir"));
  const bare = git(`--git-dir=${common}`, "rev-parse", "--is-bare-repository") === "true";
  const main = !bare && basename(common) === ".git" ? dirname(common) : null;
  return { common, main, dir: main ? join(main, STATE) : join(userConfigDir(), "rhizome", "compounding", sha(common).slice(0, 16)) };
}

// Every checkout of the repository that still exists, as real paths.
export function worktrees(cwd) {
  const out = execFileSync("git", ["worktree", "list", "--porcelain"], { cwd, encoding: "utf8" });
  const paths = [];
  for (const block of out.split(/\n\n+/)) {
    const lines = block.split("\n");
    const line = lines.find((l) => l.startsWith("worktree "));
    if (!line || lines.includes("bare")) continue;
    try {
      paths.push(realpathSync(line.slice(9)));
    } catch {}
  }
  return paths;
}

// The checkout whose .rhizome/compounding/ a state directory is, or null for
// a directory outside any checkout.
export const stateHome = (dir) => (dir.split(sep).join("/").endsWith(`/${STATE}`) ? dirname(dirname(dir)) : null);

// Mining writes transcript text into its state directory, so a directory
// inside a checkout must be one Git ignores there.
export function refuseTracked(dir, roots) {
  const target = real(dir);
  for (const root of new Set(roots.filter(Boolean).map(real))) {
    const rel = relative(root, target);
    if (!rel || rel.startsWith("..") || isAbsolute(rel)) continue;
    const path = rel.split(sep).join("/");
    try {
      execFileSync("git", ["-C", root, "check-ignore", "-q", "--", `${path}/ledger.jsonl`], { stdio: "ignore" });
    } catch (err) {
      if (err.status === 1) throw new Error(`Git does not ignore ${path} in ${root}, so transcript text written there could be committed; rerun rzm init to restore .rhizome/.gitignore`);
    }
  }
}

// The repository's store, migrated on first use. `state` names another
// directory, for tests and evaluations, which is neither migrated nor
// guarded unless the caller asks.
export function openState(root, { state, guard = !state, log } = {}) {
  const repo = state ? null : repoState(root);
  const dir = state ?? repo.dir;
  if (guard) refuseTracked(dir, [root, repo?.main]);
  if (repo) migrate(repo, root, { log });
  return openStore(dir);
}

// Only one run is open at a time; a stopped run is open until it finishes or
// is abandoned.
export function openRun(store) {
  const dir = store.path("runs");
  const ids = existsSync(dir) ? readdirSync(dir).sort() : [];
  for (const id of ids.reverse()) {
    const run = store.read(`runs/${id}/manifest.json`, null);
    if (run && (run.status === "open" || run.status === "stopped")) return run;
  }
  return null;
}

export const lockOrRefuse = (store) => {
  try {
    store.lock();
  } catch (err) {
    if (err instanceof Refused) throw new Refused(`mining in progress, or another review is changing proposals (${err.message}); retry when it finishes`);
    throw err;
  }
};

// A legacy reviewed.json approval and the field naming the file it created.
const LEGACY = { approved: "path", finding: "path", documented: "decision" };
const isStore = (dir) => ["proposals.json", "reviewed.json", "cursors.json", "pool", "runs", "decisions"].some((f) => existsSync(join(dir, f)));
// What a store holds that is not its state: the lock migration itself takes,
// and the per-checkout hook state.
const NOT_STATE = new Set(["lock", "lock.reclaim", "hook", "migrated.json"]);
const newest = (dir) => Math.max(0, ...readdirSync(dir).filter((f) => !NOT_STATE.has(f)).map((f) => statSync(join(dir, f)).mtimeMs));
const stamp = (at) => String(at).replace(/\D/g, "").slice(0, 14);

// The legacy approval as a decision event, from the proposal its store kept.
// Who approved it and the HEAD it was published at were never recorded.
function legacyEvent(p, r, witness, checkout) {
  const response = r.action === "approved" ? "remember" : r.action === "documented" ? "document" : basename(witness).match(/^(fix|guard|ask)-/)?.[1] ?? p.response;
  const at = r.at ?? new Date().toISOString();
  return { learning: learningHash(p), cluster: p.cluster, kind: p.kind, statement: p.statement, appliesTo: p.appliesTo ?? [], decision: response === "document" ? "documented" : "approved", response, ...(r.document ? { document: r.document } : {}), witness, checkout, headAtPublish: null, publishedAt: at, decider: null, at, migrated: true, payload: { ...p, response } };
}

// Adopts the main checkout's store, else the most recently modified worktree
// store, and imports the decisions of every other store. Legacy approvals in
// every store become decision events from that store's proposals; one whose
// proposal is gone is reported, never guessed. Cursors, pools, and runs of
// the stores not adopted stay behind: their sessions are read again within
// the usual window, and the imported decisions keep reviewed learnings from
// coming back. Old stores stay where they are, marked migrated.
// ponytail: a crash mid-copy leaves a partial store that the rerun adopts;
// copy to a temporary folder first if that ever happens.
export function migrate(repo, cwd, { log = (m) => process.stderr.write(`${m}\n`) } = {}) {
  const target = repo.dir;
  if (existsSync(join(target, "migration.json"))) return null;
  const others = worktrees(cwd)
    .map((c) => join(c, STATE))
    .filter((d) => d !== target && isStore(d) && !existsSync(join(d, "migrated.json")));
  const into = openStore(target);
  const stores = [...(isStore(target) ? [target] : []), ...others];
  const held = [];
  try {
    for (const s of [into, ...others.map(openStore)]) {
      lockOrRefuse(s);
      held.push(s);
    }
    for (const dir of stores) {
      const run = openRun(openStore(dir));
      if (run) throw new Refused(`run ${run.id} in ${dir} is still open, so its state cannot move yet; finish it with \`mine.mjs --state ${dir} --budget <USD>\` or discard it with \`mine.mjs --state ${dir} --abandon\`, then retry`);
    }
    const adopted = isStore(target) ? target : others.sort((a, b) => newest(b) - newest(a))[0] ?? null;
    if (adopted && adopted !== target) cpSync(adopted, target, { recursive: true, filter: (src) => !NOT_STATE.has(relative(adopted, src).split(sep)[0]) });
    const summary = { at: new Date().toISOString(), adopted, stores, converted: 0, unconverted: [], imported: 0, undecided: 0 };
    const present = new Set(existsSync(into.path("decisions")) ? readdirSync(into.path("decisions")) : []);
    for (const dir of stores) {
      const store = openStore(dir);
      const proposals = store.read("proposals.json", []);
      const reviewed = store.read("reviewed.json", {});
      for (const [cluster, r] of Object.entries(reviewed)) {
        const witness = LEGACY[r.action] && r[LEGACY[r.action]];
        if (!witness) continue;
        const p = proposals.find((x) => x.cluster === cluster);
        if (!p) {
          summary.unconverted.push({ store: dir, cluster, action: r.action, witness });
          continue;
        }
        const e = legacyEvent(p, r, witness, stateHome(dir) ?? dir);
        into.write(`decisions/${e.learning}--${stamp(e.at)}-migrated.json`, e);
        summary.converted++;
      }
      if (dir === adopted) continue;
      const decided = new Set();
      for (const f of existsSync(store.path("decisions")) ? readdirSync(store.path("decisions")).filter((f) => f.endsWith(".json")) : []) {
        const e = store.read(`decisions/${f}`, null);
        if (e?.learning) decided.add(e.learning);
        if (present.has(f)) continue;
        into.write(`decisions/${f}`, e);
        present.add(f);
        summary.imported++;
      }
      summary.undecided += proposals.filter((p) => !reviewed[p.cluster] && !decided.has(learningHash(p))).length;
    }
    for (const dir of others) writeJson(join(dir, "migrated.json"), { to: target, at: summary.at });
    into.write("migration.json", summary);
    if (stores.length) log(`compounding: moved local state to ${target}${adopted && adopted !== target ? `, adopting ${adopted}` : ""}; converted ${summary.converted} earlier approvals (${summary.unconverted.length} without a proposal, listed in migration.json), imported ${summary.imported} decisions from ${others.filter((d) => d !== adopted).length} other stores, and left ${summary.undecided} undecided proposals behind in them`);
    return summary;
  } finally {
    for (const s of held) s.unlock();
  }
}
