#!/usr/bin/env node
// Mine this repository's agent sessions into local proposals (SPEC-0124 US1):
// episodes past each session's cursor are screened by Jev passage by passage,
// extracted, grouped with earlier clusters, tiered, and verified (tier A
// always, tier B up to a per-run budget), and proposals land in
// .rhizome/compounding/proposals.json.
//
//   mine.mjs [--harness claude|codex] [--budget USD] [--dry-run]
//            [--since YYYY-MM-DD] [--until YYYY-MM-DD] [--episodes FILE]
//            [--verify-budget N] [--verify-held N] [--model ROLE=PROVIDER:MODEL:EFFORT]...
//            [--unscreened] [--abandon] [--state DIR] [--checkout DIR] [--concurrency N]
//            [--allow-unpriced]
//
// Without --budget it prints the estimate and spends nothing. With it, it runs
// or resumes the open run, which keeps its frozen bindings, and stops cleanly
// at the budget; rerun to resume. Model calls go through `rzm agent generate`
// and Jev through `rzm agent evaluate-batch` (RZM selects the binary). Run
// from the repository root. --episodes mines a prebuilt episode file instead
// of discovered sessions, for evaluations. A binding with no known price
// would reserve nothing against the budget, so it is refused unless
// --allow-unpriced accepts that only reported costs count.

import { execFileSync } from "node:child_process";
import { existsSync, readdirSync, readFileSync, rmSync } from "node:fs";
import { isAbsolute, join, relative, resolve as resolvePath, sep } from "node:path";
import { pathToFileURL } from "node:url";
import { Stop, costOf, makeCaller, pool, rzmClients, spendFrom, strictSchema } from "./lib/calls.mjs";
import { episodesFor } from "./lib/episodes.mjs";
import { callEstimates, estimate, formatEstimate } from "./lib/estimate.mjs";
import { batches, extractBatch } from "./lib/extract.mjs";
import { mergeProfile, resolve } from "./lib/models.mjs";
import { facts, group, rank, tierOf } from "./lib/reduce.mjs";
import { screen } from "./lib/screen.mjs";
import { loadFindings, recur } from "./lib/findings.mjs";
import { learningHash, loadDecisions, loadShards } from "./lib/shards.mjs";
import { openStore, readJsonl, sha, writeText } from "./lib/store.mjs";
import { verifyCluster } from "./lib/verify.mjs";
import { discover } from "./sessions.mjs";
import { pending } from "./shards.mjs";

const ROLES = ["decide", "extract", "synthesize", "verify"];
const DAY = 24 * 3600 * 1000;
// Codex accepts only strict schemas, and the probe goes straight to the client.
const PROBE = { prompt: 'Reply with {"ok": true}.', schema: strictSchema({ type: "object", properties: { ok: { type: "boolean" } }, required: ["ok"] }) };
// The most one availability probe costs; probes run before the run's caller
// exists, so the budget is checked against this before each one.
const PROBE_USD = 0.01;

export async function mine(opts, clients) {
  // No workers would skip extraction and verification yet still advance the cursors.
  const concurrency = Number(opts.concurrency ?? 4);
  if (!Number.isInteger(concurrency) || concurrency < 1) throw new Error("--concurrency must be a whole number of at least 1");
  const state = opts.state ?? join(opts.root, ".rhizome/compounding");
  refuseTracked(opts.root, state);
  const store = openStore(state);
  const dry = !opts.abandon && (opts.dryRun || opts.budget == null);
  if (dry) return mineLocked(opts, clients, store, true);
  store.lock();
  try {
    return await mineLocked(opts, clients, store, false);
  } finally {
    store.unlock();
  }
}

// Mining writes transcript text into its state directory, so a directory
// inside the checkout must be one Git ignores.
function refuseTracked(root, dir) {
  const rel = relative(root, resolvePath(dir));
  if (!rel || rel.startsWith("..") || isAbsolute(rel)) return;
  const path = rel.split(sep).join("/");
  try {
    execFileSync("git", ["-C", root, "check-ignore", "-q", "--", `${path}/ledger.jsonl`], { stdio: "ignore" });
  } catch (err) {
    if (err.status === 1) throw new Error(`Git does not ignore ${path}, so transcript text written there could be committed; rerun rzm init to restore .rhizome/.gitignore, or pass --state outside the checkout`);
  }
}

function openRun(store) {
  const dir = store.path("runs");
  const ids = existsSync(dir) ? readdirSync(dir).sort() : [];
  for (const id of ids.reverse()) {
    const run = store.read(`runs/${id}/manifest.json`, null);
    if (run && (run.status === "open" || run.status === "stopped")) return run;
  }
  return null;
}

const saveRun = (store, run) => store.write(`runs/${run.id}/manifest.json`, run);

// Only one run is open at a time, so the pool when it starts is exactly what
// abandoning it restores; its episodes then count as new again, and so does
// the recurrence bookkeeping a run keeps beside the pool. Review status is
// restored only where the run cleared it to reopen a cluster, so a decision
// a person made after the run started stays.
const POOL = ["pool/episodes.json", "pool/candidates.json", "pool/clusters.json", "proposals.json", "findings.json"];
const snapshot = (store, id) => store.write(`runs/${id}/pool-before.json`, Object.fromEntries([...POOL, "reviewed.json"].map((name) => [name, store.read(name, null)])));
function restore(store, id) {
  for (const [name, value] of Object.entries(store.read(`runs/${id}/pool-before.json`, {}))) {
    if (name === "reviewed.json") store.write(name, { ...value, ...store.read(name, {}) });
    else if (value === null) rmSync(store.path(name), { force: true });
    else store.write(name, value);
  }
}

// A frozen binding is unavailable when its CLI is gone, signed out, or no
// longer lists the model. A resumed run stops rather than switch.
function unavailable(bindings, statuses) {
  for (const [role, b] of Object.entries(bindings)) {
    if (!b || b.provider === "typesafe") continue;
    const s = statuses.find((x) => x.kind === b.provider);
    if (!s?.installed) return `${role}: ${b.provider} is not installed`;
    if (!s.loggedIn) return `${role}: ${b.provider} is not signed in`;
    // Claude's catalog lists aliases, so a full id bound by flag is checked by its calls instead.
    if (b.inCatalog && !s.models?.some((m) => m.id === b.requested || m.id === b.model)) return `${role}: ${b.requested} is no longer in the ${b.provider} catalog`;
  }
  return null;
}

// New episodes: past each session's cursor and through its last complete
// turn, or, from a prebuilt file, those not yet in the pool.
function collect(opts, store) {
  const seen = store.read("pool/episodes.json", {});
  if (opts.episodes) {
    const all = readJsonl(opts.episodes);
    return { episodes: all.filter((e) => !seen[e.id]), cursorTargets: {}, report: { file: opts.episodes, read: all.length } };
  }
  const since = opts.since ?? new Date((opts.now ?? Date.now()) - 7 * DAY).toISOString().slice(0, 10);
  const { sessions, report } = (opts.discover ?? discover)(opts.root, { since, until: opts.until, now: opts.now });
  const cursors = store.read("cursors.json", {});
  const episodes = [];
  const cursorTargets = {};
  const cmp = (x, y) => (x < y ? -1 : x > y ? 1 : 0);
  sessions.sort((x, y) => cmp(String(x.start), String(y.start)) || cmp(x.id, y.id));
  for (const s of sessions) {
    const key = `${s.harness}:${s.id}`;
    const from = (cursors[key]?.through ?? -1) + 1;
    if (s.completeThrough < from) continue;
    episodes.push(...episodesFor(s, s.family, { from, through: s.completeThrough }).episodes.filter((e) => !seen[e.id]));
    cursorTargets[key] = { through: s.completeThrough };
  }
  return { episodes, cursorTargets, report: { ...report, since, until: opts.until ?? null } };
}

// Resolves each role through the model profile. Probes are charged to the
// run. A dry run never probes; it names the profile's model so the estimate
// can price it.
async function bind(opts, clients, statuses, store, dry, runId) {
  const shippedText = readFileSync(new URL("../models.json", import.meta.url), "utf8");
  const localPath = store.path("models.json");
  const localText = existsSync(localPath) ? readFileSync(localPath, "utf8") : null;
  const profile = mergeProfile(JSON.parse(shippedText), localText && JSON.parse(localText), opts.model ?? []);
  profile.roles = Object.fromEntries(ROLES.map((r) => [r, profile.roles[r] ?? []]));
  const probes = new Map(Object.entries(store.read("probes.json", {})));
  let probeSpend = 0;
  const probe = async ({ provider, model, effort, candidate }) => {
    if (dry) return candidate?.model ?? model;
    if (opts.budget != null && probeSpend + PROBE_USD > opts.budget) throw new Stop("budget", `budget $${opts.budget} cannot cover checking ${provider}:${model}`);
    const r = await clients.generate({ provider, model, effort, ...PROBE });
    const usd = Math.max(costOf(r.model || model, r.usage) ?? 0, r.usage?.costUsd ?? 0);
    probeSpend += Math.max(usd, PROBE_USD);
    store.ledger({ run: runId, stage: "probe", role: "probe", provider, model, ok: !!r.model, reported: r.model, usage: r.usage, usd });
    // Freezing an alias would price nothing and stop the run at its first call.
    if (!r.model) throw new Error(`${provider} did not report which model ran for ${model}`);
    return r.model;
  };
  let provider = opts.harness;
  if (!provider) {
    const ready = statuses.filter((s) => s.installed && s.loggedIn).map((s) => s.kind);
    if (ready.length === 1) provider = ready[0];
    else if (dry) throw new Error(`pass --harness (signed in: ${ready.join(", ") || "none"})`);
    else provider = (await clients.generate(PROBE)).provider;
  }
  const { bindings } = await resolve(profile, { providers: [provider], statuses, probe, cache: probes });
  if (!dry) store.write("probes.json", Object.fromEntries(probes));
  for (const b of Object.values(bindings)) {
    if (!b || b.provider === "typesafe") continue;
    const status = statuses.find((s) => s.kind === b.provider);
    b.cliVersion = status?.version ?? null;
    b.inCatalog = !!status?.models?.some((m) => m.id === b.requested);
  }
  const files = { "models.json": sha(shippedText) };
  if (localText) files[".rhizome/compounding/models.json"] = sha(localText);
  return { bindings, provenance: { profileVersion: profile.version, files, flags: opts.model ?? [] } };
}

// The findings record recomputed from the pool as it is, or null when that
// would reopen a recurrence, which a run must verify.
function refreshFindings(store, root) {
  const read = (name) => store.read(name, {});
  const { reopened, record } = recur(read("findings.json"), { findings: loadFindings(root), clusters: read("pool/clusters.json"), candidates: read("pool/candidates.json"), episodes: read("pool/episodes.json"), reviewed: read("reviewed.json"), links: read("finding-links.json") });
  return reopened.size ? null : record;
}

async function mineLocked(opts, clients, store, dry) {
  const log = opts.log ?? ((m) => process.stderr.write(m + "\n"));
  let run = openRun(store);
  let episodes;
  if (opts.abandon) {
    if (!run) return { status: "nothing-to-abandon" };
    restore(store, run.id);
    saveRun(store, { ...run, status: "abandoned" });
    return { status: "abandoned", run: run.id };
  }
  if (run) {
    // Screening is frozen with the run, so the flag cannot change it mid-run.
    if (opts.unscreened && !run.unscreened) throw new Error(`run ${run.id} is screened and resumes screened; pass --abandon first, then start an unscreened run over a small window`);
    episodes = readJsonl(store.path("runs", run.id, "episodes.jsonl"));
    const why = unavailable(run.bindings, await clients.harnessStatus());
    if (why) {
      if (!dry) saveRun(store, { ...run, status: "stopped", stoppedReason: "binding", stoppedMessage: why });
      return { status: "stopped", reason: "binding", message: `${why}; the run keeps its frozen bindings, so resume when it is available or rerun with --abandon`, run: run.id };
    }
    log(`resuming run ${run.id} with its frozen bindings`);
  } else {
    const found = collect(opts, store);
    episodes = found.episodes;
    // A pending proposal with no response, or a finding whose friction the
    // pool shows came back, is verified again even when nothing is new.
    const record = !episodes.length && !opts.verifyHeld && !pending(store, opts.root).some((p) => !p.response && !p.rejected) && refreshFindings(store, opts.root);
    if (record) {
      if (!dry && Object.keys(found.cursorTargets).length) store.write("cursors.json", { ...store.read("cursors.json", {}), ...found.cursorTargets });
      // A finding checked since the last run gets its report dates now.
      if (!dry) store.write("findings.json", record);
      return { status: "nothing-new", discovery: found.report };
    }
    const id = `${new Date().toISOString().replace(/[-:]/g, "").replace(/\..*/, "")}-${sha([process.pid, Math.random()]).slice(0, 6)}`;
    const statuses = await clients.harnessStatus();
    let bound;
    try {
      bound = await bind(opts, clients, statuses, store, dry, id);
    } catch (err) {
      if (!(err instanceof Stop)) throw err;
      return { status: "stopped", reason: err.reason, message: err.message };
    }
    run = {
      id,
      status: "open",
      startedAt: new Date().toISOString(),
      checkout: resolvePath(opts.checkout ?? opts.root),
      unscreened: !!opts.unscreened,
      verifyBudget: opts.verifyBudget ?? 40,
      verifyHeld: opts.verifyHeld ?? 0,
      ...bound,
      discovery: found.report,
      cursorTargets: found.cursorTargets,
      episodes: episodes.length,
    };
    if (!dry) {
      writeText(store.path("runs", run.id, "episodes.jsonl"), episodes.map((e) => JSON.stringify(e) + "\n").join(""));
      snapshot(store, run.id);
      saveRun(store, run);
    }
  }
  const est = estimate(run, episodes, store);
  log(formatEstimate(est));
  if (dry) return { status: "estimate", estimate: est, bindings: run.bindings, discovery: run.discovery, next: "rerun with --budget <USD> to spend up to that amount" };
  const unpriced = est.rows.filter((r) => !r.priced).map((r) => `${r.role} (${r.binding})`);
  if (unpriced.length && !opts.allowUnpriced) throw new Error(`no price is known for ${unpriced.join(", ")}, so the budget cannot reserve for it; choose a priced model with --model, or pass --allow-unpriced to count only the costs the provider reports`);
  run.budget = opts.budget;
  const spend = spendFrom(readJsonl(store.path("ledger.jsonl")), run.id);
  const caller = makeCaller({ clients, store, run: run.id, bindings: run.bindings, budget: opts.budget, spend });
  try {
    const report = await stages({ root: opts.root, store, run, caller, clients, episodes, concurrency: Number(opts.concurrency ?? 4), log });
    store.write("cursors.json", { ...store.read("cursors.json", {}), ...run.cursorTargets });
    saveRun(store, { ...run, status: "done", finishedAt: new Date().toISOString(), spend, report });
    return { status: "done", run: run.id, spend, report };
  } catch (err) {
    if (!(err instanceof Stop)) throw err;
    saveRun(store, { ...run, status: "stopped", stoppedReason: err.reason, stoppedMessage: err.message, spend });
    return { status: "stopped", reason: err.reason, message: err.message, run: run.id, spend };
  }
}

// A proposal verified before verification chose a response has none, so it
// is verified again rather than defaulting to remember.
const unrouted = (c) => c.verification?.decision === "propose" && !c.verification.response;

// A cluster needs verification when it has none, when it has no response,
// or when a model rejected it and another family has since supported it.
// Decisions a person made live in reviewed.json and decision files, not
// here, so they are never reopened.
const due = (c) => !c.verification || unrouted(c) || (c.verification.decision === "reject" && c.families.length > (c.verification.families ?? Infinity));

function cleanHead(dir) {
  try {
    if (execFileSync("git", ["-C", dir, "status", "--porcelain", "--untracked-files=no"], { encoding: "utf8", stdio: ["ignore", "pipe", "ignore"] }).trim()) return null;
  } catch {
    return null;
  }
  return headCommit(dir);
}

function headCommit(dir) {
  try {
    return execFileSync("git", ["-C", dir, "rev-parse", "HEAD"], { encoding: "utf8", stdio: ["ignore", "pipe", "ignore"] }).trim();
  } catch {
    return null;
  }
}

const stub = (e) => ({ id: e.id, harness: e.harness, session: e.session, family: e.family, lens: e.lens, origin: e.origin, confidence: e.confidence, ts: e.ts, kept: false });

// Each stage reads what earlier stages wrote and skips finished work, so a
// killed or stopped run resumes without repeating a completed call.
async function stages({ root, store, run, caller, clients, episodes, concurrency, log }) {
  const per = callEstimates(run.bindings);
  const screened = run.unscreened || !episodes.length ? { gated: episodes, passages: 0 } : await screen(caller, episodes, { estimatePerItem: per.jev });
  const gated = screened.gated;
  log(`screen: kept ${gated.length} of ${episodes.length} episodes`);

  const work = batches(gated);
  const results = [];
  await pool(work, concurrency, async (batch, k) => {
    results[k] = await extractBatch(caller, batch, { estimate: per.extract, repairEstimate: per.repair });
    log(`extract: batch ${k + 1}/${work.length}, ${results[k].candidates.length} candidates`);
  });
  const eps = store.read("pool/episodes.json", {});
  for (const e of episodes) eps[e.id] ??= stub(e);
  for (const e of gated) eps[e.id] = { ...e, kept: true };
  store.write("pool/episodes.json", eps);
  const cands = store.read("pool/candidates.json", {});
  const fresh = results.flatMap((r) => r.candidates.map((c) => c.id));
  for (const r of results) for (const c of r.candidates) cands[c.id] ??= c;
  store.write("pool/candidates.json", cands);

  const clusters = store.read("pool/clusters.json", {});
  // A person already settled what they approved or dismissed, here or in a
  // teammate's recorded decision, shard, or finding.
  const reviewed = store.read("reviewed.json", {});
  const findings = loadFindings(root);
  const decided = [...loadDecisions(root, store.dir), ...loadShards(root), ...findings];
  const settled = new Set(decided.map((d) => d.learning ?? learningHash(d)));
  // Settled learnings join grouping as clusters, so a reworded rediscovery
  // joins the settled cluster and is skipped like the original.
  const hashOf = (c) => c.learning ?? learningHash(c);
  const pooled = new Set(Object.values(clusters).map(hashOf));
  let seq = Math.max(0, ...Object.values(clusters).map((c) => c.seq ?? 0));
  for (const d of decided) {
    const hash = d.learning ?? learningHash(d);
    if (pooled.has(hash)) continue;
    pooled.add(hash);
    const id = `k-settled-${hash.slice(0, 12)}`;
    clusters[id] = { id, seq: ++seq, members: [], kind: d.kind, statement: d.statement, appliesTo: d.appliesTo ?? [], commands: d.commands ?? [], learning: hash, settled: true, createdAt: new Date().toISOString() };
  }
  const firstSeen = (id) => Math.min(...(cands[id]?.evidence ?? []).map((e) => Date.parse(eps[e]?.ts) || Infinity));
  await group(caller, { candidates: cands, clusters, fresh, firstSeen, save: (c) => store.write("pool/clusters.json", c), estimate: per.group });
  const freshSet = new Set(fresh);
  const touched = Object.values(clusters).filter((cl) => cl.members.some((id) => freshSet.has(id)));
  const ranks = run.unscreened ? new Map() : await rank(caller, touched, cands, { estimatePerItem: per.jev });
  for (const cl of touched) {
    const f = facts(cl, cands, eps);
    Object.assign(cl, { evidence: f.evidence, families: f.families, tier: tierOf(f, cl, ranks.get(cl.id)), worth: ranks.get(cl.id)?.worth ?? null });
  }
  // Episodes matching a finding are recorded for the friction report; a
  // completed finding they recur for is offered for review again.
  const links = store.read("finding-links.json", {});
  const { reopened, record } = recur(store.read("findings.json", {}), { findings, clusters, candidates: cands, episodes: eps, reviewed, links });
  store.write("pool/clusters.json", clusters);
  store.write("finding-links.json", links);
  // A reopened cluster's earlier draft is withdrawn before its review status
  // is cleared, so a run that stops before re-verifying offers nothing stale.
  if (reopened.size) {
    store.write("proposals.json", store.read("proposals.json", []).filter((p) => !reopened.has(p.cluster)));
    store.write("reviewed.json", reviewed);
  }
  store.write("findings.json", record);
  const reopen = (c) => !!c.recurrence && !reviewed[c.id];

  // The plan is frozen once, so a resumed run verifies the same clusters.
  const planName = `runs/${run.id}/verify-plan.json`;
  let plan = store.read(planName, null);
  if (!plan) {
    // A learning a person already settled, here or in a teammate's shared
    // decision or shard, does not spend a verification call.
    const open = (c) => due(c) && (reopen(c) || (!reviewed[c.id] && !settled.has(hashOf(c))));
    const byWorth = (x, y) => (y.worth ?? 0) - (x.worth ?? 0) || (x.id < y.id ? -1 : 1);
    const ids = new Set(touched.map((c) => c.id));
    plan = [
      ...touched.filter((c) => open(c) && (c.tier === "A" || reopen(c))),
      ...touched.filter((c) => open(c) && c.tier === "B").sort(byWorth).slice(0, run.verifyBudget),
      ...Object.values(clusters).filter((c) => open(c) && c.tier === "B" && !ids.has(c.id)).sort(byWorth).slice(0, run.verifyHeld),
      ...Object.values(clusters).filter((c) => open(c) && (unrouted(c) || reopen(c))),
    ].map((c) => c.id);
    plan = [...new Set(plan)];
    store.write(planName, plan);
  } else {
    // A resumed run keeps its plan, but a recurrence reopened since it
    // stopped had its draft withdrawn above, so it is verified too.
    const added = [...reopened].filter((id) => clusters[id] && !plan.includes(id));
    if (added.length) {
      plan = [...plan, ...added];
      store.write(planName, plan);
    }
  }
  const ctx = { root: run.checkout, search: (args) => clients.search(args, { cwd: run.checkout }) };
  // Verification reads the working tree, so it vouches for a commit only when
  // tracked files match it; otherwise approval asks for a re-check.
  const commit = cleanHead(run.checkout);
  await pool(plan, concurrency, async (id) => {
    const cl = clusters[id];
    if (!due(cl)) return;
    const members = cl.members.map((m) => cands[m]).filter(Boolean);
    const variants = members.map((c) => ({ kind: c.kind, statement: c.statement, why: c.why, appliesTo: c.appliesTo, commands: c.commands, target: c.target }));
    const sources = excerpts(cl, eps);
    let v;
    try {
      v = await verifyCluster(caller, ctx, { cl, variants, sources, estimate: per.verify });
    } catch (err) {
      // One unreadable packet fails its cluster, not the run.
      if (err instanceof Stop) throw err;
      throw new Stop("failed", `verify ${id}: ${err.message}`);
    }
    const at = new Date().toISOString();
    if (v.contractProblems.length) {
      // A reply that still breaks the contract after repair stays retryable: an
      // unchanged packet replays from the cache at no cost, and new evidence asks again.
      cl.verifyProblems = { problems: v.contractProblems, run: run.id, at };
    } else {
      cl.verification = { ...v.result, contractProblems: [], rounds: v.rounds.length, run: run.id, commit, model: run.bindings.verify.model, at, families: cl.families.length };
      delete cl.verifyProblems;
    }
    store.write("pool/clusters.json", clusters);
    log(`verify: ${id} tier ${cl.tier} ${v.result.decision}`);
  });

  // A recurrence the verifier rejects stays in review with the reason, since
  // only a person decides whether the finding's friction is back.
  // A recurrence whose re-verification broke the output contract is offered
  // too: a retry would replay the same cached answer, so a person decides.
  const offer = (cl) => (cl.verification?.decision === "propose" ? !cl.verification.contractProblems.length : reopen(cl) && (cl.verification?.decision === "reject" || (!cl.verification && !!cl.verifyProblems)));
  const proposals = Object.values(clusters).filter(offer).map((cl) => proposal(cl, eps)).filter((p) => reopen(clusters[p.cluster]) || (!reviewed[p.cluster] && !settled.has(learningHash(p))));
  store.write("proposals.json", proposals);
  return { ...summarize({ episodes, screened, results, touched, plan, clusters, fresh, proposals }), recurring: reopened.size };
}

// One excerpt per family, so the verifier sees independent sources; a
// recurrence's episodes come first, those not offered before ahead of the
// rest, and the newer of equals first, so the newest friction is what it checks.
export function excerpts(cl, eps) {
  const recurring = new Set((cl.recurrence?.episodes ?? []).map((e) => e.id));
  const fresh = new Set(cl.recurrence?.fresh ?? []);
  const rank = (x) => fresh.has(x) + recurring.has(x);
  const time = (x) => Date.parse(eps[x]?.ts) || 0;
  const perFamily = new Map();
  for (const e of [...cl.evidence].sort((x, y) => rank(y) - rank(x) || time(y) - time(x)).map((x) => eps[x]).filter((e) => e?.text)) if (!perFamily.has(e.family)) perFamily.set(e.family, e);
  return [...perFamily.values()].slice(0, 6).map((e) => ({ lens: e.lens, origin: e.origin, confidence: e.confidence, previous: e.context?.previous?.slice(0, 600) || undefined, text: e.text.slice(0, 1500) }));
}

function proposal(cl, eps) {
  const broken = cl.verifyProblems;
  const v = cl.verification ?? { decision: "reject", rejectReason: "contract", notes: broken.problems.join("; "), run: broken.run, at: broken.at };
  // A rejection drafts nothing, so no response field it carried is proposed.
  const r = v.decision === "reject" ? {} : v;
  return {
    cluster: cl.id,
    tier: cl.tier,
    families: cl.families.length,
    familyIds: cl.families,
    rejected: v.decision === "reject" ? { reason: v.rejectReason ?? "other", notes: v.notes } : undefined,
    kind: v.kind ?? cl.kind,
    statement: v.statement ?? cl.statement,
    why: v.why ?? cl.why,
    appliesTo: v.appliesTo ?? cl.appliesTo,
    commands: v.commands ?? cl.commands,
    target: v.target ?? cl.target,
    response: r.response,
    responseReason: r.responseReason,
    workaround: r.workaround,
    edit: r.edit,
    documentHash: r.documentHash,
    change: r.change,
    guardRule: r.guardRule,
    question: r.question,
    owner: r.owner,
    recurrence: cl.recurrence,
    documentation: v.documentation,
    verification: v.verification,
    notes: v.notes,
    evidence: cl.evidence.map((id) => eps[id]).filter(Boolean).map((e) => ({ episode: e.id, harness: e.harness, session: e.session, ts: e.ts, lens: e.lens, origin: e.origin, toolCalls: e.toolCalls ?? 0 })),
    verified: { run: v.run, commit: v.commit, model: v.model, at: v.at },
  };
}

function summarize({ episodes, screened, results, touched, plan, clusters, fresh, proposals }) {
  const count = (xs, f) => xs.reduce((acc, x) => ((acc[f(x)] = (acc[f(x)] ?? 0) + 1), acc), {});
  const planned = new Set(plan);
  const verified = plan.map((id) => clusters[id]).filter((c) => c.verification);
  const freshSet = new Set(fresh);
  return {
    episodes: { read: episodes.length, kept: screened.gated.length, passages: screened.passages },
    candidates: { extracted: fresh.length, repaired: results.reduce((t, r) => t + r.repaired, 0), dropped: results.reduce((t, r) => t + r.dropped, 0), merged: touched.filter((c) => c.members.length > 1).reduce((t, c) => t + c.members.filter((m) => freshSet.has(m)).length, 0) },
    clusters: { touched: touched.length, byTier: count(touched, (c) => c.tier) },
    verified: { planned: plan.length, proposed: verified.filter((c) => c.verification.decision === "propose").length, rejected: count(verified.filter((c) => c.verification.decision === "reject"), (c) => c.verification.rejectReason ?? "other"), contractProblems: plan.map((id) => clusters[id]).filter((c) => !c.verification && c.verifyProblems).length },
    held: { tierC: touched.filter((c) => c.tier === "C").length, tierBOverBudget: touched.filter((c) => c.tier === "B" && !planned.has(c.id) && !c.verification).length },
    proposals: proposals.length,
  };
}

function args(argv) {
  const out = { model: [] };
  for (let k = 0; k < argv.length; k++) {
    const a = argv[k];
    if (!a.startsWith("--")) throw new Error(`unexpected argument ${a}`);
    const name = a.slice(2);
    const value = argv[k + 1] === undefined || argv[k + 1].startsWith("--") ? true : argv[++k];
    if (name === "model") out.model.push(value);
    else out[name] = value;
  }
  return out;
}

const isMain = process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href;
if (isMain) {
  const a = args(process.argv.slice(2));
  if (a.harness && !["claude", "codex"].includes(a.harness)) throw new Error("--harness must be claude or codex");
  const budget = a.budget === undefined ? null : Number(a.budget);
  if (budget !== null && !(budget > 0)) throw new Error("--budget must be a positive number of US dollars");
  let root = process.cwd();
  try {
    root = execFileSync("git", ["rev-parse", "--show-toplevel"], { encoding: "utf8", stdio: ["ignore", "pipe", "ignore"] }).trim();
  } catch {}
  const rzm = process.env.RZM || (existsSync(join(root, "bin/rzm")) ? join(root, "bin/rzm") : "rzm");
  const result = await mine(
    {
      root,
      harness: a.harness,
      budget,
      dryRun: !!a["dry-run"],
      since: a.since,
      until: a.until,
      episodes: a.episodes,
      verifyBudget: a["verify-budget"] === undefined ? undefined : Number(a["verify-budget"]),
      verifyHeld: a["verify-held"] === undefined ? undefined : Number(a["verify-held"]),
      model: a.model,
      unscreened: !!a.unscreened,
      abandon: !!a.abandon,
      state: a.state,
      checkout: a.checkout,
      concurrency: a.concurrency,
      allowUnpriced: !!a["allow-unpriced"],
    },
    rzmClients(rzm, { cwd: root }),
  );
  process.stdout.write(JSON.stringify(result, null, 2) + "\n");
  process.exitCode = result.status === "stopped" ? 3 : 0;
}
