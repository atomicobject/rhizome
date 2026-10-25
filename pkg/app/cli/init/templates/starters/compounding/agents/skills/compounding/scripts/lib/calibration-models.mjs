import { existsSync, readFileSync } from "node:fs";
import { costOf, Stop, strictSchema } from "./calls.mjs";
import { callCost, callEstimates } from "./estimate.mjs";
import { batches } from "./extract.mjs";
import { mergeProfile, resolve } from "./models.mjs";
import { sha } from "./store.mjs";

export async function calibrationProfile(opts, clients, store) {
  const text = readFileSync(new URL("../../models.json", import.meta.url), "utf8");
  const localText = existsSync(store.path("models.json")) ? readFileSync(store.path("models.json"), "utf8") : null;
  const local = localText === null ? null : JSON.parse(localText);
  const profile = mergeProfile(JSON.parse(text), local);
  const statuses = await clients.harnessStatus();
  let provider = opts.harness || await clients.configuredHarness?.();
  if (!provider) {
    const ready = statuses.filter((s) => s.installed && s.loggedIn);
    if (ready.length !== 1) throw new Error("pass --harness claude|codex: no unique signed-in provider");
    provider = ready[0].kind;
  }
  if (!["claude", "codex"].includes(provider)) throw new Error("--harness must be claude or codex");
  const roles = { [opts.role]: profile.roles[opts.role] };
  if (opts.role === "extract") roles.match = profile.roles.match;
  const alternatives = opts.candidates?.length ? opts.candidates.map((value) => {
    const m = value.match(/^(claude|codex):([^:]+):(\w+)$/);
    if (!m) throw new Error(`candidate ${value}: expected provider:model:effort`);
    if (m[1] !== provider) throw new Error(`candidate ${value} is outside the ${provider} harness; transcript text cannot cross providers`);
    const known = Object.values(profile.roles).flat().find((c) => c.provider === m[1] && c.model === m[2]);
    const family = known?.family ?? (m[2].startsWith("claude-") ? m[2].split("-")[1] : m[2].split("-").at(-1));
    return { ...known, provider: m[1], model: m[2], family, version: (m[2].match(/\d+/g) ?? []).slice(0, 2).join("."), effort: m[3], source: "flag" };
  }) : profile.roles[opts.role].filter((c) => c.provider === provider && !c.none);
  alternatives.forEach((c, i) => { roles[`candidate${i}`] = [c]; });
  const files = { "models.json": sha(text) };
  if (localText !== null) files[".rhizome/compounding/models.json"] = sha(localText);
  return { profile: { ...profile, roles }, statuses, provider, local, provenance: { profileVersion: profile.version, files } };
}

export async function calibrationBindings(prepared, store, { dry, budget, spend, clients, run }) {
  const cache = new Map(Object.entries(store.read("probes.json", {})));
  const probe = async ({ provider, model, effort, candidate }) => {
    const status = prepared.statuses.find((s) => s.kind === provider);
    // A versioned catalog entry already proves availability. Aliases need a probe.
    if (status.models?.some((m) => m.id === model) && /\d/.test(model)) return model;
    if (dry) return /\d/.test(model) ? model : candidate.model;
    if (spend.usd + 0.01 > budget) throw new Stop("budget", "budget cannot cover the next availability probe");
    const result = await clients.generate({ provider, model, effort, prompt: 'Reply with {"ok":true}.', schema: strictSchema({ type: "object", properties: { ok: { type: "boolean" } }, required: ["ok"] }) });
    const usd = Math.max(costOf(result.model || model, result.usage) ?? 0, result.usage?.costUsd ?? 0);
    spend.usd += usd;
    store.ledger({ run, stage: "probe", role: "probe", provider, model, reported: result.model, usage: result.usage, usd, ok: !!result.model });
    if (spend.usd > budget) throw new Stop("budget", "actual probe spend exceeded the budget; no further calls will run");
    if (!result.model) throw new Error("probe did not report the model that ran");
    return result.model;
  };
  const result = await resolve(prepared.profile, { providers: [prepared.provider], statuses: prepared.statuses, probe, cache });
  if (prepared.profile.roles.match && !result.bindings.match) throw new Error("no model for match; calibration needs a frozen judge");
  for (const [role, b] of Object.entries(result.bindings)) if (b) {
    b.cliVersion = prepared.statuses.find((s) => s.kind === b.provider)?.version ?? null;
    b.family = prepared.profile.roles[role].find((c) => c.provider === b.provider && (c.model === b.model || b.model.split(/[-.\[]/).includes(c.family)))?.family;
  }
  if (!dry) store.write("probes.json", Object.fromEntries(cache));
  return result.bindings;
}

export const bindingKey = (b) => [b.provider, b.model, b.effort].join(":");

export function calibrationEstimate(role, bindings, set) {
  const current = bindings[role];
  const unique = new Map(Object.entries(bindings).filter(([key, b]) => key.startsWith("candidate") && b).map(([, b]) => [bindingKey(b), b]));
  unique.delete(bindingKey(current));
  const rows = [current, current, ...unique.values()].map((b, i) => {
    const per = callEstimates({ [role]: b });
    const calls = role === "extract" ? batches(set.episodes).length : set.labels.length;
    const usd = calls * (role === "extract" ? per.extract + per.repair : 4 * per.verify);
    return { binding: b, repetition: i === 1, calls, usd, priced: costOf(b.model, {}) !== null };
  });
  const match = bindings.match;
  const matchUsd = match ? rows.length * Math.ceil(set.labels.length / 20) * callCost(match, 20000, 2000) : 0;
  return { rows, matchUsd, usd: rows.reduce((n, r) => n + r.usd, matchUsd) + Object.keys(bindings).length * 0.01,
    unpriced: [...new Set([...rows.map((r) => r.binding), ...(match ? [match] : [])].filter((b) => costOf(b.model, {}) === null).map(bindingKey))],
    skipped: Object.entries(bindings).flatMap(([role, b]) => b ? b.skipped.map((s) => ({ role, ...s })) : [{ role, reason: "no available candidate" }]) };
}
