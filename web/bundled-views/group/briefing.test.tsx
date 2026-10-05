import { fireEvent, screen, within } from "@testing-library/react";
import { expect, it } from "vitest";

import {
  OPEN_COLLECTION_MESSAGE,
  OPEN_ISSUES_MESSAGE,
  OPEN_NODE_MESSAGE,
  OPEN_NOTE_MESSAGE,
  OPEN_VIEW_MESSAGE,
} from "../../src/lib/customViewMessages";
import { jsonReply } from "../../src/test/fakeFetch";
import { NOW, PATHS, PLANNING_GUIDE, RECORDS_DATA, TYPE_DOCS } from "./__fixtures__/groups.ts";
import { groupRoutes, renderGroupView } from "./__fixtures__/harness.tsx";
import { isJsonObject } from "./api.ts";
import { SIGNAL_RECORDS } from "./briefing-attention.tsx";

const loadBriefing = () => import("./briefing.tsx");

const region = (name: string) => screen.getByRole("region", { name });

/** The top-level items of a block's first list. */
const items = (block: HTMLElement) =>
  [...(block.querySelector("ul")?.children ?? [])].filter(
    (child): child is HTMLElement => child instanceof HTMLElement,
  );

const item = (block: HTMLElement, text: string) => {
  const found = items(block).find((entry) => entry.textContent?.includes(text));

  if (!found) throw new Error(`no item containing ${text}`);

  return within(found);
};

const opened = (posted: readonly { type: string }[]) =>
  posted.filter((message) => message.type.startsWith("rhizome:open-"));

it("lists what needs attention with counts, records, and policy reasons", async () => {
  await renderGroupView(loadBriefing);
  const attention = await screen.findByRole("region", { name: "Needs attention" });

  expect(items(attention).map((entry) => entry.querySelector(".gv-sig-text")?.textContent)).toEqual(
    [
      "Validation issues on 1 record",
      // Danger and risk first, aggregated across members by field and value.
      "Degraded health: 1 work item, 1 release",
      "At risk release status: 1 release",
      "Blocked stage: 1 work item",
      // Required (notes), conditionally required (resolution), and lifecycle fields are skipped.
    ],
  );
  expect(within(attention).getByText("4")).toBeVisible();

  const nextStep = item(region("Context"), "Next step empty");
  expect(nextStep.getByText("3/6")).toBeVisible();
  expect(nextStep.getByText("Name the next concrete step so the work can move.")).toBeVisible();

  expect(region("Context")).toHaveTextContent("Area empty on 1 of 6 work items");
  expect(region("Context").querySelector('[data-severity="warning"]')).toBeNull();
  expect(attention).not.toHaveTextContent("Area empty");
  // Update age is context, not evidence of an obligation.
  expect(item(region("Context"), "unchanged for").getByText("40d")).toBeVisible();

  // No clean line while a record has issues.
  expect(within(attention).queryByText("No validation issues")).toBeNull();
});

it("gives every signal an action: its member's collection or issues, or each member's when several share it", async () => {
  await renderGroupView(loadBriefing);
  const attention = await screen.findByRole("region", { name: "Needs attention" });

  for (const entry of items(attention).slice(0, -1)) {
    const actions = [...entry.querySelectorAll("button")].filter(
      (button) =>
        button.classList.contains("gv-sig-act") ||
        (/^[A-Z].*s$/.test(button.textContent ?? "") &&
          button.classList.contains("gv-link") &&
          !button.classList.contains("gv-more")),
    );

    expect(actions.length, entry.textContent ?? "").toBeGreaterThan(0);
  }

  expect(
    item(attention, "Degraded").getAllByRole("button", { name: /^(Work items|Releases)$/ }),
  ).toHaveLength(2);
});

it("names a bounded number of records per signal and reveals the rest in place", async () => {
  // Areas without parents: Commerce, Loop A, and Loop B leave the tree and
  // link with nothing else.
  const areas = (Array.isArray(RECORDS_DATA.Area) ? RECORDS_DATA.Area : []).map((area) =>
    isJsonObject(area) ? { ...area, parent: null } : area,
  );

  await renderGroupView(loadBriefing, {
    routes: groupRoutes({
      "POST /api/v1/graphql": () => jsonReply({ data: { ...RECORDS_DATA, Area: areas } }),
    }),
  });
  await screen.findByRole("region", { name: "Context" });
  const unlinked = item(region("Context"), "Not linked");

  const named = () =>
    unlinked
      .getAllByRole("button")
      .filter((button) => button.className.includes("gv-inline"))
      .map((button) => button.textContent);

  expect(named()).toEqual(["Gift cards", "Commerce", "Loop A"].slice(0, SIGNAL_RECORDS));
  fireEvent.click(unlinked.getByRole("button", { name: "+2 more" }));
  expect(named()).toEqual(["Gift cards", "Commerce", "Loop A", "Loop B", "Winter release"]);
  fireEvent.click(unlinked.getByRole("button", { name: "Show fewer" }));
  expect(named()).toHaveLength(SIGNAL_RECORDS);
});

it("says there are no validation issues, and skips unlinked records in a group without links", async () => {
  await renderGroupView(loadBriefing, { group: "Library" });
  const attention = await screen.findByRole("region", { name: "Needs attention" });

  expect(items(attention).map((entry) => entry.textContent)).toEqual([
    expect.stringContaining("No validation issues"),
  ]);
  expect(region("Context")).toHaveTextContent("Term empty on 1 of 3 glossaries");
  expect(screen.queryByRole("region", { name: "Connections" })).toBeNull();
});

it("lists active-stage records newest first with their key text, whatever their tone", async () => {
  await renderGroupView(loadBriefing);
  const motion = await screen.findByRole("region", { name: "In motion" });

  // Blocked is warning-toned but declares the active stage.
  expect(within(motion).getByRole("heading", { level: 3 })).toHaveTextContent(
    "Work items · Doing, Blocked 3",
  );
  expect(items(motion).map((entry) => entry.textContent)).toEqual([
    expect.stringMatching(/Checkout redesign.*Next stepReview the payment step$/),
    expect.stringMatching(/Tax rounding.*Next stepWait for finance$/),
    expect.stringMatching(/Saved carts.*Keep carts across sessions\.$/),
  ]);
  expect(within(motion).getByText("No lifecycle: Areas, Checklists.")).toBeVisible();
});

it("names lifecycles without an active stage", async () => {
  await renderGroupView(loadBriefing, { group: "Library" });
  const motion = await screen.findByRole("region", { name: "In motion" });

  expect(within(motion).getByText("Nothing in an active stage.")).toBeVisible();
  expect(motion).toHaveTextContent(
    "memoStatusEnum (Memos) has no active stage; declare an active stage with @view(stage: active) on a value to list its records here.",
  );
  // Glossary's status enum has @view order but no stages, so it is no lifecycle.
  expect(motion).toHaveTextContent("No lifecycle: Glossaries.");
});

it("lists recent changes by day and time, collapsing a burst until expanded", async () => {
  await renderGroupView(loadBriefing);
  const changes = await screen.findByRole("region", { name: "Recent changes" });
  const days = within(changes).getAllByRole("heading", { level: 3 });

  expect(days[0]).toHaveTextContent("Today");
  expect(days[1]).toHaveTextContent(/^Yesterday, /);

  const today = within(days[0].parentElement ?? changes);
  expect(today.getAllByRole("listitem").map((entry) => entry.textContent)).toEqual([
    expect.stringMatching(/^11:00.*Checkout redesign$/),
    expect.stringMatching(/^10:00.*Tax rounding$/),
  ]);

  const burst = within(changes).getByText("changed together:", { exact: false });
  expect(burst).toHaveTextContent("4 records changed together: 2 areas, 2 releases.");
  expect(within(changes).queryByRole("button", { name: /Loop A/ })).toBeNull();

  fireEvent.click(
    within(changes).getByRole("button", { name: /^Expand the 4 records changed at 10:00/ }),
  );
  expect(within(changes).getByRole("button", { name: /Loop A/ })).toBeVisible();
  expect(within(changes).getByRole("button", { name: /^Collapse/ })).toHaveAttribute(
    "aria-expanded",
    "true",
  );
});

it("counts notes outside the group by type and lists the most connected first", async () => {
  await renderGroupView(loadBriefing);
  const outside = await screen.findByRole("region", { name: "Outside the group" });

  expect(within(outside).getByText("4 notes")).toBeVisible();
  expect(outside.querySelector(".gv-outsum")).toHaveTextContent("1 person3 untyped notes");
  expect(items(outside).map((entry) => entry.textContent)).toEqual([
    "Ada Lovelace person2 linked records",
    "Kickoff untyped note2 linked records",
    "Release guide untyped note1 linked record",
    "Scratch untyped note1 linked record",
  ]);
});

it("summarizes each member with its count, description, lifecycle, and implementing types", async () => {
  await renderGroupView(loadBriefing);
  const group = await screen.findByRole("region", { name: "In this group" });

  const work = item(group, "Work items");
  expect(work.getByText("interface")).toBeVisible();
  expect(work.getByText("Planned work.")).toBeVisible();
  expect(work.getByText("6")).toBeVisible();
  expect(work.getByText("2 issues")).toBeVisible();
  // The lifecycle distribution is labeled, not only drawn.
  expect(group.querySelector(".gv-counts")).toHaveTextContent("Backlog1Doing2Blocked1Done2");
  expect(group.querySelector(".gv-kinds")).toHaveTextContent("Story 4Bug 2");

  expect(items(group).map((entry) => entry.querySelector(".gv-member")?.textContent)).toEqual([
    "Work items",
    "Areas",
    "Releases",
    "Checklists",
  ]);
});

it("shows record links between members as a table, marking allowed but unused links", async () => {
  await renderGroupView(loadBriefing);
  const connections = await screen.findByRole("region", { name: "Connections" });
  const table = within(connections).getByRole("table");

  expect(
    within(table)
      .getAllByRole("columnheader")
      .map((header) => header.textContent),
  ).toEqual(["Work items", "Areas", "Releases", "Checklists"]);

  const row = (name: string) =>
    within(table).getByRole("rowheader", { name }).closest("tr")?.querySelectorAll("td") ?? [];

  expect([...row("Work items")].map((cell) => cell.textContent)).toEqual([
    "Same type",
    "5",
    "No link field",
    "No link field",
  ]);
  expect([...row("Releases")].map((cell) => cell.textContent)).toEqual([
    "2",
    "0 (allowed, unused)",
    "Same type",
    "1",
  ]);
});

it("renders a one-root group without connections", async () => {
  await renderGroupView(loadBriefing, { group: "People" });
  const group = await screen.findByRole("region", { name: "In this group" });

  expect(items(group)).toHaveLength(1);
  expect(within(region("Needs attention")).getByText("No validation issues")).toBeVisible();

  expect(screen.queryByRole("region", { name: "Connections" })).toBeNull();
  expect(
    within(region("Recent changes")).getByRole("button", { name: /Ada Lovelace/ }),
  ).toBeVisible();
});

it("says the outside count is a lower bound when a record has more neighbors than were read", async () => {
  const people = [
    {
      path: PATHS.ada,
      title: "Ada Lovelace",
      neighborhood: {
        truncated: true,
        nodes: [{ path: "notes/kickoff.md", title: "Kickoff", resolvedType: null }],
      },
    },
  ];

  await renderGroupView(loadBriefing, {
    group: "People",
    routes: groupRoutes({ "POST /api/v1/graphql": () => jsonReply({ data: { Person: people } }) }),
  });

  const outside = await screen.findByRole("region", { name: "Outside the group" });

  expect(within(outside).getByText("at least 1 note")).toBeVisible();
  expect(within(outside).getByText(/have more links than were read/)).toBeVisible();
});

it("says when nothing outside the group links in and no record has a change time", async () => {
  const people = [{ path: PATHS.ada, title: "Ada Lovelace" }];

  await renderGroupView(loadBriefing, {
    group: "People",
    routes: groupRoutes({ "POST /api/v1/graphql": () => jsonReply({ data: { Person: people } }) }),
  });

  expect(await screen.findByText("No change times recorded.")).toBeVisible();
  expect(within(region("Outside the group")).getByText("0 notes")).toBeVisible();
  expect(
    within(region("Outside the group")).getByText(
      "No notes outside the group link to or from its records.",
    ),
  ).toBeVisible();
});

it("keeps records that arrive with field errors, such as a missing required field", async () => {
  const people = [
    { path: PATHS.ada, title: "Ada Lovelace", updatedAt: new Date(NOW - 60_000).toISOString() },
  ];

  await renderGroupView(loadBriefing, {
    group: "People",
    routes: groupRoutes({
      "POST /api/v1/graphql": () =>
        jsonReply({
          data: { Person: people },
          errors: [
            { message: "required field displayName is missing", path: ["Person", "displayName"] },
          ],
        }),
    }),
  });

  const changes = await screen.findByRole("region", { name: "Recent changes" });
  expect(within(changes).getByRole("button", { name: /Ada Lovelace/ })).toBeVisible();
  expect(screen.queryByRole("alert")).toBeNull();
});

it("opens records, collections, scoped issues, notes, and the group's views through the workspace", async () => {
  const { posted } = await renderGroupView(loadBriefing, { embedded: true, now: NOW });
  const attention = await screen.findByRole("region", { name: "Needs attention" });

  fireEvent.click(
    item(attention, "Validation issues").getByRole("button", { name: "Open issues" }),
  );
  fireEvent.click(item(attention, "Validation issues").getByRole("button", { name: "Gift cards" }));
  fireEvent.click(item(attention, "Degraded").getByRole("button", { name: "Releases" }));
  fireEvent.click(
    item(region("Context"), "Next step empty").getByRole("button", { name: "Open work items" }),
  );
  fireEvent.click(within(region("Outside the group")).getByRole("button", { name: "Kickoff" }));
  fireEvent.click(within(region("In this group")).getByRole("button", { name: "Areas" }));
  fireEvent.click(within(region("Views")).getByRole("button", { name: "Planning board" }));
  fireEvent.click(within(region("Guide")).getByRole("button", { name: "Planning guide" }));

  expect(opened(posted)).toEqual([
    { type: OPEN_ISSUES_MESSAGE, scope: { kind: "interface", key: "Work" } },
    {
      type: OPEN_NODE_MESSAGE,
      ref: { notePath: PATHS.gifts, kind: "NOTE", typeName: "Story" },
    },
    { type: OPEN_COLLECTION_MESSAGE, name: "Release" },
    { type: OPEN_COLLECTION_MESSAGE, name: "Work" },
    { type: OPEN_NOTE_MESSAGE, path: PATHS.kickoff, beside: false },
    { type: OPEN_COLLECTION_MESSAGE, name: "Area" },
    {
      type: OPEN_VIEW_MESSAGE,
      id: "planning.board",
      context: { kind: "group", group: "Planning" },
    },
    { type: OPEN_NOTE_MESSAGE, path: PLANNING_GUIDE, beside: false },
  ]);
});

it("refreshes when records change", async () => {
  let data = RECORDS_DATA;

  const view = await renderGroupView(loadBriefing, {
    routes: groupRoutes({ "POST /api/v1/graphql": () => jsonReply({ data }) }),
  });

  await screen.findByRole("region", { name: "Needs attention" });
  data = { ...RECORDS_DATA, Story: [], Bug: [] };
  await view.emit("node.changed");

  expect(await within(region("Needs attention")).findByText("No validation issues")).toBeVisible();
});

it("keeps an optional priority with a user-only policy out of Needs attention", async () => {
  const reason = "Drew sets this; leave unset unless he states it.";

  const routes = Object.fromEntries(
    (["Work", "Story", "Bug"] as const).map((name) => {
      const doc = TYPE_DOCS[name];

      const priority = {
        name: "priority",
        kind: "scalar",
        typeName: "String",
        list: false,
        display: { importance: "KEY" },
        policy: { reason },
      };

      return [
        `GET /api/v1/ontology/types/${name}`,
        () =>
          jsonReply({
            type: {
              ...doc,
              fields: [...doc.fields, priority],
              profile: {
                ...doc.profile,
                gapFields: [...(doc.profile?.gapFields ?? []), "priority"],
              },
            },
            count: 0,
          }),
      ];
    }),
  );

  await renderGroupView(loadBriefing, { routes: groupRoutes(routes) });
  const attention = await screen.findByRole("region", { name: "Needs attention" });

  expect(attention).not.toHaveTextContent("Priority empty");
  expect(region("Context")).toHaveTextContent("Priority empty");
  expect(region("Context")).toHaveTextContent(reason);
});
