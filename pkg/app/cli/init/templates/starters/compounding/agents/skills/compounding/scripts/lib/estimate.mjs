// Rough cost of a run before it spends anything. Sizes come from the D runs
// in the effort log; Jev passages are counted exactly, minus cached answers.

import { costOf, itemKey } from "./calls.mjs";
import { BATCH_CHARS, episodeView } from "./extract.mjs";
import { CHUNK } from "./reduce.mjs";
import { SHARE, screenItems } from "./screen.mjs";

// On D, Haiku 5.5 xhigh cost about $0.036 per 45,000-character batch (18k
// input, about 65k output tokens with thinking); the 30% passage gate kept
// 44% of episodes and extraction found one candidate per 900 kept characters;
// packet verification averaged 1.5 rounds of about 20k input and 1k output.
const SIZES = {
  extract: { input: 18000, output: { xhigh: 65000, max: 65000, high: 35000 }, outputDefault: 15000 },
  group: { perCandidate: 110, perExisting: 60, outPerCandidate: 180 },
  verify: { input: 20000, output: 1200, rounds: 1.5 },
  jev: { input: 350 },
  keptEpisodes: 0.44,
  charsPerCandidate: 900,
  clustersPerCandidate: 0.7,
  tierAShare: 0.08,
};

// Claude writes each one-shot prompt to the five-minute cache.
export function callCost(b, input, output) {
  if (!b) return 0;
  const usage = b.provider === "claude" ? { cacheWriteInputTokens: input, outputTokens: output } : { inputTokens: input, outputTokens: output };
  return costOf(b.model, usage) ?? 0;
}

// Per-call reservations against the budget.
export function callEstimates(bindings) {
  const ex = bindings.extract;
  const exOut = SIZES.extract.output[ex?.effort] ?? SIZES.extract.outputDefault;
  return {
    extract: callCost(ex, SIZES.extract.input, exOut),
    repair: callCost(ex, 3000, 2000),
    group: (candidates, existing) => callCost(bindings.synthesize, 1000 + candidates * SIZES.group.perCandidate + existing * SIZES.group.perExisting, candidates * SIZES.group.outPerCandidate),
    verify: callCost(bindings.verify, SIZES.verify.input, SIZES.verify.output),
    jev: callCost(bindings.decide, SIZES.jev.input, 0),
  };
}

export function estimate(run, episodes, store) {
  const b = run.bindings;
  const per = callEstimates(b);
  const chars = episodes.reduce((t, e) => t + e.text.length, 0);
  const items = run.unscreened ? [] : screenItems(episodes);
  const jevNew = items.filter((item) => !store.cacheGet(itemKey({ stage: "screen", model: b.decide.model, item }))).length;
  const gatedChars = run.unscreened ? chars : chars * SHARE;
  // Batches are filled by each episode's view, which adds its context to the text.
  const overhead = episodes.reduce((t, e) => t + JSON.stringify(episodeView(e)).length - e.text.length, 0);
  const batches = Math.ceil((gatedChars + overhead * (run.unscreened ? 1 : SIZES.keptEpisodes)) / (BATCH_CHARS * 0.9));
  const candidates = batches && Math.max(1, Math.round(gatedChars / SIZES.charsPerCandidate));
  const clusters = candidates && Math.max(1, Math.round(candidates * SIZES.clustersPerCandidate));
  const verified = Math.round(clusters * SIZES.tierAShare) + Math.min(run.verifyBudget, Math.round(clusters * (1 - SIZES.tierAShare))) + (run.verifyHeld ?? 0);
  const chunks = Math.ceil(candidates / CHUNK);
  const exOut = SIZES.extract.output[b.extract?.effort] ?? SIZES.extract.outputDefault;
  const jev = jevNew + (run.unscreened ? 0 : clusters);
  const label = (x) => `${x.provider}:${x.model}${x.effort ? `:${x.effort}` : ""}${x.choice === "fallback" || x.unmeasured ? ` (${x.choice}${x.unmeasured ? ", unmeasured" : ""})` : ""}`;
  const rows = [
    { role: "decide", calls: jev, unit: "Jev items", inputTokens: jev * SIZES.jev.input, outputTokens: 0, usd: jev * per.jev },
    { role: "extract", calls: batches, inputTokens: batches * SIZES.extract.input, outputTokens: batches * exOut, usd: batches * per.extract },
    { role: "synthesize", calls: chunks, inputTokens: candidates * SIZES.group.perCandidate + chunks * 1000, outputTokens: candidates * SIZES.group.outPerCandidate, usd: chunks ? per.group(candidates / chunks, 0) * chunks : 0 },
    { role: "verify", calls: Math.round(verified * SIZES.verify.rounds), inputTokens: Math.round(verified * SIZES.verify.rounds * SIZES.verify.input), outputTokens: Math.round(verified * SIZES.verify.rounds * SIZES.verify.output), usd: verified * SIZES.verify.rounds * per.verify },
  ].map((r) => ({ ...r, binding: b[r.role] ? label(b[r.role]) : "none", priced: !b[r.role] || costOf(b[r.role].model, {}) !== null }));
  const transcriptsTo = [...new Set(["extract", "synthesize", "verify"].map((r) => b[r]?.provider).filter(Boolean))];
  if (!run.unscreened) transcriptsTo.push("typesafe (passages only)");
  return { episodes: episodes.length, chars, rows, usd: +rows.reduce((t, r) => t + r.usd, 0).toFixed(2), transcriptsTo, skipped: Object.entries(b).flatMap(([role, x]) => (x?.skipped ?? []).map((s) => ({ role, ...s }))) };
}

export function formatEstimate(est) {
  const n = (v) => (v >= 1e6 ? `${(v / 1e6).toFixed(1)}M` : v >= 1e3 ? `${Math.round(v / 1e3)}k` : String(Math.round(v)));
  const lines = [`estimate: ${est.episodes} new episodes, ${n(est.chars)} characters`];
  for (const r of est.rows) lines.push(`  ${r.role.padEnd(11)}${r.binding.padEnd(38)}${String(r.calls).padStart(6)} ${r.unit ?? "calls"}  ${n(r.inputTokens).padStart(6)} in ${n(r.outputTokens).padStart(6)} out  ${r.priced ? `~$${r.usd.toFixed(2)}` : "price unknown"}`);
  for (const s of est.skipped) lines.push(`  skipped for ${s.role}: ${s.model} (${s.reason})`);
  lines.push(`  total ~$${est.usd.toFixed(2)}; transcript text goes to ${est.transcriptsTo.join(", ")}`);
  return lines.join("\n");
}
