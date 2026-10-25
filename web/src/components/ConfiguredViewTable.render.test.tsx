// The probe must load before react-dom.
import { renderCount, resetRenderCounts } from "../test/renderProbe";
import { fireEvent, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type {
  OntologyEditSessionResponse,
  ViewCatalogEntry,
  ViewExecuteResponse,
  ViewFieldCapability,
  ViewTableRow,
} from "../api/types";
import { renderWithQueryClient as render } from "../test/renderWithQueryClient";
import { ConfiguredTableCellContent } from "./ConfiguredTableCell";
import { ConfiguredViewTable } from "./ConfiguredViewTable";

const rows: ViewTableRow[] = Array.from({ length: 200 }, (_, index) => ({
  ref: { notePath: `notes/n${index}.md`, kind: "NOTE" },
  path: `notes/n${index}.md`,
  title: `Note ${index}`,
  fields: { status: "open" },
}));

const columns = [
  { field: "title", label: "Title" },
  { field: "status", label: "Status" },
];

const capabilities: ViewFieldCapability[] = columns.map(({ field, label }) => ({
  key: field,
  label,
  valueKind: "text",
  sortable: true,
  groupable: false,
  filterOps: ["contains"],
}));

const view: ViewCatalogEntry = {
  id: "notes.table",
  name: "Notes",
  source: { kind: "ontology_type", type: "Note" },
  mount: { kind: "type", type: "Note" },
  defaults: { variant: "table" },
  variants: { table: { columns } },
  definition: {
    apiVersion: "rhizome.view.v1",
    id: "notes.table",
    name: "Notes",
    source: { kind: "ontology_type", type: "Note" },
    mount: { kind: "type", type: "Note" },
    variants: { table: { columns } },
  },
};

const execution: ViewExecuteResponse = {
  view,
  variant: "table",
  // SAFETY: the generated intersection rejects valid request-shaped literals.
  state: {} as ViewExecuteResponse["state"],
  capabilities,
  columns,
  rows,
  groups: [],
  pageInfo: { total: rows.length, offset: 0, first: 200, returned: rows.length, hasMore: false },
  definitionFingerprint: "definition",
  sourceFingerprint: "source",
  executionFingerprint: "execution",
};

const staged: OntologyEditSessionResponse = {
  sessionId: "s1",
  status: "dirty",
  hasUncommittedChanges: true,
  createdAt: "2026-10-25T00:00:00Z",
  updatedAt: "2026-10-25T00:00:01Z",
  ops: [{ id: "op1", kind: "setField", path: "notes/n7.md", field: "status", value: "done" }],
};

const onOpenRow = vi.fn();

const onSort = vi.fn();

const onCollapsedGroupCountChange = vi.fn();

function table(editSession: OntologyEditSessionResponse | null) {
  return (
    <ConfiguredViewTable
      execution={execution}
      rows={rows}
      columns={columns}
      capabilities={capabilities}
      state={execution.state}
      viewID="notes.table"
      loading={false}
      editSession={editSession}
      onOpenRow={onOpenRow}
      onSort={onSort}
      onCollapsedGroupCountChange={onCollapsedGroupCountChange}
    />
  );
}

const cellRenders = () => renderCount(ConfiguredTableCellContent);

describe("ConfiguredViewTable render boundaries", () => {
  it("re-renders only the rows whose selection changes, and marks staged rows", () => {
    const view = render(table(staged));

    expect(cellRenders()).toBe(rows.length * columns.length);
    expect(document.querySelectorAll("tr.is-dirty")).toHaveLength(1);
    expect(document.querySelector('tr[data-row-key="notes/n7.md:"]')).toHaveClass("is-dirty");

    resetRenderCounts();
    fireEvent.click(screen.getByText("Note 5"));
    const selectRenders = cellRenders();

    resetRenderCounts();
    view.rerender(table(staged));
    const parentRenders = cellRenders();

    console.info(
      `[render-count] 200-row table: select renders=${selectRenders} cells, parent re-render=${parentRenders} cells`,
    );

    // The newly selected row, and the first row that loses the single Tab stop.
    expect(selectRenders).toBe(2 * columns.length);
    expect(parentRenders).toBe(0);
    expect(document.querySelector('tr[data-row-key="notes/n5.md:"]')).toHaveClass("is-selected");
  });
});
