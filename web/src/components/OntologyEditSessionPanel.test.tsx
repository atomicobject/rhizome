import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { OntologyEditSessionPanel } from "./OntologyEditSessionPanel";

describe("OntologyEditSessionPanel", () => {
  it("explains why Save is disabled when editing has no changes", () => {
    render(
      <OntologyEditSessionPanel
        session={null}
        editing
        onStartEditing={vi.fn()}
        onReview={vi.fn()}
        onCommit={vi.fn()}
        onDiscard={vi.fn()}
      />,
    );
    expect(screen.getByRole("status")).toHaveTextContent("Editing");
    expect(screen.getByText("No changes")).toBeVisible();
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Done" })).toBeEnabled();
  });
  it("offers Save and an explicit Discard while an unstaged local draft exists", () => {
    const onCommit = vi.fn();
    const onDiscard = vi.fn();
    render(
      <OntologyEditSessionPanel
        session={null}
        editing
        hasLocalDrafts
        onStartEditing={vi.fn()}
        onReview={vi.fn()}
        onCommit={onCommit}
        onDiscard={onDiscard}
      />,
    );

    expect(screen.getByRole("status")).toHaveTextContent("Unsaved changes");
    expect(screen.getByRole("button", { name: "Save" })).toBeEnabled();
    expect(screen.queryByRole("button", { name: "Review" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Done" })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Discard" }));
    expect(onDiscard).toHaveBeenCalledOnce();
  });
});
