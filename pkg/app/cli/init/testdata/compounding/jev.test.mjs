import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

const moduleRoot = new URL("../../templates/starters/compounding/module/", import.meta.url);
const { mergeProfile, resolve } = await import(new URL("scripts/lib/models.mjs", moduleRoot));
const { costOf, makeCaller } = await import(new URL("scripts/lib/calls.mjs", moduleRoot));
const { callEstimates, estimate } = await import(new URL("scripts/lib/estimate.mjs", moduleRoot));
const shipped = JSON.parse(readFileSync(new URL("models.json", moduleRoot), "utf8"));

test("Jev resolves the shipped alias and preserves an explicit repository pin without probing", async () => {
  for (const pin of [null, "jev-1.13.0"]) {
    const local = pin && { roles: { decide: [{ provider: "typesafe", model: pin, effort: "" }] } };
    const profile = mergeProfile(shipped, local);
    profile.roles = { decide: profile.roles.decide };
    const { bindings } = await resolve(profile, {
      providers: ["codex"], statuses: [], probe: () => assert.fail("Jev needs no generation probe"),
    });
    assert.equal(bindings.decide.model, pin ?? "jev-latest");
    assert.equal(bindings.decide.requested, pin ?? "jev-latest");
    assert.equal(bindings.decide.choice, pin ? "local" : "default");
  }
});

test("Jev alias is priced in mining estimates and reserves enough to stop another batch", async () => {
  const bindings = {
    decide: { provider: "typesafe", model: "jev-latest", effort: "" },
    extract: { provider: "codex", model: "gpt-6.1-terra", effort: "xhigh" },
  };
  const unit = callEstimates(bindings).jev;
  assert.equal(unit, 350 * 0.042 / 1e6);
  assert.equal(costOf("jev-latest", { inputTokens: 1000, outputTokens: 123 }), 0.000042);
  assert.equal(costOf("jev-1.13.0", { inputTokens: 1000, outputTokens: 123 }), 0.000042);
  assert.equal(costOf("unknown-model", {}), null);
  const store = { cacheGet: () => null, cachePut: () => {}, ledger: (row) => ledger.push(row) };
  const projected = estimate({ bindings, verifyBudget: 0 }, [], store).rows.find((r) => r.role === "decide");
  assert.equal(projected.priced, true);

  const ledger = [];
  const requests = [];
  const spend = { usd: 0, byRole: {} };
  const caller = makeCaller({
    clients: { evaluateBatch: async (request) => {
      requests.push(request);
      return {
        usage: { input_tokens: 1000, output_tokens: 123 },
        items: request.items.map(({ id }) => ({ id, status: "succeeded", response: { model: "jev-1.14.0", answers: { q: true } } })),
      };
    } },
    store, run: "alias-budget", bindings, budget: 0.00005, spend,
  });
  const result = await caller.evaluate({ stage: "screen", items: [{ id: "one", state: "4", questions: { q: { type: "noul" } } }], estimatePerItem: unit });
  assert.deepEqual(result.get("one"), { q: true });
  assert.equal(requests[0].items[0].model, "jev-latest");
  assert.equal(spend.usd, 0.000042);
  assert.equal(ledger[0].usd, 0.000042);
  await assert.rejects(caller.evaluate({ stage: "rank", items: [{ id: "two", state: "5", questions: { q: { type: "noul" } } }], estimatePerItem: unit }), (err) => err.reason === "budget");
  assert.equal(requests.length, 1);
});
