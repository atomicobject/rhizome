import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { OntologyTypeResponse } from "../api/types";
import { NotesTypeHome } from "./NotesTypeHome";

describe("NotesTypeHome", () => {
  it("opens rows beside with Cmd or Ctrl and uses the canonical note target", () => {
    const onOpenNote = vi.fn();
    render(
      <NotesTypeHome
        typeDetail={{
          count: 1,
          issueCount: 0,
          notes: [
            {
              ref: {
                notePath: "specs/demo.md",
                kind: "EMBEDDED",
                structuralFingerprint: "stable-row",
              },
              path: "specs/demo.md#node:old",
              title: "Demo row",
              updatedAt: 1_713_000_000,
              hasIssues: false,
            },
          ],
        }}

        onOpenNote={onOpenNote}
      />,
    );
    const row = screen.getAllByRole("button", { name: /Demo row/ })[0];
    fireEvent.click(row);
    expect(onOpenNote).toHaveBeenLastCalledWith("specs/demo.md#struct:stable-row", "activate");
    fireEvent.click(row, { metaKey: true });
    expect(onOpenNote).toHaveBeenLastCalledWith("specs/demo.md#struct:stable-row", "beside");
    fireEvent.click(row, { ctrlKey: true });
    expect(onOpenNote).toHaveBeenLastCalledWith("specs/demo.md#struct:stable-row", "beside");
  });

  it("does not show empty-success conclusions while type data is loading or unavailable", () => {
    const onRetry = vi.fn();
    const props = { typeDetail: null, onOpenNote: vi.fn(), onRetry };
    const { rerender } = render(<NotesTypeHome {...props} loading />);
    expect(screen.getByText("Loading type…")).toBeVisible();
    expect(screen.queryByText("No open problems.")).toBeNull();
    rerender(<NotesTypeHome {...props} unavailable="Type service failed" />);
    expect(screen.getByRole("alert")).toHaveTextContent("Type service failed");
    expect(screen.queryByText("No modified notes yet.")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(onRetry).toHaveBeenCalledOnce();
  });

  describe("identity-status pill", () => {
    const specTypeDetail: OntologyTypeResponse = {
      count: 2,
      issueCount: 0,
      type: {
        name: "Spec",
        label: "Spec",
        description: "",
        fields: [
          {
            name: "specStatus",
            kind: "enum",
            typeName: "SpecStatus",
            enumValues: ["proposed", "active", "archived"],
            source: "spec-status",
          },
          { name: "summary", kind: "scalar", typeName: "String" },
        ],
      },
      notes: [
        {
          ref: { notePath: "specs/active.md", kind: "NOTE" },
          path: "specs/active.md",
          title: "Active spec",
          updatedAt: 1_713_000_000,
          hasIssues: false,
          identityStatus: "active",
        },
        {
          ref: { notePath: "specs/empty.md", kind: "NOTE" },
          path: "specs/empty.md",
          title: "Empty spec",
          updatedAt: 1_713_000_000,
          hasIssues: false,
        },
      ],
    };

    const personTypeDetail: OntologyTypeResponse = {
      count: 1,
      issueCount: 0,
      type: {
        name: "Person",
        label: "Person",
        description: "",
        fields: [{ name: "summary", kind: "scalar", typeName: "String" }],
      },
      notes: [
        {
          ref: { notePath: "people/alice.md", kind: "NOTE" },
          path: "people/alice.md",
          title: "Alice",
          updatedAt: 1_713_000_000,
          hasIssues: false,
          identityStatus: "active",
        },
      ],
    };

    it("renders a read-only status pill for notes that carry an identityStatus value", () => {
      render(<NotesTypeHome typeDetail={specTypeDetail} onOpenNote={() => {}} />);

      const activeRow = screen.getByText("Active spec").closest("button");
      expect(activeRow).not.toBeNull();
      const pill = activeRow?.querySelector('.widget-enum--badge[data-value="active"]');
      expect(pill).not.toBeNull();
      expect(pill?.textContent).toBe("Active");
      // Read-only — no segmented edit controls
      expect(activeRow?.querySelector(".widget-enum--segmented")).toBeNull();
    });

    it("omits the pill on cards whose identityStatus is empty", () => {
      render(<NotesTypeHome typeDetail={specTypeDetail} onOpenNote={() => {}} />);

      const emptyRow = screen.getByText("Empty spec").closest("button");
      expect(emptyRow).not.toBeNull();
      expect(emptyRow?.querySelector(".widget-enum")).toBeNull();
    });

    it("renders no pill on any card for a type that has no identity-status field", () => {
      render(<NotesTypeHome typeDetail={personTypeDetail} onOpenNote={() => {}} />);

      const personRow = screen.getByText("Alice").closest("button");
      expect(personRow).not.toBeNull();
      expect(personRow?.querySelector(".widget-enum")).toBeNull();
    });
  });
});
