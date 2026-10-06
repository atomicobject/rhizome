import { act, fireEvent, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type {
  GraphResponse,
  OntologyEditSessionResponse,
  OntologyTypeResponse,
} from "../api/types";
import { INITIAL_EDIT_READ_LIFECYCLE, type EditReadLifecycle } from "../staging/stagedQuery";
import {
  deferredReply,
  emptyValidationGroupsReply,
  emptyValidationSummariesReply,
  withFakeFetch,
} from "../test/fakeFetch";
import { renderWithQueryClient } from "../test/renderWithQueryClient";
import { baseView, execution } from "./ConfiguredView.testFixtures";
import { HomeTab } from "./HomeTab";
import { parseNotesLocation } from "./notesRoute";
import { viewSegment } from "../test/workspaceView";

const http = withFakeFetch();

const emptyGraph: GraphResponse = { nodes: [], edges: [], truncated: false };

const cleanValidation = {
  status: "ok" as const,
  health: "current_clean" as const,
  generation: 1,
  publishedGeneration: 1,
  snapshot: {
    vaultIdentity: "vault",
    generation: 1,
    scope: "default",
    selectedChecks: [],
    startedAt: 1,
    finishedAt: 2,
    durationMs: 1,
    completion: "complete" as const,
    issueCount: 0,
    errorCount: 0,
    affectedFileCount: 0,
    affectedNoteCount: 0,
    repairActionCount: 0,
    checks: [],
  },
};

const typeDetail: OntologyTypeResponse = {
  count: 1,
  issueCount: 0,
  type: { name: "Spec", label: "Spec", fields: [] },
  notes: [
    {
      ref: { notePath: "specs/demo.md", kind: "NOTE" },
      path: "specs/demo.md",
      title: "Demo spec",
      updatedAt: 1_713_000_000,
      hasIssues: false,
    },
  ],
};

beforeEach(() => {
  http.on("POST", "/api/v1/validation/summaries", emptyValidationSummariesReply);
  http.on("POST", "/api/v1/validation/groups", emptyValidationGroupsReply);
  http.json("GET", "/api/v1/ontology/types/Spec", typeDetail);
  http.json("GET", "/api/v1/ontology/types/__all__", typeDetail);
  http.json("GET", "/api/v1/ontology/types/__issues__", typeDetail);
  http.json("GET", "/api/v1/views", { views: [], targets: [] });
  http.json("GET", "/api/v1/ontology/summary", {
    types: [],
    interfaces: [],
    totalNotes: 1,
    typedNotes: 1,
    issueNotes: 0,
  });
  http.json("GET", "/api/v2/validate", cleanValidation);
});

function renderAllHome() {
  const location = parseNotesLocation("/notes", "", "");

  return renderWithQueryClient(
    <HomeTab
      summary={null}
      selection={location.selection}
      selectedType="__all__"
      location={location}
      editSession={{ session: null, replaceOps: async () => {} }}
      onOpenNote={vi.fn()}
      onSelectCollection={vi.fn()}
      onStageOps={async () => {}}
    />,
  );
}

describe("All home header", () => {
  it("summarizes typed notes and summary issues without reading every note", async () => {
    http.json("GET", "/api/v1/ontology/summary", {
      types: [{ name: "Spec", count: 1 }],
      interfaces: [],
      totalNotes: 2,
      typedNotes: 1,
      issueNotes: 4,
    });
    http.json("GET", "/api/v2/validate", { status: "never_ran", generation: 0 });
    renderAllHome();

    expect(await screen.findByText("1 of 2 notes typed across 1 types.")).toBeVisible();
    expect(
      screen.getByRole("button", { name: /Validation issues in Workspace/ }),
    ).toHaveTextContent("4");
    expect(http.count("GET", "/api/v1/ontology/types/__all__")).toBe(0);
    expect(http.count("GET", "/api/v1/graphs/global")).toBe(0);
  });

  it("shows no zero counts while the summary loads", async () => {
    http.on("GET", "/api/v1/ontology/summary", () => deferredReply().promise);
    renderAllHome();

    await waitFor(() => expect(http.count("GET", "/api/v1/ontology/summary")).toBe(1));
    expect(screen.queryByText(/notes typed/)).toBeNull();
    expect(screen.queryByRole("heading", { name: "Workspace" })).toBeNull();
  });

  it("shows indexing instead of zero counts while the index rebuilds", async () => {
    http.json("GET", "/api/v1/ontology/summary", {
      types: [],
      interfaces: [],
      totalNotes: 0,
      typedNotes: 0,
      issueNotes: 0,
      rebuilding: true,
    });
    renderAllHome();

    expect(await screen.findByText("Indexing notes…")).toBeVisible();
    expect(screen.queryByText(/0 of 0 notes typed/)).toBeNull();
  });
});

describe("Home graph reads", () => {
  it("retains a saved table reorder until the post-save read returns", async () => {
    const rows = ["Alpha", "Bravo", "Charlie"].map((title, i) => ({
      ref: { notePath: `ideas/${title}.md`, kind: "NOTE" as const },
      path: `ideas/${title}.md`,
      title,
      fields: { rank: i + 1 },
    }));

    const view = { ...baseView, mount: { kind: "type" as const, type: "Spec", default: true } };
    const stale = { ...execution(rows), view, groups: [] };

    // SAFETY: the generated execution state has no assignable concrete shape.
    stale.state = {
      ...stale.state,
      sort: [{ field: "rank", direction: "asc" }],
    } as typeof stale.state;
    stale.capabilities = [
      ...(stale.capabilities ?? []),
      {
        key: "rank",
        label: "Rank",
        sortable: true,
        indexedSortable: true,
        groupable: false,
        filterOps: ["eq"],
        valueKind: "real",
        edit: { kind: "number", operation: "setField", field: "rank", valueKind: "real" },
      },
    ];

    const session: OntologyEditSessionResponse = {
      sessionId: "home-save",
      revision: 1,
      status: "dirty",
      hasUncommittedChanges: true,
      createdAt: "2026-10-02T00:00:00Z",
      updatedAt: "2026-10-02T00:00:00Z",
      ops: [{ kind: "setField", path: "ideas/Alpha.md", field: "rank", value: "4" }],
    };

    const canonical = deferredReply();
    http.json("GET", "/api/v1/graphs/global", emptyGraph);
    http.json("GET", "/api/v1/views", {
      views: [view],
      targets: [
        {
          kind: "type",
          name: "Spec",
          defaultChoiceId: "table",
          choices: [{ id: "table", name: "Table", viewId: view.id, renderer: "table" }],
        },
      ],
    });
    http.json("POST", "/api/v1/ontology/types/Spec", typeDetail);
    http.json("POST", `/api/v1/views/${view.id}/execute`, stale);
    const location = parseNotesLocation("/notes/spec", "", "");

    const tab = (activeSession: typeof session | null, readLifecycle: EditReadLifecycle) => (
      <HomeTab
        summary={null}
        selection={location.selection}
        selectedType="Spec"
        location={location}
        editSession={{ readLifecycle, session: activeSession, replaceOps: async () => {} }}
        onOpenNote={vi.fn()}
        onSelectCollection={vi.fn()}
        onStageOps={async () => {}}
      />
    );

    const { rerender } = renderWithQueryClient(tab(null, INITIAL_EDIT_READ_LIFECYCLE));

    const order = () =>
      screen
        .getAllByRole("row")
        .slice(1)
        .map((row) => rows.find((item) => row.textContent?.includes(item.title))?.title)
        .filter(Boolean);

    await waitFor(() => expect(order()).toEqual(["Alpha", "Bravo", "Charlie"]));
    http.on("POST", `/api/v1/views/${view.id}/execute`, () => canonical.promise);
    rerender(tab(session, INITIAL_EDIT_READ_LIFECYCLE));
    expect(order()).toEqual(["Bravo", "Charlie", "Alpha"]);
    rerender(tab(null, { revision: 1, outcome: "saved", savedSession: session }));
    expect(order()).toEqual(["Bravo", "Charlie", "Alpha"]);
    await act(async () =>
      canonical.resolve({
        ...stale,
        rows: [rows[1], rows[2], { ...rows[0], fields: { rank: 4 } }],
      }),
    );
    await waitFor(() => expect(screen.queryByText("Refreshing…")).not.toBeInTheDocument());
    expect(order()).toEqual(["Bravo", "Charlie", "Alpha"]);
  });

  it("retries failed graph reads independently of type content", async () => {
    const selectedType = "Spec";
    const pending = deferredReply<GraphResponse>();
    http.on("GET", "/api/v1/graphs/global", () => pending.promise);
    const location = parseNotesLocation("/notes", "", "");
    renderWithQueryClient(
      <HomeTab
        summary={null}
        selection={location.selection}
        selectedType={selectedType}
        location={location}
        editSession={{ session: null, replaceOps: async () => {} }}
        onOpenNote={vi.fn()}
        onSelectCollection={vi.fn()}
        onStageOps={async () => {}}
      />,
    );
    expect(await screen.findByText("Demo spec")).toBeVisible();
    expect(screen.getByText("Loading graph…")).toBeVisible();
    expect(screen.queryByText("No graph data yet")).toBeNull();
    await act(async () => pending.reject(new Error("Graph offline")));
    expect(await screen.findByRole("alert")).toHaveTextContent("Graph offline");
    expect(screen.getByText("Demo spec")).toBeVisible();
    expect(screen.queryByText("No graph data yet")).toBeNull();
    http.json("GET", "/api/v1/graphs/global", emptyGraph);
    fireEvent.click(screen.getByRole("button", { name: "Retry graph" }));
    expect(await screen.findByText("No graph data yet")).toBeVisible();
    expect(http.count("GET", "/api/v1/graphs/global")).toBe(2);
    expect(http.count("GET", `/api/v1/ontology/types/${selectedType}`)).toBe(1);
  });

  it("surfaces a validation transport failure on the Problems home", async () => {
    const pendingValidation = deferredReply();
    http.json("GET", "/api/v1/graphs/global", emptyGraph);
    http.on("GET", "/api/v2/validate", () => pendingValidation.promise);
    const location = parseNotesLocation("/notes/issues", "", "");
    renderWithQueryClient(
      <HomeTab
        summary={null}
        selection={location.selection}
        selectedType="__issues__"
        location={location}
        editSession={{ session: null, replaceOps: async () => {} }}
        onOpenNote={vi.fn()}
        onSelectCollection={vi.fn()}
        onStageOps={async () => {}}
      />,
    );
    expect(await screen.findByRole("status")).toHaveTextContent("Not checked yet");

    await act(async () => pendingValidation.reject(new Error("Validation transport offline")));

    await waitFor(() =>
      expect(screen.getByRole("status")).toHaveTextContent("Validation unavailable"),
    );
    expect(screen.getByText("Validation transport offline")).toBeVisible();
  });

  it("keeps Problems inspectable while browser repair review is unavailable", async () => {
    const onStageOps = vi.fn();
    http.json("GET", "/api/v1/graphs/global", emptyGraph);
    http.json("GET", "/api/v2/validate", {
      status: "ok",
      health: "current_issues",
      generation: 2,
      publishedGeneration: 2,
      snapshot: {
        vaultIdentity: "vault",
        generation: 2,
        scope: "default",
        selectedChecks: ["aliases"],
        startedAt: 1,
        finishedAt: 2,
        durationMs: 1,
        completion: "complete",
        issueCount: 1,
        errorCount: 0,
        affectedFileCount: 1,
        affectedNoteCount: 1,
        repairActionCount: 1,
        checks: [{ check: "aliases", outcome: "completed", issueCount: 1, durationMs: 1 }],
        actions: [
          {
            id: "alias-mirror:specs/demo.md:SPEC-0001",
            check: "aliases",
            issueCode: "identifier_not_in_aliases",
            kind: "append_alias",
            safety: "safe",
            title: "Mirror identifier into aliases",
            issueKeys: ["issue-1"],
          },
        ],
      },
    });
    http.json("GET", "/api/v1/validation/diagnostics", {
      generation: 2,
      filterIdentity: "all",
      sort: "diagnostic_order",
      returned: 1,
      total: 1,
      diagnostics: [
        {
          issueKey: "issue-1",
          check: "aliases",
          code: "identifier_not_in_aliases",
          message: "Identifier is missing from aliases",
          primaryPath: "specs/demo.md",
          affectedPaths: ["specs/demo.md"],
          field: "aliases",
          actionIds: ["alias-mirror:specs/demo.md:SPEC-0001"],
        },
      ],
    });
    const location = parseNotesLocation("/notes/issues", "", "");
    renderWithQueryClient(
      <HomeTab
        summary={null}
        selection={location.selection}
        selectedType="__issues__"
        location={location}
        editSession={{ session: null, replaceOps: async () => {} }}
        onOpenNote={vi.fn()}
        onSelectCollection={vi.fn()}
        onStageOps={onStageOps}
      />,
    );

    expect(await screen.findByRole("button", { name: /specs\/demo\.md/ })).toBeVisible();
    expect(screen.getByRole("note")).toHaveTextContent("Browser review is unavailable");
    expect(screen.getByText("rzm validate fix")).toBeVisible();
    expect(screen.getByRole("checkbox")).toBeVisible();
    expect(screen.queryByRole("button", { name: /Apply .*fix/i })).toBeNull();
    expect(screen.queryByText(/select to apply/i)).toBeNull();
    expect(onStageOps).not.toHaveBeenCalled();
  });

  it("restores the saved layout when a type opens in table mode", async () => {
    const view = {
      ...baseView,
      mount: { kind: "type" as const, type: "Spec", default: true },
      availableVariants: ["table", "card"],
    };

    window.localStorage.setItem(`rhizome.view.variant.${view.id}`, "card");
    http.json("GET", "/api/v1/graphs/global", emptyGraph);
    http.json("GET", "/api/v1/views", {
      views: [view],
      targets: [
        {
          kind: "type",
          name: "Spec",
          defaultChoiceId: "cards",
          choices: [
            { id: "builtin:overview", name: "Overview", renderer: "overview" },
            { id: "cards", name: "Cards", renderer: "card", viewId: view.id, variant: "card" },
          ],
        },
      ],
    });
    http.json("POST", `/api/v1/views/${view.id}/execute`, { ...execution(), view });
    const location = parseNotesLocation("/notes/spec", "", "");

    const tab = (selectedType: string) => (
      <HomeTab
        summary={null}
        selection={location.selection}
        selectedType={selectedType}
        location={location}
        editSession={{ session: null, replaceOps: async () => {} }}
        onOpenNote={vi.fn()}
        onSelectCollection={vi.fn()}
        onStageOps={async () => {}}
      />
    );

    // Selecting the type after the catalog loads mounts the view in the same
    // commit that changes the selected type.
    const { rerender } = renderWithQueryClient(tab("__all__"));
    await waitFor(() => expect(http.count("GET", "/api/v1/views")).toBe(1));
    await screen.findByText("No views are available for All notes.");
    rerender(tab("Spec"));

    await waitFor(() => expect(viewSegment("Cards")).toHaveAttribute("aria-pressed", "true"));
  });
});
