import type { DisplayGroup, TypeDoc } from "@rhizome/kit";
import { expect, it } from "vitest";

import { DISPLAY_GROUPS, PATHS, RECORDS_DATA, TYPE_DOCS } from "./__fixtures__/groups.ts";
import { fixtureModel } from "./__fixtures__/harness.tsx";
import { isJsonObject, type JsonValue } from "./api.ts";
import {
  buildHierarchy,
  compareAdvanced,
  connections,
  defaultSpine,
  flattenTree,
  lifecycleRank,
  linkGraph,
  sectionColumns,
  sectionOrder,
  sectionRows,
  spineChoices,
  subtreeRecords,
  traceColumns,
  traceRow,
} from "./graph.ts";
import { traceMatrix } from "./matrix.ts";
import type { GroupModel, GroupRecord, Member } from "./model.ts";

const planning = fixtureModel("Planning");

const graph = linkGraph(planning);

function memberOf(model: GroupModel, name: string): Member {
  const found = model.memberIndex.get(name);

  if (!found) throw new Error(`no member ${name}`);

  return found;
}

function recordAt(model: GroupModel, path: string): GroupRecord {
  const found = model.byPath.get(path);

  if (!found) throw new Error(`no record ${path}`);

  return found;
}

const titles = (records: readonly GroupRecord[]) => records.map((record) => record.title);

const setOf = (map: ReadonlyMap<string, ReadonlySet<string>>, key: string) => [
  ...(map.get(key) ?? []),
];

it("links members through declared targets, excluding self links and broad interfaces", () => {
  expect(setOf(graph.outgoing, "Work")).toEqual(["Area"]);
  expect(setOf(graph.outgoing, "Release")).toEqual(["Work", "Checklist", "Area"]);
  expect(setOf(graph.incoming, "Area")).toEqual(["Work", "Release"]);
  expect(graph.outgoing.has("Area")).toBe(false);
  expect([...graph.referenceTypes]).toEqual(["Area", "Checklist"]);
  expect(graph.linked).toBe(true);

  // blockedBy links two Work records; a link within one member is not a group link.
  const carts = recordAt(planning, PATHS.carts).key;
  expect(graph.edges.some((edge) => edge.from === carts && edge.field === "blockedBy")).toBe(false);

  const library = linkGraph(fixtureModel("Library"));
  expect(library.linked).toBe(false);
  expect(library.edges).toEqual([]);
});

it("prefers a hierarchical member another member links to as the spine", () => {
  expect(defaultSpine(planning, graph)).toBe("Area");
  expect(spineChoices(planning, graph)).toEqual(["Area", "Work", "Release", "Checklist"]);
});

it("otherwise picks the member linked with the most others, then the most record links", () => {
  const flatArea = { ...TYPE_DOCS.Area, parentField: undefined };
  const flat = fixtureModel("Planning", { docs: { ...TYPE_DOCS, Area: flatArea } });

  // Release links with Work, Checklist, and Area; the others link with two members.
  expect(defaultSpine(flat, linkGraph(flat))).toBe("Release");

  const release: TypeDoc = {
    ...TYPE_DOCS.Release,
    fields: TYPE_DOCS.Release.fields.filter((field) => field.name !== "checklist"),
  };

  const tied = fixtureModel("Planning", {
    docs: { ...TYPE_DOCS, Area: flatArea, Release: release },
  });

  // Work, Area, and Release each link with two members; Work carries seven record links.
  expect(defaultSpine(tied, linkGraph(tied))).toBe("Work");
  expect(defaultSpine(fixtureModel("Library"), linkGraph(fixtureModel("Library")))).toBeNull();
});

it("offers every linked member with records as rows, even one the default's rows never reach", () => {
  const withoutArea = (rows: JsonValue | undefined) =>
    (Array.isArray(rows) ? rows : []).map((row) =>
      isJsonObject(row) ? { ...row, area: null } : row,
    );

  // Release links Checklist and Area, both reference types; Work links only Area.
  const model = fixtureModel("Planning", {
    docs: {
      ...TYPE_DOCS,
      Area: { ...TYPE_DOCS.Area, parentField: undefined },
      Release: {
        ...TYPE_DOCS.Release,
        fields: TYPE_DOCS.Release.fields.filter((field) => field.name !== "works"),
      },
    },
    data: {
      ...RECORDS_DATA,
      Story: withoutArea(RECORDS_DATA.Story),
      Bug: withoutArea(RECORDS_DATA.Bug),
    },
  });

  const reach = linkGraph(model);

  expect(defaultSpine(model, reach)).toBe("Release");
  expect(traceColumns(model, reach, "Release").unreached).toEqual(["Work"]);
  expect(spineChoices(model, reach)).toEqual(["Release", "Checklist", "Area", "Work"]);
});

it("orders Trace columns by declared links, then distance and depth, with outside links last", () => {
  const fromWork = traceColumns(planning, graph, "Work");

  expect(fromWork.members).toEqual([
    { member: "Area", distance: 1, relation: "linked-from-rows" },
    { member: "Release", distance: 1, relation: "links-to-rows" },
    { member: "Checklist", distance: 2, relation: "through" },
  ]);
  expect(fromWork.outside.map((link) => [link.field, link.targetGroup])).toEqual([
    ["owner", "People"],
  ]);
  expect(fromWork.unreached).toEqual([]);

  // Area declares no links; Work (depth 1) comes before Release (depth 2) at the same distance.
  const fromArea = traceColumns(planning, graph, "Area");
  expect(fromArea.members.map((column) => column.member)).toEqual(["Work", "Release", "Checklist"]);
});

it("fills a row with records linked to anything already in it, marking indirect ones", () => {
  const columns = traceColumns(planning, graph, "Area");
  const cells = traceRow(planning, graph, columns, recordAt(planning, PATHS.payments));

  const cell = (member: string) =>
    (cells.get(member) ?? []).map((entry) => [entry.record.title, entry.direct]);

  expect(cell("Work")).toEqual([
    ["Checkout redesign", true],
    ["Saved carts", true],
    ["Tax rounding", true],
  ]);
  expect(cell("Release")).toEqual([["Fall release", false]]);
  expect(cell("Checklist")).toEqual([["Fall checklist", false]]);
});

it("fills an earlier column through a later one at the same distance, marking it indirect", () => {
  const columns = traceColumns(planning, graph, "Release");
  const byName = (name: string) => columns.members.filter((column) => column.member === name);
  // Area before Work: the fall release names no area, but its work items do.
  const reordered = { ...columns, members: [...byName("Area"), ...byName("Work")] };
  const cells = traceRow(planning, graph, reordered, recordAt(planning, PATHS.fall));

  expect((cells.get("Area") ?? []).map((entry) => [entry.record.title, entry.direct])).toEqual([
    ["Payments", false],
  ]);
});

it("never fills a column back through a farther column", () => {
  // Winter ships a Payments work item and a Platform one.
  const releases = Array.isArray(RECORDS_DATA.Release) ? RECORDS_DATA.Release : [];

  const works = [
    { path: PATHS.checkout, title: "Checkout redesign", resolvedType: "Story" },
    { path: PATHS.login, title: "Login loop", resolvedType: "Bug" },
  ];

  const data = {
    ...RECORDS_DATA,
    Release: releases.map((release) =>
      isJsonObject(release) && release.path === PATHS.winter ? { ...release, works } : release,
    ),
  };

  const workFor = (docs: Record<string, TypeDoc>) => {
    const model = fixtureModel("Planning", { data, docs });
    const modelGraph = linkGraph(model);
    const columns = traceColumns(model, modelGraph, "Area");
    const cells = traceRow(model, modelGraph, columns, recordAt(model, PATHS.payments));
    const release = columns.members.find((column) => column.member === "Release");

    return {
      releaseDistance: release?.distance,
      work: (cells.get("Work") ?? []).map((entry) => [entry.record.title, entry.direct]),
    };
  };

  // Releases also link areas, so they sit as near the spine as work items do,
  // and a work item sharing a release with Payments work is placed, indirect.
  expect(workFor(TYPE_DOCS)).toEqual({
    releaseDistance: 1,
    work: [
      ["Checkout redesign", true],
      ["Saved carts", true],
      ["Tax rounding", true],
      ["Login loop", false],
    ],
  });

  // Without that link, releases are farther, and nothing returns through them.
  const release = TYPE_DOCS.Release;
  const fields = release.fields.filter((field) => field.name !== "area");

  expect(workFor({ ...TYPE_DOCS, Release: { ...release, fields } })).toEqual({
    releaseDistance: 2,
    work: [
      ["Checkout redesign", true],
      ["Saved carts", true],
      ["Tax rounding", true],
    ],
  });
});

it("never expands a row through a reference type", () => {
  // Story and Bug as separate roots that both link to Area, a reference type.
  const planningGroup = DISPLAY_GROUPS.groups.find((group) => group.name === "Planning");
  const [work, area] = planningGroup?.members ?? [];

  if (!work || !area) throw new Error("fixture group missing");

  const split: DisplayGroup = { name: "Planning", members: [...work.children, area] };
  const model = fixtureModel("Planning", { group: split });
  const splitGraph = linkGraph(model);
  const columns = traceColumns(model, splitGraph, "Story");

  // Bug reaches Story only through Area, so it gets no column that would always be empty.
  expect(columns.members.map((column) => [column.member, column.distance])).toEqual([["Area", 1]]);
  expect(columns.unreached).toEqual(["Bug"]);

  const cells = traceRow(model, splitGraph, columns, recordAt(model, PATHS.checkout));

  // Tax rounding shares the Payments area, which links nothing onward.
  expect(titles((cells.get("Area") ?? []).map((entry) => entry.record))).toEqual(["Payments"]);
  expect(
    traceRow(
      model,
      splitGraph,
      {
        ...columns,
        members: [...columns.members, { member: "Bug", distance: 2, relation: "through" }],
      },
      recordAt(model, PATHS.checkout),
    ).get("Bug"),
  ).toEqual([]);
});

it("resolves a link to an embedded record by its node, not its host note", () => {
  const releases = Array.isArray(RECORDS_DATA.Release) ? RECORDS_DATA.Release : [];

  const embedded = {
    ref: { kind: "EMBEDDED", nodeId: "chk" },
    path: PATHS.fall,
    title: "Fall checklist",
  };

  const model = fixtureModel("Planning", {
    data: {
      ...RECORDS_DATA,
      Release: releases.map((entry) =>
        isJsonObject(entry) && entry.path === PATHS.fall
          ? { ...entry, checklist: embedded }
          : entry,
      ),
      Checklist: [
        {
          ref: { notePath: PATHS.fall, kind: "EMBEDDED", nodeId: "chk", typeName: "Checklist" },
          path: PATHS.fall,
          title: "Fall checklist",
        },
      ],
    },
  });

  const embeddedGraph = linkGraph(model);
  const fall = recordAt(model, PATHS.fall);

  expect([...(embeddedGraph.adjacent.get(fall.key) ?? [])]).toContain(`${PATHS.fall}#chk`);

  const checklist = connections(model, embeddedGraph)
    .flat()
    .find((cell) => cell.from === "Release" && cell.to === "Checklist");

  expect(checklist).toMatchObject({ allowed: true, count: 1 });
});

it("ranks lifecycle values most advanced first, terminal values next, and empty last", () => {
  const work = memberOf(planning, "Work");
  const rank = (value: string | null) => lifecycleRank(work.lifecycle, value);

  expect(["blocked", "doing", "backlog", "done", "dropped", null].map(rank)).toEqual([
    0, 1, 2, 3, 4, 5,
  ]);
  expect(titles([...work.records].sort(compareAdvanced(work)))).toEqual([
    "Tax rounding",
    "Checkout redesign",
    "Saved carts",
    "Gift cards",
    "Login loop",
    "Wishlist",
  ]);
});

it("nests records under parents, keeps sibling order, and tolerates parent cycles", () => {
  const area = memberOf(planning, "Area");
  const sorted = [...area.records].sort(compareAdvanced(area));
  const roots = buildHierarchy(area, sorted);

  expect(flattenTree(roots).map((node) => [node.record.title, node.depth])).toEqual([
    ["Platform", 0],
    ["Commerce", 0],
    ["Payments", 1],
    ["Wishlists", 1],
    ["Loop B", 0],
    ["Loop A", 1],
  ]);
  expect(titles(subtreeRecords(roots[1]))).toEqual(["Commerce", "Payments", "Wishlists"]);
  expect(flattenTree(roots).find((node) => node.record.title === "Payments")?.parent?.title).toBe(
    "Commerce",
  );
});

it("keeps a record hanging off a parent cycle beneath the cycle", () => {
  const area = memberOf(planning, "Area");
  const loopA = recordAt(planning, PATHS.loopA);

  const hanging: GroupRecord = {
    ...loopA,
    key: "areas/loop-c.md",
    path: "areas/loop-c.md",
    title: "Loop C",
    links: new Map([
      ["parent", [{ key: loopA.key, path: loopA.path, title: loopA.title, type: "Area" }]],
    ]),
  };

  // Loop C sorts first but its parent, Loop A, is on the A-B cycle.
  const roots = buildHierarchy(area, [hanging, recordAt(planning, PATHS.loopB), loopA]);

  expect(flattenTree(roots).map((node) => [node.record.title, node.depth])).toEqual([
    ["Loop A", 0],
    ["Loop C", 1],
    ["Loop B", 1],
  ]);
});

it("counts record links per member pair and marks allowed relations nothing uses", () => {
  const cells = connections(planning, graph).flat();

  const cell = (from: string, to: string) =>
    cells.find((entry) => entry.from === from && entry.to === to);

  expect(cell("Work", "Area")).toEqual({ from: "Work", to: "Area", allowed: true, count: 5 });
  expect(cell("Release", "Work")).toMatchObject({ allowed: true, count: 2 });
  expect(cell("Release", "Checklist")).toMatchObject({ allowed: true, count: 1 });
  expect(cell("Release", "Area")).toMatchObject({ allowed: true, count: 0 });
  expect(cell("Area", "Work")).toMatchObject({ allowed: false, count: 0 });
  expect(cell("Work", "Work")).toMatchObject({ allowed: false });
});

it("orders sections like Trace columns when the group links, else by record count", () => {
  expect(sectionOrder(planning, graph).map((member) => member.name)).toEqual([
    "Area",
    "Work",
    "Release",
    "Checklist",
  ]);

  const library = fixtureModel("Library");
  expect(sectionOrder(library, linkGraph(library)).map((member) => member.name)).toEqual([
    "Glossary",
    "Memo",
  ]);
});

it("flattens a hierarchical member's rows into a tree", () => {
  const rows = sectionRows(memberOf(planning, "Area"));

  expect(rows.map((node) => [node.record.title, node.depth])).toEqual([
    ["Platform", 0],
    ["Commerce", 0],
    ["Payments", 1],
    ["Wishlists", 1],
    ["Loop B", 0],
    ["Loop A", 1],
  ]);
  expect(sectionRows(memberOf(planning, "Work")).every((node) => node.depth === 0)).toBe(true);
});

it("chooses section columns: implementing type, lifecycle always, and KEY fields with values", () => {
  const work = memberOf(planning, "Work");

  const names = (member: Member, records: readonly GroupRecord[]) =>
    sectionColumns(member, records).map((column) =>
      column.kind === "field" ? column.field.name : column.kind,
    );

  expect(names(work, work.records)).toEqual([
    "type",
    "lifecycle",
    "kind",
    "nextStep",
    "area",
    "resolution",
  ]);
  expect(names(work, [recordAt(planning, PATHS.gifts)])).toEqual(["type", "lifecycle", "kind"]);

  const release = memberOf(planning, "Release");
  const winter = recordAt(planning, PATHS.winter);
  const unset = { ...winter, values: new Map([...winter.values, ["releaseStatus", null]]) };

  expect(names(release, [unset])).toEqual(["lifecycle"]);
  expect(names(release, release.records)).toEqual(["lifecycle", "date", "notes"]);
  expect(names(memberOf(planning, "Checklist"), memberOf(planning, "Checklist").records)).toEqual(
    [],
  );
});

it("never lists a group record in an outside-link column, even for a field typed Note", () => {
  // Memo's related field is typed Note, so it points outside; one target is a Glossary record.
  const library = fixtureModel("Library");
  const matrix = traceMatrix(library, linkGraph(library), "Memo");

  const style = matrix.bands
    .flatMap((band) => band.roots)
    .find((row) => row.record.title === "Style memo");

  expect(matrix.columns.map((column) => column.id)).toEqual(["link:related:Note"]);
  expect(style?.cells.get("link:related:Note")).toEqual([]);
});

it("names Sections columns apart when implementors declare one KEY name differently", () => {
  const reviewer = (kind: "scalar" | "link", typeName: string) => ({
    name: "reviewer",
    kind,
    typeName,
    display: { importance: "KEY" as const },
  });

  const docs = {
    ...TYPE_DOCS,
    Story: {
      ...TYPE_DOCS.Story,
      fields: [...TYPE_DOCS.Story.fields, reviewer("scalar", "String")],
    },
    Bug: { ...TYPE_DOCS.Bug, fields: [...TYPE_DOCS.Bug.fields, reviewer("link", "Person")] },
  };

  const work = fixtureModel("Planning", { docs }).memberIndex.get("Work");

  if (!work) throw new Error("no member Work");

  const ada = { key: "people/ada.md", path: "people/ada.md", title: "Ada", type: "Person" };

  const records = work.records.map((record) =>
    record.type === "Story"
      ? { ...record, values: new Map([...record.values, ["reviewer", "Lin"]]) }
      : { ...record, links: new Map([...record.links, ["reviewer", [ada]]]) },
  );

  const columns = sectionColumns(work, records).flatMap((column) =>
    column.kind === "field" && column.field.name === "reviewer" ? [[column.id, column.label]] : [],
  );

  expect(columns).toEqual([
    ["field:reviewer:Story", "Reviewer (Story)"],
    ["field:reviewer:Bug", "Reviewer (Bug)"],
  ]);
});
