import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { expect, it } from "vitest";

import {
  OPEN_COLLECTION_MESSAGE,
  OPEN_ISSUES_MESSAGE,
  OPEN_NODE_MESSAGE,
  OPEN_NOTE_MESSAGE,
} from "../../src/lib/customViewMessages";
import { deferredReply, jsonReply } from "../../src/test/fakeFetch";
import { isViewNodeRef } from "../../src/views/context";
import { DISPLAY_GROUPS, PATHS, RECORDS_DATA, TYPE_DOCS } from "./__fixtures__/groups.ts";
import { groupRoutes, renderGroupView } from "./__fixtures__/harness.tsx";
import { RECORD_CAP } from "./api.ts";
import { SECTION_ROWS } from "./sections.tsx";

const loadSections = () => import("./sections.tsx");

const section = (name: string) => screen.getByRole("region", { name });

const rowTitles = (region: HTMLElement) =>
  within(region)
    .getAllByRole("row")
    .slice(1)
    .map((row) => within(row).getAllByRole("button")[0].textContent);

const headers = (region: HTMLElement) =>
  within(region)
    .getAllByRole("columnheader")
    .map((header) => header.textContent);

it("renders each member as a table in Trace column order, most advanced first", async () => {
  await renderGroupView(loadSections);

  expect(await screen.findByRole("region", { name: "Areas" })).toBeVisible();
  expect(screen.getAllByRole("region").map((region) => region.getAttribute("aria-label"))).toEqual([
    "Areas",
    "Work items",
    "Releases",
    "Checklists",
  ]);
  expect(screen.getByText("Ordered like Trace columns.", { exact: false })).toBeVisible();

  const work = section("Work items");
  expect(headers(work)).toEqual([
    "Title",
    "Type",
    "Stage",
    "Kind",
    "Next step",
    "Area",
    "Resolution",
    "Changed",
  ]);
  expect(rowTitles(work)).toEqual([
    "Tax rounding",
    "Checkout redesign",
    "Saved carts",
    "Gift cards",
    "Login loop",
    "Wishlist",
  ]);

  const tax = within(work).getByRole("button", { name: "Tax rounding" }).closest("tr");

  if (!tax) throw new Error("no Tax rounding row");
  const cells = within(tax).getAllByRole("cell");

  expect(cells.map((cell) => cell.textContent)).toEqual([
    // The mark's label is read before the title.
    "BlockedTax rounding" + "Totals round tax per line instead of per order.",
    "Bug",
    "Blocked",
    "—None",
    "Wait for finance",
    "Payments",
    "—None",
    expect.stringMatching(/^2h/),
  ]);
});

it("renders a hierarchical member as a tree and keeps a parent cycle finite", async () => {
  await renderGroupView(loadSections);
  const areas = await screen.findByRole("region", { name: "Areas" });

  expect(headers(areas)).toEqual(["Title", "Category", "Changed"]);

  const depths = within(areas)
    .getAllByRole("row")
    .slice(1)
    .map((row) => [
      within(row).getAllByRole("button")[0].textContent,
      row.getAttribute("data-depth"),
    ]);

  expect(depths).toEqual([
    ["Platform", null],
    ["Commerce", null],
    ["Payments", "1"],
    ["Wishlists", "1"],
    ["Loop B", null],
    ["Loop A", "1"],
  ]);

  // Nesting is read out, not only drawn.
  const payments = within(areas).getByRole("button", { name: "Payments" }).closest("tr");
  expect(payments).toHaveTextContent("Payments, under Commerce");
});

it("keeps the lifecycle column and drops other columns empty on every shown row", async () => {
  await renderGroupView(loadSections);

  expect(headers(await screen.findByRole("region", { name: "Releases" }))).toEqual([
    "Title",
    "Release status",
    "Date",
    "Notes",
    "Changed",
  ]);
  expect(headers(section("Checklists"))).toEqual(["Title", "Changed"]);
});

it("orders an unlinked group by size, drops the shared label prefix, and says when a member is empty", async () => {
  const routes = groupRoutes({
    "POST /api/v1/graphql": () => jsonReply({ data: { ...RECORDS_DATA, Memo: [] } }),
  });

  await renderGroupView(loadSections, { group: "Library", routes });

  expect(await screen.findByRole("region", { name: "Glossaries" })).toBeVisible();
  expect(screen.getAllByRole("region").map((region) => region.getAttribute("aria-label"))).toEqual([
    "Glossaries",
    "Memos",
  ]);
  expect(screen.getByText("Ordered by size.", { exact: false })).toBeVisible();
  expect(within(section("Memos")).getByText("None recorded yet.")).toBeVisible();
});

it("bounds each table and links the rest to the collection", async () => {
  const people = Array.from({ length: SECTION_ROWS + 3 }, (_, index) => ({
    path: `people/${index}.md`,
    title: `Person ${String(index).padStart(2, "0")}`,
    updatedAt: new Date(Date.UTC(2026, 8, 1 + index)).toISOString(),
  }));

  const routes = groupRoutes({
    "POST /api/v1/graphql": () => jsonReply({ data: { Person: people } }),
  });

  await renderGroupView(loadSections, { group: "People", routes });
  const region = await screen.findByRole("region", { name: "People" });

  // Without a lifecycle, newest first.
  expect(rowTitles(region)).toHaveLength(SECTION_ROWS);
  expect(rowTitles(region)[0]).toBe("Person 10");
  expect(
    within(region).getByRole("button", { name: `Show all ${SECTION_ROWS + 3}` }),
  ).toBeVisible();
  expect(within(region).getByText("· newest first", { exact: false })).toBeVisible();
});

it("opens records, collections, and scoped issues through the workspace", async () => {
  const { posted } = await renderGroupView(loadSections, { embedded: true });
  const work = await screen.findByRole("region", { name: "Work items" });

  const row = (title: string) => {
    const found = within(work).getByRole("button", { name: title }).closest("tr");

    if (!found) throw new Error(`no row for ${title}`);

    return within(found);
  };

  // Counts are published issues: the member's two are both on Gift cards, the
  // one record the facts strip counts.
  expect(document.querySelector(".gv-facts-list")).toHaveTextContent("1 record with issues");

  fireEvent.click(within(work).getByRole("button", { name: "Checkout redesign" }));
  fireEvent.click(within(work).getByRole("button", { name: "Work items" }));
  // The header's member issue count comes before the rows' counts.
  fireEvent.click(within(work).getAllByRole("button", { name: "2 issues" })[0]);
  fireEvent.click(row("Gift cards").getByRole("button", { name: "2 issues" }));
  fireEvent.click(row("Checkout redesign").getByRole("button", { name: "Payments" }));

  // The workspace's own guard accepts the ref the view posts.
  expect(isViewNodeRef(posted.find((message) => message.type === OPEN_NODE_MESSAGE)?.ref)).toBe(
    true,
  );
  expect(posted.filter((message) => message.type.startsWith("rhizome:open-"))).toEqual([
    {
      type: OPEN_NODE_MESSAGE,
      ref: { notePath: PATHS.checkout, kind: "NOTE", typeName: "Story" },
    },
    { type: OPEN_COLLECTION_MESSAGE, name: "Work" },
    { type: OPEN_ISSUES_MESSAGE, scope: { kind: "interface", key: "Work" } },
    { type: OPEN_ISSUES_MESSAGE, scope: { kind: "note", key: PATHS.gifts } },
    { type: OPEN_NOTE_MESSAGE, path: PATHS.payments, beside: false },
  ]);
});

it("shows loading, then the group, and refetches once per vault change", async () => {
  const first = deferredReply<{ data: typeof RECORDS_DATA }>();
  let answer = () => first.promise;

  const view = await renderGroupView(loadSections, {
    group: "People",
    routes: groupRoutes({ "POST /api/v1/graphql": () => answer() }),
  });

  expect(await screen.findByText("Loading the group…")).toBeVisible();
  first.resolve({ data: RECORDS_DATA });
  expect(await screen.findByRole("button", { name: "Ada Lovelace" })).toBeVisible();

  const graphqlCalls = view.http.requests("POST", "/api/v1/graphql").length;
  answer = async () =>
    jsonReply({
      data: {
        Person: [{ path: PATHS.ada, title: "Ada King", updatedAt: new Date().toISOString() }],
      },
    });

  await view.emit("node.changed");
  expect(await screen.findByRole("button", { name: "Ada King" })).toBeVisible();
  expect(view.http.requests("POST", "/api/v1/graphql").length).toBe(graphqlCalls + 1);
});

it("keeps the group on screen and says so when a refresh fails", async () => {
  let answer = async () => jsonReply({ data: RECORDS_DATA });

  const view = await renderGroupView(loadSections, {
    group: "People",
    routes: groupRoutes({ "POST /api/v1/graphql": () => answer() }),
  });

  expect(await screen.findByRole("button", { name: "Ada Lovelace" })).toBeVisible();
  expect(screen.queryByText(/Could not refresh/)).toBeNull();

  answer = async () => jsonReply({ errors: [{ message: "index offline" }] });
  await view.emit("node.changed");

  expect(await screen.findByText(/Could not refresh: index offline/)).toBeVisible();
  expect(screen.getByRole("button", { name: "Ada Lovelace" })).toBeVisible();

  answer = async () => jsonReply({ data: RECORDS_DATA });
  await view.emit("node.changed");
  await waitFor(() => expect(screen.queryByText(/Could not refresh/)).toBeNull());
});

it("loads without type labels when they fail", async () => {
  await renderGroupView(loadSections, {
    group: "People",
    routes: groupRoutes({ "GET /api/v1/ontology/types": () => jsonReply({}, 500) }),
  });

  expect(await screen.findByRole("button", { name: "Ada Lovelace" })).toBeVisible();
  expect(screen.queryByRole("alert")).toBeNull();
});

it("reports a failed load, a missing group, and truncated records", async () => {
  await renderGroupView(loadSections, {
    group: "People",
    routes: groupRoutes({
      "POST /api/v1/graphql": () => jsonReply({ errors: [{ message: "index offline" }] }),
    }),
  });

  expect(await screen.findByRole("alert")).toHaveTextContent(
    "Could not load the group: index offline",
  );
});

it("shows the records a response returns beside errors, such as a missing required field", async () => {
  await renderGroupView(loadSections, {
    group: "People",
    routes: groupRoutes({
      "POST /api/v1/graphql": () =>
        jsonReply({
          data: RECORDS_DATA,
          errors: [
            { message: "required field displayName is missing", path: ["Person", "displayName"] },
          ],
        }),
    }),
  });

  expect(await screen.findByRole("button", { name: "Ada Lovelace" })).toBeVisible();
  expect(screen.queryByRole("alert")).toBeNull();
});

it("names a display group that does not exist", async () => {
  await renderGroupView(loadSections, { group: "Gone" });

  expect(await screen.findByText("No display group named Gone.")).toBeVisible();
});

it("says when a type has more records than were read", async () => {
  const people = Array.from({ length: RECORD_CAP + 1 }, (_, index) => ({
    path: `people/${index}.md`,
    title: `Person ${index}`,
  }));

  await renderGroupView(loadSections, {
    group: "People",
    routes: groupRoutes({ "POST /api/v1/graphql": () => jsonReply({ data: { Person: people } }) }),
  });

  await waitFor(() =>
    expect(screen.getByRole("note")).toHaveTextContent(
      `Showing the ${RECORD_CAP} most recently changed records of each type; People has more.`,
    ),
  );
  expect(screen.getByText(String(RECORD_CAP), { selector: "b" })).toBeVisible();
});

it("keeps showing the group while a type that joined it loads its documentation", async () => {
  const doc = deferredReply<{ type: typeof TYPE_DOCS.Glossary; count: number }>();
  let groups = DISPLAY_GROUPS;

  const view = await renderGroupView(loadSections, {
    group: "People",
    routes: groupRoutes({
      "GET /api/v1/display-groups": () => jsonReply(groups),
      "GET /api/v1/ontology/types/Glossary": () => doc.promise,
    }),
  });

  expect(await screen.findByRole("button", { name: "Ada Lovelace" })).toBeVisible();

  const people = DISPLAY_GROUPS.groups.find((group) => group.name === "People");
  const glossary = DISPLAY_GROUPS.groups.find((group) => group.name === "Library")?.members[1];

  if (!people || !glossary) throw new Error("fixture groups missing");

  groups = {
    groups: DISPLAY_GROUPS.groups.map((group) =>
      group === people ? { ...group, members: [...group.members, glossary] } : group,
    ),
  };

  await view.emit("index.changed");
  await waitFor(() => expect(screen.getByRole("main")).toHaveAttribute("aria-busy", "true"));
  expect(screen.getByRole("button", { name: "Ada Lovelace" })).toBeVisible();
  expect(screen.queryByText("None recorded yet.")).toBeNull();

  doc.resolve({ type: TYPE_DOCS.Glossary, count: 3 });
  expect(await screen.findByRole("region", { name: "Library glossaries" })).toBeVisible();
});
