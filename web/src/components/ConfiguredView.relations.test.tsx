import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { PropsWithChildren } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { NodePreview, OntologyEditSessionResponse } from "../api/types";
import { withFakeFetch } from "../test/fakeFetch";
import { renderWithQueryClient } from "../test/renderWithQueryClient";
import { NodeEditableCell } from "./ConfiguredTableNodeEditor";
import { relationDisplayValue } from "./ConfiguredTableCellReadOnly";
import { ConfiguredViewCard } from "./ConfiguredViewCard";
import { ConfiguredViewTable } from "./ConfiguredViewTable";
import { actionItemRow, execution, rendererProps, renderView } from "./ConfiguredView.testFixtures";

const relationPreview: NodePreview = {
  ref: "people/alice.md",
  path: "people/alice.md",
  title: "Alice Example",
  format: "markdown",
  fragmentResolved: true,
  fields: [],
  hasIssues: false,
};

const linkedPreview: NodePreview = {
  ...relationPreview,
  fields: [
    {
      name: "related",
      label: "Related",
      kind: "link",
      importance: "NORMAL",
      values: [{ text: "Nested note", target: "notes/nested.md" }],
    },
  ],
};

async function advancePreview(ms: number) {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms);
  });
}

describe("Configured relation previews", () => {
  const http = withFakeFetch();

  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(0);
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("shares hover, focus, Tab, blur, Escape, and accessibility behavior", async () => {
    http.json("GET", "/api/v1/nodes/preview", relationPreview);
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });

    const row = {
      ...actionItemRow,
      relationValues: {
        assignedTo: [
          {
            value: "[[people/alice.md]]",
            ref: { notePath: "people/alice.md", kind: "NOTE" as const },
            title: "Alice Example",
          },
        ],
      },
    };

    const result = {
      ...execution([row]),
      groups: [],
      capabilities: [
        {
          key: "assignedTo",
          label: "Assigned To",
          valueKind: "relation",
          importance: "NORMAL" as const,
          sortable: true,
          groupable: true,
          filterOps: ["eq"],
        },
      ],
      columns: [{ field: "assignedTo", label: "Assigned To" }],
    };

    function Wrapper({ children }: PropsWithChildren) {
      return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
    }

    render(<ConfiguredViewTable {...rendererProps({ execution: result })} />, {
      wrapper: Wrapper,
    });

    const link = screen.getByRole("link", { name: "Alice Example" });

    fireEvent.mouseEnter(link);
    await advancePreview(100);
    await advancePreview(200);

    let card = screen.getByRole("region", { name: "Preview of Alice Example" });
    expect(link).toHaveAttribute("aria-describedby", card.id);
    expect(http.requests("GET", "/api/v1/nodes/preview")[0]?.query.get("ref")).toBe(
      "people/alice.md",
    );
    expect(http.requests("GET", "/api/v1/nodes/preview")[0]?.query.get("from")).toBe(
      "meetings/planning.md",
    );

    fireEvent.keyDown(window, { key: "Escape" });
    expect(screen.queryByRole("region", { name: "Preview of Alice Example" })).toBeNull();

    act(() => link.focus());
    expect(link.closest("tr")).not.toHaveClass("is-selected");
    await advancePreview(100);
    await advancePreview(200);

    card = screen.getByRole("region", { name: "Preview of Alice Example" });
    fireEvent.keyDown(link, { key: "Tab" });
    expect(card).toHaveFocus();
    fireEvent.keyDown(card, { key: "Tab", shiftKey: true });
    expect(link).toHaveFocus();
    fireEvent.blur(link);
    await advancePreview(150);
    expect(screen.queryByRole("region", { name: "Preview of Alice Example" })).toBeNull();
  });

  it("previews read-only and editable primary cells by their canonical row ref", async () => {
    http.json("GET", "/api/v1/nodes/preview", relationPreview);
    const readOnly = execution([actionItemRow]);
    readOnly.groups = [];
    readOnly.columns = [{ field: "title", label: "Title" }];
    readOnly.capabilities = [{ key: "title", label: "Title", sortable: true, groupable: false }];

    const first = renderWithQueryClient(
      <ConfiguredViewTable {...rendererProps({ execution: readOnly })} />,
    );

    fireEvent.focus(screen.getByRole("button", { name: "Open Follow up" }));
    await advancePreview(100);
    await advancePreview(200);

    expect(http.requests("GET", "/api/v1/nodes/preview")[0]?.query.get("ref")).toBe(
      "meetings/planning.md#^AI-1",
    );
    first.unmount();

    const editable = execution([
      { ...actionItemRow, fields: { ...actionItemRow.fields, summary: "Follow up soon" } },
    ]);

    editable.groups = [];
    editable.columns = [{ field: "summary", label: "Summary" }];
    editable.capabilities = [
      {
        key: "summary",
        label: "Summary",
        sortable: true,
        groupable: false,
        edit: { kind: "scalar", operation: "setField", field: "summary" },
      },
    ];

    renderWithQueryClient(
      <ConfiguredViewTable
        {...rendererProps({
          execution: editable,
          onStageOps: vi.fn().mockResolvedValue(undefined),
        })}
      />,
    );

    const title = screen.getByRole("button", { name: "Open Follow up soon" });
    fireEvent.focus(title);
    await advancePreview(100);
    await advancePreview(200);
    expect(http.requests("GET", "/api/v1/nodes/preview")[1]?.query.get("ref")).toBe(
      "meetings/planning.md#^AI-1",
    );

    fireEvent.keyDown(title, { key: "F2" });
    expect(screen.getByRole("textbox", { name: "Edit Summary" })).toHaveFocus();
  });

  it("previews a board card title by its canonical row ref", async () => {
    http.json("GET", "/api/v1/nodes/preview", relationPreview);
    renderWithQueryClient(
      <ConfiguredViewCard
        row={actionItemRow}
        layout={{ title: { field: "title" }, fields: [] }}
        mode="board"
        capabilities={[]}
        viewID="action-items.default"
        onOpenRow={() => {}}
      />,
    );

    fireEvent.focus(screen.getByRole("button", { name: "Follow up" }));
    await advancePreview(100);
    await advancePreview(200);

    expect(http.requests("GET", "/api/v1/nodes/preview")[0]?.query.get("ref")).toBe(
      "meetings/planning.md#^AI-1",
    );
  });

  it("keeps preview links outside table keyboard navigation and returns focus on Escape", async () => {
    http.json("GET", "/api/v1/nodes/preview", linkedPreview);
    const result = execution([actionItemRow]);
    result.groups = [];
    result.columns = [{ field: "title", label: "Title" }];
    result.capabilities = [{ key: "title", label: "Title", sortable: true, groupable: false }];
    renderWithQueryClient(<ConfiguredViewTable {...rendererProps({ execution: result })} />);

    const trigger = screen.getByRole("button", { name: "Open Follow up" });
    act(() => trigger.focus());
    await advancePreview(100);
    await advancePreview(200);
    const link = screen.getByRole("link", { name: "Nested note" });
    act(() => link.focus());

    fireEvent.keyDown(link, { key: "ArrowLeft" });
    expect(link).toHaveFocus();
    fireEvent.keyDown(link, { key: "Escape" });
    expect(screen.queryByRole("region")).not.toBeInTheDocument();
    expect(trigger).toHaveFocus();
  });

  it("lets a preview link drag without dragging its owning board card", async () => {
    http.json("GET", "/api/v1/nodes/preview", linkedPreview);
    const onDragStart = vi.fn();
    renderWithQueryClient(
      <ConfiguredViewCard
        row={actionItemRow}
        layout={{ title: { field: "title" }, fields: [] }}
        mode="board"
        capabilities={[]}
        viewID="action-items.default"
        onOpenRow={() => {}}
        draggable
        onDragStart={onDragStart}
      />,
    );
    act(() => screen.getByRole("button", { name: "Follow up" }).focus());
    await advancePreview(100);
    await advancePreview(200);

    fireEvent.dragStart(screen.getByRole("link", { name: "Nested note" }));
    expect(onDragStart).not.toHaveBeenCalled();
  });
});

describe("relationDisplayValue", () => {
  it("keeps commas inside complete wikilinks while using aliases and basenames", () => {
    expect(relationDisplayValue("[[people/jane.md|Doe, Jane]], [[people/john-smith.md]]")).toBe(
      "Doe, Jane, john-smith.md",
    );
  });
});

describe("ConfiguredView boolean and relation editing", () => {
  it("opens each resolved read-only relation without firing the source row click", () => {
    const onOpenRow = vi.fn();

    const row = {
      ...actionItemRow,
      fields: { ...actionItemRow.fields, assignedTo: "[[Alice]], [[Bob]]" },
      relationValues: {
        assignedTo: [
          {
            value: "[[people/alice.md|Aliased Alice]]",
            ref: { notePath: "people/alice.md", kind: "NOTE" as const },
            title: "Alice Example",
          },
          {
            value: "[[people/bob.md]]",
            ref: {
              notePath: "people/bob.md",
              kind: "EMBEDDED" as const,
              structuralFingerprint: "bob-structural",
            },
            title: "Bob Example",
          },
        ],
      },
    };

    renderView({
      execution: {
        ...execution([row]),
        groups: [],
        capabilities: [
          {
            key: "assignedTo",
            label: "Assigned To",
            valueKind: "relation",
            importance: "NORMAL",
            sortable: true,
            groupable: true,
            filterOps: ["eq"],
          },
        ],
        columns: [{ field: "assignedTo", label: "Assigned To" }],
      },
      onOpenRow,
    });

    const alice = screen.getByRole("link", { name: "Alice Example" });
    const bob = screen.getByRole("link", { name: "Bob Example" });

    expect(decodeURIComponent(bob.getAttribute("href") || "")).toContain("#struct:bob-structural");
    act(() => alice.focus());
    fireEvent.click(alice);
    expect(onOpenRow).toHaveBeenCalledTimes(1);
    expect(onOpenRow).toHaveBeenCalledWith(
      expect.objectContaining({
        path: "people/alice.md",
        ref: expect.objectContaining({ notePath: "people/alice.md" }),
      }),
      "activate",
    );
    expect(alice.closest("tr")).not.toHaveClass("is-selected");

    fireEvent.click(bob);
    expect(onOpenRow).toHaveBeenLastCalledWith(
      expect.objectContaining({
        path: "people/bob.md#struct:bob-structural",
        ref: expect.objectContaining({ structuralFingerprint: "bob-structural" }),
      }),
      "activate",
    );
  });

  it("stages false distinctly from an unset boolean", () => {
    const onStageOps = vi.fn().mockResolvedValue(undefined);
    renderView({
      execution: {
        ...execution([{ ...actionItemRow, fields: { ...actionItemRow.fields, done: true } }]),
        capabilities: [
          {
            key: "done",
            label: "Done",
            valueKind: "bool",
            sortable: true,
            groupable: true,
            filterOps: ["eq"],
            edit: {
              kind: "boolean",
              operation: "setField",
              field: "done",
              options: ["false", "true"],
            },
          },
        ],
        columns: [{ field: "done", label: "Done" }],
      },
      onStageOps,
    });

    fireEvent.click(screen.getByLabelText("Edit Done"));

    expect(onStageOps).toHaveBeenCalledWith([
      expect.objectContaining({
        value: "false",
        fieldValue: { kind: "scalar", scalar: "false" },
        expected: { field: { kind: "scalar", scalar: "true" } },
      }),
    ]);
  });
  it("treats a numeric zero as a set boolean without rendering its text", () => {
    renderView({
      execution: {
        ...execution([{ ...actionItemRow, fields: { ...actionItemRow.fields, done: 0 } }]),
        capabilities: [
          {
            key: "done",
            label: "Done",
            valueKind: "bool",
            sortable: true,
            groupable: true,
            filterOps: ["eq"],
            edit: {
              kind: "boolean",
              operation: "setField",
              field: "done",
              options: ["false", "true"],
            },
          },
        ],
        columns: [{ field: "done", label: "Done" }],
      },
      onStageOps: vi.fn().mockResolvedValue(undefined),
    });

    expect(screen.queryByText("0")).toBeNull();
    expect(screen.getByLabelText("Edit Done")).not.toBeChecked();
    // A present value keeps the ghost clear affordance in the DOM.
    expect(screen.getByRole("button", { name: "Clear Done" })).toBeInTheDocument();
  });
  it("rolls back optimistic boolean edits when staging fails", async () => {
    const onStageOps = vi.fn().mockRejectedValue(new Error("stage failed"));
    renderView({
      execution: {
        ...execution([actionItemRow]),
        capabilities: [
          {
            key: "title",
            label: "Title",
            sortable: true,
            groupable: false,
            filterOps: ["contains"],
          },
          {
            key: "done",
            label: "Done",
            sortable: true,
            groupable: true,
            filterOps: ["eq"],
            edit: {
              kind: "boolean",
              operation: "setField",
              field: "done",
              options: ["false", "true"],
            },
          },
        ],
        columns: [
          { field: "title", label: "Title" },
          { field: "done", label: "Done" },
        ],
      },
      onStageOps,
    });

    const checkbox = screen.getByLabelText("Edit Done");
    fireEvent.click(checkbox);

    expect(checkbox).toBeChecked();
    await waitFor(() => expect(checkbox).not.toBeChecked());
  });
  it("stages relation edits from filterable node candidates", () => {
    const onStageOps = vi.fn().mockResolvedValue(undefined);
    renderView({
      execution: {
        ...execution([actionItemRow]),
        capabilities: [
          {
            key: "title",
            label: "Title",
            sortable: true,
            groupable: false,
            filterOps: ["contains"],
          },
          {
            key: "assignedTo",
            label: "Assigned To",
            sortable: true,
            groupable: true,
            filterOps: ["eq"],
            edit: {
              kind: "node",
              operation: "setLinkField",
              field: "assignedTo",
              targetType: "Person",
              candidates: [
                {
                  ref: { notePath: "people/alice.md", kind: "NOTE" },
                  value: "[[Alice]]",
                  label: "Alice",
                  path: "people/alice.md",
                },
                {
                  ref: { notePath: "people/bob.md", kind: "NOTE" },
                  value: "[[Bob]]",
                  label: "Bob",
                  path: "people/bob.md",
                },
              ],
            },
          },
        ],
        columns: [
          { field: "title", label: "Title" },
          { field: "assignedTo", label: "Assigned To" },
        ],
      },
      onStageOps,
    });

    fireEvent.click(screen.getByRole("button", { name: "Edit Assigned To" }));
    expect(screen.getByRole("combobox", { name: "Edit Assigned To" })).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Edit Assigned To"), {
      target: { value: "[[Bob]]" },
    });

    expect(onStageOps).toHaveBeenCalledWith([
      {
        id: "field:meetings%2Fplanning.md%23%5EAI-1:AI-1:assignedTo",
        kind: "setLinkField",
        path: "meetings/planning.md#^AI-1",
        nodeId: "AI-1",
        structuralFingerprint: "fp-1",
        field: "assignedTo",
        values: ["[[Bob]]"],
        fieldValue: { kind: "list", items: ["[[Bob]]"] },
        expected: { field: { kind: "list", items: ["people/alice.md"] } },
      },
    ]);
  });
  it("does not auto-select the first relation candidate", () => {
    renderView({
      execution: {
        ...execution([{ ...actionItemRow, fields: {} }]),
        capabilities: [
          {
            key: "assignedTo",
            label: "Assigned To",
            sortable: true,
            groupable: true,
            filterOps: ["eq"],
            edit: {
              kind: "node",
              operation: "setLinkField",
              field: "assignedTo",
              targetType: "Person",
              candidates: [
                {
                  ref: { notePath: "people/alice.md", kind: "NOTE" },
                  value: "[[Alice]]",
                  label: "Alice",
                  path: "people/alice.md",
                },
                {
                  ref: { notePath: "people/bob.md", kind: "NOTE" },
                  value: "[[Bob]]",
                  label: "Bob",
                  path: "people/bob.md",
                },
              ],
            },
          },
        ],
        columns: [{ field: "assignedTo", label: "Assigned To" }],
      },
      onStageOps: vi.fn().mockResolvedValue(undefined),
    });

    fireEvent.click(screen.getByRole("button", { name: "Edit Assigned To" }));
    expect(screen.getByLabelText("Edit Assigned To")).toHaveValue("");
  });
  it("stages clearing a scalar relation as an explicit unset", () => {
    const onStageOps = vi.fn().mockResolvedValue(undefined);
    renderView({
      execution: {
        ...execution([actionItemRow]),
        capabilities: [
          {
            key: "assignedTo",
            label: "Assigned To",
            sortable: true,
            groupable: true,
            filterOps: ["eq"],
            edit: {
              kind: "node",
              operation: "setLinkField",
              field: "assignedTo",
              targetType: "Person",
              candidates: [
                {
                  ref: { notePath: "people/alice.md", kind: "NOTE" },
                  value: "[[Alice]]",
                  label: "Alice",
                  path: "people/alice.md",
                },
                {
                  ref: { notePath: "people/bob.md", kind: "NOTE" },
                  value: "[[Bob]]",
                  label: "Bob",
                  path: "people/bob.md",
                },
              ],
            },
          },
        ],
        columns: [{ field: "assignedTo", label: "Assigned To" }],
      },
      onStageOps,
    });

    fireEvent.click(screen.getByRole("button", { name: "Edit Assigned To" }));
    const relation = screen.getByRole("combobox", { name: "Edit Assigned To" });
    fireEvent.change(relation, { target: { value: "" } });

    expect(onStageOps).toHaveBeenLastCalledWith([
      expect.objectContaining({
        kind: "setLinkField",
        values: [],
        fieldValue: { kind: "unset" },
        expected: { field: { kind: "list", items: ["people/alice.md"] } },
      }),
    ]);
  });
  it("adds and removes list relation values without replacing the rest", () => {
    const onStageOps = vi.fn().mockResolvedValue(undefined);
    renderView({
      execution: {
        ...execution([
          {
            ...actionItemRow,
            fields: {
              ...actionItemRow.fields,
              assignedTo: ["people/bob.md", "people/alice.md"],
            },
          },
        ]),
        capabilities: [
          {
            key: "assignedTo",
            label: "Assigned To",
            sortable: true,
            groupable: true,
            filterOps: ["eq"],
            edit: {
              kind: "node",
              operation: "setLinkField",
              field: "assignedTo",
              targetType: "Person",
              candidates: [
                {
                  ref: { notePath: "people/alice.md", kind: "NOTE" },
                  value: "[[Alice]]",
                  label: "Alice",
                  path: "people/alice.md",
                },
                {
                  ref: { notePath: "people/bob.md", kind: "NOTE" },
                  value: "[[Bob]]",
                  label: "Bob",
                  path: "people/bob.md",
                },
                {
                  ref: { notePath: "people/carol.md", kind: "NOTE" },
                  value: "[[Carol]]",
                  label: "Carol",
                  path: "people/carol.md",
                },
              ],
            },
          },
        ],
        columns: [{ field: "assignedTo", label: "Assigned To" }],
      },
      onStageOps,
    });

    fireEvent.click(screen.getByRole("button", { name: "Edit Assigned To" }));
    fireEvent.change(screen.getByRole("combobox", { name: "Add Assigned To" }), {
      target: { value: "[[Carol]]" },
    });

    expect(onStageOps).toHaveBeenLastCalledWith([
      expect.objectContaining({
        values: ["[[Bob]]", "[[Alice]]", "[[Carol]]"],
        fieldValue: { kind: "list", items: ["[[Bob]]", "[[Alice]]", "[[Carol]]"] },
        expected: {
          field: { kind: "list", items: ["people/bob.md", "people/alice.md"] },
        },
      }),
    ]);

    // The row has not refetched yet, so the removal builds on the staged add.
    fireEvent.click(screen.getByRole("button", { name: "Remove Bob" }));

    expect(onStageOps).toHaveBeenLastCalledWith([
      expect.objectContaining({
        values: ["[[Alice]]", "[[Carol]]"],
        fieldValue: { kind: "list", items: ["[[Alice]]", "[[Carol]]"] },
      }),
    ]);
  });
  it("keeps relation candidates usable while a staged edit refetches them", async () => {
    let calls = 0;

    vi.stubGlobal(
      "fetch",
      vi.fn(() => {
        calls += 1;

        if (calls > 1) return new Promise(() => {});

        return Promise.resolve({
          ok: true,
          headers: { get: () => "application/json" },
          json: async () => ({
            field: "assignedTo",
            candidates: [{ value: "[[Bob]]", label: "Bob", path: "people/bob.md" }],
          }),
        });
      }),
    );

    const edit = {
      kind: "node",
      operation: "setLinkField",
      field: "assignedTo",
      targetType: "Person",
    };

    const session = (revision: number): OntologyEditSessionResponse => ({
      sessionId: "s1",
      status: "dirty",
      revision,
      hasUncommittedChanges: true,
      createdAt: "2026-09-23T00:00:00Z",
      updatedAt: "2026-09-23T00:00:01Z",
    });

    const cell = (revision: number) => (
      <NodeEditableCell
        viewID="specs.default"
        field="assignedTo"
        capability={undefined}
        edit={edit}
        value=""
        list={false}
        dirty={false}
        editSession={session(revision)}
        onStage={vi.fn()}
      />
    );

    const { rerender } = renderWithQueryClient(cell(1));
    fireEvent.click(screen.getByRole("button"));
    expect(await screen.findByRole("option", { name: "Bob" })).toBeInTheDocument();

    rerender(cell(2));
    await waitFor(() => expect(calls).toBe(2));
    expect(screen.getByRole("option", { name: "Bob" })).toBeInTheDocument();
    expect(screen.getByRole("combobox")).toBeEnabled();
    expect(screen.queryByRole("option", { name: "Loading…" })).toBeNull();
  });

  it("loads relation edit candidates when they are not embedded", async () => {
    const onStageOps = vi.fn().mockResolvedValue(undefined);

    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      headers: { get: () => "application/json" },
      json: async () => ({
        field: "assignedTo",
        targetType: "Person",
        candidates: [
          {
            ref: { notePath: "people/bob.md", kind: "NOTE" },
            value: "[[Bob]]",
            label: "Bob",
            path: "people/bob.md",
          },
        ],
      }),
    });

    vi.stubGlobal("fetch", fetchMock);

    renderView({
      execution: {
        ...execution([actionItemRow]),
        capabilities: [
          {
            key: "assignedTo",
            label: "Assigned To",
            sortable: true,
            groupable: true,
            filterOps: ["eq"],
            edit: {
              kind: "node",
              operation: "setLinkField",
              field: "assignedTo",
              targetType: "Person",
            },
          },
        ],
        columns: [{ field: "assignedTo", label: "Assigned To" }],
      },
      onStageOps,
    });

    fireEvent.click(screen.getByRole("button", { name: "Edit Assigned To" }));
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        "/api/v1/views/specs.default/field-candidates?field=assignedTo&limit=50",
        expect.objectContaining({ signal: expect.any(AbortSignal) }),
      );
    });
    await waitFor(() => {
      expect(
        within(screen.getByLabelText("Edit Assigned To")).getByRole("option", {
          name: "Bob",
        }),
      ).toBeInTheDocument();
    });

    fireEvent.change(screen.getByLabelText("Edit Assigned To"), {
      target: { value: "[[Bob]]" },
    });

    expect(onStageOps).toHaveBeenCalledWith([
      {
        id: "field:meetings%2Fplanning.md%23%5EAI-1:AI-1:assignedTo",
        kind: "setLinkField",
        path: "meetings/planning.md#^AI-1",
        nodeId: "AI-1",
        structuralFingerprint: "fp-1",
        field: "assignedTo",
        values: ["[[Bob]]"],
        fieldValue: { kind: "list", items: ["[[Bob]]"] },
        expected: { field: { kind: "list", items: ["people/alice.md"] } },
      },
    ]);
  });
  it("titles an unresolved relation from its edit candidates", () => {
    renderView({
      execution: {
        ...execution([actionItemRow]),
        capabilities: [
          {
            key: "assignedTo",
            label: "Assigned To",
            sortable: true,
            groupable: true,
            filterOps: ["eq"],
            edit: {
              kind: "node",
              operation: "setLinkField",
              field: "assignedTo",
              targetType: "Person",
              candidates: [
                {
                  ref: { notePath: "people/alice.md", kind: "NOTE" },
                  value: "[[Alice]]",
                  label: "Alice",
                  path: "people/alice.md",
                },
              ],
            },
          },
        ],
        columns: [{ field: "assignedTo", label: "Assigned To" }],
      },
      onStageOps: vi.fn().mockResolvedValue(undefined),
    });

    // The row stores a path and carries no resolved relation values.
    expect(screen.getByRole("link", { name: "Alice" })).toBeInTheDocument();
    expect(screen.queryByText("people/alice.md")).toBeNull();
  });

  it("selects the current relation candidate from a title wikilink", () => {
    renderView({
      execution: {
        ...execution([
          {
            ...actionItemRow,
            fields: { ...actionItemRow.fields, assignedTo: "[[Alice]]" },
          },
        ]),
        capabilities: [
          {
            key: "assignedTo",
            label: "Assigned To",
            sortable: true,
            groupable: true,
            filterOps: ["eq"],
            edit: {
              kind: "node",
              operation: "setLinkField",
              field: "assignedTo",
              targetType: "Person",
              candidates: [
                {
                  ref: { notePath: "people/alice.md", kind: "NOTE" },
                  value: "[[Alice]]",
                  label: "Alice",
                  path: "people/alice.md",
                },
                {
                  ref: { notePath: "people/bob.md", kind: "NOTE" },
                  value: "[[Bob]]",
                  label: "Bob",
                  path: "people/bob.md",
                },
              ],
            },
          },
        ],
        columns: [{ field: "assignedTo", label: "Assigned To" }],
      },
      onStageOps: vi.fn().mockResolvedValue(undefined),
    });

    fireEvent.click(screen.getByRole("button", { name: "Edit Assigned To" }));
    expect(screen.getByLabelText("Edit Assigned To")).toHaveValue("[[Alice]]");
  });
  it("disambiguates duplicate relation candidate labels", () => {
    const onStageOps = vi.fn().mockResolvedValue(undefined);
    renderView({
      execution: {
        ...execution([actionItemRow]),
        capabilities: [
          {
            key: "assignedTo",
            label: "Assigned To",
            sortable: true,
            groupable: true,
            filterOps: ["eq"],
            edit: {
              kind: "node",
              operation: "setLinkField",
              field: "assignedTo",
              targetType: "Person",
              candidates: [
                {
                  ref: { notePath: "people/alice.md", kind: "NOTE" },
                  value: "[[people/alice.md|Alex]]",
                  label: "Alex",
                  path: "people/alice.md",
                },
                {
                  ref: { notePath: "people/alex.md", kind: "NOTE" },
                  value: "[[people/alex.md|Alex]]",
                  label: "Alex",
                  path: "people/alex.md",
                },
              ],
            },
          },
        ],
        columns: [{ field: "assignedTo", label: "Assigned To" }],
      },
      onStageOps,
    });

    fireEvent.click(screen.getByRole("button", { name: "Edit Assigned To" }));
    fireEvent.change(screen.getByLabelText("Edit Assigned To"), {
      target: { value: "[[people/alex.md|Alex]]" },
    });

    expect(onStageOps).toHaveBeenCalledWith([
      {
        id: "field:meetings%2Fplanning.md%23%5EAI-1:AI-1:assignedTo",
        kind: "setLinkField",
        path: "meetings/planning.md#^AI-1",
        nodeId: "AI-1",
        structuralFingerprint: "fp-1",
        field: "assignedTo",
        values: ["[[people/alex.md|Alex]]"],
        fieldValue: { kind: "list", items: ["[[people/alex.md|Alex]]"] },
        expected: { field: { kind: "list", items: ["people/alice.md"] } },
      },
    ]);
  });
});
