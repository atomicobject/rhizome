// Synthetic API responses for the type Briefing's tests: the group fixtures in
// groups.ts plus what a collection reads beyond a group.
//
// - Work items (Story, Bug, and the Work interface) gain a `meetings` reverse
//   field that four of six work items fill and two of four stories fill.
// - Meeting, in a Log group, is a dated type: a KEY `date` that one record
//   leaves empty, people (`attendees`) and relation (`work`) fields, and four
//   records changed in one minute.
// - Each collection's switcher offers its Briefing and generated layouts;
//   stories also offer an authored Triage view.
import type { DisplayGroupsResponse, TypeDoc, TypeFieldDoc } from "@rhizome/kit";

import { isJsonObject, type JsonObject, type JsonValue } from "../api.ts";
import {
  BURST,
  DISPLAY_GROUPS,
  NOW,
  PATHS,
  RECORDS_DATA,
  TYPE_DOCS,
  TYPE_SUMMARIES,
  VIEW_CATALOG,
  profile,
} from "./groups.ts";

const DAY = 24 * 60 * 60 * 1000;

const MEETINGS_FIELD: TypeFieldDoc = {
  name: "meetings",
  kind: "reverse",
  typeName: "Meeting",
  list: true,
};

function withMeetings(doc: TypeDoc): TypeDoc {
  return {
    ...doc,
    fields: [...doc.fields, MEETINGS_FIELD],
    profile: doc.profile && { ...doc.profile, reverseFields: ["meetings"] },
  };
}

export const MEETING_PATHS = {
  kickoff: "log/kickoff-sync.md",
  review: "log/design-review.md",
  retro: "log/retro.md",
  planning: "log/planning.md",
  hallway: "log/hallway-chat.md",
} as const;

const MEETING: TypeDoc = {
  name: "Meeting",
  role: "NOTE",
  label: "Meeting",
  pluralLabel: "Meetings",
  profile: profile({
    // oxlint-disable-next-line anti-slop/no-shape-in-symbol-names -- `shape` is the profile's API field.
    shape: "dated",
    primaryDateField: "date",
    peopleFields: ["attendees"],
    relationFields: ["work"],
    gapFields: ["date"],
  }),
  fields: [
    { name: "date", kind: "scalar", typeName: "Date", display: { importance: "KEY" } },
    { name: "attendees", kind: "link", typeName: "Person", list: true },
    { name: "work", kind: "link", typeName: "Work", list: true },
  ],
};

export const COLLECTION_DOCS = {
  ...TYPE_DOCS,
  Story: withMeetings(TYPE_DOCS.Story),
  Bug: withMeetings(TYPE_DOCS.Bug),
  Work: withMeetings(TYPE_DOCS.Work),
  Meeting: MEETING,
} satisfies Record<string, TypeDoc>;

export const COLLECTION_GROUPS: DisplayGroupsResponse = {
  groups: [
    ...DISPLAY_GROUPS.groups,
    {
      name: "Log",
      members: [
        {
          name: "Meeting",
          kind: "type",
          label: "Meeting",
          pluralLabel: "Meetings",
          count: 5,
          issueCount: 0,
          implementors: [],
          children: [],
        },
      ],
    },
  ],
};

const target = (path: string, title: string, type: string | null) => ({
  path,
  title,
  resolvedType: type,
});

const ada = target(PATHS.ada, "Ada Lovelace", "Person");

const grace = target(PATHS.grace, "Grace Hopper", "Person");

const work = {
  checkout: target(PATHS.checkout, "Checkout redesign", "Story"),
  carts: target(PATHS.carts, "Saved carts", "Story"),
  tax: target(PATHS.tax, "Tax rounding", "Bug"),
  login: target(PATHS.login, "Login loop", "Bug"),
};

const meeting = {
  kickoff: target(MEETING_PATHS.kickoff, "Kickoff sync", "Meeting"),
  review: target(MEETING_PATHS.review, "Design review", "Meeting"),
  retro: target(MEETING_PATHS.retro, "Retro", "Meeting"),
};

function meetingRecord(
  path: string,
  title: string,
  updatedAt: number,
  fields: JsonObject,
): JsonObject {
  return {
    ref: { notePath: path, kind: "NOTE", typeName: "Meeting" },
    path,
    title,
    updatedAt: new Date(updatedAt).toISOString(),
    issueCount: 0,
    neighborhood: { truncated: false, nodes: [] },
    ...fields,
  };
}

const MEETINGS = [
  meetingRecord(MEETING_PATHS.kickoff, "Kickoff sync", BURST + 1_000, {
    date: "2026-10-01",
    attendees: [ada],
    work: [work.checkout, work.carts],
  }),
  meetingRecord(MEETING_PATHS.review, "Design review", BURST + 2_000, {
    date: "2026-09-29",
    attendees: [ada, grace],
    work: [work.checkout, work.tax],
  }),
  meetingRecord(MEETING_PATHS.retro, "Retro", BURST + 3_000, {
    date: "2026-09-15",
    attendees: [grace],
    work: [work.login],
  }),
  meetingRecord(MEETING_PATHS.planning, "Planning", BURST + 4_000, {
    date: "2026-08-04",
    attendees: [],
    work: [],
  }),
  meetingRecord(MEETING_PATHS.hallway, "Hallway chat", NOW - 2 * DAY, {
    date: null,
    attendees: [ada],
    work: [],
  }),
];

/** The meetings that link to each work item, by path. */
const MEETINGS_BY_WORK = new Map<JsonValue | undefined, JsonValue[]>([
  [PATHS.checkout, [meeting.kickoff, meeting.review]],
  [PATHS.carts, [meeting.kickoff]],
  [PATHS.tax, [meeting.review]],
  [PATHS.login, [meeting.retro]],
]);

const rowsWithMeetings = (rows: JsonValue | undefined) =>
  (Array.isArray(rows) ? rows : []).map((row) =>
    isJsonObject(row) ? { ...row, meetings: MEETINGS_BY_WORK.get(row.path) ?? [] } : row,
  );

/** GraphQL `data` answering every collection's records query. */
export const COLLECTION_DATA: JsonObject = {
  ...RECORDS_DATA,
  Story: rowsWithMeetings(RECORDS_DATA.Story),
  Bug: rowsWithMeetings(RECORDS_DATA.Bug),
  Meeting: MEETINGS,
};

const choice = (viewId: string, name: string, renderer: string, extra: JsonObject = {}) => ({
  id: `view:${JSON.stringify([viewId, renderer === "custom" ? "custom" : renderer])}`,
  name,
  renderer,
  viewId,
  ...extra,
});

/** A collection's switcher: its Briefing, then its generated Table. */
const collectionTarget = (kind: "type" | "interface", name: string, extra: JsonValue[] = []) => {
  const generated = `generated.${kind}.${name}.table`;

  return {
    kind,
    name,
    choices: [
      choice(`${kind}.briefing`, "Briefing", "custom"),
      choice(generated, "Table", "table", { variant: "table" }),
      ...extra,
    ],
  };
};

/** `GET /api/v1/ontology/types`, with meetings. */
export const COLLECTION_SUMMARIES: JsonValue = [
  ...(Array.isArray(TYPE_SUMMARIES) ? TYPE_SUMMARIES : []),
  { name: "Meeting", label: "Meeting", pluralLabel: "Meetings", displayGroup: "Log", count: 5 },
];

/** `GET /api/v1/views` with collection targets beside the group ones. */
export const COLLECTION_CATALOG: JsonObject = {
  views: [
    ...(Array.isArray(VIEW_CATALOG.views) ? VIEW_CATALOG.views : []),
    { id: "story.triage", name: "Triage", description: "Stories nobody has sized." },
  ],
  targets: [
    ...(Array.isArray(VIEW_CATALOG.targets) ? VIEW_CATALOG.targets : []),
    collectionTarget("type", "Story", [
      choice("story.triage", "Triage · Table", "table", { custom: true }),
    ]),
    collectionTarget("type", "Release"),
    collectionTarget("type", "Area"),
    collectionTarget("type", "Memo"),
    collectionTarget("type", "Meeting"),
    collectionTarget("interface", "Work"),
  ],
};
