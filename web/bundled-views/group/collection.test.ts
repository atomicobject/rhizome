import { expect, it } from "vitest";

import { COLLECTION_DATA, COLLECTION_DOCS } from "./__fixtures__/collections.ts";
import { NOW, PATHS, PLANNING_GUIDE } from "./__fixtures__/groups.ts";
import { collectionModel } from "./__fixtures__/harness.tsx";
import { boundary, recentChanges, reverseGaps } from "./activity.ts";
import { isJsonObject, type JsonValue } from "./api.ts";
import { dateMonths, distributions, fieldConnections } from "./collection.ts";

const only = (name: string) => {
  const member = collectionModel(name).members[0];

  if (!member) throw new Error(`no collection ${name}`);

  return member;
};

/** A distribution as `field (role): value count, …, empty n/total`. */
const spread = (name: string) =>
  distributions(only(name)).map(
    (entry) =>
      `${entry.field} (${entry.role}): ${entry.values
        .map(({ value, count }) => `${value.label ?? value.name} ${count}`)
        .join(", ")}; empty ${entry.empty}/${entry.total}`,
  );

it("models a collection as a group of its own records, without display children", () => {
  const release = collectionModel("Release");

  expect(release.members.map((member) => member.name)).toEqual(["Release"]);
  expect(release.members[0]?.records.map((record) => record.title)).toEqual([
    "Fall release",
    "Winter release",
  ]);
  expect(collectionModel("Work").members[0]?.concreteTypes).toEqual(["Story", "Bug"]);
  expect(collectionModel("Story").tableChoice).toBe(
    `view:${JSON.stringify(["generated.type.Story.table", "table"])}`,
  );
});

it("spreads a declared-stage lifecycle, an ordered field, and a category in profile order", () => {
  expect(spread("Story")).toEqual([
    "stage (lifecycle): Backlog 1, Doing 2, Done 1; empty 0/4",
    "health (ordered): OK 1, Degraded 1; empty 2/4",
    "kind (category): feature 3, chore 1; empty 0/4",
  ]);
});

it("puts values outside the enum in an Other bucket", () => {
  // A hand-edited kind the enum does not list still belongs in the bar.
  const rows = (Array.isArray(COLLECTION_DATA.Story) ? COLLECTION_DATA.Story : []).map((row) =>
    isJsonObject(row) && row.path === PATHS.carts ? { ...row, kind: "spike" } : row,
  );

  const story = collectionModel("Story", { data: { ...COLLECTION_DATA, Story: rows } }).members[0];

  if (!story) throw new Error("no Story collection");
  const kind = distributions(story).find((entry) => entry.field === "kind");

  expect(kind).toMatchObject({ other: 1, empty: 0, total: 4 });
  expect(kind?.values.map(({ value, count }) => `${value.name} ${count}`)).toEqual([
    "feature 2",
    "chore 1",
  ]);
});

it("counts a burst by implementing type in an interface's collection", () => {
  // Every work item changes in one minute: one Work member says nothing, its types do.
  const at = new Date(NOW - 3 * 60 * 60 * 1000).toISOString();

  const sameMinute = (rows: JsonValue | undefined) =>
    (Array.isArray(rows) ? rows : []).map((row) =>
      isJsonObject(row) ? { ...row, updatedAt: at } : row,
    );

  const data = {
    ...COLLECTION_DATA,
    Story: sameMinute(COLLECTION_DATA.Story),
    Bug: sameMinute(COLLECTION_DATA.Bug),
  };

  const [burst] = recentChanges(collectionModel("Work", { data }))[0]?.moments ?? [];

  expect(burst?.burst).toBe(true);
  expect(burst?.byType).toEqual([
    { type: "Story", count: 4 },
    { type: "Bug", count: 2 },
  ]);
});

it("spreads a lifecycle with inferred stages in its @view order", () => {
  expect(spread("Release")).toEqual([
    "releaseStatus (lifecycle): Planned 1, At risk 1; empty 0/2",
    "health (ordered): OK 1, Degraded 1; empty 0/2",
  ]);
});

it("spreads only categories for a type without a lifecycle", () => {
  expect(spread("Area")).toEqual(["category (category): product 4, platform 1; empty 1/6"]);
  expect(spread("Meeting")).toEqual([]);
});

it("joins an interface's lifecycle across its implementors", () => {
  expect(spread("Work")).toEqual([
    "stage (lifecycle): Backlog 1, Doing 2, Blocked 1, Done 2; empty 0/6",
  ]);
});

it("charts the primary date per month from the first dated month, counting undated records", () => {
  const months = dateMonths(only("Meeting"), NOW);

  expect(months).toEqual({
    field: "date",
    months: [
      { month: "2026-08", count: 1 },
      { month: "2026-09", count: 2 },
      { month: "2026-10", count: 1 },
    ],
    first: "2026-08-04",
    last: "2026-10-01",
    earlier: 0,
    undated: 1,
  });

  // A shorter span drops older months and counts what it left out.
  expect(dateMonths(only("Meeting"), NOW, 2)).toMatchObject({
    months: [
      { month: "2026-09", count: 2 },
      { month: "2026-10", count: 1 },
    ],
    earlier: 1,
  });
  expect(dateMonths(only("Release"), NOW)).toMatchObject({
    months: [{ month: "2026-10", count: 1 }],
    undated: 1,
  });
  expect(dateMonths(only("Area"), NOW)).toBeNull();
});

it("reaches past this month when records are dated later", () => {
  const later = new Date(2026, 7, 20, 12).getTime();

  expect(dateMonths(only("Meeting"), later)?.months.map((entry) => entry.month)).toEqual([
    "2026-08",
    "2026-09",
    "2026-10",
  ]);
});

it("counts how many records fill each people, relation, and reverse field, and its top targets", () => {
  const summary = (name: string) =>
    fieldConnections(only(name)).map((entry) => ({
      field: entry.field,
      direction: entry.direction,
      people: entry.people,
      targetType: entry.targetType,
      filled: `${entry.filled}/${entry.total}`,
      distinct: entry.distinct,
      top: entry.top.map(({ note, count }) => `${note.title} ${count}`),
    }));

  expect(summary("Meeting")).toEqual([
    {
      field: "attendees",
      direction: "forward",
      people: true,
      targetType: "Person",
      filled: "4/5",
      distinct: 2,
      top: ["Ada Lovelace 3", "Grace Hopper 2"],
    },
    {
      field: "work",
      direction: "forward",
      people: false,
      targetType: "Work",
      filled: "3/5",
      distinct: 4,
      top: ["Checkout redesign 2", "Login loop 1", "Saved carts 1", "Tax rounding 1"],
    },
  ]);

  // An interface reads its relations and its reverse fields across implementors;
  // a reverse field's targets are the records linking in, by how many they reach.
  expect(summary("Work")).toEqual([
    {
      field: "area",
      direction: "forward",
      people: false,
      targetType: "Area",
      filled: "5/6",
      distinct: 3,
      top: ["Payments 3", "Platform 1", "Wishlists 1"],
    },
    {
      field: "meetings",
      direction: "reverse",
      people: false,
      targetType: "Meeting",
      filled: "4/6",
      distinct: 3,
      top: ["Design review 2", "Kickoff sync 2", "Retro 1"],
    },
  ]);
});

it("counts a reverse field as filled from its count when the sample holds fewer targets", () => {
  // Gift cards' meetings were not sampled, but its count says three link in.
  const counted = (rows: JsonValue | undefined) =>
    (Array.isArray(rows) ? rows : []).map((row) =>
      isJsonObject(row) && row.path === PATHS.gifts ? { ...row, meetings__generatedCount: 3 } : row,
    );

  const data = {
    ...COLLECTION_DATA,
    Story: counted(COLLECTION_DATA.Story),
    Bug: counted(COLLECTION_DATA.Bug),
  };

  const work = collectionModel("Work", { data }).members[0];

  if (!work) throw new Error("no Work collection");

  expect(fieldConnections(work).find((entry) => entry.field === "meetings")).toMatchObject({
    filled: 5,
    total: 6,
    sampled: true,
  });
  expect(reverseGaps(work)).toEqual([
    expect.objectContaining({
      field: "meetings",
      records: [expect.objectContaining({ title: "Wishlist" })],
    }),
  ]);
  expect(fieldConnections(only("Work")).find((entry) => entry.field === "meetings")?.sampled).toBe(
    false,
  );
});

it("reads a reverse field across implementors that declare it with different sources", () => {
  // Bug's meetings come from another type, so it is its own declaration of the name.
  const docs = {
    ...COLLECTION_DOCS,
    Bug: {
      ...COLLECTION_DOCS.Bug,
      fields: COLLECTION_DOCS.Bug.fields.map((field) =>
        field.name === "meetings" ? { ...field, typeName: "Event" } : field,
      ),
    },
  };

  const work = collectionModel("Work", { docs }).members[0];

  if (!work) throw new Error("no Work collection");

  expect(fieldConnections(work).find((entry) => entry.field === "meetings")).toMatchObject({
    filled: 4,
    total: 6,
  });
  expect(reverseGaps(work)).toEqual([expect.objectContaining({ field: "meetings", total: 6 })]);
});

it("labels an interface target from navigation, since type summaries list only types", () => {
  const blockedBy = fieldConnections(only("Story")).find((entry) => entry.field === "blockedBy");

  expect(blockedBy?.targetType).toBe("Work");
  expect(collectionModel("Story").typeLabels.get("Work")).toEqual({
    label: "Work item",
    pluralLabel: "Work items",
    group: "Planning",
  });
});

it("flags a reverse field more than half the records fill, but not one only half fill", () => {
  expect(reverseGaps(only("Work"))).toEqual([
    expect.objectContaining({
      kind: "reverse",
      member: "Work",
      field: "meetings",
      targetType: "Meeting",
      total: 6,
      records: [
        expect.objectContaining({ title: "Gift cards" }),
        expect.objectContaining({ title: "Wishlist" }),
      ],
    }),
  ]);
  // Two of four stories have meetings: half is not more than half.
  expect(reverseGaps(only("Story"))).toEqual([]);
  // A type without reverse fields has nothing to flag.
  expect(reverseGaps(only("Area"))).toEqual([]);
});

it("lists the notes of other types that link in, without the records' own link targets", () => {
  const linked = boundary(collectionModel("Story"), { links: false });

  expect(linked.notes.map((note) => `${note.title} ${note.touches.length}`)).toEqual([
    "Kickoff 2",
    "Ada Lovelace 1",
    "Payments 1",
  ]);
  // Wishlists is only an `area` target, which Connections already counts.
  expect(linked.notes.some((note) => note.path === PATHS.wishlists)).toBe(false);
  expect(linked.notes.some((note) => note.path === PLANNING_GUIDE)).toBe(false);
});
