import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { OntologyNoteListItem } from "../api/types";
import { withFakeFetch } from "../test/fakeFetch";
import { renderWithQueryClient as render } from "../test/renderWithQueryClient";
import { HomeInfoBand } from "./HomeInfoBand";

function note(path: string, title: string): OntologyNoteListItem {
  return { ref: { notePath: path, kind: "NOTE" }, path, title, hasIssues: false };
}

const notes = ["a", "b", "c", "d"].map((name) => note(`${name}.md`, `Note ${name}`));

describe("HomeInfoBand", () => {
  const http = withFakeFetch();

  it("lists the notes with the most issues and links to the full Problems view", () => {
    const onOpenIssues = vi.fn();
    const onOpenNote = vi.fn();
    render(
      <HomeInfoBand
        label="Specs"
        notes={notes}
        noteIssueCounts={
          new Map([
            ["a.md", 1],
            ["b.md", 0],
            ["c.md", 5],
            ["d.md", 2],
          ])
        }
        health="current_issues"
        issueCount={8}
        onOpenNote={onOpenNote}
        onOpenIssues={onOpenIssues}
      />,
    );

    const problems = screen.getByRole("heading", { name: "Problems" }).closest("section")!;
    const rows = within(problems).getAllByRole("button", { name: /Note/ });
    expect(rows.map((row) => row.textContent)).toEqual([
      "Note c5 issues",
      "Note d2 issues",
      "Note a1 issue",
    ]);
    fireEvent.click(rows[0]);
    expect(onOpenNote).toHaveBeenCalledWith("c.md", "activate");
    fireEvent.click(within(problems).getByRole("button", { name: "All 8 issues" }));
    expect(onOpenIssues).toHaveBeenCalledOnce();
  });

  it("does not claim a clean result before issue counts arrive", () => {
    const props = { label: "Specs", notes, onOpenNote: vi.fn() };

    const { rerender } = render(
      <HomeInfoBand {...props} noteIssueCounts={new Map()} health="current_issues" />,
    );

    expect(screen.getByText("Checking…")).toBeVisible();

    rerender(
      <HomeInfoBand {...props} noteIssueCounts={new Map([["a.md", 0]])} health="current_clean" />,
    );
    expect(screen.getByText("No problems in these notes.")).toBeVisible();

    rerender(<HomeInfoBand {...props} noteIssueCounts={new Map()} health="never_checked" />);
    expect(screen.getByText("Not checked yet.")).toBeVisible();
  });

  it("previews a row using its canonical embedded identity", async () => {
    const embedded: OntologyNoteListItem = {
      ref: {
        notePath: "specs/demo.md",
        fragment: "item-17",
        kind: "EMBEDDED",
        structuralFingerprint: "criterion-fingerprint",
      },
      path: "specs/demo.md#item-17",
      title: "Acceptance criterion",
      hasIssues: false,
      updatedAt: 10,
    };

    http.json("GET", "/api/v1/nodes/preview", {
      ref: "specs/demo.md#struct:criterion-fingerprint",
      path: "specs/demo.md",
      title: "Acceptance criterion preview",
      format: "markdown",
      fragmentResolved: true,
      fields: [],
      hasIssues: false,
    });

    render(
      <HomeInfoBand
        label="Specs"
        notes={[embedded]}
        noteIssueCounts={new Map()}
        health="current_clean"
        onOpenNote={vi.fn()}
      />,
    );

    const recent = screen.getByRole("heading", { name: "Recently changed" }).closest("section")!;
    fireEvent.focus(within(recent).getByRole("button", { name: /Acceptance criterion/ }));

    await waitFor(() =>
      expect(http.requests("GET", "/api/v1/nodes/preview")[0]?.query.get("ref")).toBe(
        "specs/demo.md#struct:criterion-fingerprint",
      ),
    );
    expect(
      await screen.findByRole("region", { name: "Preview of Acceptance criterion preview" }),
    ).toBeVisible();
  });
});
