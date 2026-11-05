import { fireEvent, screen, within } from "@testing-library/react";
import { expect, it } from "vitest";

import {
  OPEN_NODE_MESSAGE,
  OPEN_NOTE_MESSAGE,
  OPEN_VIEW_MESSAGE,
} from "../../src/lib/customViewMessages";
import { isJsonObject } from "../../src/api/parse";
import { jsonReply } from "../../src/test/fakeFetch";
import { COLLECTION_DATA } from "./__fixtures__/collections";
import { PLANNING_GUIDE } from "./__fixtures__/groups.ts";
import { collectionRoutes, renderGroupView } from "./__fixtures__/harness.tsx";

const loadBriefing = () => import("./type-briefing.tsx");

const openType = (type: string, embedded = false) =>
  renderGroupView(loadBriefing, {
    context: { kind: "type", type },
    routes: collectionRoutes(),
    embedded,
  });

const region = (name: string) => screen.findByRole("region", { name });

/** The text of each top-level item of a block's first list. */
const itemTexts = (block: HTMLElement) =>
  [...(block.querySelector("ul")?.children ?? [])].map((entry) =>
    (entry.textContent ?? "").replace(/\s+/g, " ").trim(),
  );

/** The record titles a block lists, in order. */
const recordTitles = (block: HTMLElement) =>
  [...block.querySelectorAll(".gv-change-title")].map((entry) => entry.textContent);

const signalTexts = (block: HTMLElement) =>
  [...block.querySelectorAll(".gv-sig-text")].map((entry) => entry.textContent);

it("briefs a declared-stage type: attention, motion, changes, spread, connections, links in, and guide", async () => {
  await openType("Story");

  const attention = await region("Needs attention");

  expect(signalTexts(attention)).toEqual([
    "Validation issues on 1 record",
    "Degraded health: 1 story",
  ]);

  // Shared metadata moves into the heading for a homogeneous list.
  const motion = await region("In motion");
  expect(within(motion).getByText("Last changed")).toBeVisible();

  expect(recordTitles(motion)).toEqual(["Checkout redesign", "Saved carts"]);
  expect(motion.querySelector(".gv-change-meta")).toBeNull();
  expect(motion.querySelector(".gv-motion-metadata")).toHaveTextContent("Story · work/");
  expect(motion.querySelectorAll(".gv-change-compact")).toHaveLength(2);
  expect(motion.querySelector(".gv-mark")).toBeNull();
  expect(motion.querySelector(".rz-info")).toBeNull();
  expect(within(motion).getByRole("heading")).toHaveAttribute(
    "title",
    expect.stringContaining("active stage"),
  );

  expect(within(await region("Recent changes")).getByText("Checkout redesign")).toBeVisible();

  expect(document.querySelector(".gv-facts-list")).not.toHaveTextContent(/records/);
  const spread = await region("Composition");

  expect(within(spread).getByRole("heading", { name: "Stage" })).toBeVisible();
  expect(within(spread).getByRole("heading", { name: "Health ordered" })).toBeVisible();
  expect(within(spread).getByRole("heading", { name: "Kind category" })).toBeVisible();
  expect(within(spread).getByText("Empty")).toHaveTextContent("Empty 2");

  const connections = await region("Connections");

  // People, relation, then reverse fields: fill, then the most common targets.
  expect(itemTexts(connections)).toEqual([
    // Each target is a row: its name, then its count in a right-aligned column.
    "Owner→ People1 of 4 stories linkedLinked storiesAda Lovelace1",
    "Area→ Areas3 of 4 stories linkedLinked storiesPayments2Wishlists1",
    "Blocked by→ Work items1 of 4 stories linkedLinked storiesCheckout redesign1",
    "Meetings← Meetings2 of 4 stories linkedLinked storiesKickoff sync2Design review1",
  ]);

  const linked = await region("Linked from outside");

  expect(
    [...linked.querySelectorAll(".gv-outside-totals > span > span")].map(
      (label) => label.textContent,
    ),
  ).toEqual(["Person", "Area", "Untyped note"]);

  expect(itemTexts(linked)).toEqual([
    expect.stringMatching(/^Kickoff/),
    expect.stringMatching(/^Ada Lovelace/),
    expect.stringMatching(/^Payments/),
  ]);

  const guide = await region("Guide");

  expect(within(guide).getByRole("button", { name: "Planning guide" })).toBeVisible();
  // The Guide block is the one place the guide shows.
  expect(screen.getAllByRole("button", { name: "Planning guide" })).toHaveLength(1);
  expect(within(guide).getByRole("button", { name: "Triage · Table" })).toBeVisible();
});

it("briefs an interface across its implementors and flags reverse links most records have", async () => {
  await renderGroupView(loadBriefing, {
    context: { kind: "interface", interface: "Work" },
    routes: collectionRoutes(),
  });

  const attention = await region("Needs attention");

  expect(signalTexts(attention)).not.toContain("No meetings link to 2 of 6 work items");
  expect(signalTexts(await region("Review suggested"))).toContain(
    "No meetings link to 2 of 6 work items",
  );

  const spread = await region("Composition");

  expect(within(spread).getByRole("heading", { name: "Work item types" })).toBeVisible();
  expect(within(spread).getByText("Story")).toBeVisible();
  expect(within(spread).getByText("Bug")).toBeVisible();

  expect(recordTitles(await region("In motion"))).toEqual([
    "Checkout redesign",
    "Tax rounding",
    "Saved carts",
  ]);
});

it("leaves out In motion for a lifecycle without an active stage", async () => {
  await openType("Memo");

  // A contract-shaped type has nothing in motion by design; no hint to change the schema.
  expect(
    within(await region("Composition")).getByRole("heading", { name: "Memo status" }),
  ).toBeVisible();
  expect(screen.queryByRole("region", { name: "In motion" })).toBeNull();
  expect(screen.queryByText(/has no active stage/)).toBeNull();
});

it("leaves out In motion for a type without a lifecycle", async () => {
  await openType("Area");

  await region("Composition");
  expect(screen.queryByRole("region", { name: "In motion" })).toBeNull();
  expect(
    within(await region("Composition")).getByRole("heading", { name: "Category category" }),
  ).toBeVisible();
});

it("charts a dated type's primary date by month and collapses a burst of changes", async () => {
  await openType("Meeting");

  const spread = await region("Composition");

  expect(
    within(spread).getByRole("img", {
      name: "Date per month, Aug 2026 to Oct 2026: busiest Sep 2026 with 2",
    }),
  ).toBeVisible();
  expect(within(spread).getByText("1 undated")).toBeVisible();

  const changes = await region("Recent changes");

  const burst = within(changes).getByRole("button", { name: "4 records changed together" });
  expect(burst.closest("li")).toHaveTextContent("4 meetings");
  fireEvent.click(burst);
  expect(within(changes).getByText("Planning")).toBeVisible();

  const connections = await region("Connections");

  expect(itemTexts(connections)[0]).toBe(
    "Attendees→ People4 of 5 meetings linkedLinked meetingsAda Lovelace3Grace Hopper2",
  );
});

it("opens the collection's Table from a signal and notes from its targets", async () => {
  const { posted } = await openType("Story", true);

  await region("Needs attention");

  const gap = [...(await region("Review suggested")).querySelectorAll("li")].find((entry) =>
    entry.textContent?.includes("Next step empty"),
  );

  if (!gap) throw new Error("no next step gap");

  fireEvent.click(within(gap).getByRole("button", { name: "Open table" }));

  expect(posted.at(-1)).toEqual({
    type: OPEN_VIEW_MESSAGE,
    id: `view:${JSON.stringify(["generated.type.Story.table", "table"])}`,
    context: { kind: "type", type: "Story" },
  });

  fireEvent.click(within(await region("Guide")).getByRole("button", { name: "Planning guide" }));
  expect(posted.at(-1)).toMatchObject({ type: OPEN_NOTE_MESSAGE, path: PLANNING_GUIDE });
});

it("says which type it could not find", async () => {
  await openType("Gone");

  expect(await screen.findByText("No type or interface named Gone in navigation.")).toBeVisible();
});

it("opens the embedded identity from an unchanged record link", async () => {
  const ref = {
    kind: "EMBEDDED",
    notePath: "work/items.md",
    nodeId: "saved-carts",
    fragment: "^saved-carts",
    typeName: "Story",
  };

  const stories = COLLECTION_DATA.Story;

  const data = {
    ...COLLECTION_DATA,
    Story: Array.isArray(stories)
      ? stories.map((row) =>
          isJsonObject(row) && row.title === "Saved carts"
            ? { ...row, ref, path: ref.notePath }
            : row,
        )
      : [],
  };

  const { posted } = await renderGroupView(loadBriefing, {
    context: { kind: "type", type: "Story" },
    routes: collectionRoutes({ "POST /api/v1/graphql": () => jsonReply({ data }) }),
    embedded: true,
  });

  const motion = await region("In motion");
  const details = motion.querySelector("details");

  if (!details) throw new Error("No unchanged records");
  details.open = true;
  fireEvent.click(within(details).getByRole("button", { name: "Saved carts" }));
  expect(posted.at(-1)).toMatchObject({ type: OPEN_NODE_MESSAGE, ref });
});
