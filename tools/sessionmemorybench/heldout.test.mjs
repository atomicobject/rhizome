import assert from "node:assert/strict";
import { test } from "node:test";
import { validateFreeze, validateGold } from "./heldout.mjs";

const freeze = { status: "frozen", providers: ["codex", "claude"], extract: [{ name: "terra-max-1", model: "gpt-6.1-terra", effort: "max" }] };

test("confirmation requires frozen bindings to the approved providers", () => {
  assert.doesNotThrow(() => validateFreeze(freeze));
  assert.throws(() => validateFreeze({ ...freeze, status: "draft" }));
  assert.throws(() => validateFreeze({ ...freeze, providers: ["codex"] }));
  assert.throws(() => validateFreeze({ ...freeze, extract: [...freeze.extract, ...freeze.extract] }));
  assert.throws(() => validateFreeze({ ...freeze, extract: [{ ...freeze.extract[0], model: "unapproved-model" }] }));
});

test("full-window recall requires 208 populated gold statements and more episodes than gating", () => {
  const full = Array.from({ length: 209 }, (_, i) => ({ id: `e${i}` })), gated = full.slice(0, 20);
  const labels = { learnings: Array.from({ length: 208 }, (_, i) => ({ id: `g${i}`, level: "strong", evidence: [`e${i}`] })) };
  const statements = new Map(labels.learnings.map((l) => [l.id, "Synthetic operational statement"]));
  assert.equal(validateGold(full, gated, labels, statements).length, 208);
  assert.throws(() => validateGold(gated, gated, labels, statements));
  assert.throws(() => validateGold(full, [{ id: "foreign" }], labels, statements));
  assert.throws(() => validateGold(full, gated, { learnings: labels.learnings.slice(1) }, statements));
  statements.delete("g50");
  assert.throws(() => validateGold(full, gated, labels, statements));
});
