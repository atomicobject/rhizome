import assert from "node:assert/strict";
import { test } from "node:test";
import { buildFixture, exactIds, scorePairs, scoreMatchPairs, RoleContractError, captureContractFailure, groupDiagnostics, validateFinalGroups, validateMatches, roleMetrics, referenceItems, membershipIndex, auditItems } from "./roles.mjs";

const candidate = (id, statement) => ({ id, kind: "fact", statement, appliesTo: [], commands: [] });

test("fixture selection is deterministic, includes close distinct candidates, and excludes ambiguous historical memberships", () => {
  const candidates = [candidate("a", "Serialize database writes"), candidate("b", "Database writes must serialize"), candidate("c", "Serialize database reads"), candidate("d", "Database reads must serialize"), candidate("shared", "Serialize operations")];
  const clusters = [{ id: "one", members: ["a", "b", "shared"] }, { id: "two", members: ["c", "d", "shared"] }];
  const fixture = buildFixture(candidates, clusters);
  assert.deepEqual(fixture, buildFixture([...candidates].reverse(), [...clusters].reverse()));
  assert.deepEqual(fixture.candidates.map((c) => c.id), ["a", "b", "c", "d"]);
  assert.ok(fixture.pairs.some((p) => p.selection === "historical-same-cluster"));
  assert.ok(fixture.pairs.some((p) => p.selection === "lexically-close-other-cluster"));
  assert.ok(fixture.pairs.every((p) => p.a !== "shared" && p.b !== "shared"));
});

test("pair metrics report false positives and false negatives separately and exclude uncertain references", () => {
  const references = ["same", "same", "distinct", "distinct", "ambiguous"].map((verdict, i) => ({ id: String(i), verdict }));
  const metrics = scorePairs(references, [true, false, true, false, true].map((same, i) => ({ id: String(i), same })));
  assert.deepEqual(metrics, { truePositive: 1, falsePositive: 1, trueNegative: 1, falseNegative: 1, ambiguous: 1, disagreements: ["1", "2"], positive: 2, negative: 2, falsePositiveRate: 0.5, falseNegativeRate: 0.5 });
  assert.equal(scorePairs([{ id: "a", verdict: "same" }], [{ id: "a", same: true }]).falsePositiveRate, null);
});

test("incomplete, duplicated, and unknown IDs fail rather than inflate scores", () => {
  for (const entries of [[{ id: "a" }], [{ id: "a" }, { id: "a" }], [{ id: "a" }, { id: "other" }]]) assert.throws(() => exactIds(entries, ["a", "b"]));
  assert.doesNotThrow(() => exactIds([{ candidate: "b" }, { candidate: "a" }], ["a", "b"], "candidate"));
  assert.throws(() => buildFixture([candidate("a", "One")], [{ id: "missing", members: ["a", "unknown"] }]));
});


test("bundled learnings can share an atomic cluster while directional matching differs", () => {
  const reference = [
    { id: "stronger", candidate: "Serialize database writes and preserve backup files", label: "Serialize database writes", verdict: "same", matchVerdict: "match" },
    { id: "reverse", candidate: "Serialize database writes", label: "Serialize database writes and preserve backup files", verdict: "same", matchVerdict: "no_match" },
  ];
  const matching = scoreMatchPairs(reference, [{ id: "stronger", same: true }, { id: "reverse", same: false }]);
  assert.equal(matching.truePositive, 1);
  assert.equal(matching.trueNegative, 1);
  assert.equal(matching.falsePositive, 0);
  assert.equal(matching.falseNegative, 0);
  const grouping = scorePairs(reference, [{ id: "stronger", same: true }, { id: "reverse", same: true }]);
  assert.equal(grouping.truePositive, 2);
  assert.equal(grouping.falsePositive, 0);
  assert.equal(scorePairs(reference, [{ id: "stronger", same: true }, { id: "reverse", same: false }]).falseNegative, 1);
  assert.throws(() => scoreMatchPairs([{ id: "missing", verdict: "same" }], [{ id: "missing", same: true }]));
});


test("raw duplicate/unknown members are diagnosed while the shipped reducer normalizes harmless cases", async () => {
  const { group } = await import("../../pkg/app/cli/init/templates/starters/compounding/agents/skills/compounding/scripts/lib/reduce.mjs");
  const output = { clusters: [{ members: ["a", "a", "foreign"] }] };
  assert.deepEqual(groupDiagnostics(output, ["a", "b"]), { unknownMembers: 1, intraClusterDuplicates: 1, crossClusterMemberships: 0, missingMembers: ["b"] });
  const candidates = { a: candidate("a", "One learning"), b: candidate("b", "Another learning") };
  const clusters = await group({ generate: async () => output }, { candidates, clusters: {}, fresh: ["a", "b"], firstSeen: (id) => id === "a" ? 0 : 1, save: () => {} });
  assert.doesNotThrow(() => validateFinalGroups(clusters, ["a", "b"]));
  const duplicate = await group({ generate: async () => ({ clusters: [{ members: ["a", "b"] }, { members: ["b"] }] }) }, { candidates, clusters: {}, fresh: ["a", "b"], firstSeen: (id) => id === "a" ? 0 : 1, save: () => {} });
  assert.doesNotThrow(() => validateFinalGroups(duplicate, ["a", "b"]));
  assert.equal(membershipIndex(duplicate).get("b").size, 2);
  assert.throws(() => validateFinalGroups({ k: { id: "k", members: ["a", "foreign"] } }, ["a", "b"]), { code: "group_unknown_final_member" });
  assert.throws(() => validateFinalGroups({ k: { id: "k", members: ["a"] } }, ["a", "b"]), { code: "group_incomplete_final_coverage" });
});

test("matcher rejects unknown/duplicated candidate IDs and foreign labels without repairing answers", () => {
  const asked = [{ candidate: "p1", labels: [{ id: "b" }] }, { candidate: "p2", labels: [{ id: "c" }] }];
  assert.throws(() => validateMatches({ matches: [{ candidate: "p1", labels: [] }, { candidate: "p1", labels: [] }] }, asked), { code: "match_duplicate_candidate" });
  assert.throws(() => validateMatches({ matches: [{ candidate: "p1", labels: [] }, { candidate: "foreign", labels: [] }] }, asked), { code: "match_unknown_candidate" });
  assert.throws(() => validateMatches({ matches: [{ candidate: "p1", labels: ["foreign"] }, { candidate: "p2", labels: [] }] }, asked), { code: "match_invalid_label" });
  assert.deepEqual(validateMatches({ matches: [{ candidate: "p1", labels: ["b", "b"] }, { candidate: "p2", labels: [] }] }, asked), [{ id: "p1", same: true }, { id: "p2", same: false }]);
});

test("role contract failures remain unavailable while independent role quality still scores", async () => {
  const reference = [{ id: "p", verdict: "same", matchVerdict: "match" }];
  const fixture = { candidates: [{ id: "a" }, { id: "b" }], pairs: [{ id: "p", a: "a", b: "b" }] };
  const groupedFailure = await captureContractFailure(async () => { throw new RoleContractError("group_incomplete_final_coverage"); }, { clusters: null });
  const metrics = roleMetrics(fixture, reference, groupedFailure, { predictions: [{ id: "p", same: true }] }, { skipped: true });
  assert.equal(metrics.synthesize.contractFailure, true);
  assert.equal(metrics.synthesize.qualityAvailable, false);
  assert.equal(metrics.synthesize.falsePositive, undefined);
  assert.equal(metrics.match.truePositive, 1);
  const matcherFailure = await captureContractFailure(async () => { throw new RoleContractError("match_invalid_label"); }, { predictions: null });
  const validGroup = { clusters: { k: { id: "k", members: ["a", "b"] } }, omissions: [] };
  const inverse = roleMetrics(fixture, reference, validGroup, matcherFailure, { verdicts: [] });
  assert.equal(inverse.match.qualityAvailable, false);
  assert.equal(inverse.match.falseNegative, undefined);
  assert.equal(inverse.synthesize.truePositive, 1);
  for (const reason of ["budget", "binding", "credential", "failed", undefined]) {
    const error = Object.assign(new Error("generation failure"), { reason });
    await assert.rejects(captureContractFailure(async () => { throw error; }, {}), (thrown) => thrown === error);
  }
});


test("reference judges receive only the fields available to their respective shipped roles", () => {
  const fixture = { candidates: [
    { ...candidate("a", "Preserve write ordering"), why: "EXTRA_REASON", appliesTo: ["EXTRA_SCOPE"], commands: ["EXTRA_COMMAND"], target: "EXTRA_TARGET" },
    { ...candidate("b", "Preserve operation ordering"), why: "OTHER_REASON", appliesTo: ["OTHER_SCOPE"], commands: ["OTHER_COMMAND"] },
  ], pairs: [{ id: "p", a: "a", b: "b" }] };
  const grouping = referenceItems(fixture, fixture.pairs, "group");
  assert.deepEqual(grouping, [{ id: "p", a: { id: "a", kind: "fact", statement: "Preserve write ordering", appliesTo: ["EXTRA_SCOPE"], commands: ["EXTRA_COMMAND"] }, b: { id: "b", kind: "fact", statement: "Preserve operation ordering", appliesTo: ["OTHER_SCOPE"], commands: ["OTHER_COMMAND"] } }]);
  assert.equal(JSON.stringify(grouping).includes("EXTRA_REASON"), false);
  assert.equal(JSON.stringify(grouping).includes("EXTRA_TARGET"), false);
  const matching = referenceItems(fixture, fixture.pairs, "match");
  assert.deepEqual(matching, [{ id: "p", candidateStatement: "Preserve write ordering", labelStatement: "Preserve operation ordering" }]);
  assert.equal(JSON.stringify(matching).includes("EXTRA_"), false);
});


test("pair scoring recognizes any shared cluster instead of overwriting earlier memberships", () => {
  const fixture = { candidates: [{ id: "bundle" }, { id: "a" }, { id: "b" }], pairs: [{ id: "pa", a: "bundle", b: "a" }, { id: "pb", a: "bundle", b: "b" }, { id: "negative", a: "a", b: "b" }] };
  const grouped = { clusters: { first: { id: "first", members: ["bundle", "a"] }, last: { id: "last", members: ["bundle", "b"] } }, omissions: [] };
  const reference = [{ id: "pa", verdict: "same", matchVerdict: "match" }, { id: "pb", verdict: "same", matchVerdict: "match" }, { id: "negative", verdict: "distinct", matchVerdict: "no_match" }];
  const metrics = roleMetrics(fixture, reference, grouped, { predictions: [{ id: "pa", same: true }, { id: "pb", same: true }, { id: "negative", same: false }] }, { verdicts: [] });
  assert.equal(metrics.synthesize.truePositive, 2);
  assert.equal(metrics.synthesize.trueNegative, 1);
  assert.equal(metrics.synthesize.falseNegative, 0);
  assert.equal(metrics.synthesize.multiClusterCandidates, 1);
  assert.equal(metrics.synthesize.coveredCandidates, 3);
});

test("split-aware audit shows each bundled member's companion atomic statements", () => {
  const fixture = { candidates: [candidate("bundle", "Serialize writes and preserve backup files"), candidate("a", "Serialize writes"), candidate("b", "Preserve backup files")] };
  const clusters = { write: { id: "write", members: ["bundle", "a"], kind: "fact", statement: "Serialize writes", appliesTo: ["writes/**"], commands: [] }, backup: { id: "backup", members: ["bundle", "b"], kind: "fact", statement: "Preserve backup files", appliesTo: ["backups/**"], commands: [] } };
  const items = auditItems(fixture, clusters);
  assert.deepEqual(items.map((i) => i.id), ["s0", "s1"]);
  assert.equal(items[0].members.find((m) => m.id === "bundle").companionSyntheses[0].statement, "Preserve backup files");
  assert.equal(items[1].members.find((m) => m.id === "bundle").companionSyntheses[0].statement, "Serialize writes");
  assert.deepEqual(items[0].members.find((m) => m.id === "a").companionSyntheses, []);
  assert.equal(items[0].members.find((m) => m.id === "bundle").companionSyntheses[0].id, "s1");
});
