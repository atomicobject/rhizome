import { expect, it } from "vitest";

import { AGGREGATE, SCOPE_GROUPS, SUMMARIES } from "./__fixtures__/aggregate.ts";
import { TYPE_DOCS } from "./__fixtures__/groups.ts";
import { layoutMap, placeEdgeLabels, type LayoutInput } from "./map-layout.ts";
import {
  coverage,
  folderLines,
  lifecycleSummary,
  memberRows,
  type MemberRow,
  type NodeRow,
} from "./members.ts";
import { scopeModel, type Scope } from "./scope.ts";

const members = AGGREGATE.members!;

const pairs = AGGREGATE.links!;

function rows(scope: Scope, expanded: string[] = []) {
  const set = new Set(expanded);

  const model = scopeModel({
    scope,
    groups: SCOPE_GROUPS.groups,
    members,
    summaries: SUMMARIES,
    expanded: set,
  });

  if (!model) throw new Error("no scope");

  return memberRows(model, {
    members,
    pairs,
    docs: TYPE_DOCS,
    summaries: SUMMARIES,
    expanded: set,
  });
}

const isNode = (row: MemberRow): row is NodeRow => row.kind !== "heading";

function rowOf(list: readonly MemberRow[], label: string): NodeRow {
  const found = list.filter(isNode).find((row) => row.node.label === label);

  if (!found) throw new Error(`no row ${label}`);

  return found;
}

it("lists a group's members most records first, an interface followed by its implementors", () => {
  const planning = rows({ kind: "group", group: "Planning" });

  expect(
    planning.map((row) => (isNode(row) ? [row.kind, row.node.label, row.depth] : row.kind)),
  ).toEqual([
    ["member", "Areas", 0],
    ["member", "Work items", 0],
    ["implementor", "Stories", 1],
    ["implementor", "Bugs", 1],
    ["member", "Releases", 0],
    ["member", "Checklists", 0],
  ]);
});

it("counts linked share, links to other members, and links leaving the group", () => {
  const planning = rows({ kind: "group", group: "Planning" });
  const work = rowOf(planning, "Work items");

  // Story's records linking to Area (3) and Bug's linking to Area and Release (1);
  // Story's records linking only to untyped notes do not count.
  expect(work.linkedShare).toBeCloseTo(4 / 6);
  expect(work).toMatchObject({ links: 7, outside: 5 });
  expect(rowOf(planning, "Areas")).toMatchObject({ links: 6, outside: 0 });
  expect(rowOf(planning, "Checklists").linkedShare).toBe(1);
});

it("summarizes lifecycles by stage, names the active count, and says when there is none", () => {
  const planning = rows({ kind: "group", group: "Planning" });

  expect(rowOf(planning, "Work items").lifecycle).toEqual({
    segments: [
      { name: "backlog", label: "Backlog", tone: "neutral", stage: "open", count: 1 },
      { name: "doing", label: "Doing", tone: "progress", stage: "active", count: 3 },
      { name: "blocked", label: "Blocked", tone: "warning", stage: "active", count: 1 },
      { name: "done", label: "Done", tone: "success", stage: "done", count: 1 },
    ],
    active: 4,
    activeLabel: "active",
  });
  expect(rowOf(planning, "Releases").lifecycle?.activeLabel).toBe("shipping");
  expect(rowOf(planning, "Areas").lifecycle).toBeNull();
  // Values documentation does not know keep the aggregate's order after the known ones.
  expect(
    lifecycleSummary(
      {
        name: "X",
        count: 2,
        issueCount: 0,
        lastChanged: null,
        gaps: [],
        lifecycle: { field: "s", values: [{ name: "odd", count: 2 }] },
      },
      [],
    )?.segments,
  ).toEqual([{ name: "odd", label: "odd", tone: "neutral", stage: null, count: 2 }]);
});

it("lists gap fields with empty counts, marking only required fields", () => {
  const planning = rows({ kind: "group", group: "Planning" });

  expect(rowOf(planning, "Work items").gaps).toEqual([
    { field: "nextStep", label: "Next step", empty: 3, required: false },
  ]);
  expect(rowOf(planning, "Areas").gaps).toEqual([
    { field: "category", label: "Category", empty: 1, required: false },
  ]);
});

it("lists named groups, then ungrouped types under a heading, then untyped notes at All notes", () => {
  const all = rows({ kind: "workspace" });

  expect(
    all.map((row) =>
      isNode(row) ? [row.kind, row.node.label, row.depth] : [row.kind, row.label, row.types],
    ),
  ).toEqual([
    ["group", "Library", 0],
    ["group", "People", 0],
    ["group", "Planning", 0],
    ["heading", "No group", 2],
    ["member", "Meetings", 1],
    ["member", "Notices", 1],
    ["member", "Untyped notes", 0],
  ]);
  expect(rowOf(all, "Planning")).toMatchObject({
    expanded: false,
    outside: null,
    lifecycle: undefined,
  });
  // Every link to another type counts at All notes: Meeting's to People and untyped notes.
  expect(rowOf(all, "Meetings")).toMatchObject({ links: 10, linkedShare: 3 / 5 });
  expect(rowOf(all, "Untyped notes")).toMatchObject({ links: 11, linkedShare: null });
  expect(rowOf(all, "Notices").linkedShare).toBeNull();
});

it("expands a named group's row into its members", () => {
  const all = rows({ kind: "workspace" }, ["Planning"]);
  const index = all.findIndex((row) => isNode(row) && row.node.label === "Planning");

  expect(rowOf(all, "Planning").expanded).toBe(true);
  expect(
    all.slice(index + 1, index + 4).map((row) => (isNode(row) ? row.node.label : row.kind)),
  ).toEqual(["Areas", "Work items", "Stories"]);
  expect(rowOf(all, "Planning").links).toBe(5);
});

it("counts an expanded interface's links between its own implementors in linked share at All notes", () => {
  const story = members.types.get("Story")!;
  const bug = members.types.get("Bug")!;

  // Stories link only to Bugs, and Bugs only to Stories, both inside Work.
  const figures = {
    ...members,
    types: new Map([
      ...members.types,
      ["Story", { ...story, targetSets: [{ types: ["Bug"], records: 3 }] }],
      ["Bug", { ...bug, targetSets: [{ types: ["Story"], records: 1 }] }],
    ]),
  };

  const set = new Set(["Planning"]);

  const model = scopeModel({
    scope: { kind: "workspace" },
    groups: SCOPE_GROUPS.groups,
    members: figures,
    summaries: SUMMARIES,
    expanded: set,
  });

  if (!model) throw new Error("no scope");

  const all = memberRows(model, {
    members: figures,
    pairs,
    docs: TYPE_DOCS,
    summaries: SUMMARIES,
    expanded: set,
  });

  expect(rowOf(all, "Work items").linkedShare).toBeCloseTo(4 / 6);
  expect(rowOf(all, "Stories").linkedShare).toBeCloseTo(3 / 4);
});

it("lists folders with untyped notes, most first, with the two types they link to most", () => {
  expect(folderLines(AGGREGATE.folders!, SUMMARIES)).toEqual([
    {
      folder: "Notes",
      untyped: 12,
      total: 30,
      share: 0.4,
      linksTo: [
        { type: "Meeting", label: "Meetings", count: 4 },
        { type: "Person", label: "People", count: 2 },
      ],
    },
    { folder: "Inbox", untyped: 8, total: 8, share: 1, linksTo: [] },
  ]);
});

it("names empty, unlinked, and embedded types for the coverage line", () => {
  expect(coverage(members, pairs, SUMMARIES)).toEqual({
    empty: [{ name: "Notice", label: "Notices", count: 0 }],
    unlinked: [{ name: "Glossary", label: "Library glossaries", count: 3 }],
    embedded: [{ name: "Task", label: "Tasks", count: 7 }],
  });
});

const LAYOUT: LayoutInput = {
  width: 760,
  height: 340,
  nodes: [
    { id: "Area", radius: 30, label: "Areas 6" },
    { id: "Work", radius: 30, label: "Work items 6" },
    { id: "Release", radius: 17, label: "Releases 2" },
    { id: "Checklist", radius: 12, label: "Checklists 1" },
    { id: "Lonely", radius: 8, label: "Lonelies 1" },
  ],
  edges: [
    { a: "Area", b: "Work", links: 6 },
    { a: "Release", b: "Work", links: 1 },
    { a: "Checklist", b: "Release", links: 1 },
  ],
  outside: [{ id: "Person", label: "People 2 links", byMember: new Map([["Work", 2]]) }],
};

it("lays the map out the same way every time, inside the box", () => {
  const first = layoutMap(LAYOUT);
  const second = layoutMap(LAYOUT);

  expect([...second.positions]).toEqual([...first.positions]);
  expect([...second.labels]).toEqual([...first.labels]);

  for (const point of first.positions.values()) {
    expect(point.x).toBeGreaterThanOrEqual(0);
    expect(point.x).toBeLessThanOrEqual(LAYOUT.width);
    expect(point.y).toBeGreaterThanOrEqual(0);
    expect(point.y).toBeLessThanOrEqual(LAYOUT.height);
  }
});

it("keeps linked members apart in a canvas narrower than the ring's padding", () => {
  const layout = layoutMap({ ...LAYOUT, width: 380, height: 400 });
  const points = ["Area", "Work", "Release", "Checklist"].map((id) => layout.positions.get(id));
  const distinct = new Set(points.map((point) => `${point?.x},${point?.y}`));

  expect(distinct.size).toBe(points.length);
});

it("puts members without links in a bottom row and outside neighbors on the ring", () => {
  const layout = layoutMap(LAYOUT);

  expect(layout.loose).toEqual(["Lonely"]);
  expect(layout.looseTop).toBe(300);
  expect(layout.positions.get("Lonely")?.y).toBe(315);

  for (const id of ["Area", "Work", "Release", "Checklist"])
    expect(layout.positions.get(id)?.y).toBeLessThan(300);
  expect(layout.positions.has("Person")).toBe(true);

  // Rows grow to fit large unlinked members, between the row line and the box's bottom.
  const large = layoutMap({ ...LAYOUT, edges: [{ a: "Release", b: "Checklist", links: 1 }] });
  const top = large.looseTop ?? 0;

  for (const id of ["Area", "Work", "Lonely"]) {
    const node = LAYOUT.nodes.find((candidate) => candidate.id === id);
    const y = large.positions.get(id)?.y ?? 0;
    expect(y - (node?.radius ?? 0)).toBeGreaterThanOrEqual(top);
    expect(y + (node?.radius ?? 0)).toBeLessThan(LAYOUT.height);
  }
});

it("keeps linked members placed when unlinked rows would fill the box", () => {
  const crowd = Array.from({ length: 13 }, (_, n) => ({
    id: `Loose${n}`,
    radius: 30,
    label: "A long member name 9",
  }));

  const layout = layoutMap({ ...LAYOUT, nodes: [...LAYOUT.nodes, ...crowd] });

  for (const point of layout.positions.values()) {
    expect(Number.isFinite(point.x)).toBe(true);
    expect(Number.isFinite(point.y)).toBe(true);
  }
});

it("draws the row line at the top of the unlinked rows", () => {
  const crowd = Array.from({ length: 10 }, (_, n) => ({
    id: `Loose${n}`,
    radius: 30,
    label: "A long member name 9",
  }));

  const layout = layoutMap({ ...LAYOUT, nodes: [...LAYOUT.nodes, ...crowd] });
  const top = layout.looseTop ?? 0;

  for (const id of layout.loose) {
    const node = [...LAYOUT.nodes, ...crowd].find((candidate) => candidate.id === id);
    expect((layout.positions.get(id)?.y ?? 0) - (node?.radius ?? 0)).toBeGreaterThanOrEqual(top);
  }
});

it("spreads the members over the box when none links to another", () => {
  const layout = layoutMap({ ...LAYOUT, edges: [], outside: [] });

  expect(layout.looseTop).toBeNull();
  expect(layout.loose).toEqual([]);

  for (const node of LAYOUT.nodes) {
    const point = layout.positions.get(node.id) ?? { x: 0, y: 0 };
    expect(point.x - node.radius).toBeGreaterThanOrEqual(0);
    expect(point.x + node.radius).toBeLessThanOrEqual(LAYOUT.width);
    expect(point.y - node.radius).toBeGreaterThanOrEqual(0);
    expect(point.y + node.radius).toBeLessThanOrEqual(LAYOUT.height);
  }

  const ys = LAYOUT.nodes.map((node) => layout.positions.get(node.id)?.y ?? 0);
  expect(Math.max(...ys) - Math.min(...ys)).toBeGreaterThan(40);
});

it("moves an edge count off a node label along its edge, and drops it when nothing is clear", () => {
  const positions = new Map([
    ["A", { x: 0, y: 100 }],
    ["B", { x: 200, y: 100 }],
  ]);

  const edge = { a: "A", b: "B", links: 12, label: { key: "A|B", text: "12" } };
  // A node label across the middle of the edge.
  const middle = { x: 80, y: 80, w: 40, h: 30 };

  // 38% and 62% along still touch it; 28% clears it.
  const moved = placeEdgeLabels(positions, [edge], [middle]).get("A|B");
  expect(moved?.x).toBeCloseTo(56);
  expect(moved?.y).toBe(97);

  const covered = { x: 0, y: 80, w: 200, h: 30 };
  expect(placeEdgeLabels(positions, [edge], [covered]).has("A|B")).toBe(false);
  expect(placeEdgeLabels(positions, [{ ...edge, label: undefined }], []).size).toBe(0);
});
