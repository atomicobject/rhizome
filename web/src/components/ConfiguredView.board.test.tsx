import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { OntologyEditSessionResponse } from "../api/types";

import { ConfiguredViewBoard } from "./ConfiguredViewBoard";
import { baseView, boardExecution, rendererProps, renderView } from "./ConfiguredView.testFixtures";

describe("ConfiguredViewBoard", () => {
  it("renders ordered columns, an empty strip, collapsed strip, and remaining count", () => {
    const { container } = render(
      <ConfiguredViewBoard {...rendererProps({ onStageOps: vi.fn(async () => {}) })} />,
    );

    const columns = [...container.querySelectorAll(".configured-view__board-columns > *")];
    expect(columns.map((column) => column.textContent)).toEqual([
      expect.stringContaining("Planned"),
      expect.stringContaining("(empty)"),
      expect.stringContaining("Active"),
    ]);
    // A value with no records stays as a narrow strip that still takes drops.
    expect(screen.getByLabelText("(empty) column")).toHaveClass("is-empty");
    expect(screen.getByText("2 more")).toBeVisible();

    fireEvent.click(screen.getByRole("button", { name: "Expand Active column, 1 items" }));
    expect(screen.getByLabelText("Active column")).toBeVisible();
  });

  it("collapses and re-expands a column that starts expanded", () => {
    render(<ConfiguredViewBoard {...rendererProps()} />);

    fireEvent.click(screen.getByRole("button", { name: "Collapse Planned column" }));
    expect(screen.queryByRole("article")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Expand Planned column, 3 items" }));
    expect(within(screen.getByLabelText("Planned column")).getByRole("article")).toBeVisible();
  });

  it("counts only the rows a local filter leaves visible", () => {
    const props = rendererProps();
    render(<ConfiguredViewBoard {...props} rows={[]} />);

    expect(screen.queryByText("2 more")).toBeNull();
    expect(screen.getByRole("button", { name: "Expand Active column, 0 items" })).toBeVisible();
  });

  it("stages one setField operation when dropped on another column", async () => {
    const onStageOps = vi.fn(async () => {});
    render(<ConfiguredViewBoard {...rendererProps({ onStageOps })} />);
    const card = screen.getByRole("article", { name: "" });
    const dataTransfer = dragDataTransfer();

    fireEvent.dragStart(card, { dataTransfer });
    fireEvent.dragEnter(screen.getByLabelText("(empty) column"), { dataTransfer });
    fireEvent.drop(screen.getByLabelText("(empty) column"), { dataTransfer });

    await waitFor(() => expect(onStageOps).toHaveBeenCalledTimes(1));
    expect(onStageOps).toHaveBeenCalledWith([
      expect.objectContaining({
        kind: "setField",
        path: "docs/specs/alpha.md",
        field: "specStatus",
        value: "",
        fieldValue: { kind: "unset" },
      }),
    ]);
  });

  it("shows a staged move in the target column while the view re-executes", () => {
    const editSession: OntologyEditSessionResponse = {
      sessionId: "edit-1",
      status: "dirty",
      hasUncommittedChanges: true,
      createdAt: "2026-09-23T00:00:00Z",
      updatedAt: "2026-09-23T00:00:01Z",
      ops: [
        {
          kind: "setField",
          path: "docs/specs/alpha.md",
          field: "specStatus",
          value: "",
          fieldValue: { kind: "unset" },
        },
      ],
    };

    const { rerender } = render(
      <ConfiguredViewBoard {...rendererProps({ editSession, loading: true })} />,
    );

    expect(within(screen.getByLabelText("(empty) column")).getByRole("article")).toBeVisible();
    expect(within(screen.getByLabelText("Planned column")).queryByRole("article")).toBeNull();
    expect(screen.getByLabelText("Planned column")).toHaveTextContent("2 more");

    // Once the refetch lands, the server's columns are canonical again.
    rerender(<ConfiguredViewBoard {...rendererProps({ editSession, loading: false })} />);
    expect(within(screen.getByLabelText("Planned column")).getByRole("article")).toBeVisible();
  });

  it("slots a staged move where the view's sort will put it", () => {
    const result = boardExecution();
    Object.assign(result.state, { sort: [{ field: "title", direction: "desc" }] });

    const editSession: OntologyEditSessionResponse = {
      sessionId: "edit-1",
      status: "dirty",
      hasUncommittedChanges: true,
      createdAt: "2026-09-23T00:00:00Z",
      updatedAt: "2026-09-23T00:00:01Z",
      ops: [
        {
          kind: "setField",
          path: "docs/specs/alpha.md",
          field: "specStatus",
          value: "active",
          fieldValue: { kind: "scalar", scalar: "active" },
        },
      ],
    };

    render(
      <ConfiguredViewBoard {...rendererProps({ execution: result, editSession, loading: true })} />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Expand Active column, 2 items" }));

    const titles = within(screen.getByLabelText("Active column"))
      .getAllByRole("article")
      .map((card) => card.textContent);

    expect(titles).toEqual([
      expect.stringContaining("Bravo Spec"),
      expect.stringContaining("Alpha Spec"),
    ]);
  });

  it("does not stage a same-column drop", () => {
    const onStageOps = vi.fn(async () => {});
    render(<ConfiguredViewBoard {...rendererProps({ onStageOps })} />);
    const card = screen.getByRole("article", { name: "" });
    const dataTransfer = dragDataTransfer();

    fireEvent.dragStart(card, { dataTransfer });
    fireEvent.drop(screen.getByLabelText("Planned column"), { dataTransfer });

    expect(onStageOps).not.toHaveBeenCalled();
  });

  it("opens the keyboard move menu and stages the target value", async () => {
    const onStageOps = vi.fn(async () => {});
    render(<ConfiguredViewBoard {...rendererProps({ onStageOps })} />);
    const card = screen.getByRole("article", { name: "" });

    card.focus();
    fireEvent.keyDown(card, { key: "m" });
    fireEvent.click(screen.getByRole("menuitem", { name: "Active" }));

    await waitFor(() => expect(onStageOps).toHaveBeenCalledTimes(1));
    expect(onStageOps).toHaveBeenCalledWith([
      expect.objectContaining({
        kind: "setField",
        path: "docs/specs/alpha.md",
        field: "specStatus",
        value: "active",
      }),
    ]);
  });

  it("addresses a note row by path, not the fingerprint of its staged preview", async () => {
    const onStageOps = vi.fn(async () => {});
    const result = boardExecution();

    const rows = result.rows.map((row) => ({
      ...row,
      ref: { ...row.ref, structuralFingerprint: "staged-preview" },
    }));

    render(
      <ConfiguredViewBoard {...rendererProps({ onStageOps, execution: { ...result, rows } })} />,
    );
    const card = screen.getByRole("article", { name: "" });

    card.focus();
    fireEvent.keyDown(card, { key: "m" });
    fireEvent.click(screen.getByRole("menuitem", { name: "Active" }));

    await waitFor(() => expect(onStageOps).toHaveBeenCalledTimes(1));
    expect(onStageOps).toHaveBeenCalledWith([
      expect.objectContaining({ path: "docs/specs/alpha.md", structuralFingerprint: undefined }),
    ]);
  });

  it("reports a failed move instead of leaving a silent rejection", async () => {
    const onStageOps = vi.fn(async () => {
      throw new Error("edit session conflict");
    });

    render(<ConfiguredViewBoard {...rendererProps({ onStageOps })} />);
    const card = screen.getByRole("article", { name: "" });

    card.focus();
    fireEvent.keyDown(card, { key: "m" });
    fireEvent.click(screen.getByRole("menuitem", { name: "Active" }));

    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent(/Could not move .+ to Active: edit session conflict/);
    expect(card).not.toHaveAttribute("aria-busy");
  });

  it("has no move control when the board is read-only", () => {
    const result = boardExecution();
    const onStageOps = vi.fn(async () => {});

    const { rerender } = render(
      <ConfiguredViewBoard {...rendererProps({ execution: result, onStageOps })} />,
    );

    const card = screen.getByRole("article", { name: "" });
    expect(card).toHaveAttribute("draggable", "true");
    expect(screen.getByRole("button", { name: /^Move / })).toBeVisible();

    if (result.board) result.board.editable = false;
    rerender(<ConfiguredViewBoard {...rendererProps({ execution: result, onStageOps })} />);
    expect(screen.getByRole("article", { name: "" })).toHaveAttribute("draggable", "false");
    expect(screen.queryByRole("button", { name: /^Move / })).not.toBeInTheDocument();
    fireEvent.keyDown(screen.getByRole("article", { name: "" }), { key: "m" });
    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
    expect(onStageOps).not.toHaveBeenCalled();
  });

  it("shows the unavailable message when board layout is absent", () => {
    const result = boardExecution();
    result.board = undefined;
    render(<ConfiguredViewBoard {...rendererProps({ execution: result })} />);

    expect(screen.getByText("Board layout is unavailable for this view.")).toBeVisible();
  });

  it("replaces grouping with column and lane pickers in the board shell", () => {
    const view = {
      ...baseView,
      availableVariants: ["table", "kanban", "card"],
      variants: {
        ...baseView.variants,
        card: { title: "title" },
        kanban: { columnField: "frontmatter.spec-status" },
      },
    };

    const result = boardExecution();
    result.view = view;
    renderView({
      view,
      execution: result,
      state: { variant: "kanban", page: { offset: 0, first: 25 } },
      onStageOps: vi.fn(async () => {}),
    });

    expect(screen.queryByLabelText("Group by")).not.toBeInTheDocument();
    expect(screen.getByLabelText("Board columns")).toHaveValue("frontmatter.spec-status");
    expect(screen.getByLabelText("Board lanes")).toHaveValue("none");
    expect(screen.queryByRole("button", { name: "Columns" })).not.toBeInTheDocument();
  });
});

function dragDataTransfer() {
  return {
    effectAllowed: "none",
    dropEffect: "none",
    setData: vi.fn(),
    getData: vi.fn(),
  };
}
