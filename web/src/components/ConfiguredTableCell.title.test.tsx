import { fireEvent, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { renderWithQueryClient as render } from "../test/renderWithQueryClient";
import { ConfiguredTableCellContent } from "./ConfiguredTableCell";

it.each([false, true])(
  "keeps a custom primary value consistent with its editor (editable: %s)",
  (editable) => {
    render(
      <ConfiguredTableCellContent
        row={{
          ref: { kind: "EMBEDDED", notePath: "tasks.md", nodeId: "task" },
          title: "Full catalog title",
          fields: { name: "Authored name" },
        }}
        field="name"
        columnIndex={0}
        onOpenRow={vi.fn()}
        capability={
          editable
            ? {
                key: "name",
                sortable: false,
                groupable: false,
                edit: { kind: "text", operation: "setField", field: "name" },
              }
            : undefined
        }
        onStageOps={editable ? vi.fn() : undefined}
      />,
    );
    const cell = screen.getByText("Authored name");
    expect(screen.queryByText("Full catalog title")).toBeNull();

    if (editable) {
      fireEvent.keyDown(cell, { key: "F2" });
      expect(screen.getByRole("textbox")).toHaveValue("Authored name");
    }
  },
);
