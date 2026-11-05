import { render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { ConfiguredViewTableHeader } from "./ConfiguredViewTableHeader";

it("numbers visible sort keys consecutively and names the complete order", () => {
  const props = {
    capabilities: [
      { key: "status", label: "Status", sortable: true, groupable: true },
      { key: "due", label: "Due", sortable: true, groupable: true },
      { key: "source", label: "Source", sortable: true, groupable: true },
    ],
    sort: [
      { field: "status", direction: "asc" as const },
      { field: "due", direction: "asc" as const },
      { field: "source", direction: "asc" as const },
    ],
    onSort: vi.fn(),
    primaryColumnIndex: 0,
    allChecked: false,
    onToggleAll: vi.fn(),
  };

  const { container, rerender } = render(
    <table>
      <ConfiguredViewTableHeader {...props} columns={[{ field: "due" }, { field: "source" }]} />
    </table>,
  );

  expect(
    [...container.querySelectorAll(".configured-view__sort-rank")].map((el) => el.textContent),
  ).toEqual(["1", "2"]);
  expect(screen.getAllByTitle("Sorted by Status, then Due, then Source")).toHaveLength(2);

  rerender(
    <table>
      <ConfiguredViewTableHeader {...props} columns={[{ field: "due" }]} />
    </table>,
  );
  expect(container.querySelector(".configured-view__sort-rank")).toBeNull();
});
