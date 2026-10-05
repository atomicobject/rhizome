import { fireEvent, screen, within } from "@testing-library/react";
import { expect, it } from "vitest";

import { OPEN_NOTE_MESSAGE, OPEN_VIEW_MESSAGE } from "../../src/lib/customViewMessages";
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
  [...block.querySelectorAll(".gv-record")].map((entry) => entry.textContent);

const signalTexts = (block: HTMLElement) =>
  [...block.querySelectorAll(".gv-sig-text")].map((entry) => entry.textContent);

it("briefs a declared-stage type: attention, motion, changes, spread, connections, links in, and guide", async () => {
  await openType("Story");

  const attention = await region("Needs attention");

  expect(signalTexts(attention)).toEqual([
    "Validation issues on 1 record",
    "Degraded health: 1 story",
  ]);

  // Newest first, each with its first key text field.
  const motion = await region("In motion");

  expect(recordTitles(motion)).toEqual(["Checkout redesign", "Saved carts"]);
  expect(motion.querySelector(".gv-mo-note")?.textContent).toBe("Next stepReview the payment step");

  expect(within(await region("Recent changes")).getByText("Checkout redesign")).toBeVisible();

  const spread = await region("Shape");

  expect(within(spread).getByRole("heading", { name: "Stage lifecycle" })).toBeVisible();
  expect(within(spread).getByRole("heading", { name: "Health ordered" })).toBeVisible();
  expect(within(spread).getByRole("heading", { name: "Kind category" })).toBeVisible();
  expect(within(spread).getByText("Empty")).toHaveTextContent("Empty 2");

  const connections = await region("Connections");

  // People, relation, then reverse fields: fill, then the most common targets.
  expect(itemTexts(connections)).toEqual([
    "Owner→ People1/4Ada Lovelace 1",
    "Area→ Areas3/4Payments 2Wishlists 1",
    "Blocked by→ Work items1/4Checkout redesign 1",
    "Meetings← Meetings2/4Kickoff sync 2Design review 1",
  ]);

  const linked = await region("Linked from outside");

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
  expect(signalTexts(await region("Context"))).toContain("No meetings link to 2 of 6 work items");

  const spread = await region("Shape");

  expect(within(spread).getByRole("heading", { name: "Implementing types" })).toBeVisible();
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
    within(await region("Shape")).getByRole("heading", { name: "Memo status lifecycle" }),
  ).toBeVisible();
  expect(screen.queryByRole("region", { name: "In motion" })).toBeNull();
  expect(screen.queryByText(/has no active stage/)).toBeNull();
});

it("leaves out In motion for a type without a lifecycle", async () => {
  await openType("Area");

  await region("Shape");
  expect(screen.queryByRole("region", { name: "In motion" })).toBeNull();
  expect(
    within(await region("Shape")).getByRole("heading", { name: "Category category" }),
  ).toBeVisible();
});

it("charts a dated type's primary date by month and collapses a burst of changes", async () => {
  await openType("Meeting");

  const spread = await region("Shape");

  expect(
    within(spread).getByRole("img", {
      name: "Date per month, Aug 2026 to Oct 2026: busiest Sep 2026 with 2",
    }),
  ).toBeVisible();
  expect(within(spread).getByText("1 undated")).toBeVisible();

  const changes = await region("Recent changes");

  expect(within(changes).getByText("4 records")).toBeVisible();
  expect(within(changes).getByText(/changed together: 4 meetings/)).toBeVisible();
  fireEvent.click(within(changes).getByRole("button", { name: /^Expand the 4 records/ }));
  expect(within(changes).getByText("Planning")).toBeVisible();

  const connections = await region("Connections");

  expect(itemTexts(connections)[0]).toBe("Attendees→ People4/5Ada Lovelace 3Grace Hopper 2");
});

it("opens the collection's Table from a signal and notes from its targets", async () => {
  const { posted } = await openType("Story", true);

  await region("Needs attention");

  const gap = [...(await region("Context")).querySelectorAll("li")].find((entry) =>
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
