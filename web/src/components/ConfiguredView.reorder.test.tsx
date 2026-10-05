import { createEvent, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { ViewExecuteResponse, ViewTableRow } from "../api/types";
import { ConfiguredViewBoard } from "./ConfiguredViewBoard";
import { ConfiguredViewCards } from "./ConfiguredViewCards";
import { ConfiguredViewTable } from "./ConfiguredViewTable";
import { configuredCardLayout, execution, rendererProps } from "./ConfiguredView.testFixtures";

function rankedRow(name: string, rank: number): ViewTableRow {
  return {
    ref: { notePath: `ideas/${name}.md`, kind: "NOTE" },
    path: `ideas/${name}.md`,
    title: name,
    fields: { rank, frontmatter: { "spec-status": "draft" } },
  };
}

const rows = [rankedRow("Alpha", 1), rankedRow("Bravo", 2), rankedRow("Charlie", 3)];

/** A view sorted by an editable real `rank`, as Drew's IP opportunities view is. */
function rankedExecution(overrides: Partial<ViewExecuteResponse> = {}): ViewExecuteResponse {
  const base = execution(rows);

  return {
    ...base,
    groups: [],
    // SAFETY: see `execution`; the generated state type admits no literal.
    state: {
      ...base.state,
      sort: [{ field: "rank", direction: "asc" }],
    } as ViewExecuteResponse["state"],
    capabilities: [
      ...(base.capabilities ?? []),
      {
        key: "rank",
        label: "Rank",
        sortable: true,
        indexedSortable: true,
        groupable: false,
        filterOps: ["eq"],
        valueKind: "real",
        edit: { kind: "number", operation: "setField", field: "rank", valueKind: "real" },
      },
    ],
    ...overrides,
  };
}

function props(result: ViewExecuteResponse, onStageOps = vi.fn(async () => {})) {
  return rendererProps({
    execution: result,
    rows: result.rows,
    capabilities: result.capabilities ?? [],
    onStageOps,
  });
}

function fakeDataTransfer() {
  return {
    effectAllowed: "none",
    dropEffect: "none",
    setData: vi.fn(),
    getData: vi.fn(),
    setDragImage: vi.fn(),
  };
}

function closest(name: string, selector: string): HTMLElement {
  const element = screen.getByText(name).closest<HTMLElement>(selector);

  if (!element) throw new Error(`${name} has no ${selector}`);

  return element;
}

const rowElement = (name: string) => closest(name, "tr");

const cardElement = (name: string) => closest(name, "article");

/** Fires a drag event at a pointer position; jsdom drag events otherwise have none. */
function dragAt(
  type: "dragOver" | "drop",
  element: HTMLElement,
  dataTransfer: ReturnType<typeof fakeDataTransfer>,
  at = 1,
) {
  const event = createEvent[type](element, { dataTransfer });
  // jsdom targets have no size, so a negative offset is the leading half and a positive one the trailing half.
  Object.defineProperties(event, { clientX: { value: at }, clientY: { value: at } });
  fireEvent(element, event);
}

function stagedRank(value: string, path: string) {
  return expect.objectContaining({ kind: "setField", path, field: "rank", value });
}

/** A one-column board of the ranked rows. */
function draftBoard() {
  return rankedExecution({
    variant: "kanban",
    card: configuredCardLayout,
    board: {
      columnField: "frontmatter.spec-status",
      editable: true,
      columns: [{ key: "draft", value: "draft", label: "Draft", count: 3, rowStart: 0, rowEnd: 3 }],
    },
  });
}

describe("manual ordering", () => {
  it("stages a midpoint rank when a table row is dropped between two others", async () => {
    const onStageOps = vi.fn(async () => {});
    const { container } = render(<ConfiguredViewTable {...props(rankedExecution(), onStageOps)} />);
    const handle = rowElement("Charlie").querySelector(".configured-view__drag-handle");

    if (!handle) throw new Error("Charlie has no drag handle");
    const transfer = fakeDataTransfer();

    expect(
      container.querySelectorAll(".configured-view__drag-handle[draggable=true]"),
    ).toHaveLength(3);
    fireEvent.dragStart(handle, { dataTransfer: transfer });
    dragAt("dragOver", rowElement("Bravo"), transfer, -1);
    expect(rowElement("Bravo")).toHaveClass("is-drop-before");
    dragAt("drop", rowElement("Bravo"), transfer, -1);

    await waitFor(() => expect(onStageOps).toHaveBeenCalledTimes(1));
    expect(onStageOps).toHaveBeenCalledWith([stagedRank("1.5", "ideas/Charlie.md")]);
  });

  it("moves a focused row with Alt+ArrowUp", async () => {
    const onStageOps = vi.fn(async () => {});
    render(<ConfiguredViewTable {...props(rankedExecution(), onStageOps)} />);

    fireEvent.keyDown(rowElement("Bravo"), { key: "ArrowUp", altKey: true });

    await waitFor(() =>
      expect(onStageOps).toHaveBeenCalledWith([stagedRank("0", "ideas/Bravo.md")]),
    );
  });

  it("is read-only with an explanation while the result is truncated", () => {
    const onStageOps = vi.fn(async () => {});
    const truncated = rankedExecution();
    truncated.pageInfo = { ...truncated.pageInfo, total: 50, hasMore: true };
    const { container } = render(<ConfiguredViewTable {...props(truncated, onStageOps)} />);

    const handle = container.querySelector(".configured-view__drag-handle");
    expect(handle).toHaveAttribute("title", "Show all items to reorder");
    expect(handle).not.toHaveAttribute("draggable", "true");
    fireEvent.keyDown(rowElement("Bravo"), { key: "ArrowUp", altKey: true });
    expect(onStageOps).not.toHaveBeenCalled();
  });

  it("shows no handle when the sort field cannot be edited", () => {
    const byTitle = rankedExecution();
    // SAFETY: see `execution`; the generated state type admits no literal.
    byTitle.state = {
      ...byTitle.state,
      sort: [{ field: "title", direction: "asc" }],
    } as ViewExecuteResponse["state"];
    const { container } = render(<ConfiguredViewTable {...props(byTitle)} />);

    expect(container.querySelector(".configured-view__drag-handle")).toBeNull();
  });

  it("reorders cards by dropping beside another card", async () => {
    const onStageOps = vi.fn(async () => {});
    render(
      <ConfiguredViewCards
        {...props(rankedExecution({ card: configuredCardLayout }), onStageOps)}
      />,
    );
    const transfer = fakeDataTransfer();

    expect(cardElement("Alpha")).toHaveAttribute("draggable", "true");
    fireEvent.dragStart(cardElement("Alpha"), { dataTransfer: transfer });
    dragAt("dragOver", cardElement("Charlie"), transfer);
    dragAt("drop", cardElement("Charlie"), transfer);

    await waitFor(() =>
      expect(onStageOps).toHaveBeenCalledWith([stagedRank("4", "ideas/Alpha.md")]),
    );
  });

  it("places a board card within its column", async () => {
    const onStageOps = vi.fn(async () => {});

    const board = rankedExecution({
      variant: "kanban",
      card: configuredCardLayout,
      board: {
        columnField: "frontmatter.spec-status",
        editable: true,
        columns: [
          { key: "draft", value: "draft", label: "Draft", count: 3, rowStart: 0, rowEnd: 3 },
        ],
      },
    });

    render(<ConfiguredViewBoard {...props(board, onStageOps)} />);
    const transfer = fakeDataTransfer();

    fireEvent.dragStart(cardElement("Charlie"), { dataTransfer: transfer });
    dragAt("dragOver", cardElement("Alpha"), transfer, -1);
    dragAt("drop", cardElement("Alpha"), transfer, -1);

    await waitFor(() =>
      expect(onStageOps).toHaveBeenCalledWith([stagedRank("0", "ideas/Charlie.md")]),
    );
  });

  it("moves a focused card with Alt+ArrowLeft without moving focus to its neighbor", async () => {
    const onStageOps = vi.fn(async () => {});
    render(
      <ConfiguredViewCards
        {...props(rankedExecution({ card: configuredCardLayout }), onStageOps)}
      />,
    );

    cardElement("Bravo").focus();
    fireEvent.keyDown(cardElement("Bravo"), { key: "ArrowLeft", altKey: true });

    expect(cardElement("Bravo")).toHaveFocus();
    await waitFor(() =>
      expect(onStageOps).toHaveBeenCalledWith([stagedRank("0", "ideas/Bravo.md")]),
    );
  });

  it("stages nothing when a card is dropped back on itself", async () => {
    const onStageOps = vi.fn(async () => {});
    render(<ConfiguredViewBoard {...props(draftBoard(), onStageOps)} />);
    const transfer = fakeDataTransfer();

    fireEvent.dragStart(cardElement("Alpha"), { dataTransfer: transfer });

    for (const at of [-1, 1]) {
      dragAt("dragOver", cardElement("Alpha"), transfer, at);
      expect(cardElement("Alpha").className).not.toMatch(/is-drop/);
    }

    dragAt("drop", cardElement("Alpha"), transfer);
    fireEvent.dragEnd(cardElement("Alpha"), { dataTransfer: transfer });

    render(
      <ConfiguredViewCards
        {...props(rankedExecution({ card: configuredCardLayout }), onStageOps)}
      />,
    );
    const cards = screen.getAllByText("Bravo").map((node) => node.closest<HTMLElement>("article"));
    const card = cards.at(-1);

    if (!card) throw new Error("Bravo has no card");
    fireEvent.dragStart(card, { dataTransfer: transfer });
    dragAt("dragOver", card, transfer);
    dragAt("drop", card, transfer);

    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(onStageOps).not.toHaveBeenCalled();
  });

  it("places a card dropped on another column's background first in that column", async () => {
    const onStageOps = vi.fn(async () => {});
    const board = draftBoard();
    board.rows = [
      { ...rankedRow("Alpha", 5), fields: { rank: 5, frontmatter: { "spec-status": "active" } } },
      rankedRow("Bravo", 2),
      rankedRow("Charlie", 3),
    ];
    board.board = {
      columnField: "frontmatter.spec-status",
      editable: true,
      columns: [
        { key: "active", value: "active", label: "Active", count: 1, rowStart: 0, rowEnd: 1 },
        { key: "draft", value: "draft", label: "Draft", count: 2, rowStart: 1, rowEnd: 3 },
      ],
    };
    render(<ConfiguredViewBoard {...props(board, onStageOps)} />);
    const transfer = fakeDataTransfer();
    const draft = screen.getByLabelText("Draft column");

    fireEvent.dragStart(cardElement("Alpha"), { dataTransfer: transfer });
    fireEvent.dragEnter(draft, { dataTransfer: transfer });
    fireEvent.dragOver(draft, { dataTransfer: transfer });
    fireEvent.drop(draft, { dataTransfer: transfer });

    await waitFor(() => expect(onStageOps).toHaveBeenCalledTimes(1));
    expect(onStageOps).toHaveBeenCalledWith([
      expect.objectContaining({ path: "ideas/Alpha.md", field: "specStatus", value: "draft" }),
      stagedRank("1", "ideas/Alpha.md"),
    ]);
  });

  it("skips past equal enum values when moving by keyboard, and offers no drop among them", async () => {
    const onStageOps = vi.fn(async () => {});

    const status = (name: string, value: string): ViewTableRow => ({
      ...rankedRow(name, 0),
      fields: { frontmatter: { "spec-status": value } },
    });

    const byStatus = rankedExecution({
      rows: [status("Alpha", "active"), status("Bravo", "active"), status("Charlie", "draft")],
    });

    // SAFETY: see `execution`; the generated state type admits no literal.
    byStatus.state = {
      ...byStatus.state,
      sort: [{ field: "frontmatter.spec-status", direction: "asc" }],
    } as ViewExecuteResponse["state"];

    byStatus.capabilities = (byStatus.capabilities ?? []).map((capability) =>
      capability.key === "frontmatter.spec-status"
        ? { ...capability, indexedSortable: true, valueKind: "enum" }
        : capability,
    );
    render(<ConfiguredViewTable {...props(byStatus, onStageOps)} />);
    const handle = rowElement("Alpha").querySelector(".configured-view__drag-handle");

    if (!handle) throw new Error("Alpha has no drag handle");
    const transfer = fakeDataTransfer();
    fireEvent.dragStart(handle, { dataTransfer: transfer });
    dragAt("dragOver", rowElement("Bravo"), transfer, 1);
    expect(rowElement("Bravo").className).not.toMatch(/is-drop/);
    fireEvent.dragEnd(handle, { dataTransfer: transfer });

    fireEvent.keyDown(rowElement("Alpha"), { key: "ArrowDown", altKey: true });

    await waitFor(() =>
      expect(onStageOps).toHaveBeenCalledWith([
        expect.objectContaining({ path: "ideas/Alpha.md", field: "specStatus", value: "draft" }),
      ]),
    );
  });

  it("shows a staged cross-group move in its new group", () => {
    const grouped = rankedExecution({
      rows: [
        { ...rankedRow("Alpha", 1), fields: { rank: 1, frontmatter: { "spec-status": "active" } } },
        // Bravo's staged value has moved it out of the Draft group the server returned.
        { ...rankedRow("Bravo", 2), fields: { rank: 2, frontmatter: { "spec-status": "active" } } },
        rankedRow("Charlie", 3),
      ],
    });

    grouped.groups = [
      {
        field: "frontmatter.spec-status",
        key: "active",
        label: "Active",
        value: "active",
        count: 1,
        depth: 0,
        rowStart: 0,
        rowEnd: 1,
      },
      {
        field: "frontmatter.spec-status",
        key: "draft",
        label: "Draft",
        value: "draft",
        count: 2,
        depth: 0,
        rowStart: 1,
        rowEnd: 3,
      },
    ];
    const { container } = render(<ConfiguredViewTable {...props(grouped)} showStagedEdits />);

    const order = [...container.querySelectorAll("tbody tr")].map((row) => row.textContent ?? "");
    const draftHeader = order.findIndex((text) => text.includes("Draft"));
    const bravo = order.findIndex((text) => text.includes("Bravo"));

    expect(draftHeader).toBeGreaterThan(-1);
    expect(bravo).toBeLessThan(draftHeader);
  });

  it("reorders a row within each group of a list field it appears in", async () => {
    // Grouped by a list of links, as IP ideas are by opportunity: Bravo is in both groups.
    const grouped = rankedExecution({
      rows: [
        rankedRow("Alpha", 1),
        rankedRow("Bravo", 2),
        rankedRow("Bravo", 2),
        rankedRow("Charlie", 3),
      ],
    });

    grouped.groups = ["One", "Two"].map((label, index) => ({
      field: "opportunities",
      key: label,
      label,
      value: label,
      count: 2,
      depth: 0,
      rowStart: index * 2,
      rowEnd: index * 2 + 2,
    }));
    const onStageOps = vi.fn(async () => {});
    render(<ConfiguredViewTable {...props(grouped, onStageOps)} />);
    const [, bravoInTwo] = screen.getAllByText("Bravo").map((name) => name.closest("tr"));

    if (!bravoInTwo) throw new Error("Bravo is not in the second group");
    const transfer = fakeDataTransfer();
    const handle = bravoInTwo.querySelector(".configured-view__drag-handle");

    if (!handle) throw new Error("Bravo has no drag handle");
    fireEvent.dragStart(handle, { dataTransfer: transfer });
    dragAt("drop", rowElement("Charlie"), transfer, 1);

    await waitFor(() =>
      expect(onStageOps).toHaveBeenCalledWith([stagedRank("4", "ideas/Bravo.md")]),
    );

    onStageOps.mockClear();
    bravoInTwo.focus();
    fireEvent.keyDown(bravoInTwo, { key: "ArrowDown", altKey: true });

    await waitFor(() =>
      expect(onStageOps).toHaveBeenCalledWith([stagedRank("4", "ideas/Bravo.md")]),
    );
    // Focus stays on the copy that moved, not the first copy in the other group.
    await new Promise((resolve) => window.requestAnimationFrame(resolve));
    expect(bravoInTwo).toHaveFocus();
  });

  it("moves a board card within its column with Alt+ArrowDown", async () => {
    const onStageOps = vi.fn(async () => {});

    const board = rankedExecution({
      variant: "kanban",
      card: configuredCardLayout,
      board: {
        columnField: "frontmatter.spec-status",
        editable: true,
        columns: [
          { key: "draft", value: "draft", label: "Draft", count: 3, rowStart: 0, rowEnd: 3 },
        ],
      },
    });

    render(<ConfiguredViewBoard {...props(board, onStageOps)} />);
    fireEvent.keyDown(cardElement("Alpha"), { key: "ArrowDown", altKey: true });

    await waitFor(() =>
      expect(onStageOps).toHaveBeenCalledWith([stagedRank("2.5", "ideas/Alpha.md")]),
    );
  });
});
