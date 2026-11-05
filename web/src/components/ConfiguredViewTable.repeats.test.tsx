import { screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { renderWithQueryClient as render } from "../test/renderWithQueryClient";
import { ConfiguredViewTable } from "./ConfiguredViewTable";
import { execution, firstRow, rendererProps } from "./ConfiguredView.testFixtures";

function table(sorted: boolean, { totalCount = 3, status = "active", groupField = "done" } = {}) {
  const rows = ["a", "b", "c"].map((id, index) => ({
    ...firstRow,
    ref: { notePath: `${id}.md`, kind: "NOTE" },
    path: `${id}.md`,
    title: id,
    fields: {
      due: index < 2 ? "2000-01-01" : "",
      done: false,
      status: index === 0 ? status : "active",
    },
  }));

  const result = {
    ...execution(rows),
    columns: [
      { field: "title", label: "Title" },
      { field: "due", label: "Due" },
      { field: "tags", label: "Tags" },
    ],
    capabilities: [
      { key: "due", valueKind: "date", sortable: true, groupable: false, filterOps: [] },
      {
        key: "tags",
        valueKind: "enum",
        sortable: false,
        groupable: true,
        filterOps: [],
        edit: { kind: "enum", operation: "setField", field: "tags", list: true },
      },
      {
        key: "status",
        valueKind: "enum",
        sortable: true,
        groupable: true,
        filterOps: [],
        enumValues: [
          { value: "active", stage: "active" as const },
          { value: "complete", stage: "done" as const },
        ],
      },
    ],
    groups: [
      {
        field: groupField,
        key: "open",
        value: "false",
        label: "Open",
        count: 3,
        totalCount,
        depth: 0,
        rowStart: 0,
        rowEnd: 3,
      },
    ],
  };

  return (
    <ConfiguredViewTable
      {...rendererProps({
        execution: result,
        state: { sort: sorted ? [{ field: "due", direction: "asc" }] : [] },
      })}
    />
  );
}

describe("sorted table repetitions", () => {
  it("dims repeated sorted values while retaining text and each overdue marker", () => {
    const { container } = render(table(true));
    const repeats = container.querySelectorAll<HTMLTableCellElement>("td.is-repeat");
    expect(repeats).toHaveLength(1);
    expect(repeats[0]).toHaveTextContent("Jan 1, 2000");
    expect(within(repeats[0]).getByText("Overdue")).toBeVisible();
    expect(screen.getByRole("button", { name: "Open 3 · 2 overdue" })).toBeVisible();
    expect(screen.getAllByText("Overdue")).toHaveLength(2);
  });

  it("leaves the overdue count off a group whose rows are not all loaded", () => {
    render(table(true, { totalCount: 40 }));
    expect(screen.getByRole("button", { name: "Open 40" })).toBeVisible();
  });

  it("does not mark a past due date overdue once the record's lifecycle is done", () => {
    render(table(true, { status: "complete" }));
    expect(screen.getByRole("button", { name: "Open 3 · 1 overdue" })).toBeVisible();
    expect(screen.getAllByText("Overdue")).toHaveLength(1);
  });

  it("keeps a grouped date column, since a month group holds different days", () => {
    render(table(true, { groupField: "due" }));
    expect(screen.getByRole("columnheader", { name: /Due/ })).toBeVisible();
  });

  it("keeps a grouped list column, since a row holds members beyond its group", () => {
    render(table(true, { groupField: "tags" }));
    expect(screen.getByRole("columnheader", { name: /Tags/ })).toBeVisible();
  });

  it("keeps equal values at normal emphasis when their column is not sorted", () => {
    const { container } = render(table(false));
    expect(container.querySelectorAll<HTMLTableCellElement>("td.is-repeat")).toHaveLength(0);
  });
});
