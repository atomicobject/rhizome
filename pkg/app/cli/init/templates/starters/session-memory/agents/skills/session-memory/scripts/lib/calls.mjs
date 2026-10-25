// Model and Jev calls. Every call goes through `rzm agent generate` or
// `rzm agent evaluate-batch`, behind clients that tests replace with fakes.
// Results are cached by content; attempts, including failures, go to the
// ledger, and a failure is never cached.

import { spawn } from "node:child_process";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { PROMPT_VERSION } from "./prompts.mjs";
import { sha } from "./store.mjs";

// Published per-million-token prices (effort 2026-10-12 and 2026-10-18).
// Codex runs may bill a subscription; their cost is the API equivalent.
export const PRICES = {
  "claude-opus-5-5": { input: 4, write5m: 5, write1h: 8, read: 0.2, output: 20 },
  "claude-sonnet-5-5": { input: 2, write5m: 2.5, write1h: 4, read: 0.1, output: 10 },
  "claude-haiku-5-5": { input: 0.1, write5m: 0.125, write1h: 0.2, read: 0.01, output: 0.5 },
  "gpt-6-luna": { input: 0.1, read: 0.01, output: 0.5 },
  "gpt-6.1-sol": { input: 2, read: 0.1, output: 10 },
  "jev-1.13.0": { input: 0.042, read: 0.042, output: 0 },
};

// Claude reports uncached input, cache reads, and cache writes separately;
// Codex input includes its cache reads.
const inputIncludesReads = (model) => Boolean(model?.startsWith("gpt-"));

// Null when no price is known.
export function costOf(model, usage = {}) {
  const p = PRICES[model];
  if (!p) return usage.costUsd || null;
  const read = usage.cachedInputTokens ?? 0;
  const write = usage.cacheWriteInputTokens ?? 0;
  const fresh = inputIncludesReads(model) ? (usage.inputTokens ?? 0) - read : usage.inputTokens ?? 0;
  return (fresh * p.input + write * (p.write5m ?? p.input) + read * p.read + (usage.outputTokens ?? 0) * p.output) / 1e6;
}

// Codex structured output needs strict schemas: every property required and
// no extra properties. Optional fields become nullable, and nulls are dropped
// from the answer so both providers return the same shape.
export function strictSchema(s) {
  if (Array.isArray(s)) return s.map(strictSchema);
  if (!s || typeof s !== "object") return s;
  const out = {};
  for (const [k, v] of Object.entries(s)) if (k !== "minItems") out[k] = strictSchema(v);
  if (out.type === "object" && out.properties) {
    const required = new Set(s.required ?? []);
    for (const [k, v] of Object.entries(out.properties)) {
      if (required.has(k)) continue;
      if (v.enum) v.enum = [...v.enum, null];
      else if (v.type) v.type = [v.type, "null"];
    }
    out.required = Object.keys(out.properties);
    out.additionalProperties = false;
  }
  return out;
}

export const dropNulls = (v) => (Array.isArray(v) ? v.map(dropNulls) : v && typeof v === "object" ? Object.fromEntries(Object.entries(v).filter(([, x]) => x !== null).map(([k, x]) => [k, dropNulls(x)])) : v);

const CALL_TIMEOUT_MS = 60 * 60 * 1000;

// The real clients: one `rzm` subprocess per call.
export function rzmClients(rzm = "rzm", { cwd } = {}) {
  const run = (args, input = "", dir = cwd) =>
    new Promise((done, fail) => {
      const child = spawn(rzm, args, { cwd: dir, stdio: ["pipe", "pipe", "pipe"] });
      let out = "";
      let err = "";
      // A hung CLI would hold the run's lock forever.
      const timer = setTimeout(() => child.kill("SIGKILL"), CALL_TIMEOUT_MS);
      child.stdout.on("data", (d) => (out += d));
      child.stderr.on("data", (d) => (err += d));
      child.on("error", fail);
      child.on("close", (code) => {
        clearTimeout(timer);
        done({ code, out, err: code === null ? `timed out after ${CALL_TIMEOUT_MS / 60000} minutes` : err });
      });
      child.stdin.end(input);
    });
  const parse = (r, what) => {
    if (r.code === 0) return JSON.parse(r.out.trim().split("\n").pop());
    let detail = (r.err || r.out).trim();
    try {
      detail = JSON.parse(detail.split("\n").pop()).error ?? detail;
    } catch {}
    throw new Error(`${what}: ${typeof detail === "string" ? detail.slice(-500) : JSON.stringify(detail).slice(0, 500)}`);
  };
  return {
    async generate({ provider, model, effort, prompt, schema }) {
      const args = ["agent", "generate"];
      for (const [flag, v] of [["--provider", provider], ["--model", model], ["--effort", effort]]) if (v) args.push(flag, v);
      const dir = schema ? mkdtempSync(join(tmpdir(), "sm-schema-")) : null;
      try {
        if (dir) {
          writeFileSync(join(dir, "schema.json"), JSON.stringify(schema), { mode: 0o600 });
          args.push("--schema", join(dir, "schema.json"));
        }
        return parse(await run(args, prompt), "generate");
      } finally {
        if (dir) rmSync(dir, { recursive: true, force: true });
      }
    },
    evaluateBatch: async (request) => parse(await run(["agent", "evaluate-batch"], JSON.stringify(request)), "evaluate-batch"),
    harnessStatus: async () => parse(await run(["harness", "status"]), "harness status"),
    configuredHarness: async () => (parse(await run(["harness", "status", "--configured"]), "configured harness")).harness,
    // Rhizome search for verification evidence; a failure is evidence too.
    async search(args, { cwd: dir } = {}) {
      const r = await run(["search", ...args], "", dir ?? cwd);
      return r.code === 0 ? r.out : `(failed: ${(r.err || r.out).trim().split("\n")[0].slice(0, 200)})`;
    },
  };
}

export class Stop extends Error {
  constructor(reason, message) {
    super(message);
    this.reason = reason;
  }
}

// Cache keys: stage, prompt version, input content, and model binding.
export const callKey = ({ stage, binding: b, prompt, schema }) => sha({ stage, version: PROMPT_VERSION, provider: b.provider, model: b.model, effort: b.effort, prompt, schema });
export const itemKey = ({ stage, model, item }) => sha({ stage, version: PROMPT_VERSION, model, state: item.state, questions: item.questions });

// Rebuilds a run's spend from its ledger, so a resumed run counts what the
// killed one paid for.
export function spendFrom(entries, run) {
  const spend = { usd: 0, byRole: {} };
  for (const e of entries) if (e.run === run && (e.ok || e.usd)) account(spend, e.role, e.usage, e.usd, e.model);
  return spend;
}

const FATAL = /harness (not installed|not logged in|unsupported)/;
const JEV_ITEMS = 128;

function account(spend, role, usage, usd, model) {
  const r = (spend.byRole[role] ??= { calls: 0, cached: 0, inputTokens: 0, outputTokens: 0, usd: 0 });
  r.calls++;
  r.inputTokens += (usage?.inputTokens ?? 0) + (inputIncludesReads(model) ? 0 : usage?.cachedInputTokens ?? 0) + (usage?.cacheWriteInputTokens ?? 0);
  r.outputTokens += usage?.outputTokens ?? 0;
  r.usd += usd ?? 0;
  spend.usd += usd ?? 0;
}

// A caller bound to one run's frozen bindings, budget, and spend record.
// `estimate` is the dollar reservation a call takes against the budget.
export function makeCaller({ clients, store, run, bindings, budget = null, spend, onResult }) {
  let reserved = 0;
  const reserve = (usd) => {
    if (budget !== null && spend.usd + reserved + usd > budget) throw new Stop("budget", `budget $${budget} reached ($${spend.usd.toFixed(4)} spent)`);
    reserved += usd;
  };

  // `effort` overrides the binding's for cheap follow-ups such as repairs.
  async function generate({ role, stage, prompt, schema, estimate = 0, effort, tally, bypassCache = false }) {
    const b = bindings[role];
    if (!b) throw new Stop("binding", `no binding for ${role}`);
    effort ??= b.effort;
    const key = callKey({ stage, binding: { ...b, effort }, prompt, schema });
    const hit = bypassCache ? null : store.cacheGet(key);
    if (hit) {
      (spend.byRole[role] ??= { calls: 0, cached: 0, inputTokens: 0, outputTokens: 0, usd: 0 }).cached++;
      if (tally) tally.cached++;
      onResult?.({ ...hit, role, cached: true });
      return hit.output;
    }
    reserve(estimate);
    try {
      for (let attempt = 1; attempt <= 2; attempt++) {
        let r;
        const started = Date.now();
        try {
          r = await clients.generate({ provider: b.provider, model: b.model, effort, prompt, schema: b.provider === "codex" && schema ? strictSchema(schema) : schema });
        } catch (err) {
          store.ledger({ run, stage, role, key, attempt, ok: false, error: String(err.message).slice(0, 500) });
          if (FATAL.test(err.message)) throw new Stop("binding", `${role} binding ${b.provider}:${b.model} is unavailable: ${err.message}`);
          continue;
        }
        // A frozen model that now resolves to another is unavailable, not a substitute.
        if (r.model && b.model && r.model !== b.model) {
          const paid = Math.max(costOf(r.model, r.usage) ?? 0, r.usage?.costUsd ?? 0);
          store.ledger({ run, stage, role, key, attempt, ok: false, model: r.model, usage: r.usage, usd: paid, error: `model ${r.model} ran instead of ${b.model}` });
          account(spend, role, r.usage, paid, r.model);
          throw new Stop("binding", `${role} is frozen to ${b.model} but ${r.model} ran; resume when ${b.model} is available`);
        }
        // Token prices can miss requests a reply needed; Claude's own total covers them.
        const usd = Math.max(costOf(r.model || b.model, r.usage) ?? 0, r.usage?.costUsd ?? 0);
        const output = b.provider === "codex" ? dropNulls(r.output) : r.output;
        const result = { stage, role, model: r.model || b.model, output, usage: r.usage, usd, ms: Date.now() - started };
        store.ledger({ run, key, attempt, ok: true, ...result, output: undefined });
        store.cachePut(key, result);
        account(spend, role, r.usage, usd, r.model || b.model);
        onResult?.({ ...result, cached: false });
        if (tally) addTally(tally, r.usage, usd, Date.now() - started);
        return output;
      }
    } finally {
      reserved -= estimate;
    }
    throw new Stop("failed", `${stage} call failed twice; see ledger key ${key.slice(0, 12)}`);
  }

  // Jev items, each {id, state, questions}. Returns a Map of id to answers for
  // the items that succeeded; the rest are in the ledger and retried later.
  async function evaluate({ stage, items, estimatePerItem = 0 }) {
    const b = bindings.decide;
    const out = new Map();
    const todo = [];
    for (const item of items) {
      const key = itemKey({ stage, model: b.model, item });
      const hit = store.cacheGet(key);
      if (hit) out.set(item.id, hit.answers);
      else todo.push({ ...item, key });
    }
    (spend.byRole.decide ??= { calls: 0, cached: 0, inputTokens: 0, outputTokens: 0, usd: 0 }).cached += items.length - todo.length;
    for (let attempt = 1; attempt <= 2 && todo.some((t) => !out.has(t.id)); attempt++) {
      const pending = todo.filter((t) => !out.has(t.id));
      for (let k = 0; k < pending.length; k += JEV_ITEMS) {
        const chunk = pending.slice(k, k + JEV_ITEMS);
        const est = estimatePerItem * chunk.length;
        reserve(est);
        let res;
        try {
          res = await clients.evaluateBatch({ items: chunk.map((t) => ({ id: t.id, model: b.model, state: t.state, questions: t.questions })), concurrency: 8, budgetMs: 60000 });
        } catch (err) {
          if (/\b40[13]\b|credential|api key|TYPESAFE/i.test(err.message)) throw new Stop("credential", `Jev is unavailable (${err.message.slice(0, 200)}); set up the TypeSafe key and resume, or pass --abandon and start an unscreened run (--unscreened) over a small window`);
          res = { items: [], error: err.message };
        } finally {
          reserved -= est;
        }
        const usage = { inputTokens: res.usage?.input_tokens ?? 0, outputTokens: res.usage?.output_tokens ?? 0 };
        const usd = costOf(b.model, usage);
        store.ledger({ run, stage, role: "decide", attempt, ok: true, items: chunk.length, usage, usd });
        account(spend, "decide", usage, usd);
        const byId = new Map((res.items ?? []).map((it) => [it.id, it]));
        for (const t of chunk) {
          const it = byId.get(t.id);
          if (it?.status === "succeeded" && it.response?.answers) {
            out.set(t.id, it.response.answers);
            store.cachePut(t.key, { stage, answers: it.response.answers });
            continue;
          }
          store.ledger({ run, stage, role: "decide", key: t.key, attempt, ok: false, status: it?.status ?? "missing", error: String(it?.error ?? res.error ?? "").slice(0, 300) });
          if ([401, 402, 403].includes(it?.statusCode)) throw new Stop("credential", `TypeSafe returned HTTP ${it.statusCode}; set up the TypeSafe key and resume, or pass --abandon and start an unscreened run (--unscreened) over a small window`);
        }
      }
    }
    return out;
  }

  return { generate, evaluate };
}

// Per-unit usage, for reports that attribute cost to a batch or cluster.
export const newTally = () => ({ calls: 0, cached: 0, usd: 0, ms: 0, usage: {} });
function addTally(t, usage, usd, ms) {
  t.calls++;
  t.usd += usd ?? 0;
  t.ms += ms;
  for (const [k, v] of Object.entries(usage ?? {})) if (typeof v === "number") t.usage[k] = (t.usage[k] ?? 0) + v;
}

const SEVERITY = { failed: 1, budget: 2, binding: 3, credential: 3, error: 4 };

// Bounded concurrency. A failed unit lets the others finish; any other Stop,
// or an unexpected error, halts new work. Every in-flight unit returns before
// the most severe one is thrown, so nothing writes after the lock is released.
export async function pool(items, n, fn) {
  let next = 0;
  let stop = null;
  let halted = false;
  await Promise.all(
    Array.from({ length: Math.min(n, items.length) }, async () => {
      while (!halted && next < items.length) {
        const k = next++;
        try {
          await fn(items[k], k);
        } catch (err) {
          const reason = err instanceof Stop ? err.reason : "error";
          if (!stop || SEVERITY[reason] > SEVERITY[stop instanceof Stop ? stop.reason : "error"]) stop = err;
          if (reason !== "failed") halted = true;
        }
      }
    }),
  );
  if (stop) throw stop;
}
