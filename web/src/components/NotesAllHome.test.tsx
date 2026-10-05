import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { OntologySummaryResponse, OntologyTypeResponse } from "../api/types";
import { NotesAllHome } from "./NotesAllHome";

const baseSummary: OntologySummaryResponse = {
  schemaPresent: true,
  totalNotes: 2,
  typedNotes: 2,
  untypedNotes: 0,
  ambiguousNotes: 0,
  issueNotes: 1,
  types: [
    {
      name: "Plan",
      label: "Plan",
      count: 1,
      issueCount: 1,
      role: "note",
    },
    {
      name: "Spec",
      label: "Spec",
      count: 1,
      issueCount: 0,
      role: "note",
    },
  ],
  interfaces: [],
};

const baseTypeDetail: OntologyTypeResponse = {
  count: 2,
  issueCount: 1,
  notes: [
    {
      ref: { notePath: "specs/100-demo/plan.md", kind: "NOTE" },
      path: "specs/100-demo/plan.md",
      title: "Demo plan",
      relationCount: 2,
      updatedAt: 1_713_000_000,
      resolvedType: "Plan",
      hasIssues: true,
    },
    {
      ref: { notePath: "specs/100-demo/spec.md", kind: "NOTE" },
      path: "specs/100-demo/spec.md",
      title: "Demo spec",
      relationCount: 1,
      updatedAt: 1_713_000_100,
      resolvedType: "Spec",
      hasIssues: false,
    },
  ],
};

describe("NotesAllHome", () => {
  it("opens rows beside with Cmd or Ctrl and uses the canonical note target", () => {
    const onOpenNote = vi.fn();
    render(
      <NotesAllHome
        typeDetail={baseTypeDetail}
        summary={baseSummary}
        onOpenIssues={() => {}}
        onOpenNote={onOpenNote}
      />,
    );
    const row = screen.getAllByRole("button", { name: /Demo spec/ })[0];
    fireEvent.click(row);
    expect(onOpenNote).toHaveBeenLastCalledWith("specs/100-demo/spec.md", "activate");
    fireEvent.click(row, { metaKey: true });
    expect(onOpenNote).toHaveBeenLastCalledWith("specs/100-demo/spec.md", "beside");
    fireEvent.click(row, { ctrlKey: true });
    expect(onOpenNote).toHaveBeenLastCalledWith("specs/100-demo/spec.md", "beside");
  });

  it("does not present zero counts as trusted data when unavailable", () => {
    const onRetry = vi.fn();
    render(
      <NotesAllHome
        summary={null}
        typeDetail={null}
        unavailable="Rhizome could not connect."
        onRetry={onRetry}
        onOpenNote={() => {}}
        onOpenIssues={() => {}}
      />,
    );

    expect(screen.getByText("Workspace data is unavailable")).toBeVisible();
    expect(screen.queryByText("0 notes")).toBeNull();
    screen.getByRole("button", { name: "Retry connection" }).click();
    expect(onRetry).toHaveBeenCalledOnce();
  });

  it("renders the summary workstreams and forwards collection actions", () => {
    const onOpenIssues = vi.fn();
    render(
      <NotesAllHome
        summary={baseSummary}
        typeDetail={baseTypeDetail}
        onOpenNote={() => {}}
        onOpenIssues={onOpenIssues}
      />,
    );

    expect(screen.getByText("Recently changed")).toBeVisible();
    expect(screen.getByText("Most linked")).toBeVisible();
    screen.getByRole("button", { name: "All 1 issue" }).click();
    expect(onOpenIssues).toHaveBeenCalledOnce();
  });
});
