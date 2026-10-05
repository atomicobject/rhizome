import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type {
  OntologyEditSessionResponse,
  ViewCatalogEntry,
  ViewExecuteRequest,
  ViewExecuteResponse,
  ViewTableRow,
} from "../api/types";
import { ConfiguredView } from "./ConfiguredView";

const rows: ViewTableRow[] = [
  {
    ref: { notePath: "notes/alpha.md", kind: "NOTE" },
    path: "notes/alpha.md",
    title: "Alpha",
  },
  {
    ref: { notePath: "notes/bravo.md", kind: "NOTE" },
    path: "notes/bravo.md",
    title: "Bravo",
  },
];

const view: ViewCatalogEntry = {
  id: "work.items",
  name: "Work items",
  source: { kind: "ontology_type", type: "WorkItem" },
  mount: { kind: "type", type: "WorkItem" },
  defaults: { variant: "table" },
  variants: {
    table: { columns: [{ field: "title", label: "Title" }] },
    kanban: { columnField: "status" },
    card: { title: "title" },
  },
  availableVariants: ["table", "kanban", "card"],
  definition: {
    apiVersion: "rhizome.view.v1",
    id: "work.items",
    name: "Work items",
    source: { kind: "ontology_type", type: "WorkItem" },
    mount: { kind: "type", type: "WorkItem" },
    variants: {
      table: { columns: [{ field: "title", label: "Title" }] },
      kanban: { columnField: "status" },
      card: { title: "title" },
    },
    filterPresets: [
      { id: "active", label: "Active", filters: [{ field: "status", op: "eq", value: "active" }] },
    ],
  },
};

function response(overrides: Partial<ViewExecuteResponse> = {}): ViewExecuteResponse {
  return {
    view,
    variant: "table",
    // SAFETY: the generated intersection rejects valid request-shaped literals.
    state: {} as ViewExecuteResponse["state"],
    capabilities: [
      {
        key: "title",
        label: "Title",
        valueKind: "text",
        sortable: true,
        groupable: false,
        filterOps: ["contains"],
      },
    ],
    columns: [{ field: "title", label: "Title" }],
    rows,
    groups: [],
    pageInfo: { total: 2, offset: 0, first: 25, returned: 2, hasMore: false },
    definitionFingerprint: "definition",
    sourceFingerprint: "source",
    executionFingerprint: "execution",
    ...overrides,
  };
}

function renderConfiguredView({
  catalogView = view,
  state = {},
  onStateChange = vi.fn(),
  onOpenRow = vi.fn(),
  editSession,
  execution = response(),
}: {
  catalogView?: ViewCatalogEntry;
  state?: ViewExecuteRequest;
  onStateChange?: (state: ViewExecuteRequest) => void;
  onOpenRow?: (row: ViewTableRow) => void;
  editSession?: OntologyEditSessionResponse;
  execution?: ViewExecuteResponse;
} = {}) {
  return render(
    <ConfiguredView
      view={catalogView}
      execution={execution}
      loading={false}
      error={null}
      state={state}
      onStateChange={onStateChange}
      onRefresh={() => {}}
      onOpenRow={onOpenRow}
      editSession={editSession}
    />,
  );
}

afterEach(() => {
  window.localStorage.clear();
});

describe("ConfiguredView shell interactions", () => {
  it("sets and clears a filter preset", () => {
    const onStateChange = vi.fn();
    const rendered = renderConfiguredView({ onStateChange });
    fireEvent.click(screen.getByRole("button", { name: "Active" }));
    expect(onStateChange).toHaveBeenLastCalledWith({ filterPreset: "active", page: { offset: 0 } });

    rendered.rerender(
      <ConfiguredView
        view={view}
        execution={response()}
        loading={false}
        error={null}
        state={{ filterPreset: "active" }}
        onStateChange={onStateChange}
        onRefresh={() => {}}
        onOpenRow={() => {}}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Active" }));
    expect(onStateChange).toHaveBeenLastCalledWith({ filterPreset: "", page: { offset: 0 } });
  });

  it("opens rows from the title and row keys while arrow keys move selection", () => {
    const onOpenRow = vi.fn();
    renderConfiguredView({ onOpenRow });
    fireEvent.keyDown(screen.getByRole("button", { name: "Open Alpha" }), { key: "Enter" });
    expect(onOpenRow).toHaveBeenCalledWith(rows[0], "activate");
    fireEvent.click(screen.getByRole("button", { name: "Open Alpha" }), { metaKey: true });
    expect(onOpenRow).toHaveBeenLastCalledWith(rows[0], "beside");

    const tableRows = screen.getAllByRole("row").filter((row) => row.hasAttribute("data-row-key"));
    fireEvent.focus(tableRows[0]);
    fireEvent.keyDown(tableRows[0], { key: "ArrowDown" });
    expect(tableRows[1]).toHaveFocus();
    fireEvent.keyDown(tableRows[1], { key: "Enter" });
    expect(onOpenRow).toHaveBeenLastCalledWith(rows[1]);
  });

  it("uses a semantic title column when an identifier comes first", () => {
    const onOpenRow = vi.fn();

    const titledRows = rows.map((row, index) => ({
      ...row,
      fields: { id: `WORK-${index + 1}`, name: `${row.title} work` },
    }));

    renderConfiguredView({
      onOpenRow,
      execution: response({
        rows: titledRows,
        columns: [
          { field: "id", label: "ID" },
          { field: "name", label: "Work item" },
        ],
        capabilities: [
          {
            key: "id",
            semanticRole: "identifier",
            sortable: true,
            groupable: false,
          },
          {
            key: "name",
            semanticRole: "title",
            sortable: true,
            groupable: false,
          },
        ],
      }),
    });

    expect(screen.queryByRole("button", { name: "Open WORK-1" })).toBeNull();
    const title = screen.getByRole("button", { name: "Open Alpha work" });
    fireEvent.click(title);
    fireEvent.doubleClick(title);
    expect(onOpenRow).toHaveBeenCalledWith(titledRows[0], "activate");
  });

  it("marks the active sort and rows with staged changes", () => {
    renderConfiguredView({
      state: { sort: [{ field: "title", direction: "desc" }] },
      editSession: {
        sessionId: "session",
        status: "dirty",
        hasUncommittedChanges: true,
        createdAt: "2026-09-19T00:00:00Z",
        updatedAt: "2026-09-19T00:00:01Z",
        ops: [{ kind: "setField", path: "notes/alpha.md", field: "title", value: "A" }],
      },
    });

    expect(screen.getByRole("columnheader", { name: "Title" })).toHaveAttribute(
      "aria-sort",
      "descending",
    );
    expect(screen.getByLabelText("Staged changes")).toBeVisible();
    expect(screen.getByText("1 staged change")).toBeVisible();
  });
});
