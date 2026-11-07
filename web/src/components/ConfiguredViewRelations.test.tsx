import { describe, expect, it, vi } from "vitest";

import { renderWithQueryClient } from "../test/renderWithQueryClient";
import { actionItemRow } from "./ConfiguredView.testFixtures";
import { RelationChips } from "./ConfiguredViewRelations";

describe("RelationChips", () => {
  it("marks a linked record by its status word, not its schema tone", () => {
    const { container } = renderWithQueryClient(
      <RelationChips
        row={actionItemRow}
        onOpenRow={vi.fn()}
        fact={{
          field: "spec",
          label: "Spec",
          values: [
            {
              value: "[[specs/alpha.md]]",
              ref: { notePath: "specs/alpha.md", kind: "NOTE" },
              title: "Alpha",
              // Active specs sit on a done-stage schema tone; the word still wins.
              status: { value: "active", label: "Active", tone: "success" },
            },
          ],
        }}
      />,
    );

    expect(container.querySelector(".status-mark--progress")).not.toBeNull();
    expect(container.querySelector(".status-mark--success")).toBeNull();
  });
});
