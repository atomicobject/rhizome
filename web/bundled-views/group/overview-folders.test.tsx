import { fireEvent, screen, within } from "@testing-library/react";
import { expect, it } from "vitest";
import { jsonReply } from "../../src/test/fakeFetch";
import { AGGREGATE_PARTS } from "./__fixtures__/aggregate.ts";
import { renderGroupView, scopeRoutes } from "./__fixtures__/harness.tsx";

const loadOverview = () => import("./overview.tsx");

const WORKSPACE = { kind: "workspace" } as const;

const region = (name: string) => screen.getByRole("region", { name });

function rowNamed(table: HTMLElement, name: string) {
  const row = within(table)
    .getAllByRole("row")
    .find((candidate) => candidate.querySelector("td")?.textContent?.includes(name));

  if (!row) throw new Error(`no row ${name}`);

  return row;
}

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
  expect(screen.queryByRole("region", { name: "Declared relations with no links" })).toBeNull();
});

it("searches the vault root's own untyped notes as folder /", async () => {
  const { posted } = await renderGroupView(loadOverview, {
    context: WORKSPACE,
    embedded: true,
    routes: scopeRoutes({
      "GET /api/v1/ontology/shape": (request) =>
        request.query.get("parts") === "folders"
          ? jsonReply({
              ...AGGREGATE_PARTS.folders,
              folders: {
                rows: [{ folder: "", total: 3, untyped: 2, typed: {}, untypedLinksTo: {} }],
              },
            })
          : jsonReply(
              request.query.get("parts") === "members"
                ? AGGREGATE_PARTS.members
                : AGGREGATE_PARTS.links,
            ),
    }),
  });

  fireEvent.click(within(region("Untyped notes by folder")).getByRole("button", { name: "/" }));

  expect(posted.filter((message) => message.type === "rhizome:open-search")).toEqual([
    { type: "rhizome:open-search", folder: "/" },
  ]);
});

it("lists the eight largest folders before +N more", async () => {
  const rows = Array.from({ length: 11 }, (_, index) => ({
    folder: `F${index}`,
    total: 20,
    untyped: 20 - index,
    typed: {},
    untypedLinksTo: {},
  }));

  await renderGroupView(loadOverview, {
    context: WORKSPACE,
    embedded: true,
    routes: scopeRoutes({
      "GET /api/v1/ontology/shape": (request) =>
        request.query.get("parts") === "folders"
          ? jsonReply({ ...AGGREGATE_PARTS.folders, folders: { rows } })
          : jsonReply(
              request.query.get("parts") === "members"
                ? AGGREGATE_PARTS.members
                : AGGREGATE_PARTS.links,
            ),
    }),
  });

  const folders = within(region("Untyped notes by folder"));
  expect(folders.getAllByRole("row")).toHaveLength(1 + 8);
  fireEvent.click(folders.getByRole("button", { name: "Show 3 more" }));
  expect(folders.getAllByRole("row")).toHaveLength(1 + 11);
});
