import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { expect, it } from "vitest";

import {
  OPEN_COLLECTION_MESSAGE,
  OPEN_ISSUES_MESSAGE,
  OPEN_NOTE_MESSAGE,
  OPEN_VIEW_MESSAGE,
} from "../../src/lib/customViewMessages";
import { jsonReply } from "../../src/test/fakeFetch";
import { AGGREGATE_MEMBERS, AGGREGATE_PARTS } from "./__fixtures__/aggregate.ts";
import { PATHS, PLANNING_GUIDE } from "./__fixtures__/groups.ts";
import { renderGroupView, scopeRoutes } from "./__fixtures__/harness.tsx";

const loadOverview = () => import("./overview.tsx");

const WORKSPACE = { kind: "workspace" } as const;

const region = (name: string) => screen.getByRole("region", { name });

const map = () => screen.getByRole("group", { name: /map$/ });

/** Member nodes in keyboard order. */
const nodeNames = () =>
  [...map().querySelectorAll<SVGGElement>("g[tabindex='0']")].map((node) =>
    node.getAttribute("aria-label"),
  );

const opened = (posted: readonly { type: string }[]) =>
  posted.filter((message) => message.type.startsWith("rhizome:open-"));

function rowNamed(table: HTMLElement, name: string) {
  const row = within(table)
    .getAllByRole("row")
    .find((candidate) => candidate.querySelector("td")?.textContent?.includes(name));

  if (!row) throw new Error(`no row ${name}`);

  return row;
}

it("draws a group's members most records first, with keyboard focus in that order", async () => {
  await renderGroupView(loadOverview, { routes: scopeRoutes() });

  expect(nodeNames()).toEqual([
    "Areas, 6 records, 6 links to other members",
    "Work items, 6 records, 7 links to other members",
    "Releases, 2 records, 2 links to other members",
    "Checklists, 1 record, 1 link to other members",
  ]);
  // Outside neighbors sit on the ring with their link counts, and the unused relation is a hairline.
  expect(map().querySelectorAll("[data-outside='true']")).toHaveLength(2);
  expect(map()).toHaveTextContent("Untyped notes 3 links");
  expect(map().querySelectorAll(".gv-map-unused")).toHaveLength(1);
  expect(region("Map")).toHaveTextContent("declared, unused");
});

it("lays the map out the same way on every load", async () => {
  const positions = () =>
    [...map().querySelectorAll("circle")].map((circle) => [
      circle.getAttribute("cx"),
      circle.getAttribute("cy"),
    ]);

  const first = await renderGroupView(loadOverview, { routes: scopeRoutes() });
  const drawn = positions();
  first.unmount();

  await renderGroupView(loadOverview, { routes: scopeRoutes() });

  expect(positions()).toEqual(drawn);
});

it("highlights a focused node's edges and shows its figures", async () => {
  await renderGroupView(loadOverview, { routes: scopeRoutes() });
  const work = map().querySelector<SVGGElement>("[data-node='Work']");

  if (!work) throw new Error("no Work node");
  fireEvent.focus(work);

  expect(map()).toHaveAttribute("data-dim", "true");
  expect(work).toHaveAttribute("data-hot", "true");
  expect(map().querySelector("[data-node='Checklist']")).not.toHaveAttribute("data-hot");
  expect(screen.getByRole("tooltip")).toHaveTextContent(
    "Work items6 records · 7 links in the group2 among themselves",
  );
});

it("opens a member from its node and the group's Trace from a relation edge", async () => {
  const { posted } = await renderGroupView(loadOverview, { routes: scopeRoutes(), embedded: true });
  const areas = map().querySelector<SVGGElement>("[data-node='Area']");

  if (!areas) throw new Error("no Area node");
  fireEvent.keyDown(areas, { key: "Enter" });
  fireEvent.click(map().querySelector("[data-action='trace']") ?? areas);

  expect(opened(posted)).toEqual([
    { type: OPEN_COLLECTION_MESSAGE, name: "Area" },
    { type: OPEN_VIEW_MESSAGE, id: "group.trace", context: { kind: "group", group: "Planning" } },
  ]);
});

it("switches to the Matrix, with self links, an Outside column, and unused relations", async () => {
  await renderGroupView(loadOverview, { routes: scopeRoutes() });
  fireEvent.click(within(region("Map")).getByRole("button", { name: "Matrix" }));
  const table = within(region("Map")).getByRole("table");

  expect(
    within(table)
      .getAllByRole("columnheader")
      .map((header) => header.textContent),
  ).toEqual(["Areas", "Work items", "Releases", "Checklists", "Outside"]);

  const cells = (name: string) =>
    [
      ...(within(table).getByRole("rowheader", { name }).closest("tr")?.querySelectorAll("td") ??
        []),
    ].map((cell) => [cell.getAttribute("data-cell"), cell.textContent]);

  expect(cells("Work items")).toEqual([
    ["relation", "6 (5 relation, 1 plain)"],
    ["self", "2 among its own records"],
    ["relation", "1 (1 relation, 0 plain)"],
    [null, ""],
    ["plain", "5"],
  ]);
  expect(cells("Releases")[0]).toEqual(["unused", "declared, unused"]);
  expect(within(region("Map")).getByRole("button", { name: "Matrix" })).toHaveAttribute(
    "aria-pressed",
    "true",
  );
});

it("compares members in one table, nesting an interface's implementors", async () => {
  const { posted } = await renderGroupView(loadOverview, { routes: scopeRoutes(), embedded: true });
  const table = within(region("Members")).getByRole("table");
  const work = rowNamed(table, "Work items");

  expect(work).toHaveAttribute("title", "Planned work.");
  expect(work).toHaveTextContent("Work items6");
  expect(work).toHaveTextContent("4 active");
  expect(work).toHaveTextContent("Next step 3/6");
  expect(work).toHaveTextContent("67%");
  // Optional gaps stay neutral.
  expect(work.querySelector("[data-required='true']")).toBeNull();
  expect(rowNamed(table, "Stories")).toHaveAttribute("data-kind", "implementor");
  expect(rowNamed(table, "Areas")).toHaveTextContent("no lifecycle");

  fireEvent.click(within(rowNamed(table, "Areas")).getByRole("button", { name: /Areas/ }));
  fireEvent.click(within(work).getByRole("button", { name: "2 issues in Work items" }));

  expect(opened(posted)).toEqual([
    { type: OPEN_COLLECTION_MESSAGE, name: "Area" },
    { type: OPEN_ISSUES_MESSAGE, scope: { kind: "interface", key: "Work" } },
  ]);
});

it("lists relations declared between members that no record uses", async () => {
  await renderGroupView(loadOverview, { routes: scopeRoutes() });
  const unused = region("Declared, unused");

  expect(within(unused).getByText("1")).toBeVisible();
  expect(unused).toHaveTextContent("Releasesarea → Areas");
  // A long list truncates beside the name; the full list is its title.
  expect(within(unused).getByTitle("area → Areas")).toBeVisible();
});

it("lists the notes outside the group most connected first, then the guide and views", async () => {
  const { posted } = await renderGroupView(loadOverview, { routes: scopeRoutes(), embedded: true });
  const outside = region("Outside the group");

  expect(within(outside).getByText("4 notes")).toBeVisible();
  expect(outside.querySelector(".gv-outsum")).toHaveTextContent("1 person3 untyped notes");
  expect([...outside.querySelectorAll("li")].map((entry) => entry.textContent)).toEqual([
    "Ada Lovelace person2 linked records",
    "Kickoff untyped note2 linked records",
    "Release guide untyped note1 linked record",
    "Scratch untyped note1 linked record",
  ]);

  fireEvent.click(within(outside).getByRole("button", { name: "Kickoff" }));
  fireEvent.click(
    within(region("Guide and views")).getByRole("button", { name: "Planning board" }),
  );
  fireEvent.click(
    within(region("Guide and views")).getByRole("button", { name: "Planning guide" }),
  );

  expect(opened(posted)).toEqual([
    { type: OPEN_NOTE_MESSAGE, path: PATHS.kickoff, beside: false },
    {
      type: OPEN_VIEW_MESSAGE,
      id: "planning.board",
      context: { kind: "group", group: "Planning" },
    },
    { type: OPEN_NOTE_MESSAGE, path: PLANNING_GUIDE, beside: false },
  ]);
});

it("says when no member records link and keeps the Matrix, and draws a one-member group", async () => {
  const library = await renderGroupView(loadOverview, { group: "Library", routes: scopeRoutes() });

  expect(region("Map")).toHaveTextContent("No member records link to each other.");
  expect(map()).toHaveTextContent("No member links");
  expect(within(region("Map")).getByRole("button", { name: "Matrix" })).toBeEnabled();
  library.unmount();

  await renderGroupView(loadOverview, { group: "People", routes: scopeRoutes() });

  expect(nodeNames()).toEqual(["People, 2 records, 0 links to other members"]);
  expect(map()).toHaveTextContent("Meetings 4 links");
  expect(within(region("Members")).getAllByRole("row")).toHaveLength(2);
});

it("names a display group that no longer exists", async () => {
  await renderGroupView(loadOverview, { group: "Gone", routes: scopeRoutes() });

  expect(screen.getByText("No display group named Gone.")).toBeVisible();
  expect(screen.queryByRole("region", { name: "Map" })).toBeNull();
});

it("states All notes' facts and draws named groups collapsed beside ungrouped types and untyped notes", async () => {
  const { posted } = await renderGroupView(loadOverview, {
    context: WORKSPACE,
    routes: scopeRoutes(),
    embedded: true,
  });

  await waitFor(() => expect(screen.getByRole("button", { name: "5 issues" })).toBeVisible());
  expect(document.querySelector(".gv-facts-list")).toHaveTextContent(
    "49 notes59% typed10 note types3 ambiguous5 issues",
  );
  expect(nodeNames()).toEqual([
    "Untyped notes, 20 notes, 11 links",
    "Planning, 15 records, 5 links, collapsed group of 4 types",
    "Library, 5 records, 2 links, collapsed group of 2 types",
    "Meetings, 5 records, 10 links",
    "People, 2 records, 6 links, collapsed group of 1 type",
    "Notices, 0 records, 0 links",
  ]);

  fireEvent.click(screen.getByRole("button", { name: "3 ambiguous" }));
  expect(opened(posted)).toEqual([{ type: OPEN_ISSUES_MESSAGE }]);
});

it("expands a group in place on the map and in the table, and collapses it from the legend", async () => {
  await renderGroupView(loadOverview, { context: WORKSPACE, routes: scopeRoutes() });
  const planning = map().querySelector<SVGGElement>("[data-node='group:Planning']");

  if (!planning) throw new Error("no Planning node");
  fireEvent.keyDown(planning, { key: " " });

  await waitFor(() => expect(map().querySelector("[data-node='Area']")).not.toBeNull());
  const table = within(region("Members")).getByRole("table");
  expect(within(rowNamed(table, "Planning")).getByRole("button", { expanded: true })).toBeVisible();
  expect(rowNamed(table, "Areas")).toBeVisible();

  fireEvent.click(within(region("Map")).getByRole("button", { name: "collapse" }));
  await waitFor(() => expect(map().querySelector("[data-node='group:Planning']")).not.toBeNull());
  expect(within(table).queryByText("Areas")).toBeNull();
});

it("lists untyped notes by folder and searches a folder, with the coverage line beneath", async () => {
  const { posted } = await renderGroupView(loadOverview, {
    context: WORKSPACE,
    routes: scopeRoutes(),
    embedded: true,
  });

  const folders = region("Untyped notes by folder");
  expect(within(folders).getByText("20 notes")).toBeVisible();
  const notes = rowNamed(within(folders).getByRole("table"), "Notes/");
  // One line per row: the folder size and full link list live in titles.
  expect(notes).toHaveTextContent("Notes/1240%Meetings 4, People 2");
  expect(notes.querySelector("[title='12 of 30 notes in Notes/']")).not.toBeNull();
  expect(notes.querySelector(".gv-ov-links")).toHaveAttribute("title", "Meetings 4, People 2");
  expect(within(folders).queryByText("Work/")).toBeNull();

  fireEvent.click(within(folders).getByRole("button", { name: "Inbox/" }));
  expect(posted.filter((message) => message.type === "rhizome:open-search")).toEqual([
    { type: "rhizome:open-search", folder: "Inbox" },
  ]);

  expect(region("Coverage")).toHaveTextContent(
    "No recordsNoticesNo linksLibrary glossaries 3EmbeddedTasks 7 · items inside notes, not drawn",
  );
  expect(screen.queryByRole("region", { name: "Declared, unused" })).toBeNull();
});

it("paints every block's heading at once and fills each in as its part arrives", async () => {
  let release = () => {};

  const held = new Promise<void>((resolve) => {
    release = resolve;
  });

  await renderGroupView(loadOverview, {
    context: WORKSPACE,
    settle: false,
    routes: scopeRoutes({
      "GET /api/v1/ontology/shape": async (request) => {
        const part = request.query.get("parts");

        if (part === "links") await held;

        return jsonReply(
          part === "members"
            ? AGGREGATE_PARTS.members
            : part === "links"
              ? AGGREGATE_PARTS.links
              : AGGREGATE_PARTS.folders,
        );
      },
    }),
  });

  // The folder block needs no links; the map and table wait for them.
  await waitFor(() =>
    expect(within(region("Untyped notes by folder")).getByRole("table")).toBeVisible(),
  );
  expect(within(region("Map")).getByRole("status")).toHaveTextContent("Loading the map…");
  expect(within(region("Members")).getByRole("status")).toHaveTextContent("Loading members…");
  expect(document.querySelector(".gv-facts-list")).toHaveTextContent("49 notes");

  release();

  await waitFor(() => expect(within(region("Members")).getByRole("table")).toBeVisible());
});

it("fails one block without the others and retries it", async () => {
  let fail = true;

  await renderGroupView(loadOverview, {
    context: WORKSPACE,
    settle: false,
    routes: scopeRoutes({
      "GET /api/v1/ontology/shape": (request) => {
        const part = request.query.get("parts");

        if (part === "folders")
          return fail ? jsonReply({ error: "down" }, 500) : jsonReply(AGGREGATE_PARTS.folders);

        return jsonReply(part === "members" ? AGGREGATE_PARTS.members : AGGREGATE_PARTS.links);
      },
    }),
  });

  const alert = await within(region("Untyped notes by folder")).findByRole("alert");
  expect(alert).toHaveTextContent("Could not load folders");
  await waitFor(() => expect(within(region("Members")).getByRole("table")).toBeVisible());
  expect(within(region("Map")).queryByRole("alert")).toBeNull();

  fail = false;
  fireEvent.click(within(alert).getByRole("button", { name: "Retry" }));

  await waitFor(() =>
    expect(within(region("Untyped notes by folder")).getByRole("table")).toBeVisible(),
  );
});

it("says the index is rebuilding instead of drawing empty counts", async () => {
  await renderGroupView(loadOverview, {
    context: WORKSPACE,
    routes: scopeRoutes({
      "GET /api/v1/ontology/shape": (request) =>
        jsonReply({
          ...(request.query.get("parts") === "members"
            ? AGGREGATE_PARTS.members
            : request.query.get("parts") === "links"
              ? AGGREGATE_PARTS.links
              : AGGREGATE_PARTS.folders),
          rebuilding: true,
          members: { ...AGGREGATE_MEMBERS, types: [] },
        }),
    }),
  });

  expect(document.querySelector(".gv-facts")).toHaveTextContent("The index is rebuilding");
  expect(within(region("Map")).getByText(/index is rebuilding/)).toBeVisible();
  expect(within(region("Members")).queryByRole("table")).toBeNull();
});
