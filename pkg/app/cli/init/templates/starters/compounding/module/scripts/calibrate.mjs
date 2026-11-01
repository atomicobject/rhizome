#!/usr/bin/env node
// SPEC-0124 US6. Run from the repository root. No budget means no model calls.
import { execFileSync } from "node:child_process";
import { readFileSync, realpathSync } from "node:fs";
import { join, relative, resolve, sep } from "node:path";
import { pathToFileURL } from "node:url";
import { Stop, rzmClients } from "./lib/calls.mjs";
import { fixtureCheckout, labeledSet } from "./lib/calibration-data.mjs";
import { bindingKey, calibrationBindings, calibrationEstimate, calibrationProfile } from "./lib/calibration-models.mjs";
import { compareMeasurements, measure } from "./lib/calibration-score.mjs";
import { openStore, readJson, sha } from "./lib/store.mjs";

function localStore(root) {
  const dir = join(root, ".rhizome/compounding");
  try {
    execFileSync("git", ["-C", root, "check-ignore", "-q", "--", ".rhizome/compounding/ledger.jsonl"], { stdio: "pipe" });
  } catch {
    throw new Error("Git must ignore .rhizome/compounding; run rzm init before calibrating");
  }
  return openStore(dir);
}

function proposal(report) {
  const comparison = compareMeasurements(report.samples, report.set.synthetic);
  if (report.status !== "done" || !comparison.winner) return null;
  const { candidate, reason } = comparison.winner;
  const b = candidate.binding;
  const chosen = { provider: b.provider, model: b.model, effort: b.effort, family: b.family,
    version: (b.model.match(/\d+/g) ?? []).slice(0, 2).join("."), evidence: report.evidenceFile };
  const before = report.previousCandidates;
  const after = [chosen, ...before.filter((c) => c.provider !== b.provider || c.model !== b.model || c.effort !== b.effort)];
  return { role: report.role, reason, evidence: report.evidenceFile, before, after };
}

export async function calibrate(opts, clients) {
  if (!["extract", "verify"].includes(opts.role)) throw new Error("--role must be extract or verify");
  if (opts.budget != null && (!Number.isFinite(opts.budget) || opts.budget <= 0)) throw new Error("--budget must be a positive finite number");
  const store = localStore(opts.root);
  store.lock();
  try {
    const prepared = await calibrationProfile(opts, clients, store);
    const set = labeledSet(opts.root, store, prepared.provider, opts.role);
    const dryBindings = await calibrationBindings(prepared, store, { dry: true });
    const estimate = calibrationEstimate(opts.role, dryBindings, set);
    const summary = { name: set.name, synthetic: set.synthetic, labelCount: set.labels.length, qualityLabels: set.labels.filter((l) => opts.role === "verify" || l.decision === "propose").length, localApproved: set.localApproved, staleExcluded: set.staleExcluded,
      limitation: set.synthetic ? "Fixture quality supports only cost/latency-driven changes; quality-driven diffs are refused." : null };
    const log = opts.log ?? ((text) => process.stderr.write(text + "\n"));
    log(JSON.stringify({ set: summary, estimate, transcriptsTo: [prepared.provider] }));
    if (opts.budget == null) return { status: "estimate", set: summary, estimate, bindings: dryBindings, next: "rerun with --budget USD" };
    if (estimate.unpriced.length) throw new Error(`no price for ${estimate.unpriced.join(", ")}; choose priced candidates so the budget can be enforced`);
    const id = `${new Date().toISOString().replace(/[:.]/g, "-")}-${sha([process.pid, Math.random()]).slice(0, 6)}`;
    const evidenceFile = `.rhizome/compounding/calibrations/${id}/evidence.json`;
    const name = `calibrations/${id}/evidence.json`;
    const spend = { usd: 0, byRole: {} };
    const report = { version: 1, id, role: opts.role, status: "running", set: summary, estimate, evidenceFile, provenance: prepared.provenance, localProfileHash: sha(prepared.local),
      previousCandidates: prepared.profile.roles[opts.role].map(({ source, ...c }) => c), candidates: Object.values(prepared.profile.roles).flat(), samples: [], spend };
    store.write(name, report);
    store.write(`calibrations/${id}/inputs.json`, set);
    try {
      const bindings = await calibrationBindings(prepared, store, { dry: false, budget: opts.budget, spend, clients, run: id });
      report.bindings = bindings;
      const actual = calibrationEstimate(opts.role, bindings, set);
      if (actual.unpriced.length) throw new Stop("binding", `resolved models have no price: ${actual.unpriced.join(", ")}`);
      store.write(name, report);
      const root = set.synthetic && opts.role === "verify" ? fixtureCheckout(store, id, set.files) : opts.root;
      const ctx = { root, search: set.synthetic ? async () => "(synthetic fixture: no other documents)" : (args) => clients.search(args, { cwd: root }),
        exclude: set.synthetic ? undefined : (path) => path.startsWith("docs/memory/") || path.startsWith(".rhizome/compounding/") };
      for (const [i, row] of actual.rows.entries()) {
        const sample = await measure({ role: opts.role, binding: row.binding, match: bindings.match, set, store, clients, run: id, budget: opts.budget, spend, ctx, bypassCache: i === 1 });
        report.samples.push(sample);
        store.write(name, report);
        log(JSON.stringify({ binding: bindingKey(row.binding), ...sample }));
      }
      report.status = "done";
    } catch (err) {
      if (!(err instanceof Stop)) throw err;
      report.status = "stopped";
      report.reason = err.reason;
      report.message = err.message;
    }
    const { noiseFloor, refusals } = compareMeasurements(report.samples, set.synthetic);
    Object.assign(report, { noiseFloor, refusals, diff: proposal(report) });
    store.write(name, report);
    return report;
  } finally {
    store.unlock();
  }
}

export function applyCalibration({ root, evidenceFile, confirm }) {
  if (!confirm) throw new Error("review the evidence and profile diff, then pass --confirm to apply it");
  const store = localStore(root);
  store.lock();
  try {
    const file = realpathSync(resolve(root, evidenceFile));
    const parent = realpathSync(store.path("calibrations"));
    if (!file.startsWith(parent + sep) || !file.endsWith(`${sep}evidence.json`)) throw new Error("evidence must be a local calibration evidence.json");
    const report = readJson(file);
    if (report.version !== 1 || !["extract", "verify"].includes(report.role) || report.status !== "done") throw new Error("not a completed calibration");
    if (relative(realpathSync(root), file).split(sep).join("/") !== report.evidenceFile) throw new Error("evidence path differs from the reviewed diff");
    const local = store.read("models.json", null);
    if (sha(local) !== report.localProfileHash) throw new Error("local profile changed; recalibrate before applying");
    const shipped = readFileSync(new URL("../models.json", import.meta.url), "utf8");
    if (sha(shipped) !== report.provenance.files["models.json"]) throw new Error("shipped profile changed; recalibrate before applying");
    const diff = proposal(report);
    if (!diff || sha(diff) !== sha(report.diff)) throw new Error("no eligible reviewed profile diff; fixture quality alone cannot justify changes");
    store.write("models.json", { ...local, version: JSON.parse(shipped).version, roles: { ...local?.roles, [diff.role]: diff.after } });
    return { status: "applied", path: ".rhizome/compounding/models.json", diff };
  } finally {
    store.unlock();
  }
}

function args(argv) {
  const out = { candidates: [] };
  if (argv[0] === "apply") { out.apply = true; out.evidenceFile = argv[1]; argv = argv.slice(2); }
  for (let i = 0; i < argv.length; i++) {
    const key = argv[i];
    if (key === "--confirm") out.confirm = true;
    else if (key === "--candidates") {
      const start = out.candidates.length;
      while (argv[i + 1] && !argv[i + 1].startsWith("--")) out.candidates.push(...argv[++i].split(",").filter(Boolean));
      if (out.candidates.length === start) throw new Error("--candidates needs provider:model:effort values");
    } else if (["--role", "--harness", "--budget"].includes(key) && argv[i + 1] && !argv[i + 1].startsWith("--")) out[key.slice(2)] = argv[++i];
    else throw new Error(`unexpected argument ${key}`);
  }
  if (out.budget != null) out.budget = Number(out.budget);
  return out;
}

if (process.argv[1] && import.meta.url === pathToFileURL(realpathSync(process.argv[1])).href) {
  try {
    const options = args(process.argv.slice(2));
    const root = execFileSync("git", ["rev-parse", "--show-toplevel"], { encoding: "utf8" }).trim();
    const result = options.apply ? applyCalibration({ ...options, root }) : await calibrate({ ...options, root }, rzmClients(process.env.RZM || "rzm", { cwd: root }));
    process.stdout.write(JSON.stringify(result, null, 2) + "\n");
    process.exitCode = result.status === "stopped" ? 3 : 0;
  } catch (err) {
    process.stderr.write(err.message + "\n");
    process.exitCode = 2;
  }
}
