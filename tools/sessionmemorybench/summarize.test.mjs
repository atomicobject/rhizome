import assert from "node:assert/strict";
import { test } from "node:test";
import { accounting, percentile } from "./summarize.mjs";

test("cost normalization removes provider caching without counting reasoning output twice", () => {
  const result = accounting([
    { ok: true, model: "gpt-6.1-terra", usd: 0.0093, ms: 100, usage: { inputTokens: 10000, cachedInputTokens: 6000, outputTokens: 1000, reasoningOutputTokens: 700 }, output: "PRIVATE_PAYLOAD" },
    { ok: true, model: "claude-sonnet-5-5", usd: 0.0236, ms: 300, usage: { inputTokens: 4000, cachedInputTokens: 6000, cacheWriteInputTokens: 2000, outputTokens: 1000 } },
  ]);
  assert.equal(result.inputTokens, 22000);
  assert.equal(result.cachedInputTokens, 12000);
  assert.equal(result.outputTokens, 2000);
  assert.equal(result.reasoningOutputTokens, 700);
  assert.ok(Math.abs(result.noProviderCacheUsd - 0.049) < 1e-12);
  assert.ok(Math.abs(result.recordedUsd - 0.0329) < 1e-12);
  assert.equal(result.complete, true);
  assert.equal(JSON.stringify(result).includes("PRIVATE_PAYLOAD"), false);
});

test("missing or malformed accounting stays visible rather than becoming a zero-cost success", () => {
  const result = accounting([{ ok: false, model: "gpt-6.1-terra" }], 1);
  assert.equal(result.complete, false);
  assert.equal(result.noProviderCacheUsd, null);
  assert.equal(result.failedAttempts, 1);
  assert.equal(result.missingUsdEntries, 1);
  assert.equal(result.missingUsageEntries, 1);
  assert.equal(result.malformedLines, 1);
  assert.equal(result.latencyMs.median, null);
});

test("percentiles interpolate the same paid latency sample", () => {
  assert.equal(percentile([400, 100, 200, 300], 0.5), 250);
  assert.equal(percentile([400, 100, 200, 300], 0.1), 130);
  assert.equal(percentile([400, 100, 200, 300], 0.9), 370);
  assert.equal(percentile([], 0.5), null);
});
