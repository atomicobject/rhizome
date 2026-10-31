import assert from "node:assert/strict";
import { test } from "node:test";
import { auditBatches, sampleCandidates, validateVerdicts } from "./audit-extraction.mjs";

const batches = Array.from({ length: 4 }, (_, i) => ({ id: `batch-${i}`, candidates: Array.from({ length: 3 }, (_, j) => ({ id: `c-${i}-${j}`, strength: j ? "strong" : "weak" })) }));

test("candidate sampling balances batches without favoring strength labels", () => {
  const selected = sampleCandidates(batches, 6, "fixed");
  assert.equal(new Set(selected.slice(0, 4).map((s) => s.batch)).size, 4);
  assert.equal(new Set(selected.map((s) => s.candidate.id)).size, 6);
  const reversed = batches.toReversed().map((b) => ({ ...b, candidates: b.candidates.toReversed().map((c) => ({ ...c, strength: c.strength === "strong" ? "weak" : "strong" })) }));
  assert.deepEqual(selected.map((s) => s.candidate.id), sampleCandidates(reversed, 6, "fixed").map((s) => s.candidate.id));
  assert.throws(() => sampleCandidates(batches, 13, "fixed"));
});

test("packing preserves full source text even when a single item exceeds the target", () => {
  const items = [{ id: "small", source: "context" }, { id: "large", source: "x".repeat(46000) }, { id: "last", source: "more context" }];
  const packed = auditBatches(items);
  assert.deepEqual(packed, [[items[0]], [items[1]], [items[2]]]);
  assert.deepEqual(packed.flat(), items);
});

test("source-fidelity judgments must cover every item exactly once with concrete reasons", () => {
  const verdicts = [{ id: "a", verdict: "supported", reason: "Episode one explicitly requires this." }, { id: "b", verdict: "unsupported", reason: "Episode two records an agent suggestion without user acceptance." }];
  assert.doesNotThrow(() => validateVerdicts(verdicts, ["a", "b"]));
  assert.throws(() => validateVerdicts(verdicts.slice(0, 1), ["a", "b"]));
  assert.throws(() => validateVerdicts([verdicts[0], verdicts[0]], ["a", "b"]));
  assert.throws(() => validateVerdicts([{ ...verdicts[0], reason: "" }], ["a"]));
  assert.throws(() => validateVerdicts([{ ...verdicts[0], verdict: "useful" }], ["a"]));
});
