import { mapLabelLine } from "./map-labels.ts";
import { expect, it } from "vitest";

import { AGGREGATE, SCOPE_GROUPS, SUMMARIES } from "./__fixtures__/aggregate.ts";
import { TYPE_DOCS } from "./__fixtures__/groups.ts";
import { INSET, layoutMap, placeEdgeLabels, type LayoutInput } from "./map-layout.ts";
import {
  coverage,
  folderLines,
  lifecycleSummary,
  memberRows,
  pluralLabelOf,
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

it("lists a group's members most records first, an interface folded until opened", () => {
  const layout = (list: MemberRow[]) =>
    list.map((row) => (isNode(row) ? [row.kind, row.node.label, row.depth] : row.kind));

  const folded = rows({ kind: "group", group: "Planning" });

  expect(layout(folded)).toEqual([
    ["member", "Areas", 0],
    ["member", "Work items", 0],
    ["member", "Releases", 0],
    ["member", "Checklists", 0],
  ]);
  expect(rowOf(folded, "Work items")).toMatchObject({ expanded: false, toggle: "interface:Work" });

  const planning = rows({ kind: "group", group: "Planning" }, ["interface:Work"]);

  expect(layout(planning)).toEqual([
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
  const all = rows({ kind: "workspace" }, ["Planning", "interface:Work"]);
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

  const set = new Set(["Planning", "interface:Work"]);

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

it("names unlabelled types in plain English", () => {
  expect(pluralLabelOf("AcceptanceCriterion", "AcceptanceCriterion")).toBe("Acceptance criteria");
  expect(pluralLabelOf("UserStory", undefined)).toBe("User stories");
  expect(pluralLabelOf("ActionItem", "ActionItem")).toBe("Action items");
  expect(pluralLabelOf("Task", "Tasks")).toBe("Tasks");
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

it("leaves members without links out of the drawing and puts outside neighbors on the ring", () => {
  const layout = layoutMap(LAYOUT);

  expect(layout.loose).toEqual(["Lonely"]);
  expect(layout.positions.has("Lonely")).toBe(false);
  expect(layout.labels.has("Lonely")).toBe(false);
  expect(layout.positions.has("Person")).toBe(true);
  expect(layout.height).toBe(LAYOUT.height);
});

it("draws a member whose only links go outside the group, so its spokes have a start", () => {
  const outside = [{ id: "Person", label: "People 1 link", byMember: new Map([["Lonely", 1]]) }];
  const layout = layoutMap({ ...LAYOUT, outside });

  expect(layout.loose).toEqual([]);
  expect(layout.positions.has("Lonely")).toBe(true);
});

it("keeps every node at least 24px inside the box", () => {
  const layout = layoutMap(LAYOUT);

  for (const node of LAYOUT.nodes) {
    const point = layout.positions.get(node.id);

    if (!point) continue;
    expect(point.x - node.radius).toBeGreaterThanOrEqual(INSET);
    expect(point.x + node.radius).toBeLessThanOrEqual(LAYOUT.width - INSET);
    expect(point.y - node.radius).toBeGreaterThanOrEqual(INSET);
    expect(point.y + node.radius).toBeLessThanOrEqual(LAYOUT.height - INSET);
  }
});

it("keeps node labels off other nodes", () => {
  const layout = layoutMap(LAYOUT);
  const drawn = LAYOUT.nodes.filter((node) => layout.positions.has(node.id));

  for (const node of drawn) {
    const label = layout.labels.get(node.id);

    if (!label) throw new Error(`no label for ${node.id}`);
    const w = node.label.length * 7;

    const left =
      label.anchor === "start" ? label.x : label.anchor === "end" ? label.x - w : label.x - w / 2;

    for (const other of drawn) {
      const point = layout.positions.get(other.id) ?? { x: 0, y: 0 };
      const nearestX = Math.min(Math.max(point.x, left), left + w);
      const nearestY = Math.min(Math.max(point.y, label.y - 11), label.y + 3);
      expect(
        Math.hypot(point.x - nearestX, point.y - nearestY),
        `${node.id} label on ${other.id}`,
      ).toBeGreaterThan(other.radius);
    }
  }
});

it("spreads the members over the box when none links to another", () => {
  const layout = layoutMap({ ...LAYOUT, edges: [], outside: [] });

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

it("puts edge counts on their stroke and finds a clear stretch before omitting obstructed counts", () => {
  const positions = new Map([
    ["A", { x: 0, y: 100 }],
    ["B", { x: 200, y: 100 }],
  ]);

  const edge = { a: "A", b: "B", links: 12, label: { key: "A|B", text: "12" } };
  // A node label across the middle of the edge.
  const middle = { x: 80, y: 80, w: 40, h: 30 };

  // The 40% and 60% stops touch it; the 30% stop clears it.
  const moved = placeEdgeLabels(positions, [edge], [middle]).get("A|B");
  expect(moved).toEqual({ x: 60, y: 100 });
  expect(placeEdgeLabels(positions, [edge], []).get("A|B")).toEqual({ x: 100, y: 100 });

  const covered = { x: 0, y: 50, w: 200, h: 100 };
  expect(placeEdgeLabels(positions, [edge], [covered]).has("A|B")).toBe(false);
  expect(placeEdgeLabels(positions, [{ ...edge, label: undefined }], []).size).toBe(0);
});

it("anchors every label when three outside neighbors surround the largest member", () => {
  const layout = layoutMap({
    width: 652,
    height: 236,
    nodes: [
      { id: "Specs", radius: 30, label: "Specs\n123 notes" },
      { id: "Efforts", radius: 21.25, label: "Efforts\n52 notes" },
      { id: "Materials", radius: 7.25, label: "Materials\n1 note" },
    ],
    edges: [
      { a: "Specs", b: "Efforts", links: 127, label: { key: "se", text: "127" } },
      { a: "Efforts", b: "Materials", links: 1, label: { key: "em", text: "1" } },
    ],
    outside: [
      {
        id: "Reference",
        label: "Reference docs\n194 links",
        byMember: new Map([
          ["Specs", 170],
          ["Efforts", 24],
        ]),
      },
      {
        id: "Untyped",
        label: "Untyped notes\n173 links",
        byMember: new Map([
          ["Specs", 168],
          ["Efforts", 5],
        ]),
      },
      {
        id: "People",
        label: "People\n4 links",
        byMember: new Map([
          ["Specs", 1],
          ["Efforts", 3],
        ]),
      },
    ],
  });

  expect([...layout.labels.keys()].sort()).toEqual([...layout.positions.keys()].sort());
});

it("keeps small-map edges clear of every other node", () => {
  const nodes = ["Delivery", "Knowledge", "Untyped", "People"].map((id) => ({
    id,
    radius: 30,
    label: `${id}\n100 notes`,
  }));

  const edges = nodes.flatMap((a, i) =>
    nodes.slice(i + 1).map((b) => ({
      a: a.id,
      b: b.id,
      links: 100,
      label: { key: `${a.id}|${b.id}`, text: "100" },
    })),
  );

  const layout = layoutMap({ width: 652, height: 260, nodes, edges, outside: [] });
  expect(layout.edgeLabels.size).toBe(edges.length);

  for (const edge of edges) {
    const a = layout.positions.get(edge.a)!;
    const b = layout.positions.get(edge.b)!;

    for (const node of nodes.filter((node) => node.id !== edge.a && node.id !== edge.b)) {
      const p = layout.positions.get(node.id)!;

      const t = Math.max(
        0,
        Math.min(
          1,
          ((p.x - a.x) * (b.x - a.x) + (p.y - a.y) * (b.y - a.y)) /
            ((b.x - a.x) ** 2 + (b.y - a.y) ** 2),
        ),
      );

      expect(Math.hypot(p.x - a.x - t * (b.x - a.x), p.y - a.y - t * (b.y - a.y))).toBeGreaterThan(
        node.radius + 8,
      );
    }
  }
});

it("aligns a label left of its node by its near end", async () => {
  const { placeLabels } = await import("./map-labels.ts");

  const { labels } = placeLabels(
    new Map([["Knowledge", { x: 600, y: 130 }]]),
    [
      {
        id: "Knowledge",
        radius: 22,
        label: "Knowledge\n166 notes · 3 member types",
        alignToNode: true,
      },
    ],
    [],
    652,
    260,
  );

  const label = labels.get("Knowledge")!;
  expect(label.anchor).toBe("end");
  expect(Math.hypot(600 - label.x, Math.max(0, label.y - 12 - 130)) - 22).toBeLessThanOrEqual(12);
});

it("fits a label wider than the canvas without adding height", () => {
  const layout = layoutMap({
    width: 380,
    height: 260,
    nodes: [{ id: "wide", radius: 20, label: "Long name ".repeat(30) }],
    edges: [],
    outside: [],
  });

  expect(mapLabelLine("Long name ".repeat(30), 380)).toBe(
    "Long name Long name Long name Long name Lon…",
  );
  expect(layout.labels.has("wide")).toBe(true);
  expect(layout.height).toBe(260);
});

it("keeps a crowded map near its height when its labels cannot all fit", () => {
  // Shaped like a large vault: a dense, fully linked core and one member with a single link.
  const core = Array.from({ length: 11 }, (_, index) => ({
    id: `T${index}`,
    radius: index ? 6 + index : 30,
    label: `Member type ${index}\n${(index + 1) * 37} notes`,
  }));

  const edges = core.flatMap((a, i) =>
    core.slice(i + 1).map((b, j) => ({
      a: a.id,
      b: b.id,
      links: (i + j) * 13 + 1,
      label: { key: `${a.id}|${b.id}`, text: String((i + j) * 13 + 1) },
    })),
  );

  const input: LayoutInput = {
    width: 560,
    height: 260,
    nodes: [...core, { id: "Leaf", radius: 5, label: "Coaching\n1 note" }],
    edges: [...edges, { a: "T0", b: "Leaf", links: 1, label: { key: "T0|Leaf", text: "1" } }],
    outside: [],
  };

  // Growing to fit every label would make this map over 2,000px tall.
  expect(layoutMap(input).height).toBeLessThanOrEqual(input.height + 120);
});
