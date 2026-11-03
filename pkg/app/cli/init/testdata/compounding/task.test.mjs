// Mining as a scheduled task (SPEC-0124 US8): task runs are not mined,
// mine.mjs --task-mode, and the compounding/mine task's check. Every rzm
// below is a fake, and no credential is real.
import assert from "node:assert/strict";
import { execFileSync, spawnSync } from "node:child_process";
import { mkdirSync, mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { test } from "node:test";
import { SESSIONS, fakeClients, fakeDiscover, session, statuses, tempRepo, turn } from "./fakes.mjs";

const scripts = new URL("../../templates/starters/compounding/module/scripts/", import.meta.url);
const { repoMatcher } = await import(new URL("sessions.mjs", scripts));
const { mine } = await import(new URL("mine.mjs", scripts));
const { check, MIN_EPISODES } = await import(new URL("task-check.mjs", scripts));
const { episodesFor } = await import(new URL("lib/episodes.mjs", scripts));
const tmp = (prefix) => mkdtempSync(join(tmpdir(), prefix));
const noWindows = { skip: process.platform === "win32" && "the fake rzm is a script with a shebang" };

test("discovery skips sessions run under .rhizome/task-state, even when the remote matches", () => {
  const root = tempRepo();
  execFileSync("git", ["-C", root, "remote", "add", "origin", "git@github.com:example/app.git"]);
  const m = repoMatcher(root);
  const remote = "https://github.com/example/app";
  assert.ok(m.matches(join(root, "pkg"), null));
  assert.ok(m.matches("/elsewhere/clone", remote));
  for (const cwd of [join(root, ".rhizome/task-state/worktrees/compounding_mine-1"), join(root, ".rhizome/task-state/checkout"), "/home/me/.config/rhizome/task-state/0123abcd/worktrees/x"]) {
    assert.equal(m.matches(cwd, null), false, cwd);
    assert.equal(m.matches(cwd, remote), false, cwd);
  }
});

// Each fake dry run is what mine.mjs returns in that situation.
const credentials = (configured) => async () => ({ configured, source: configured ? "direct" : "none" });
const stoppedRun = (reason) => ({ status: "estimate", episodes: 9, open: { run: "r1", status: "stopped", stoppedReason: reason, stoppedMessage: `${reason} message` } });

test("the check reports, in order: no credential, a blocked run, a run to resume, new episodes", async () => {
  let dryRuns = 0;
  const dry = (r) => async () => (dryRuns++, r);
  await assert.rejects(check({ credentials: credentials(false), dryRun: dry(stoppedRun("budget")) }), /no TypeSafe credential/);
  assert.equal(dryRuns, 0, "a missing credential is reported before state is read");
  await assert.rejects(check({ credentials: credentials(true), dryRun: dry(stoppedRun("budget")) }), /run r1 stopped \(budget\): budget message; .*--abandon/);
  // A run stopped for a credential or a binding that is available again
  // resumes rather than waiting on a person.
  for (const reason of ["credential", "binding"]) {
    const r = await check({ credentials: credentials(true), dryRun: dry(stoppedRun(reason)) });
    assert.deepEqual([r.needed, r.summary], [true, "resume mining run r1"], reason);
  }
  // A resumed run whose frozen binding is gone comes back as a dry-run stop.
  await assert.rejects(check({ credentials: credentials(true), dryRun: dry({ status: "stopped", reason: "binding", message: "verify: claude is not signed in", run: "r2" }) }), /run r2 stopped \(binding\)/);
  for (const open of [{ run: "r3", status: "open" }, { run: "r3", status: "stopped", stoppedReason: "failed", stoppedMessage: "verify k1: bad" }]) {
    const r = await check({ credentials: credentials(true), dryRun: dry({ status: "estimate", episodes: 0, open }) });
    assert.deepEqual([r.needed, r.summary], [true, "resume mining run r3"]);
  }
  const fresh = await check({ credentials: credentials(true), dryRun: dry({ status: "estimate", episodes: MIN_EPISODES, discovery: { inWindow: 2 } }) });
  assert.deepEqual([fresh.needed, fresh.summary], [true, `${MIN_EPISODES} new session episodes to mine`]);
  assert.equal((await check({ credentials: credentials(true), dryRun: dry({ status: "estimate", episodes: MIN_EPISODES - 1 }) })).needed, false);
  assert.equal((await check({ credentials: credentials(true), dryRun: dry({ status: "nothing-new" }) })).needed, false);
});

test("the task check resumes a real count-deferred pool without new episodes", async () => {
  const root = tempRepo();
  const state = tmp("sm-state-");
  const opts = { root, state, harness: "claude", discover: fakeDiscover([SESSIONS.s1]), log: () => {} };
  const deferred = await mine({ ...opts, budget: 5, verifyBudget: 0 }, fakeClients());
  assert.equal(deferred.status, "deferred");
  for (const harness of ["claude", undefined]) {
    const clients = fakeClients({ status: [statuses()[0], { ...statuses()[0], kind: "codex" }] });
    const dryRun = await mine({ ...opts, harness, budget: null }, clients);
    assert.equal(dryRun.episodes, 0);
    if (!harness) assert.equal(dryRun.estimate, null, "both signed-in harnesses leave the estimate unbound");
    assert.ok(dryRun.verification?.pending > 0, "unbound discovery must still report the backlog");
    const result = await check({ credentials: credentials(true), dryRun: async () => dryRun });
    assert.deepEqual([result.needed, result.summary], [true, `${dryRun.verification.pending} clusters await evidence verification`]);
    assert.equal(clients.calls.generate + clients.calls.evaluate, 0, "the task check spends nothing");
  }
});

test("a dry run names an open run and why it stopped, and counts episodes without a harness", async () => {
  const repo = tempRepo();
  const state = tmp("sm-state-");
  const opts = { root: repo, state, discover: fakeDiscover([SESSIONS.s1, SESSIONS.s2]), log: () => {} };
  // Both CLIs signed in and no --harness: still a count, and no model call.
  const both = fakeClients({ status: [statuses()[0], { ...statuses()[0], kind: "codex" }] });
  const unbound = await mine({ ...opts, budget: null }, both);
  assert.deepEqual([unbound.status, unbound.estimate, unbound.open], ["estimate", null, undefined]);
  assert.ok(unbound.episodes > 0);
  assert.match(unbound.next, /pass --harness/);
  assert.equal(both.calls.generate + both.calls.evaluate, 0);

  const stopped = await mine({ ...opts, harness: "claude", budget: 0.03 }, fakeClients());
  assert.deepEqual([stopped.status, stopped.reason], ["stopped", "budget"]);
  const dry = await mine({ ...opts, harness: "claude", budget: null }, fakeClients());
  assert.deepEqual(dry.open, { run: stopped.run, status: "stopped", stoppedReason: "budget", stoppedMessage: stopped.message });

  // A task run's worktree is gone by the time the run resumes: the resume
  // verifies in the checkout it runs from.
  const manifest = join(state, "runs", stopped.run, "manifest.json");
  writeFileSync(manifest, JSON.stringify({ ...JSON.parse(readFileSync(manifest, "utf8")), checkout: join(tmp("sm-gone-"), "removed-worktree") }));
  await mine({ ...opts, harness: "claude", budget: 0.03 }, fakeClients());
  assert.equal(JSON.parse(readFileSync(manifest, "utf8")).checkout, repo);
});

// A fake rzm: harness status reports Claude signed in, generate answers the
// availability probe, evaluate-batch fails as an unauthorized TypeSafe call
// does, and --check-credentials reports FAKE_TYPESAFE_SOURCE.
function fakeRzm(dir) {
  const path = join(dir, "rzm");
  const status = JSON.stringify(statuses());
  writeFileSync(
    path,
    `#!${process.execPath}
const [a, b, c] = process.argv.slice(2);
const aliases = { haiku: "claude-haiku-5-5", sonnet: "claude-sonnet-5-5", opus: "claude-opus-5-5" };
if (a === "harness") console.log(${JSON.stringify(status)});
else if (b === "evaluate-batch" && c === "--check-credentials") { const s = process.env.FAKE_TYPESAFE_SOURCE; console.log(JSON.stringify({ configured: s !== "none", source: s })); }
else if (b === "evaluate-batch") { console.error(JSON.stringify({ error: "TypeSafe request failed: HTTP 401 unauthorized" })); process.exit(1); }
else if (b === "generate") { const m = process.argv[process.argv.indexOf("--model") + 1]; console.log(JSON.stringify({ provider: "claude", model: aliases[m] ?? m, output: { ok: true }, usage: { inputTokens: 10, outputTokens: 5 } })); }
else process.exit(9);
`,
    { mode: 0o755 },
  );
  return path;
}

function sandbox() {
  const dir = tmp("sm-task-");
  const home = join(dir, "home");
  mkdirSync(home);
  // Discovery reads these homes, so the real ones are never read.
  return { dir, env: { ...process.env, RZM: fakeRzm(dir), CLAUDE_CONFIG_DIR: home, CODEX_HOME: home } };
}

test("--task-mode needs a budget, and a credential stop exits 4 with its reason", noWindows, () => {
  const { dir, env } = sandbox();
  const repo = tempRepo();
  const file = join(dir, "episodes.jsonl");
  const s = session("t1", [turn(1, "alpha"), turn(2, "beta")], "2026-10-10T10:00:00Z");
  writeFileSync(file, episodesFor(s, s.family, { through: s.completeThrough }).episodes.map((e) => JSON.stringify(e) + "\n").join(""));
  const mineCli = (...args) => spawnSync(process.execPath, [fileURLToPath(new URL("mine.mjs", scripts)), "--harness", "claude", "--state", join(dir, "state"), "--episodes", file, ...args], { cwd: repo, encoding: "utf8", env });

  const unbudgeted = mineCli("--task-mode");
  assert.notEqual(unbudgeted.status, 0);
  assert.match(unbudgeted.stderr, /--task-mode needs --budget/);

  const r = mineCli("--task-mode", "--budget", "1");
  assert.equal(r.status, 4, r.stderr);
  assert.match(r.stderr, /mining stopped \(credential\): Jev is unavailable/);
  assert.equal(JSON.parse(r.stdout).reason, "credential");
  // Outside task mode the same stop keeps exit 3.
  assert.equal(mineCli("--budget", "1").status, 3);
});

test("the check script prints its JSON, and exits 1 with the reason when Jev has no credential", noWindows, () => {
  const { env } = sandbox();
  const repo = tempRepo();
  writeFileSync(join(repo, ".gitignore"), ".rhizome/\n");
  const checkCli = (source) => spawnSync(process.execPath, [fileURLToPath(new URL("task-check.mjs", scripts))], { cwd: repo, encoding: "utf8", env: { ...env, FAKE_TYPESAFE_SOURCE: source } });
  const missing = checkCli("none");
  assert.equal(missing.status, 1);
  assert.equal(missing.stdout, "");
  assert.match(missing.stderr, /no TypeSafe credential/);
  const quiet = checkCli("team");
  assert.equal(quiet.status, 0, quiet.stderr);
  assert.deepEqual(JSON.parse(quiet.stdout), { needed: false, summary: "no new session episodes" });
});
