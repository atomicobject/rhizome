// The benchmark's bridge to the compounding starter: its stage functions,
// clients, and caller, so a benchmark measures what ships. Model calls go
// through `rzm agent generate` (RZM, default ./bin/darwin/rzm) and are cached
// by content under each run's state directory.

import { existsSync } from "node:fs";
import { join, resolve } from "node:path";

const SCRIPTS = new URL("../../pkg/app/cli/init/templates/starters/compounding/agents/skills/compounding/scripts/", import.meta.url);
const load = (path) => import(new URL(path, SCRIPTS));

export const calls = await load("lib/calls.mjs");
export const extract = await load("lib/extract.mjs");
export const reduce = await load("lib/reduce.mjs");
export const screen = await load("lib/screen.mjs");
export const verify = await load("lib/verify.mjs");
export const prompts = await load("lib/prompts.mjs");
const { openStore } = await load("lib/store.mjs");

process.env.RZM_SKIP_REPO_DELEGATE ??= "1";
export const RZM = resolve(process.env.RZM ?? "./bin/darwin/rzm");
export const CLIENTS = calls.rzmClients(RZM);
export const JEV = { provider: "typesafe", model: "jev-1.13.0", effort: "" };

// Bench models are full ids, so the reported model matches the binding.
export const bindingOf = (model, effort) => ({ provider: model.startsWith("gpt-") ? "codex" : "claude", model, requested: model, effort: effort ?? "" });

// A caller whose cache and ledger live in `dir`. A new run directory is a
// cold run; rerunning the same directory resumes it for free.
export function benchCaller(dir, bindings) {
  const spend = { usd: 0, byRole: {} };
  return { spend, ...calls.makeCaller({ clients: CLIENTS, store: openStore(join(dir, "state")), run: "bench", bindings: { decide: JEV, ...bindings }, spend }) };
}

// Search for verification evidence runs in the checkout, with its own binary
// when it has one.
export function searchIn(checkout) {
  const own = join(checkout, "bin/darwin/rzm");
  const client = calls.rzmClients(existsSync(own) ? own : RZM, { cwd: checkout });
  return (args) => client.search(args);
}

// The older call shape bench-only commands use: {ok, data, usage, cost, ms, error}.
// Cached in the shared eval state, since matcher and proposer calls repeat.
export async function generate({ prompt, schema, model, effort, stage = "bench" }) {
  const caller = benchCaller(".rhizome/session-memory/eval", { match: bindingOf(model, effort) });
  const tally = calls.newTally();
  try {
    const data = await caller.generate({ role: "match", stage, prompt, schema, tally });
    return { ok: true, data, usage: tally.usage, cost: tally.usd, ms: tally.ms };
  } catch (err) {
    if (!(err instanceof calls.Stop)) throw err;
    return { ok: false, data: null, usage: tally.usage, cost: tally.usd, ms: tally.ms, error: err.message };
  }
}
