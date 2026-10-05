import type { DisplayGroup, TypeDoc } from "@rhizome/kit";
import { expect, it } from "vitest";

import {
  DISPLAY_GROUPS,
  PATHS,
  PLANNING_GUIDE,
  RECORDS_DATA,
  TYPE_DOCS,
  profile,
} from "./__fixtures__/groups.ts";
import { fixtureModel } from "./__fixtures__/harness.tsx";
import {
  INBOUND_CAP,
  RECORD_CAP,
  REVERSE_SAMPLE,
  parseRecords,
  parseTableChoice,
  parseTargetViews,
  parseTypeLabels,
  recordsQuery,
  type JsonObject,
} from "./api.ts";
import {
  collectionGroup,
  firstSentence,
  lifecycleValue,
  linkTargets,
  memberLabel,
  pickGuide,
  planMembers,
  recordField,
} from "./model.ts";

const planning = () => fixtureModel("Planning");

const member = (name: string, group = "Planning") => {
  const found = fixtureModel(group).memberIndex.get(name);

  if (!found) throw new Error(`no member ${name}`);

  return found;
};

it("plans one member per root, letting interfaces claim implementors and display children stand alone", () => {
  const group = DISPLAY_GROUPS.groups.find((candidate) => candidate.name === "Planning");

  if (!group) throw new Error("fixture group missing");

  const plans = planMembers(group);

  expect(plans.map((plan) => [plan.name, plan.concreteTypes])).toEqual([
    ["Work", ["Story", "Bug"]],
    ["Area", ["Area"]],
    ["Release", ["Release"]],
    ["Checklist", ["Checklist"]],
  ]);
  expect(plans[0].covers).toEqual(["Work", "Story", "Bug"]);
});

it("merges an interface member's fields across implementors", () => {
  const fields = new Map(member("Work").fields.map((field) => [field.name, field]));

  expect([...fields.keys()]).toEqual([
    "summary",
    "kind",
    "stage",
    "nextStep",
    "area",
    "owner",
    "blockedBy",
    "health",
    "estimate",
    "resolution",
  ]);
  expect(fields.get("kind")?.declaredBy).toEqual(["Story"]);
  expect(fields.get("stage")).toMatchObject({
    key: true,
    required: true,
    declaredBy: ["Story", "Bug"],
  });
  expect(fields.get("resolution")?.requiredWhen).toEqual([{ field: "stage", equals: "done" }]);
  expect(fields.get("nextStep")?.policyReason).toBe(
    "Name the next concrete step so the work can move.",
  );
});

it("keeps an interface field apart per declaration when implementors disagree", () => {
  const story: TypeDoc = {
    ...TYPE_DOCS.Story,
    fields: [...TYPE_DOCS.Story.fields, { name: "reviewer", kind: "link", typeName: "Person" }],
  };

  const bug: TypeDoc = {
    ...TYPE_DOCS.Bug,
    fields: [...TYPE_DOCS.Bug.fields, { name: "reviewer", kind: "scalar", typeName: "String" }],
  };

  const work = fixtureModel("Planning", {
    docs: { ...TYPE_DOCS, Story: story, Bug: bug },
  }).memberIndex.get("Work");

  if (!work) throw new Error("no member Work");

  const reviewers = work.fields.filter((field) => field.name === "reviewer");

  expect(reviewers.map((field) => [field.kind, field.typeName, field.declaredBy])).toEqual([
    ["link", "Person", ["Story"]],
    ["scalar", "String", ["Bug"]],
  ]);

  const [asLink, asText] = reviewers;
  const link = work.links.find((candidate) => candidate.field === "reviewer");
  const found = work.records.find((record) => record.type === "Bug");

  if (!asLink || !asText || !link || !found) throw new Error("fixture shape changed");

  const ada = { key: "people/ada.md", path: "people/ada.md", title: "Ada", type: "Person" };

  const bugRecord = {
    ...found,
    values: new Map([...found.values, ["reviewer", "Lin"]]),
    links: new Map([...found.links, ["reviewer", [ada]]]),
  };

  // A Bug's reviewer is text; the Story declaration never reads it as a link.
  expect(recordField(bugRecord, asText)).toBe("Lin");
  expect(recordField(bugRecord, asLink)).toEqual([]);
  expect(linkTargets(bugRecord, link)).toEqual([]);
});

it("keeps one lifecycle across implementors whose status enums differ", () => {
  const bug: TypeDoc = {
    ...TYPE_DOCS.Bug,
    fields: TYPE_DOCS.Bug.fields.map((field) =>
      field.name === "stage"
        ? {
            ...field,
            typeName: "BugStage",
            enum: {
              values: [
                { name: "triage", label: "Triage", order: 5, stage: "open", stageDeclared: true },
                { name: "done", label: "Done", order: 90, stage: "done", stageDeclared: true },
              ],
            },
          }
        : field,
    ),
  };

  const work = fixtureModel("Planning", { docs: { ...TYPE_DOCS, Bug: bug } }).memberIndex.get(
    "Work",
  );

  const found = work?.records.find((record) => record.type === "Bug");

  if (!work || !found) throw new Error("fixture shape changed");

  expect(work.lifecycle?.declaredBy).toEqual(["Story", "Bug"]);
  expect(work.lifecycle?.values.map((value) => value.name)).toEqual([
    "backlog",
    "doing",
    "blocked",
    "done",
    "dropped",
    "triage",
  ]);
  expect(lifecycleValue(work, { ...found, values: new Map([["stage", "triage"]]) })).toBe("triage");
});

it("reads each member's lifecycle from its type profile", () => {
  // Declared stages, read from the Work interface's own profile.
  expect(member("Work").lifecycle?.field).toBe("stage");
  // Stages inferred from authored tones.
  expect(member("Release").lifecycle?.field).toBe("releaseStatus");
  expect(member("Memo", "Library").lifecycle?.field).toBe("memoStatus");
  expect(member("Area").lifecycle).toBeNull();
  // A field named `status` with @view order but no stages is a category.
  expect(member("Glossary", "Library").lifecycle).toBeNull();
});

it("takes an interface member's lifecycle and gaps from the interface, not its implementors", () => {
  const work: TypeDoc = { ...TYPE_DOCS.Work, profile: profile({ gapFields: ["area"] }) };

  const found = fixtureModel("Planning", { docs: { ...TYPE_DOCS, Work: work } }).memberIndex.get(
    "Work",
  );

  // Story and Bug still name `stage` as their lifecycle.
  expect(found?.lifecycle).toBeNull();
  expect(found?.gapFields).toEqual(["area"]);
  expect(found?.keyTextFields).toEqual([]);
  expect(member("Work")).toMatchObject({
    gapFields: ["nextStep", "area"],
    keyTextFields: ["nextStep"],
  });
});

it("has no lifecycle, gaps, or key text when the profile is missing", () => {
  const { profile: _omitted, ...release } = TYPE_DOCS.Release;

  const found = fixtureModel("Planning", {
    docs: { ...TYPE_DOCS, Release: release },
  }).memberIndex.get("Release");

  expect(found).toMatchObject({ lifecycle: null, gapFields: [], keyTextFields: [] });
});

it("classifies each link by the member its declared target belongs to", () => {
  const links = (name: string, group = "Planning") =>
    member(name, group).links.map((link) => [link.field, link.target, link.targetGroup]);

  expect(links("Work")).toEqual([
    ["area", "Area", "Planning"],
    ["owner", null, "People"],
    ["blockedBy", "Work", "Planning"],
  ]);
  expect(links("Release")).toEqual([
    ["works", "Work", "Planning"],
    ["checklist", "Checklist", "Planning"],
    ["area", "Area", "Planning"],
  ]);
  // A field typed by the broad Note interface stays outside the group.
  expect(links("Memo", "Library")).toEqual([["related", null, null]]);
});

it("assigns records to members with their summaries and resolves note paths", () => {
  const model = planning();
  const checkout = model.byPath.get(PATHS.checkout);

  expect(checkout).toMatchObject({
    member: "Work",
    type: "Story",
    title: "Checkout redesign",
    summary: "Rebuild checkout around saved payment methods.",
    issueCount: 0,
  });
  expect(checkout?.values.get("stage")).toBe("doing");
  expect(checkout?.links.get("area")).toEqual([
    { key: PATHS.payments, path: PATHS.payments, title: "Payments", type: "Area" },
  ]);
  // Strict: the workspace rejects a ref carrying undefined fields.
  expect(checkout?.ref).toStrictEqual({
    notePath: PATHS.checkout,
    kind: "NOTE",
    typeName: "Story",
  });
  expect(model.records.size).toBe(15);
  expect(model.rootCount).toBe(3);
});

it("picks the companion document most members share as the guide", () => {
  expect(planning().guide).toEqual({
    path: PLANNING_GUIDE,
    title: "Planning guide",
    summary: "How planning records fit together.",
    sharedBy: 2,
  });
  expect(fixtureModel("People").guide).toBeNull();
});

it("prefers an interface's own companion document, then the one most implementors share", () => {
  const [work] = planMembers(
    collectionGroup(DISPLAY_GROUPS.groups, "Work") ?? DISPLAY_GROUPS.groups[0],
  );

  const companions = (...paths: string[]) => paths.map((path) => ({ path }));

  const docs = (own: string[], story: string[], bug: string[]) => ({
    ...TYPE_DOCS,
    Work: { ...TYPE_DOCS.Work, companionDocs: companions(...own) },
    Story: { ...TYPE_DOCS.Story, companionDocs: companions(...story) },
    Bug: { ...TYPE_DOCS.Bug, companionDocs: companions(...bug) },
  });

  expect(work.concreteTypes).toEqual(["Story", "Bug"]);
  expect(pickGuide([work], docs(["docs/work.md"], [PLANNING_GUIDE], [PLANNING_GUIDE]))?.path).toBe(
    "docs/work.md",
  );
  expect(
    pickGuide([work], docs([], ["docs/story.md", PLANNING_GUIDE], [PLANNING_GUIDE]))?.path,
  ).toBe(PLANNING_GUIDE);
});

it("drops a first word every member label shares", () => {
  const library = fixtureModel("Library");

  expect(library.labelPrefix).toBe("Library ");
  expect(library.members.map((entry) => memberLabel(library, entry, { plural: true }))).toEqual([
    "Memos",
    "Glossaries",
  ]);
  expect(memberLabel(library, library.members[0], { count: 1 })).toBe("Memo");
  expect(planning().labelPrefix).toBe("");
});

it("lists the authored views the group's switcher offers, generic ones included", () => {
  const team = { id: "team.dashboard", name: "Team dashboard", description: "Who is on what." };

  expect(planning().views).toEqual([
    { id: "planning.board", name: "Planning board", description: null },
    team,
  ]);
  // A hidden view is not offered; Overview and the bundled group views never list.
  expect(fixtureModel("Library").views).toEqual([team]);
  expect(parseTargetViews(null, "group", "Planning")).toEqual([]);
  expect(parseTargetViews({ views: [], targets: [] }, "group", "Planning")).toEqual([]);
});

it("reads labels and groups of every type", () => {
  const labels = parseTypeLabels([
    { name: "Person", label: "Person", pluralLabel: "People", displayGroup: "People" },
    { name: "Loose" },
  ]);

  expect(labels.get("Person")).toEqual({ label: "Person", pluralLabel: "People", group: "People" });
  expect(labels.get("Loose")).toEqual({ label: "Loose", pluralLabel: "Loose", group: null });
});

it("queries each concrete type under its own alias with Node fragments for link targets", () => {
  const query = recordsQuery([TYPE_DOCS.Story, TYPE_DOCS.Area], PLANNING_GUIDE);

  expect(query).toMatch(/^query GroupRecords\(\$guide: String\) \{/);
  // Newest first, so a type past the record cap keeps its recent records.
  expect(query).toContain(
    `Story: story(first: ${RECORD_CAP + 1}, sort: [{ field: "updatedAt", direction: desc }]) {`,
  );
  expect(query).toContain(
    'Area: area(first: 501, sort: [{ field: "updatedAt", direction: desc }]) {',
  );
  expect(query).toContain(
    "area { ... on Node { ref { kind fragment nodeId } path title resolvedType } }",
  );
  // Both directions, typed and body links: `connected` reads only body links and backlinks.
  expect(query).toContain(
    "neighborhood(direction: BOTH, first: 200) { truncated nodes { ... on Node { path title resolvedType } } }",
  );
  expect(query).toContain("structuralFingerprint: structural");
  expect(query).toContain("groupGuide: note(path: $guide) { path title frontmatter }");
  // KEY, enum, and summary scalars are read; an unmarked Int is not.
  expect(query).toMatch(/\bnextStep\b/);
  expect(query).toMatch(/\bkind\b/);
  expect(query).not.toMatch(/\bestimate\b/);
});

/** Areas that work items link to through `area`, read back as reverse fields. */
const AREA_WITH_REVERSE: TypeDoc = {
  ...TYPE_DOCS.Area,
  fields: [
    ...TYPE_DOCS.Area.fields,
    { name: "works", kind: "reverse", typeName: "Work", list: true },
    { name: "home", kind: "reverse", typeName: "Release" },
  ],
  profile: profile({ ...TYPE_DOCS.Area.profile, reverseFields: ["works", "home"] }),
};

it("bounds what a collection reads per record on a hub type: counts, capped samples, capped links in", () => {
  const query = recordsQuery([AREA_WITH_REVERSE], null, { reverse: true, neighbors: "INBOUND" });

  // Fill counts come from the generated <field>Count, under an alias no authored
  // field can take; targets are a capped sample for the most common ones.
  expect(query).toContain("works__generatedCount: worksCount");
  expect(query).toContain(
    `works(first: ${REVERSE_SAMPLE}) { ... on Node { ref { kind fragment nodeId } path title resolvedType } }`,
  );
  // A single-valued reverse field has no count field and at most one target.
  expect(query).not.toContain("homeCount");
  expect(query).toMatch(/\bhome \{ \.\.\. on Node/);
  expect(query).toContain(`neighborhood(direction: INBOUND, first: ${INBOUND_CAP})`);
  // A group reads neither.
  expect(recordsQuery([AREA_WITH_REVERSE], null)).not.toMatch(/\bworks\b/);

  const parsed = parseRecords(
    {
      Area: [
        {
          path: PATHS.payments,
          title: "Payments",
          works: [{ path: PATHS.tax, title: "Tax rounding", resolvedType: "Bug" }],
          works__generatedCount: 40,
          home: { path: PATHS.fall, title: "Fall release", resolvedType: "Release" },
        },
        { path: PATHS.platform, title: "Platform", works__generatedCount: 0, home: null },
      ],
    },
    [AREA_WITH_REVERSE],
  );

  const [payments, platform] = parsed.pages.get("Area")?.records ?? [];

  expect(payments?.reverse.get("works")).toEqual([
    { key: PATHS.tax, path: PATHS.tax, title: "Tax rounding", type: "Bug" },
  ]);
  expect(payments?.reverseCounts.get("works")).toBe(40);
  expect(payments?.reverseCounts.get("home")).toBe(1);
  expect(platform?.reverse.get("works")).toEqual([]);
  expect(platform?.reverseCounts.get("works")).toBe(0);
  expect(platform?.reverseCounts.get("home")).toBe(0);
});

it("never reads an authored <field>Count as a reverse field's count", () => {
  // The type authors `worksCount`, a KEY number, so the generated count does not exist.
  const authored: TypeDoc = {
    ...AREA_WITH_REVERSE,
    fields: [
      ...AREA_WITH_REVERSE.fields,
      { name: "worksCount", kind: "scalar", typeName: "Int", display: { importance: "KEY" } },
    ],
  };

  const query = recordsQuery([authored], null, { reverse: true, neighbors: "INBOUND" });

  expect(query).not.toContain("works__generatedCount");
  // One past the sample tells a full list from a sampled one.
  expect(query).toContain(`works(first: ${REVERSE_SAMPLE + 1}) {`);

  const linking = Array.from({ length: REVERSE_SAMPLE + 1 }, (_, index) => ({
    path: `work/w${index}.md`,
    title: `W${index}`,
    resolvedType: "Story",
  }));

  const [payments, platform] =
    parseRecords(
      {
        Area: [
          { path: PATHS.payments, title: "Payments", worksCount: 99, works: linking },
          { path: PATHS.platform, title: "Platform", worksCount: 7, works: linking.slice(0, 2) },
        ],
      },
      [authored],
    ).pages.get("Area")?.records ?? [];

  expect(payments?.reverse.get("works")).toHaveLength(REVERSE_SAMPLE);
  expect(payments?.reverseCounts.get("works")).toBe(REVERSE_SAMPLE + 1);
  expect(platform?.reverseCounts.get("works")).toBe(2);
  expect(payments?.values.get("worksCount")).toBe(99);
});

it("reads the generated count under an alias no field of the type takes", () => {
  // An authored field named exactly the first-choice alias pushes the alias on.
  const taken: TypeDoc = {
    ...AREA_WITH_REVERSE,
    fields: [
      ...AREA_WITH_REVERSE.fields,
      {
        name: "works__generatedCount",
        kind: "scalar",
        typeName: "Int",
        display: { importance: "KEY" },
      },
    ],
  };

  const query = recordsQuery([taken], null, { reverse: true, neighbors: "INBOUND" });

  expect(query).toContain("works__generatedCount2: worksCount");
  expect(query).not.toContain("works__generatedCount: worksCount");

  const [payments] =
    parseRecords(
      {
        Area: [
          {
            path: PATHS.payments,
            title: "Payments",
            works__generatedCount: 99,
            works__generatedCount2: 40,
            works: [],
          },
        ],
      },
      [taken],
    ).pages.get("Area")?.records ?? [];

  expect(payments?.reverseCounts.get("works")).toBe(40);
  expect(payments?.values.get("works__generatedCount")).toBe(99);
});

it("finds a collection's authored views and its standard Table choice", () => {
  const generated = "generated.type.Story.table";

  const catalog: JsonObject = {
    views: [{ id: "story.triage", name: "Triage", description: "Untriaged stories." }],
    targets: [
      {
        kind: "type",
        name: "Story",
        choices: [
          { id: "b", name: "Briefing", renderer: "custom", viewId: "type.briefing" },
          { id: "t", name: "Table", renderer: "table", viewId: generated, variant: "table" },
          { id: "c", name: "Cards", renderer: "card", viewId: generated, variant: "card" },
          {
            id: "x",
            name: "Triage · Table",
            renderer: "table",
            viewId: "story.triage",
            custom: true,
          },
        ],
      },
    ],
  };

  expect(parseTargetViews(catalog, "type", "Story")).toEqual([
    { id: "story.triage", name: "Triage · Table", description: "Untriaged stories." },
  ]);
  expect(parseTableChoice(catalog, "type", "Story")).toBe("t");
  expect(parseTableChoice(catalog, "interface", "Story")).toBeNull();
});

it("reads embedded node types without neighbors and without a guide variable", () => {
  const query = recordsQuery([{ ...TYPE_DOCS.Checklist, role: "EMBEDDED_NODE" }], null);

  expect(query).toMatch(/^query GroupRecords \{/);
  expect(query).not.toContain("neighborhood");
});

it("caps records per type and reports the truncation", () => {
  const rows = Array.from({ length: RECORD_CAP + 1 }, (_, index) => ({
    path: `people/${index}.md`,
    title: `Person ${index}`,
  }));

  const parsed = parseRecords({ Person: rows }, [TYPE_DOCS.Person]);

  expect(parsed.pages.get("Person")?.records).toHaveLength(RECORD_CAP);
  expect(parsed.pages.get("Person")?.truncated).toBe(true);
});

it("tolerates missing types, pathless records, and malformed values", () => {
  const data: JsonObject = {
    Story: [
      { title: "No path" },
      { path: "a.md", title: "A", stage: 7, area: "oops", updatedAt: "never" },
    ],
  };

  const parsed = parseRecords(data, [TYPE_DOCS.Story, TYPE_DOCS.Bug]);
  const [record] = parsed.pages.get("Story")?.records ?? [];

  expect(parsed.pages.get("Story")?.records).toHaveLength(1);
  expect(parsed.pages.get("Bug")).toEqual({ records: [], truncated: false });
  expect(record).toMatchObject({ key: "a.md", updatedAt: null, issueCount: 0 });
  expect(record.values.get("stage")).toBe(7);
  expect(record.links.get("area")).toEqual([]);
  expect(parsed.guide).toBeNull();
});

it("keys embedded records and link targets by note path and node id", () => {
  const data: JsonObject = {
    Checklist: [
      {
        ref: {
          notePath: "releases/fall.md",
          kind: "EMBEDDED",
          nodeId: "chk-1",
          fragment: "checklist",
        },
        path: "releases/fall.md",
        title: "Fall checklist",
      },
    ],
    Release: [
      {
        path: "releases/fall.md",
        title: "Fall release",
        checklist: {
          ref: { kind: "EMBEDDED", nodeId: "chk-1" },
          path: "releases/fall.md",
          title: "Fall checklist",
        },
        works: [{ ref: { kind: "NOTE" }, path: PATHS.checkout, title: "Checkout redesign" }],
      },
    ],
  };

  const parsed = parseRecords(data, [TYPE_DOCS.Checklist, TYPE_DOCS.Release]);
  const [record] = parsed.pages.get("Checklist")?.records ?? [];
  const [release] = parsed.pages.get("Release")?.records ?? [];

  expect(release.links.get("checklist")?.map((target) => target.key)).toEqual([
    "releases/fall.md#chk-1",
  ]);
  expect(release.links.get("works")?.map((target) => target.key)).toEqual([PATHS.checkout]);
  expect(record.key).toBe("releases/fall.md#chk-1");
  expect(record.ref).toEqual({
    notePath: "releases/fall.md",
    kind: "EMBEDDED",
    nodeId: "chk-1",
    fragment: "checklist",
  });
});

it("summarizes a description by its first sentence", () => {
  expect(firstSentence("Planned work. Stories and bugs share one lifecycle.")).toBe(
    "Planned work.",
  );
  expect(firstSentence("Line one\ncontinues here.")).toBe("Line one continues here.");
  expect(firstSentence("")).toBe("");
});

it("parses the fixture's GraphQL data without losing records", () => {
  const concrete = Object.values(TYPE_DOCS).filter((doc) => doc.role !== "INTERFACE");
  const parsed = parseRecords(RECORDS_DATA, concrete);

  expect([...parsed.pages].map(([type, page]) => [type, page.records.length])).toEqual([
    ["Story", 4],
    ["Bug", 2],
    ["Area", 6],
    ["Release", 2],
    ["Checklist", 1],
    ["Person", 2],
    ["Memo", 2],
    ["Glossary", 3],
  ]);
});

it("labels an outside link with its target's own group before an interface's", () => {
  const root = (name: string, kind: "type" | "interface", implementors: string[] = []) => ({
    name,
    kind,
    label: name,
    pluralLabel: `${name}s`,
    count: 0,
    issueCount: 0,
    implementors,
    children: [],
  });

  // Alpha sorts first and lists an interface Gadget implements; Gadget itself is in Zeta.
  const groups: DisplayGroup[] = [
    { name: "Alpha", members: [root("Thing", "interface", ["Gadget"])] },
    { name: "Zeta", members: [root("Gadget", "type")] },
  ];

  const story: TypeDoc = {
    ...TYPE_DOCS.Story,
    fields: [...TYPE_DOCS.Story.fields, { name: "gadget", kind: "link", typeName: "Gadget" }],
  };

  const model = fixtureModel("Planning", { docs: { ...TYPE_DOCS, Story: story }, groups });
  const gadget = model.memberIndex.get("Work")?.links.find((link) => link.field === "gadget");

  expect(gadget).toMatchObject({ target: null, targetGroup: "Zeta" });
});

it("reads an authored Section summary into group records without a duplicate scalar", () => {
  const story: TypeDoc = {
    ...TYPE_DOCS.Story,
    summaryField: "overview",
    fields: [
      ...TYPE_DOCS.Story.fields.filter((field) => field.name !== "summary"),
      { name: "overview", kind: "section", typeName: "OverviewSection", list: false },
    ],
  };

  expect(recordsQuery([story], null)).toContain("overview { content }");

  const records = [
    {
      path: "Work/readable.md",
      title: "Readable",
      overview: { content: "The problem and why it matters." },
    },
    { path: "Work/missing.md", title: "Missing", overview: null },
  ];

  const model = fixtureModel("Planning", {
    docs: { ...TYPE_DOCS, Work: { ...TYPE_DOCS.Work, summaryField: "overview" }, Story: story },
    data: { ...RECORDS_DATA, Story: records },
  });

  expect(model.records.get("Work/readable.md")?.summary).toBe("The problem and why it matters.");
  expect(model.records.get("Work/missing.md")?.summary).toBeNull();
});
