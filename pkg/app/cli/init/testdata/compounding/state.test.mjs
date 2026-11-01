// One store per repository (SPEC-0124 US8): worktrees and task checkouts
// share it, approvals say where they stand, stale or concurrent changes are
// refused, and per-worktree stores from earlier releases migrate. Every
// repository, proposal, and person below is synthetic.
import assert from "node:assert/strict";
import { execFileSync, spawnSync } from "node:child_process";
import { existsSync, mkdirSync, mkdtempSync, readdirSync, readFileSync, realpathSync, rmSync, utimesSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { test } from "node:test";
import { scratchRepo } from "./fakes.mjs";

const scripts = new URL("../../templates/starters/compounding/module/scripts/", import.meta.url);
const { run } = await import(new URL("shards.mjs", scripts));
const { migrate, openState, repoState } = await import(new URL("lib/state.mjs", scripts));
const { openStore, writeJson } = await import(new URL("lib/store.mjs", scripts));
const { contentHash, loadShards } = await import(new URL("lib/shards.mjs", scripts));

const user = { found: true, typeName: "Person", path: "docs/people/pat-example.md", title: "Pat Example" };
const git = (root, ...args) => execFileSync("git", args, { cwd: root, encoding: "utf8" });
const quiet = { log: () => {} };
const read = (file) => JSON.parse(readFileSync(file, "utf8"));
const at = (root, ...argv) => run(argv, { root, user });

// A main checkout on `main` that ignores compounding's state, and one
// worktree per name on a branch of that name.
function checkouts(...names) {
  const main = realpathSync.native(scratchRepo());
  git(main, "branch", "-M", "main");
  writeFileSync(join(main, ".gitignore"), ".rhizome/compounding/\n.rhizome/task-state/\n");
  mkdirSync(join(main, ".rhizome"));
  writeFileSync(join(main, ".rhizome/config.yml"), "tasks:\n  primaryRef: main\n");
  git(main, "add", "-A");
  git(main, "commit", "-qm", "ignore state");
  const wt = {};
  for (const name of names) {
    wt[name] = join(realpathSync.native(mkdtempSync(join(tmpdir(), "sm-wt-"))), name);
    git(main, "worktree", "add", "-q", "-b", name, wt[name]);
  }
  return { main, wt, state: join(main, ".rhizome/compounding") };
}

const proposal = (cluster, over = {}) => ({
  cluster,
  tier: "A",
  families: 2,
  kind: "rule",
  statement: `Busy timeout ${cluster}: a value of 0 means the 30-second default.`,
  why: "Callers expecting no wait block for 30 seconds.",
  appliesTo: ["pkg/store/**/*.go"],
  commands: [],
  response: "remember",
  responseReason: "A lasting rule.",
  verification: [{ path: "pkg/store/db.go", checked: "The default is applied when the value is 0." }],
  evidence: [{ episode: "claude:s1:4", harness: "claude", session: "s1", ts: "2026-10-01T10:00:00Z", lens: "discovery", origin: "agent", toolCalls: 7 }],
  ...over,
});

// Proposals verified at main's HEAD, in a store already migrated.
function propose({ main, state }, list) {
  const commit = git(main, "rev-parse", "HEAD").trim();
  writeJson(join(state, "proposals.json"), list.map((p) => ({ verified: { commit }, ...p })));
  migrate(repoState(main), main, quiet);
}

// Each proposal's status from `list --all`, with where an approval is.
const standing = (root) => Object.fromEntries(at(root, "list", "--all").map((p) => [p.cluster, [p.status, p.ref ?? p.branch ?? p.checkout].filter(Boolean).join(" ")]));

test("every worktree and task checkout reads the main checkout's state, and the ignore guard checks it there", () => {
  const repo = checkouts("x");
  const task = join(repo.main, ".rhizome/task-state/worktrees/t1");
  mkdirSync(dirname(task), { recursive: true });
  git(repo.main, "worktree", "add", "-q", "--detach", task);
  propose(repo, [proposal("k-1")]);
  for (const root of [repo.main, repo.wt.x, task]) {
    assert.equal(repoState(root).dir, repo.state);
    assert.deepEqual(at(root, "list").map((p) => p.cluster), ["k-1"]);
  }

  // A worktree that ignores the folder does not make the main checkout ignore it.
  const other = realpathSync.native(scratchRepo());
  const y = join(realpathSync.native(mkdtempSync(join(tmpdir(), "sm-wt-"))), "y");
  git(other, "worktree", "add", "-q", "-b", "y", y);
  writeFileSync(join(y, ".gitignore"), ".rhizome/compounding/\n");
  git(y, "add", "-A");
  git(y, "commit", "-qm", "ignore here only");
  assert.throws(() => openState(y, { guard: true }), /Git does not ignore \.rhizome\/compounding in /);
});

test("an approval made in one worktree shows from another whether it is present, uncommitted there, committed, merged, or missing", () => {
  const repo = checkouts("x", "y");
  const { wt } = repo;
  const documentEdit = { document: "docs/store.md", old: "# Store\n", new: "# Store\n\nA busy timeout of 0 means the 30-second default.\n" };
  propose(repo, [proposal("k-1"), proposal("k-2"), proposal("k-3"), proposal("k-doc", { response: "document", edit: documentEdit, documentHash: contentHash("# Store\n") })]);

  const { approved } = at(wt.x, "approve", "k-1", "--confirm-publish");
  const [event] = readdirSync(join(repo.state, "decisions")).map((f) => read(join(repo.state, "decisions", f)));
  assert.deepEqual(
    { witness: event.witness, checkout: event.checkout, head: event.headAtPublish, response: event.response, payload: event.payload.cluster },
    { witness: approved, checkout: wt.x, head: git(wt.x, "rev-parse", "HEAD").trim(), response: "remember", payload: "k-1" },
  );
  assert.ok(event.publishedAt && event.learning);
  assert.equal(standing(wt.x)["k-1"], "present");
  assert.equal(standing(wt.y)["k-1"], `published-elsewhere ${wt.x}`);
  git(wt.x, "add", approved);
  git(wt.x, "commit", "-qm", "k-1");
  assert.equal(standing(wt.y)["k-1"], "committed-elsewhere x");
  git(repo.main, "merge", "-q", "--ff-only", "x");
  assert.equal(standing(wt.y)["k-1"], "merged main");

  // Discarded while its branch still exists: back to review from the saved
  // payload, with the earlier decision shown, and approvable again.
  const second = at(wt.x, "approve", "k-2", "--confirm-publish").approved;
  rmSync(join(wt.x, second));
  const back = at(wt.y, "list").find((p) => p.cluster === "k-2");
  assert.deepEqual([back.status, back.previous.witness, back.previous.checkout, back.previous.decider], ["missing", second, wt.x, "Pat Example"]);
  assert.equal(at(wt.y, "approve", "k-2", "--confirm-publish").approved, second);
  assert.equal(standing(wt.y)["k-2"], "present");

  // Committed and then deleted on every branch: history still holds it, a
  // branch tip does not.
  const third = at(wt.x, "approve", "k-3", "--confirm-publish").approved;
  git(wt.x, "add", third);
  git(wt.x, "commit", "-qm", "k-3");
  git(wt.x, "rm", "-q", third);
  git(wt.x, "commit", "-qm", "drop k-3");
  assert.notEqual(git(wt.y, "log", "--all", "--format=%h", "--", third).trim(), "");
  assert.equal(standing(wt.y)["k-3"], "missing");

  // A document approval's witness is its decision file, not the document,
  // which is still on main.
  const doc = at(wt.x, "approve", "k-doc", "--confirm-publish");
  assert.equal(standing(wt.y)["k-doc"], `published-elsewhere ${wt.x}`);
  git(wt.x, "checkout", "--", "docs/store.md");
  rmSync(join(wt.x, doc.decision));
  assert.equal(git(wt.y, "cat-file", "-t", "main:docs/store.md").trim(), "blob");
  assert.equal(standing(wt.y)["k-doc"], "missing");
});

test("approve and dismiss refuse a proposal or checkout that changed since it was shown", () => {
  const repo = checkouts("x");
  propose(repo, [proposal("k-1")]);
  const [shown] = at(repo.wt.x, "list");
  assert.match(shown.revision, /^[0-9a-f]{16}$/);
  assert.equal(shown.head, git(repo.wt.x, "rev-parse", "HEAD").trim());
  // A later run redrafted the proposal.
  propose(repo, [proposal("k-1", { why: "Callers block for 30 seconds without saying so." })]);
  assert.throws(() => at(repo.wt.x, "approve", "k-1", "--confirm-publish", "--revision", shown.revision, "--head", shown.head), /k-1 changed since it was shown/);
  const again = at(repo.wt.x, "show", "k-1");
  assert.throws(() => at(repo.wt.x, "dismiss", "k-1", "--reason", "not wanted", "--local", "--revision", again.revision, "--head", "0".repeat(40)), /checkout moved since k-1 was shown/);
  assert.ok(at(repo.wt.x, "approve", "k-1", "--confirm-publish", "--revision", again.revision, "--head", again.head).approved);
});

test("a held store lock refuses a change with exit 2, mining in progress", () => {
  const repo = checkouts("x");
  propose(repo, [proposal("k-1")]);
  const mining = openStore(repo.state);
  mining.lock();
  let r;
  try {
    r = spawnSync(process.execPath, [fileURLToPath(new URL("shards.mjs", scripts)), "dismiss", "k-1", "--reason", "not wanted", "--local"], { cwd: repo.wt.x, encoding: "utf8" });
  } finally {
    mining.unlock();
  }
  assert.equal(r.status, 2, r.stderr);
  assert.match(r.stderr, /^refused: mining in progress/);
});

test("the first run adopts the main checkout's store, converts legacy approvals in every store, imports decisions, and waits for open runs", () => {
  const repo = checkouts("w");
  const { main, state } = repo;
  writeJson(join(state, "proposals.json"), [proposal("k-a"), proposal("k-b")]);
  writeJson(join(state, "reviewed.json"), { "k-a": { action: "approved", path: "docs/memory/context-shards/rule-a.md", at: "2026-10-01T10:00:00.000Z" } });
  writeJson(join(state, "cursors.json"), { "claude:s1": { through: 4 } });
  const old = join(repo.wt.w, ".rhizome/compounding");
  writeJson(join(old, "proposals.json"), [proposal("k-c", { response: "fix" }), proposal("k-d"), proposal("k-e")]);
  writeJson(join(old, "reviewed.json"), {
    "k-c": { action: "finding", path: "docs/memory/agent-findings/fix-c.md", at: "2026-10-02T10:00:00.000Z" },
    "k-gone": { action: "approved", path: "docs/memory/context-shards/rule-gone.md", at: "2026-10-02T11:00:00.000Z" },
  });
  writeJson(join(old, "cursors.json"), { "claude:s9": { through: 9 } });
  writeJson(join(old, "decisions/dismissed-x.json"), { learning: "f".repeat(16), cluster: "k-x", decision: "dismissed", reason: "covered", decider: "Pat Example", at: "2026-10-03T00:00:00.000Z" });
  writeJson(join(old, "runs/r1/manifest.json"), { id: "r1", status: "stopped" });

  assert.throws(() => at(repo.wt.w, "list"), /run r1 in .* is still open/);
  assert.equal(existsSync(join(state, "migration.json")), false);
  writeJson(join(old, "runs/r1/manifest.json"), { id: "r1", status: "done" });
  const lines = [];
  const summary = migrate(repoState(repo.wt.w), repo.wt.w, { log: (m) => lines.push(m) });
  assert.equal(summary.adopted, state);
  assert.deepEqual([summary.converted, summary.imported, summary.undecided], [2, 1, 2]);
  assert.deepEqual(summary.unconverted.map((u) => u.cluster), ["k-gone"]);
  assert.match(lines.join("\n"), /left 2 undecided proposals behind/);

  const events = Object.fromEntries(readdirSync(join(state, "decisions")).map((f) => read(join(state, "decisions", f))).map((e) => [e.cluster, e]));
  assert.deepEqual([events["k-a"].response, events["k-a"].witness, events["k-a"].checkout, events["k-a"].payload.statement], ["remember", "docs/memory/context-shards/rule-a.md", main, proposal("k-a").statement]);
  assert.deepEqual([events["k-c"].response, events["k-c"].witness, events["k-c"].checkout], ["fix", "docs/memory/agent-findings/fix-c.md", repo.wt.w]);
  assert.equal(events["k-x"].decision, "dismissed");
  // Only the adopted store's cursors count; the old store stays, marked.
  assert.deepEqual(read(join(state, "cursors.json")), { "claude:s1": { through: 4 } });
  assert.equal(read(join(old, "migrated.json")).to, state);
  assert.ok(existsSync(join(old, "proposals.json")));
  assert.equal(migrate(repoState(main), main, quiet), null);
  // Neither converted approval's file exists anywhere, so both are back in review.
  assert.deepEqual(Object.fromEntries(at(main, "list").map((p) => [p.cluster, p.status])), { "k-a": "missing", "k-b": "pending", "k-c": "missing" });
});

test("with no store in the main checkout, the most recently modified worktree store is adopted", () => {
  const repo = checkouts("p", "q");
  for (const [name, day] of [["p", "2026-10-01"], ["q", "2026-10-05"]]) {
    const dir = join(repo.wt[name], ".rhizome/compounding");
    writeJson(join(dir, "cursors.json"), { [`claude:${name}`]: { through: 1 } });
    writeJson(join(dir, "proposals.json"), [proposal(`k-${name}`)]);
    for (const f of readdirSync(dir)) utimesSync(join(dir, f), new Date(day), new Date(day));
  }
  // Hook state alone is not a store.
  writeJson(join(repo.state, "hook/command-prefixes.json"), []);
  const summary = migrate(repoState(repo.main), repo.main, quiet);
  assert.equal(summary.adopted, join(repo.wt.q, ".rhizome/compounding"));
  assert.equal(summary.undecided, 1);
  assert.deepEqual(read(join(repo.state, "cursors.json")), { "claude:q": { through: 1 } });
  assert.deepEqual(read(join(repo.state, "hook/command-prefixes.json")), []);
  assert.deepEqual(at(repo.wt.p, "list").map((p) => p.cluster), ["k-q"]);
});

test("mine refuses with exit 2 and a reason while migration waits for an open run", () => {
  const repo = checkouts("w");
  writeJson(join(repo.state, "proposals.json"), [proposal("k-a")]);
  writeJson(join(repo.wt.w, ".rhizome/compounding/runs/r1/manifest.json"), { id: "r1", status: "stopped" });
  const mine = spawnSync(process.execPath, [fileURLToPath(new URL("mine.mjs", scripts)), "--harness", "claude"], { cwd: repo.wt.w, encoding: "utf8", env: { ...process.env, RZM: "/nonexistent/rzm" } });
  assert.equal(mine.status, 2, mine.stderr);
  assert.match(mine.stderr, /^refused: .*run r1 .* is still open/m);
  assert.doesNotMatch(mine.stderr, /\n\s+at /, "no stack trace");
});

test("migration recovers an approval whose proposal mining already dropped, and keeps shared dismissals from other checkouts", () => {
  const repo = checkouts("w");
  const { main, state } = repo;
  const old = join(repo.wt.w, ".rhizome/compounding");
  // k-snap survives only in a run snapshot; k-pool only as its verified cluster.
  writeJson(join(old, "proposals.json"), []);
  writeJson(join(old, "runs/r1/manifest.json"), { id: "r1", status: "done" });
  writeJson(join(old, "runs/r1/pool-before.json"), { "proposals.json": [proposal("k-snap")] });
  const pooled = proposal("k-pool");
  writeJson(join(old, "pool/clusters.json"), { "k-pool": { id: "k-pool", tier: "A", families: ["f1", "f2"], evidence: [], kind: pooled.kind, statement: pooled.statement, verification: { decision: "propose", kind: pooled.kind, statement: pooled.statement, appliesTo: pooled.appliesTo, response: "remember", run: "r1", at: "2026-10-01T10:00:00.000Z" } } });
  writeJson(join(old, "reviewed.json"), {
    "k-snap": { action: "approved", path: "docs/memory/context-shards/rule-snap.md", at: "2026-10-02T10:00:00.000Z" },
    "k-pool": { action: "approved", path: "docs/memory/context-shards/rule-pool.md", at: "2026-10-02T11:00:00.000Z" },
  });
  // A shared dismissal made in the worktree and never committed.
  const dismissal = { learning: "e".repeat(16), cluster: "k-no", kind: "rule", statement: "Not wanted.", decision: "dismissed", reason: "covered", decider: "Pat Example", at: "2026-10-03T00:00:00.000Z" };
  writeJson(join(repo.wt.w, "docs/memory/context-shards/decisions/dismissed-no.json"), dismissal);

  const summary = migrate(repoState(main), main, quiet);
  assert.equal(summary.converted, 2);
  assert.deepEqual(summary.unconverted, []);
  const events = readdirSync(join(state, "decisions")).map((f) => read(join(state, "decisions", f)));
  assert.deepEqual(events.filter((e) => e.migrated).map((e) => e.cluster).sort(), ["k-pool", "k-snap"]);
  assert.ok(events.some((e) => e.decision === "dismissed" && e.learning === dismissal.learning), "the worktree's shared dismissal is kept");
  assert.deepEqual(at(main, "list", "--all").map((p) => [p.cluster, p.status]).sort(), [["k-pool", "missing"], ["k-snap", "missing"]]);
});

test("a finding approved again after it went missing keeps its assignee", () => {
  const repo = checkouts();
  const { main } = repo;
  propose(repo, [proposal("k-fix", { response: "fix", change: "Default the timeout explicitly." })]);
  const lee = { found: true, typeName: "Person", path: "docs/people/lee-example.md", title: "Lee Example" };
  const people = (_root, name) => (name.toLowerCase().startsWith("lee") ? lee : name.toLowerCase().startsWith("pat") ? user : { found: false });
  const first = run(["approve", "k-fix", "--confirm-publish", "--assignee", "Lee Example"], { root: main, user, people });
  assert.match(first.assignee, /lee-example/);
  rmSync(join(main, first.finding));
  assert.equal(at(main, "list").find((p) => p.cluster === "k-fix").status, "missing");
  const again = run(["approve", "k-fix", "--confirm-publish"], { root: main, user, people });
  assert.match(again.assignee, /lee-example/, "the recovered payload keeps the earlier assignee");

  // The default assignee is the first reviewer, and a teammate restoring the
  // finding does not take it over.
  propose(repo, [proposal("k-own", { response: "fix", change: "Default the timeout explicitly." })]);
  const own = at(main, "approve", "k-own", "--confirm-publish");
  assert.match(own.assignee, /pat-example/);
  rmSync(join(main, own.finding));
  const restored = run(["approve", "k-own", "--confirm-publish"], { root: main, user: lee, people });
  assert.match(restored.assignee, /pat-example/);
});

test("migration takes an approved shard's published wording, and marks an approval it can only recover as a draft", () => {
  const repo = checkouts();
  const { main, state } = repo;
  writeJson(join(state, "proposals.json"), [proposal("k-ed"), proposal("k-lost", { response: "fix" })]);
  writeJson(join(state, "reviewed.json"), {
    "k-ed": { action: "approved", path: "docs/memory/context-shards/rule-ed.md", at: "2026-10-01T10:00:00.000Z" },
    "k-lost": { action: "finding", path: "docs/memory/agent-findings/fix-lost.md", at: "2026-10-01T11:00:00.000Z" },
  });
  // The person edited the shard's wording before approving it.
  mkdirSync(join(main, "docs/memory/context-shards"), { recursive: true });
  writeFileSync(join(main, "docs/memory/context-shards/rule-ed.md"), "---\ntype: ContextShard\nkind: rule\nsummary: Edited wording the person approved.\napplies-to:\n  - pkg/store/**/*.go\n---\n\n# Edited wording\n");

  migrate(repoState(main), main, quiet);
  const events = Object.fromEntries(readdirSync(join(state, "decisions")).map((f) => read(join(state, "decisions", f))).map((e) => [e.cluster, e]));
  assert.equal(events["k-ed"].payload.statement, "Edited wording the person approved.");
  assert.notEqual(events["k-ed"].payload.exact, false);
  assert.equal(events["k-lost"].payload.exact, false);
  const lost = at(main, "list").find((p) => p.cluster === "k-lost");
  assert.deepEqual([lost.status, lost.previous.draft], ["missing", true]);
});

test("a recurrence stays in review when an older approval of its cluster also exists", () => {
  const repo = checkouts();
  const { main, state } = repo;
  const newer = "docs/memory/agent-findings/fix-new.md";
  propose(repo, [proposal("k-r", { response: "fix", change: "Default the timeout explicitly.", recurrence: { finding: newer, episodes: ["claude:s2:1"] } })]);
  const event = (name, witness, at, statement) => writeJson(join(state, "decisions", name), { learning: name.slice(0, 16).padEnd(16, "0"), cluster: "k-r", decision: "approved", response: "fix", witness, checkout: main, at, publishedAt: at, payload: proposal("k-r", { response: "fix", statement }) });
  // The first approval, then the approval of the finding the recurrence reopened, edited.
  event("aaaaaaaaaaaaaaaa--old.json", "docs/memory/agent-findings/fix-old.md", "2026-10-01T10:00:00.000Z", "Original wording.");
  event("bbbbbbbbbbbbbbbb--new.json", newer, "2026-10-05T10:00:00.000Z", "Edited wording.");
  const listed = at(main, "list").find((p) => p.cluster === "k-r");
  assert.ok(listed, "the recurrence is offered for review");
  assert.equal(listed.status, "pending", "as a new proposal, not as a missing approval");
});

test("a revised shard that goes missing comes back with its revised wording", () => {
  const repo = checkouts();
  const { main } = repo;
  propose(repo, [proposal("k-rev")]);
  const { approved } = at(main, "approve", "k-rev", "--confirm-publish");
  const shard = loadShards(main).find((x) => x.path === approved);
  const edit = join(mkdtempSync(join(tmpdir(), "sm-edit-")), "edit.json");
  writeFileSync(edit, JSON.stringify({ statement: "Busy timeout k-rev: 0 means the 30-second default; pass -1 to wait forever." }));
  at(main, "revise", approved, "--expect", shard.contentHash, "--edit", edit, "--confirm-publish");
  rmSync(join(main, approved));
  const back = at(main, "list").find((p) => p.cluster === "k-rev");
  assert.equal(back.status, "missing");
  assert.equal(back.statement, "Busy timeout k-rev: 0 means the 30-second default; pass -1 to wait forever.");

  // Retiring the revised shard retires every version, so the first wording
  // does not come back either.
  at(main, "approve", "k-rev", "--confirm-publish");
  const revised = loadShards(main).find((x) => x.path === approved);
  at(main, "retire", approved, "--expect", revised.contentHash, "--reason", "no longer true");
  assert.equal(at(main, "list").find((p) => p.cluster === "k-rev"), undefined);
});

test("a shard retired in one worktree does not come back for review in another", () => {
  const repo = checkouts("a", "b");
  propose(repo, [proposal("k-ret")]);
  const { approved } = at(repo.wt.a, "approve", "k-ret", "--confirm-publish");
  const shard = loadShards(repo.wt.a).find((x) => x.path === approved);
  at(repo.wt.a, "retire", approved, "--expect", shard.contentHash, "--reason", "no longer true");
  assert.equal(at(repo.wt.b, "list").find((p) => p.cluster === "k-ret"), undefined);
});

test("dismissing a revised approval that went missing keeps every wording of it out of review", () => {
  const repo = checkouts();
  const { main } = repo;
  propose(repo, [proposal("k-dis")]);
  const { approved } = at(main, "approve", "k-dis", "--confirm-publish");
  const shard = loadShards(main).find((x) => x.path === approved);
  const edit = join(mkdtempSync(join(tmpdir(), "sm-edit-")), "edit.json");
  writeFileSync(edit, JSON.stringify({ statement: "Busy timeout k-dis: revised wording." }));
  at(main, "revise", approved, "--expect", shard.contentHash, "--edit", edit, "--confirm-publish");
  rmSync(join(main, approved));
  assert.equal(at(main, "list").find((p) => p.cluster === "k-dis").statement, "Busy timeout k-dis: revised wording.");
  at(main, "dismiss", "k-dis", "--reason", "not wanted after all", "--local");
  assert.equal(at(main, "list").find((p) => p.cluster === "k-dis"), undefined);
});
