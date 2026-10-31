import assert from "node:assert/strict";
import { test } from "node:test";
import { compareCases, stateOf } from "./verify-reference.mjs";

const item = (cluster, decision, contractProblems = []) => ({ cluster, result: { decision }, contractProblems });

test("only contract-valid proposals count as proposals", () => {
  assert.equal(stateOf(item("a", "propose")), "propose");
  assert.equal(stateOf(item("b", "propose", ["missing required response"])), "invalid_proposal");
  assert.equal(stateOf({ result: { decision: "propose" } }), "invalid_proposal");
  assert.equal(stateOf(item("c", "need_evidence")), "need_evidence");
  assert.equal(stateOf({}), "ambiguous");
});

test("relative errors separate uncertain references and unavailable candidate decisions", () => {
  const reference = [item("a", "propose"), item("b", "reject"), item("c", "need_evidence"), item("d", "propose"), item("e", "ambiguous"), item("f", "propose", ["invalid"]), item("g", "propose")];
  const candidates = [item("a", "reject"), item("b", "propose"), item("c", "propose"), item("d", "propose", ["invalid"]), item("e", "reject"), item("f", "reject"), item("g", "need_evidence")];
  const result = compareCases(reference, candidates);
  assert.equal(result.cases, 7);
  assert.equal(result.compared, 3);
  assert.equal(result.excluded, 4);
  assert.equal(result.relativeFalsePositive, 1);
  assert.equal(result.relativeFalseNegative, 2);
  assert.deepEqual(result.relativeFalsePositiveIds, ["b"]);
  assert.deepEqual(result.relativeFalseNegativeIds, ["a", "d"]);
  assert.deepEqual(result.excludedIds, ["c", "e", "f", "g"]);
  assert.equal(result.referenceStates.need_evidence, 1);
  assert.equal(result.candidateStates.invalid_proposal, 1);
});

test("matched reference comparisons reject missing, duplicate, or foreign case IDs", () => {
  const reference = [item("a", "propose"), item("b", "reject")];
  assert.throws(() => compareCases(reference, [item("a", "propose")]));
  assert.throws(() => compareCases(reference, [item("a", "propose"), item("a", "reject")]));
  assert.throws(() => compareCases(reference, [item("a", "propose"), item("foreign", "reject")]));
  const result = compareCases(reference, [item("b", "reject"), item("a", "propose")]);
  assert.equal(result.relativeTruePositive, 1);
  assert.equal(result.relativeTrueNegative, 1);
});
