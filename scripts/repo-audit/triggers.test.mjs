import test from "node:test";
import assert from "node:assert/strict";
import { buildReviewQueue, documentMetadata } from "./triggers.mjs";

const source = (path, kind = "note", content = "Evidence") => ({
  path,
  kind,
  content,
  startLine: 1,
  endLine: 3,
  heading: "Behavior",
});
const finding = (a, b, extra = {}) => ({
  focus: a,
  candidate: b,
  action: "investigate_disagreement",
  evidence: "partial",
  distribution: { contradicts: 0.95 },
  impact: 2,
  packetId: "one",
  ...extra,
});
const metadata = (sources) =>
  new Map(sources.map((s) => [s.path, documentMetadata(s.path, s.content)]));

test("routine matches and weak disagreements never become follow-up jobs", () => {
  const a = source("a.md"),
    b = source("b.go", "code");
  const r = buildReviewQueue(
    [
      finding(a, b, {
        action: "consider_link",
        linkScore: 2.5,
        relation: "supports",
        distribution: { supports: 0.95 },
      }),
      finding(a, b, { distribution: { contradicts: 0.4 } }),
    ],
    metadata([a, b]),
  );
  assert.equal(r.packets.length, 0);
  assert.equal(r.summary.suppressed.below_trigger_or_routine_match, 1);
  assert.equal(r.summary.suppressed.exploration_disabled, 1);
});

test("reverse and repeated section findings produce one bounded issue packet", () => {
  const a = source("guide.md"),
    b = source("service.go", "code");
  const findings = [
    finding(a, b),
    finding(b, a, { packetId: "two" }),
    finding({ ...a, startLine: 12 }, b),
  ];
  const r = buildReviewQueue(findings, metadata([a, b]));
  assert.equal(r.packets.length, 1);
  assert.equal(r.packets[0].supportingFindings, 3);
  assert.equal(r.packets[0].evidence.length, 2);
  assert.deepEqual(r.packets[0].checkpointIds, ["one", "two"]);
  const rebuilt = buildReviewQueue([...findings].reverse(), metadata([a, b]));
  assert.equal(rebuilt.packets[0].evidenceHash, r.packets[0].evidenceHash);
});

test("bounded packets retain the strongest evidence pairs deterministically", () => {
  const a = source("guide.md"),
    b = source("service.go", "code");
  const findings = [
    finding({ ...a, startLine: 10 }, b, {
      packetId: "weak",
      impact: 1,
      distribution: { contradicts: 0.81 },
    }),
    finding({ ...a, startLine: 20 }, b, {
      packetId: "medium",
      impact: 2,
      distribution: { contradicts: 0.9 },
    }),
    finding({ ...a, startLine: 30 }, b, {
      packetId: "strong",
      impact: 3,
      distribution: { contradicts: 0.99 },
    }),
    finding({ ...a, startLine: 40 }, b, {
      packetId: "strongest",
      impact: 4,
      distribution: { contradicts: 0.95 },
    }),
  ];
  const result = buildReviewQueue(findings, metadata([a, b]));
  assert.deepEqual(
    result.packets[0].evidence.map((e) => e.packetId),
    ["strongest", "strong", "medium"],
  );
  const rebuilt = buildReviewQueue([...findings].reverse(), metadata([a, b]));
  assert.equal(rebuilt.packets[0].evidenceHash, result.packets[0].evidenceHash);
});

test("expected generated copies and supersession are suppressed, not treated as fixes", () => {
  const a = source(
    "AGENTS.md",
    "note",
    "<!-- BEGIN RZM INIT CORE -->\nGenerated\n<!-- END RZM INIT CORE -->",
  );
  const b = source("template.md");
  const c = source("old.md", "note", "---\nspec-status: superseded\n---\n# Old");
  const r = buildReviewQueue([finding(a, b), finding(c, b)], metadata([a, b, c]));
  assert.equal(r.packets.length, 0);
  assert.equal(r.summary.suppressed.generated_source, 1);
  assert.equal(r.summary.suppressed.superseded_or_archived, 1);
});

test("completed effort status reconciliation remains possible without comparing unrelated effort plans", () => {
  const a = {
    ...source("docs/efforts/done.md", "note", "---\nstatus: complete\n---\n# Done"),
    heading: "Delivery Tracking",
  };
  const b = source("feature.go", "code");
  const unrelated = source("docs/efforts/other.md");
  const r = buildReviewQueue(
    [finding(a, b), finding(a, unrelated), finding({ ...a, heading: "Plan" }, b)],
    metadata([a, b, unrelated]),
  );
  assert.equal(r.packets.length, 1);
  assert.equal(r.summary.suppressed.different_effort_scopes, 1);
  assert.equal(r.summary.suppressed.historical_effort_section, 1);
  const status = buildReviewQueue(
    [finding(a, { ...a, heading: "Execution Notes" })],
    metadata([a]),
  );
  assert.equal(status.packets.length, 1);
});

test("review caps report omissions and source changes invalidate affected jobs", () => {
  const a = source("a.md"),
    b = source("b.go", "code"),
    c = source("c.go", "code");
  let r = buildReviewQueue([finding(a, b), finding(a, c)], metadata([a, b, c]), 1);
  assert.equal(r.packets.length, 1);
  assert.equal(r.summary.omittedByReviewLimit, 1);
  const m = metadata([a, b]);
  m.get("a.md").stale = true;
  r = buildReviewQueue([finding(a, b)], m);
  assert.equal(r.packets.length, 0);
  assert.equal(r.summary.suppressed.source_changed, 1);
});
