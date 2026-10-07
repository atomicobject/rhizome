import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { expect, it } from "vitest";

import {
  OPEN_COLLECTION_MESSAGE,
  OPEN_ISSUES_MESSAGE,
  OPEN_NODE_MESSAGE,
} from "../../src/lib/customViewMessages";
import { jsonReply } from "../../src/test/fakeFetch";
import { NOW, PATHS, RECORDS_DATA, TYPE_DOCS } from "./__fixtures__/groups.ts";
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

it("shows only activity: Needs attention, In motion, and Recent changes", async () => {
  await renderGroupView(loadBriefing, { group: "People" });

  expect(
    screen.getAllByRole("region").map((block) => block.querySelector("h2 span")?.textContent),
  ).toEqual(["Needs attention", "In motion", "Recent changes"]);
  expect(within(region("Needs attention")).getByText("No validation issues")).toBeVisible();
  expect(
    within(region("Recent changes")).getByRole("button", { name: /Ada Lovelace/ }),
  ).toBeVisible();
});

it("paints every block's heading at once and fills each in when the records arrive", async () => {
  let release = () => {};

  const held = new Promise<void>((resolve) => {
    release = resolve;
  });

  await renderGroupView(loadBriefing, {
    settle: false,
    routes: groupRoutes({
      "POST /api/v1/graphql": async () => {
        await held;

        return jsonReply({ data: RECORDS_DATA });
      },
    }),
  });

  for (const name of ["Needs attention", "In motion", "Recent changes"])
    expect(within(region(name)).getByRole("status")).toHaveTextContent("Loading");
  expect(screen.getByText("Loading the group…")).toBeVisible();

  release();

  await waitFor(() =>
    expect(within(region("In motion")).getByText("Checkout redesign")).toBeVisible(),
  );
  expect(screen.queryByText("Loading the group…")).toBeNull();
});

it("fails one block's read without the others and retries it", async () => {
  let fail = true;

  // Only Needs attention reads link fields without neighbors.
  const isAttention = (body: string) =>
    body.includes("blockedBy") && !body.includes("neighborhood");

  const view = await renderGroupView(loadBriefing, {
    settle: false,
    routes: groupRoutes({
      "POST /api/v1/graphql": (request) =>
        fail && isAttention(request.body ?? "")
          ? jsonReply({ error: "unavailable" }, 503)
          : jsonReply({ data: RECORDS_DATA }),
    }),
  });

  const attention = await within(region("Needs attention")).findByRole("alert");
  expect(attention).toHaveTextContent("Could not load what needs attention");
  await waitFor(() =>
    expect(within(region("In motion")).getByText("Checkout redesign")).toBeVisible(),
  );
  expect(within(region("Recent changes")).queryByRole("alert")).toBeNull();
  expect(screen.queryByText("Loading the group…")).toBeNull();
  // The facts strip, Needs attention, In motion, and Recent changes each read on their own.
  expect(view.http.count("POST", "/api/v1/graphql")).toBeGreaterThanOrEqual(4);

  fail = false;
  fireEvent.click(within(attention).getByRole("button", { name: "Retry" }));

  await waitFor(() => expect(screen.queryByRole("alert")).toBeNull());
  expect(within(region("Needs attention")).getByText(/Checkout redesign/)).toBeVisible();
});

it("says when no record has a change time", async () => {
  const people = [{ path: PATHS.ada, title: "Ada Lovelace" }];

  await renderGroupView(loadBriefing, {
    group: "People",
    routes: groupRoutes({ "POST /api/v1/graphql": () => jsonReply({ data: { Person: people } }) }),
  });

  expect(await screen.findByText("No change times recorded.")).toBeVisible();
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

it("opens records, collections, and scoped issues through the workspace", async () => {
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

  expect(opened(posted)).toEqual([
    { type: OPEN_ISSUES_MESSAGE, scope: { kind: "interface", key: "Work" } },
    {
      type: OPEN_NODE_MESSAGE,
      ref: { notePath: PATHS.gifts, kind: "NOTE", typeName: "Story" },
    },
    { type: OPEN_COLLECTION_MESSAGE, name: "Release" },
    { type: OPEN_COLLECTION_MESSAGE, name: "Work" },
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

it("retries type documentation that failed to load", async () => {
  let fail = true;

  await renderGroupView(loadBriefing, {
    settle: false,
    routes: groupRoutes({
      "GET /api/v1/ontology/types/Area": () =>
        fail ? jsonReply({ error: "down" }, 500) : jsonReply({ type: TYPE_DOCS.Area, count: 0 }),
    }),
  });

  const alert = await within(region("In motion")).findByRole("alert");

  fail = false;
  fireEvent.click(within(alert).getByRole("button", { name: "Retry" }));

  await waitFor(() =>
    expect(within(region("In motion")).getByText("Checkout redesign")).toBeVisible(),
  );
});
