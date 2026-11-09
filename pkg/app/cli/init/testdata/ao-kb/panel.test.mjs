import assert from "node:assert/strict";
import test from "node:test";
import { buildSite } from "../../templates/starters/ao-kb/module/views/knowledge/lib/site.ts";
import {
  RECENT_MAX,
  addRecent,
  findByTitle,
  kindCounts,
  locatorBox,
  locatorScope,
  parseRecent,
  recentEntry,
  recentKey,
  titleIndex,
} from "../../templates/starters/ao-kb/module/views/knowledge/lib/panel.ts";

// Synthetic records shaped like the orientation query (SPEC-0135 R5).
const rec = (path, title = path.split("/").pop().replace(/\.md$/, "")) => ({ ref: { notePath: path, kind: "NOTE", typeName: "X" }, path, aoId: null, title });
const A = (slug, kind = "fact") => ({ ...rec(`assertions/${slug}.md`), headline: null, kind, legacyKind: null, confidence: "validated", provenanceSource: null });
const C = (slug, { out = [], related = [], aliases = [], title } = {}) => ({
  ...rec(`concepts/${slug}.md`, title ?? slug),
  name: null, headline: null, aliases,
  linkedAssertions: out.map((s) => ({ path: `assertions/${s}.md` })),
  linkingAssertions: [],
  relatedConceptsSection: { relatedConcepts: related.map((s) => ({ path: `concepts/${s}.md` })) },
});
const S = (path) => ({ ...rec(path), name: null, headline: null, recordedDate: null, extractedAssertionIds: [] });

const site = buildSite({
  assertion: [A("fare-flat"), A("ramp", "need"), A("storm", "risk"), A("crew", "risk"), A("bare", null)],
  concept: [
    C("tickets", { out: ["fare-flat"], related: ["timetable"], aliases: ["Fares"], title: "Tickets" }),
    C("timetable", { out: ["storm", "crew"], title: "Timetable" }),
    C("weather", { title: "Weather" }),
  ],
  source: [S("sources/meetings/kickoff.md"), S("sources/docs/handbook.md")],
  artifact: [],
  mapOfContent: [],
});
const label = (r) => r.title;
const index = titleIndex(site, label);

test("find matches titles and concept aliases, exact and prefix before substring", () => {
  assert.deepEqual(findByTitle(index, "time").map((m) => m.path), ["concepts/timetable.md"]);
  // "fares" is Tickets' alias; the match says which alias it came through.
  const [fares] = findByTitle(index, "FARES");
  assert.equal(fares.path, "concepts/tickets.md");
  assert.equal(fares.alias, "Fares");
  // At one rank a title match comes before an alias match.
  assert.deepEqual(findByTitle(index, "fare").map((m) => m.path), ["assertions/fare-flat.md", "concepts/tickets.md"]);
  assert.deepEqual(findByTitle(index, "  "), []);
  assert.equal(findByTitle(index, "a", 2).length, 2);
});

test("a record whose title and alias both match appears once", () => {
  const only = titleIndex(buildSite({ assertion: [], concept: [C("fares", { aliases: ["fares"], title: "Fares" })], source: [], artifact: [], mapOfContent: [] }), label);
  assert.equal(findByTitle(only, "fares").length, 1);
  assert.equal(findByTitle(only, "fares")[0].alias, undefined);
});

test("Recent keeps subjects newest first, once each, bounded", () => {
  const node = (p) => recentEntry({ kind: "node", type: "Concept", ref: { notePath: p, kind: "NOTE" } });
  let list = [];
  list = addRecent(list, node("concepts/a.md"));
  list = addRecent(list, node("concepts/b.md"));
  list = addRecent(list, node("concepts/a.md"));
  assert.deepEqual(list.map((e) => e.ref.notePath), ["concepts/a.md", "concepts/b.md"]);
  // The same subject again keeps the same array, so nothing re-renders or rewrites storage.
  assert.equal(addRecent(list, node("concepts/a.md")), list);
  for (let i = 0; i < RECENT_MAX + 5; i++) list = addRecent(list, node(`concepts/n${i}.md`));
  assert.equal(list.length, RECENT_MAX);
  assert.equal(list[0].ref.notePath, `concepts/n${RECENT_MAX + 4}.md`);
});

test("Recent records records and collections, never groups or the workspace", () => {
  assert.equal(recentEntry({ kind: "group", group: "Knowledge" }), null);
  assert.equal(recentEntry({ kind: "workspace" }), null);
  assert.equal(recentEntry({ kind: "standalone" }), null);
  assert.deepEqual(recentEntry({ kind: "type", type: "Source" }), { key: "type:Source", kind: "type", name: "Source" });
  const e = recentEntry({ kind: "node", type: "Assertion", ref: { notePath: "assertions/x.md", kind: "SECTION", fragment: "f", nodeId: "n1", extra: 1 } });
  assert.deepEqual(e, { key: "node:assertions/x.md#n1", kind: "node", type: "Assertion", ref: { notePath: "assertions/x.md", kind: "SECTION", fragment: "f", nodeId: "n1" } });
});

test("stored Recent is read defensively and keyed by vault and experience", () => {
  assert.equal(recentKey("v1", "e1"), "rhizome.ao-kb.panel.recent:v1:e1");
  assert.notEqual(recentKey("v1", "e1"), recentKey("v2", "e1"));
  assert.deepEqual(parseRecent(null), []);
  assert.deepEqual(parseRecent("not json"), []);
  assert.deepEqual(parseRecent('{"a":1}'), []);
  const good = { key: "type:Source", kind: "type", name: "Source" };
  assert.deepEqual(parseRecent(JSON.stringify([good, { key: 3 }, { kind: "node", key: "node:x", type: "T", ref: {} }])), [good]);
});

test("the locator's scope: a concept and its Related Concepts, an assertion's concepts, nothing for others", () => {
  const ctx = (type, path) => ({ kind: "node", type, ref: { notePath: path, kind: "NOTE" } });
  const concept = locatorScope(site, ctx("Concept", "concepts/tickets.md"));
  assert.deepEqual([...concept.topics], ["concepts/tickets.md", "concepts/timetable.md"]);
  assert.equal(concept.mark, "concepts/tickets.md");
  const assertion = locatorScope(site, ctx("Assertion", "assertions/storm.md"));
  assert.deepEqual([...assertion.topics], ["concepts/timetable.md"]);
  assert.equal(assertion.mark, undefined);
  // A record the site does not know, or a page that is not a record, collapses the locator.
  assert.equal(locatorScope(site, ctx("ReferenceDoc", "docs/ref.md")), null);
  assert.equal(locatorScope(site, { kind: "group", group: "Knowledge" }), null);
  // Sources and artifacts wait for their scope read, then take its concepts.
  assert.equal(locatorScope(site, ctx("Source", "sources/meetings/kickoff.md")), undefined);
  const source = locatorScope(site, ctx("Source", "sources/meetings/kickoff.md"), {
    source: [{ path: "assertions/storm.md", sourcesSection: { citations: [{ recordedLink: "[[kickoff]]", sources: [{ path: "sources/meetings/kickoff.md" }] }] } }],
  });
  assert.deepEqual([...source.topics], ["concepts/timetable.md"]);
});

test("kind counts are raw kinds by count, then name; folders come from the library", () => {
  assert.deepEqual(kindCounts(site), [{ kind: "risk", count: 2 }, { kind: "fact", count: 1 }, { kind: "need", count: 1 }]);
});

test("the locator box frames every node of the map and keeps its aspect", () => {
  const nodes = [{ x: 100, y: 50, r: 10 }, { x: 300, y: 250, r: 5 }];
  const box = locatorBox(nodes);
  assert.ok(box.x <= 90 && box.y <= 40);
  assert.ok(box.x + box.w >= 305 && box.y + box.h >= 255);
  assert.equal(locatorBox([]), null);
});
