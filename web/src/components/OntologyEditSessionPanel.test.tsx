import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { OntologyEditSessionPanel } from "./OntologyEditSessionPanel";

describe("OntologyEditSessionPanel", () => {
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

    expect(screen.getByRole("status")).toHaveTextContent("Draft");
    expect(screen.getByRole("button", { name: "Save" })).toBeEnabled();
    expect(screen.getByRole("button", { name: "Review" })).toBeDisabled();
    expect(screen.queryByRole("button", { name: "Done" })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Discard" }));
    expect(onDiscard).toHaveBeenCalledOnce();
  });
});
