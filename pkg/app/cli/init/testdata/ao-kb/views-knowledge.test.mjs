import assert from "node:assert/strict";
import test from "node:test";
import {
  badges,
  claimText,
  citations,
  coverage,
  conflictPair,
  filterAssertions,
  observations,
  physicalSourceLine,
  relationships,
  sourceText,
} from "../../templates/starters/ao-kb/module/views/knowledge/lib/knowledge.ts";
import { loadSourceDocument } from "../../templates/starters/ao-kb/module/views/knowledge/lib/source-document.ts";
import { resolvePassage } from "../../templates/starters/ao-kb/module/views/knowledge/lib/locators.ts";

const record = (path, aoId, title = path) => ({
  path,
  aoId,
  title,
  ref: { notePath: path, kind: "NOTE" },
});
const assertion = (path, extra = {}) => ({
  ...record(path, path),
  content: "The complete claim.",
  concepts: [],
  workspace: { capabilities: {} },
  ...extra,
});
const source = (path, id, extra = {}) => ({
  ...record(path, id),
  content: "---\nid: source\n---\n\nBody fallback\nsecond line",
  frontmatter: {},
  sourcePaths: [],
  extractedAssertionIds: [],
  ...extra,
});
const data = (assertions = [], sources = [], extra = {}) => ({
  assertion: assertions,
  source: sources,
  concept: [],
  artifact: [],
  conflictRegistry: [],
  ...extra,
});
const link = (r) => ({ recordedLink: `[[${r.path}]]`, sources: [{ path: r.path }] });

test("complete claim text retains authored headings before the structured sections", () => {
  const a = assertion("a", {
    content:
      "Full claim.\n\n## Rationale\n\nAll its reasoning.\n\n## Connections\n\n- supports:: [[b]]",
  });
  assert.equal(claimText(a), "Full claim.\n\n## Rationale\n\nAll its reasoning.");
});

test("recorded origin wins; repeated Sources occurrences stay ordered and do not inflate origin coverage", () => {
  const s1 = source("sources/interview.md", "s-1"),
    s2 = source("sources/operations.md", "s-2");
  const a = assertion("a", {
    provenanceSource: "s-2",
    provenanceLocator: "L1",
    sourcesSection: { citations: [link(s1), link(s2), link(s1)] },
  });
  assert.deepEqual(
    citations(a, [s1, s2]).map((c) => [c.source.path, c.slot, c.origin, c.locator]),
    [
      ["sources/operations.md", 2, true, "L1"],
      ["sources/interview.md", 1, false, undefined],
      ["sources/interview.md", 3, false, undefined],
    ],
  );
  assert.equal(
    citations({ ...a, provenanceSource: undefined }, [s1, s2])[0].chosenBy,
    "first Sources link",
  );
  assert.equal(citations({ ...a, provenanceSource: "missing" }, [s1, s2])[0].source, undefined);
});

test("ambiguous AO IDs never choose a source", () => {
  const a = assertion("a", { provenanceSource: "s-duplicate" });
  assert.equal(
    citations(a, [source("one.md", "s-duplicate"), source("two.md", "s-duplicate")])[0].source,
    undefined,
  );
});

test("coverage is one of five mutually exclusive locator states, including malformed and timestamp", () => {
  const s = source("sources/a.md", "s-1", { locatorScheme: "line" }),
    c = { raw: "s-1", source: s, origin: true };
  const text = sourceText(s);
  assert.equal(coverage(undefined, undefined), "No source");
  assert.equal(coverage(c, resolvePassage(text, "L1", new Map())), "Recorded passage");
  assert.equal(coverage(c, resolvePassage(text, undefined, new Map())), "Document only");
  assert.equal(
    coverage(c, resolvePassage(text, "Lbanana", new Map())),
    "Unresolved source/locator",
  );
  assert.equal(
    coverage(c, resolvePassage(undefined, "L1", new Map())),
    "Unresolved source/locator",
  );
  assert.equal(
    coverage(c, resolvePassage({ ...text, scheme: "timestamp" }, "01:10-01:30", new Map())),
    "Unsupported locator",
  );
});

test("Attention uses normalized inverse and frontmatter relations and keeps questions and dates honest", () => {
  const old = assertion("old", { kind: "question", validUntil: "2026-10-31", status: "active" });
  const current = assertion("new", {
    frontmatterSupersedes: ["old"],
    connectionsSection: { workspace: { fields: [{ name: "supersedes", values: ["old"] }] } },
  });
  const answered = assertion("answered", { kind: "question", frontmatterResolvedBy: ["new"] });
  const retracted = assertion("retracted", { kind: "question", status: "retracted" });
  const open = assertion("open", { kind: "question", validFrom: "2026-10-10" });
  const graph = data([old, current, answered, retracted, open]);
  assert.equal(relationships(graph).edges.filter((e) => e.rel === "supersedes").length, 1);
  assert.deepEqual(badges(old, relationships(graph).edges, "2026-11-06"), [
    "Superseded",
    "Expired",
  ]);
  const groups = observations(graph, "2026-11-06");
  assert.deepEqual(
    groups["Open questions"].map((o) => o.record.path),
    ["open"],
  );
  assert.match(groups["Open questions"][0].basis, /Valid from 2026-10-10/);
  assert.equal(badges({ ...old, validUntil: "2026-99-99" }, [], "2026-11-06").length, 0);
});

test("typed raw dangling declarations are visible without parsing unknown Connections text", () => {
  const a = assertion("a", {
    connectionsSection: {
      content: "- teleports-to:: [[b]]",
      workspace: { fields: [{ name: "dependsOn", values: ["missing"] }] },
    },
  });
  const groups = observations(data([a]), "2026-11-06");
  assert.equal(groups["Dangling declared references"].length, 1);
  assert.match(groups["Dangling declared references"][0].basis, /depends-on: missing/);
  assert.equal(relationships(data([a])).edges.length, 0);
});

test("exact registry Assertions field determines the pair, retaining other recorded fields", () => {
  const c = {
    fields: [
      { label: "Assertions", value: "[[a]], [[b]]" },
      { label: "Resolution", value: "See [[c]]" },
    ],
  };
  assert.deepEqual(
    conflictPair(c, (x) => x),
    ["a", "b"],
  );
  const a = assertion("a", { frontmatterContradicts: ["b"] }),
    b = assertion("b", { frontmatterContradicts: ["a"] });
  const registry = {
    ...record("registry", "r"),
    ...c,
    fields: [...c.fields, { label: "State", value: "accepted-trade-off" }],
  };
  const groups = observations(
    data([a, b], [], { conflictRegistry: [{ groups: [{ entries: [registry] }] }] }),
    "2026-11-06",
  );
  assert.equal(groups["Contradictions recorded"].length, 1);
  assert.equal(groups["Contradictions recorded"][0].risk, false);
  assert.match(groups["Contradictions recorded"][0].basis, /accepted-trade-off/);
});

test("sync observations use authored timestamps and missing or incomparable means unknown", () => {
  const a = assertion("a", { updatedAt: "2026-11-03T10:00:00Z" });
  const artifact = {
    ...record("artifact", "art"),
    supportedBy: [a],
    additionalAssertionReferences: [],
    conceptReferences: [],
    sourceReferences: [],
    artifactReferences: [],
  };
  let groups = observations(
    data([a], [], { artifact: [{ ...artifact, graphSyncedAt: "2026-10-31" }] }),
    "2026-11-06",
  );
  assert.equal(groups["Knowledge updated since sync"].length, 1);
  groups = observations(
    data([a], [], { artifact: [{ ...artifact, graphSyncedAt: "bad" }] }),
    "2026-11-06",
  );
  assert.equal(groups["Knowledge updated since sync"].length, 0);
  assert.equal(groups["Sync date unknown"].length, 1);
});

test("the concept filter uses shared membership, so a link from the concept side counts", () => {
  const a = assertion("a"), b = assertion("b");
  const conceptsOf = new Map([["a", ["concepts/one.md", "concepts/two.md"]], ["b", []]]);
  assert.deepEqual(filterAssertions([a, b], "", "concepts/two.md", conceptsOf), [a]);
  assert.deepEqual(filterAssertions([a, b], "does not match", "", conceptsOf), []);
});

test("source paths retain blank slots; selected raw file passage is marked only in its own file", async () => {
  const s = source("sources/s.md", "s", {
    frontmatter: { path: ["", "sources/_raw/second.md", "https://example.invalid"] },
    sourcePaths: ["sources/_raw/second.md"],
    locatorScheme: "line",
  });
  const a = assertion("a", { provenanceSource: "s", provenanceLocator: "2::L2-L3" });
  const calls = [];
  const doc = await loadSourceDocument(s, data([a], [s]), async (path) => {
    calls.push(path);
    return "first\nsecond\nthird\n";
  });
  assert.deepEqual(calls, ["sources/_raw/second.md"]);
  assert.equal(doc.slots[0].path, "");
  assert.equal(doc.citers[0].passage.path, "sources/_raw/second.md");
  assert.equal(doc.citers[0].passage.startLine, 2);
  assert.equal(doc.slots[2].status, "Not an eligible local text file");
});

test("source read refusal cannot become a fallback passage", async () => {
  const s = source("sources/s.md", "s", {
    frontmatter: { path: "sources/_raw/s.txt" },
    locatorScheme: "line",
  });
  const a = assertion("a", { provenanceSource: "s", provenanceLocator: "L1" });
  const doc = await loadSourceDocument(s, data([a], [s]), async () => {
    throw new Error("refused");
  });
  assert.deepEqual(doc.files, []);
  assert.notEqual(doc.citers[0].passage.status, "resolved");
});

test("GraphQL null locator is document-only and never passed into the span parser", async () => {
  const s = source("sources/s.md", "s");
  const a = assertion("a", { provenanceSource: "s", provenanceLocator: null });
  const doc = await loadSourceDocument(s, data([a], [s]), async () => undefined);
  assert.equal(doc.citers[0].passage.status, "document-only");
});

test("source-note fallback links account for frontmatter and trimmed blank lines", () => {
  const s = source("sources/s.md", "s");
  assert.equal(physicalSourceLine(s, s.path, 2), 6);
  assert.equal(physicalSourceLine(s, "sources/_raw/s.txt", 2), 2);
});


test("an unresolved native citation stays unresolved even when its raw text matches a source", () => {
 const s = source("sources/interview.md", "s-1");
 const a = assertion("a", { sourcesSection: { citations: [{ recordedLink: "[[sources/interview.md]]", sources: [] }] } });
 assert.equal(citations(a, [s])[0].source, undefined);
});
