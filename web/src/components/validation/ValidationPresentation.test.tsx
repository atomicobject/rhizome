import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { ValidateEnvelope } from "../../api/types";
import { ValidationIssueBadge } from "./ValidationIssueBadge";
import { ValidationStatusStrip } from "./ValidationStatusStrip";

const envelope: ValidateEnvelope = {
  status: "ok",
  health: "incomplete",
  generation: 3,
  publishedGeneration: 3,
  snapshot: {
    vaultIdentity: "vault",
    generation: 3,
    scope: "default",
    selectedChecks: ["ontology", "broken_links"],
    startedAt: 1,
    finishedAt: 2,
    durationMs: 7,
    completion: "incomplete",
    issueCount: 1,
    errorCount: 1,
    affectedFileCount: 1,
    affectedNoteCount: 1,
    repairActionCount: 0,
    checks: [
      { check: "ontology", outcome: "completed", issueCount: 1, durationMs: 4 },
      {
        check: "broken_links",
        outcome: "blocked",
        issueCount: 0,
        durationMs: 3,
        summary: "Index unavailable",
      },
    ],
  },
};

describe("validation presentation", () => {
  it("hides zero badges and gives status-only badges an accessible label", () => {
    const { rerender } = render(<ValidationIssueBadge count={0} label="No issues" />);
    expect(screen.queryByLabelText("No issues")).toBeNull();

    rerender(<ValidationIssueBadge count={0} health="stale" label="No stale issues" />);
    expect(screen.queryByLabelText("No stale issues")).toBeNull();

    rerender(
      <ValidationIssueBadge count={null} health="stale" label="Validation results are stale" />,
    );
    expect(screen.getByLabelText("Validation results are stale")).toHaveTextContent("old");
  });

  it("discloses scope and blocked checks without relying on color", () => {
    render(<ValidationStatusStrip envelope={envelope} />);
    expect(screen.getByRole("status")).toHaveTextContent("Validation incomplete");
    fireEvent.click(screen.getByText("Check details"));
    expect(screen.getByText("default")).toBeVisible();
    expect(screen.getByText("blocked")).toBeVisible();
    expect(screen.getByText("Index unavailable")).toBeVisible();
  });

  it("makes a scoped count actionable", () => {
    const open = vi.fn();
    render(<ValidationIssueBadge count={3} label="3 issues in this note" onClick={open} />);
    fireEvent.click(screen.getByRole("button", { name: "3 issues in this note" }));
    expect(open).toHaveBeenCalledOnce();
  });
});
