#!/usr/bin/env node
// H is opened only when this command is explicitly executed after finalists
// freeze. Implementation/tests never need the real held-out files.
import { createHash } from "node:crypto";
import { spawn } from "node:child_process";
import { chmodSync, existsSync, mkdirSync, readFileSync, readdirSync, renameSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const CAMPAIGN = resolve(".rhizome/session-memory/eval/runs/terra61-2026-10-30");
const ROOT = join(CAMPAIGN, "heldout");
const FREEZE = join(CAMPAIGN, "finalists.json");
const EXPECTED_GOLD = 208;
const MODELS = ["gpt-6.1-terra", "gpt-6.1-sol", "gpt-6-luna"];
const SOURCES = ["tools/sessionmemorybench/bench.mjs", "tools/sessionmemorybench/shipped.mjs", "tools/sessionmemorybench/budget.mjs", "bin/darwin/rzm", "pkg/app/cli/init/templates/starters/compounding/agents/skills/compounding/scripts/lib/extract.mjs", "pkg/app/cli/init/templates/starters/compounding/agents/skills/compounding/scripts/lib/calls.mjs", "pkg/app/cli/init/templates/starters/compounding/agents/skills/compounding/scripts/lib/prompts.mjs"];
const hash = (v) => createHash("sha256").update(typeof v === "string" || Buffer.isBuffer(v) ? v : JSON.stringify(v)).digest("hex");
const read = (path) => JSON.parse(readFileSync(path, "utf8"));
const lines = (text) => text.trim().split("\n").filter(Boolean).map(JSON.parse);
const write = (path, text) => { mkdirSync(dirname(path), { recursive: true, mode: 0o700 }); chmodSync(dirname(path), 0o700); writeFileSync(path + ".tmp", text, { mode: 0o600 }); chmodSync(path + ".tmp", 0o600); renameSync(path + ".tmp", path); };
const save = (path, value) => write(path, JSON.stringify(value, null, 2) + "\n");

export function validateFreeze(freeze) {
  if (freeze.status !== "frozen" || !Array.isArray(freeze.providers) || freeze.providers.length !== 2 || !["codex", "claude"].every((p) => freeze.providers.includes(p))) throw new Error("finalists need frozen status and approved codex/claude providers");
  if (!Array.isArray(freeze.extract) || !freeze.extract.length) throw new Error("freeze needs extraction bindings");
  const seen = new Set();
  for (const arm of freeze.extract) {
    if (!/^[a-z0-9]+(?:-[a-z0-9]+)*$/.test(arm.name ?? "") || seen.has(arm.name) || !MODELS.includes(arm.model) || !["low", "medium", "high", "xhigh", "max"].includes(arm.effort)) throw new Error("invalid or duplicate frozen extraction binding");
    seen.add(arm.name);
  }
}

export function validateGold(full, gated, labels, statements) {
  const ids = new Set(full.map((e) => e.id)), gatedIds = new Set(gated.map((e) => e.id));
  if (!ids.size || !gatedIds.size || ids.size !== full.length || gatedIds.size !== gated.length || gatedIds.size >= ids.size || gated.some((e) => !ids.has(e.id))) throw new Error("full H must be a complete superset of gated H with unique IDs");
  const selected = labels.learnings.filter((l) => ["strong", "proposed", "accepted"].includes(l.level) && l.evidence?.some((id) => ids.has(id)));
  if (selected.length !== EXPECTED_GOLD || new Set(selected.map((l) => l.id)).size !== selected.length) throw new Error("full H strong-learning gold must contain exactly 208 distinct labels");
  for (const label of selected) if (!/^[A-Za-z0-9_-]+$/.test(label.id) || typeof statements.get(label.id) !== "string" || !statements.get(label.id).trim()) throw new Error("a held-out gold statement is missing");
  return selected;
}

function authorization() {
  const plan = read(join(CAMPAIGN, "plan.json")), freeze = read(FREEZE);
  if (plan.status !== "approved" || plan.payloadAuthorization !== "approved" || plan.capUsd !== 75) throw new Error("campaign budget and payload/provider approval required");
  validateFreeze(freeze); return freeze;
}
function stage(options, freeze) {
  const manifestPath = join(ROOT, "manifest.json");
  if (existsSync(manifestPath)) return manifest(freeze);
  for (const key of ["episodes", "gated", "labels"]) if (!options[key] || !options[key].endsWith(key === "labels" ? ".json" : ".jsonl")) throw new Error("prepare requires explicit --episodes FULL_H --gated GATED_H --labels H_LABELS paths");
  // These are the first reads of held-out data, after the frozen bindings and
  // existing provider approval have been checked.
  const fullText = readFileSync(options.episodes, "utf8"), gatedText = readFileSync(options.gated, "utf8"), labelsText = readFileSync(options.labels, "utf8");
  const full = lines(fullText), gated = lines(gatedText), labels = JSON.parse(labelsText);
  const goldRun = resolve(options["gold-run"] ?? labels.run), needed = new Set(labels.learnings.filter((l) => ["strong", "proposed", "accepted"].includes(l.level)).map((l) => l.id));
  const candidatesPath = join(goldRun, "candidates.jsonl"), clustersPath = join(goldRun, "clusters.json"), statements = new Map(), provenance = {};
  const candidatesText = readFileSync(candidatesPath, "utf8"), candidates = lines(candidatesText).filter((c) => needed.has(c.id));
  provenance.candidates = { path: candidatesPath, sha256: hash(candidatesText) };
  for (const candidate of candidates) statements.set(candidate.id, candidate.statement);
  let clusters = [];
  if (existsSync(clustersPath)) {
    const text = readFileSync(clustersPath, "utf8"); clusters = JSON.parse(text).clusters.filter((c) => needed.has(c.id)); provenance.clusters = { path: clustersPath, sha256: hash(text) };
    for (const cluster of clusters) statements.set(cluster.id, cluster.statement);
  }
  const verifications = [];
  for (const id of needed) {
    if (!/^[A-Za-z0-9_-]+$/.test(id)) throw new Error("invalid held-out gold ID");
    const path = join(goldRun, "verify", id + ".json");
    if (!existsSync(path)) continue;
    const text = readFileSync(path, "utf8"), result = JSON.parse(text);
    if (result.cluster !== id) throw new Error("held-out verification ID mismatch");
    verifications.push({ id, data: result }); provenance[id] = { path, sha256: hash(text) };
    if (result.result?.statement) statements.set(id, result.result.statement);
  }
  const selected = validateGold(full, gated, labels, statements), selectedIds = new Set(selected.map((l) => l.id)), stagedGold = join(ROOT, "gold");
  write(join(ROOT, "full.jsonl"), fullText); write(join(ROOT, "gated.jsonl"), gatedText);
  write(join(stagedGold, "candidates.jsonl"), candidates.filter((c) => selectedIds.has(c.id)).map((c) => JSON.stringify(c)).join("\n") + "\n");
  if (clusters.length) save(join(stagedGold, "clusters.json"), { clusters: clusters.filter((c) => selectedIds.has(c.id)) });
  for (const verification of verifications.filter((v) => selectedIds.has(v.id))) save(join(stagedGold, "verify", verification.id + ".json"), verification.data);
  save(join(ROOT, "labels.json"), { ...labels, run: stagedGold, learnings: selected });
  const stageFiles = [join(ROOT, "full.jsonl"), join(ROOT, "gated.jsonl"), join(ROOT, "labels.json"), join(stagedGold, "candidates.jsonl"), ...(clusters.length ? [join(stagedGold, "clusters.json")] : []), ...verifications.filter((v) => selectedIds.has(v.id)).map((v) => join(stagedGold, "verify", v.id + ".json"))];
  const data = { version: 1, freezeSha256: hash(freeze), expectedGold: EXPECTED_GOLD, fullEpisodes: full.length, gatedEpisodes: gated.length, stagedGoldStatements: selected.length,
    source: { full: { path: resolve(options.episodes), sha256: hash(fullText) }, gated: { path: resolve(options.gated), sha256: hash(gatedText) }, labels: { path: resolve(options.labels), sha256: hash(labelsText) }, gold: provenance },
    stageSources: Object.fromEntries(SOURCES.map((p) => [p, hash(readFileSync(p))])), files: Object.fromEntries(stageFiles.map((p) => [p, hash(readFileSync(p))])), at: new Date().toISOString() };
  save(manifestPath, data); return data;
}
function manifest(freeze) {
  const data = read(join(ROOT, "manifest.json"));
  if (data.freezeSha256 !== hash(freeze) || data.expectedGold !== EXPECTED_GOLD) throw new Error("held-out freeze changed after staging");
  for (const [path, expected] of Object.entries({ ...data.stageSources, ...data.files })) if (hash(readFileSync(path)) !== expected) throw new Error("held-out staged data or benchmark source changed");
  return data;
}
function paidEnvironment() {
  if (resolve(process.env.SM_BENCH_SPEND_FILE ?? "") !== join(CAMPAIGN, "spend.json") || Number(process.env.SM_BENCH_BUDGET_USD) !== 75) throw new Error("exact shared campaign budget environment required");
  return { ...process.env, SM_BENCH_MATCH_DIR: join(ROOT, "matcher"), RZM_SKIP_REPO_DELEGATE: "1" };
}
function validateMap(dir) {
  const paths = readdirSync(join(dir, "map")).filter((p) => /^batch-\d+\.json$/.test(p));
  if (!paths.length || paths.some((p) => !read(join(dir, "map", p)).ok)) throw new Error("held-out extraction has missing or failed batches");
}
async function command(name, args, env, validate = () => {}) {
  const marker = join(ROOT, name + ".done.json");
  if (existsSync(marker)) { validate(); return; }
  const start = Date.now(); let stderr = "";
  const code = await new Promise((done, fail) => {
    const child = spawn(process.execPath, ["tools/sessionmemorybench/bench.mjs", ...args], { env, stdio: ["ignore", "ignore", "pipe"] });
    child.stderr.on("data", (chunk) => { stderr += chunk; }); child.on("error", fail); child.on("close", done);
  });
  write(join(ROOT, name + ".log"), stderr);
  if (code !== 0) throw new Error("held-out stage failed; inspect its local log before resuming");
  validate();
  save(marker, { args, ms: Date.now() - start, at: new Date().toISOString() });
  console.log(JSON.stringify({ stage: name, status: "complete", wallMs: Date.now() - start }));
}
async function main(phase, options) {
  const freeze = authorization();
  if (phase === "prepare" || phase === "run") stage(options, freeze); else manifest(freeze);
  if (phase === "prepare") { const data = manifest(freeze); console.log(JSON.stringify({ status: "prepared", fullEpisodes: data.fullEpisodes, gatedEpisodes: data.gatedEpisodes, gold: data.expectedGold, arms: freeze.extract.length })); return; }
  if (!["run", "map", "recall"].includes(phase)) throw new Error("usage: heldout.mjs prepare|run --episodes FULL_H --gated GATED_H --labels H_LABELS [--gold-run RUN]; map|recall use the staged manifest");
  const env = paidEnvironment();
  for (const arm of freeze.extract) {
    const mapDir = join(ROOT, "map-" + arm.name);
    if (phase === "map" || phase === "run") await command("map-" + arm.name, ["map", "--episodes", join(ROOT, "gated.jsonl"), "--out", mapDir, "--model", arm.model, "--effort", arm.effort, "--concurrency", "5"], env, () => validateMap(mapDir));
    if (phase === "recall" || phase === "run") {
      if (!existsSync(join(ROOT, "map-" + arm.name + ".done.json"))) throw new Error("held-out extraction must finish before scoring");
      const out = join(ROOT, "recall-" + arm.name + ".json");
      await command("recall-" + arm.name, ["recall", "--episodes", join(ROOT, "full.jsonl"), "--gold-labels", join(ROOT, "labels.json"), "--level", "strong", "--runs", mapDir, "--out", out, "--model", "claude-sonnet-5-5", "--concurrency", "5"], env);
      const score = read(out), row = score.runs?.["map-" + arm.name], sources = Object.values(row?.bySource ?? {});
      if (row?.failedBatches || score.gold !== EXPECTED_GOLD || sources.reduce((n, s) => n + s.gold, 0) !== EXPECTED_GOLD || !row?.matcherOk) throw new Error("held-out recall denominator or matcher is invalid");
      console.log(JSON.stringify({ arm: arm.name, gold: EXPECTED_GOLD, found: sources.reduce((n, s) => n + s.found, 0), missed: row.missed.length, matcherCostExact: row.matcherCostExact ?? null }));
    }
  }
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const phase = process.argv[2], options = {};
  for (let i = 3; i < process.argv.length; i += 2) { if (!process.argv[i].startsWith("--") || process.argv[i + 1] == null) throw new Error("flags need values"); options[process.argv[i].slice(2)] = process.argv[i + 1]; }
  main(phase, options).catch(() => { console.error("held-out runner stopped; inspect frozen configuration and local stage logs"); process.exitCode = 1; });
}
