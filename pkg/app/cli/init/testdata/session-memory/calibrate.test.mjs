import assert from "node:assert/strict";
import { execFileSync, spawnSync } from "node:child_process";
import { mkdirSync, readFileSync, writeFileSync, existsSync, symlinkSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { afterEach, beforeEach, mock, test } from "node:test";
import { fakeClients, tempRepo } from "./fakes.mjs";

const scripts = new URL("../../templates/starters/session-memory/agents/skills/session-memory/scripts/", import.meta.url);
const { calibrate, applyCalibration } = await import(new URL("calibrate.mjs", scripts));
const { renderShard, evidenceLines } = await import(new URL("lib/shards.mjs", scripts));
const { writeJson } = await import(new URL("lib/store.mjs", scripts));
const { compareMeasurements } = await import(new URL("lib/calibration-score.mjs", scripts));
const { MAP_PROMPT, PACKET_PREFIX } = await import(new URL("lib/prompts.mjs", scripts));
const fixture = JSON.parse(readFileSync(new URL("../references/calibration-fixture.json", scripts), "utf8"));
const BASE = "claude-haiku-5-5";
const ALT = "claude-sonnet-5-5";
beforeEach(() => { let time = 0; mock.method(Date, "now", () => time += 1000); });
afterEach(() => mock.restoreAll());

test("the installed command also runs through a symlinked path", () => {
  const root = repo();
  const command = join(root, "calibrate.mjs");
  symlinkSync(fileURLToPath(new URL("calibrate.mjs", scripts)), command, "file");
  const result = spawnSync(process.execPath, [command, "--role", "invalid"], { cwd: root, encoding: "utf8" });
  assert.equal(result.status, 2);
  assert.match(result.stderr, /--role must be/);
});

function repo(local = false) {
  const root = tempRepo();
  writeFileSync(join(root, ".gitignore"), ".rhizome/session-memory/\n");
  if (local) {
    const episodes = {};
    const clusters = {};
    mkdirSync(join(root, "docs/memory/context-shards"), { recursive: true });
    for (let i = 0; i < 10; i++) {
      const id = `local-${i}`;
      const statement = `Learning ${i}: worker ${i} keeps its queue until acknowledgement.`;
      episodes[id] = { id, harness: "claude", session: id, family: id, lens: "research", origin: "person", text: statement };
      const s = { kind: "fact", statement, appliesTo: [], reviewedBy: "Synthetic Reviewer", reviewedOn: "2026-10-20", verifiedOn: "2026-10-20", evidenceLines: evidenceLines([{ harness: "claude", session: id, episode: id, ts: "2026-10-20", lens: "research" }]) };
      writeFileSync(join(root, `docs/memory/context-shards/fact-${i}.md`), renderShard(s));
      clusters[id] = { ...s, id, evidence: [id], members: [] };
    }
    writeJson(join(root, ".rhizome/session-memory/pool/episodes.json"), episodes);
    writeJson(join(root, ".rhizome/session-memory/pool/clusters.json"), clusters);
  }
  return root;
}

function client({ counts = [1, 1, 1], costs = [0.1, 0.1], extra = false, verify = "correct" } = {}) {
  const f = fakeClients();
  const calls = [];
  let baseline = 0;
  f.harnessStatus = async () => [{ kind: "claude", installed: true, loggedIn: true, version: "synthetic", models: [BASE, ALT, "claude-opus-5-5"].map((id) => ({ id })) }];
  f.generate = async ({ provider, model, effort, prompt }) => {
    calls.push({ model, effort, prompt });
    let output;
    if (prompt.startsWith(MAP_PROMPT)) {
      const eps = prompt.match(/<episodes>\n([\s\S]*?)\n<\/episodes>/)[1].split("\n").map(JSON.parse);
      const count = model === BASE ? counts[Math.min(baseline++, 1)] : counts[2];
      const candidates = eps.slice(0, count).map((e) => ({ kind: "fact", statement: fixture.labels.find((l) => l.evidence.includes(e.id))?.statement ?? e.text, why: "Synthetic evidence", appliesTo: [], commands: [], evidence: [e.id], strength: "strong" }));
      if (extra) candidates.push({ kind: "fact", statement: "An additional learning without a human label.", why: "Synthetic", appliesTo: [], commands: [], evidence: [eps[0].id], strength: "strong" });
      output = { candidates };
    } else if (prompt.startsWith("Match each candidate")) {
      const items = JSON.parse(prompt.split("\n\n")[1]);
      output = { matches: items.map((i) => ({ candidate: i.candidate, labels: i.labels.filter((l) => l.statement === i.statement).map((l) => l.id) })) };
    } else if (prompt.startsWith(PACKET_PREFIX)) {
      const c = JSON.parse(prompt.match(/<candidate>\n([\s\S]*?)\n<\/candidate>/)[1]);
      const decision = verify === "wrong" ? "reject" : fixture.labels.find((l) => l.statement === c.statement)?.decision ?? "propose";
      output = { ...c, decision, why: "Synthetic", documentation: "", notes: "", verification: [], ...(decision === "propose" ? { response: "remember", responseReason: "Synthetic" } : {}) };
    } else output = { ok: true };
    return { provider, model, output, usage: { inputTokens: 100, outputTokens: 10, costUsd: model === BASE ? costs[0] : costs[1] } };
  };
  return { ...f, recorded: calls };
}

const run = (root, fake, opts = {}) => calibrate({ root, role: "extract", harness: "claude", budget: 10, candidates: [`claude:${ALT}:medium`], log: () => {}, ...opts }, fake);

test("calibrate estimates the shipped fixture before any model call without a budget", async () => {
  const root = repo();
  const f = client();
  const r = await run(root, f, { budget: undefined });
  assert.equal(r.status, "estimate");
  assert.equal(f.recorded.length, 0);
  assert.equal(r.set.name, fixture.name);
  assert.equal(r.set.labelCount, 3);
  assert.ok(r.estimate.usd > 0);
  assert.equal(existsSync(join(root, ".rhizome/session-memory/models.json")), false);
});

test("calibrate stops at its budget and cannot propose a partial comparison", async () => {
  const root = repo();
  const f = client();
  const r = await run(root, f, { budget: 0.000001 });
  assert.equal(r.status, "stopped");
  assert.equal(r.reason, "budget");
  assert.equal(r.diff, null);
  assert.equal(f.recorded.length, 0);
});

test("the uncached current-model rerun determines the quality noise floor", async () => {
  const r = await run(repo(true), client({ counts: [2, 4, 3] }));
  assert.equal(r.set.synthetic, false);
  assert.equal(r.samples[0].quality.matched, 2);
  assert.equal(r.samples[1].quality.matched, 4);
  assert.equal(r.samples[1].cached, 0);
  assert.equal(r.samples[1].bypassCache, true);
  assert.equal(r.noiseFloor, 0.2);
  assert.equal(r.diff, null);
});

test("a quality improvement beyond noise proposes a diff on local labels", async () => {
  const root = repo(true);
  const r = await run(root, client({ counts: [1, 2, 5] }));
  assert.equal(r.diff.reason, "quality");
  assert.equal(r.diff.after[0].model, ALT);
  assert.equal(r.diff.evidence, r.evidenceFile);
  assert.equal(JSON.parse(readFileSync(join(root, r.evidenceFile))).diff.reason, "quality");
  assert.equal(existsSync(join(root, ".rhizome/session-memory/models.json")), false);
});

test("equal quality for at least 25 percent less cost proposes a fixture-safe diff", async () => {
  const r = await run(repo(), client({ costs: [0.1, 0.075] }));
  assert.equal(r.diff.reason, "cost");
  assert.equal(r.samples[2].quality.matched, r.samples[0].quality.matched);
  assert.equal(r.samples[2].inputTokens, 100);
  assert.equal(r.samples[2].outputTokens, 10);
  assert.equal(typeof r.samples[2].medianSeconds, "number");
});

test("equal quality without 25 percent savings proposes nothing", async () => {
  const r = await run(repo(), client({ costs: [0.1, 0.08] }));
  assert.equal(r.diff, null);
});

test("quality improvements on the shipped fixture cannot produce a quality diff", async () => {
  const r = await run(repo(), client({ counts: [1, 1, 2] }));
  assert.equal(r.diff, null);
  assert.match(r.refusals[0].reason, /fixture quality/);
  assert.match(r.set.limitation, /quality-driven diffs are refused/);
});

test("unmatched extracted candidates are unlabeled, not additional misses", async () => {
  const r = await run(repo(), client({ extra: true }));
  const q = r.samples[0].quality;
  assert.equal(q.unlabeled, 1);
  assert.equal(q.matched, 1);
  assert.equal(q.missed.length, 1);
  assert.equal(q.total, 2);
});

test("apply requires confirmation and writes only the local override", async () => {
  const root = repo();
  const shippedFile = new URL("../models.json", scripts);
  const original = readFileSync(shippedFile, "utf8");
  const r = await run(root, client({ costs: [0.1, 0.05] }));
  assert.throws(() => applyCalibration({ root, evidenceFile: r.evidenceFile }), /--confirm/);
  assert.equal(existsSync(join(root, ".rhizome/session-memory/models.json")), false);
  const a = applyCalibration({ root, evidenceFile: r.evidenceFile, confirm: true });
  assert.equal(a.path, ".rhizome/session-memory/models.json");
  assert.equal(JSON.parse(readFileSync(join(root, a.path))).roles.extract[0].model, ALT);
  assert.equal(readFileSync(shippedFile, "utf8"), original);
  assert.throws(() => applyCalibration({ root, evidenceFile: r.evidenceFile, confirm: true }), /profile changed/);
});

test("apply rechecks fixture restrictions and refuses fabricated quality diffs", async () => {
  const root = repo();
  const r = await run(root, client({ counts: [1, 1, 2] }));
  r.diff = { role: "extract", reason: "quality", after: [{ model: ALT }] };
  writeJson(join(root, r.evidenceFile), r);
  assert.throws(() => applyCalibration({ root, evidenceFile: r.evidenceFile, confirm: true }), /no eligible/);
});

test("verify measures recorded decisions using the synthetic repository", async () => {
  const f = client();
  const r = await run(repo(), f, { role: "verify", candidates: ["claude:claude-opus-5-5:medium"] });
  assert.equal(r.samples[0].quality.matched, 3);
  assert.equal(r.samples[0].quality.total, 3);
  assert.ok(f.recorded.some((c) => c.prompt.includes("journal.delete(id)")));
  assert.ok(f.recorded.every((c) => !c.prompt.includes("<episodes>")));
});

test("provider selection honors explicit harness then configured harness and blocks cross-provider candidates", async () => {
  const root = repo();
  const f = client();
  f.configuredHarness = async () => "codex";
  const r = await run(root, f, { budget: undefined });
  assert.equal(r.bindings.extract.provider, "claude");
  await assert.rejects(run(root, f, { candidates: ["codex:gpt-6.1-sol:low"] }), /cannot cross providers/);
  await assert.rejects(run(root, f, { harness: undefined, budget: undefined, candidates: [] }), /no model for extract/);
  assert.equal(f.recorded.length, 0);
});

test("missing local transcript or a different source provider falls back to fixture", async () => {
  const root = repo(true);
  const path = join(root, ".rhizome/session-memory/pool/episodes.json");
  const eps = JSON.parse(readFileSync(path));
  eps["local-0"].harness = "codex";
  writeJson(path, eps);
  const r = await run(root, client(), { budget: undefined });
  assert.equal(r.set.synthetic, true);
  assert.equal(r.set.localApproved, 9);
});

test("latency savings require a quarter off both baseline measurements", () => {
  const sample = (score, seconds) => ({ binding: {}, quality: { score }, usd: 1, medianSeconds: seconds });
  assert.equal(compareMeasurements([sample(1, 8), sample(1, 4), sample(1, 3)], true).winner.reason, "latency");
  assert.equal(compareMeasurements([sample(1, 8), sample(1, 4), sample(1, 4)], true).winner, null);
});

test("an actual cost overrun stops the final sample and cannot propose a diff", async () => {
  const f = client({ costs: [0.001, 0.001] });
  const generate = f.generate;
  let calls = 0;
  f.generate = async (request) => {
    const result = await generate(request);
    if (++calls === 9) result.usage.costUsd = 0.5;
    return result;
  };
  const r = await run(repo(), f, { role: "verify", candidates: [`claude:${BASE}:low`], budget: 0.4 });
  assert.equal(calls, 9);
  assert.equal(r.status, "stopped");
  assert.equal(r.reason, "budget");
  assert.equal(r.diff, null);
  assert.ok(r.spend.usd > 0.4);
});

test("a cross-role candidate retains its family in the approved diff", async () => {
  const r = await run(repo(), client({ costs: [0.01, 0.1] }), { role: "verify", candidates: [`claude:${BASE}:low`] });
  assert.equal(r.diff.reason, "cost");
  assert.equal(r.diff.after[0].family, "haiku");
  assert.equal(r.diff.after[0].version, "5.5");
});

test("one extracted candidate may recover multiple matching labels", async () => {
  const root = repo(true);
  const s = { kind: "fact", statement: "Learning 0: worker 0 keeps its queue until acknowledgement.", appliesTo: [], reviewedBy: "Synthetic Reviewer", reviewedOn: "2026-10-20", verifiedOn: "2026-10-20", evidenceLines: evidenceLines([{ harness: "claude", session: "local-0", episode: "local-0", ts: "2026-10-20", lens: "research" }]) };
  writeFileSync(join(root, "docs/memory/context-shards/fact-duplicate.md"), renderShard(s));
  const r = await run(root, client());
  assert.equal(r.samples[0].quality.matched, 2);
  assert.equal(r.samples[0].quality.candidates, 1);
});

test("local verify replays decisions and withholds label files from search, grep, and file requests", async () => {
  const root = repo(true);
  const labelPath = "docs/memory/context-shards/label-only.md";
  writeFileSync(join(root, labelPath), "QueueGuard PRIVATE_LABEL_TEXT\n");
  const state = join(root, ".rhizome/session-memory");
  const eps = JSON.parse(readFileSync(join(state, "pool/episodes.json")));
  eps.rejected = { id: "rejected", harness: "claude", session: "rejected", text: "The QueueGuard issue was a one-off failure." };
  writeJson(join(state, "pool/episodes.json"), eps);
  const clusters = JSON.parse(readFileSync(join(state, "pool/clusters.json")));
  clusters.rejected = { id: "rejected", kind: "fact", statement: "The `QueueGuard` always loses messages.", evidence: ["rejected"], members: [] };
  writeJson(join(state, "pool/clusters.json"), clusters);
  writeJson(join(state, "decisions/rejected.json"), { cluster: "rejected", learning: "rejected", kind: "fact", statement: clusters.rejected.statement, decision: "dismissed", decider: "Synthetic Reviewer", at: "2026-10-20" });
  execFileSync("git", ["-C", root, "add", "docs"]);
  const f = client();
  f.search = async (args) => {
    assert.ok(args.includes("--json"));
    if (args.includes("QueueGuard")) return JSON.stringify({ sources: null });
    return JSON.stringify({ sources: [{ result: { Path: labelPath } }, { result: { Path: "README.md" } }] });
  };
  const original = f.generate;
  f.generate = async (req) => {
    if (req.prompt.startsWith(PACKET_PREFIX)) {
      assert.ok(!req.prompt.includes("PRIVATE_LABEL_TEXT"));
      assert.ok(!req.prompt.includes("reviewed-by:"));
      if (!req.prompt.includes("requested file")) return { model: req.model, output: { decision: "need_evidence", requests: [{ kind: "file", value: labelPath }, { kind: "grep", value: "QueueGuard" }, { kind: "search", value: "QueueGuard" }] }, usage: { costUsd: 0.001 } };
    }
    return original(req);
  };
  const r = await run(root, f, { role: "verify", candidates: [`claude:${BASE}:low`] });
  assert.equal(r.status, "done");
  assert.equal(r.set.labelCount, 11);
  assert.equal(r.samples[0].quality.matched, 10);
});

test("applying a diff preserves other locally overridden roles and providers", async () => {
  const root = repo();
  const local = { version: 1, providers: ["claude", "codex"], roles: { verify: [{ provider: "claude", model: "claude-opus-5-5", family: "opus", version: "5.5", effort: "medium", provisional: true }] } };
  writeJson(join(root, ".rhizome/session-memory/models.json"), local);
  const r = await run(root, client({ costs: [0.1, 0.05] }));
  applyCalibration({ root, evidenceFile: r.evidenceFile, confirm: true });
  const updated = JSON.parse(readFileSync(join(root, ".rhizome/session-memory/models.json")));
  assert.deepEqual(updated.roles.verify, local.roles.verify);
  assert.deepEqual(updated.providers, local.providers);
});
