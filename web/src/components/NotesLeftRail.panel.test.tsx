import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { OntologySummaryResponse } from "../api/types";
import { withFakeFetch } from "../test/fakeFetch";
import { renderWithQueryClient as render } from "../test/renderWithQueryClient";
import { NotesLeftRail, type NotesLeftRailProps } from "./NotesLeftRail";

// The Notes sidebar's panel slot as the rail holds it (SPEC-0136 R6).

const http = withFakeFetch();

const summary: OntologySummaryResponse = {
  schemaPresent: true,
  totalNotes: 2,
  typedNotes: 2,
  untypedNotes: 0,
  ambiguousNotes: 0,
  issueNotes: 0,
  types: [{ name: "Plan", label: "Plan", count: 2, issueCount: 0, role: "note" }],
  interfaces: [],
};

function props(): NotesLeftRailProps {
  return {
    summary,
    selectedType: "__all__",
    activeCollection: "__all__",
    activeViewID: null,
    editSession: null,
    touchedPaths: new Set(),
    onSelectCollection: vi.fn(),
    onOpenConfiguredView: vi.fn(),
  };
}

describe("the sidebar panel", () => {
  const notePanel: NotesLeftRailProps["activePanel"] = {
    panel: { kind: "folder" },
    context: { kind: "node", type: "Plan", ref: { notePath: "plans/demo-plan.md", kind: "NOTE" } },
  };

  beforeEach(() => {
    window.localStorage.clear();
    http.json("GET", "/api/v1/views", { views: [] });
    http.json("GET", "/api/v2/validate", { status: "ok", generation: 1 });
    http.json("GET", "/api/v1/graphs/folders", {
      files: [
        { id: "a", kind: "note", label: "Demo plan", notePath: "plans/demo-plan.md" },
        { id: "b", kind: "note", label: "Second plan", notePath: "plans/second-plan.md" },
      ],
      links: [],
      nestedTypes: [],
      truncated: false,
    });
  });

  it("holds Folder below the type tree for a note, minimised until opened", async () => {
    render(<NotesLeftRail {...props()} vaultKey="/vault" activePanel={notePanel} />);

    const rail = await screen.findByRole("complementary", { name: "Notes navigation" });
    const panel = within(rail).getByRole("region", { name: "Folder panel" });
    const collections = within(rail).getByRole("region", { name: "Collections" });

    // The panel follows the navigation in the sidebar.
    expect(
      collections.compareDocumentPosition(panel) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    const title = within(panel).getByRole("button", { name: "Folder" });
    await waitFor(() => expect(title).toHaveAttribute("aria-expanded", "false"));
    // Minimised, Folder reads nothing.
    expect(http.count("GET", "/api/v1/graphs/folders")).toBe(0);

    fireEvent.click(title);
    await waitFor(() => expect(title).toHaveAttribute("aria-expanded", "true"));
    expect(await within(panel).findByRole("button", { name: "Second plan" })).toBeVisible();
    expect(within(panel).getByRole("button", { name: "Demo plan" })).toHaveAttribute(
      "aria-current",
      "page",
    );
  });

  it("holds no panel for a target that resolves to none", async () => {
    render(
      <NotesLeftRail
        {...props()}
        vaultKey="/vault"
        activePanel={{ panel: { kind: "none" }, context: { kind: "type", type: "Plan" } }}
      />,
    );

    await screen.findByRole("region", { name: "Collections" });
    expect(screen.queryByRole("region", { name: "Folder panel" })).toBeNull();
  });
});
