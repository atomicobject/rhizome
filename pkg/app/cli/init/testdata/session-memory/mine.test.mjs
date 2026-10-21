// The mine workflow against fake clients: replay, incremental runs, cursors,
// crash resume, cluster identity, budgets, frozen bindings, and the ledger.
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { existsSync, mkdtempSync, readdirSync, readFileSync, rmSync, statSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { test } from "node:test";
import { SESSIONS, fakeClients, fakeDiscover, session, statuses, tempRepo, turn } from "./fakes.mjs";

const scripts = new URL("../../templates/starters/session-memory/agents/skills/session-memory/scripts/", import.meta.url);
const { mine } = await import(new URL("mine.mjs", scripts));
const P = await import(new URL("lib/prompts.mjs", scripts));
const { readJsonl } = await import(new URL("lib/store.mjs", scripts));

const repo = tempRepo();
const tmp = () => mkdtempSync(join(tmpdir(), "sm-state-"));
const run = (state, clients, over = {}) => mine({ root: repo, state, budget: 5, harness: "claude", discover: fakeDiscover([SESSIONS.s1, SESSIONS.s2]), log: () => {}, ...over }, clients);
const read = (state, name) => JSON.parse(readFileSync(join(state, name), "utf8"));
const manifests = (state) => readdirSync(join(state, "runs")).map((id) => read(state, `runs/${id}/manifest.json`));
const topics = (state) => read(state, "proposals.json").map((p) => p.statement.match(/Topic (\w+)/)[1]).sort();
const clusterIds = (state) => Object.fromEntries(Object.values(read(state, "pool/clusters.json")).map((c) => [c.statement.match(/Topic (\w+)/)[1], c.id]));
const prompts = (fake, prefix) => fake.calls.prompts.filter((p) => p.prompt.startsWith(prefix));

// Probes for haiku, opus, and sonnet, one extraction batch, one grouping
// chunk, and two verifications (alpha in tier A, beta in tier B).
const COLD_CALLS = 7;

test("a cold run proposes tiers A and B, holds tier C, and keeps state owner-only", async () => {
  const state = tmp();
  const fake = fakeClients();
  const r = await run(state, fake);
  assert.equal(r.status, "done");
  assert.equal(fake.calls.generate, COLD_CALLS);
  assert.deepEqual(topics(state), ["alpha", "beta"]);
  const tiers = Object.fromEntries(read(state, "proposals.json").map((p) => [p.statement.match(/Topic (\w+)/)[1], p.tier]));
  assert.deepEqual(tiers, { alpha: "A", beta: "B" });
  assert.equal(r.report.held.tierC, 1);
  // Windows has no owner-only mode bits; the state inherits the checkout's access control.
  if (process.platform !== "win32") {
    assert.equal(statSync(state).mode & 0o777, 0o700);
    assert.equal(statSync(join(state, "proposals.json")).mode & 0o777, 0o600);
  }
  assert.ok(!existsSync(join(state, "lock")));
  const m = manifests(state)[0];
  assert.equal(m.bindings.extract.model, "claude-haiku-5-5");
  assert.equal(m.bindings.extract.cliVersion, "2.1.300");
  assert.match(m.provenance.files["models.json"], /^[0-9a-f]{64}$/);
});

test("an unchanged replay makes zero model calls, with or without the mining state", async () => {
  const state = tmp();
  const fake = fakeClients();
  await run(state, fake);
  const before = { generate: fake.calls.generate, evaluate: fake.calls.evaluate };
  assert.equal((await run(state, fake)).status, "nothing-new");
  assert.deepEqual({ generate: fake.calls.generate, evaluate: fake.calls.evaluate }, before);
  // The result cache alone serves a rebuild of the pool, runs, and cursors.
  const proposals = read(state, "proposals.json");
  for (const name of ["pool", "runs", "cursors.json", "proposals.json"]) rmSync(join(state, name), { recursive: true });
  assert.equal((await run(state, fake)).status, "done");
  assert.deepEqual({ generate: fake.calls.generate, evaluate: fake.calls.evaluate }, before);
  assert.deepEqual(read(state, "proposals.json").map((p) => p.cluster), proposals.map((p) => p.cluster));
});

test("after a session is added, only its episodes cost calls", async () => {
  const state = tmp();
  await run(state, fakeClients());
  const ids = clusterIds(state);
  const fake = fakeClients();
  const r = await run(state, fake, { discover: fakeDiscover([SESSIONS.s1, SESSIONS.s2, SESSIONS.s3]) });
  assert.equal(r.status, "done");
  assert.ok(fake.calls.items.length > 0 && fake.calls.items.every((id) => id.startsWith("claude:s3:") || id.startsWith("k-")), fake.calls.items.join(" "));
  const map = prompts(fake, P.MAP_PROMPT);
  assert.equal(map.length, 1);
  assert.ok(!/claude:s[12]:/.test(map[0].prompt));
  assert.match(prompts(fake, P.GROUP_PROMPT)[0].prompt, /<existing-clusters>/);
  // alpha gained a family but keeps its id and verification; delta is new.
  const after = clusterIds(state);
  assert.equal(after.alpha, ids.alpha);
  assert.deepEqual(prompts(fake, P.PACKET_PREFIX).map((p) => p.prompt.match(/"statement": "Topic (\w+)/)[1]), ["delta"]);
  assert.deepEqual(topics(state), ["alpha", "beta", "delta"]);
});

test("the cursor stops before an unfinished trailing turn, and a later run reads that turn whole", async () => {
  const state = tmp();
  const start = "2026-10-11T09:00:00Z";
  const partial = session("s4", [turn(7, "epsilon"), turn(8, "zeta", { reply: false })], start);
  assert.equal(partial.completeThrough, 2);
  const first = fakeClients();
  await run(state, first, { discover: fakeDiscover([partial]) });
  assert.equal(read(state, "cursors.json")["claude:s4"].through, 2);
  assert.ok(first.calls.items.every((id) => !/^claude:s4:[3-9]/.test(id)));

  const whole = session("s4", [turn(7, "epsilon"), turn(8, "zeta")], start);
  assert.equal(whole.completeThrough, whole.messages.length - 1);
  const second = fakeClients();
  await run(state, second, { discover: fakeDiscover([whole]) });
  const episodes = [...new Set(second.calls.items.filter((id) => id.startsWith("claude:")).map((id) => id.split("#")[0]))].sort();
  assert.deepEqual(episodes, ["claude:s4:3", "claude:s4:5"]);
  assert.equal(read(state, "cursors.json")["claude:s4"].through, 5);
  assert.deepEqual(topics(state), ["epsilon", "zeta"]);
});

test("a killed run resumes without repeating a finished call", async () => {
  const reference = tmp();
  await run(reference, fakeClients(), { concurrency: 1 });
  const state = tmp();
  const child = spawnSync(process.execPath, [fileURLToPath(new URL("crash-run.mjs", import.meta.url)), state, repo, "4"]);
  // Windows reports a killed process by exit status, not by signal.
  if (process.platform === "win32") assert.notEqual(child.status, 0);
  else assert.equal(child.signal, "SIGKILL");
  assert.ok(existsSync(join(state, "lock")), "the dead run still holds its lock");
  const fake = fakeClients();
  const r = await run(state, fake, { concurrency: 1 });
  assert.equal(r.status, "done");
  assert.equal(fake.calls.generate, COLD_CALLS - 4);
  assert.equal(manifests(state).length, 1);
  assert.deepEqual(topics(state), topics(reference));
  assert.deepEqual(clusterIds(state), clusterIds(reference));
});

test("cluster ids survive a model that rewords every statement", async () => {
  const a = tmp();
  const b = tmp();
  await run(a, fakeClients());
  await run(b, fakeClients({ word: "now says, in other words," }));
  assert.deepEqual(clusterIds(a), clusterIds(b));
  assert.notDeepEqual(read(a, "proposals.json").map((p) => p.statement), read(b, "proposals.json").map((p) => p.statement));
});

test("a run stops cleanly at its budget and resumes to the same result", async () => {
  const state = tmp();
  const first = fakeClients();
  // Extraction and grouping fit in $0.03; a verification reservation does not.
  const r = await run(state, first, { budget: 0.03 });
  assert.deepEqual([r.status, r.reason], ["stopped", "budget"]);
  assert.ok(r.spend.usd <= 0.03);
  assert.equal(prompts(first, P.PACKET_PREFIX).length, 0);
  assert.ok(!existsSync(join(state, "proposals.json")));
  assert.equal(manifests(state)[0].status, "stopped");
  const second = fakeClients();
  assert.equal((await run(state, second)).status, "done");
  assert.equal(first.calls.generate + second.calls.generate, COLD_CALLS);
  assert.deepEqual(topics(state), ["alpha", "beta"]);
});

test("a resumed run keeps its frozen bindings and stops while one is unavailable", async () => {
  const state = tmp();
  await run(state, fakeClients(), { budget: 0.03 });
  const frozen = manifests(state)[0].bindings;

  const signedOut = fakeClients({ status: statuses({ loggedIn: false }) });
  const r = await run(state, signedOut);
  assert.deepEqual([r.status, r.reason], ["stopped", "binding"]);
  assert.match(r.message, /not signed in/);
  assert.equal(signedOut.calls.generate + signedOut.calls.evaluate, 0);

  // A frozen model that now resolves to another stops the run; nothing is cached.
  const moved = fakeClients({ reported: "claude-sonnet-5-7" });
  const m = await run(state, moved);
  assert.deepEqual([m.status, m.reason], ["stopped", "binding"]);
  assert.ok(readJsonl(join(state, "ledger.jsonl")).some((e) => !e.ok && /claude-sonnet-5-7 ran instead/.test(e.error)));

  // Abandoning restores the pool, so a new run with new bindings redoes the work.
  const other = tmp();
  await run(other, fakeClients(), { budget: 0.03 });
  assert.equal((await run(other, fakeClients(), { abandon: true })).status, "abandoned");
  const redo = fakeClients();
  assert.equal((await run(other, redo, { model: ["extract=claude:claude-sonnet-5-5:medium"] })).status, "done");
  assert.ok(prompts(redo, P.MAP_PROMPT).every((p) => p.model === "claude-sonnet-5-5"));
  assert.deepEqual(topics(other), ["alpha", "beta"]);

  // A new profile flag does not move a frozen role.
  const fake = fakeClients();
  assert.equal((await run(state, fake, { model: ["verify=claude:claude-opus-5-5:medium"] })).status, "done");
  assert.deepEqual(manifests(state)[0].bindings, frozen);
  const verifies = prompts(fake, P.PACKET_PREFIX);
  assert.equal(verifies.length, 2);
  assert.ok(verifies.every((p) => p.model === "claude-sonnet-5-5"));
});

test("a failed call is retried and recorded in the ledger, and never cached", async () => {
  const state = tmp();
  const failing = fakeClients({ fail: (prompt) => prompt.startsWith(P.MAP_PROMPT) });
  const r = await run(state, failing);
  assert.deepEqual([r.status, r.reason], ["stopped", "failed"]);
  const failures = readJsonl(join(state, "ledger.jsonl")).filter((e) => e.stage === "extract");
  assert.deepEqual(failures.map((e) => [e.attempt, e.ok]), [[1, false], [2, false]]);
  const key = failures[0].key;
  assert.ok(!existsSync(join(state, "cache", key.slice(0, 2), `${key}.json`)));

  const fake = fakeClients();
  assert.equal((await run(state, fake)).status, "done");
  assert.equal(prompts(fake, P.MAP_PROMPT).length, 1);
  assert.ok(existsSync(join(state, "cache", key.slice(0, 2), `${key}.json`)));

  // One transient failure is retried within the run.
  const once = tmp();
  let failed = false;
  const flaky = fakeClients({ fail: (prompt) => prompt.startsWith(P.MAP_PROMPT) && !failed && (failed = true) });
  assert.equal((await run(once, flaky)).status, "done");
  assert.deepEqual(readJsonl(join(once, "ledger.jsonl")).filter((e) => e.stage === "extract").map((e) => e.ok), [false, true]);
});

test("without --budget a run only estimates, and names who receives transcript text", async () => {
  const state = tmp();
  const fake = fakeClients();
  const r = await mine({ root: repo, state, budget: null, harness: "claude", discover: fakeDiscover([SESSIONS.s1]), log: () => {} }, fake);
  assert.equal(r.status, "estimate");
  assert.equal(fake.calls.generate + fake.calls.evaluate, 0);
  assert.deepEqual(r.estimate.transcriptsTo, ["claude", "typesafe (passages only)"]);
  assert.ok(r.estimate.rows.find((x) => x.role === "extract").usd > 0);
  assert.ok(!existsSync(join(state, "runs")));
});

test("review regressions: unreadable packet paths, cost totals, unreported probes, flag bindings, partial ranks", async () => {
  // A statement naming a tracked symlink to a directory is verified, not a crash.
  const linked = tmp();
  assert.equal((await run(linked, fakeClients({ word: "about `skilllink` states" }))).status, "done");

  // Claude's own total counts when token prices miss part of a reply.
  const priced = tmp();
  const r = await run(priced, fakeClients({ costUsd: 0.01 }));
  assert.ok(r.spend.usd >= 0.01 * COLD_CALLS - 1e-9, String(r.spend.usd));

  // A probe that names no model is a failed probe, never a frozen alias.
  const alias = tmp();
  await run(alias, fakeClients({ unreported: "haiku" }));
  const extract = manifests(alias)[0].bindings.extract;
  assert.deepEqual([extract.model, extract.choice], ["claude-sonnet-5-5", "fallback"]);
  assert.match(extract.skipped[0].reason, /did not report/);

  // A full model id bound by flag is not in Claude's alias catalog, and still resumes.
  const flagged = tmp();
  const model = ["extract=claude:claude-haiku-5-5:xhigh"];
  assert.equal((await run(flagged, fakeClients(), { model, budget: 0.03 })).reason, "budget");
  assert.equal((await run(flagged, fakeClients(), { model })).status, "done");

  // A binding with no known price is refused before any pipeline call, unless accepted.
  const unpriced = tmp();
  const custom = ["extract=claude:claude-custom-9:low"];
  await assert.rejects(run(unpriced, fakeClients(), { model: custom }), /no price is known for extract/);
  assert.equal((await run(unpriced, fakeClients(), { model: custom, allowUnpriced: true })).status, "done");

  // A budget too small for the availability probes stops before probing.
  const tiny = tmp();
  const probing = fakeClients();
  const stopped = await run(tiny, probing, { budget: 0.005 });
  assert.deepEqual([stopped.status, stopped.reason], ["stopped", "budget"]);
  assert.match(stopped.message, /cannot cover checking/);

  // Lost rank answers stop the run before tiers freeze into the verify plan.
  const ranked = tmp();
  const s = await run(ranked, fakeClients({ dropRank: true }));
  assert.deepEqual([s.status, s.reason], ["stopped", "failed"]);
  assert.ok(!existsSync(join(ranked, "runs", s.run, "verify-plan.json")));
  assert.equal((await run(ranked, fakeClients())).status, "done");
});

test("a dismissed learning is not proposed again, even when the pool is rebuilt", async () => {
  const root = tempRepo();
  const state = tmp();
  const fake = fakeClients();
  await run(state, fake, { root });
  const alpha = read(state, "proposals.json").find((p) => /Topic alpha/.test(p.statement));
  const { run: review } = await import(new URL("shards.mjs", scripts));
  const user = { found: true, typeName: "Person", path: "people/pat.md", title: "Pat" };
  review(["dismiss", alpha.cluster, "--reason", "not wanted", "--shared"], { root, state, user });
  // The shared decision alone suppresses it, as on a teammate's machine.
  for (const name of ["pool", "runs", "cursors.json", "proposals.json", "reviewed.json"]) rmSync(join(state, name), { recursive: true });
  await run(state, fake, { root });
  assert.deepEqual(topics(state), ["beta"]);
});
