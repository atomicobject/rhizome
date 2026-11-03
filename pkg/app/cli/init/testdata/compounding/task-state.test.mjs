// Draft task attempts use fresh private state, even from linked worktrees.
// All sessions, model clients, configuration, and state below are synthetic.
import assert from "node:assert/strict";
import { execFileSync, spawnSync } from "node:child_process";
import { existsSync, mkdirSync, mkdtempSync, readdirSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { test } from "node:test";
import { SESSIONS, fakeClients, fakeDiscover, scratchRepo } from "./fakes.mjs";

const scripts = new URL("../../templates/starters/compounding/module/scripts/", import.meta.url);
const { openState, repoState, migrate } = await import(new URL("lib/state.mjs", scripts));
const { writeJson, Refused } = await import(new URL("lib/store.mjs", scripts));
const { mine } = await import(new URL("mine.mjs", scripts));
const { report } = await import(new URL("report.mjs", scripts));
const { lookup } = await import(new URL("lookup.mjs", scripts));
const { repoMatcher } = await import(new URL("sessions.mjs", scripts));
const { cachedShards, publishPrefixes } = await import(new URL("hook.mjs", scripts));
const temp = () => mkdtempSync(join(tmpdir(), "compounding-task-test-"));

async function inTest(data, run) {
  const old = { test: process.env.RZM_TASK_TEST, data: process.env.RZM_TASK_DATA_DIR };
  process.env.RZM_TASK_TEST = "1";
  if (data === undefined) delete process.env.RZM_TASK_DATA_DIR;
  else process.env.RZM_TASK_DATA_DIR = data;
  try {
    return await run();
  } finally {
    for (const [key, value] of [["RZM_TASK_TEST", old.test], ["RZM_TASK_DATA_DIR", old.data]]) {
      if (value === undefined) delete process.env[key];
      else process.env[key] = value;
    }
  }
}

function bytes(dir) {
  if (!existsSync(dir)) return {};
  return Object.fromEntries(readdirSync(dir, { recursive: true, withFileTypes: true }).filter((e) => e.isFile()).map((e) => {
    const path = join(e.parentPath ?? e.path, e.name);
    return [path.slice(dir.length + 1), readFileSync(path, "base64")];
  }).sort(([a], [b]) => a.localeCompare(b)));
}

function fixture() {
  const root = scratchRepo();
  const state = join(root, ".rhizome/compounding");
  writeJson(join(state, "proposals.json"), [{ cluster: "live-proposal" }]);
  writeJson(join(state, "runs/live-run/manifest.json"), { id: "live-run", status: "stopped" });
  writeJson(join(state, "config.json"), { extraPaths: ["/synthetic/live-checkout"] });
  writeJson(join(state, "models.json"), { version: "live-profile-must-not-be-read" });
  writeJson(join(state, "cursors.json"), { live: { through: 12 } });
  writeJson(join(state, "probes.json"), { live: "cached-probe" });
  writeJson(join(state, "cache/aa/live.json"), { live: "cached-response" });
  writeFileSync(join(root, ".gitignore"), ".rhizome/compounding/\n");
  execFileSync("git", ["add", ".gitignore"], { cwd: root });
  execFileSync("git", ["-c", "user.email=test@example.com", "-c", "user.name=Test", "commit", "-qm", "ignore private state"], { cwd: root });
  const checkout = join(temp(), "checkout");
  execFileSync("git", ["worktree", "add", "-q", "--detach", checkout], { cwd: root });
  const legacy = join(checkout, ".rhizome/compounding");
  writeJson(join(legacy, "runs/legacy-run/manifest.json"), { id: "legacy-run", status: "open" });
  return { root, state, checkout, legacy };
}

test("a task test selects fresh state across worktrees and bypasses migration and explicit state overrides", async () => {
  const f = fixture();
  const before = { live: bytes(f.state), legacy: bytes(f.legacy) };
  const first = temp();
  await inTest(first, () => {
    for (const checkout of [f.root, f.checkout]) {
      assert.equal(repoState(checkout).dir, join(first, "compounding"));
      const store = openState(checkout, { state: f.state });
      assert.equal(store.dir, join(first, "compounding"));
      assert.equal(store.read("proposals.json", null), null);
      store.write("cursors.json", { attempt: 1 });
      assert.equal(migrate(repoState(checkout), checkout), null);
    }
  });
  await inTest(temp(), () => {
    const store = openState(f.checkout);
    assert.equal(store.read("cursors.json", null), null, "a later attempt starts fresh");
  });
  assert.deepEqual({ live: bytes(f.state), legacy: bytes(f.legacy) }, before);
});

test("mining, reports, discovery configuration, and hook caches use the same isolated attempt", async () => {
  const f = fixture();
  const before = { live: bytes(f.state), legacy: bytes(f.legacy) };
  const data = temp();
  await inTest(data, async () => {
    const store = openState(f.checkout);
    writeJson(store.path("config.json"), { extraPaths: ["/synthetic/test-checkout"] });
    const matcher = repoMatcher(f.checkout);
    assert.equal(matcher.matches("/synthetic/test-checkout/pkg", null), true);
    assert.equal(matcher.matches("/synthetic/live-checkout/pkg", null), false);
    const result = await mine({ root: f.checkout, harness: "claude", budget: 10, discover: fakeDiscover([SESSIONS.s1, SESSIONS.s2]), log: () => {} }, fakeClients());
    assert.equal(result.status, "done");
    assert.ok(store.read("proposals.json", []).length > 0);
    assert.ok(store.read("cursors.json", {})["claude:s1"]);
    assert.ok(report({ root: f.checkout }).open.some((entry) => entry.proposal));
    assert.deepEqual(lookup({ root: f.checkout }).shards, []);
    cachedShards(f.checkout);
    publishPrefixes(f.checkout, []);
    assert.ok(existsSync(store.path("hook/shard-cache.json")));
    assert.ok(existsSync(store.path("hook/command-prefixes.json")));
  });
  assert.deepEqual({ live: bytes(f.state), legacy: bytes(f.legacy) }, before);
});

test("an invalid task context refuses every state entry point instead of using repository state", async () => {
  const f = fixture();
  const before = { live: bytes(f.state), legacy: bytes(f.legacy) };
  for (const data of [undefined, "", "   ", "relative/data"]) {
    await inTest(data, async () => {
      const refused = (err) => err instanceof Refused && /RZM_TASK_DATA_DIR.*absolute/.test(err.message);
      for (const run of [() => repoState(f.checkout), () => openState(f.checkout, { state: f.state }), () => repoMatcher(f.checkout), () => report({ root: f.checkout }), () => lookup({ root: f.checkout }), () => cachedShards(f.checkout), () => publishPrefixes(f.checkout, [])]) {
        assert.throws(run, refused);
      }
      await assert.rejects(mine({ root: f.checkout, budget: 10 }, fakeClients()), refused);
    });
  }
  assert.deepEqual({ live: bytes(f.state), legacy: bytes(f.legacy) }, before);
});

test("the task check's child mining process inherits isolated state", { skip: process.platform === "win32" && "the fake rzm uses a shebang" }, () => {
  const f = fixture();
  const before = { live: bytes(f.state), legacy: bytes(f.legacy) };
  const data = temp();
  const home = temp();
  const rzm = join(home, "rzm");
  writeFileSync(rzm, `#!${process.execPath}\nconsole.log(JSON.stringify({ configured: true, source: "fixture" }));\n`, { mode: 0o755 });
  const env = { ...process.env, RZM: rzm, RZM_TASK_TEST: "1", RZM_TASK_DATA_DIR: data, CLAUDE_CONFIG_DIR: home, CODEX_HOME: home };
  const result = spawnSync(process.execPath, [fileURLToPath(new URL("task-check.mjs", scripts))], { cwd: f.checkout, env, encoding: "utf8" });
  assert.equal(result.status, 0, result.stderr);
  assert.deepEqual(JSON.parse(result.stdout), { needed: false, summary: "no new session episodes" });
  assert.ok(existsSync(join(data, "compounding")));
  const invalid = spawnSync(process.execPath, [fileURLToPath(new URL("mine.mjs", scripts))], { cwd: f.checkout, env: { ...env, RZM_TASK_DATA_DIR: "relative" }, encoding: "utf8" });
  assert.equal(invalid.status, 2, invalid.stderr);
  assert.match(invalid.stderr, /RZM_TASK_DATA_DIR.*absolute/);
  assert.deepEqual({ live: bytes(f.state), legacy: bytes(f.legacy) }, before);
});
