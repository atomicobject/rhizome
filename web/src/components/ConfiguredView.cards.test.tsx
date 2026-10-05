import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { OntologyEditSessionResponse } from "../api/types";
import { ConfiguredViewCard } from "./ConfiguredViewCard";
import { ConfiguredViewCards } from "./ConfiguredViewCards";
import {
  actionItemRow,
  boardExecution,
  configuredCardLayout,
  rendererProps,
} from "./ConfiguredView.testFixtures";

describe("ConfiguredViewCards", () => {
  it("renders one counted section per group and hides a collapsed group", () => {
    const result = boardExecution();
    result.card = configuredCardLayout;
    result.groups = [
      {
        field: "frontmatter.spec-status",
        key: "active",
        label: "Active",
        value: "active",
        count: 1,
        totalCount: 4,
        tone: "progress",
        depth: 0,
        rowStart: 0,
        rowEnd: 1,
      },
      {
        field: "frontmatter.spec-status",
        key: "draft",
        label: "Draft",
        value: "draft",
        count: 1,
        tone: "neutral",
        depth: 0,
        rowStart: 1,
        rowEnd: 2,
        collapsedByDefault: true,
      },
    ];

    render(<ConfiguredViewCards {...rendererProps({ execution: result })} />);

    expect(screen.getByRole("button", { name: /Active\s*4/ })).toHaveAttribute(
      "aria-expanded",
      "true",
    );
    expect(screen.getByRole("button", { name: /Draft\s*1/ })).toHaveAttribute(
      "aria-expanded",
      "false",
    );
    expect(screen.getByRole("button", { name: "Alpha Spec" })).toBeVisible();
    expect(screen.queryByRole("button", { name: "Bravo Spec" })).not.toBeInTheDocument();
  });

  it("opens a row from its title button", () => {
    const onOpenRow = vi.fn();
    render(<ConfiguredViewCards {...rendererProps({ onOpenRow })} />);

    fireEvent.click(screen.getByRole("button", { name: "Alpha Spec" }));
    expect(onOpenRow).toHaveBeenCalledWith(
      expect.objectContaining({ title: "Alpha Spec" }),
      "activate",
    );
  });

  it("opens a read-only relation without opening or dragging the source card", () => {
    const onOpenRow = vi.fn();
    const onDragStart = vi.fn();

    const row = {
      ...actionItemRow,
      relationValues: {
        assignedTo: [
          {
            value: "[[people/alice.md|Aliased Alice]]",
            ref: { notePath: "people/alice.md", kind: "NOTE" as const },
            title: "Alice Example",
          },
        ],
      },
    };

    render(
      <ConfiguredViewCard
        row={row}
        layout={{ title: { field: "title" }, fields: [{ field: "assignedTo" }] }}
        mode="board"
        capabilities={[
          {
            key: "assignedTo",
            label: "Assigned To",
            valueKind: "relation",
            importance: "NORMAL",
            sortable: true,
            groupable: true,
          },
        ]}
        viewID="action-items.default"
        onOpenRow={onOpenRow}
        draggable
        onDragStart={onDragStart}
      />,
    );

    const relation = screen.getByRole("link", { name: "Alice Example" });

    fireEvent.click(relation);
    expect(onOpenRow).toHaveBeenCalledTimes(1);
    expect(onOpenRow).toHaveBeenCalledWith(
      expect.objectContaining({
        path: "people/alice.md",
        ref: expect.objectContaining({ notePath: "people/alice.md" }),
      }),
      "activate",
    );

    fireEvent.dragStart(relation);
    expect(onDragStart).not.toHaveBeenCalled();
  });

  it("renders relation and enum card slots with their structured presentation", () => {
    const row = {
      ...actionItemRow,
      fields: {
        ...actionItemRow.fields,
        assignedTo: "[[people/alice.md|Doe, Jane]]",
        statuses: ["active", "blocked"],
      },
      relationValues: {
        assignedTo: [
          {
            value: "[[people/alice.md|Doe, Jane]]",
            ref: { notePath: "people/alice.md", kind: "NOTE" as const },
            title: "Jane Doe",
          },
        ],
      },
    };

    const { container } = render(
      <ConfiguredViewCard
        row={row}
        layout={{
          title: { field: "title" },
          eyebrow: { field: "assignedTo" },
          preview: { field: "statuses" },
          fields: [],
        }}
        mode="board"
        capabilities={[
          {
            key: "assignedTo",
            label: "Assigned To",
            valueKind: "relation",
            sortable: true,
            groupable: true,
          },
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
        ]}
        viewID="action-items.default"
        onOpenRow={() => {}}
      />,
    );

    expect(screen.getByRole("link", { name: "Jane Doe" })).toBeVisible();
    expect(container.querySelector(".configured-card__eyebrow")).not.toHaveTextContent("[[");
    expect(screen.getByText("In progress").closest(".status-mark")).toHaveClass(
      "status-mark--progress",
    );
    expect(screen.getByText("Needs help").closest(".status-mark")).toHaveClass("status-mark--risk");
  });

  it("marks a card whose row has staged changes", () => {
    const editSession: OntologyEditSessionResponse = {
      sessionId: "session",
      status: "dirty",
      hasUncommittedChanges: true,
      createdAt: "2026-09-19T00:00:00Z",
      updatedAt: "2026-09-19T00:00:00Z",
      ops: [{ kind: "setField", path: "docs/specs/alpha.md", field: "specStatus" }],
    };

    render(<ConfiguredViewCards {...rendererProps({ editSession })} />);

    expect(screen.getByText("staged")).toBeVisible();
    expect(screen.getByText("staged").closest("article")).toHaveClass("is-staged");
  });

  it("falls back to title-only cards without a card layout", () => {
    const result = boardExecution();
    result.card = undefined;
    result.groups = [];
    const { container } = render(<ConfiguredViewCards {...rendererProps({ execution: result })} />);

    expect(screen.getByRole("button", { name: "Alpha Spec" })).toBeVisible();
    expect(screen.getByRole("button", { name: "Bravo Spec" })).toBeVisible();
    expect(container.querySelector(".configured-card__fields")).not.toBeInTheDocument();
  });
});
