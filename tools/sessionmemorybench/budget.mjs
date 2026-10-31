// One campaign process at a time. Persisted spend includes every stage and
// matcher invocation; reservations cover concurrent calls before they start.
import { existsSync, readFileSync, writeFileSync, renameSync, mkdirSync } from "node:fs";
import { dirname } from "node:path";

export function campaignBudget({ file, limit, prices, costOf, Stop }) {
  if (!file) return null;
  if (!(limit > 0) || !Number.isFinite(limit)) throw new Error("SM_BENCH_BUDGET_USD must be positive");
  const state = existsSync(file) ? JSON.parse(readFileSync(file, "utf8")) : { usd: 0, calls: 0 };
  // A crashed process cannot prove whether its outstanding calls were billed.
  state.usd += Math.max(0, state.reserved ?? 0);
  state.uncertainUsd = Math.max(0, state.uncertainUsd ?? 0) + Math.max(0, state.reserved ?? 0);
  let reserved = 0;
  const measured = (result, model) => {
    const u = result?.usage;
    return result?.model === model && u && [u.inputTokens, u.outputTokens, u.cachedInputTokens ?? 0, u.cacheWriteInputTokens ?? 0].every((n) => Number.isFinite(n) && n >= 0) && [u.inputTokens, u.cachedInputTokens, u.cacheWriteInputTokens].some((n) => n > 0) && (!model.startsWith("gpt-") || (u.cachedInputTokens ?? 0) <= u.inputTokens);
  };
  const save = () => {
    try {
      mkdirSync(dirname(file), { recursive: true, mode: 0o700 });
      writeFileSync(file + ".tmp", JSON.stringify({ ...state, reserved, limit, at: new Date().toISOString() }) + "\n", { mode: 0o600 });
      renameSync(file + ".tmp", file);
    } catch (err) {
      throw new Stop("budget", `campaign accounting could not be saved: ${err.message}`);
    }
  };
  return async (binding, prompt, work) => {
    const p = prices[binding.model];
    if (!p) throw new Stop("binding", `no price for ${binding.model}`);
    // All compared models have at most 128k output tokens. Byte count bounds
    // prompt tokens, plus headroom for the CLI's measured 1.3k-token overhead.
    const reserve = ((Buffer.byteLength(prompt) + 32000) * Math.max(p.input, p.write5m ?? 0) + 128000 * p.output) / 1e6;
    if (state.usd + reserved + reserve > limit) throw new Stop("budget", `campaign cap $${limit}: $${state.usd.toFixed(4)} spent, $${reserved.toFixed(4)} reserved`);
    reserved += reserve;
    save();
    let result;
    try {
      result = await work();
      if (!measured(result, binding.model)) throw new Stop("binding", "model response has unusable accounting; its full reservation is charged pending reconciliation");
      return result;
    } finally {
      reserved = Math.max(0, reserved - reserve);
      const u = result?.usage;
      const known = measured(result, binding.model);
      const usd = known ? Math.max(costOf(result.model, u) ?? 0, u.costUsd ?? 0) : reserve;
      state.usd += usd;
      if (!known) state.uncertainUsd += reserve;
      state.calls++;
      state.failed = (state.failed ?? 0) + (result ? 0 : 1);
      save();
    }
  };
}
