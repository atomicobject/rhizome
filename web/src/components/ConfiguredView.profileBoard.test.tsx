import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { OntologyEditSessionResponse, ViewExecuteResponse } from "../api/types";
import { ConfiguredViewBoard } from "./ConfiguredViewBoard";
import { idea, ideaBoard, ideaView } from "./ConfiguredView.profileFixtures";
import { rendererProps, renderView } from "./ConfiguredView.testFixtures";

/** Ideas laid out in priority lanes: Alpha is high, Bravo and Charlie low. */
function priorityLanes() {
  const result = ideaBoard();

  result.board = {
    ...result.board!,
    laneField: "priority",
    lanes: [
      {
        key: "priority=high",
        value: "high",
        label: "High",
        tone: "warning",
        count: 1,
        cells: [{ value: "captured", rows: [0] }],
      },
      {
        key: "priority=low",
        value: "low",
        label: "Low",
        count: 2,
        cells: [{ value: "pursuing", rows: [1, 2] }],
      },
    ],
  };

  return result;
}

/**
 * Board renderer props with a working stage callback, so a read-only board
 * is read-only because the board says so, never for want of a callback.
 */
function boardProps(execution: ViewExecuteResponse, { editable = true } = {}) {
  return rendererProps({
    execution: { ...execution, board: { ...execution.board!, editable } },
    onStageOps: vi.fn(async () => {}),
  });
}

/** Cards move only on an editable board. */
function expectCardsMovable(editable: boolean) {
  for (const card of screen.getAllByRole("article")) {
    expect(card).toHaveAttribute("draggable", String(editable));
  }
}

const BOARD_STATES = [
  { state: "editable", editable: true },
  { state: "read-only", editable: false },
];

const dataTransfer = {
  effectAllowed: "none",
  dropEffect: "none",
  setData: vi.fn(),
  getData: vi.fn(),
};

function drag(title: string, cellLabel: string) {
  const card = screen.getAllByRole("button", { name: title })[0]!.closest("article")!;
  const cell = screen.getByLabelText(cellLabel);
  fireEvent.dragStart(card, { dataTransfer });
  fireEvent.dragEnter(cell, { dataTransfer });
  fireEvent.drop(cell, { dataTransfer });
}

function laneCount(name: string) {
  return screen.getByRole("region", { name }).querySelector(".configured-view__board-count")
    ?.textContent;
}

function cardTitles(container: HTMLElement) {
  return within(container)
    .queryAllByRole("article")
    .map((card) => card.querySelector(".configured-card__title")?.textContent);
}

describe("ConfiguredViewBoard with a type profile", () => {
  it("draws empty and collapsed values as strips, and an empty strip takes a drop", async () => {
    const onStageOps = vi.fn(async () => {});
    const result = ideaBoard(undefined, { lanes: false });
    render(<ConfiguredViewBoard {...rendererProps({ execution: result, onStageOps })} />);

    expect(screen.getByRole("button", { name: "Expand Parked column, 0 items" })).toBeVisible();
    const strip = screen.getByLabelText("Exploring column");
    expect(strip).toHaveClass("is-empty");

    const alpha = screen.getByRole("button", { name: "Alpha" }).closest("article");

    const dataTransfer = {
      effectAllowed: "none",
      dropEffect: "none",
      setData: vi.fn(),
      getData: vi.fn(),
    };

    fireEvent.dragStart(alpha!, { dataTransfer });
    fireEvent.dragEnter(strip, { dataTransfer });
    fireEvent.drop(strip, { dataTransfer });

    await waitFor(() => expect(onStageOps).toHaveBeenCalledTimes(1));
    expect(onStageOps).toHaveBeenCalledWith([
      expect.objectContaining({ kind: "setField", field: "status", value: "exploring" }),
    ]);
  });

  it.each(BOARD_STATES)(
    "splits cards into the server's lanes and says how many others hold a card when $state",
    ({ editable }) => {
      render(<ConfiguredViewBoard {...boardProps(ideaBoard(), { editable })} />);
      expectCardsMovable(editable);

      const agents = screen.getByRole("region", { name: "Agents lane" });
      const products = screen.getByRole("region", { name: "Products lane" });
      const none = screen.getByRole("region", { name: "No opportunity lane" });

      expect(cardTitles(agents)).toEqual(["Alpha", "Bravo"]);
      expect(cardTitles(products)).toEqual(["Bravo"]);
      expect(cardTitles(none)).toEqual(["Charlie"]);
      expect(within(agents).getByText("+1 lane")).toBeVisible();
      expect(within(none).queryByText(/lane$/)).toBeNull();
      // Lane headers carry the target's status mark and count, and collapse.
      expect(agents.querySelector(".status-mark--progress")).not.toBeNull();
      fireEvent.click(within(agents).getByRole("button", { expanded: true }));
      expect(cardTitles(agents)).toEqual([]);
    },
  );

  it("moves a card to another lane by staging the lane value with the column", async () => {
    const onStageOps = vi.fn(async () => {});
    render(<ConfiguredViewBoard {...rendererProps({ execution: priorityLanes(), onStageOps })} />);

    drag("Alpha", "Pursuing column, Low lane");

    await waitFor(() => expect(onStageOps).toHaveBeenCalledTimes(1));
    expect(onStageOps).toHaveBeenCalledWith([
      expect.objectContaining({ field: "status", value: "pursuing" }),
      expect.objectContaining({ field: "priority", value: "low" }),
    ]);
  });

  it("shows a staged lane move in the target lane while the view re-executes", () => {
    const editSession: OntologyEditSessionResponse = {
      sessionId: "edit-1",
      status: "dirty",
      hasUncommittedChanges: true,
      createdAt: "2026-10-03T00:00:00Z",
      updatedAt: "2026-10-03T00:00:01Z",
      ops: [{ kind: "setField", path: "ideas/Bravo.md", field: "priority", value: "high" }],
    };

    const props = { ...boardProps(priorityLanes()), editSession, loading: true };
    const { rerender } = render(<ConfiguredViewBoard {...props} />);

    expect(cardTitles(screen.getByLabelText("Pursuing column, High lane"))).toEqual(["Bravo"]);
    expect(cardTitles(screen.getByLabelText("Pursuing column, Low lane"))).toEqual(["Charlie"]);
    // Lane headers count the staged move too.
    expect(laneCount("High lane")).toBe("2");
    expect(laneCount("Low lane")).toBe("1");

    // Once the refetch lands, the server's lanes are canonical again.
    rerender(<ConfiguredViewBoard {...props} loading={false} />);
    expect(cardTitles(screen.getByLabelText("Pursuing column, Low lane"))).toEqual([
      "Bravo",
      "Charlie",
    ]);
    expect(laneCount("Low lane")).toBe("2");
  });

  it("changes only the lane for a drop in the card's own column", async () => {
    const onStageOps = vi.fn(async () => {});
    render(<ConfiguredViewBoard {...rendererProps({ execution: priorityLanes(), onStageOps })} />);

    drag("Bravo", "Pursuing column, High lane");

    await waitFor(() => expect(onStageOps).toHaveBeenCalledTimes(1));
    expect(onStageOps).toHaveBeenCalledWith([
      expect.objectContaining({ field: "priority", value: "high" }),
    ]);
  });

  it("takes drops only in a card's own lanes when the lane field cannot be set", async () => {
    const onStageOps = vi.fn(async () => {});
    render(<ConfiguredViewBoard {...rendererProps({ execution: ideaBoard(), onStageOps })} />);

    // Opportunities is a link field with no single-value edit.
    drag("Charlie", "Captured column, Agents lane");
    expect(screen.getByLabelText("Captured column, Agents lane")).not.toHaveClass("is-drop-target");
    expect(onStageOps).not.toHaveBeenCalled();

    drag("Charlie", "Captured column, No opportunity lane");
    await waitFor(() => expect(onStageOps).toHaveBeenCalledTimes(1));
    expect(onStageOps).toHaveBeenCalledWith([
      expect.objectContaining({ field: "status", value: "captured" }),
    ]);
  });

  it("keeps collapsed lanes to their lane field", () => {
    const props = boardProps(ideaBoard());
    const { container, rerender } = render(<ConfiguredViewBoard {...props} />);
    const agents = () => screen.getByRole("region", { name: "Agents lane" });

    fireEvent.click(within(agents()).getByRole("button", { expanded: true }));
    expect(cardTitles(agents())).toEqual([]);

    rerender(<ConfiguredViewBoard {...props} execution={priorityLanes()} />);

    const laneToggles = [
      ...container.querySelectorAll(".configured-view__board-lane-header button"),
    ];

    expect(laneToggles.length).toBeGreaterThan(0);
    expect(laneToggles.map((toggle) => toggle.getAttribute("aria-expanded"))).not.toContain(
      "false",
    );

    rerender(<ConfiguredViewBoard {...props} />);
    expect(cardTitles(agents())).toEqual([]);
  });

  it.each(BOARD_STATES)(
    "draws no lanes when the server laid out none when $state",
    ({ editable }) => {
      render(
        <ConfiguredViewBoard
          {...boardProps(ideaBoard(undefined, { lanes: false }), { editable })}
        />,
      );

      expectCardsMovable(editable);
      expect(screen.queryByRole("region", { name: /^[^,]+ lane$/ })).toBeNull();
      expect(screen.getAllByRole("button", { name: "Bravo" })).toHaveLength(1);
    },
  );

  it("shows the profile's card content without the column and lane fields", () => {
    render(<ConfiguredViewBoard {...boardProps(ideaBoard(undefined, { lanes: false }))} />);
    const alpha = screen.getByRole("button", { name: "Alpha" }).closest("article")!;

    expect(alpha).toHaveTextContent("Alpha summary");
    expect(within(alpha).getByText("Next step")).toBeVisible();
    expect(alpha).toHaveTextContent("Alpha next step");
    expect(within(alpha).getByText("High").closest(".status-mark")).toHaveClass(
      "status-mark--warning",
    );
    // With no lanes, the first relation field shows as chips.
    expect(within(alpha).getByRole("link", { name: "Agents" })).toBeVisible();
    expect(alpha).toHaveTextContent("3 ideas");
    expect(within(alpha).getByTitle("Drew Colthorp")).toHaveTextContent("DC");
    expect(within(alpha).getByText("3d")).toBeVisible();
    expect(within(alpha).queryByText("Captured")).toBeNull();
  });

  it("marks active cards unchanged for 30 days and counts them in the column header", () => {
    const { container } = render(
      <ConfiguredViewBoard {...boardProps(ideaBoard(undefined, { lanes: false }))} />,
    );

    const bravo = screen.getByRole("button", { name: "Bravo" }).closest("article")!;
    const charlie = screen.getByRole("button", { name: "Charlie" }).closest("article")!;

    expect(within(bravo).getByText("stale")).toBeVisible();
    expect(within(charlie).queryByText("stale")).toBeNull();

    const headers = [...container.querySelectorAll(".configured-view__board-column-header")];
    expect(headers.map((header) => header.textContent)).toEqual([
      expect.not.stringContaining("stale"),
      expect.stringContaining("1 stale"),
    ]);
  });

  it.each(BOARD_STATES)(
    "renders compact cards in a column holding more than 24 when $state",
    ({ editable }) => {
      const rows = Array.from({ length: 25 }, (_, index) => idea(`Idea ${index}`, "captured"));
      render(
        <ConfiguredViewBoard {...boardProps(ideaBoard(rows, { lanes: false }), { editable })} />,
      );

      const cards = screen.getAllByRole("article");
      expect(cards).toHaveLength(25);
      expectCardsMovable(editable);
      expect(cards[0]).toHaveClass("configured-card--compact");
      expect(cards[0]).not.toHaveTextContent("summary");
    },
  );

  it("renders an authored card layout compact too", () => {
    const rows = Array.from({ length: 25 }, (_, index) => idea(`Idea ${index}`, "captured"));
    const result = ideaBoard(rows, { lanes: false });
    result.view = {
      ...result.view,
      generated: false,
      variants: { kanban: { columnField: "status", card: { title: "title", preview: "summary" } } },
    };
    result.card = { title: { field: "title" }, preview: { field: "summary" }, fields: [] };

    const { container } = render(<ConfiguredViewBoard {...boardProps(result)} />);

    expect(screen.getAllByRole("article")[0]).toHaveClass("configured-card--compact");
    expect(container.querySelector(".configured-card__preview")).toBeNull();
  });

  it("switches the column and lane fields from the toolbar", () => {
    const onStateChange = vi.fn();
    const view = { ...ideaView, availableVariants: ["table", "kanban"] };
    const result = { ...ideaBoard(), view };

    renderView({
      view,
      execution: result,
      state: { variant: "kanban" },
      onStateChange,
      onStageOps: vi.fn(async () => {}),
    });

    const lanes = screen.getByLabelText("Board lanes");
    expect(lanes).toHaveValue("opportunities");
    expect([...lanes.querySelectorAll("option")].map((option) => option.textContent)).toEqual([
      "None",
      "Priority",
      "Owner",
      "Opportunities",
    ]);

    fireEvent.change(lanes, { target: { value: "none" } });
    expect(onStateChange).toHaveBeenLastCalledWith(expect.objectContaining({ laneField: "none" }));

    fireEvent.change(screen.getByLabelText("Board columns"), { target: { value: "priority" } });
    expect(onStateChange).toHaveBeenLastCalledWith(
      expect.objectContaining({ columnField: "priority" }),
    );
  });
});
