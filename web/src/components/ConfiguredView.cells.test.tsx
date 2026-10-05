import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { ConfiguredView } from "./ConfiguredView";
import {
  baseView,
  execution,
  executionWithMoreRows,
  firstRow,
  openEnumEditor,
  renderView,
} from "./ConfiguredView.testFixtures";

describe("ConfiguredView cell editing", () => {
  it("keeps an empty draft filter row when the last selected value is cleared", async () => {
    const onStateChange = vi.fn();

    const { rerender } = renderView({
      execution: executionWithMoreRows(),
      state: { page: { offset: 25, first: 25 } },
      onStateChange,
    });

    fireEvent.click(screen.getByRole("button", { name: "Add filter" }));
    fireEvent.click(screen.getByRole("button", { name: "active" }));
    await waitFor(() => {
      expect(onStateChange).toHaveBeenLastCalledWith({
        page: { offset: 0, first: 25 },
        filters: [{ field: "specStatus", op: "in", values: ["active"] }],
      });
    });
    rerender(
      <ConfiguredView
        view={baseView}
        execution={executionWithMoreRows()}
        loading={false}
        error={null}
        state={{
          page: { offset: 0, first: 25 },
          filters: [{ field: "specStatus", op: "in", values: ["active"] }],
        }}
        onStateChange={onStateChange}
        onRefresh={vi.fn()}
        onOpenRow={vi.fn()}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "active" }));
    await waitFor(() => {
      expect(onStateChange).toHaveBeenLastCalledWith({
        page: { offset: 0, first: 25 },
        filters: [],
      });
    });

    rerender(
      <ConfiguredView
        view={baseView}
        execution={executionWithMoreRows()}
        loading={false}
        error={null}
        state={{ page: { offset: 0, first: 25 }, filters: [] }}
        onStateChange={onStateChange}
        onRefresh={vi.fn()}
        onOpenRow={vi.fn()}
      />,
    );
    expect(screen.getByLabelText("Field")).toBeVisible();
    expect(screen.getByRole("button", { name: "active" })).toHaveAttribute("aria-pressed", "false");
  });
  it("stages enum edits through view cells", () => {
    const onStageOps = vi.fn().mockResolvedValue(undefined);
    renderView({ onStageOps });

    fireEvent.change(openEnumEditor(), {
      target: { value: "archived" },
    });

    expect(onStageOps).toHaveBeenCalledWith([
      {
        id: "field:docs%2Fspecs%2Falpha.md:note:specStatus",
        kind: "setField",
        path: "docs/specs/alpha.md",
        field: "specStatus",
        value: "archived",
        fieldValue: { kind: "scalar", scalar: "archived" },
        expected: { field: { kind: "scalar", scalar: "active" } },
      },
    ]);
  });
  it("edits enum lists as removable values plus an add picker", () => {
    const onStageOps = vi.fn().mockResolvedValue(undefined);

    const listExecution = execution([
      {
        ...firstRow,
        fields: { frontmatter: { id: "SPEC-0001", "spec-status": ["archived", "active"] } },
      },
    ]);

    listExecution.capabilities = (listExecution.capabilities || []).map((capability) =>
      capability.key === "frontmatter.spec-status"
        ? {
            ...capability,
            valueKind: "list",
            edit: { ...capability.edit!, valueKind: "list", inputMode: "enum-list" },
          }
        : capability,
    );
    renderView({ execution: listExecution, onStageOps });

    fireEvent.click(screen.getAllByRole("button", { name: "Edit Status" })[0]);
    const add = screen.getByRole("combobox", { name: "Add Status" });
    // A plain pick adds to the list; it never replaces the existing values.
    fireEvent.change(add, { target: { value: "draft" } });

    expect(onStageOps).toHaveBeenLastCalledWith([
      expect.objectContaining({
        values: ["archived", "active", "draft"],
        fieldValue: { kind: "list", items: ["archived", "active", "draft"] },
      }),
    ]);

    // The row has not refetched yet, so the removal builds on the staged add.
    fireEvent.click(screen.getByRole("button", { name: "Remove Archived" }));

    expect(onStageOps).toHaveBeenLastCalledWith([
      expect.objectContaining({
        values: ["active", "draft"],
        fieldValue: { kind: "list", items: ["active", "draft"] },
      }),
    ]);
  });
  it("renders each read-only enum list value with its schema label and tone", () => {
    const result = execution([
      {
        ...firstRow,
        fields: { ...firstRow.fields, statuses: ["active", "blocked"] },
      },
    ]);

    result.groups = [];
    result.columns = [
      { field: "title", label: "Title" },
      { field: "statuses", label: "Statuses" },
    ];
    result.capabilities = [
      ...(result.capabilities ?? []),
      {
        key: "statuses",
        label: "Statuses",
        valueKind: "enum",
        sortable: true,
        groupable: true,
        enumValues: [
          { value: "active", label: "In progress", tone: "progress" },
          { value: "blocked", label: "Needs help", tone: "risk" },
        ],
      },
    ];

    renderView({ execution: result });

    expect(screen.getByText("In progress").closest(".status-mark")).toHaveClass(
      "status-mark--progress",
    );
    expect(screen.getByText("Needs help").closest(".status-mark")).toHaveClass("status-mark--risk");
  });
  it("represents an unset optional enum without selecting the first option", () => {
    const onStageOps = vi.fn().mockResolvedValue(undefined);

    const rowWithoutStatus = {
      ...firstRow,
      fields: { frontmatter: { id: "SPEC-0001" } },
    };

    renderView({
      execution: {
        ...execution([rowWithoutStatus]),
        columns: [{ field: "frontmatter.spec-status", label: "Status" }],
      },
      onStageOps,
    });

    const status = openEnumEditor();
    expect(status).toHaveValue("");
    expect(within(status).getByRole("option", { name: "Unset" })).toBeInTheDocument();

    fireEvent.change(status, { target: { value: "active" } });

    expect(onStageOps).toHaveBeenCalledWith([
      expect.objectContaining({
        fieldValue: { kind: "scalar", scalar: "active" },
        expected: { field: { kind: "unset" } },
      }),
    ]);
  });
  it("stages clearing an enum as an explicit unset", () => {
    const onStageOps = vi.fn().mockResolvedValue(undefined);
    renderView({ onStageOps });

    const status = openEnumEditor();
    fireEvent.change(status, { target: { value: "" } });

    expect(onStageOps).toHaveBeenCalledWith([
      expect.objectContaining({
        fieldValue: { kind: "unset" },
        expected: { field: { kind: "scalar", scalar: "active" } },
      }),
    ]);
  });
  it("filters the current server-backed issue summaries and opens scoped detail", () => {
    const onOpenIssues = vi.fn();
    renderView({
      issueCounts: new Map([
        ["docs/specs/alpha.md", 2],
        ["docs/specs/bravo.md", 0],
      ]),
      onOpenIssues,
    });
    fireEvent.click(screen.getByRole("button", { name: "Columns" }));
    fireEvent.click(screen.getByRole("checkbox", { name: "Issues" }));
    fireEvent.click(screen.getByRole("checkbox", { name: "Problems on this page" }));

    expect(screen.getByText("Alpha Spec")).toBeVisible();
    expect(screen.queryByText("Bravo Spec")).toBeNull();
    fireEvent.click(
      screen.getByRole("button", { name: /Open 2 problems for docs\/specs\/alpha.md/ }),
    );
    expect(onOpenIssues).toHaveBeenCalledWith({ kind: "note", key: "docs/specs/alpha.md" });
  });
  it("renders the problems filter flat so group ranges cannot point at the wrong rows", () => {
    const base = execution();
    renderView({
      execution: {
        ...base,
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
          },
        ],
      },
      issueCounts: new Map([
        ["docs/specs/alpha.md", 0],
        ["docs/specs/bravo.md", 3],
      ]),
    });
    expect(screen.getByRole("button", { name: "Draft 1" })).toBeVisible();

    fireEvent.click(screen.getByRole("checkbox", { name: "Problems on this page" }));

    expect(screen.getByText("Bravo Spec")).toBeVisible();
    expect(screen.queryByText("Alpha Spec")).toBeNull();
    expect(screen.queryByRole("button", { name: "Draft 1" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Active 1" })).toBeNull();
  });
  it("keeps server enum values canonical while marking dirty fields", () => {
    renderView({
      editSession: {
        sessionId: "session-1",
        status: "dirty",
        hasUncommittedChanges: true,
        createdAt: "2026-05-06T00:00:00Z",
        updatedAt: "2026-05-06T00:00:01Z",
        ops: [
          {
            kind: "setField",
            path: "docs/specs/alpha.md",
            field: "specStatus",
            value: "archived",
          },
        ],
        changedFieldsByNode: {
          "docs/specs/alpha.md": ["specStatus"],
        },
      },
      onStageOps: vi.fn().mockResolvedValue(undefined),
    });

    const trigger = screen.getAllByRole("button", { name: "Edit Status" })[0];
    expect(trigger).toHaveTextContent("Active");
    expect(trigger).toHaveClass("is-dirty");
    expect(trigger.closest("tr")).toHaveClass("is-dirty");
  });
  it("keeps showing staged values the server has not accepted", () => {
    const result = execution();
    result.capabilities = result.capabilities?.map((column) =>
      column.key === "title"
        ? { ...column, edit: { kind: "string", operation: "setField", field: "title" } }
        : column,
    );
    renderView({
      execution: result,
      stagedEditsPending: true,
      editSession: {
        sessionId: "session-1",
        status: "dirty",
        hasUncommittedChanges: true,
        createdAt: "2026-05-06T00:00:00Z",
        updatedAt: "2026-05-06T00:00:01Z",
        ops: [
          { kind: "setField", path: "docs/specs/alpha.md", field: "specStatus", value: "archived" },
          { kind: "setField", path: "docs/specs/alpha.md", field: "title", value: "Renamed" },
        ],
      },
      onStageOps: vi.fn().mockResolvedValue(undefined),
    });

    expect(screen.getAllByRole("button", { name: "Edit Status" })[0]).toHaveTextContent("Archived");
    expect(screen.getByRole("button", { name: "Open Renamed" })).toBeVisible();
  });

  it("marks rows dirty from edit ops when session field maps miss the row key", () => {
    renderView({
      execution: {
        ...execution([
          {
            ...firstRow,
            title: "Old action #action-item",
            resolvedType: "ActionItem",
            ref: {
              notePath: "docs/tasks.md",
              fragment: "^AI-1",
              nodeId: "AI-1",
              structuralFingerprint: "fp-1",
              kind: "EMBEDDED",
            },
            path: "docs/tasks.md",
            fields: {
              summary: "Old action #action-item",
              assignee: "Alice Anderson",
            },
          },
        ]),
        capabilities: [
          {
            key: "summary",
            label: "Action",
            valueKind: "string",
            sortable: true,
            groupable: false,
            filterOps: ["contains"],
            edit: {
              kind: "scalar",
              operation: "setField",
              field: "summary",
            },
          },
          {
            key: "assignee",
            label: "Assignee",
            valueKind: "node",
            sortable: true,
            groupable: true,
            filterOps: ["eq"],
            edit: {
              kind: "node",
              operation: "setLinkField",
              field: "assignee",
              candidates: [
                {
                  value: "people/alice.md",
                  label: "Alice Anderson",
                  path: "people/alice.md",
                  ref: { notePath: "people/alice.md", kind: "NOTE" },
                },
                {
                  value: "people/bob.md",
                  label: "Bob Baker",
                  path: "people/bob.md",
                  ref: { notePath: "people/bob.md", kind: "NOTE" },
                },
              ],
            },
          },
        ],
        columns: [
          { field: "summary", label: "Action" },
          { field: "assignee", label: "Assignee" },
        ],
        groups: [],
      },
      editSession: {
        sessionId: "session-1",
        status: "dirty",
        hasUncommittedChanges: true,
        createdAt: "2026-05-06T00:00:00Z",
        updatedAt: "2026-05-06T00:00:01Z",
        ops: [
          {
            kind: "setLinkField",
            path: "docs/tasks.md#^AI-1",
            nodeId: "AI-1",
            structuralFingerprint: "fp-1",
            field: "assignee",
            values: ["people/bob.md"],
          },
        ],
      },
      onStageOps: vi.fn().mockResolvedValue(undefined),
    });

    const assignee = screen.getByLabelText("Edit Assignee");
    expect(assignee.parentElement).toHaveClass("is-dirty");
    expect(assignee.closest("tr")).toHaveClass("is-dirty");
  });
  it("keeps server scalar values canonical after editing ends", () => {
    const rendered = renderView({
      execution: {
        ...execution([
          {
            ...firstRow,
            title: "Old action #action-item",
            resolvedType: "ActionItem",
            ref: {
              notePath: "docs/tasks.md",
              fragment: "^AI-1",
              nodeId: "AI-1",
              structuralFingerprint: "fp-1",
              kind: "EMBEDDED",
            },
            path: "docs/tasks.md",
            fields: {
              summary: "Old action #action-item",
              title: "Old action #action-item",
            },
          },
        ]),
        capabilities: [
          {
            key: "title",
            label: "Action",
            valueKind: "string",
            sortable: true,
            groupable: false,
            filterOps: ["contains"],
            edit: {
              kind: "scalar",
              operation: "setField",
              field: "summary",
            },
          },
        ],
        columns: [{ field: "title", label: "Action" }],
      },
      onStageOps: vi.fn().mockResolvedValue(undefined),
    });

    fireEvent.keyDown(screen.getByRole("button", { name: /^Open / }), { key: "F2" });
    const input = screen.getByRole("textbox", { name: "Edit Action" });
    fireEvent.change(input, { target: { value: "New action #action-item" } });
    fireEvent.blur(input);

    rendered.rerender(
      <ConfiguredView
        view={baseView}
        execution={{
          ...execution([
            {
              ...firstRow,
              title: "Old action #action-item",
              resolvedType: "ActionItem",
              ref: {
                notePath: "docs/tasks.md",
                fragment: "^AI-1",
                nodeId: "AI-1",
                structuralFingerprint: "fp-1",
                kind: "EMBEDDED",
              },
              path: "docs/tasks.md",
              fields: {
                summary: "Old action #action-item",
                title: "Old action #action-item",
              },
            },
          ]),
          capabilities: [
            {
              key: "title",
              label: "Action",
              valueKind: "string",
              sortable: true,
              groupable: false,
              filterOps: ["contains"],
              edit: {
                kind: "scalar",
                operation: "setField",
                field: "summary",
              },
            },
          ],
          columns: [{ field: "title", label: "Action" }],
        }}
        loading={false}
        error={null}
        state={{ page: { offset: 0, first: 25 } }}
        onStateChange={() => {}}
        onRefresh={() => {}}
        onOpenRow={() => {}}
        editSession={{
          sessionId: "session-1",
          status: "dirty",
          hasUncommittedChanges: true,
          createdAt: "2026-05-06T00:00:00Z",
          updatedAt: "2026-05-06T00:00:01Z",
          ops: [
            {
              kind: "setField",
              path: "docs/tasks.md#^AI-1",
              nodeId: "AI-1",
              structuralFingerprint: "fp-1",
              field: "summary",
              value: "New action #action-item",
            },
          ],
          changedFieldsByNodeRef: {
            "docs/tasks.md|^AI-1|AI-1|EMBEDDED|fp-1": ["summary"],
          },
        }}
        onStageOps={vi.fn().mockResolvedValue(undefined)}
      />,
    );

    expect(screen.getByRole("button", { name: /^Open / })).toHaveTextContent(
      "Old action #action-item",
    );
    expect(screen.getByRole("button", { name: /^Open / })).toHaveClass("is-dirty");
    expect(screen.queryByText("New action #action-item")).toBeNull();
  });
});
