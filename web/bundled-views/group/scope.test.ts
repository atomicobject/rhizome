import { expect, it } from "vitest";

import { AGGREGATE, AGGREGATE_PARTS, SCOPE_GROUPS, SUMMARIES } from "./__fixtures__/aggregate.ts";
import { TYPE_DOCS } from "./__fixtures__/groups.ts";
import { aggregatePath, parseAggregate, UNTYPED } from "./aggregate.ts";
import {
  declaredRelations,
  edgeKey,
  groupNodeId,
  linkModel,
  scopeModel,
  scopeOf,
  type Scope,
  type ScopeModel,
} from "./scope.ts";

const members =
  AGGREGATE.members ??
  (() => {
    throw new Error("no members");
  })();

const pairs = AGGREGATE.links ?? [];

function model(scope: Scope, expanded: string[] = []): ScopeModel {
  const built = scopeModel({
    scope,
    groups: SCOPE_GROUPS.groups,
    members,
    summaries: SUMMARIES,
    expanded: new Set(expanded),
  });

  if (!built) throw new Error("no scope");

  return built;
}

const planning = model({ kind: "group", group: "Planning" });

const workspace = model({ kind: "workspace" });

it("reads one part per request and leaves the others out", () => {
  expect(aggregatePath("links")).toBe("/api/v1/ontology/shape?parts=links");
  const parsed = parseAggregate(AGGREGATE_PARTS.links);

  expect(parsed.members).toBeNull();
  expect(parsed.folders).toBeNull();
  expect(parsed.links?.[1]).toMatchObject({ a: "Area", b: "Story", links: 5, relationLinks: 4 });
  expect(parsed.facts).toEqual({
    rebuilding: false,
    totalNotes: 49,
    typedNotes: 29,
    untypedNotes: 20,
    ambiguousNotes: 3,
  });
  // Change times arrive in seconds and read as milliseconds.
  expect(members.types.get("Story")?.lastChanged).toBeGreaterThan(1e12);
  expect(parseAggregate(null).facts.totalNotes).toBe(0);
});

it("maps the view context to a scope", () => {
  expect(scopeOf({ kind: "workspace" })).toEqual({ kind: "workspace" });
  expect(scopeOf({ kind: "group", group: "Planning" })).toEqual({
    kind: "group",
    group: "Planning",
  });
  expect(scopeOf({ kind: "type", type: "Story" })).toBeNull();
});

it("models a group's members, an interface standing for its implementors, most records first", () => {
  expect(planning.nodes.map((node) => [node.id, node.count])).toEqual([
    ["Area", 6],
    ["Work", 6],
    ["Release", 2],
    ["Checklist", 1],
  ]);
  expect(planning.nodeIndex.get("Work")).toMatchObject({
    kind: "interface",
    types: ["Story", "Bug"],
    issueCount: 2,
    label: "Work items",
  });
  expect(planning.nodesOf.get("Bug")).toEqual(["Work"]);
});

it("returns null for a group that no longer exists", () => {
  expect(
    scopeModel({
      scope: { kind: "group", group: "Gone" },
      groups: SCOPE_GROUPS.groups,
      members,
      summaries: SUMMARIES,
      expanded: new Set(),
    }),
  ).toBeNull();
});

it("leaves embedded types out of a group's members", () => {
  const other = model({ kind: "group", group: "Other" });

  expect(other.nodes.map((node) => node.id)).toEqual(["Meeting", "Notice"]);
  expect(other.types.has("Task")).toBe(false);
});

it("collapses named groups at All notes and adds ungrouped types and untyped notes", () => {
  expect(workspace.nodes.map((node) => [node.id, node.count])).toEqual([
    [UNTYPED, 20],
    [groupNodeId("Planning"), 15],
    [groupNodeId("Library"), 5],
    ["Meeting", 5],
    [groupNodeId("People"), 2],
    ["Notice", 0],
  ]);
  expect(workspace.sections.map((section) => section.group?.name ?? null)).toEqual([
    "Library",
    "People",
    "Planning",
    null,
  ]);
  expect(workspace.nodesOf.get("Story")).toEqual([groupNodeId("Planning")]);
  expect(workspace.types.has("Task")).toBe(false);
});

it("expands a named group into its members in place", () => {
  const expanded = model({ kind: "workspace" }, ["Planning"]);

  expect(expanded.nodes.map((node) => node.id)).toEqual([
    UNTYPED,
    "Area",
    "Work",
    groupNodeId("Library"),
    "Meeting",
    groupNodeId("People"),
    "Release",
    "Checklist",
    "Notice",
  ]);
  expect(expanded.nodeIndex.get("Work")?.group).toBe("Planning");
});

it("rolls type-pair links up to member edges, self links, and outside neighbors", () => {
  const links = linkModel(planning, pairs, SUMMARIES);

  expect(links.edges.map((edge) => [edge.key, edge.links, edge.relation])).toEqual([
    [edgeKey("Area", "Work"), 6, true],
    [edgeKey("Checklist", "Release"), 1, true],
    [edgeKey("Release", "Work"), 1, true],
  ]);
  expect(links.edgeIndex.get(edgeKey("Area", "Work"))).toMatchObject({
    relationLinks: 5,
    plainLinks: 1,
    fields: [
      { type: "Story", field: "area", count: 4 },
      { type: "Bug", field: "area", count: 1 },
    ],
  });
  // Bug links to Story inside the Work member, and Area records link to each other.
  expect([...links.self]).toEqual([
    ["Area", 2],
    ["Work", 2],
  ]);
  expect(links.outside.map((neighbor) => [neighbor.id, neighbor.label, neighbor.links])).toEqual([
    [UNTYPED, "Untyped notes", 3],
    ["Person", "People", 2],
  ]);
  expect(links.outside[0].byMember.get("Work")).toBe(3);
});

it("classifies an edge by whether relation links are at least half of it", () => {
  const scope = model({ kind: "group", group: "Planning" });

  const links = linkModel(
    scope,
    [
      { a: "Area", b: "Release", links: 4, relationLinks: 2, plainLinks: 2, fields: [] },
      { a: "Checklist", b: "Release", links: 3, relationLinks: 1, plainLinks: 2, fields: [] },
    ],
    SUMMARIES,
  );

  expect(links.edgeIndex.get(edgeKey("Area", "Release"))?.relation).toBe(true);
  expect(links.edgeIndex.get(edgeKey("Checklist", "Release"))?.relation).toBe(false);
});

it("keeps a collapsed group's internal links in its hover, not as edges, at All notes", () => {
  const links = linkModel(workspace, pairs, SUMMARIES);
  const planningId = groupNodeId("Planning");

  expect(links.self.get(planningId)).toBe(12);
  expect(links.outside).toEqual([]);
  expect(links.edges.map((edge) => [edge.a, edge.b, edge.links, edge.relation])).toEqual([
    ["Meeting", UNTYPED, 6, false],
    ["Meeting", groupNodeId("People"), 4, false],
    [UNTYPED, groupNodeId("Planning"), 3, false],
    [UNTYPED, groupNodeId("Library"), 2, false],
    [groupNodeId("People"), groupNodeId("Planning"), 2, true],
  ]);
});

it("does not draw a type's own links as an edge between two groups that share it", () => {
  const shared = scopeModel({
    scope: { kind: "workspace" },
    groups: [
      ...SCOPE_GROUPS.groups,
      { name: "Triage", members: [{ ...SCOPE_GROUPS.groups[2].members[0], children: [] }] },
    ],
    members,
    summaries: SUMMARIES,
    expanded: new Set(),
  });

  if (!shared) throw new Error("no scope");
  const links = linkModel(shared, pairs, SUMMARIES);

  expect(shared.nodesOf.get("Story")).toEqual([groupNodeId("Planning"), groupNodeId("Triage")]);
  // Area–Story, Area–Bug, and Release–Bug cross from Planning's own types into
  // Triage's; Bug–Story lies inside both groups, so it counts in each and
  // joins neither to the other.
  expect(links.edgeIndex.get(edgeKey(groupNodeId("Planning"), groupNodeId("Triage")))?.links).toBe(
    7,
  );
  expect(links.self.get(groupNodeId("Triage"))).toBe(2);
  expect(links.self.get(groupNodeId("Planning"))).toBe(12);
});

it("marks each declared relation toward another member used or unused", () => {
  const relations = declaredRelations(planning, TYPE_DOCS, pairs);

  expect(
    relations.map((relation) => [relation.member, relation.field, relation.target, relation.count]),
  ).toEqual([
    ["Work", "area", "Area", 5],
    ["Release", "works", "Work", 1],
    ["Release", "checklist", "Checklist", 1],
    ["Release", "area", "Area", 0],
  ]);
});
