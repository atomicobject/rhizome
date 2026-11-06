import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { expect, it } from "vitest";

import {
  OPEN_COLLECTION_MESSAGE,
  OPEN_ISSUES_MESSAGE,
  OPEN_NODE_MESSAGE,
} from "../../src/lib/customViewMessages";
import { jsonReply } from "../../src/test/fakeFetch";
import { NOW, PATHS, RECORDS_DATA, TYPE_DOCS, TYPE_SUMMARIES } from "./__fixtures__/groups.ts";
import { VALIDATION_ENVELOPE } from "./__fixtures__/aggregate.ts";
import { groupRoutes, renderGroupView } from "./__fixtures__/harness.tsx";
import { isJsonObject, type JsonValue } from "./api.ts";
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
  const guide = document.querySelector(".gv-facts-note button");

  expect(guide).toHaveAttribute("title", expect.stringContaining("Shared by"));
  expect(guide).toHaveAttribute("aria-description", guide?.getAttribute("title"));
  expect(document.querySelector(".gv-facts-note")).not.toHaveTextContent("shared by");

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

  const nextStep = item(region("Review suggested"), "Next step empty");
  expect(nextStep.getByText("3/6")).toBeVisible();
  expect(nextStep.getByText("Name the next concrete step so the work can move.")).toBeVisible();

  expect(region("Review suggested")).toHaveTextContent("Area empty on 1 of 6 work items");
  expect(region("Review suggested").querySelector('[data-severity="warning"]')).toBeNull();
  expect(attention).not.toHaveTextContent("Area empty");
  // Update age is context, not evidence of an obligation.
  const stale = item(region("Review suggested"), "unchanged for");
  const toggle = stale.getByRole("button", { name: /^Show \d+ / });
  expect(toggle).toHaveAttribute("aria-expanded", "false");
  expect(stale.queryByText("40d")).toBeNull();
  fireEvent.click(toggle);
  expect(toggle).toHaveAttribute("aria-expanded", "true");
  expect(stale.getByText("40d")).toBeVisible();

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
  const areas: JsonValue[] = (Array.isArray(RECORDS_DATA.Area) ? RECORDS_DATA.Area : []).map(
    (area) => (isJsonObject(area) ? { ...area, parent: null } : area),
  );

  areas.push({
    path: "areas/extra.md",
    title: "Extra area",
    parent: null,
    updatedAt: NOW / 1000,
    issueCount: 0,
  });
  await renderGroupView(loadBriefing, {
    routes: groupRoutes({
      "POST /api/v1/graphql": () => jsonReply({ data: { ...RECORDS_DATA, Area: areas } }),
    }),
  });
  // Unlinked records are a coverage fact under their own heading, not a review prompt.
  await screen.findByRole("region", { name: "Link coverage" });
  const unlinked = item(region("Link coverage"), "Not linked");

  const named = () =>
    unlinked
      .getAllByRole("button")
      .filter((button) => button.className.includes("gv-inline"))
      .map((button) => button.textContent);

  expect(named()).toEqual(["Gift cards", "Commerce", "Loop A", "Loop B", "Winter release"]);
  fireEvent.click(unlinked.getByRole("button", { name: "Show 1 more" }));
  expect(named()).toEqual([
    "Gift cards",
    "Commerce",
    "Loop A",
    "Loop B",
    "Extra area",
    "Winter release",
  ]);
  fireEvent.click(unlinked.getByRole("button", { name: "Show fewer" }));
  expect(named()).toHaveLength(SIGNAL_RECORDS + 2);
});

it("says there are no validation issues, and skips unlinked records in a group without links", async () => {
  await renderGroupView(loadBriefing, { group: "Library" });
  await screen.findByRole("region", { name: "Review suggested" });

  // The status strip says "No validation issues" once; the body adds no Validation row.
  expect(screen.getByText("No validation issues")).toBeVisible();
  expect(await screen.findByText("Checked 2h ago")).toBeVisible();
  expect(document.querySelector(".gv-facts-list")).toHaveTextContent(/Last changed .* ago/);
  expect(screen.queryByRole("region", { name: "Validation" })).toBeNull();
  expect(screen.queryByRole("region", { name: "Needs attention" })).toBeNull();
  expect(region("Review suggested")).toHaveTextContent("Term empty on 1 of 3 glossaries");
  expect(screen.queryByRole("region", { name: "Connections" })).toBeNull();
});

it("lists active-stage records newest first in flat rows with type metadata", async () => {
  await renderGroupView(loadBriefing);
  const motion = await screen.findByRole("region", { name: "In motion" });
  expect(within(motion).getByText("Last changed")).toBeVisible();

  expect(within(motion).queryByRole("heading", { level: 3 })).toBeNull();
  expect(items(motion).map((entry) => within(entry).getByRole("button").textContent)).toEqual([
    "Checkout redesign",
    "Tax rounding",
    "Saved carts",
  ]);
  expect(
    [...motion.parentElement!.querySelectorAll(":scope > section")].map(
      (section) =>
        section.getAttribute("aria-labelledby") && section.querySelector("h2 > span")?.textContent,
    ),
  ).toEqual(["Needs attention", "Review suggested", "In motion", "Link coverage"]);
  expect(motion.querySelector(".gv-motion-metadata")).not.toBeNull();
  expect(motion.querySelectorAll(".gv-change-meta")).toHaveLength(1);
  expect(motion.querySelectorAll(".gv-mark").length).toBeGreaterThan(0);
  expect(motion.querySelector(".gv-hint-lead")).toHaveTextContent(
    "Areas, Checklists have no lifecycle.",
  );
});

it("names lifecycles without an active stage", async () => {
  await renderGroupView(loadBriefing, { group: "Library" });
  const motion = await screen.findByRole("region", { name: "In motion" });

  expect(within(motion).getByText("Nothing in an active stage.")).toBeVisible();
  // Setup help stays attached to the heading, behind the info disclosure.
  expect(motion.querySelector("h2 .gv-hint")).not.toBeNull();
  expect(motion.querySelector(".gv-stage-note")).not.toHaveTextContent("shows active");
  const config = motion.querySelector("details")!;
  const summary = within(motion).getByLabelText("Why some types are not listed");
  expect(summary).toBeVisible();
  expect(config).not.toHaveAttribute("open");
  fireEvent.click(summary);
  expect(config).toHaveAttribute("open");
  expect(config.querySelector(".gv-hint-lead")).toHaveTextContent(
    "Memos have no active stage configured. Glossaries have no lifecycle.",
  );
  expect(config).toHaveTextContent(
    "mark a value of memoStatusEnum with @view(stage: active) in the schema.",
  );
});

it("lists recent changes newest first with type, folder, and age, collapsing a burst", async () => {
  await renderGroupView(loadBriefing);
  const changes = await screen.findByRole("region", { name: "Recent changes" });
  const lines = within(changes).getAllByRole("listitem");

  expect(lines[0]).toHaveTextContent(/^Checkout redesign/);
  expect(lines[1]).toHaveTextContent(/^Tax rounding/);

  const burst = within(changes).getByRole("button", { name: "4 records changed together" });
  expect(burst.closest("li")).toHaveTextContent("2 areas, 2 releases");
  expect(within(changes).queryByRole("button", { name: /Loop A/ })).toBeNull();

  fireEvent.click(burst);
  expect(within(changes).getByRole("button", { name: /Loop A/ })).toBeVisible();
  expect(burst).toHaveAttribute("aria-expanded", "true");
  expect(burst.querySelector(".rz-disclosure-icon")).not.toBeNull();
  const members = within(changes).getByRole("list", { name: "Records changed together" });
  expect(within(members).getAllByRole("listitem")).toHaveLength(4);
  fireEvent.click(burst);
  expect(burst).toHaveAttribute("aria-expanded", "false");
  expect(within(changes).queryByRole("list", { name: "Records changed together" })).toBeNull();
  expect(within(changes).getAllByRole("listitem")).toHaveLength(lines.length);
});

it("does not call an out-of-date validation run clean", async () => {
  await renderGroupView(loadBriefing, {
    group: "People",
    routes: groupRoutes({
      "GET /api/v2/validate": () => jsonReply({ ...VALIDATION_ENVELOPE, health: "stale" }),
    }),
  });

  expect(await screen.findByText("Validation out of date")).toBeVisible();
  expect(screen.queryByText("No validation issues")).toBeNull();
});

it("shows only activity: Needs attention, In motion, and Recent changes", async () => {
  await renderGroupView(loadBriefing, { group: "People" });

  expect(
    screen.getAllByRole("region").map((block) => block.querySelector("h2 span")?.textContent),
  ).toEqual(["In motion", "Recent changes"]);
  expect(await screen.findByText("No validation issues")).toBeVisible();
  expect(await screen.findByText("Checked 2h ago")).toBeVisible();
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
    item(region("Review suggested"), "Next step empty").getByRole("button", {
      name: "Open work items",
    }),
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
  expect(within(region("Needs attention")).getByText("Validation issues")).toBeVisible();
  data = { ...RECORDS_DATA, Story: [], Bug: [] };
  await view.emit("node.changed");

  // The issues signal leaves; the strip, not the block, says "No validation issues".
  await waitFor(() =>
    expect(within(region("Needs attention")).queryByText("Validation issues")).toBeNull(),
  );
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
  expect(region("Review suggested")).toHaveTextContent("Priority empty");
  expect(region("Review suggested")).toHaveTextContent(reason);
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

it.each(["Area", "Special area"])("shows subtype metadata only when useful: %s", async (label) => {
  const summaries = Array.isArray(TYPE_SUMMARIES) ? TYPE_SUMMARIES : [];
  await renderGroupView(loadBriefing, {
    routes: groupRoutes({
      "POST /api/v1/graphql": () =>
        jsonReply({
          data: {
            ...RECORDS_DATA,
            Area: (Array.isArray(RECORDS_DATA.Area) ? RECORDS_DATA.Area : []).map((area) =>
              isJsonObject(area) ? { ...area, parent: null } : area,
            ),
          },
        }),
      "GET /api/v1/ontology/types": () =>
        jsonReply(
          summaries.map((entry) =>
            isJsonObject(entry) && entry.name === "Area" ? { ...entry, label } : entry,
          ),
        ),
    }),
  });
  const coverage = await screen.findByRole("region", { name: "Link coverage" });
  const areaGroup = within(coverage).getByRole("heading", { name: /^Areas/ }).parentElement;
  const metadata = areaGroup?.querySelectorAll(".gv-coverage-subtype") ?? [];

  if (label === "Area") expect(metadata).toHaveLength(0);
  else {
    expect(metadata.length).toBeGreaterThan(0);
    expect(metadata[0]).toHaveTextContent(label);
    expect(metadata[0]?.closest("ul")).toHaveAttribute("data-subtypes", "true");
  }
});
