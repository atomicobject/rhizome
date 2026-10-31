#!/usr/bin/env node
// Counts-only snapshot. Raw ledgers retain the cost and latency of paid calls
// even when a resumed arm overwrites its batch summaries with cache hits.
import { existsSync, readFileSync, readdirSync, realpathSync, writeFileSync } from "node:fs";
import { join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { PRICES } from "../../pkg/app/cli/init/templates/starters/compounding/agents/skills/compounding/scripts/lib/calls.mjs";

const ROOT = resolve(".rhizome/session-memory/eval/runs/terra61-2026-10-30");
const GOLD = 192;
const read = (path, fallback = null) => existsSync(path) ? JSON.parse(readFileSync(path, "utf8")) : fallback;
const names = (path) => existsSync(path) ? readdirSync(path) : [];
const validNumber = (n) => typeof n === "number" && Number.isFinite(n) && n >= 0;
const sum = (xs, key) => xs.reduce((n, x) => n + (validNumber(x[key]) ? x[key] : 0), 0);
const count = (xs, key, allowed) => Object.fromEntries([...allowed, "missing_or_invalid"].map((v) => [v, xs.filter((x) => (allowed.includes(x[key]) ? x[key] : "missing_or_invalid") === v).length]));

export function percentile(values, fraction) {
  if (!values.length) return null;
  const ns = [...values].sort((a, b) => a - b), index = (ns.length - 1) * fraction, low = Math.floor(index);
  return ns[low] + (ns[Math.ceil(index)] - ns[low]) * (index - low);
}

export function accounting(entries, malformedLines = 0) {
  let normalizedUsd = 0, missingUsage = 0, missingPrice = 0, missingUsd = 0;
  let inputTokens = 0, cachedInputTokens = 0, cacheWriteInputTokens = 0, outputTokens = 0, reasoningOutputTokens = 0;
  const latencies = [];
  for (const e of entries) {
    if (!validNumber(e.usd)) missingUsd++;
    const u = e.usage, p = PRICES[e.model];
    if (!p) missingPrice++;
    if (!u || !validNumber(u.inputTokens) || !validNumber(u.outputTokens)) { missingUsage++; continue; }
    const cached = u.cachedInputTokens ?? 0, write = u.cacheWriteInputTokens ?? 0;
    if (!validNumber(cached) || !validNumber(write) || (e.model?.startsWith("gpt-") && cached > u.inputTokens)) { missingUsage++; continue; }
    const totalInput = u.inputTokens + write + (e.model?.startsWith("gpt-") ? 0 : cached);
    inputTokens += totalInput; cachedInputTokens += cached; cacheWriteInputTokens += write; outputTokens += u.outputTokens; reasoningOutputTokens += u.reasoningOutputTokens ?? 0;
    if (p) normalizedUsd += (totalInput * p.input + u.outputTokens * p.output) / 1e6;
    if (e.ok && validNumber(e.ms)) latencies.push(e.ms);
  }
  return { entries: entries.length, successfulCalls: entries.filter((e) => e.ok).length, failedAttempts: entries.filter((e) => !e.ok).length,
    recordedUsd: sum(entries, "usd"), noProviderCacheUsd: missingUsage || missingPrice ? null : normalizedUsd,
    inputTokens, cachedInputTokens, cacheWriteInputTokens, outputTokens, reasoningOutputTokens,
    latencyMs: { count: latencies.length, median: percentile(latencies, 0.5), p10: percentile(latencies, 0.1), p90: percentile(latencies, 0.9), sum: latencies.reduce((a, b) => a + b, 0) },
    missingUsdEntries: missingUsd, missingUsageEntries: missingUsage, missingPriceEntries: missingPrice, malformedLines,
    complete: !missingUsd && !missingUsage && !missingPrice && !malformedLines };
}

function readLedger(path) {
  let malformedLines = 0;
  const entries = [];
  if (existsSync(path)) for (const line of readFileSync(path, "utf8").split("\n").filter(Boolean)) {
    try { entries.push(JSON.parse(line)); } catch { malformedLines++; }
  }
  return { entries, malformedLines };
}
const ledgerOf = (dir) => { const path = join(dir, "state/ledger.jsonl"), ledger = readLedger(path); return { exists: existsSync(path), ...accounting(ledger.entries, ledger.malformedLines) }; };
function ledgerPaths(dir) {
  const out = [];
  if (existsSync(dir)) for (const item of readdirSync(dir, { withFileTypes: true })) {
    if (item.name === "cache" || item.name === "search-cache" || item.isSymbolicLink()) continue;
    const path = join(dir, item.name);
    if (item.isDirectory()) out.push(...ledgerPaths(path)); else if (item.name === "ledger.jsonl") out.push(realpathSync(path));
  }
  return [...new Set(out)];
}
const repetition = (name) => Number(name.match(/-(\d+)$/)?.[1] ?? 1);
const numericMetrics = (value) => Object.fromEntries(Object.entries(value ?? {}).filter(([, v]) => v === null || typeof v === "number" || typeof v === "boolean" || (v && typeof v === "object" && !Array.isArray(v))).map(([k, v]) => [k, v && typeof v === "object" ? numericMetrics(v) : v]));

function extraction(root, plan) {
  const expected = [];
  const models = { terra: "gpt-6.1-terra", sol: "gpt-6.1-sol", luna: "gpt-6-luna" };
  for (const [alias, efforts] of Object.entries(plan.extract ?? {})) if (models[alias] && Array.isArray(efforts)) for (const effort of efforts) {
    const repeats = (alias === "sol" && effort === "low") || (alias === "luna" && effort === "xhigh") ? plan.extract.baselineRepeats ?? 2 : 1;
    for (let repeat = 1; repeat <= repeats; repeat++) expected.push({ name: `map-${alias}-${effort}-${repeat}`, model: models[alias], effort, repeat });
  }
  for (const name of names(root).filter((n) => /^map-.+-\d+$/.test(n))) if (!expected.some((e) => e.name === name)) {
    const cfg = read(join(root, name, "config.json"), {}).map ?? {};
    expected.push({ name, model: cfg.model ?? null, effort: cfg.effort ?? null, repeat: repetition(name) });
  }
  return expected.map((arm) => {
    const dir = join(root, arm.name), marker = read(join(root, arm.name + ".done.json"));
    if (!marker) return { ...arm, status: existsSync(dir) ? "incomplete" : "not_started" };
    const batches = names(join(dir, "map")).filter((n) => /^batch-\d+\.json$/.test(n)).map((n) => read(join(dir, "map", n)));
    const recallName = arm.name.replace(/^map-/, "recall-"), recallDone = existsSync(join(root, recallName + ".done.json"));
    const report = recallDone ? read(join(root, recallName + ".json")) : null, recall = report?.runs?.[arm.name];
    const sourceCounts = Object.values(recall?.bySource ?? {}), gold = sum(sourceCounts, "gold"), found = sum(sourceCounts, "found");
    const cfg = read(join(dir, "config.json"), {}).map ?? {};
    const accounts = ledgerOf(dir);
    return { ...arm, model: cfg.model ?? arm.model, effort: cfg.effort ?? arm.effort, status: "complete", batches: batches.length, failedBatches: batches.filter((b) => !b.ok).length,
      repairedBatches: batches.filter((b) => b.repaired > 0).length, repairs: sum(batches, "repaired"), droppedCandidates: sum(batches, "dropped"), candidates: batches.reduce((n, b) => n + (b.candidates?.length ?? 0), 0),
      wallMs: marker.ms ?? null, accounting: accounts,
      recall: recall ? { status: "complete", matcher: report.matcher, matcherOk: recall.matcherOk, gold, expectedGold: GOLD, denominatorMatches: gold === GOLD && report.gold === GOLD,
        found, missed: recall.missed?.length ?? null, countsConsistent: found + (recall.missed?.length ?? 0) === gold,
        matcherCostExact: recall.matcherCostExact ?? null, matcherCostRounded: recall.matcherCost ?? null }
        : { status: recallDone ? "missing_result" : "not_completed" } };
  });
}

function verifiers(root) {
  const rejectReasons = ["false_now", "already_documented", "obvious_from_code", "generic", "preference", "one_off", "plan_not_done", "conflicts_with_policy", "too_vague", "not_repository_specific", "other"];
  return readdirSync(root, { withFileTypes: true }).filter((e) => e.isDirectory() && /^verify-(pilot|full)-/.test(e.name)).map((e) => e.name).map((name) => {
    const dir = join(root, name), cfg = read(join(dir, "config.json"), {}), summary = read(join(dir, "summary.json"));
    const files = names(dir).filter((n) => /^g\d+\.json$/.test(n)), cases = files.map((n) => read(join(dir, n))), decisions = cases.map((c) => c.result ?? {});
    const expected = new Set(cfg.ids ?? []), ids = cases.map((c) => c.cluster), completeIds = ids.length === expected.size && new Set(ids).size === ids.length && ids.every((id) => expected.has(id));
    const valid = cases.filter((c) => c.result?.decision === "propose" && Array.isArray(c.contractProblems) && !c.contractProblems.length);
    return { name, model: cfg.model ?? null, effort: cfg.effort ?? null, repeat: Number(cfg.repeat ?? repetition(name)), phase: name.startsWith("verify-pilot-") ? "pilot" : "full", status: summary && completeIds ? "complete" : "incomplete",
      expectedCases: expected.size, recordedCases: cases.length, completeIds, validProposals: valid.length,
      contractInvalidCases: cases.filter((c) => (c.contractProblems?.length ?? 0) > 0).length,
      decisions: count(decisions, "decision", ["propose", "reject", "need_evidence"]), rejectReasons: count(decisions.filter((r) => r.decision === "reject"), "rejectReason", rejectReasons),
      proposalResponses: count(valid.map((c) => c.result), "response", ["remember", "document", "fix", "guard", "ask"]),
      responseReasonsPresent: valid.filter((c) => typeof c.result.responseReason === "string" && c.result.responseReason.trim()).length,
      verificationEvidenceEntries: cases.reduce((n, c) => n + (Array.isArray(c.result?.verification) ? c.result.verification.length : 0), 0),
      summaryCounts: summary ? { cases: summary.cases, proposed: summary.proposed, invalid: summary.invalid, recoveredHistorical: summary.recoveredHistorical, historicalGoldInSample: summary.historicalGoldInSample,
        historicalBinaryAgreement: summary.historicalBinaryAgreement, searchFailures: summary.searchFailures } : null,
      wallMs: summary?.wallMs ?? null, accounting: ledgerOf(dir) };
  });
}

function roles(root) {
  const base = join(root, "roles");
  return names(base).filter((name) => /^gpt-/.test(name)).map((name) => {
    const dir = join(base, name), result = read(join(dir, "result.json"));
    if (!result) return { name, status: "incomplete" };
    return { name, status: "complete", model: result.binding?.model, effort: result.binding?.effort, repeat: result.repeat, metrics: numericMetrics(result.metrics),
      accounting: ledgerOf(dir), auditAccounting: ledgerOf(join(dir, "audit")) };
  });
}

export function summarize(root = ROOT) {
  const plan = read(join(root, "plan.json"), {}), spend = read(join(root, "spend.json")), paths = ledgerPaths(root), entries = [], malformed = [];
  for (const path of paths) {
    const ledger = readLedger(path); entries.push(...ledger.entries);
    if (ledger.malformedLines) malformed.push({ path: relative(root, path), lines: ledger.malformedLines });
  }
  const totals = accounting(entries, sum(malformed, "lines")), extractionArms = extraction(root, plan), verifierArms = verifiers(root), roleArms = roles(root);
  const reference = read(join(root, "roles/reference.json"));
  const limitations = ["Latencies use successful paid ledger calls and linear-interpolated percentiles; local cache replays are not measured as new calls.", "No-provider-cache pricing charges every input token at the published fresh-input rate; reasoning tokens already belong to output tokens.", "Historical verifier agreement and recovered historical proposals describe differences from an earlier run, not correctness.", "This is a live snapshot; incomplete arms have no final extraction quality metrics, and reserved calls may finish after the snapshot.", "Wall time is the completion marker or verifier summary invocation duration. A resumed invocation may omit time spent before it restarted."];
  if (!paths.some((p) => p.includes("/matcher/"))) limitations.push("No campaign matcher ledger exists yet; scoring spend will appear when scoring completes.");
  if (!verifierArms.length) limitations.push("No verification runs are present.");
  if (!roleArms.length) limitations.push("No match/synthesis runs are present.");
  return { at: new Date().toISOString(), extraction: extractionArms, verification: verifierArms, roles: roleArms,
    roleReference: reference ? { status: "complete", pairs: reference.pairs?.length ?? 0, verdicts: count(reference.pairs ?? [], "verdict", ["same", "distinct", "ambiguous"]), matchVerdicts: count(reference.pairs ?? [], "matchVerdict", ["match", "no_match", "ambiguous"]), currentProtocolTally: reference.tally, groupTally: reference.groupTally, matchTally: reference.matchTally, allReferenceAttemptsAccounting: ledgerOf(join(root, "roles/reference")) } : { status: "not_completed" },
    matcherAccounting: ledgerOf(join(root, "matcher")),
    campaign: { approvedCapUsd: plan.capUsd ?? null, authoritativeSpend: spend ? { usd: spend.usd, uncertainUsd: spend.uncertainUsd ?? 0, reservedUsd: spend.reserved ?? 0, calls: spend.calls, failed: spend.failed ?? 0, at: spend.at } : null,
      ledgerFiles: paths.length, ledgerAccounting: totals, malformedLedgers: malformed,
      authoritativeMinusLedgerUsd: spend ? spend.usd - totals.recordedUsd : null, authoritativeCallsMinusLedgerEntries: spend ? spend.calls - entries.length : null,
      accountingReconciled: Boolean(spend && totals.complete && Math.abs(spend.usd - totals.recordedUsd) < 1e-8 && spend.calls === entries.length),
      missingAccounting: { extractionArms: extractionArms.filter((a) => a.status === "complete" && !a.accounting.exists).length, verificationArms: verifierArms.filter((a) => a.recordedCases && !a.accounting.exists).length,
        roleArms: roleArms.filter((a) => a.status === "complete" && !a.accounting.exists).length } }, limitations };
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const summary = summarize();
  writeFileSync(join(ROOT, "summary.json"), JSON.stringify(summary, null, 2) + "\n", { mode: 0o600 });
  console.log(JSON.stringify(summary));
}
