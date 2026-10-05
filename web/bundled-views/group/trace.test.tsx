import { fireEvent, screen, within } from "@testing-library/react";
import { expect, it } from "vitest";

import {
  OPEN_COLLECTION_MESSAGE,
  OPEN_NODE_MESSAGE,
  OPEN_NOTE_MESSAGE,
} from "../../src/lib/customViewMessages";
import { jsonReply } from "../../src/test/fakeFetch";
import { NOW, PATHS, RECORDS_DATA, TYPE_DOCS } from "./__fixtures__/groups.ts";
import { groupRoutes, renderGroupView } from "./__fixtures__/harness.tsx";
import { isJsonObject, type JsonObject, type JsonValue } from "./api.ts";
import { BAND_ROWS, CELL_RECORDS } from "./traceRows.tsx";

const loadTrace = () => import("./trace.tsx");

const rowsOf = (value: JsonValue | undefined) => (Array.isArray(value) ? value : []);

/** Fixture rows of one type with `change` applied to each. */
const changed = (type: string, change: (row: JsonObject) => JsonObject) =>
  rowsOf(RECORDS_DATA[type]).map((row) => (isJsonObject(row) ? change(row) : row));

const payments = { path: PATHS.payments, title: "Payments", resolvedType: "Area" };

/** Generated bugs in one stage, all in the Payments area. */
const bugs = (count: number, stage: string) =>
  Array.from({ length: count }, (_, index) => ({
    ref: { notePath: `work/bug-${index}.md`, kind: "NOTE", typeName: "Bug" },
    path: `work/bug-${index}.md`,
    title: `Bug ${String(index).padStart(2, "0")}`,
    updatedAt: new Date(NOW - index * 60_000).toISOString(),
    stage,
    area: payments,
  }));

const withData = (data: JsonObject) =>
  groupRoutes({ "POST /api/v1/graphql": () => jsonReply({ data }) });

async function chooseRows(label: string) {
  fireEvent.click(await screen.findByRole("radio", { name: new RegExp(`^${label}`) }));
}

/** A column's label: its first button's text without the record count. */
const columnName = (header: HTMLElement) =>
  within(header)
    .getAllByRole("button")[0]
    .textContent?.replace(/\u00a0\d+$/, "");

const columnNames = () => screen.getAllByRole("columnheader").map(columnName);

const columnHeader = (name: string) => {
  const header = screen
    .getAllByRole("columnheader")
    .find((candidate) => columnName(candidate) === name);

  if (!header) throw new Error(`no column ${name}`);

  return header;
};

/** The record rows in reading order, as [title, depth]. */
const rowTitles = () =>
  within(screen.getByRole("table"))
    .getAllByRole("row")
    .flatMap((row) => {
      if (!row.hasAttribute("data-record-key")) return [];
      const header = within(row).getAllByRole("rowheader")[0];

      const title = within(header)
        .getAllByRole("button")
        .find((button) => !button.hasAttribute("aria-expanded"));

      return [[title?.textContent, row.getAttribute("data-depth")]];
    });

function row(title: string) {
  const found = screen.getAllByRole("row").find((candidate) =>
    within(candidate)
      .queryAllByRole("rowheader")
      .some((header) => within(header).queryByRole("button", { name: title })),
  );

  if (!found) throw new Error(`no row for ${title}`);

  return found;
}

/** Chip titles in each of a row's cells, in column order; an indirect chip says so. */
const cellTitles = (title: string) =>
  within(row(title))
    .getAllByRole("cell")
    .map((cell) =>
      within(cell)
        .queryAllByRole("button")
        .map((button) => button.title || button.textContent),
    );

/** Band headers in order, without their caret. */
const bands = () =>
  screen
    .getAllByRole("button", { expanded: true })
    .concat(screen.queryAllByRole("button", { expanded: false }))
    .filter((button) => !button.getAttribute("aria-label")?.startsWith("Rows under"))
    .sort((a, b) => (a.compareDocumentPosition(b) & Node.DOCUMENT_POSITION_FOLLOWING ? -1 : 1))
    .map((button) => button.textContent?.replace(/[▾▸]/g, ""));

it("defaults to the hierarchical member, nests its rows, and rolls subtrees up", async () => {
  await renderGroupView(loadTrace);
  await screen.findByRole("table");

  expect(screen.getAllByRole("radio").map((radio) => radio.parentElement?.textContent)).toEqual([
    "Areas6(default)",
    "Work items6",
    "Releases2",
    "Checklists1",
  ]);
  expect(screen.getByRole("radio", { name: /^Areas/ })).toBeChecked();
  expect(screen.getByText(/the tree other types link to/)).toBeVisible();

  // Most connected subtree first; the Loop A-B parent cycle still renders, once each.
  expect(rowTitles()).toEqual([
    ["Commerce", null],
    ["Payments", "1"],
    ["Wishlists", "1"],
    ["Platform", null],
    ["Loop A", null],
    ["Loop B", "1"],
  ]);
  expect(row("Payments")).toHaveTextContent("Payments, under Commerce");

  // Commerce links nothing itself; its column cells count what its subtree links.
  const commerce = within(row("Commerce")).getAllByRole("cell");
  expect(commerce.map((cell) => cell.textContent)).toEqual([
    "4 in subtree",
    "1 in subtree",
    "1 in subtree",
  ]);
  expect(within(row("Payments")).getAllByRole("cell")[0]).not.toHaveTextContent("in subtree");

  const toggle = screen.getByRole("button", { name: "Rows under Commerce" });
  expect(toggle).toHaveAttribute("aria-expanded", "true");

  fireEvent.click(toggle);
  expect(toggle).toHaveAttribute("aria-expanded", "false");
  expect(rowTitles().map(([title]) => title)).toEqual(["Commerce", "Platform", "Loop A", "Loop B"]);
  expect(row("Commerce")).toHaveTextContent("2 nested");
  expect(within(row("Commerce")).getAllByRole("cell")[0]).toHaveTextContent("4 in subtree");

  fireEvent.click(toggle);
  expect(rowTitles()).toHaveLength(6);
});

it("fills cells with records linked to anything in the row, marking indirect ones, and reports coverage", async () => {
  await renderGroupView(loadTrace);
  await screen.findByRole("table");

  expect(cellTitles("Payments")).toEqual([
    ["Checkout redesign", "Saved carts", "Tax rounding"],
    ["Fall release (reached through another record)"],
    ["Fall checklist (reached through another record)"],
  ]);
  expect(within(row("Payments")).getAllByText("(indirect)")).toHaveLength(2);

  expect(columnHeader("Work items")).toHaveTextContent("3/6 rows · link to rows");
  expect(columnHeader("Releases")).toHaveTextContent("1/6 rows · link to rows");
  expect(columnHeader("Checklists")).toHaveTextContent("1/6 rows · through other columns");
  expect(screen.getByText("2 records appear in no row: 1 work item and 1 release.")).toBeVisible();
});

it("switches rows and orders columns by contract with outside links last", async () => {
  await renderGroupView(loadTrace);

  await chooseRows("Work items");
  expect(screen.getByRole("radio", { name: /^Work items/ })).toBeChecked();
  expect(columnNames()).toEqual(["Work items", "Areas", "Releases", "Checklists", "Owner"]);
  expect(columnHeader("Areas")).toHaveTextContent("5/6 rows · linked from rows");
  expect(columnHeader("Owner")).toHaveTextContent("1/6 rows · in People");
  expect(cellTitles("Checkout redesign")).toEqual([
    ["Payments"],
    ["Fall release"],
    ["Fall checklist (reached through another record)"],
    ["Ada Lovelace"],
  ]);
});

it("bands rows by lifecycle, most advanced first and closed values last, naming empty values", async () => {
  const data = {
    ...RECORDS_DATA,
    Story: changed("Story", (story) =>
      story.path === PATHS.gifts ? { ...story, stage: null } : story,
    ),
  };

  await renderGroupView(loadTrace, { routes: withData(data) });
  await chooseRows("Work items");

  expect(bands()).toEqual(["Blocked1", "Doing2", "Done2", "No stage1"]);
  expect(columnHeader("Work items")).toHaveTextContent("by stage, most advanced first");
  expect(columnHeader("Work items")).toHaveTextContent("Empty: Backlog, Dropped");
  expect(rowTitles().map(([title]) => title)).toEqual([
    "Tax rounding",
    "Checkout redesign",
    "Saved carts",
    "Login loop",
    "Wishlist",
    "Gift cards",
  ]);
});

it("folds a large closed band until it is opened", async () => {
  const data = { ...RECORDS_DATA, Bug: [...rowsOf(RECORDS_DATA.Bug), ...bugs(4, "dropped")] };

  await renderGroupView(loadTrace, { routes: withData(data) });
  await chooseRows("Work items");

  const dropped = screen.getByRole("button", { name: /^Dropped/ });
  expect(dropped).toHaveAttribute("aria-expanded", "false");
  expect(rowTitles().map(([title]) => title)).not.toContain("Bug 00");

  // Done is closed too, but small enough to stay open.
  expect(screen.getByRole("button", { name: /^Done/ })).toHaveAttribute("aria-expanded", "true");

  fireEvent.click(dropped);
  expect(rowTitles().map(([title]) => title)).toContain("Bug 00");
});

it("bounds rows per band and records per cell", async () => {
  const extra = BAND_ROWS + 5;
  const data = { ...RECORDS_DATA, Bug: [...rowsOf(RECORDS_DATA.Bug), ...bugs(extra, "doing")] };

  await renderGroupView(loadTrace, { routes: withData(data) });
  await screen.findByRole("table");

  // Payments now links three fixture records and every generated bug.
  const work = within(row("Payments")).getAllByRole("cell")[0];
  expect(within(work).getAllByRole("button")).toHaveLength(CELL_RECORDS + 1);
  fireEvent.click(within(work).getByRole("button", { name: `+${extra + 3 - CELL_RECORDS} more` }));
  expect(within(work).getAllByRole("button")).toHaveLength(extra + 3);

  await chooseRows("Work items");
  expect(bands()[1]).toBe(`Doing${extra + 2}`);
  // Blocked 1, Doing capped, Backlog 1, Done 2.
  expect(rowTitles()).toHaveLength(1 + BAND_ROWS + 1 + 2);

  fireEvent.click(screen.getByRole("button", { name: "Show 7 more" }));
  expect(rowTitles()).toHaveLength(1 + extra + 2 + 1 + 2);

  expect(screen.queryByRole("button", { name: /^Show \d+ more/ })).toBeNull();
});

it("collapses columns for members without records and outside links no row uses", async () => {
  const data = {
    ...RECORDS_DATA,
    Checklist: [],
    Story: changed("Story", (story) => ({ ...story, owner: null })),
  };

  await renderGroupView(loadTrace, { routes: withData(data) });
  await chooseRows("Work items");

  expect(screen.getByRole("button", { name: "Checklists 0, none recorded" })).toBeVisible();
  expect(screen.getByRole("button", { name: "Owner 0, no row links one" })).toBeVisible();

  const cells = within(row("Checkout redesign")).getAllByRole("cell");
  expect(cells.map((cell) => cell.textContent)).toEqual([
    expect.stringContaining("Payments"),
    expect.stringContaining("Fall release"),
    "",
    "",
  ]);
});

it("lights every occurrence of a record while it is pointed at or focused", async () => {
  await renderGroupView(loadTrace);
  await chooseRows("Work items");

  const occurrences = screen.getAllByRole("button", { name: "Payments" });
  const platform = screen.getByRole("button", { name: "Platform" });

  expect(occurrences).toHaveLength(3);

  fireEvent.pointerEnter(occurrences[1]);

  for (const chip of occurrences) expect(chip).toHaveAttribute("data-highlighted", "true");
  expect(platform).not.toHaveAttribute("data-highlighted");

  fireEvent.pointerLeave(occurrences[1]);
  fireEvent.focus(platform);
  expect(platform).toHaveAttribute("data-highlighted", "true");
  expect(occurrences[0]).not.toHaveAttribute("data-highlighted");
});

it("names members the rows never reach", async () => {
  const release = {
    ...TYPE_DOCS.Release,
    fields: TYPE_DOCS.Release.fields.filter((field) => field.name !== "works"),
  };

  const routes = groupRoutes({
    "GET /api/v1/ontology/types/Area": () =>
      jsonReply({ type: { ...TYPE_DOCS.Area, parentField: undefined }, count: 0 }),
    "GET /api/v1/ontology/types/Release": () => jsonReply({ type: release, count: 0 }),
    "POST /api/v1/graphql": () =>
      jsonReply({
        data: {
          ...RECORDS_DATA,
          Story: changed("Story", (story) => ({ ...story, area: null })),
          Bug: changed("Bug", (bug) => ({ ...bug, area: null })),
        },
      }),
  });

  await renderGroupView(loadTrace, { routes });
  await screen.findByRole("table");

  expect(screen.getByRole("radio", { name: /^Releases/ })).toBeChecked();
  expect(screen.getByText("Not linked to releases: work items.")).toBeVisible();
});

it("explains why a group without links has no Trace", async () => {
  await renderGroupView(loadTrace, { group: "Library" });

  expect(
    await screen.findByText(
      "Trace needs member types that link to each other, and Library has none.",
    ),
  ).toBeVisible();
  expect(screen.queryByRole("table")).toBeNull();
});

it("opens records, outside notes, and collections through the workspace", async () => {
  const { posted } = await renderGroupView(loadTrace, { embedded: true });

  await chooseRows("Work items");
  fireEvent.click(screen.getByRole("button", { name: "Checkout redesign" }));
  fireEvent.click(within(row("Checkout redesign")).getByRole("button", { name: "Payments" }));
  fireEvent.click(within(row("Checkout redesign")).getByRole("button", { name: "Ada Lovelace" }));
  fireEvent.click(within(columnHeader("Work items")).getByRole("button", { name: "Work items" }));
  fireEvent.click(within(columnHeader("Areas")).getByRole("button", { name: /^Areas/ }));
  fireEvent.click(within(columnHeader("Owner")).getByRole("button", { name: "Owner" }));

  expect(posted.filter((message) => message.type.startsWith("rhizome:open-"))).toEqual([
    {
      type: OPEN_NODE_MESSAGE,
      ref: { notePath: PATHS.checkout, kind: "NOTE", typeName: "Story" },
    },
    { type: OPEN_NODE_MESSAGE, ref: { notePath: PATHS.payments, kind: "NOTE", typeName: "Area" } },
    { type: OPEN_NOTE_MESSAGE, path: PATHS.ada, beside: false },
    { type: OPEN_COLLECTION_MESSAGE, name: "Work" },
    { type: OPEN_COLLECTION_MESSAGE, name: "Area" },
    { type: OPEN_COLLECTION_MESSAGE, name: "Person" },
  ]);
});
