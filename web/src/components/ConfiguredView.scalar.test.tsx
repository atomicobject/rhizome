import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { ViewExecuteResponse, ViewFieldCapability } from "../api/types";
import { ConfiguredView } from "./ConfiguredView";
import { readPersistedEditorDraftRecord } from "./editing/draftStorage";
import {
  actionItemRow,
  baseView,
  execution,
  firstRow,
  openEnumEditor,
  renderView,
} from "./ConfiguredView.testFixtures";

describe("ConfiguredView scalar and boolean editing", () => {
  it("keeps server read-only node rows canonical without making computed fields editable", () => {
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
              summaryText: "Computed old",
            },
          },
        ]),
        capabilities: [
          {
            key: "title",
            label: "Action",
            canonicalField: "summary",
            valueKind: "string",
            sortable: true,
            groupable: false,
            filterOps: ["contains"],
          },
        ],
        columns: [
          { field: "title", label: "Action" },
          { field: "summaryText", label: "Computed" },
        ],
      },
      editSession: {
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
        changedFieldsByNode: {
          "docs/tasks.md#^AI-1": ["summary"],
        },
      },
    });

    expect(screen.getByText("Old action #action-item")).toBeVisible();
    expect(screen.getByText("Computed old")).toBeVisible();
    expect(screen.queryByRole("button", { name: "Edit Action" })).toBeNull();
    expect(screen.queryByText("New action #action-item")).toBeNull();
  });
  it("stages scalar edits on blur after F2", () => {
    const onStageOps = vi.fn().mockResolvedValue(undefined);
    renderView({
      execution: {
        ...execution([
          {
            ...firstRow,
            fields: { ...firstRow.fields, summary: "Old summary" },
          },
        ]),
        capabilities: [
          ...(execution().capabilities ?? []),
          {
            key: "summary",
            label: "Summary",
            valueKind: "string",
            sortable: true,
            groupable: true,
            filterOps: ["eq", "contains"],
            edit: {
              kind: "scalar",
              operation: "setField",
              field: "summary",
            },
          },
        ],
        columns: [{ field: "summary", label: "Summary" }],
      },
      onStageOps,
    });

    expect(screen.queryByRole("textbox", { name: "Edit Summary" })).toBeNull();
    fireEvent.keyDown(screen.getByRole("button", { name: /^Open / }), { key: "F2" });
    const input = screen.getByRole("textbox", { name: "Edit Summary" });
    fireEvent.change(input, { target: { value: "New summary" } });
    fireEvent.blur(input);

    expect(onStageOps).toHaveBeenCalledWith([
      {
        id: "field:docs%2Fspecs%2Falpha.md:note:summary",
        kind: "setField",
        path: "docs/specs/alpha.md",
        field: "summary",
        value: "New summary",
        fieldValue: { kind: "scalar", scalar: "New summary" },
        expected: { field: { kind: "scalar", scalar: "Old summary" } },
      },
    ]);
  });
  it("uses a durable typed date draft when a row unmounts before staging succeeds", async () => {
    window.sessionStorage.setItem("rhizome:ontology-edit-session:tab-id", "table-test");
    const onStageOps = vi.fn().mockRejectedValue(new Error("offline"));

    const tableExecution: ViewExecuteResponse = {
      ...execution([{ ...firstRow, fields: { ...firstRow.fields, due: "2026-04-12" } }]),
      capabilities: [
        {
          key: "due",
          label: "Due",
          valueKind: "date",
          sortable: true,
          groupable: true,
          filterOps: ["eq"],
          edit: { kind: "scalar", operation: "setField", field: "due" },
        },
      ],
      columns: [{ field: "due", label: "Due" }],
    };

    const rendered = renderView({
      execution: tableExecution,
      vaultKey: "/vault",
      onStageOps,
    });

    fireEvent.keyDown(screen.getByRole("button", { name: /^Open / }), { key: "F2" });
    const input = screen.getByLabelText<HTMLInputElement>("Edit Due");
    expect(input.type).toBe("date");
    fireEvent.change(input, { target: { value: "2026-05-01" } });
    rendered.unmount();

    const operationTarget = "field:docs%2Fspecs%2Falpha.md:note:due";
    await waitFor(() =>
      expect(readPersistedEditorDraftRecord("/vault", operationTarget)?.value).toBe("2026-05-01"),
    );
    renderView({ execution: tableExecution, vaultKey: "/vault", onStageOps });
    fireEvent.keyDown(screen.getByRole("button", { name: /^Open / }), { key: "F2" });
    expect(screen.getByLabelText<HTMLInputElement>("Edit Due")).toHaveValue("2026-05-01");
  });
  it("preserves present-empty and unset scalar witnesses", () => {
    const onStageOps = vi.fn().mockResolvedValue(undefined);

    const capability: ViewFieldCapability = {
      key: "summary",
      label: "Summary",
      valueKind: "string",
      sortable: true,
      groupable: true,
      filterOps: ["eq", "contains"],
      edit: {
        kind: "scalar",
        operation: "setField",
        field: "summary",
      },
    };

    const rendered = renderView({
      execution: {
        ...execution([{ ...firstRow, fields: { summary: "" } }]),
        capabilities: [capability],
        columns: [{ field: "summary", label: "Summary" }],
      },
      onStageOps,
    });

    fireEvent.keyDown(screen.getByRole("button", { name: /^Open / }), { key: "F2" });
    const input = screen.getByRole("textbox", { name: "Edit Summary" });
    fireEvent.change(input, { target: { value: "Filled" } });
    fireEvent.blur(input);

    expect(onStageOps).toHaveBeenLastCalledWith([
      expect.objectContaining({ expected: { field: { kind: "scalar", scalar: "" } } }),
    ]);

    rendered.rerender(
      <ConfiguredView
        view={baseView}
        execution={{
          ...execution([{ ...firstRow, fields: {} }]),
          capabilities: [capability],
          columns: [{ field: "summary", label: "Summary" }],
        }}
        loading={false}
        error={null}
        state={{ page: { offset: 0, first: 25 } }}
        onStateChange={() => {}}
        onRefresh={() => {}}
        onOpenRow={() => {}}
        onStageOps={onStageOps}
      />,
    );
    fireEvent.keyDown(screen.getByRole("button", { name: /^Open / }), { key: "F2" });
    const missingInput = screen.getByRole("textbox", { name: "Edit Summary" });
    fireEvent.change(missingInput, { target: { value: "Filled" } });
    fireEvent.blur(missingInput);

    expect(onStageOps).toHaveBeenLastCalledWith([
      expect.objectContaining({ expected: { field: { kind: "unset" } } }),
    ]);
  });
  it("renders numeric zero in scalar controls", () => {
    renderView({
      execution: {
        ...execution([{ ...firstRow, fields: { estimate: 0 } }]),
        capabilities: [
          {
            key: "estimate",
            label: "Estimate",
            valueKind: "int",
            sortable: true,
            groupable: true,
            filterOps: ["eq", "gt"],
            edit: {
              kind: "scalar",
              operation: "setField",
              field: "estimate",
            },
          },
        ],
        columns: [{ field: "estimate", label: "Estimate" }],
      },
      onStageOps: vi.fn().mockResolvedValue(undefined),
    });

    expect(screen.getByRole("button", { name: /^Open / })).toHaveTextContent("0");
  });
  it("opens an editable primary cell on Enter or double-click and keeps F2 for editing", () => {
    const onOpenRow = vi.fn();
    const onStageOps = vi.fn(() => Promise.resolve());
    renderView({
      execution: {
        ...execution([{ ...firstRow, fields: { ...firstRow.fields, summary: "Old summary" } }]),
        capabilities: [
          ...(execution().capabilities ?? []),
          {
            key: "summary",
            label: "Summary",
            valueKind: "string",
            sortable: true,
            groupable: true,
            filterOps: ["eq", "contains"],
            edit: { kind: "scalar", operation: "setField", field: "summary" },
          },
        ],
        columns: [{ field: "summary", label: "Summary" }],
      },
      onOpenRow,
      onStageOps,
    });

    const open = screen.getByRole("button", { name: "Open Old summary" });
    // A click selects the row.
    fireEvent.click(open);
    expect(onOpenRow).not.toHaveBeenCalled();
    fireEvent.keyDown(open, { key: "Enter" });
    fireEvent.doubleClick(open);
    expect(onOpenRow).toHaveBeenCalledTimes(2);
    expect(screen.queryByRole("textbox", { name: "Edit Summary" })).toBeNull();
    fireEvent.keyDown(open, { key: "F2" });
    expect(screen.getByRole("textbox", { name: "Edit Summary" })).toHaveFocus();
  });

  it("starts scalar editing with Enter or F2 and cancels with Escape", () => {
    const onStageOps = vi.fn().mockResolvedValue(undefined);
    renderView({
      execution: {
        ...execution([
          {
            ...firstRow,
            fields: { ...firstRow.fields, summary: "Old summary" },
          },
        ]),
        capabilities: [
          ...(execution().capabilities ?? []),
          {
            key: "summary",
            label: "Summary",
            valueKind: "string",
            sortable: true,
            groupable: true,
            filterOps: ["eq", "contains"],
            edit: {
              kind: "scalar",
              operation: "setField",
              field: "summary",
            },
          },
        ],
        columns: [
          { field: "title", label: "Action" },
          { field: "summary", label: "Summary" },
        ],
      },
      onStageOps,
    });

    fireEvent.keyDown(screen.getByRole("button", { name: "Edit Summary" }), {
      key: "F2",
    });
    let input = screen.getByRole("textbox", { name: "Edit Summary" });
    expect(input).toHaveFocus();
    fireEvent.keyDown(input, { key: "Escape" });
    expect(onStageOps).not.toHaveBeenCalled();
    // Closing the editor hands focus back to the cell, not the page.
    expect(screen.getByRole("button", { name: "Edit Summary" })).toHaveFocus();
    expect(screen.getByRole("button", { name: "Edit Summary" })).toHaveAccessibleDescription(
      "Old summary",
    );

    // Arrow keys walk cells and rows; Escape leaves a cell for its row.
    const row = screen.getByRole("button", { name: "Edit Summary" }).closest("tr")!;
    fireEvent.keyDown(screen.getByRole("button", { name: "Edit Summary" }), { key: "ArrowLeft" });
    // Cell 0 holds the row's selection checkbox.
    const titleCell = row.cells[1].querySelector("button")!;
    expect(titleCell).toHaveFocus();
    fireEvent.keyDown(titleCell, { key: "ArrowRight" });
    expect(screen.getByRole("button", { name: "Edit Summary" })).toHaveFocus();
    fireEvent.keyDown(screen.getByRole("button", { name: "Edit Summary" }), { key: "Escape" });
    expect(row).toHaveFocus();
    expect(row).toHaveAttribute("tabindex", "0");
    fireEvent.keyDown(row, { key: "ArrowRight" });
    expect(titleCell).toHaveFocus();

    fireEvent.keyDown(screen.getByRole("button", { name: "Edit Summary" }), {
      key: "Enter",
    });
    input = screen.getByRole("textbox", { name: "Edit Summary" });
    expect(input).toHaveFocus();
    // Leaving an unchanged editor closes it instead of stranding an input in the cell.
    fireEvent.blur(input);
    expect(screen.queryByRole("textbox", { name: "Edit Summary" })).toBeNull();
    expect(onStageOps).not.toHaveBeenCalled();

    fireEvent.keyDown(screen.getByRole("button", { name: "Edit Summary" }), {
      key: "Enter",
    });
    input = screen.getByRole("textbox", { name: "Edit Summary" });
    fireEvent.change(input, { target: { value: "Committed summary" } });
    fireEvent.keyDown(input, { key: "Enter" });

    expect(onStageOps).toHaveBeenCalledWith([
      {
        id: "field:docs%2Fspecs%2Falpha.md:note:summary",
        kind: "setField",
        path: "docs/specs/alpha.md",
        field: "summary",
        value: "Committed summary",
        fieldValue: { kind: "scalar", scalar: "Committed summary" },
        expected: { field: { kind: "scalar", scalar: "Old summary" } },
      },
    ]);
  });
  it("does not stage invalid scalar edits", () => {
    const onStageOps = vi.fn().mockResolvedValue(undefined);
    renderView({
      execution: {
        ...execution([
          {
            ...firstRow,
            fields: { ...firstRow.fields, estimate: "3" },
          },
        ]),
        capabilities: [
          ...(execution().capabilities ?? []),
          {
            key: "estimate",
            label: "Estimate",
            valueKind: "int",
            sortable: true,
            groupable: true,
            filterOps: ["eq", "gt"],
            edit: {
              kind: "scalar",
              operation: "setField",
              field: "estimate",
            },
          },
        ],
        columns: [{ field: "estimate", label: "Estimate" }],
      },
      onStageOps,
    });

    fireEvent.keyDown(screen.getByRole("button", { name: /^Open / }), { key: "F2" });
    const input = screen.getByRole("spinbutton", { name: "Edit Estimate" });
    fireEvent.change(input, { target: { value: "3.5" } });
    fireEvent.keyDown(input, { key: "Enter" });

    expect(input).toHaveAttribute("aria-invalid", "true");
    expect(onStageOps).not.toHaveBeenCalled();
  });
  it("uses a numeric editor for real fields and enforces required values", () => {
    const onStageOps = vi.fn().mockResolvedValue(undefined);
    renderView({
      execution: {
        ...execution([{ ...firstRow, fields: { ...firstRow.fields, estimate: "3.5" } }]),
        capabilities: [
          {
            key: "estimate",
            label: "Estimate",
            valueKind: "real",
            required: true,
            sortable: true,
            groupable: true,
            filterOps: ["eq", "gt"],
            edit: {
              kind: "number",
              operation: "setField",
              field: "estimate",
              valueKind: "real",
            },
          },
        ],
        columns: [{ field: "estimate", label: "Estimate" }],
      },
      onStageOps,
    });

    fireEvent.keyDown(screen.getByRole("button", { name: /^Open / }), { key: "F2" });
    const input = screen.getByRole("spinbutton", { name: "Edit Estimate" });
    expect(input).toHaveAttribute("step", "any");
    expect(input).toBeRequired();
    fireEvent.change(input, { target: { value: "" } });
    fireEvent.blur(input);

    expect(onStageOps).not.toHaveBeenCalled();
    expect(screen.getByRole("alert")).toHaveTextContent("Edit Estimate is required.");
  });
  it("keeps editable first columns editable", () => {
    const onStageOps = vi.fn().mockResolvedValue(undefined);
    const onOpenRow = vi.fn();
    renderView({
      execution: {
        ...execution(),
        columns: [{ field: "frontmatter.spec-status", label: "Status" }],
      },
      onStageOps,
      onOpenRow,
    });

    const firstEditableRow = screen
      .getAllByRole("button", { name: "Edit Status" })[0]
      .closest("tr");

    fireEvent.change(openEnumEditor(), {
      target: { value: "archived" },
    });

    if (!(firstEditableRow instanceof HTMLTableRowElement)) {
      throw new Error("Editable status is not inside a table row");
    }

    fireEvent.focus(firstEditableRow);
    fireEvent.keyDown(firstEditableRow, { key: "Enter" });
    const headers = screen.getAllByRole("columnheader").map((header) => header.textContent?.trim());

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
    expect(onOpenRow).toHaveBeenCalledWith(expect.objectContaining(firstRow));
    expect(headers).toEqual(["", "Status"]);
  });
  it("stages boolean edits through view cells", () => {
    const onStageOps = vi.fn().mockResolvedValue(undefined);
    renderView({
      execution: {
        ...execution([{ ...actionItemRow, fields: { assignedTo: "people/alice.md" } }]),
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

    fireEvent.click(screen.getByLabelText("Edit Done"));

    expect(screen.getByLabelText("Edit Done")).toBeChecked();
    expect(onStageOps).toHaveBeenCalledWith([
      {
        id: "field:meetings%2Fplanning.md%23%5EAI-1:AI-1:done",
        kind: "setField",
        path: "meetings/planning.md#^AI-1",
        nodeId: "AI-1",
        structuralFingerprint: "fp-1",
        field: "done",
        value: "true",
        fieldValue: { kind: "scalar", scalar: "true" },
        expected: { field: { kind: "unset" } },
      },
    ]);
  });
});
