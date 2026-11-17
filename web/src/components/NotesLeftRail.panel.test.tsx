import { screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { OntologySummaryResponse, ViewCatalogEntry } from "../api/types";
import { withFakeFetch } from "../test/fakeFetch";
import { renderWithQueryClient as render } from "../test/renderWithQueryClient";
import { ExperienceFrames } from "./experienceFrames";
import { NotesLeftRail, type NotesLeftRailProps } from "./NotesLeftRail";
import type { PanelServices } from "./SidebarPanelView";

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

  it("docks Files below the type tree, open, revealing the active note", async () => {
    render(<NotesLeftRail {...props()} vaultKey="/vault" activePanel={notePanel} />);

    const rail = await screen.findByRole("complementary", { name: "Notes navigation" });
    const panel = within(rail).getByRole("region", { name: "Files panel" });
    const collections = within(rail).getByRole("region", { name: "Collections" });

    // The panel follows the navigation in the sidebar.
    expect(
      collections.compareDocumentPosition(panel) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    expect(within(panel).getByRole("button", { name: "Files" })).toHaveAttribute(
      "aria-expanded",
      "true",
    );
    expect(await within(panel).findByRole("button", { name: "second-plan.md" })).toBeVisible();
    expect(within(panel).getByRole("button", { name: "demo-plan.md" })).toHaveAttribute(
      "aria-current",
      "page",
    );
  });

  it("keeps Files mounted while the next subject's panel is still unknown", async () => {
    const view = render(<NotesLeftRail {...props()} vaultKey="/vault" activePanel={notePanel} />);
    const panel = await screen.findByRole("region", { name: "Files panel" });

    // A note tab whose node is still loading reports null.
    view.rerender(<NotesLeftRail {...props()} vaultKey="/vault" activePanel={null} />);
    expect(screen.getByRole("region", { name: "Files panel" })).toBe(panel);
  });

  it("holds only Files for a target that resolves to none", async () => {
    render(
      <NotesLeftRail
        {...props()}
        vaultKey="/vault"
        activePanel={{ panel: { kind: "none" }, context: { kind: "type", type: "Plan" } }}
      />,
    );

    await screen.findByRole("region", { name: "Collections" });
    expect(
      screen
        .getAllByRole("region", { name: /panel$/ })
        .map((region) => region.getAttribute("aria-label")),
    ).toEqual(["Files panel"]);
  });

  it("replaces Files with an experience panel instead of stacking them", async () => {
    const source = { kind: "query_recipe", queryRecipe: "delivery" } as const;
    const mount = { kind: "type", type: "Plan" } as const;

    const delivery: ViewCatalogEntry = {
      id: "delivery.panel",
      name: "Delivery",
      source,
      mount,
      defaults: {},
      variants: {},
      definition: {
        apiVersion: "rhizome.view.v1",
        id: "delivery.panel",
        name: "Delivery",
        source,
        mount,
        variants: {},
      },
    };

    http.json("GET", "/api/v1/views", { views: [delivery] });

    const frames = new ExperienceFrames({
      fetchInvocation: () => new Promise(() => undefined),
      fetchGeneration: async () => "g1",
    });

    const panelServices: PanelServices = {
      onOpenNote: vi.fn(),
      onStageOps: vi.fn(async () => undefined),
    };

    const shared = { ...props(), vaultKey: "/vault", frames, panelServices };

    const view = render(
      <NotesLeftRail
        {...shared}
        activePanel={{
          panel: { kind: "view", viewId: delivery.id, experienceKey: "delivery" },
          context: { kind: "type", type: "Plan" },
        }}
      />,
    );

    const panels = () =>
      screen
        .getAllByRole("region", { name: /panel$/ })
        .map((region) => region.getAttribute("aria-label"));

    await screen.findByRole("region", { name: "Delivery panel" });
    expect(panels()).toEqual(["Delivery panel"]);

    // Leaving the experience gives the slot back to Files.
    view.rerender(<NotesLeftRail {...shared} activePanel={notePanel} />);
    await screen.findByRole("region", { name: "Files panel" });
    expect(panels()).toEqual(["Files panel"]);
  });
});
