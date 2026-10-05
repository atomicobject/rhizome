import test from "node:test";
import assert from "node:assert/strict";
import { splitSource } from "./inventory.mjs";
import { enrichChunks, focusExclusion, sampleFocus, contextualCandidates } from "./context.mjs";
import { auditQuestions, findingsFor } from "./questions.mjs";
import { buildReviewQueue, documentMetadata } from "./triggers.mjs";

const chunks = (file, kind, text) => enrichChunks(splitSource(file, kind, text), text);

test("screening keeps status contradictions while dropping empty headings and shared navigation", () => {
  const list = chunks(
    "docs/efforts/example.md",
    "note",
    "---\nstatus: complete\n---\n# Example\n## User Stories\n## Related\n- [[Shared hub]]\n## Status\nImplementation is pending.\n## Actual Delivered\nThe feature and tests are delivered.\n## Execution Notes\nVerified all stories.",
  );
  assert.equal(
    focusExclusion(list.find((c) => c.heading === "User Stories")),
    "empty_or_navigation",
  );
  assert.equal(focusExclusion(list.find((c) => c.heading === "Related")), "empty_or_navigation");
  const status = list.find((c) => c.heading === "Status");
  assert.equal(focusExclusion(status), undefined);
  const result = contextualCandidates(status, { candidates: [] }, new Map([[status.path, list]]));
  assert.equal(result.candidates[0].heading, "Actual Delivered");
  assert.ok(result.candidates.every((c) => c.path === status.path));
  assert.deepEqual(status.context.headingPath, ["Example", "Status"]);
  assert.equal(status.context.document.status, "complete");
});

test("code packets retain build and enclosing-test setup context beyond line chunks", () => {
  const text =
    '//go:build race\npackage p\nfunc TestCurrentState(t *testing.T) {\n state := "current"\n' +
    "\n".repeat(102) +
    " assertReady(state)\n}\n";
  const list = chunks("state_test.go", "code", text);
  const last = list.at(-1);
  assert.equal(
    last.context.enclosingDeclarationHint.signature,
    "func TestCurrentState(t *testing.T) {",
  );
  assert.match(last.context.declarationExcerpt, /current/);
  assert.deepEqual(last.context.buildConditions, ["//go:build race"]);
  assert.equal(focusExclusion(last), "test_or_fixture_focus");
  assert.equal(
    focusExclusion({ ...last, path: "testdata/example.go" }),
    "test_or_fixture_focus",
  );
});

test("candidate selection prioritizes admitted ancestor guidance and sampling is reproducible across input order", () => {
  const code = chunks("pkg/component/main.go", "code", "package component\nfunc Execute() {}")[0];
  const owner = chunks(
    "pkg/component/CONTEXT.md",
    "note",
    "# Component\nExecute must preserve source authority.",
  )[0];
  const unrelated = chunks("other.md", "note", "# Other\nDifferent responsibilities.")[0];
  const result = contextualCandidates(
    code,
    { candidates: [unrelated], candidateCoverage: {} },
    new Map([[owner.path, [owner]]]),
  );
  assert.equal(result.candidates[0].path, owner.path);
  const all = Array.from({ length: 30 }, (_, i) => ({
    ...code,
    id: String(i),
    path: `file-${i}.go`,
  }));
  assert.deepEqual(sampleFocus(all, 5, "seed"), sampleFocus([...all].reverse(), 5, "seed"));
  assert.notDeepEqual(sampleFocus(all, 5, "seed"), sampleFocus(all, 5, "another"));
});

test("expected differences never become corrections and exploration is explicit", () => {
  const a = chunks("a.md", "note", "# Contract\nThe operation must be serial.")[0];
  const b = chunks("b.go", "code", "package b\nfunc Execute() {}")[0];
  assert.equal(auditQuestions(a, [b]).link_0, undefined);
  assert.ok(auditQuestions(a, [b], true).link_0);
  const packet = { state: { focus: a, candidates: [b] } };
  const base = {
    relation_0: { choice: "contradicts", probabilities: { contradicts: 0.96 } },
    action_0: { choice: "expected" },
  };
  assert.equal(findingsFor(packet, base).length, 0);
  const yes = findingsFor(packet, {
    ...base,
    action_0: { choice: "correction" },
    role: { choice: "historical" },
  });
  assert.equal(yes[0].action, "investigate_disagreement");
  const metadata = new Map([a, b].map((s) => [s.path, documentMetadata(s.path, s.content)]));
  const link = {
    focus: a,
    candidate: b,
    action: "consider_link",
    evidence: "sufficient",
    relation: "supports",
    distribution: { supports: 0.99 },
    linkScore: 3,
  };
  assert.equal(buildReviewQueue([link], metadata).packets.length, 0);
  assert.equal(buildReviewQueue([link], metadata, 100, true).packets.length, 1);
});
