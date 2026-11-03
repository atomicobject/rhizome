// Exercise the actual mine stages with deterministic model replies. These
// assert scheduling, provenance and evidence delivery, not model accuracy.
import assert from "node:assert/strict";
import { mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { SESSIONS, fakeClients, fakeDiscover, tempRepo } from "./fakes.mjs";

const scripts = new URL("../../templates/starters/compounding/module/scripts/", import.meta.url);
const { mine } = await import(new URL("mine.mjs", scripts));
const P = await import(new URL("lib/prompts.mjs", scripts));
const read = (state, name) => JSON.parse(readFileSync(join(state, name), "utf8"));
function fixture() {
  const root = tempRepo();
  const state = mkdtempSync(join(tmpdir(), "sm-queue-"));
  const opts = { root, state, harness: "claude", budget: 5, discover: fakeDiscover([SESSIONS.s1, SESSIONS.s2]), log: () => {}, concurrency: 1 };
  return { state, run: (clients = fakeClients(), over = {}) => mine({ ...opts, ...over }, clients) };
}
const verifiedCalls = (fake) => fake.calls.prompts.filter((p) => p.prompt.startsWith(P.PACKET_PREFIX));

test("count-deferred candidates resume without new episodes or verify-held, then finish without duplicates", async () => {
  const f = fixture();
  const first = await f.run(fakeClients(), { verifyBudget: 0 });
  assert.equal(first.status, "deferred");
  assert.equal(first.report.verification.examined, 1);
  assert.deepEqual(first.report.verification.deferred, { count: 2, reasons: { count_limit: 2 } });
  assert.match(first.report.verification.next.command, /--harness claude --budget 5 --state '/);
  const dry = await f.run(fakeClients(), { budget: null, verifyBudget: 1 });
  assert.equal(dry.episodes, 0);
  assert.equal(dry.verification.pending, 2);
  assert.ok(dry.estimate.rows.find((r) => r.role === "verify").calls > 0, "estimate includes the existing queue");
  const calls = [];
  for (let i = 0; i < 2; i++) {
    const fake = fakeClients();
    const result = await f.run(fake, { verifyBudget: 1 });
    assert.equal(result.report.episodes.read, 0);
    assert.equal(result.report.verification.examined, 1);
    assert.equal(result.report.verification.pending, 1 - i);
    assert.equal(result.status, i ? "done" : "deferred");
    assert.equal(fake.calls.prompts.filter((p) => p.prompt.startsWith(P.MAP_PROMPT)).length, 0);
    calls.push(...verifiedCalls(fake).map((p) => p.prompt.match(/"statement": "([^"]+)"/)[1]));
  }
  assert.equal(new Set(calls).size, 2, "no candidate is verified twice");
  const proposals = read(f.state, "proposals.json");
  assert.equal(proposals.length, 2);
  assert.equal(new Set(proposals.map((p) => p.cluster)).size, 2);
  const final = fakeClients();
  assert.equal((await f.run(final)).status, "nothing-new");
  assert.equal(final.calls.generate + final.calls.evaluate, 0);
});

test("saved tier C estimates remain eligible, while approved and dismissed cluster decisions suppress verification", async () => {
  const f = fixture();
  await f.run(fakeClients(), { verifyBudget: 0 });
  const clusters = read(f.state, "pool/clusters.json");
  const beta = Object.values(clusters).find((c) => /Topic beta/.test(c.statement));
  beta.tier = "C";
  beta.rank = { checkable: 0.01, worth: 0.99 };
  writeFileSync(join(f.state, "pool/clusters.json"), JSON.stringify(clusters));
  const fake = fakeClients();
  const result = await f.run(fake);
  assert.equal(result.status, "done");
  assert.equal(result.report.episodes.read, 0);
  assert.ok(verifiedCalls(fake).some((p) => p.prompt.includes(beta.statement)));
  assert.equal(read(f.state, "pool/clusters.json")[beta.id].rank.checkable, 0.01, "keep original score");
  assert.equal(read(f.state, "proposals.json").find((p) => p.cluster === beta.id).tier, "B");

  for (const action of ["approved", "dismissed"]) {
    const settled = fixture();
    await settled.run(fakeClients(), { verifyBudget: 0 });
    const pending = Object.values(read(settled.state, "pool/clusters.json")).filter((c) => !c.verification);
    writeFileSync(join(settled.state, "reviewed.json"), JSON.stringify(Object.fromEntries(pending.map((c) => [c.id, { action }]))));
    const suppressed = fakeClients();
    assert.equal((await settled.run(suppressed)).status, "nothing-new");
    assert.equal(verifiedCalls(suppressed).length, 0);
  }
});

test("spend-deferred evidence checks report pending work and resume the same frozen plan", async () => {
  const f = fixture();
  await f.run(fakeClients(), { verifyBudget: 0 });
  const stopped = await f.run(fakeClients(), { budget: 0.001 });
  assert.equal(stopped.status, "stopped");
  assert.equal(stopped.reason, "budget");
  assert.equal(stopped.report.verification.pending, 2);
  assert.deepEqual(stopped.report.verification.deferred.reasons, { budget: 2 });
  const resumed = await f.run(fakeClients());
  assert.equal(resumed.run, stopped.run);
  assert.equal(resumed.status, "done");
  assert.equal(resumed.report.verification.examined, 2);
  assert.equal(resumed.report.verification.pending, 0);
});
