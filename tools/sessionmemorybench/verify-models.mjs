#!/usr/bin/env node
// Matched packet-verifier comparison. Labels select/score cases but never
// enter a verifier prompt. File evidence stays at the original frozen commit.
import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import { existsSync, mkdirSync, readFileSync, readdirSync, writeFileSync } from "node:fs";
import { join, resolve } from "node:path";

const root = resolve(".rhizome/session-memory/eval/runs/terra61-2026-10-30");
const evalRoot = resolve(".rhizome/session-memory/eval");
const read = (file) => JSON.parse(readFileSync(file, "utf8"));
const lines = (file) => readFileSync(file, "utf8").split("\n").filter(Boolean).map(JSON.parse);
const hash = (s) => createHash("sha256").update(s).digest("hex");
const write = (file, data) => writeFileSync(file, JSON.stringify(data, null, 2) + "\n", { mode: 0o600 });
const base = join(evalRoot, "runs/D-v1-jev35");
const evidence = "/private/tmp/sm-terra61-evidence-20261030";
const searchRoot = "/private/tmp/sm-terra61-search-20261030";
const commit = "184889cbeb2020b8887364602e4139ae6f062233";
const phase = process.argv[2];
const model = process.argv[3];
const effort = process.argv[4];
const repeat = process.argv[5] ?? "1";
const plan = read(join(root, "plan.json"));
if (plan.status !== "approved" || plan.payloadAuthorization !== "approved") throw new Error("campaign approval missing");
process.env.RZM = resolve("bin/darwin/rzm");
process.env.RZM_SKIP_REPO_DELEGATE = "1";
process.env.RZM_RUNTIME_AUTOSTART = "0";
process.env.SM_BENCH_SPEND_FILE = join(root, "spend.json");
process.env.SM_BENCH_BUDGET_USD = "75";
const { calls, verify, benchCaller, bindingOf, searchIn } = await import("./shipped.mjs");
const { clusters } = read(join(base, "clusters.json"));
const old = readdirSync(join(base, "verify")).filter((f) => f.endsWith(".json")).map((f) => read(join(base, "verify", f)));
const byId = new Map(clusters.map((c) => [c.id, c]));
const positives = new Set(read(join(base, "proposals.json")).filter((p) => read(join(base, "accepted-both.json")).includes(p.cluster)).flatMap((p) => [p.cluster, ...(p.mergedFrom ?? [])]));
const order = (xs) => [...xs].sort((a, b) => hash("terra61-pilot:" + a.cluster).localeCompare(hash("terra61-pilot:" + b.cluster)));
const pilot = order(old.filter((v) => positives.has(v.cluster))).slice(0, 12);
const negatives = order(old.filter((v) => !positives.has(v.cluster) && v.result?.decision === "reject"));
const reasons = new Set();
for (const v of negatives) if (!reasons.has(v.result.rejectReason)) { pilot.push(v); reasons.add(v.result.rejectReason); }
for (const v of negatives) if (pilot.length < 24 && !pilot.includes(v)) pilot.push(v);
const ids = phase === "pilot" ? pilot.map((v) => v.cluster) : old.map((v) => v.cluster).sort();
if (!["pilot", "full"].includes(phase) || !model || !effort) throw new Error("usage: verify-models.mjs pilot|full MODEL EFFORT [REPEAT]");
if (new Set(ids).size !== ids.length || ids.length !== (phase === "pilot" ? 24 : 94) || ids.some((id) => !byId.has(id))) throw new Error("verification case fixture is incomplete or duplicated");
// Searches run in their own checkout, so it must stay at the same commit.
for (const checkout of [evidence, searchRoot]) {
  if (execFileSync("git", ["-C", checkout, "rev-parse", "HEAD"], { encoding: "utf8" }).trim() !== commit || execFileSync("git", ["-C", checkout, "status", "--porcelain", "--untracked-files=no"], { encoding: "utf8" }).trim()) throw new Error(`historical checkout ${checkout} drifted`);
}
const dir = join(root, `verify-${phase}-${model}-${effort}-${repeat}`);
mkdirSync(dir, { recursive: true, mode: 0o700 });
mkdirSync(join(root, "search-cache"), { recursive: true, mode: 0o700 });
const provenance = { model, effort, repeat, ids, commit, sourceHash: hash(readFileSync(join(base, "clusters.json"))), candidateHash: hash(readFileSync(join(base, "candidates.jsonl"))), episodeHash: hash(readFileSync(join(evalRoot, "D/episodes-v1-jev35.jsonl"))), verifierHash: hash(readFileSync("pkg/app/cli/init/templates/starters/compounding/agents/skills/compounding/scripts/lib/verify.mjs")), binaryHash: hash(readFileSync(process.env.RZM)), promptsHash: hash(readFileSync("pkg/app/cli/init/templates/starters/compounding/agents/skills/compounding/scripts/lib/prompts.mjs")), search: "frozen keyword/code index at same commit; embeddings disabled; --fast" };
if (existsSync(join(dir, "config.json")) && JSON.stringify(read(join(dir, "config.json"))) !== JSON.stringify(provenance)) throw new Error("verifier run provenance drift");
write(join(dir, "config.json"), provenance);
if (process.argv.includes("--dry-run")) { console.log(JSON.stringify({ cases: ids.length, historicalGoldInSample: ids.filter((id) => positives.has(id)).length, provenance })); process.exit(0); }
const candidates = Object.fromEntries(lines(join(base, "candidates.jsonl")).map((c) => [c.id, c]));
const episodes = Object.fromEntries(lines(join(evalRoot, "D/episodes-v1-jev35.jsonl")).map((e) => [e.id, e]));
const search = searchIn(searchRoot);
let searchFailures = 0;
const ctx = { root: evidence, search: async (args) => {
  const invocation = ["--fast", ...args];
  const file = join(root, "search-cache", hash(JSON.stringify({ invocation, commit, binary: provenance.binaryHash })) + ".json");
  const text = existsSync(file) ? read(file).text : await search(invocation);
  if (text.startsWith("(failed:")) searchFailures++;
  write(file, { args: invocation, text });
  return text;
} };
const caller = benchCaller(dir, { verify: bindingOf(model, effort) });
const started = Date.now();
let done = 0;
await calls.pool(ids, 5, async (id) => {
  const file = join(dir, id + ".json");
  if (existsSync(file)) { done++; return; }
  const cl = byId.get(id);
  const variants = cl.members.map((m) => candidates[m]).map(({ kind, statement, why, appliesTo, commands, target }) => ({ kind, statement, why, appliesTo, commands, target }));
  const families = new Map();
  for (const e of cl.evidence.map((e) => episodes[e]).filter(Boolean)) if (!families.has(e.family)) families.set(e.family, e);
  const sources = [...families.values()].slice(0, 6).map((e) => ({ lens: e.lens, origin: e.origin, confidence: e.confidence, previous: e.context?.previous?.slice(0, 600) || undefined, text: e.text.slice(0, 1500) }));
  const tally = calls.newTally();
  const out = await verify.verifyCluster(caller, ctx, { cl, variants, sources, tally });
  write(file, { cluster: id, ...out, cost: tally.usd, ms: tally.ms, usage: tally.usage });
  process.stderr.write(`verify ${model} ${effort} ${++done}/${ids.length} ${id} ${out.result.decision} contract=${out.contractProblems.length}\n`);
});
const results = ids.map((id) => read(join(dir, id + ".json")));
const proposed = results.filter((v) => v.result.decision === "propose" && !v.contractProblems.length);
const ledger = lines(join(dir, "state/ledger.jsonl"));
const summary = { ...provenance, cases: ids.length, proposed: proposed.length, invalid: results.filter((v) => v.contractProblems.length).length, recoveredHistorical: proposed.filter((v) => positives.has(v.cluster)).length, historicalGoldInSample: ids.filter((id) => positives.has(id)).length, historicalBinaryAgreement: results.filter((v) => (old.find((o) => o.cluster === v.cluster).result.decision === "propose") === proposed.includes(v)).length, searchFailures, usd: ledger.reduce((s, v) => s + (v.usd ?? 0), 0), modelMs: ledger.reduce((s, v) => s + (v.ms ?? 0), 0), wallMs: Date.now() - started };
write(join(dir, "summary.json"), summary);
console.log(JSON.stringify(summary));
