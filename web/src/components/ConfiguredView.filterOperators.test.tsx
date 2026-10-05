import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { execution, renderView } from "./ConfiguredView.testFixtures";

describe("configured view filter operators", () => {
  it("keeps neq when choosing an enum value and switches its single selection", async () => {
    const onStateChange = vi.fn();
    renderView({
      execution: {
        ...execution(),
        capabilities: [
          {
            key: "status",
            label: "Status",
            valueKind: "enum",
            sortable: true,
            groupable: true,
            filterOps: ["eq", "neq", "in", "exists"],
            enumValues: [{ value: "active" }, { value: "archived" }],
          },
        ],
      },
      onStateChange,
    });
    fireEvent.click(screen.getByRole("button", { name: "Add filter" }));
    fireEvent.change(screen.getByLabelText("Operator"), { target: { value: "neq" } });
    fireEvent.click(screen.getByRole("button", { name: "Active" }));
    await waitFor(() =>
      expect(onStateChange).toHaveBeenLastCalledWith(
        expect.objectContaining({
          filters: [{ field: "status", op: "neq", value: "active" }],
        }),
      ),
    );
    fireEvent.click(screen.getByRole("button", { name: "Archived" }));
    expect(screen.getByRole("button", { name: "Active" })).toHaveAttribute("aria-pressed", "false");
    expect(screen.getByRole("button", { name: "Archived" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    await waitFor(() =>
      expect(onStateChange).toHaveBeenLastCalledWith(
        expect.objectContaining({
          filters: [{ field: "status", op: "neq", value: "archived" }],
        }),
      ),
    );
    fireEvent.change(screen.getByLabelText("Operator"), { target: { value: "exists" } });
    expect(screen.queryByRole("group", { name: "Values" })).toBeNull();
    await waitFor(() =>
      expect(onStateChange).toHaveBeenLastCalledWith(
        expect.objectContaining({
          filters: [expect.objectContaining({ field: "status", op: "exists" })],
        }),
      ),
    );
  });

  it.each(["eq", "neq"])(
    "preserves visible selections through %s and in transitions",
    async (op) => {
      const onStateChange = vi.fn();
      renderView({
        execution: {
          ...execution(),
          capabilities: [
            {
              key: "status",
              label: "Status",
              valueKind: "enum",
              sortable: true,
              groupable: true,
              filterOps: ["eq", "neq", "in"],
              enumValues: [{ value: "active" }, { value: "archived" }],
            },
          ],
        },
        onStateChange,
      });
      fireEvent.click(screen.getByRole("button", { name: "Add filter" }));
      fireEvent.change(screen.getByLabelText("Operator"), { target: { value: op } });
      fireEvent.click(screen.getByRole("button", { name: "Active" }));
      fireEvent.change(screen.getByLabelText("Operator"), { target: { value: "in" } });
      expect(screen.getByRole("button", { name: "Active" })).toHaveAttribute(
        "aria-pressed",
        "true",
      );
      await waitFor(() =>
        expect(onStateChange).toHaveBeenLastCalledWith(
          expect.objectContaining({ filters: [{ field: "status", op: "in", values: ["active"] }] }),
        ),
      );
      fireEvent.click(screen.getByRole("button", { name: "Archived" }));
      await waitFor(() =>
        expect(onStateChange).toHaveBeenLastCalledWith(
          expect.objectContaining({
            filters: [{ field: "status", op: "in", values: ["active", "archived"] }],
          }),
        ),
      );
      fireEvent.change(screen.getByLabelText("Operator"), { target: { value: op } });
      expect(screen.getByRole("button", { name: "Active" })).toHaveAttribute(
        "aria-pressed",
        "true",
      );
      expect(screen.getByRole("button", { name: "Archived" })).toHaveAttribute(
        "aria-pressed",
        "false",
      );
      await waitFor(() =>
        expect(onStateChange).toHaveBeenLastCalledWith(
          expect.objectContaining({ filters: [{ field: "status", op, value: "active" }] }),
        ),
      );
    },
  );

  it("uses exclusive radio choices for large scalar enums and checkboxes for in", async () => {
    const onStateChange = vi.fn();
    renderView({
      execution: {
        ...execution(),
        capabilities: [
          {
            key: "status",
            label: "Status",
            valueKind: "enum",
            sortable: true,
            groupable: true,
            filterOps: ["eq", "neq", "in"],
            enumValues: Array.from({ length: 13 }, (_, i) => ({ value: `status-${i}` })),
          },
        ],
      },
      onStateChange,
    });
    fireEvent.click(screen.getByRole("button", { name: "Add filter" }));
    fireEvent.change(screen.getByLabelText("Operator"), { target: { value: "neq" } });
    fireEvent.click(screen.getByText("Select values"));
    expect(screen.getAllByRole("radio")).toHaveLength(13);
    const filters = screen.getByRole("group", { name: "Values" });
    expect(within(filters).queryAllByRole("checkbox")).toHaveLength(0);
    fireEvent.click(screen.getByRole("radio", { name: "Status 0" }));
    fireEvent.click(screen.getByRole("radio", { name: "Status 1" }));
    expect(screen.getByRole("radio", { name: "Status 0" })).not.toBeChecked();
    expect(screen.getByRole("radio", { name: "Status 1" })).toBeChecked();
    await waitFor(() =>
      expect(onStateChange).toHaveBeenLastCalledWith(
        expect.objectContaining({ filters: [{ field: "status", op: "neq", value: "status-1" }] }),
      ),
    );
    fireEvent.change(screen.getByLabelText("Operator"), { target: { value: "in" } });
    expect(screen.queryAllByRole("radio")).toHaveLength(0);
    expect(
      within(screen.getByRole("group", { name: "Values" })).getAllByRole("checkbox"),
    ).toHaveLength(13);
    expect(screen.getByRole("checkbox", { name: "Status 1" })).toBeChecked();
    fireEvent.click(screen.getByRole("checkbox", { name: "Status 2" }));
    expect(screen.getByRole("checkbox", { name: "Status 1" })).toBeChecked();
    expect(screen.getByRole("checkbox", { name: "Status 2" })).toBeChecked();
    fireEvent.click(screen.getByRole("checkbox", { name: "Status 1" }));
    await waitFor(() =>
      expect(onStateChange).toHaveBeenLastCalledWith(
        expect.objectContaining({ filters: [{ field: "status", op: "in", values: ["status-2"] }] }),
      ),
    );
  });
});
