// Synthetic responses for the Overview and the All notes Briefing, over the
// groups in groups.ts plus the API's `Other` group holding one ungrouped type
// (Meeting) and an embedded type (Task) that the scope leaves out.
//
// In Planning, Story and Bug link to Area mostly through relation fields, Bug
// links to Story (inside the Work member), Release links to Bug and Checklist,
// and Release's `area` relation is declared but unused. Story links to Person
// (People) and to untyped notes, outside the group. Library's only links are
// plain links from Memo to untyped notes.
import type { DisplayGroupsResponse } from "@rhizome/kit";

import type { JsonObject, JsonValue } from "../api.ts";
import { parseAggregate, parseTypeSummaries, UNTYPED } from "../aggregate.ts";
import { DISPLAY_GROUPS, NOW } from "./groups.ts";

/** Unix seconds, as the aggregate reports change times. */
const seconds = (hoursAgo: number) => Math.floor((NOW - hoursAgo * 3_600_000) / 1000);

const type = (name: string, count: number, extra: JsonObject = {}): JsonObject => ({
  name,
  kind: "type",
  count,
  issueCount: 0,
  lastChanged: count ? seconds(count) : 0,
  lifecycle: null,
  gaps: [],
  targetSets: [],
  ...extra,
});

const stages = (field: string, values: Record<string, number>) => ({
  field,
  values: Object.entries(values).map(([name, count]) => ({ name, count })),
});

const sets = (...entries: [string[], number][]) =>
  entries.map(([types, records]) => ({ types, records }));

const pair = (
  a: string,
  b: string,
  relationLinks: number,
  plainLinks: number,
  fields: [string, string, number][] = [],
): JsonObject => ({
  a,
  b,
  links: relationLinks + plainLinks,
  relationLinks,
  plainLinks,
  fields: fields.map(([owner, field, count]) => ({ type: owner, field, count })),
});

export const AGGREGATE_MEMBERS: JsonObject = {
  types: [
    type("Story", 4, {
      issueCount: 2,
      lifecycle: stages("stage", { backlog: 1, doing: 2, done: 1 }),
      gaps: [
        { field: "nextStep", empty: 1 },
        { field: "area", empty: 2 },
        { field: "kind", empty: 0 },
      ],
      targetSets: sets([["Area"], 3], [[UNTYPED], 1]),
    }),
    type("Bug", 2, {
      lifecycle: stages("stage", { doing: 1, blocked: 1 }),
      gaps: [{ field: "nextStep", empty: 2 }],
      targetSets: sets([["Area", "Release"], 1]),
    }),
    type("Area", 6, {
      gaps: [{ field: "category", empty: 1 }],
      targetSets: sets([["Story"], 3], [["Bug", "Release"], 1]),
    }),
    type("Release", 2, {
      lifecycle: stages("releaseStatus", { planned: 1, shipping: 1 }),
      targetSets: sets([["Bug", "Checklist"], 1]),
    }),
    type("Checklist", 1, { targetSets: sets([["Release"], 1]) }),
    type("Person", 2, { targetSets: sets([["Story", "Meeting"], 2]) }),
    type("Memo", 2, {
      lifecycle: stages("memoStatus", { draft: 2 }),
      targetSets: sets([[UNTYPED], 1]),
    }),
    type("Glossary", 3),
    type("Meeting", 5, { targetSets: sets([["Person", UNTYPED], 3]) }),
    type("Notice", 0),
  ],
  interfaces: [
    {
      name: "Work",
      count: 6,
      issueCount: 2,
      lastChanged: seconds(1),
      lifecycle: stages("stage", { backlog: 1, doing: 3, blocked: 1, done: 1 }),
      gaps: [{ field: "nextStep", empty: 3 }],
      implementors: ["Story", "Bug"],
    },
  ],
  untyped: { count: 20, links: 11 },
};

export const AGGREGATE_LINKS: JsonObject = {
  pairs: [
    pair("Area", "Area", 2, 0, [["Area", "parent", 2]]),
    pair("Area", "Story", 4, 1, [["Story", "area", 4]]),
    pair("Area", "Bug", 1, 0, [["Bug", "area", 1]]),
    pair("Bug", "Release", 1, 0, [["Release", "works", 1]]),
    pair("Bug", "Story", 2, 0, [["Story", "blockedBy", 2]]),
    pair("Checklist", "Release", 1, 0, [["Release", "checklist", 1]]),
    pair("Person", "Story", 2, 0, [["Story", "owner", 2]]),
    pair("Meeting", "Person", 0, 4),
    pair("Story", UNTYPED, 0, 3),
    pair("Meeting", UNTYPED, 0, 6),
    pair("Memo", UNTYPED, 0, 2),
  ],
};

export const AGGREGATE_FOLDERS: JsonObject = {
  rows: [
    {
      folder: "Notes",
      total: 30,
      untyped: 12,
      typed: { Meeting: 5 },
      untypedLinksTo: { Person: 2, Meeting: 4, Story: 1, Task: 9 },
    },
    { folder: "Work", total: 10, untyped: 0, typed: { Story: 4 }, untypedLinksTo: {} },
    { folder: "Inbox", total: 8, untyped: 8, typed: {}, untypedLinksTo: {} },
  ],
};

const FACTS = {
  rebuilding: false,
  totalNotes: 49,
  typedNotes: 29,
  untypedNotes: 20,
  ambiguousNotes: 3,
};

/** One response per part, as the Overview requests them. */
export const AGGREGATE_PARTS = {
  members: { ...FACTS, members: AGGREGATE_MEMBERS },
  links: { ...FACTS, links: AGGREGATE_LINKS },
  folders: { ...FACTS, folders: AGGREGATE_FOLDERS },
} satisfies Record<string, JsonObject>;

export const AGGREGATE = parseAggregate({
  ...FACTS,
  members: AGGREGATE_MEMBERS,
  links: AGGREGATE_LINKS,
  folders: AGGREGATE_FOLDERS,
});

const summary = (
  name: string,
  label: string,
  pluralLabel: string,
  extra: JsonObject = {},
): JsonObject => ({ name, label, pluralLabel, displayGroup: "", role: "note", count: 1, ...extra });

/** `GET /api/v1/ontology/types`, with roles, descriptions, and counts. */
export const SUMMARIES_JSON: JsonValue = [
  summary("Story", "Story", "Stories", { displayGroup: "Planning", count: 4 }),
  summary("Bug", "Bug", "Bugs", { displayGroup: "Planning", count: 2 }),
  summary("Area", "Area", "Areas", {
    displayGroup: "Planning",
    count: 6,
    description: "A product or platform area. Areas nest.",
  }),
  summary("Release", "Release", "Releases", { displayGroup: "Planning", count: 2 }),
  summary("Checklist", "Checklist", "Checklists", { count: 1 }),
  summary("Person", "Person", "People", { displayGroup: "People", count: 2 }),
  summary("Memo", "Library memo", "Library memos", { displayGroup: "Library", count: 2 }),
  summary("Glossary", "Library glossary", "Library glossaries", {
    displayGroup: "Library",
    count: 3,
  }),
  summary("Meeting", "Meeting", "Meetings", {
    count: 5,
    description: "A conversation with notes. Usually weekly.",
  }),
  summary("Notice", "Notice", "Notices", { count: 0 }),
  summary("Task", "Task", "Tasks", { role: "embedded", count: 7 }),
];

export const SUMMARIES = parseTypeSummaries(SUMMARIES_JSON);

const ungrouped = (name: string, label: string, plural: string, count: number) => ({
  name,
  kind: "type" as const,
  label,
  pluralLabel: plural,
  count,
  issueCount: 0,
  implementors: [],
  children: [],
});

/** The fixture groups plus the API's `Other` group of ungrouped roots, which nests an embedded type. */
export const SCOPE_GROUPS: DisplayGroupsResponse = {
  groups: [
    ...DISPLAY_GROUPS.groups,
    {
      name: "Other",
      members: [
        {
          ...ungrouped("Meeting", "Meeting", "Meetings", 5),
          children: [ungrouped("Task", "Task", "Tasks", 7)],
        },
        ungrouped("Notice", "Notice", "Notices", 0),
      ],
    },
  ],
};
