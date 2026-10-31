#!/usr/bin/env node
// Reproduce the approved October 30 model sweep. Transcript-derived inputs
// and outputs stay in the ignored eval tree. Run only one campaign at a time.
import { spawn } from "node:child_process";
import { createWriteStream, existsSync, mkdirSync, readFileSync, readdirSync, writeFileSync } from "node:fs";
import { join, resolve } from "node:path";

const evalRoot = resolve(".rhizome/session-memory/eval");
const root = join(evalRoot, "runs/terra61-2026-10-30");
const plan = JSON.parse(readFileSync(join(root, "plan.json")));
if (plan.status !== "approved" || plan.capUsd !== 75) throw new Error("campaign requires its recorded $75 approval");
if (plan.payloadAuthorization !== "approved") throw new Error("campaign requires explicit payload/provider approval");
const env = { ...process.env, RZM: resolve("bin/darwin/rzm"), RZM_SKIP_REPO_DELEGATE: "1", SM_BENCH_SPEND_FILE: join(root, "spend.json"), SM_BENCH_BUDGET_USD: "75", SM_BENCH_MATCH_DIR: join(root, "matcher") };
const judgeConcurrency = Number(process.env.SM_BENCH_JUDGE_CONCURRENCY ?? 5);
if (!Number.isInteger(judgeConcurrency) || judgeConcurrency < 1 || judgeConcurrency > 5) throw new Error("judge concurrency must be 1 through 5");
const matrix = [
  ["sol-low-1", "gpt-6.1-sol", "low"],
  ["luna-xhigh-1", "gpt-6-luna", "xhigh"],
  ["sol-low-2", "gpt-6.1-sol", "low"],
  ["luna-xhigh-2", "gpt-6-luna", "xhigh"],
  ...["low", "medium", "high", "xhigh", "max"].map((e) => [`terra-${e}-1`, "gpt-6.1-terra", e]),
  ["sol-medium-1", "gpt-6.1-sol", "medium"],
  ["luna-medium-1", "gpt-6-luna", "medium"],
];
mkdirSync(root, { recursive: true });

async function run(name, args) {
  const marker = join(root, name + ".done.json");
  if (existsSync(marker)) return;
  const start = Date.now();
  const log = createWriteStream(join(root, name + ".log"), { flags: "a", mode: 0o600 });
  process.stderr.write(`START ${name}\n`);
  const code = await new Promise((done, fail) => {
    const child = spawn(process.execPath, ["tools/sessionmemorybench/bench.mjs", ...args], { env, stdio: ["ignore", "pipe", "pipe"] });
    child.stdout.pipe(log, { end: false });
    child.stderr.on("data", (chunk) => { log.write(chunk); process.stderr.write(chunk); });
    child.on("error", fail);
    child.on("close", done);
  });
  log.end();
  if (code !== 0) throw new Error(`${name} failed; inspect its local log before resuming`);
  // map exits 0 even when batches failed, and a marker would let recall score them.
  if (args[0] === "map") {
    const dir = join(args[args.indexOf("--out") + 1], "map");
    const failed = readdirSync(dir).filter((f) => f.endsWith(".json") && !JSON.parse(readFileSync(join(dir, f))).ok);
    if (failed.length) throw new Error(`${name} has ${failed.length} failed batches; rerun it before scoring`);
  }
  writeFileSync(marker, JSON.stringify({ args, ms: Date.now() - start, at: new Date().toISOString() }) + "\n");
  process.stderr.write(`DONE ${name} ${Math.round((Date.now() - start) / 1000)}s\n`);
}

const phase = process.argv[2];
if (phase === "extract-one" || phase === "recall-one") {
  const [model, effort, repeat] = process.argv.slice(3);
  if (!["gpt-6.1-terra", "gpt-6.1-sol", "gpt-6-luna"].includes(model) || !["low", "medium", "high", "xhigh", "max"].includes(effort) || !/^\d+$/.test(repeat ?? "")) throw new Error("single-arm phase requires MODEL EFFORT REPEAT");
  const family = model.split("-").at(-1);
  matrix.splice(0, matrix.length, [`${family}-${effort}-${repeat}`, model, effort]);
}
if (phase === "extract" || phase === "extract-one") {
  for (const [name, model, effort] of matrix) await run("map-" + name, ["map", "--episodes", join(evalRoot, "jevlab/gated/passage30.jsonl"), "--out", join(root, "map-" + name), "--model", model, "--effort", effort, "--concurrency", "5"]);
} else if (phase === "recall" || phase === "recall-one") {
  for (const [name] of matrix) {
    if (!existsSync(join(root, `map-${name}.done.json`))) throw new Error(`map-${name} is incomplete; finish extraction before scoring`);
    await run("recall-" + name, ["recall", "--episodes", join(evalRoot, "D/episodes-v1.jsonl"), "--gold-labels", join(evalRoot, "jevlab/labels-D-v1.json"), "--level", "strong", "--runs", join(root, "map-" + name), "--out", join(root, "recall-" + name + ".json"), "--model", "claude-sonnet-5-5", "--concurrency", String(judgeConcurrency)]);
  }
} else {
  throw new Error("usage: node tools/sessionmemorybench/campaign.mjs extract|recall|extract-one|recall-one MODEL EFFORT REPEAT");
}
