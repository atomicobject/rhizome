import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type {
  OntologyEditSessionResponse,
  OntologyNoteListItem,
  OntologySummaryResponse,
  OntologyTypeResponse,
  ViewCatalogEntry,
} from "../api/types";
import { deferredReply, jsonReply, withFakeFetch } from "../test/fakeFetch";
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
    activeTabID: "tab-spec",
    editSession: null,
    touchedPaths: new Set(),
    findTabByPath: () => undefined,
    onSelectCollection: vi.fn(),
    onOpenConfiguredView: vi.fn(),
    onOpenNote: vi.fn(),
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

  it("does not show a pending note list for a display group", async () => {
    render(<NotesLeftRail {...props({ selectedType: null, activeGroup: "Delivery" })} />);
    expect(await screen.findByRole("region", { name: "Collections" })).toBeVisible();
    expect(screen.queryByRole("region", { name: "Note list" })).not.toBeInTheDocument();
    expect(screen.queryByText("Loading notes…")).not.toBeInTheDocument();
  });

  it("keeps type groups the way the user left them after a remount", async () => {
    const first = render(<NotesLeftRail {...props()} />);
    fireEvent.click(await screen.findByRole("button", { name: "Expand Other" }));
    first.unmount();

    render(<NotesLeftRail {...props()} />);

    expect(await screen.findByRole("button", { name: "Collapse Other" })).toBeVisible();
  });

  it("renders collection and note-list regions with one local filter", async () => {
    const openView = vi.fn();
    registerRailData("__all__", allNotes, [coverageView, planView]);
    render(
      <NotesLeftRail
        {...props({
          activeViewID: coverageView.id,
          editSession: editSession(),
          onOpenConfiguredView: openView,
          findTabByPath: (path) => (path === specNote.path ? { id: "tab-spec" } : undefined),
          touchedPaths: new Set([specNote.path]),
        })}
      />,
    );

    const rail = await screen.findByRole("complementary", { name: "Notes navigation" });
    expect(screen.queryByRole("searchbox", { name: "Search notes" })).not.toBeInTheDocument();
    const collections = await within(rail).findByRole("region", { name: "Collections" });
    const noteList = within(rail).getByRole("region", { name: "Note list" });
    expect(collections).toHaveClass("notes-left-rail__collections");
    expect(noteList).toHaveClass("notes-left-rail__notes");

    expect(within(collections).getByRole("button", { name: "Coverage" })).toHaveClass(
      "is-selected",
    );
    expect(within(collections).getByRole("button", { name: "Problems" })).toBeVisible();
    expect(within(collections).getByRole("button", { name: "Changes 1" })).toBeVisible();
    expect(within(collections).getByRole("button", { name: "All notes (2)" })).toBeVisible();
    fireEvent.click(within(collections).getByRole("button", { name: "Coverage" }));
    expect(openView).toHaveBeenCalledWith(coverageView);

    fireEvent.click(within(collections).getByRole("button", { name: "Expand Other" }));
    fireEvent.click(within(collections).getByRole("button", { name: "Expand Document" }));
    expect(within(collections).getByRole("button", { name: "Plan (1)" })).toBeVisible();
    expect(within(collections).getByRole("button", { name: "Spec (1)" })).toBeVisible();
    expect(
      within(collections).queryByRole("button", { name: "Open Plan table for Plan" }),
    ).toBeNull();

    const specRow = within(noteList)
      .getByRole("button", { name: /Demo spec.*specs\/demo-spec\.md/i })
      .closest(".ontology-note");

    expect(specRow).toHaveClass("is-active", "is-modified");
    fireEvent.change(within(noteList).getByRole("searchbox", { name: "Filter note list" }), {
      target: { value: "plan" },
    });
    expect(within(noteList).getByText("Demo plan")).toBeVisible();
    expect(within(noteList).queryByText("Demo spec")).toBeNull();
    fireEvent.change(within(noteList).getByRole("searchbox", { name: "Filter note list" }), {
      target: { value: "" },
    });
    const settings = within(noteList).getByRole("button", { name: "Open note list settings" });
    fireEvent.click(settings);
    const recent = within(noteList).getByRole("button", { name: "Recent" });
    fireEvent.click(recent);
    expect(recent).toHaveAttribute("aria-pressed", "true");
    expect(
      [...noteList.querySelectorAll(".ontology-note__title")].map((node) => node.textContent),
    ).toEqual(["Demo spec", "Demo plan"]);

    // Escape closes the settings and returns focus to their trigger.
    recent.focus();
    fireEvent.keyDown(recent, { key: "Escape" });
    expect(within(noteList).queryByRole("group", { name: "Note list settings" })).toBeNull();
    expect(settings).toHaveFocus();
  });

  it("keeps the note list to one Tab stop that follows keyboard movement", async () => {
    registerRailData("__all__", allNotes);
    render(<NotesLeftRail {...props()} />);

    const noteList = await screen.findByRole("region", { name: "Note list" });
    await within(noteList).findByText("Demo plan");
    const rows = () => [...noteList.querySelectorAll<HTMLButtonElement>(".ontology-note__main")];
    const tabbable = () => rows().filter((row) => row.tabIndex === 0);

    expect(tabbable()).toEqual([rows()[0]]);
    expect(
      within(noteList)
        .getAllByRole("button", { name: "Open beside" })
        .every((button) => button.tabIndex === -1),
    ).toBe(true);

    rows()[0].focus();
    fireEvent.keyDown(rows()[0], { key: "ArrowDown" });
    expect(rows()[1]).toHaveFocus();
    expect(tabbable()).toEqual([rows()[1]]);
  });

  it("titles a single-type note list by its label and drops the repeated type", async () => {
    const alphaNote = note("alpha/one.md", "First alpha", "Alpha", 10);
    registerRailData("Alpha", typeDetail("Alpha", [alphaNote]));
    render(<NotesLeftRail {...props({ summary: presentationSummary, selectedType: "Alpha" })} />);

    const noteList = await screen.findByRole("region", { name: "Note list" });
    expect(within(noteList).getByRole("heading", { name: "Alpha item notes" })).toBeVisible();
    await within(noteList).findByText("First alpha");
    expect(noteList.querySelector(".ontology-note__type-mini")).toBeNull();
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

  it("filters locally, supports keyboard movement, and preserves note open modes", async () => {
    const openNote = vi.fn();
    registerRailData("__all__", allNotes);
    render(<NotesLeftRail {...props({ onOpenNote: openNote })} />);

    const noteList = await screen.findByRole("region", { name: "Note list" });
    const rows = () => [...noteList.querySelectorAll<HTMLButtonElement>(".ontology-note__main")];

    const search = within(noteList).getByRole<HTMLInputElement>("searchbox", {
      name: "Filter note list",
    });

    expect(screen.getAllByRole("searchbox")).toHaveLength(1);
    fireEvent.change(search, { target: { value: "demo" } });
    expect(await screen.findByText("Demo plan")).toBeVisible();
    fireEvent.keyDown(search, { key: "ArrowDown" });
    expect(rows()[0]).toHaveFocus();
    fireEvent.keyDown(rows()[0], { key: "j" });
    expect(rows()[1]).toHaveFocus();
    fireEvent.keyDown(rows()[1], { key: "k" });
    expect(rows()[0]).toHaveFocus();
    fireEvent.keyDown(rows()[0], { key: "o" });
    fireEvent.keyDown(rows()[0], { key: "x" });
    expect(openNote).toHaveBeenNthCalledWith(1, planNote.path, "beside");
    expect(openNote).toHaveBeenNthCalledWith(2, planNote.path, "beside");

    fireEvent.keyDown(rows()[0], { key: "ArrowUp" });
    expect(search).toHaveFocus();
    fireEvent.keyDown(search, { key: "Enter" });
    expect(openNote).toHaveBeenLastCalledWith(planNote.path, "activate");

    fireEvent.click(rows()[1]);
    expect(openNote).toHaveBeenLastCalledWith(specNote.path, "activate");
    fireEvent.click(rows()[0], { metaKey: true });
    expect(openNote).toHaveBeenLastCalledWith(planNote.path, "beside");
    fireEvent.click(within(noteList).getAllByRole("button", { name: "Open beside" })[0]);
    expect(openNote).toHaveBeenLastCalledWith(planNote.path, "beside");
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

    const allNotesWrap = within(collections)
      .getByRole("button", { name: "All notes (2)" })
      .closest(".ontology-type-row-wrap")!;

    expect(allNotesWrap.firstElementChild).toHaveClass("ontology-type-row__collapse-spacer");
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

  it("opens embedded list entries by stable identity while marking their owning tab", async () => {
    const onOpenNote = vi.fn();

    const embedded: OntologyNoteListItem = {
      ...specNote,
      path: `${specNote.path}#item-373`,
      ref: {
        notePath: specNote.path,
        fragment: "item-373",
        kind: "EMBEDDED",
        structuralFingerprint: "criterion-fingerprint",
      },
    };

    registerRailData("__all__", typeDetail("All", [embedded]));
    http.json("GET", "/api/v1/nodes/preview", {
      ref: `${specNote.path}#struct:criterion-fingerprint`,
      path: specNote.path,
      title: "Embedded criterion preview",
      format: "markdown",
      fragmentResolved: true,
      fields: [],
      hasIssues: false,
    });
    render(
      <NotesLeftRail
        {...props({
          onOpenNote,
          findTabByPath: (path) => (path === specNote.path ? { id: "tab-spec" } : undefined),
          touchedPaths: new Set([specNote.path]),
        })}
      />,
    );
    const row = await screen.findByRole("button", { name: /Demo spec.*item-373/i });
    expect(row.closest(".ontology-note")).toHaveClass("is-active", "is-modified");
    fireEvent.focus(row);
    await waitFor(() =>
      expect(http.requests("GET", "/api/v1/nodes/preview")[0]?.query.get("ref")).toBe(
        `${specNote.path}#struct:criterion-fingerprint`,
      ),
    );
    expect(
      await screen.findByRole("region", { name: "Preview of Embedded criterion preview" }),
    ).toBeVisible();
    fireEvent.click(row);
    expect(onOpenNote).toHaveBeenLastCalledWith(
      `${specNote.path}#struct:criterion-fingerprint`,
      "activate",
    );
    fireEvent.keyDown(row, { key: "Enter" });
    expect(onOpenNote).toHaveBeenLastCalledWith(
      `${specNote.path}#struct:criterion-fingerprint`,
      "activate",
    );
  });

  it("shows loading and keeps the current type when an older type reply arrives", async () => {
    const planReply = deferredReply<OntologyTypeResponse>();
    const specReply = deferredReply<OntologyTypeResponse>();
    http.on("GET", "/api/v1/ontology/types/Plan", () => planReply.promise);
    http.on("GET", "/api/v1/ontology/types/Spec", () => specReply.promise);
    const view = render(<NotesLeftRail {...props({ selectedType: "Plan" })} />);
    expect(await screen.findByRole("status")).toHaveTextContent("Loading notes");

    view.rerender(<NotesLeftRail {...props({ selectedType: "Spec" })} />);
    await waitFor(() => expect(http.count("GET", "/api/v1/ontology/types/Spec")).toBe(1));
    specReply.resolve(typeDetail("Spec", [specNote]));
    expect(await screen.findByText("Demo spec")).toBeVisible();
    await act(async () => {
      planReply.resolve(typeDetail("Plan", [planNote]));
      await planReply.promise;
    });
    expect(screen.getByText("Demo spec")).toBeVisible();
    expect(screen.queryByText("Demo plan")).toBeNull();
  });

  it("recovers from a type-list error and renders a loaded empty state", async () => {
    let attempts = 0;
    http.on("GET", "/api/v1/ontology/types/Plan", () => {
      attempts += 1;

      return attempts === 1
        ? jsonReply({ error: "Type list failed" }, 500)
        : jsonReply(typeDetail("Plan", []));
    });
    render(<NotesLeftRail {...props({ selectedType: "Plan" })} />);

    expect(await screen.findByText("Type list failed")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    await waitFor(() => expect(attempts).toBe(2));
    expect(await screen.findByText("No notes match the current filter.")).toBeVisible();
  });
});
