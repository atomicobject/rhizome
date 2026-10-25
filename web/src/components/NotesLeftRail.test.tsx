import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type {
  OntologyEditSessionResponse,
  OntologyNoteListItem,
  OntologySummaryResponse,
  OntologyTypeResponse,
  ViewCatalogEntry,
} from "../api/types";
import { jsonReply, withFakeFetch } from "../test/fakeFetch";
import { renderWithQueryClient as render } from "../test/renderWithQueryClient";
import { NotesLeftRail, type NotesLeftRailProps } from "./NotesLeftRail";

const http = withFakeFetch();

const planNote = note("plans/demo-plan.md", "Demo plan", "Plan", 10);

const specNote = note("specs/demo-spec.md", "Demo spec", "Spec", 20);

const summary: OntologySummaryResponse = {
  schemaPresent: true,
  totalNotes: 2,
  typedNotes: 2,
  untypedNotes: 0,
  ambiguousNotes: 0,
  issueNotes: 2,
  types: [
    { name: "Plan", label: "Plan", count: 1, issueCount: 0, role: "note" },
    { name: "Spec", label: "Spec", count: 1, issueCount: 1, role: "note" },
  ],
  interfaces: [
    {
      name: "Document",
      label: "Document",
      count: 2,
      issueCount: 1,
      implementors: ["Plan", "Spec"],
    },
  ],
};

const presentationSummary: OntologySummaryResponse = {
  ...summary,
  types: [
    {
      name: "EffortNote",
      label: "Effort note",
      pluralLabel: "Effort notes",
      displayParent: "Effort",
      count: 3,
      role: "note",
    },
    {
      name: "EffortWorkspace",
      label: "Effort workspace",
      pluralLabel: "Effort workspaces",
      displayParent: "Effort",
      count: 2,
      role: "note",
    },
    {
      name: "Material",
      label: "Material",
      pluralLabel: "Materials",
      displayParent: "EffortWorkspace",
      count: 1,
      role: "note",
    },
    {
      name: "Alpha",
      label: "Alpha item",
      pluralLabel: "Alpha items",
      displayGroup: "Knowledge",
      count: 1,
      role: "note",
    },
    {
      name: "Zeta",
      label: "Zeta item",
      pluralLabel: "Zeta items",
      displayGroup: "Knowledge",
      count: 1,
      role: "note",
    },
    { name: "Loose", label: "Loose item", pluralLabel: "Loose items", count: 1, role: "note" },
  ],
  interfaces: [
    {
      name: "Effort",
      label: "Effort",
      pluralLabel: "Efforts",
      displayGroup: "Delivery",
      count: 2,
      issueCount: 0,
      implementors: ["EffortNote", "EffortWorkspace"],
    },
  ],
};

function note(
  path: string,
  title: string,
  resolvedType: string,
  updatedAt: number,
): OntologyNoteListItem {
  return {
    ref: { notePath: path, kind: "NOTE" },
    path,
    title,
    resolvedType,
    hasIssues: resolvedType === "Spec",
    updatedAt,
  };
}

function typeDetail(typeName: string, notes: OntologyNoteListItem[]): OntologyTypeResponse {
  return {
    count: notes.length,
    issueCount: notes.filter((item) => item.hasIssues).length,
    type: { name: typeName, label: typeName, fields: [] },
    notes,
  };
}

const allNotes = typeDetail("All", [planNote, specNote]);

const coverageView: ViewCatalogEntry = {
  id: "coverage",
  name: "Coverage",
  source: { kind: "query_recipe", queryRecipe: "coverage" },
  mount: { kind: "standalone", group: "Views", order: 1 },
  defaults: {},
  variants: { table: { columns: [{ field: "title", label: "Title" }] } },
  definition: {
    apiVersion: "rhizome.view.v1",
    id: "coverage",
    name: "Coverage",
    source: { kind: "query_recipe", queryRecipe: "coverage" },
    mount: { kind: "standalone", group: "Views", order: 1 },
    variants: { table: { columns: [{ field: "title", label: "Title" }] } },
  },
};

const planView: ViewCatalogEntry = {
  id: "plans.default",
  name: "Plan table",
  source: { kind: "ontology_type", type: "Plan" },
  mount: { kind: "type", type: "Plan", default: true },
  defaults: {},
  variants: { table: { columns: [{ field: "title", label: "Title" }] } },
  definition: {
    apiVersion: "rhizome.view.v1",
    id: "plans.default",
    name: "Plan table",
    source: { kind: "ontology_type", type: "Plan" },
    mount: { kind: "type", type: "Plan", default: true },
    variants: { table: { columns: [{ field: "title", label: "Title" }] } },
  },
};

function editSession(): OntologyEditSessionResponse {
  return {
    sessionId: "edit-1",
    status: "dirty",
    ops: [],
    hasUncommittedChanges: true,
    touchedPaths: [specNote.path],
    createdAt: "2026-09-06T00:00:00Z",
    updatedAt: "2026-09-06T00:00:01Z",
  };
}

function props(overrides: Partial<NotesLeftRailProps> = {}): NotesLeftRailProps {
  return {
    summary,
    selectedType: "__all__",
    activeCollection: overrides.selectedType ?? "__all__",
    activeViewID: null,
    editSession: null,
    touchedPaths: new Set(),
    onSelectCollection: vi.fn(),
    onOpenConfiguredView: vi.fn(),
    ...overrides,
  };
}

function registerRailData(
  typeName: string,
  detail: OntologyTypeResponse,
  views: ViewCatalogEntry[] = [],
) {
  http
    .json("GET", `/api/v1/ontology/types/${typeName}`, detail)
    .json("POST", `/api/v1/ontology/types/${typeName}`, detail)
    .json("GET", "/api/v1/views", { views })
    .json("GET", "/api/v2/validate", {
      status: "ok",
      generation: 1,
      result: {
        ok: false,
        issueCount: 2,
        errorCount: 0,
        durationMs: 1,
        selectedChecks: [],
        checks: [],
      },
    });
}

describe("NotesLeftRail", () => {
  beforeEach(() => {
    window.localStorage.clear();
    registerRailData("__all__", allNotes, [coverageView, planView]);
  });

  it("lists no notes and never loads a type's note list", async () => {
    render(<NotesLeftRail {...props({ selectedType: "Plan", activeCollection: "Plan" })} />);
    expect(await screen.findByRole("region", { name: "Collections" })).toBeVisible();
    expect(screen.queryByRole("region", { name: "Note list" })).not.toBeInTheDocument();
    expect(screen.queryByRole("searchbox")).not.toBeInTheDocument();
    await waitFor(() => expect(http.count("GET", "/api/v1/views")).toBe(1));
    expect(http.requests().some((request) => request.url.includes("/ontology/types/"))).toBe(false);
  });

  it("lists types without counts while the first index after a start rebuilds", async () => {
    const rebuilding: OntologySummaryResponse = {
      ...summary,
      rebuilding: true,
      totalNotes: 0,
      typedNotes: 0,
      issueNotes: 0,
      types: (summary.types ?? []).map((type) => ({ ...type, count: 0, issueCount: 0 })),
    };

    render(<NotesLeftRail {...props({ summary: rebuilding })} />);
    fireEvent.click(await screen.findByRole("button", { name: "Expand Other" }));
    fireEvent.click(screen.getByRole("button", { name: "Expand Document" }));

    expect(await screen.findByRole("button", { name: "Plan" })).toBeVisible();
    expect(screen.queryByRole("button", { name: "Plan (0)" })).not.toBeInTheDocument();
  });

  it("keeps type groups the way the user left them after a remount", async () => {
    const first = render(<NotesLeftRail {...props()} />);
    fireEvent.click(await screen.findByRole("button", { name: "Expand Other" }));
    first.unmount();

    render(<NotesLeftRail {...props()} />);

    expect(await screen.findByRole("button", { name: "Collapse Other" })).toBeVisible();
  });

  it("exposes the current collection, the open note, its change, and type issues by name", async () => {
    http.json("GET", "/api/v2/validate", {
      status: "ok",
      generation: 1,
      snapshot: { generation: 1, issueCount: 2 },
    });
    http.on("POST", "/api/v1/validation/summaries", (request) => {
      // SAFETY: the rail sends the typed validation-summary request.
      const body = JSON.parse(request.body || "{}") as {
        generation?: number;
        scopes?: Array<{ kind: string; key?: string }>;
      };

      return jsonReply({
        generation: body.generation ?? 0,
        summaries: (body.scopes ?? []).map((scope) => ({
          scope,
          issueCount: scope.kind === "type" && scope.key === "Spec" ? 2 : 0,
          affectedFileCount: 0,
          affectedNoteCount: 0,
          repairActionCount: 0,
        })),
      });
    });
    render(<NotesLeftRail {...props({ activeCollection: "Spec" })} />);

    fireEvent.click(await screen.findByRole("button", { name: "Expand Other" }));
    fireEvent.click(screen.getByRole("button", { name: "Expand Document" }));
    const spec = await screen.findByRole("button", { name: "Spec (1), 2 validation issues" });
    expect(spec).toHaveAttribute("aria-current", "page");
    expect(screen.getByRole("button", { name: "All notes (2)" })).not.toHaveAttribute(
      "aria-current",
    );
    // Problems shows its count in the risk color only while there are problems.
    expect(
      screen.getByRole("button", { name: "Problems 2" }).querySelector(".is-risk"),
    ).toHaveTextContent("2");
  });

  it("names the vault, then quick items, views, and types", async () => {
    const openView = vi.fn();
    const openRecent = vi.fn();
    registerRailData("__all__", allNotes, [coverageView, planView]);
    render(
      <NotesLeftRail
        {...props({
          vaultName: "/vaults/demo",
          vaultPath: "/vaults/demo",
          activeViewID: coverageView.id,
          editSession: editSession(),
          onOpenConfiguredView: openView,
          onOpenRecent: openRecent,
          touchedPaths: new Set([specNote.path]),
        })}
      />,
    );

    const rail = await screen.findByRole("complementary", { name: "Notes navigation" });
    expect(within(rail).getByTitle("/vaults/demo")).toHaveTextContent(/^demo$/);
    const collections = await within(rail).findByRole("region", { name: "Collections" });
    expect(collections).toHaveClass("notes-left-rail__collections");

    expect(within(collections).getByRole("button", { name: "Coverage" })).toHaveClass(
      "is-selected",
    );
    expect(within(collections).getByRole("button", { name: "Problems" })).toBeVisible();
    expect(within(collections).getByRole("button", { name: "Changes 1" })).toBeVisible();
    expect(within(collections).getByRole("button", { name: "All notes (2)" })).toBeVisible();
    fireEvent.click(within(collections).getByRole("button", { name: "Recently changed" }));
    expect(openRecent).toHaveBeenCalled();
    fireEvent.click(within(collections).getByRole("button", { name: "Coverage" }));
    expect(openView).toHaveBeenCalledWith(coverageView);

    // Views is one collapsible section.
    fireEvent.click(within(collections).getByRole("button", { name: "Views" }));
    expect(within(collections).queryByRole("button", { name: "Coverage" })).toBeNull();
    fireEvent.click(within(collections).getByRole("button", { name: "Views" }));

    fireEvent.click(within(collections).getByRole("button", { name: "Expand Other" }));
    fireEvent.click(within(collections).getByRole("button", { name: "Expand Document" }));
    expect(within(collections).getByRole("button", { name: "Plan (1)" })).toBeVisible();
    expect(within(collections).getByRole("button", { name: "Spec (1)" })).toBeVisible();
    expect(
      within(collections).queryByRole("button", { name: "Open Plan table for Plan" }),
    ).toBeNull();
  });

  it("keeps the sidebar to one Tab stop that follows arrow keys", async () => {
    render(<NotesLeftRail {...props({ activeCollection: null })} />);

    const collections = await screen.findByRole("region", { name: "Collections" });
    await within(collections).findByRole("button", { name: "Coverage" });
    const rows = () => [...collections.querySelectorAll<HTMLButtonElement>("[data-rail-row]")];
    const tabbable = () => rows().filter((row) => row.tabIndex === 0);

    expect(tabbable()).toEqual([rows()[0]]);
    expect(
      within(collections)
        .getAllByRole("button", { name: /^(Expand|Collapse) / })
        .every((caret) => caret.tabIndex === -1),
    ).toBe(true);

    rows()[0].focus();
    fireEvent.keyDown(rows()[0], { key: "ArrowDown" });
    expect(rows()[1]).toHaveFocus();
    await waitFor(() => expect(tabbable()).toEqual([rows()[1]]));
    fireEvent.keyDown(rows()[1], { key: "End" });
    const other = within(collections).getByRole("button", { name: "Other" });
    expect(other).toHaveFocus();

    // Right opens the focused row's section and Left closes it.
    fireEvent.keyDown(other, { key: "ArrowRight" });
    expect(within(collections).getByRole("button", { name: "Collapse Other" })).toBeVisible();
    fireEvent.keyDown(other, { key: "ArrowLeft" });
    expect(within(collections).getByRole("button", { name: "Expand Other" })).toBeVisible();
    fireEvent.keyDown(other, { key: "Home" });
    expect(rows()[0]).toHaveFocus();
  });

  it("names a group once by showing its views inside the matching type group", async () => {
    const knowledgeView: ViewCatalogEntry = {
      ...coverageView,
      id: "knowledge-board",
      name: "Knowledge board",
      mount: { kind: "standalone", group: "Knowledge", order: 1 },
    };

    registerRailData("__all__", allNotes, [coverageView, knowledgeView]);
    render(<NotesLeftRail {...props({ summary: presentationSummary })} />);

    const collections = await screen.findByRole("region", { name: "Collections" });
    const view = await within(collections).findByRole("button", { name: "Knowledge board" });

    // The group holding a view starts open, and its label appears once.
    expect(within(collections).getByRole("button", { name: "Collapse Knowledge" })).toBeVisible();
    expect(within(collections).getAllByText("Knowledge")).toHaveLength(1);
    expect(view.closest(".ontology-type-group--presentation")).not.toBeNull();
    // Views without a matching type group keep their own labelled section.
    expect(within(collections).getByRole("navigation", { name: "Views" })).toBeVisible();

    fireEvent.click(within(collections).getByRole("button", { name: "Collapse Knowledge" }));
    expect(within(collections).queryByRole("button", { name: "Knowledge board" })).toBeNull();
  });

  it("retains disabled embedded implementors while hiding standalone embedded types", async () => {
    registerRailData("__all__", allNotes);

    const mixedSummary: OntologySummaryResponse = {
      ...summary,
      types: [
        ...(summary.types ?? []),
        { name: "Metric", label: "Metric", count: 2, role: "embedded" },
        { name: "LooseMetric", label: "Loose metric", count: 1, role: "embedded" },
      ],
      interfaces: [
        { ...summary.interfaces![0], count: 4, implementors: ["Plan", "Spec", "Metric"] },
      ],
    };

    const onSelectCollection = vi.fn();
    render(<NotesLeftRail {...props({ summary: mixedSummary, onSelectCollection })} />);
    const collections = await screen.findByRole("region", { name: "Collections" });
    fireEvent.click(within(collections).getByRole("button", { name: "Expand Other" }));
    fireEvent.click(within(collections).getByRole("button", { name: "Expand Document" }));
    expect(within(collections).getByRole("button", { name: "Document (4)" })).toBeVisible();
    const metric = within(collections).getByRole("button", { name: "Metric" });
    expect(metric).toBeDisabled();
    fireEvent.click(metric);
    expect(onSelectCollection).not.toHaveBeenCalled();
    expect(within(collections).queryByRole("button", { name: "Loose metric" })).toBeNull();
  });

  it("keeps arrow keys working after a caret is clicked", async () => {
    registerRailData("__all__", allNotes);
    render(<NotesLeftRail {...props()} />);
    const collections = await screen.findByRole("region", { name: "Collections" });

    fireEvent.click(within(collections).getByRole("button", { name: "Expand Other" }));

    const row = document.activeElement;

    if (!(row instanceof HTMLElement)) throw new Error("the clicked caret left focus nowhere");
    expect(row).toHaveAttribute("data-rail-row");
    fireEvent.keyDown(row, { key: "ArrowDown" });
    expect(document.activeElement).not.toBe(row);
    expect(document.activeElement).toHaveAttribute("data-rail-row");
  });

  it("lets the keyboard open a disabled embedded parent's children", async () => {
    registerRailData("__all__", allNotes);

    const nestedSummary: OntologySummaryResponse = {
      ...summary,
      types: [
        ...(summary.types ?? []),
        { name: "Metric", label: "Metric", count: 2, role: "embedded" },
        { name: "MetricSample", label: "Metric sample", count: 3, displayParent: "Metric" },
      ],
      interfaces: [
        { ...summary.interfaces![0], count: 4, implementors: ["Plan", "Spec", "Metric"] },
      ],
    };

    render(<NotesLeftRail {...props({ summary: nestedSummary })} />);
    const collections = await screen.findByRole("region", { name: "Collections" });

    fireEvent.click(within(collections).getByRole("button", { name: "Expand Other" }));
    fireEvent.click(within(collections).getByRole("button", { name: "Expand Document" }));

    // The disabled row's caret stands in as its keyboard row.
    const caret = within(collections).getByRole("button", { name: "Expand Metric" });

    expect(caret).toHaveAttribute("data-rail-row");
    caret.focus();
    fireEvent.keyDown(caret, { key: "ArrowRight" });

    expect(await within(collections).findByRole("button", { name: /Metric sample/ })).toBeVisible();
  });

  it("organizes presentation parents under inherited groups without changing semantic counts", async () => {
    registerRailData("Material", typeDetail("Material", []));

    const rendered = render(
      <NotesLeftRail {...props({ summary: presentationSummary, selectedType: "Material" })} />,
    );

    const collections = await screen.findByRole("region", { name: "Collections" });
    expect(within(collections).getByRole("button", { name: "Collapse Delivery" })).toHaveAttribute(
      "aria-expanded",
      "true",
    );
    expect(within(collections).getByRole("button", { name: "Collapse Efforts" })).toHaveAttribute(
      "aria-expanded",
      "true",
    );
    expect(
      within(collections).getByRole("button", { name: "Collapse Effort workspaces" }),
    ).toHaveAttribute("aria-expanded", "true");
    expect(within(collections).getByRole("button", { name: "Efforts (2)" })).toBeVisible();
    expect(within(collections).getByRole("button", { name: "Effort notes (3)" })).toBeVisible();
    expect(
      within(collections).getByRole("button", { name: "Effort workspaces (2)" }),
    ).toBeVisible();
    expect(within(collections).getByRole("button", { name: "Materials (1)" })).toBeVisible();
    expect(
      within(collections)
        .getByRole("button", { name: "Efforts (2)" })
        .closest(".ontology-type-row-wrap"),
    ).toHaveStyle("--ontology-type-depth: 1");
    expect(
      within(collections)
        .getByRole("button", { name: "Effort workspaces (2)" })
        .closest(".ontology-type-row-wrap"),
    ).toHaveStyle("--ontology-type-depth: 2");
    expect(
      within(collections)
        .getByRole("button", { name: "Materials (1)" })
        .closest(".ontology-type-row-wrap"),
    ).toHaveStyle("--ontology-type-depth: 3");
    expect(within(collections).getByRole("button", { name: "Delivery" })).toHaveClass(
      "ontology-type-row",
      "ontology-type-row--group",
    );

    fireEvent.click(within(collections).getByRole("button", { name: "Collapse Delivery" }));
    expect(within(collections).getByRole("button", { name: "Expand Delivery" })).toHaveAttribute(
      "aria-expanded",
      "false",
    );
    expect(within(collections).queryByRole("button", { name: "Materials (1)" })).toBeNull();

    const refreshedSummary = {
      ...presentationSummary,
      totalNotes: presentationSummary.totalNotes + 1,
    };

    rendered.rerender(
      <NotesLeftRail {...props({ summary: refreshedSummary, selectedType: "Material" })} />,
    );
    expect(within(collections).getByRole("button", { name: "Expand Delivery" })).toHaveAttribute(
      "aria-expanded",
      "false",
    );

    fireEvent.click(within(collections).getByRole("button", { name: "Expand Delivery" }));

    const knowledge = within(collections).getByRole("button", { name: "Expand Knowledge" });
    fireEvent.click(knowledge);

    const labels = [...collections.querySelectorAll(".ontology-type-row__label")].map(
      (element) => element.textContent,
    );

    expect(labels.indexOf("Alpha items")).toBeLessThan(labels.indexOf("Zeta items"));

    const other = within(collections).getByRole("button", { name: "Expand Other" });
    fireEvent.click(other);
    expect(within(collections).getByRole("button", { name: "Loose items (1)" })).toBeVisible();
  });

  it("gives caretless rows a caret gutter and drops the highlight when no collection is active", async () => {
    registerRailData("Material", typeDetail("Material", []));
    render(
      <NotesLeftRail
        {...props({
          summary: presentationSummary,
          selectedType: "Material",
          activeCollection: null,
        })}
      />,
    );

    const collections = await screen.findByRole("region", { name: "Collections" });
    expect(collections.querySelectorAll(".ontology-type-row.is-selected")).toHaveLength(0);

    const materials = within(collections)
      .getByRole("button", { name: "Materials (1)" })
      .closest(".ontology-type-row-wrap")!;

    expect(materials.querySelector(".ontology-type-row__collapse-spacer")).toBeInTheDocument();
    expect(materials.firstElementChild).toHaveClass("ontology-type-row__collapse-spacer");
  });

  it("expands a collapsed branch when navigating to a sibling type", async () => {
    registerRailData("EffortNote", typeDetail("EffortNote", []));
    registerRailData("EffortWorkspace", typeDetail("EffortWorkspace", []));

    const rendered = render(
      <NotesLeftRail {...props({ summary: presentationSummary, selectedType: "EffortNote" })} />,
    );

    const collections = await screen.findByRole("region", { name: "Collections" });
    fireEvent.click(within(collections).getByRole("button", { name: "Collapse Delivery" }));
    expect(within(collections).queryByRole("button", { name: "Effort workspaces (2)" })).toBeNull();

    rendered.rerender(
      <NotesLeftRail
        {...props({ summary: presentationSummary, selectedType: "EffortWorkspace" })}
      />,
    );
    expect(
      within(collections).getByRole("button", { name: "Effort workspaces (2)" }),
    ).toBeVisible();
  });
});
