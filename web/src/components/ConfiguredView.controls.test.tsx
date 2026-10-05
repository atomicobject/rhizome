import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it, onTestFinished, vi } from "vitest";

import type { ViewCatalogEntry, ViewExecuteResponse } from "../api/types";
import { clearPreferenceStores } from "../viewPreferences/store";
import { ConfiguredView } from "./ConfiguredView";
import {
  baseView,
  execution,
  executionWithMoreRows,
  filterFieldOptionLabels,
  firstRow,
  boardExecution,
  renderView,
} from "./ConfiguredView.testFixtures";

describe("ConfiguredView states and controls", () => {
  it("renders loading, error, and empty states", () => {
    const { rerender } = render(
      <ConfiguredView
        view={baseView}
        execution={null}
        loading={true}
        error={null}
        state={{}}
        onStateChange={() => {}}
        onRefresh={() => {}}
        onOpenRow={() => {}}
      />,
    );

    expect(screen.getByText("Loading view…")).toBeVisible();

    rerender(
      <ConfiguredView
        view={baseView}
        execution={execution()}
        loading={true}
        error={null}
        state={{}}
        onStateChange={() => {}}
        onRefresh={() => {}}
        onOpenRow={() => {}}
      />,
    );
    expect(screen.getByText("Refreshing view…")).toBeVisible();
    expect(screen.getByText("Alpha Spec")).toBeVisible();
    expect(screen.getByText("Refreshing view…")).toHaveClass("configured-view__loading-overlay");

    rerender(
      <ConfiguredView
        view={baseView}
        execution={null}
        loading={false}
        error="View failed"
        state={{}}
        onStateChange={() => {}}
        onRefresh={() => {}}
        onOpenRow={() => {}}
      />,
    );
    expect(screen.getByRole("alert")).toHaveTextContent("View failed");

    rerender(
      <ConfiguredView
        view={baseView}
        execution={null}
        loading={false}
        error={{
          message: "View failed with details",
          status: 400,
          code: "invalid_request",
          details: { field: "done", op: "contains" },
        }}
        state={{}}
        onStateChange={() => {}}
        onRefresh={() => {}}
        onOpenRow={() => {}}
      />,
    );
    expect(screen.getByRole("alert")).toHaveTextContent("View failed with details");
    fireEvent.click(screen.getByText("Details"));
    expect(screen.getByText(/invalid_request/)).toBeVisible();
    expect(screen.getByText(/"field": "done"/)).toBeVisible();

    rerender(
      <ConfiguredView
        view={baseView}
        execution={execution([])}
        loading={false}
        error={null}
        state={{}}
        onStateChange={() => {}}
        onRefresh={() => {}}
        onOpenRow={() => {}}
      />,
    );
    expect(screen.getByText("No rows match this view.")).toBeVisible();
  });
  it("renders configured columns, group header rows, rows, and row identity", () => {
    const onOpenRow = vi.fn();
    renderView({ onOpenRow });

    expect(screen.getByRole("heading", { name: "Spec Table" })).toBeVisible();
    expect(screen.getByRole("button", { name: "Active 1" })).toHaveAttribute(
      "aria-expanded",
      "true",
    );
    expect(screen.getByRole("button", { name: "Draft 1" })).toHaveAttribute(
      "aria-expanded",
      "true",
    );

    const headers = screen.getAllByRole("columnheader").map((header) => header.textContent?.trim());
    // The first column holds the row selection checkboxes.
    expect(headers).toEqual(["", "Title", "Status", "ID", "Updated"]);

    const first = screen.getByRole("button", { name: /Open Alpha Spec/ });
    const firstTableRow = first.closest("tr");
    expect(first).toHaveTextContent("Alpha Spec");
    expect(firstTableRow).toHaveTextContent("Alpha Spec");
    expect(firstTableRow).toHaveTextContent("active");
    expect(firstTableRow).toHaveTextContent("SPEC-0001");
    expect(firstTableRow).not.toHaveTextContent("docs/specs/alpha.md");

    // A click selects the row; Enter opens it.
    fireEvent.click(first);
    expect(onOpenRow).not.toHaveBeenCalled();
    fireEvent.keyDown(first, { key: "Enter" });
    expect(onOpenRow).toHaveBeenCalledWith(firstRow, "activate");
  });
  it("does not expose a sort action for an explicitly unsortable column", () => {
    const result = execution();
    result.capabilities = result.capabilities?.map((capability) =>
      capability.key === "title" ? { ...capability, sortable: false } : capability,
    );

    renderView({ execution: result });

    expect(screen.queryByRole("button", { name: /Sort Title/ })).not.toBeInTheDocument();
    expect(screen.getByRole("columnheader", { name: "Title" })).toHaveTextContent("Title");
  });
  it("shows execution warnings once in every presentation, including an unavailable board", () => {
    const variants = ["table", "card", "kanban"] as const;

    const variantsConfig = {
      table: baseView.variants.table,
      card: { title: "title" },
      kanban: { columnField: "frontmatter.spec-status" },
    };

    const view = {
      ...baseView,
      variants: variantsConfig,
      availableVariants: [...variants],
      definition: { ...baseView.definition, variants: variantsConfig },
    };

    for (const [index, variant] of [...variants, "kanban" as const].entries()) {
      const result = boardExecution();
      result.view = view;
      result.variant = variant;
      result.warnings = [{ code: "partial_results", message: "Some rows were omitted." }];

      if (index === 3) result.board = undefined;

      const rendered = renderView({
        view,
        execution: result,
        state: { variant, page: { offset: 0, first: 25 } },
      });

      expect(screen.getAllByText("Some rows were omitted.")).toHaveLength(1);
      rendered.unmount();
    }
  });
  it("collapses group rows and hides nested descendants", () => {
    renderView({
      execution: {
        ...execution(),
        groups: [
          {
            field: "frontmatter.spec-status",
            key: "frontmatter.spec-status=active",
            label: "Active",
            value: "active",
            count: 1,
            depth: 0,
            rowStart: 0,
            rowEnd: 1,
            children: [
              {
                field: "frontmatter.id",
                key: "frontmatter.spec-status=active/frontmatter.id=SPEC-0001",
                label: "SPEC-0001",
                value: "SPEC-0001",
                count: 1,
                depth: 1,
                rowStart: 0,
                rowEnd: 1,
              },
            ],
          },
          {
            field: "frontmatter.spec-status",
            key: "frontmatter.spec-status=draft",
            label: "Draft",
            value: "draft",
            count: 1,
            depth: 0,
            rowStart: 1,
            rowEnd: 2,
            collapsedByDefault: true,
            children: [
              {
                field: "frontmatter.id",
                key: "frontmatter.spec-status=draft/frontmatter.id=SPEC-0002",
                label: "SPEC-0002",
                value: "SPEC-0002",
                count: 1,
                depth: 1,
                rowStart: 1,
                rowEnd: 2,
              },
            ],
          },
        ],
      },
    });

    expect(screen.getByRole("button", { name: "Active 1" })).toHaveAttribute(
      "aria-expanded",
      "true",
    );
    expect(screen.getByRole("button", { name: "SPEC-0001 1" })).toBeVisible();
    expect(screen.getByRole("button", { name: "Draft 1" })).toHaveAttribute(
      "aria-expanded",
      "false",
    );
    expect(screen.queryByRole("button", { name: "SPEC-0002 1" })).toBeNull();
    expect(screen.queryByRole("button", { name: /Open Bravo Spec/ })).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Active 1" }));
    expect(screen.getByRole("button", { name: "Active 1" })).toHaveAttribute(
      "aria-expanded",
      "false",
    );
    expect(screen.queryByRole("button", { name: "SPEC-0001 1" })).toBeNull();
    expect(screen.queryByRole("button", { name: /Open Alpha Spec/ })).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Draft 1" }));
    expect(screen.getByRole("button", { name: "Draft 1" })).toHaveAttribute(
      "aria-expanded",
      "true",
    );
    expect(screen.getByRole("button", { name: "SPEC-0002 1" })).toBeVisible();
    expect(screen.getByRole("button", { name: /Open Bravo Spec/ })).toBeVisible();
  });
  it("resets column visibility to the new view's columns when the view changes", () => {
    const viewFor = (id: string, fields: string[]): ViewCatalogEntry => ({
      ...baseView,
      id,
      name: id,
      variants: { table: { columns: fields.map((field) => ({ field, label: field })) } },
    });

    const executionFor = (view: ViewCatalogEntry): ViewExecuteResponse => ({
      ...execution([firstRow]),
      view,
      columns: view.variants.table?.columns ?? [],
    });
    // The views share one column: the old behaviour narrowed the new view down
    // to that intersection instead of showing its configured columns.

    const viewA = viewFor("view.a", ["title", "alpha"]);
    const viewB = viewFor("view.b", ["title", "charlie", "delta"]);

    const columnHeaderLabels = () =>
      // The leading selection column has no label.
      screen.getAllByRole("columnheader").flatMap((header) => header.textContent || []);

    const rendered = render(
      <ConfiguredView
        view={viewA}
        execution={executionFor(viewA)}
        loading={false}
        error={null}
        state={{ page: { offset: 0, first: 25 } }}
        onStateChange={() => {}}
        onRefresh={() => {}}
        onOpenRow={() => {}}
      />,
    );

    expect(columnHeaderLabels()).toEqual(["title", "alpha"]);

    rendered.rerender(
      <ConfiguredView
        view={viewB}
        execution={executionFor(viewB)}
        loading={false}
        error={null}
        state={{ page: { offset: 0, first: 25 } }}
        onStateChange={() => {}}
        onRefresh={() => {}}
        onOpenRow={() => {}}
      />,
    );

    expect(columnHeaderLabels()).toEqual(["title", "charlie", "delta"]);
  });
  it("orders all available columns by importance and keeps detail fields hidden by default", () => {
    const baseExecution = execution([
      { ...firstRow, fields: { ...firstRow.fields, detailNote: "More context" } },
    ]);

    const capabilities = (baseExecution.capabilities ?? []).map((capability) => ({
      ...capability,
      importance:
        capability.key === "title" || capability.key === "frontmatter.id"
          ? ("KEY" as const)
          : capability.key === "updatedAt"
            ? ("DETAIL" as const)
            : ("NORMAL" as const),
    }));

    capabilities.push({
      key: "detailNote",
      label: "Detail note",
      canonicalField: "detailNote",
      importance: "DETAIL",
      sortable: true,
      groupable: false,
    });

    renderView({ execution: { ...baseExecution, capabilities } });

    expect(screen.queryByRole("columnheader", { name: "Detail note" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Columns" }));
    const columns = screen.getByRole("group", { name: "Visible columns" });
    expect(
      within(columns)
        .getAllByRole("checkbox")
        .map((checkbox) => checkbox.parentElement?.textContent),
    ).toEqual(["Title", "ID", "Status", "Updated", "Detail note"]);

    fireEvent.click(within(columns).getByLabelText("Detail note"));
    expect(screen.getByRole("columnheader", { name: "Detail note" })).toBeVisible();
    expect(screen.getByText("More context")).toBeVisible();
  });
  it("remembers the column choice for the view after a remount", async () => {
    onTestFinished(clearPreferenceStores);
    const rendered = renderView({ vaultKey: "vault" });
    fireEvent.click(await screen.findByRole("button", { name: "Columns" }));
    fireEvent.click(
      within(screen.getByRole("group", { name: "Visible columns" })).getByLabelText("Status"),
    );
    expect(screen.queryByRole("columnheader", { name: "Status" })).toBeNull();

    rendered.unmount();
    renderView({ vaultKey: "vault" });

    await waitFor(() => expect(screen.queryByRole("columnheader", { name: "Status" })).toBeNull());
    expect(screen.getByRole("columnheader", { name: "Title" })).toBeVisible();
  });
  it("offers boolean fields as value-pick filters", async () => {
    const onStateChange = vi.fn();
    renderView({
      execution: {
        ...executionWithMoreRows(),
        capabilities: [
          {
            key: "done",
            label: "Done",
            valueKind: "bool",
            sortable: false,
            groupable: true,
            filterOps: ["eq", "exists"],
            values: ["false", "true"],
          },
          ...(executionWithMoreRows().capabilities ?? []),
        ],
      },
      state: { page: { offset: 25, first: 25 } },
      onStateChange,
    });

    fireEvent.click(screen.getByRole("button", { name: "Add filter" }));
    fireEvent.change(screen.getByLabelText("Field"), {
      target: { value: "done" },
    });
    fireEvent.click(screen.getByRole("button", { name: "true" }));

    await waitFor(() => {
      expect(onStateChange).toHaveBeenLastCalledWith({
        page: { offset: 0, first: 25 },
        filters: [{ field: "done", op: "eq", value: "true" }],
      });
    });
  });
  it("offers schema enum values, by label, as filter options instead of observed values", async () => {
    const onStateChange = vi.fn();
    renderView({
      execution: {
        ...executionWithMoreRows(),
        capabilities: [
          {
            key: "specStatus",
            label: "Spec Status",
            valueKind: "enum",
            sortable: true,
            groupable: true,
            filterOps: ["eq", "in", "exists"],
            values: ["active"],
            enumValues: [
              { value: "active", label: "Active" },
              { value: "archived", label: "Archived" },
            ],
          },
        ],
      },
      state: { page: { offset: 25, first: 25 } },
      onStateChange,
    });

    fireEvent.click(screen.getByRole("button", { name: "Add filter" }));
    fireEvent.click(screen.getByRole("button", { name: "Archived" }));

    await waitFor(() => {
      expect(onStateChange).toHaveBeenLastCalledWith({
        page: { offset: 0, first: 25 },
        filters: [{ field: "specStatus", op: "in", values: ["archived"] }],
      });
    });
  });
  it("uses typed date and number inputs for comparable filters", () => {
    renderView({
      execution: {
        ...executionWithMoreRows(),
        capabilities: [
          {
            key: "due",
            label: "Due",
            valueKind: "date",
            sortable: true,
            groupable: true,
            filterOps: ["eq", "gte", "lte"],
          },
          {
            key: "estimate",
            label: "Estimate",
            valueKind: "int",
            sortable: true,
            groupable: true,
            filterOps: ["eq", "gte", "lte"],
          },
        ],
      },
    });

    fireEvent.click(screen.getByRole("button", { name: "Add filter" }));
    expect(screen.getByLabelText("Value")).toHaveAttribute("type", "date");
    fireEvent.change(screen.getByLabelText("Field"), {
      target: { value: "estimate" },
    });
    expect(screen.getByLabelText("Value")).toHaveAttribute("type", "number");
  });
  it("updates search, grouping, filters, sorting, pagination, and visible columns", async () => {
    const onStateChange = vi.fn();
    const onRefresh = vi.fn();
    renderView({
      execution: executionWithMoreRows(),
      state: { page: { offset: 25, first: 25 } },
      onStateChange,
      onRefresh,
    });

    fireEvent.change(screen.getByLabelText("Search view"), {
      target: { value: "ready" },
    });
    expect(onStateChange).toHaveBeenLastCalledWith({
      page: { offset: 0, first: 25 },
      search: "ready",
    });

    fireEvent.change(screen.getByLabelText("Group by"), {
      target: { value: "specStatus" },
    });
    expect(onStateChange).toHaveBeenLastCalledWith({
      page: { offset: 0, first: 25 },
      group: { field: "specStatus" },
    });

    fireEvent.click(screen.getByRole("button", { name: "Add filter" }));
    const fieldOptions = filterFieldOptionLabels();
    expect(fieldOptions.filter((option) => option === "Id")).toHaveLength(1);
    expect(fieldOptions).toContain("Spec status");
    expect(fieldOptions).not.toContain("SpecStatus");
    fireEvent.click(screen.getByRole("button", { name: "active" }));
    await waitFor(() => {
      expect(onStateChange).toHaveBeenLastCalledWith({
        page: { offset: 0, first: 25 },
        filters: [
          {
            field: "specStatus",
            op: "in",
            values: ["active"],
          },
        ],
      });
    });

    fireEvent.click(screen.getByRole("button", { name: "Sort Title ascending" }));
    expect(onStateChange).toHaveBeenLastCalledWith({
      page: { offset: 0, first: 25 },
      sort: [{ field: "title", direction: "asc" }],
    });

    fireEvent.click(screen.getByRole("button", { name: "Refresh view" }));
    expect(onRefresh).toHaveBeenCalledTimes(1);

    fireEvent.click(screen.getByRole("button", { name: "Columns" }));
    const columns = screen.getByRole("group", { name: "Visible columns" });
    fireEvent.click(within(columns).getByLabelText("Status"));
    expect(screen.queryByRole("columnheader", { name: "Status" })).toBeNull();
  });
  it("sends in filters and explicit empty state when defaults are cleared", async () => {
    const onStateChange = vi.fn();

    const { rerender } = renderView({
      execution: executionWithMoreRows(),
      state: {
        page: { offset: 25, first: 25 },
        filters: [{ field: "frontmatter.spec-status", op: "eq", value: "active" }],
        group: { field: "frontmatter.spec-status" },
      },
      onStateChange,
    });

    fireEvent.click(screen.getByRole("button", { name: "Add filter" }));
    fireEvent.click(screen.getByRole("button", { name: "active" }));
    fireEvent.click(screen.getByRole("button", { name: "draft" }));
    await waitFor(() => {
      expect(onStateChange).toHaveBeenLastCalledWith({
        page: { offset: 0, first: 25 },
        filters: [
          { field: "frontmatter.spec-status", op: "eq", value: "active" },
          {
            field: "specStatus",
            op: "in",
            values: ["active", "draft"],
          },
        ],
        group: { field: "frontmatter.spec-status" },
      });
    });

    fireEvent.click(screen.getAllByRole("button", { name: "Remove" })[0]);
    fireEvent.click(screen.getAllByRole("button", { name: "Remove" })[0]);
    await waitFor(() => {
      expect(onStateChange).toHaveBeenLastCalledWith({
        page: { offset: 0, first: 25 },
        filters: [],
        group: { field: "frontmatter.spec-status" },
      });
    });

    fireEvent.change(screen.getByLabelText("Group by"), {
      target: { value: "" },
    });
    expect(onStateChange).toHaveBeenLastCalledWith({
      page: { offset: 0, first: 25 },
      filters: [{ field: "frontmatter.spec-status", op: "eq", value: "active" }],
      group: { field: "" },
    });

    fireEvent.click(screen.getByRole("button", { name: "Columns" }));
    const columns = screen.getByRole("group", { name: "Visible columns" });
    fireEvent.click(within(columns).getByLabelText("Status"));
    expect(screen.queryByRole("columnheader", { name: "Status" })).toBeNull();
    rerender(
      <ConfiguredView
        view={baseView}
        execution={executionWithMoreRows()}
        loading={false}
        error={null}
        state={{
          page: { offset: 25, first: 25 },
          filters: [{ field: "frontmatter.spec-status", op: "eq", value: "active" }],
        }}
        onStateChange={onStateChange}
        onRefresh={vi.fn()}
        onOpenRow={vi.fn()}
      />,
    );
    expect(screen.queryByRole("columnheader", { name: "Status" })).toBeNull();
  });
  it("fills a configured Issues column from validation counts, not the index flag", () => {
    const withIssues = execution([{ ...firstRow, hasIssues: false }]);
    withIssues.columns = [...(withIssues.columns ?? []), { field: "hasIssues", label: "Issues" }];

    renderView({
      execution: withIssues,
      issueCounts: new Map([["docs/specs/alpha.md", 2]]),
    });

    expect(
      screen.getByRole("button", { name: "Open 2 problems for docs/specs/alpha.md" }),
    ).toBeVisible();
    // The appended validation column would repeat it.
    expect(screen.getAllByRole("columnheader", { name: "Issues" })).toHaveLength(1);
  });
  it("labels boolean groups with the field name instead of true and false", () => {
    const grouped = execution();
    grouped.capabilities = [
      ...(grouped.capabilities ?? []),
      { key: "done", label: "Done", valueKind: "bool", sortable: true, groupable: true },
    ];
    grouped.groups = [
      {
        field: "done",
        key: "done=false",
        label: "false",
        value: "false",
        count: 1,
        depth: 0,
        rowStart: 0,
        rowEnd: 1,
      },
      {
        field: "done",
        key: "done=true",
        label: "true",
        value: "true",
        count: 1,
        depth: 0,
        rowStart: 1,
        rowEnd: 2,
      },
    ];

    renderView({ execution: grouped });

    expect(screen.getByRole("button", { name: "Done: no 1" })).toBeVisible();
    expect(screen.getByRole("button", { name: "Done: yes 1" })).toBeVisible();
  });
});
