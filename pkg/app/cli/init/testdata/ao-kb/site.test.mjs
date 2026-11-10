import assert from "node:assert/strict";
import test from "node:test";
import {
  artifactFollowUp,
  artifactScope,
  assertionFilter,
  bodyLinkParagraph,
  buildSite,
  citationState,
  conceptScope,
  drawable,
  gap,
  GAPS,
  kbTitle,
  layout,
  matrix,
  rowLabel,
  scopedPair,
  short,
  sourceCandidates,
  sourceMark,
  sourceScope,
  timeAxis,
  timeline,
  tintPercent,
} from "../../templates/starters/ao-kb/module/views/knowledge/lib/site.ts";
import { cropBox, neighbourhood } from "../../templates/starters/ao-kb/module/views/knowledge/lib/neighbourhood.ts";
import { breakdown, groupOf } from "../../templates/starters/ao-kb/module/views/knowledge/lib/kinds.ts";
import { citations } from "../../templates/starters/ao-kb/module/views/knowledge/lib/knowledge.ts";

// Synthetic records shaped like the orientation query (SPEC-0135 R5).
const rec = (path, aoId = null, title = path.split("/").pop().replace(/\.md$/, "")) => ({ ref: { notePath: path, kind: "NOTE" }, path, aoId, title });
const A = (slug, extra = {}) => ({ ...rec(`assertions/${slug}.md`, `a-${slug}`), headline: null, kind: "fact", legacyKind: null, confidence: "validated", provenanceSource: null, ...extra });
const S = (path, extra = {}) => ({ ...rec(path, `s-${path.split("/").pop().replace(/\.md$/, "")}`), name: null, headline: null, recordedDate: null, extractedAssertionIds: [], ...extra });
const C = (slug, { out = [], inn = [], related = [], ...extra } = {}) => ({
  ...rec(`concepts/${slug}.md`, `c-${slug}`, extra.title ?? slug),
  name: null, headline: null, aliases: [],
  linkedAssertions: out.map((s) => ({ path: `assertions/${s}.md` })),
  linkingAssertions: inn.map((s) => ({ path: `assertions/${s}.md` })),
  relatedConceptsSection: { relatedConcepts: related.map((s) => ({ path: `concepts/${s}.md` })) },
});
const kb = ({ assertion = [], source = [], concept = [], artifact = [], mapOfContent = [] } = {}) => ({ assertion, source, concept, artifact, mapOfContent });
const complete = (n) => ({ status: "complete", returnedCount: n });
const shuffle = (xs, seed) => {
  const out = [...xs];
  for (let i = out.length - 1; i > 0; i--) {
    seed = (seed * 1103515245 + 12345) % 2147483648;
    const j = seed % (i + 1);
    [out[i], out[j]] = [out[j], out[i]];
  }
  return out;
};
const shuffled = (o, seed) => Object.fromEntries(Object.entries(o).map(([k, v]) => [k, shuffle(v, seed)]));

// A medium KB: concepts with members, related pairs, dated sources in folders.
function medium(n = 21) {
  const assertion = [], concept = [], source = [];
  const kinds = ["fact", "decision", "risk", "tribal-knowledge", "constraint", "assumption", "lesson"];
  for (let i = 0; i < n * 4; i++) assertion.push(A(`m${i}`, { kind: kinds[i % kinds.length], confidence: ["validated", "hypothesis", "guess", "high"][i % 4] }));
  for (let c = 0; c < n; c++) {
    const members = assertion.filter((_, i) => i % n === c || i % (c + 2) === 0).map((a) => a.path.slice(11, -3));
    concept.push(C(`k${c}`, { title: `Concept number ${c} about a long named topic area`, out: members.slice(0, 2), inn: members.slice(1), related: c % 3 ? [`k${(c + 1) % n}`] : [] }));
  }
  for (let s = 0; s < 9; s++) source.push(S(`sources/f${s % 3}/2024-0${(s % 9) + 1}-10-note${s}.md`, { recordedDate: s % 2 ? `2024-0${(s % 9) + 1}-12` : null, extractedAssertionIds: [`a-m${s}`, `a-m${s + 1}`] }));
  return kb({ assertion, concept, source });
}

test("shuffled input gives identical layout and counts", () => {
  const o = medium();
  const a = buildSite(o), b = buildSite(shuffled(o, 7));
  assert.deepEqual(layout(b), layout(a));
  assert.deepEqual(matrix(b), matrix(a));
  assert.deepEqual(GAPS.map((g) => gap(b, g.id)), GAPS.map((g) => gap(a, g.id)));
  assert.deepEqual(timeline(b), timeline(a));
  assert.deepEqual([...b.members], [...a.members]);
});

test("both vocabularies group as R3 says, with principle in Intent and rules and custom kinds in Other", () => {
  const cases = { fact: "statements", "tribal-knowledge": "statements", "customer-voice": "statements", need: "statements", decision: "intent", principle: "intent", constraint: "intent", "trade-off": "intent", risk: "risks", hypothesis: "risks", assumption: "risks", lesson: "other", [""]: "other" };
  for (const [kind, group] of Object.entries(cases)) assert.equal(groupOf(kind), group, kind);
  assert.equal(groupOf(null), "other");
});

test("confidence high is never counted as hypothesis or guess, and a kind hypothesis is not a confidence", () => {
  const rows = [A("h", { confidence: "high", kind: "hypothesis" }), A("g", { confidence: "guess" }), A("y", { confidence: "hypothesis" })];
  const b = breakdown(rows);
  assert.deepEqual(b.confidence, [{ value: "hypothesis", count: 1 }, { value: "guess", count: 1 }]);
  assert.deepEqual(b.groups.find((g) => g.id === "risks").kinds, [{ kind: "hypothesis", count: 1 }]);
  const site = buildSite(kb({ assertion: rows, concept: [C("c", { inn: ["h", "g", "y"] })] }));
  assert.equal(matrix(site).rows[0].stored, 2);
  assert.deepEqual(gap(site, "confidence-other").values, { high: 1 });
  assert.deepEqual(site.assertions.filter(assertionFilter(site, { confidence: "hypothesis,guess" })).map((a) => a.path), ["assertions/g.md", "assertions/y.md"]);
});

test("a file-name date is never reported as recorded, and an unreadable recorded date is not absent", () => {
  const site = buildSite(kb({ source: [
    S("sources/2024-03-01-call.md"),
    S("sources/b.md", { recordedDate: "2024-02-02T10:00:00Z" }),
    S("sources/2024-05-05-c.md", { recordedDate: 20240505 }),
    S("sources/d.md", { recordedDate: "next week" }),
    S("sources/2024-13-40-e.md"),
  ] }));
  const d = (p) => site.dates.get(`sources/${p}.md`);
  assert.deepEqual(d("2024-03-01-call"), { date: "2024-03-01", basis: "file", unreadable: false });
  assert.deepEqual(d("b"), { date: "2024-02-02", basis: "recorded", unreadable: false });
  assert.deepEqual(d("2024-05-05-c"), { date: "2024-05-05", basis: "file", unreadable: true });
  assert.deepEqual(d("d"), { date: null, basis: "none", unreadable: true });
  assert.deepEqual(d("2024-13-40-e"), { date: null, basis: "none", unreadable: false });
  assert.deepEqual(gap(site, "source-no-recorded-date").items, ["sources/2024-03-01-call.md", "sources/2024-05-05-c.md", "sources/2024-13-40-e.md", "sources/d.md"]);
  assert.deepEqual(gap(site, "source-unreadable-date").items, ["sources/2024-05-05-c.md", "sources/d.md"]);
  assert.deepEqual(timeline(site).counts, { recorded: 1, file: 2, none: 2, unreadable: 2 });
});

test("membership unites both directions, deduplicated, including an assertion linked only from the concept side", () => {
  const site = buildSite(kb({
    assertion: [A("both"), A("outonly"), A("inonly"), A("none")],
    concept: [C("c", { out: ["both", "outonly", "ghost"], inn: ["both", "inonly"] }), C("empty")],
  }));
  assert.deepEqual(site.members.get("concepts/c.md"), ["assertions/both.md", "assertions/inonly.md", "assertions/outonly.md"]);
  assert.deepEqual(site.conceptsOf.get("assertions/outonly.md"), ["concepts/c.md"]);
  assert.deepEqual(gap(site, "assertion-no-concept").items, ["assertions/none.md"]);
  assert.deepEqual(gap(site, "concept-no-members").items, ["concepts/empty.md"]);
});

test("every not-enough-to-draw threshold, including each half of the scoped pair", () => {
  const two = buildSite(kb({ assertion: [A("a")], concept: [C("x"), C("y")], source: [S("sources/2024-01-01-a.md"), S("sources/2024-01-02-b.md")] }));
  assert.deepEqual(drawable(two), { map: false, timeline: false, matrix: false });
  const pair = scopedPair(two, { topics: new Set(["concepts/x.md"]), sources: new Set(["sources/2024-01-01-a.md", "sources/u.md"]) });
  assert.equal(pair.map.drawable, false);
  assert.equal(pair.map.concepts, 2);
  assert.equal(pair.strip.drawable, false);
  assert.equal(pair.strip.datedInKb, 2);
  assert.deepEqual(pair.strip.dated, ["sources/2024-01-01-a.md"]);
  assert.deepEqual(pair.strip.undated, ["sources/u.md"]);

  const three = buildSite(kb({ assertion: [A("a")], concept: [C("x", { inn: ["a"] }), C("y"), C("z")], source: ["01", "02", "03"].map((d) => S(`sources/2024-01-${d}-s.md`)) }));
  assert.deepEqual(drawable(three), { map: true, timeline: true, matrix: true });
  const ok = scopedPair(three, { topics: new Set(["concepts/x.md", "concepts/y.md"]), sources: new Set() });
  assert.equal(ok.map.drawable, true);
  assert.equal(ok.map.missing, null);
  assert.ok(ok.map.hood.box.w > 0);
  assert.equal(ok.strip.drawable, true);
  // A scope with no concept has nothing to place on the map.
  const none = scopedPair(three, { topics: new Set(), sources: new Set() });
  assert.deepEqual([none.map.drawable, none.map.missing, none.map.hood], [false, "no-concept", null]);
  const noMembers = buildSite(kb({ assertion: [A("a")], concept: [C("x"), C("y"), C("z")] }));
  assert.equal(drawable(noMembers).matrix, false);
});

test("a root that is not complete is disclosed, and a missing coverage entry is not assumed complete", () => {
  const site = buildSite(kb({ assertion: [A("a")] }), { assertion: { status: "capped", returnedCount: 1 }, source: complete(0), concept: complete(0), artifact: complete(0) });
  assert.deepEqual(site.incomplete, [{ root: "assertion", status: "capped", returned: 1 }, { root: "mapOfContent", status: "not reported", returned: 0 }]);
  assert.deepEqual(buildSite(kb(), Object.fromEntries(["assertion", "source", "concept", "artifact", "mapOfContent"].map((r) => [r, complete(0)]))).incomplete, []);
});

test("support values resolve by AO ID and by file name; ambiguous and unknown values stay with their raw text", () => {
  const assertion = [A("by-id", { aoId: "a-0001" }), A("by-name"), { ...A("twin"), path: "assertions/x/twin.md", aoId: "a-t1" }, { ...A("twin"), path: "assertions/y/twin.md", aoId: "a-t2" }];
  const site = buildSite(kb({ assertion, concept: [C("k", { inn: ["by-id"] })], source: [S("sources/s.md")], artifact: [{ ...rec("artifacts/doc.md"), name: null, headline: null }] }));
  const read = {
    path: "artifacts/doc.md",
    supportValues: ["a-0001", "[[by-name]]", "twin", "a-missing"],
    additionalAssertionReferences: [{ path: "assertions/by-id.md", sourcesSection: { citations: [] } }, { path: "assertions/x/twin.md", sourcesSection: { citations: [{ recordedLink: "[[s]]", sources: [{ path: "sources/s.md" }] }] } }],
    conceptReferences: [{ path: "concepts/k.md" }],
  };
  assert.deepEqual(artifactFollowUp(site, read), ["assertions/by-name.md"]);
  const scope = artifactScope(site, read, [{ path: "assertions/by-name.md", sourcesSection: { citations: [] } }]);
  assert.deepEqual(scope.supported, ["assertions/by-id.md", "assertions/by-name.md"]);
  assert.deepEqual(scope.unresolved, ["twin", "a-missing"]);
  assert.deepEqual(scope.links, ["assertions/x/twin.md"]);
  assert.deepEqual([...scope.topics], ["concepts/k.md"]);
  assert.deepEqual([...scope.sources], ["sources/s.md"]);
});

test("each US3 citation state", () => {
  const assertion = A("claim", { provenanceSource: "s-one", provenanceLocator: "L3" });
  const sources = [S("sources/one.md"), S("sources/two.md"), S("sources/three.md")];
  const site = buildSite(kb({ assertion: [assertion], source: sources }));
  const read = { ...assertion, sourcesSection: { citations: [
    { recordedLink: "[[two]]", sources: [{ path: "sources/two.md" }] },
    { recordedLink: "[[three]]", sources: [{ path: "sources/three.md" }] },
    { recordedLink: "[[gone]]", sources: [] },
  ] } };
  const [origin, two, three, gone] = citations(read, sources);
  assert.equal(citationState(origin, { status: "resolved" }, -1), "passage");
  assert.equal(citationState(origin, { status: "unsupported" }, -1), "locator");
  assert.equal(citationState(origin, { status: "unresolved" }, 2), "locator");
  const body2 = "Intro paragraph.\n\nSee [the claim](../assertions/claim.md#Top) for it.\n\nAlso [[claim]].";
  const p = bodyLinkParagraph(body2, assertion.path, site.resolveAssertion);
  assert.equal(p, 1);
  assert.equal(citationState(two, undefined, p), "linked");
  assert.equal(citationState(three, undefined, bodyLinkParagraph("Mentions [[other]] and https://x.test/claim.md only.", assertion.path, site.resolveAssertion)), "not-linked");
  assert.equal(citationState(gone, undefined, -1), "unresolved");
});

test("a link inside code is not an authored assertion link, and paragraphs keep their places", () => {
  const assertion = A("claim");
  const site = buildSite(kb({ assertion: [assertion] }));
  const fenced = "Intro.\n\n```md\nSee [[claim]].\n\n[the claim](../assertions/claim.md)\n```\n\nLater.";
  assert.equal(bodyLinkParagraph(fenced, assertion.path, site.resolveAssertion), -1);
  assert.equal(bodyLinkParagraph("Use `[[claim]]` to link.", assertion.path, site.resolveAssertion), -1);
  assert.equal(bodyLinkParagraph("~~~\n[[claim]]\n~~~\n\nAfter the fence, [[claim]].", assertion.path, site.resolveAssertion), 1);
  assert.equal(bodyLinkParagraph(`${fenced}\n\nAnd \`code\` then [[claim]].`, assertion.path, site.resolveAssertion), 4);
});

test("a source's scope includes an assertion that cites it only through provenanceSource, with its concepts on the map", () => {
  const site = buildSite(kb({
    assertion: [A("linked"), A("prov", { provenanceSource: "s-src" }), A("aside")],
    concept: [C("k1", { inn: ["linked"] }), C("k2", { out: ["prov"] }), C("k3", { inn: ["aside"] })],
    source: [S("sources/src.md"), S("sources/other.md")],
  }));
  const candidates = sourceCandidates(site, "sources/src.md", [{ path: "assertions/linked.md" }, { path: "assertions/aside.md" }]);
  assert.deepEqual(candidates, ["assertions/aside.md", "assertions/linked.md", "assertions/prov.md"]);
  const scope = sourceScope(site, "sources/src.md", [
    { path: "assertions/linked.md", sourcesSection: { citations: [{ recordedLink: "[[src]]", sources: [{ path: "sources/src.md" }] }] } },
    { path: "assertions/aside.md", sourcesSection: { citations: [{ recordedLink: "[[other]]", sources: [{ path: "sources/other.md" }] }] } },
    { path: "assertions/prov.md", sourcesSection: { citations: [] } },
  ]);
  assert.deepEqual(scope.citing, ["assertions/linked.md", "assertions/prov.md"]);
  assert.deepEqual([...scope.topics], ["concepts/k1.md", "concepts/k2.md"]);
  assert.deepEqual([...scope.sources], ["sources/src.md"]);
  assert.equal(scope.mark, "sources/src.md");
});

test("concept scope: its Related Concepts on the map and the sources its members cite on the strip", () => {
  const site = buildSite(kb({ assertion: [A("a"), A("b")], concept: [C("k", { inn: ["a", "b"], related: ["j"] }), C("j")], source: [S("sources/s.md")] }));
  const scope = conceptScope(site, "concepts/k.md", [{ path: "assertions/a.md", sourcesSection: { citations: [{ recordedLink: "[[s]]", sources: [{ path: "sources/s.md" }] }] } }]);
  assert.deepEqual([...scope.topics], ["concepts/j.md", "concepts/k.md"]);
  assert.deepEqual([...scope.sources], ["sources/s.md"]);
});

test("the Assertions list breakdown follows the list's other filters, and a matrix cell equals its list", () => {
  const o = medium();
  const site = buildSite(o);
  const concept = matrix(site).rows[0].concept;
  const rows = site.assertions.filter(assertionFilter(site, { concept }));
  const b = breakdown(rows);
  assert.equal(b.groups.reduce((n, g) => n + g.count, 0), rows.length);
  const m = matrix(site);
  m.columns.forEach((col, i) => {
    const listed = site.assertions.filter(assertionFilter(site, { concept: m.rows[0].concept, assertionKind: col.kind }));
    assert.equal(listed.length, m.rows[0].cells[i], col.kind);
  });
  const stored = site.assertions.filter(assertionFilter(site, { concept: m.rows[0].concept, confidence: "hypothesis,guess" }));
  assert.equal(stored.length, m.rows[0].stored);
  const narrowed = breakdown(site.assertions.filter(assertionFilter(site, { concept, assertionKind: "fact" })));
  assert.deepEqual(narrowed.groups.map((g) => g.id), ["statements"]);
  const gapRows = site.assertions.filter(assertionFilter(site, { gap: "kind-conflict" }));
  assert.equal(gapRows.length, 0);
});

test("matrix totals count each assertion once and the tint scales linearly from the 12% floor to the cap", () => {
  const site = buildSite(kb({ assertion: [A("a"), A("b"), A("c", { kind: "risk" })], concept: [C("x", { inn: ["a", "b", "c"] }), C("y", { inn: ["a"] }), C("z")] }));
  const m = matrix(site);
  assert.deepEqual(m.columns.map((c) => c.kind), ["fact", "risk"]);
  assert.deepEqual(m.rows.map((r) => r.cells), [[2, 1], [1, 0]]);
  assert.deepEqual(m.totals, [2, 1]);
  assert.equal(m.max, 2);
  assert.deepEqual(m.emptyConcepts, ["concepts/z.md"]);
  assert.equal(tintPercent(0, 10, 60), 0);
  assert.equal(tintPercent(1, 10, 60), 12);
  assert.equal(tintPercent(10, 10, 60), 60);
  assert.equal(tintPercent(4, 7, 50), 12 + (50 - 12) / 2);
  assert.equal(tintPercent(1, 1, 60), 60);
});

test("the kind conflict gap reports both keys recorded with different values", () => {
  const site = buildSite(kb({ assertion: [A("x", { kind: "fact", legacyKind: "tribal-knowledge" }), A("same", { kind: "risk", legacyKind: "risk" }), A("legacy", { kind: "assumption", legacyKind: "assumption" })] }));
  assert.deepEqual(gap(site, "kind-conflict").items, ["assertions/x.md"]);
  assert.equal(gap(site, "kind-conflict").command, "/ao-kb:curate");
});

test("extraction IDs resolve to assertions; unresolved IDs are counted by ID and keep their raw value", () => {
  const site = buildSite(kb({ assertion: [A("a"), A("b")], source: [S("sources/s.md", { extractedAssertionIds: ["a-a", "b", "a-zzz", "a-a"] }), S("sources/t.md", { recordedDate: "2024-01-01" })] }));
  assert.deepEqual(site.extraction.get("sources/s.md"), { resolved: ["assertions/a.md", "assertions/b.md"], unresolved: ["a-zzz"] });
  assert.equal(gap(site, "extraction-unresolved").count, 1);
  assert.deepEqual(gap(site, "source-no-extraction").items, ["sources/t.md"]);
  assert.deepEqual(sourceMark(site, "sources/t.md"), { r: 3.2, group: null, resolved: 0, basis: "recorded" });
});

test("a source mark takes a kind group's colour only by strict majority", () => {
  const assertion = [A("f1"), A("f2"), A("r1", { kind: "risk" }), A("r2", { kind: "risk" })];
  const site = buildSite(kb({ assertion, source: [S("sources/even.md", { extractedAssertionIds: ["a-f1", "a-r1"] }), S("sources/most.md", { extractedAssertionIds: ["a-f1", "a-f2", "a-r1"] })] }));
  assert.equal(sourceMark(site, "sources/even.md").group, null);
  assert.equal(sourceMark(site, "sources/most.md").group, "statements");
  assert.equal(sourceMark(site, "sources/most.md").r, 3.2 + 1.45 * Math.sqrt(3));
});

test("no two topic-map nodes overlap at 48 concepts with no Related Concepts, and unlabelled names are counted", () => {
  const assertion = Array.from({ length: 190 }, (_, i) => A(`p${i}`, { kind: i % 5 ? "principle" : "fact" }));
  const concept = Array.from({ length: 48 }, (_, c) => C(`t${c}`, { title: `Topic ${c} on rather long design principles here`, inn: assertion.filter((_, i) => i % 48 === c || (c < 6 && i % 7 === c)).map((a) => a.path.slice(11, -3)) }));
  const l = layout(buildSite(kb({ assertion, concept })));
  assert.equal(l.height, 680, "250 + 9 · 48 is past the 680px cap");
  assert.equal(l.edges.length, 0);
  for (let i = 0; i < l.nodes.length; i++)
    for (let j = i + 1; j < l.nodes.length; j++) {
      const a = l.nodes[i], b = l.nodes[j];
      assert.ok(Math.hypot(a.x - b.x, a.y - b.y) >= a.r + b.r, `${a.path} overlaps ${b.path}`);
    }
  assert.equal(l.unlabelled, l.nodes.filter((n) => n.label.hidden).length);
  for (const n of l.nodes) assert.ok(n.x - n.r >= 0 && n.x + n.r <= l.width && n.y - n.r >= 0 && n.y + n.r <= l.height);
  const labelled = l.nodes.filter((n) => !n.label.hidden).map((n) => n.label);
  const hit = (a, b) => a.x < b.x + b.w && b.x < a.x + a.w && a.y < b.y + b.h && b.y < a.y + a.h;
  for (let i = 0; i < labelled.length; i++) for (let j = i + 1; j < labelled.length; j++) assert.ok(!hit(labelled[i], labelled[j]));
  assert.equal(new Set(l.order).size, l.nodes.length);
  const members = (i) => l.nodes[i].members;
  for (let k = 1; k < l.order.length; k++) assert.ok(members(l.order[k - 1]) >= members(l.order[k]));
});

test("topic map radii, hollow nodes, arcs threshold, height bounds, and the scoped crop", () => {
  const site = buildSite(kb({ assertion: [A("a"), A("b"), A("c"), A("d")], concept: [C("big", { inn: ["a", "b", "c", "d"], related: ["mid"] }), C("mid", { inn: ["a", "b"] }), C("none")] }));
  const l = layout(site);
  const node = (p) => l.nodes.find((n) => n.path === `concepts/${p}.md`);
  assert.equal(node("big").r, 22);
  assert.equal(node("mid").r, 4 + 18 * Math.sqrt(2 / 4));
  assert.equal(node("none").r, 5);
  assert.equal(node("none").hollow, true);
  assert.equal(node("big").arcs, true);
  assert.equal(node("none").arcs, false);
  assert.deepEqual(node("big").shares, { statements: 4, intent: 0, risks: 0, other: 0 });
  assert.deepEqual(l.edges.length, 1);
  assert.equal(l.height, 430);
  const many = layout(buildSite(kb({ concept: Array.from({ length: 60 }, (_, i) => C(`c${i}`)) })));
  assert.equal(many.height, 680);
  const box = cropBox(l, new Set(["concepts/big.md"]));
  assert.ok(box.x <= node("big").x - node("big").r && box.x + box.w >= node("big").x + node("big").r);
  assert.equal(cropBox(l, new Set()), null);
});

test("the scoped crop reports exactly what it draws, and a lone node is nothing to draw", () => {
  const node = (name, x, y, r = 8) => ({ path: `concepts/${name}.md`, x, y, r, label: { x: x + r + 4, y: y - 8, w: 40, h: 16 } });
  // a and its neighbour b sit together; c is far away; the box is at least 320 wide.
  const l = { width: 760, height: 680, nodes: [node("a", 100, 100), node("b", 180, 120), node("c", 700, 600)], edges: [[0, 1], [0, 2], [1, 2]] };
  const near = neighbourhood(l, new Set(["concepts/a.md"]));
  assert.deepEqual([...near.drawn], ["concepts/a.md", "concepts/b.md"]);
  assert.deepEqual(near.faint, ["concepts/b.md"]);
  assert.deepEqual(near.edges, [[0, 1]], "a line to a node outside the crop is not drawn");
  assert.equal(near.outside, 1);
  assert.deepEqual(near.box, cropBox(l, new Set(["concepts/a.md"])));
  const alone = neighbourhood(l, new Set(["concepts/c.md"]));
  assert.deepEqual([[...alone.drawn], alone.faint, alone.edges, alone.outside], [["concepts/c.md"], [], [], 2]);
  assert.equal(neighbourhood(l, new Set()), null);

  // Three concepts spread over the overview: one alone draws nothing to place it against, while
  // a concept page's scope (it and its Related Concepts) draws both.
  const site = buildSite(kb({ assertion: [A("a")], concept: [C("x", { inn: ["a"], related: ["y"] }), C("y"), C("z")] }));
  const map = (...cs) => scopedPair(site, { topics: new Set(cs.map((c) => `concepts/${c}.md`)), sources: new Set() }).map;
  assert.deepEqual(["x", "y", "z"].map((c) => [map(c).drawable, map(c).missing]), Array(3).fill([false, "alone"]));
  assert.deepEqual([map("x", "y").missing, map("x", "y").hood.edges.length], [null, 1]);
});

test("short names cut at a word boundary past 30 characters", () => {
  assert.equal(short("Short title"), "Short title");
  assert.equal(short("Customer onboarding and the payments review flow"), "Customer onboarding and the…");
  assert.equal(short("Exactly thirty characters long here"), "Exactly thirty characters long…");
  assert.equal(short("Supercalifragilisticexpialidocious-and-more"), "Supercalifragilisticexpialidoc…");
});

test("timeline row labels keep the last folder whole, so sibling folders with a long shared parent read differently", () => {
  const parent = "sources/client-workshops-and-discovery-sessions/phase-two-interviews";
  const labels = ["alpha", "beta", "gamma-extended-name"].map((f) => rowLabel(`${parent}/${f}`));
  assert.equal(new Set(labels).size, 3);
  for (const [i, f] of ["alpha", "beta", "gamma-extended-name"].entries()) assert.ok(labels[i].endsWith(`/ ${f}`), labels[i]);
  assert.ok(labels.every((l) => l.length <= 24 || l === "… / gamma-extended-name"));
  assert.equal(rowLabel("sources/calls"), "calls");
  assert.equal(rowLabel("sources"), "(top level)");
  assert.equal(rowLabel("products/acme/sources/calls"), "calls");
});

test("timeline rows by folder, arrow order by date then folder then path, and one shared time domain", () => {
  const site = buildSite(medium());
  const t = timeline(site);
  assert.deepEqual(t.rows.map((r) => r.folder), ["sources/f0", "sources/f1", "sources/f2"]);
  const dates = t.order.map((p) => site.dates.get(p).date);
  assert.deepEqual(dates, [...dates].sort());
  const axis = timeAxis(site);
  assert.equal(axis.u(Date.parse(`${dates[0]}T00:00:00Z`)), 0);
  assert.ok(Math.abs(axis.u(Date.parse(`${dates.at(-1)}T00:00:00Z`)) - 1) < 1e-9);
  const gapped = buildSite(kb({ source: ["2020-01-01", "2020-01-20", "2024-06-01", "2024-06-03"].map((d) => S(`sources/${d}-s.md`)) }));
  assert.equal(timeAxis(gapped).segments.length, 2, "a long empty stretch is an axis break, never a trim");
});

test("KB title from _moc.md without its Map of Content prefix, else the vault name; start-here order", () => {
  const mocs = [{ ...rec("MOCs/zeta.md"), title: "Zeta" }, { ...rec("_moc.md"), title: "Map of Content — Harbor KB" }, { ...rec("MOCs/alpha.md"), title: "Alpha" }];
  assert.deepEqual(kbTitle(mocs, "vault"), { title: "Harbor KB", from: "moc" });
  assert.deepEqual(kbTitle([], "vault"), { title: "vault", from: "vault" });
  assert.deepEqual(kbTitle([{ ...rec("_moc.md"), title: "Map of Content" }], "vault"), { title: "vault", from: "vault" });
  assert.deepEqual(buildSite(kb({ mapOfContent: mocs })).mocs.map((m) => m.path), ["_moc.md", "MOCs/alpha.md", "MOCs/zeta.md"]);
});

test("layout timing at 21, 48, and 200 concepts", () => {
  for (const n of [21, 48, 200]) {
    const o = medium(n), runs = [];
    for (let i = 0; i < 6; i++) {
      const site = buildSite(o), t0 = performance.now();
      layout(site);
      runs.push(performance.now() - t0);
    }
    const warm = runs.slice(1).sort((a, b) => a - b);
    console.log(`layout ${n} concepts: first ${runs[0].toFixed(1)} ms, warm median ${warm[2].toFixed(1)} ms`);
  }
});

test("code spans use complete equal-length backtick runs and preserve paragraph positions", () => {
  const resolve = (ref) => ref === "claim" ? "claim" : undefined;
  for (const span of ["``a ` [[claim]] b``", "```a `` [[claim]] ` b```", "``a ``` [[claim]] b``", "``a `\n[[claim]] b``"]) {
    assert.equal(bodyLinkParagraph(`Use ${span}`, "claim", resolve), -1, span);
    assert.equal(bodyLinkParagraph(`Use ${span}\n\nAuthored [[claim]].`, "claim", resolve), 1, span);
  }
  for (const literal of ["``[[claim]]`", "`[[claim]]``", "``before\n\n[[claim]]``", "``before\r\n\r\n[[claim]]``"]) {
    assert.notEqual(bodyLinkParagraph(literal, "claim", resolve), -1, literal);
  }
});
