import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { after, test } from "node:test";
import { campaignBudget } from "./budget.mjs";
import { calls } from "./shipped.mjs";

const root = mkdtempSync(join(tmpdir(), "sm-budget-test-"));
after(() => execFileSync("trash", [root]));
const budget = (name, limit) => campaignBudget({ file: join(root, name), limit, prices: calls.PRICES, costOf: calls.costOf, Stop: calls.Stop });
const binding = { model: "gpt-6.1-terra" };

test("Terra pricing subtracts cache reads from total input and prices output once", () => {
  assert.equal(calls.costOf(binding.model, { inputTokens: 1000000, cachedInputTokens: 200000, outputTokens: 100000 }), 1.31);
});

test("campaign persists spend, resumes, and reserves concurrent calls", async () => {
  const run = budget("spend.json", 1);
  let release;
  const pending = run(binding, "test", () => new Promise((resolve) => { release = resolve; }));
  await assert.rejects(run(binding, "test", () => assert.fail("over cap call started")), { reason: "budget" });
  release({ model: binding.model, usage: { inputTokens: 100000, outputTokens: 10000 } });
  await pending;
  assert.equal(JSON.parse(readFileSync(join(root, "spend.json"))).usd, 0.15);
  await assert.rejects(budget("spend.json", 0.8)(binding, "test", () => assert.fail("resumed over cap call started")), { reason: "budget" });
});

test("unknown prices stop before a model call", async () => {
  await assert.rejects(budget("unknown.json", 75)({ model: "unpriced-model" }, "test", () => assert.fail("unpriced call started")), { reason: "binding" });
});

test("unknown-charge failures consume the reservation and survive restart", async () => {
  const run = budget("failed.json", 1);
  await assert.rejects(run(binding, "test", async () => { throw new Error("lost response"); }), /lost response/);
  const state = JSON.parse(readFileSync(join(root, "failed.json")));
  assert.ok(state.uncertainUsd > 0.67);
  assert.equal(state.usd, state.uncertainUsd);
  await assert.rejects(budget("failed.json", 1)(binding, "test", () => assert.fail("uncertain paid call retried beyond cap")), { reason: "budget" });
});

test("missing usage and unknown returned models are conservatively charged", async () => {
  for (const result of [{ model: binding.model }, { model: "unknown-model", usage: { inputTokens: 1, outputTokens: 1 } }, { model: "invalid-usage", usage: { inputTokens: -1, outputTokens: -1 } }]) {
    const file = result.model + ".json";
    await assert.rejects(budget(file, 1)(binding, "test", async () => result), { reason: "binding" });
    assert.ok(JSON.parse(readFileSync(join(root, file))).uncertainUsd > 0.67);
  }
});

test("a killed process's reservations count against the resumed cap", async () => {
  writeFileSync(join(root, "crashed.json"), JSON.stringify({ usd: 0.1, reserved: 0.7, calls: 0 }));
  await assert.rejects(budget("crashed.json", 1)(binding, "test", () => assert.fail("crashed paid call forgotten")), { reason: "budget" });
});

test("an accounting write failure stops before inference", async () => {
  const file = "blocked";
  writeFileSync(join(root, file), "{}");
  const run = campaignBudget({ file: join(root, file, "spend.json"), limit: 1, prices: calls.PRICES, costOf: calls.costOf, Stop: calls.Stop });
  await assert.rejects(run(binding, "test", () => assert.fail("call started without durable reservation")), { reason: "budget" });
});

test("the shipped caller propagates budget stops without retrying", async () => {
  let count = 0;
  const caller = calls.makeCaller({ clients: { generate: async () => { count++; throw new calls.Stop("budget", "cap"); } }, store: { cacheGet: () => null }, run: "test", bindings: { extract: binding }, spend: { usd: 0, byRole: {} } });
  await assert.rejects(caller.generate({ role: "extract", stage: "extract", prompt: "test" }), { reason: "budget" });
  assert.equal(count, 1);
});
