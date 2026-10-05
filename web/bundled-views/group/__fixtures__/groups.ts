// Synthetic API responses for the group views' tests, in the shapes Rhizome
// serves. Each note type and interface carries the profile the server derives;
// the views read lifecycle, gap, and key text fields only from it. Three groups:
//
// - Planning: a Work interface (Story, Bug) whose lifecycle declares stages,
//   with a warning-toned `blocked` value in the active stage, plus a category
//   enum, a KEY field with a policy reason, a conditionally required field, a
//   link to a Person outside the group, and a self link; Area, a hierarchical
//   catalog type with a parent cycle and no lifecycle; Release, whose
//   `releaseStatus` lifecycle has stages inferred from authored tones and whose
//   link to Area no record uses; and Checklist, a display child of Release.
// - People: one root, no links.
// - Library: two roots whose only link field is typed by the broad Note
//   interface, so the group has no links. Its labels share "Library ". Memo's
//   lifecycle has inferred stages and no active one; Glossary's `status` enum
//   has `@view` order but no stages, so it is a category, not a lifecycle.
import type {
  DisplayGroupsResponse,
  EnumValueDoc,
  TypeDoc,
  TypeFieldDoc,
  TypeProfile,
} from "@rhizome/kit";

import type { JsonObject, JsonValue } from "../api.ts";

const HOUR = 60 * 60 * 1000;

const DAY = 24 * HOUR;

/** The fixtures' "now": local noon, so day boundaries hold in any time zone. */
export const NOW = new Date(2026, 9, 3, 12, 0, 0).getTime();

/** Four records changed within this minute, a day and two hours before NOW. */
export const BURST = NOW - 26 * HOUR;

export const PLANNING_GUIDE = "docs/planning-guide.md";

export const RELEASE_GUIDE = "docs/release-guide.md";

/** Declared stages, with authored tone on `blocked` and authored `collapsed` on `done`. */
const WORK_STAGE: EnumValueDoc[] = [
  {
    name: "backlog",
    label: "Backlog",
    order: 10,
    tone: "neutral",
    stage: "open",
    stageDeclared: true,
  },
  {
    name: "doing",
    label: "Doing",
    order: 20,
    tone: "progress",
    stage: "active",
    stageDeclared: true,
  },
  {
    name: "blocked",
    label: "Blocked",
    order: 30,
    tone: "warning",
    stage: "active",
    stageDeclared: true,
  },
  {
    name: "done",
    label: "Done",
    order: 90,
    tone: "success",
    collapsed: true,
    stage: "done",
    stageDeclared: true,
  },
  {
    name: "dropped",
    label: "Dropped",
    order: 100,
    tone: "muted",
    collapsed: true,
    stage: "dropped",
    stageDeclared: true,
  },
];

/** A category: no value carries `@view` tone or collapsed, so there are no stages. */
const WORK_KIND: EnumValueDoc[] = [{ name: "feature" }, { name: "chore" }];

const AREA_CATEGORY: EnumValueDoc[] = [{ name: "product" }, { name: "platform" }];

/** An authored tone infers stages: both values are `open`. */
const HEALTH: EnumValueDoc[] = [
  { name: "ok", label: "OK", stage: "open" },
  { name: "degraded", label: "Degraded", tone: "danger", stage: "open" },
];

/** Stages inferred from authored tones. */
const RELEASE_STATUS: EnumValueDoc[] = [
  { name: "planned", label: "Planned", order: 10, tone: "neutral", stage: "open" },
  { name: "shipping", label: "Shipping", order: 20, tone: "progress", stage: "active" },
  { name: "at_risk", label: "At risk", order: 30, tone: "risk", stage: "open" },
  { name: "shipped", label: "Shipped", order: 90, tone: "success", stage: "done" },
];

/** Stages inferred from authored tones, none of them `active`. */
const MEMO_STATUS: EnumValueDoc[] = [
  { name: "draft", label: "Draft", order: 10, tone: "neutral", stage: "open" },
  { name: "filed", label: "Filed", order: 20, tone: "muted", collapsed: true, stage: "dropped" },
];

/** `@view` order and labels without tones: no stages, so a category despite its name. */
const GLOSSARY_STATUS: EnumValueDoc[] = [
  { name: "proposed", label: "Proposed", order: 10 },
  { name: "agreed", label: "Agreed", order: 20 },
];

const KEY = { display: { importance: "KEY" } } as const;

const scalar = (name: string, extra: Partial<TypeFieldDoc> = {}): TypeFieldDoc => ({
  name,
  kind: "scalar",
  typeName: "String",
  ...extra,
});

const enumField = (
  name: string,
  values: EnumValueDoc[],
  extra: Partial<TypeFieldDoc> = {},
): TypeFieldDoc => ({ name, kind: "enum", typeName: `${name}Enum`, enum: { values }, ...extra });

const link = (name: string, target: string, extra: Partial<TypeFieldDoc> = {}): TypeFieldDoc => ({
  name,
  kind: "link",
  typeName: target,
  ...extra,
});

const summaryField = scalar("summary", { required: true, display: { role: "SUMMARY" } });

const nextStep = scalar("nextStep", {
  ...KEY,
  policy: { reason: "Name the next concrete step so the work can move." },
});

const typeDoc = (doc: Omit<TypeDoc, "role">): TypeDoc => ({ role: "NOTE", ...doc });

/** A profile as the server reports it, with omitted lists empty. */
export const profile = (fields: Partial<TypeProfile>): TypeProfile => ({
  // oxlint-disable-next-line anti-slop/no-shape-in-symbol-names -- `shape` is the profile's API field.
  shape: "reference",
  orderedFields: [],
  categoryFields: [],
  peopleFields: [],
  keyTextFields: [],
  relationFields: [],
  reverseFields: [],
  gapFields: [],
  ...fields,
});

const WORK_PROFILE: Partial<TypeProfile> = {
  // oxlint-disable-next-line anti-slop/no-shape-in-symbol-names -- `shape` is the profile's API field.
  shape: "workflow",
  lifecycleField: "stage",
  summaryField: "summary",
  relationFields: ["area"],
};

export const TYPE_DOCS = {
  Story: typeDoc({
    name: "Story",
    label: "Story",
    pluralLabel: "Stories",
    implements: ["Work"],
    summaryField: "summary",
    companionDocs: [{ path: PLANNING_GUIDE }],
    profile: profile({
      ...WORK_PROFILE,
      orderedFields: ["health"],
      categoryFields: ["kind"],
      peopleFields: ["owner"],
      keyTextFields: ["nextStep"],
      relationFields: ["area", "blockedBy"],
      gapFields: ["kind", "nextStep", "area"],
    }),
    fields: [
      summaryField,
      enumField("kind", WORK_KIND, KEY),
      enumField("stage", WORK_STAGE, { ...KEY, required: true }),
      nextStep,
      link("area", "Area", KEY),
      link("owner", "Person"),
      link("blockedBy", "Work"),
      enumField("health", HEALTH),
      scalar("estimate", { typeName: "Int" }),
    ],
  }),
  Bug: typeDoc({
    name: "Bug",
    label: "Bug",
    pluralLabel: "Bugs",
    implements: ["Work"],
    summaryField: "summary",
    companionDocs: [{ path: PLANNING_GUIDE }],
    profile: profile({
      ...WORK_PROFILE,
      keyTextFields: ["nextStep", "resolution"],
      gapFields: ["nextStep", "area"],
    }),
    fields: [
      summaryField,
      enumField("stage", WORK_STAGE, { ...KEY, required: true }),
      nextStep,
      link("area", "Area", KEY),
      scalar("resolution", { ...KEY, requiredWhen: [{ field: "stage", equals: "done" }] }),
    ],
  }),
  Area: typeDoc({
    name: "Area",
    label: "Area",
    pluralLabel: "Areas",
    summaryField: "summary",
    parentField: "parent",
    companionDocs: [{ path: PLANNING_GUIDE }],
    profile: profile({
      // oxlint-disable-next-line anti-slop/no-shape-in-symbol-names -- `shape` is the profile's API field.
      shape: "catalog",
      categoryFields: ["category"],
      summaryField: "summary",
      relationFields: ["parent"],
      gapFields: ["category"],
    }),
    fields: [
      summaryField,
      enumField("category", AREA_CATEGORY, KEY),
      link("parent", "Area", { display: { role: "PARENT" } }),
    ],
  }),
  Release: typeDoc({
    name: "Release",
    label: "Release",
    pluralLabel: "Releases",
    companionDocs: [{ path: RELEASE_GUIDE }],
    profile: profile({
      // oxlint-disable-next-line anti-slop/no-shape-in-symbol-names -- `shape` is the profile's API field.
      shape: "workflow",
      lifecycleField: "releaseStatus",
      orderedFields: ["health"],
      primaryDateField: "date",
      keyTextFields: ["notes"],
      relationFields: ["works", "checklist", "area"],
      gapFields: ["date"],
    }),
    fields: [
      enumField("releaseStatus", RELEASE_STATUS, KEY),
      link("works", "Work", { list: true }),
      link("checklist", "Checklist"),
      link("area", "Area"),
      enumField("health", HEALTH),
      scalar("date", { ...KEY, typeName: "Date" }),
      scalar("notes", { ...KEY, required: true }),
    ],
  }),
  Checklist: typeDoc({
    name: "Checklist",
    label: "Checklist",
    pluralLabel: "Checklists",
    profile: profile({}),
    fields: [scalar("items")],
  }),
  Person: typeDoc({
    name: "Person",
    label: "Person",
    pluralLabel: "People",
    profile: profile({}),
    fields: [scalar("displayName", { required: true })],
  }),
  Memo: typeDoc({
    name: "Memo",
    label: "Library memo",
    pluralLabel: "Library memos",
    summaryField: "summary",
    profile: profile({
      // oxlint-disable-next-line anti-slop/no-shape-in-symbol-names -- `shape` is the profile's API field.
      shape: "contract",
      lifecycleField: "memoStatus",
      summaryField: "summary",
      relationFields: ["related"],
    }),
    fields: [
      summaryField,
      enumField("memoStatus", MEMO_STATUS, KEY),
      link("related", "Note", { list: true }),
    ],
  }),
  Glossary: typeDoc({
    name: "Glossary",
    label: "Library glossary",
    pluralLabel: "Library glossaries",
    profile: profile({
      // oxlint-disable-next-line anti-slop/no-shape-in-symbol-names -- `shape` is the profile's API field.
      shape: "catalog",
      categoryFields: ["status"],
      keyTextFields: ["term"],
      gapFields: ["term"],
    }),
    fields: [scalar("term", KEY), enumField("status", GLOSSARY_STATUS)],
  }),
  Work: {
    name: "Work",
    role: "INTERFACE",
    label: "Work item",
    pluralLabel: "Work items",
    summaryField: "summary",
    profile: profile({
      ...WORK_PROFILE,
      keyTextFields: ["nextStep"],
      gapFields: ["nextStep", "area"],
    }),
    fields: [
      summaryField,
      enumField("stage", WORK_STAGE, { ...KEY, required: true }),
      nextStep,
      link("area", "Area", KEY),
    ],
  },
} satisfies Record<string, TypeDoc>;

const member = (
  name: string,
  labels: [label: string, plural: string],
  count: number,
  extra: Partial<DisplayGroupsResponse["groups"][number]["members"][number]> = {},
) => ({
  name,
  kind: "type" as const,
  label: labels[0],
  pluralLabel: labels[1],
  count,
  issueCount: 0,
  implementors: [],
  children: [],
  ...extra,
});

export const DISPLAY_GROUPS: DisplayGroupsResponse = {
  groups: [
    {
      name: "Library",
      members: [
        member("Memo", ["Library memo", "Library memos"], 2),
        member("Glossary", ["Library glossary", "Library glossaries"], 3),
      ],
    },
    { name: "People", members: [member("Person", ["Person", "People"], 2)] },
    {
      name: "Planning",
      members: [
        member("Work", ["Work item", "Work items"], 6, {
          kind: "interface",
          description: "Planned work. Stories and bugs share one lifecycle.",
          issueCount: 2,
          implementors: ["Story", "Bug"],
          children: [
            member("Story", ["Story", "Stories"], 4, { issueCount: 2 }),
            member("Bug", ["Bug", "Bugs"], 2),
          ],
        }),
        member("Area", ["Area", "Areas"], 6, {
          description: "A product or platform area. Areas nest under parent areas.",
        }),
        member("Release", ["Release", "Releases"], 2, {
          children: [member("Checklist", ["Checklist", "Checklists"], 1)],
        }),
      ],
    },
  ],
};

const iso = (time: number) => new Date(time).toISOString();

const target = (path: string, title: string, type: string | null = null) => ({
  path,
  title,
  resolvedType: type,
});

type RecordOptions = {
  updatedAt: number;
  issueCount?: number;
  /** Notes linked with the record in either direction, typed or not, as `neighborhood` lists them. */
  neighbors?: JsonValue[];
  fields?: JsonObject;
};

function record(type: string, path: string, title: string, options: RecordOptions): JsonObject {
  return {
    ref: {
      notePath: path,
      kind: "NOTE",
      fragment: null,
      nodeId: null,
      typeName: type,
      structuralFingerprint: null,
    },
    path,
    title,
    updatedAt: iso(options.updatedAt),
    issueCount: options.issueCount ?? 0,
    neighborhood: { truncated: false, nodes: options.neighbors ?? [] },
    ...options.fields,
  };
}

export const PATHS = {
  checkout: "work/checkout-redesign.md",
  carts: "work/saved-carts.md",
  gifts: "work/gift-cards.md",
  wishlist: "work/wishlist.md",
  tax: "work/tax-rounding.md",
  login: "work/login-loop.md",
  platform: "areas/platform.md",
  payments: "areas/payments.md",
  wishlists: "areas/wishlists.md",
  commerce: "areas/commerce.md",
  loopA: "areas/loop-a.md",
  loopB: "areas/loop-b.md",
  fall: "releases/fall.md",
  winter: "releases/winter.md",
  fallChecklist: "releases/fall-checklist.md",
  ada: "people/ada.md",
  grace: "people/grace.md",
  kickoff: "notes/kickoff.md",
  scratch: "notes/scratch.md",
} as const;

const ada = target(PATHS.ada, "Ada Lovelace", "Person");

const kickoff = target(PATHS.kickoff, "Kickoff");

const payments = target(PATHS.payments, "Payments", "Area");

const STORIES = [
  record("Story", PATHS.checkout, "Checkout redesign", {
    updatedAt: NOW - HOUR,
    neighbors: [payments, ada, target(PLANNING_GUIDE, "Planning guide"), kickoff],
    fields: {
      summary: "Rebuild checkout around saved payment methods.",
      kind: "feature",
      stage: "doing",
      nextStep: "Review the payment step",
      area: payments,
      owner: ada,
      blockedBy: null,
      health: "degraded",
    },
  }),
  record("Story", PATHS.carts, "Saved carts", {
    updatedAt: NOW - 40 * DAY,
    neighbors: [kickoff],
    fields: {
      summary: "Keep carts across sessions.",
      kind: "feature",
      stage: "doing",
      nextStep: "",
      area: payments,
      owner: null,
      blockedBy: target(PATHS.checkout, "Checkout redesign", "Story"),
      health: "ok",
    },
  }),
  record("Story", PATHS.gifts, "Gift cards", {
    updatedAt: NOW - 3 * DAY,
    issueCount: 2,
    fields: {
      summary: "Sell and redeem gift cards.",
      kind: "chore",
      stage: "backlog",
      nextStep: null,
      area: null,
    },
  }),
  record("Story", PATHS.wishlist, "Wishlist", {
    updatedAt: NOW - 10 * DAY,
    fields: {
      summary: "Let shoppers save items for later.",
      kind: "feature",
      stage: "done",
      nextStep: "Ship it",
      area: target(PATHS.wishlists, "Wishlists", "Area"),
    },
  }),
];

const BUGS = [
  record("Bug", PATHS.tax, "Tax rounding", {
    updatedAt: NOW - 2 * HOUR,
    neighbors: [ada, target(PATHS.scratch, "Scratch", "_Note")],
    fields: {
      summary: "Totals round tax per line instead of per order.",
      stage: "blocked",
      nextStep: "Wait for finance",
      area: payments,
      resolution: null,
    },
  }),
  record("Bug", PATHS.login, "Login loop", {
    updatedAt: NOW - 5 * DAY,
    fields: {
      summary: "Expired sessions redirect forever.",
      stage: "done",
      nextStep: null,
      area: target(PATHS.platform, "Platform", "Area"),
      resolution: "Fixed session cookie",
    },
  }),
];

const area = (
  path: string,
  title: string,
  updatedAt: number,
  parent: { path: string; title: string } | null,
  category: string | null = "product",
) =>
  record("Area", path, title, {
    updatedAt,
    fields: {
      summary: `${title} work.`,
      category,
      parent: parent && target(parent.path, parent.title, "Area"),
    },
  });

const commerce = { path: PATHS.commerce, title: "Commerce" };

const AREAS = [
  area(PATHS.platform, "Platform", NOW - 20 * DAY, null, "platform"),
  area(PATHS.payments, "Payments", NOW - 21 * DAY, commerce),
  area(PATHS.wishlists, "Wishlists", NOW - 22 * DAY, commerce),
  area(PATHS.commerce, "Commerce", NOW - 23 * DAY, null),
  area(PATHS.loopA, "Loop A", BURST + 20_000, { path: PATHS.loopB, title: "Loop B" }, null),
  area(PATHS.loopB, "Loop B", BURST + 30_000, { path: PATHS.loopA, title: "Loop A" }),
];

const RELEASES = [
  record("Release", PATHS.fall, "Fall release", {
    updatedAt: BURST,
    neighbors: [target(RELEASE_GUIDE, "Release guide")],
    fields: {
      releaseStatus: "at_risk",
      works: [
        target(PATHS.checkout, "Checkout redesign", "Story"),
        target(PATHS.tax, "Tax rounding", "Bug"),
      ],
      checklist: target(PATHS.fallChecklist, "Fall checklist", "Checklist"),
      area: null,
      health: "degraded",
      date: "2026-10-15",
      notes: "Cut on Friday",
    },
  }),
  record("Release", PATHS.winter, "Winter release", {
    updatedAt: BURST + 10_000,
    fields: {
      releaseStatus: "planned",
      works: [],
      checklist: null,
      area: null,
      health: "ok",
      date: null,
      notes: "",
    },
  }),
];

const CHECKLISTS = [
  record("Checklist", PATHS.fallChecklist, "Fall checklist", { updatedAt: NOW - 30 * HOUR }),
];

const PEOPLE = [
  record("Person", PATHS.ada, "Ada Lovelace", {
    updatedAt: NOW - 2 * DAY,
    fields: { displayName: "Ada" },
  }),
  record("Person", PATHS.grace, "Grace Hopper", {
    updatedAt: NOW - 4 * DAY,
    fields: { displayName: "Grace" },
  }),
];

const MEMOS = [
  record("Memo", "library/style.md", "Style memo", {
    updatedAt: NOW - 6 * DAY,
    fields: {
      summary: "House style.",
      memoStatus: "draft",
      related: [target("library/api.md", "API", "Glossary")],
    },
  }),
  record("Memo", "library/naming.md", "Naming memo", {
    updatedAt: NOW - 7 * DAY,
    fields: { summary: "Naming rules.", memoStatus: "filed", related: [] },
  }),
];

const GLOSSARY = [
  record("Glossary", "library/api.md", "API", {
    updatedAt: NOW - 8 * DAY,
    fields: { term: "Application programming interface", status: "agreed" },
  }),
  record("Glossary", "library/sdk.md", "SDK", {
    updatedAt: NOW - 9 * DAY,
    fields: { term: "Software development kit", status: "proposed" },
  }),
  record("Glossary", "library/cli.md", "CLI", {
    updatedAt: NOW - 11 * DAY,
    fields: { term: null },
  }),
];

/**
 * GraphQL `data` answering every group's records query: each concrete type
 * under its alias, and the Planning guide note.
 */
export const RECORDS_DATA: JsonObject = {
  Story: STORIES,
  Bug: BUGS,
  Area: AREAS,
  Release: RELEASES,
  Checklist: CHECKLISTS,
  Person: PEOPLE,
  Memo: MEMOS,
  Glossary: GLOSSARY,
  groupGuide: {
    path: PLANNING_GUIDE,
    title: "Planning guide",
    frontmatter: { summary: "How planning records fit together." },
  },
};

const mount = (group: string, extra: JsonObject = {}) => ({ kind: "group", group, ...extra });

const VIEW_NAMES = {
  "group.briefing": "Briefing",
  "group.trace": "Trace",
  "group.sections": "Sections",
  "planning.board": "Planning board",
  "team.dashboard": "Team dashboard",
} as const;

/** A group's switcher choices as the catalog lists them: Overview, then views. */
const groupTarget = (name: string, viewIds: (keyof typeof VIEW_NAMES)[]) => ({
  kind: "group",
  name,
  choices: [
    { id: "builtin:overview", name: "Overview", renderer: "overview" },
    ...viewIds.map((viewId) => ({
      id: `view:${JSON.stringify([viewId, "custom"])}`,
      name: VIEW_NAMES[viewId],
      renderer: "custom",
      viewId,
    })),
  ],
});

/** `GET /api/v1/views`, trimmed to the fields the views read. */
export const VIEW_CATALOG: JsonObject = {
  views: [
    {
      id: "planning.board",
      name: "Planning board",
      origin: "repository",
      mount: mount("Planning"),
    },
    {
      id: "library.hidden",
      name: "Hidden",
      origin: "repository",
      mount: mount("Library", { hidden: true }),
    },
    { id: "group.sections", name: "Sections", origin: "bundled", mount: mount("*", { order: 3 }) },
    {
      id: "team.dashboard",
      name: "Team dashboard",
      description: "Who is on what.",
      origin: "repository",
      mount: mount("*"),
    },
    {
      id: "work.table",
      name: "Work",
      origin: "generated",
      mount: { kind: "interface", interface: "Work" },
    },
  ],
  targets: [
    groupTarget("Library", ["group.briefing", "group.sections", "team.dashboard"]),
    groupTarget("People", ["group.briefing", "group.sections", "team.dashboard"]),
    groupTarget("Planning", [
      "group.briefing",
      "group.trace",
      "group.sections",
      "planning.board",
      "team.dashboard",
    ]),
  ],
};

const summary = (name: string, label: string, pluralLabel: string, displayGroup = "") => ({
  name,
  label,
  pluralLabel,
  displayGroup,
  count: 1,
});

/** `GET /api/v1/ontology/types`. */
export const TYPE_SUMMARIES: JsonValue = [
  summary("Story", "Story", "Stories", "Planning"),
  summary("Bug", "Bug", "Bugs", "Planning"),
  summary("Area", "Area", "Areas", "Planning"),
  summary("Release", "Release", "Releases", "Planning"),
  summary("Checklist", "Checklist", "Checklists"),
  summary("Person", "Person", "People", "People"),
  summary("Memo", "Library memo", "Library memos", "Library"),
  summary("Glossary", "Library glossary", "Library glossaries", "Library"),
];
