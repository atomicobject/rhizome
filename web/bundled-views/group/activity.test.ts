import { expect, it } from "vitest";

import {
  BURST,
  NOW,
  PATHS,
  RECORDS_DATA,
  RELEASE_GUIDE,
  TYPE_DOCS,
} from "./__fixtures__/groups.ts";
import { fixtureModel } from "./__fixtures__/harness.tsx";
import {
  BURST_SIZE,
  boundary,
  inMotion,
  needsAttention,
  recentChanges,
  type AttentionSignal,
} from "./activity.ts";
import { isJsonObject } from "./api.ts";
import { linkGraph } from "./graph.ts";
import type { GroupRecord } from "./model.ts";

const planning = fixtureModel("Planning");

const titles = (records: readonly GroupRecord[]) => records.map((record) => record.title);

/** Work's documentation with `reviewer` among its profile's gap fields. */
const withReviewerGap = {
  ...TYPE_DOCS.Work,
  profile: {
    ...TYPE_DOCS.Work.profile,
    gapFields: [...TYPE_DOCS.Work.profile.gapFields, "reviewer"],
  },
};

function describe(signal: AttentionSignal) {
  switch (signal.kind) {
    case "issues":
      return { kind: signal.kind, records: titles(signal.records) };
    case "tone":
      return {
        kind: signal.kind,
        field: signal.field,
        value: signal.value.name,
        tone: signal.tone,
        byMember: signal.byMember.map((entry) => [entry.member, titles(entry.records)]),
      };
    case "stale":
      return { kind: signal.kind, member: signal.member, records: titles(signal.records) };
    case "gap":
      return {
        kind: signal.kind,
        member: signal.member,
        field: signal.field,
        empty: titles(signal.records),
        total: signal.total,
        policyReason: signal.policyReason,
      };
    case "unlinked":
      return {
        kind: signal.kind,
        byMember: signal.byMember.map((entry) => [entry.member, titles(entry.records)]),
      };
  }
}

it("lists what needs attention, in order, with the records behind each signal", () => {
  const signals = needsAttention(planning, linkGraph(planning), NOW).map(describe);

  expect(signals).toEqual([
    { kind: "issues", records: ["Gift cards"] },
    // Danger and risk before warning; values aggregate across members by field and value.
    {
      kind: "tone",
      field: "health",
      value: "degraded",
      tone: "danger",
      byMember: [
        ["Work", ["Checkout redesign"]],
        ["Release", ["Fall release"]],
      ],
    },
    {
      kind: "tone",
      field: "releaseStatus",
      value: "at_risk",
      tone: "risk",
      byMember: [["Release", ["Fall release"]]],
    },
    {
      kind: "tone",
      field: "stage",
      value: "blocked",
      tone: "warning",
      byMember: [["Work", ["Tax rounding"]]],
    },
    { kind: "stale", member: "Work", records: ["Saved carts"] },
    // Required (notes), conditionally required (resolution), and lifecycle (stage) fields are skipped.
    {
      kind: "gap",
      member: "Work",
      field: "nextStep",
      empty: ["Saved carts", "Gift cards", "Login loop"],
      total: 6,
      policyReason: "Name the next concrete step so the work can move.",
    },
    {
      kind: "gap",
      member: "Work",
      field: "area",
      empty: ["Gift cards"],
      total: 6,
      policyReason: null,
    },
    {
      kind: "gap",
      member: "Area",
      field: "category",
      empty: ["Loop A"],
      total: 6,
      policyReason: null,
    },
    {
      kind: "gap",
      member: "Release",
      field: "date",
      empty: ["Winter release"],
      total: 2,
      policyReason: null,
    },
    {
      kind: "unlinked",
      // Areas in the tree count as linked: Commerce has children, and Loop A
      // and Loop B are each other's parent.
      byMember: [
        ["Work", ["Gift cards"]],
        ["Release", ["Winter release"]],
      ],
    },
  ]);
});

it("counts empty gap fields only on records whose type declares them", () => {
  const kind = needsAttention(planning, linkGraph(planning), NOW).find(
    (signal) => signal.kind === "gap" && signal.field === "kind",
  );

  // Bugs have no kind field, so their missing values do not count.
  expect(kind).toBeUndefined();
});

it("reports one empty gap signal per name when implementors declare it differently", () => {
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
    Work: withReviewerGap,
  };

  const model = fixtureModel("Planning", { docs });
  const work = model.memberIndex.get("Work");

  const reviewers = needsAttention(model, linkGraph(model), NOW).filter(
    (signal) => signal.kind === "gap" && signal.field === "reviewer",
  );

  expect(reviewers).toHaveLength(1);
  expect(reviewers[0]).toMatchObject({ total: work?.records.length });
});

it("shows an empty gap field's policy reason only when it applies to every listed record", () => {
  const reviewer = (kind: "scalar" | "link", typeName: string, reason: string) => ({
    name: "reviewer",
    kind,
    typeName,
    display: { importance: "KEY" as const },
    policy: { reason },
  });

  const signalFor = (story: string, bug: string) => {
    const docs = {
      ...TYPE_DOCS,
      Story: {
        ...TYPE_DOCS.Story,
        fields: [...TYPE_DOCS.Story.fields, reviewer("scalar", "String", story)],
      },
      Bug: { ...TYPE_DOCS.Bug, fields: [...TYPE_DOCS.Bug.fields, reviewer("link", "Person", bug)] },
      Work: withReviewerGap,
    };

    const model = fixtureModel("Planning", { docs });

    return needsAttention(model, linkGraph(model), NOW).find(
      (signal) => signal.kind === "gap" && signal.field === "reviewer",
    );
  };

  expect(signalFor("Name a reviewer.", "Name a reviewer.")).toMatchObject({
    policyReason: "Name a reviewer.",
  });
  expect(signalFor("Stories need one.", "Bugs need one.")).toMatchObject({ policyReason: null });
});

it("reports unlinked records only when the group has links", () => {
  const library = fixtureModel("Library");
  const signals = needsAttention(library, linkGraph(library), NOW).map(describe);

  expect(signals).toEqual([
    {
      kind: "gap",
      member: "Glossary",
      field: "term",
      empty: ["CLI"],
      total: 3,
      policyReason: null,
    },
  ]);
});

it("treats in-progress work as stale only after thirty days", () => {
  const day = 24 * 60 * 60 * 1000;
  const carts = planning.byPath.get(PATHS.carts)?.updatedAt ?? 0;

  const stale = (now: number) =>
    needsAttention(planning, linkGraph(planning), now).filter((signal) => signal.kind === "stale");

  expect(stale(carts + 30 * day)).toEqual([]);
  expect(stale(carts + 30 * day + 1)).toHaveLength(1);
  // Every active-stage value goes stale, the warning-toned one included.
  expect(stale(NOW + 31 * day).map(describe)).toEqual([
    {
      kind: "stale",
      member: "Work",
      records: ["Saved carts", "Tax rounding", "Checkout redesign"],
    },
  ]);
});

it("flags only the profile's gap fields as empty", () => {
  const work = {
    ...TYPE_DOCS.Work,
    profile: { ...TYPE_DOCS.Work.profile, gapFields: ["nextStep"] },
  };

  const model = fixtureModel("Planning", { docs: { ...TYPE_DOCS, Work: work } });

  const empty = needsAttention(model, linkGraph(model), NOW).flatMap((signal) =>
    signal.kind === "gap" && signal.member === "Work" ? [signal.field] : [],
  );

  // Area is a KEY field left empty on Gift cards, but the profile does not list it.
  expect(empty).toEqual(["nextStep"]);
});

it("flags warning values held in a list enum", () => {
  const flags = {
    name: "flags",
    kind: "enum" as const,
    typeName: "Flag",
    list: true,
    enum: { values: [{ name: "late", label: "Late", tone: "warning" }, { name: "small" }] },
  };

  const releases = Array.isArray(RECORDS_DATA.Release) ? RECORDS_DATA.Release : [];
  const release = { ...TYPE_DOCS.Release, fields: [...TYPE_DOCS.Release.fields, flags] };

  const model = fixtureModel("Planning", {
    docs: { ...TYPE_DOCS, Release: release },
    data: {
      ...RECORDS_DATA,
      Release: releases.map((entry) =>
        isJsonObject(entry) && entry.path === PATHS.winter
          ? { ...entry, flags: ["small", "late"] }
          : entry,
      ),
    },
  });

  const late = needsAttention(model, linkGraph(model), NOW).find(
    (signal) => signal.kind === "tone" && signal.field === "flags",
  );

  expect(late && describe(late)).toEqual({
    kind: "tone",
    field: "flags",
    value: "late",
    tone: "warning",
    byMember: [["Release", ["Winter release"]]],
  });
});

it("lists active-stage records by member, newest first, with their first key text or summary", () => {
  const motion = inMotion(planning);

  expect(
    motion.groups.map((group) => [
      group.member,
      group.entries.map((entry) => [entry.record.title, entry.note]),
    ]),
  ).toEqual([
    [
      "Work",
      [
        ["Checkout redesign", { label: "Next step", text: "Review the payment step" }],
        // Blocked carries the warning tone, but its stage is active.
        ["Tax rounding", { label: "Next step", text: "Wait for finance" }],
        ["Saved carts", { label: null, text: "Keep carts across sessions." }],
      ],
    ],
  ]);
  expect(motion.withoutActiveStage).toEqual([]);
  expect(motion.withoutLifecycle).toEqual(["Area", "Checklist"]);

  const library = inMotion(fixtureModel("Library"));
  expect(library.groups).toEqual([]);
  expect(library.withoutActiveStage).toEqual(["Memo"]);
  // A `status` enum with @view order but no stages is not a lifecycle.
  expect(library.withoutLifecycle).toEqual(["Glossary"]);
});

it("takes key text from the profile's key text fields", () => {
  const work = {
    ...TYPE_DOCS.Work,
    profile: { ...TYPE_DOCS.Work.profile, keyTextFields: [] },
  };

  const motion = inMotion(fixtureModel("Planning", { docs: { ...TYPE_DOCS, Work: work } }));

  expect(motion.groups[0]?.entries[0]?.note).toEqual({
    label: null,
    text: "Rebuild checkout around saved payment methods.",
  });
});

it("lists changes newest first by day, collapsing a burst in one minute", () => {
  const days = recentChanges(planning, 6);

  const moments = days.map((day) =>
    day.moments.map((moment) => (moment.burst ? moment.byMember : titles(moment.records))),
  );

  expect(moments).toEqual([
    [["Checkout redesign"], ["Tax rounding"]],
    [
      [
        { member: "Area", count: 2 },
        { member: "Release", count: 2 },
      ],
      ["Fall checklist"],
    ],
    [["Gift cards"]],
    // The limit counts lines: the burst is one, so Wishlist and older fall off.
    [["Login loop"]],
  ]);
  expect(days[1].moments[0]).toMatchObject({ burst: true, time: BURST + 30_000 });
  expect(days[1].moments[0].records).toHaveLength(BURST_SIZE);
  expect(new Set(days.map((day) => day.day)).size).toBe(days.length);
});

it("does not collapse fewer than four changes in a minute", () => {
  const records = [...planning.records.values()].filter((record) => record.path !== PATHS.loopB);
  const model = { ...planning, records: new Map(records.map((record) => [record.key, record])) };
  const burst = recentChanges(model, 20)[1].moments[0];

  expect(burst.burst).toBe(false);
  expect(titles(burst.records)).toEqual(["Loop A", "Winter release", "Fall release"]);
});

it("counts notes outside the group by resolved type, excluding the guide", () => {
  const outside = boundary(planning);

  expect(outside.notes.map((note) => [note.title, note.type, note.touches.length])).toEqual([
    ["Ada Lovelace", "Person", 2],
    ["Kickoff", null, 2],
    ["Release guide", null, 1],
    ["Scratch", null, 1],
  ]);
  expect(outside.notes.some((note) => note.path === RELEASE_GUIDE)).toBe(true);
  expect(outside.byType).toEqual([
    { type: "Person", count: 1 },
    { type: null, count: 3 },
  ]);
});

it("counts a record's typed links to outside notes even without neighbors", () => {
  // Embedded records read no neighbors, and a neighbor list can stop at its cap.
  const records = [...planning.records.values()].map((record) => ({ ...record, neighbors: [] }));
  const model = { ...planning, records: new Map(records.map((record) => [record.key, record])) };

  expect(boundary(model).notes.map((note) => [note.title, note.type, note.touches])).toEqual([
    ["Ada Lovelace", "Person", [PATHS.checkout]],
  ]);
});

it("does not count sections of group records as outside notes", () => {
  const section = {
    key: `${PATHS.loopA}#spec-set`,
    path: `${PATHS.loopA}#spec-set`,
    title: "Spec Set",
    type: "SpecSetSection",
  };

  const records = [...planning.records.values()].map((record) =>
    record.path === PATHS.carts ? { ...record, neighbors: [...record.neighbors, section] } : record,
  );

  const model = { ...planning, records: new Map(records.map((record) => [record.key, record])) };

  expect(boundary(model).notes.map((note) => note.title)).not.toContain("Spec Set");
});

it("counts an outside note's sections as the note, titled by the note", () => {
  const agenda = {
    key: `${PATHS.kickoff}#agenda`,
    path: `${PATHS.kickoff}#agenda`,
    title: "Agenda",
    type: "AgendaSection",
  };

  const minutes = {
    key: "notes/minutes.md#decisions",
    path: "notes/minutes.md#decisions",
    title: "Decisions",
    type: "DecisionSection",
  };

  const records = [...planning.records.values()].map((record) =>
    record.path === PATHS.checkout || record.path === PATHS.tax
      ? { ...record, neighbors: [...record.neighbors, agenda, minutes] }
      : record,
  );

  const model = { ...planning, records: new Map(records.map((record) => [record.key, record])) };
  const outside = boundary(model);

  const listed = outside.notes.map((note) => [note.path, note.title, note.touches.length]);

  // Kickoff already linked from Checkout and Saved carts; its section adds Tax rounding.
  expect(listed).toContainEqual([PATHS.kickoff, "Kickoff", 3]);
  expect(listed).toContainEqual(["notes/minutes.md", "minutes", 2]);
  expect(outside.notes.filter((note) => note.path.includes("#"))).toEqual([]);
  expect(outside.byType.reduce((sum, entry) => sum + entry.count, 0)).toBe(outside.notes.length);
});

it("does not count records of member types beyond the record cap as outside", () => {
  const unread = { key: "work/unread.md", path: "work/unread.md", title: "Unread", type: "Bug" };

  const records = [...planning.records.values()].map((record) =>
    record.path === PATHS.carts
      ? { ...record, neighbors: [unread], links: new Map([["blockedBy", [unread]]]) }
      : record,
  );

  const model = { ...planning, records: new Map(records.map((record) => [record.key, record])) };

  expect(boundary(model).notes.map((note) => note.title)).not.toContain("Unread");
});
