import { screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { renderWithQueryClient as render } from "../test/renderWithQueryClient";
import { ConfiguredViewTable } from "./ConfiguredViewTable";
import { execution, firstRow, rendererProps } from "./ConfiguredView.testFixtures";

function table(sorted: boolean) {
  const rows = ["a", "b", "c"].map((id, index) => ({
    ...firstRow,
    ref: { notePath: `${id}.md`, kind: "NOTE" },
    path: `${id}.md`,
    title: id,
    fields: { due: index < 2 ? "2000-01-01" : "", done: false },
  }));

  const result = {
    ...execution(rows),
    columns: [
      { field: "title", label: "Title" },
      { field: "due", label: "Due" },
    ],
    capabilities: [
      { key: "due", valueKind: "date", sortable: true, groupable: false, filterOps: [] },
    ],
    groups: [
      {
        field: "done",
        key: "open",
        value: "false",
        label: "Open",
        count: 3,
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

  it("keeps equal values at normal emphasis when their column is not sorted", () => {
    const { container } = render(table(false));
    expect(container.querySelectorAll<HTMLTableCellElement>("td.is-repeat")).toHaveLength(0);
  });
});
