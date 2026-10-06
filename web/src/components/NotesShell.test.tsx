import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";

import { queryKeys } from "../api/queryKeys";
import { isString } from "../api/parse";
import type {
  OntologyNoteListItem,
  OntologySummaryResponse,
  OntologyTypeResponse,
  ViewCatalogEntry,
} from "../api/types";
import type {
  PublicGraphQLResult,
  PublicNode,
  PublicNodeDetailData,
} from "../api/publicGraphQLTypes";
import {
  deferredReply,
  graphQLRequestBody,
  jsonReply,
  type FakeFetchRequest,
  withFakeFetch,
} from "../test/fakeFetch";
import { withFakeEventSource } from "../test/fakeEventSource";
import { renderWithQueryClient as render } from "../test/renderWithQueryClient";
import { NotesShell } from "./NotesShell";
import { viewSegment } from "../test/workspaceView";

const emptyGraph = { nodes: [], edges: [], truncated: false };

const summary: OntologySummaryResponse = {
  schemaPresent: true,
  totalNotes: 2,
  typedNotes: 2,
  untypedNotes: 0,
  ambiguousNotes: 0,
  issueNotes: 0,
  types: [
    { name: "Plan", label: "Plan", count: 1, issueCount: 0, role: "note" },
    { name: "Spec", label: "Spec", count: 1, issueCount: 0, role: "note" },
  ],
  interfaces: [],
};

function note(path: string, title: string, resolvedType: string): OntologyNoteListItem {
  return {
    ref: { notePath: path, kind: "NOTE" },
    path,
    title,
    resolvedType,
    hasIssues: false,
    updatedAt: 1_713_000_000,
  };
}

const planNote = note("plans/demo-plan.md", "Demo plan", "Plan");

const specNote = note("specs/demo-spec.md", "Demo spec", "Spec");

function typeDetail(typeName: string, notes: OntologyNoteListItem[]): OntologyTypeResponse {
  return {
    count: notes.length,
    issueCount: 0,
    type: { name: typeName, label: typeName, pluralLabel: `${typeName}s`, fields: [] },
    notes,
  };
}

const allNotes = typeDetail("All", [planNote, specNote]);

const planTable: ViewCatalogEntry = {
  id: "plans.default",
  name: "Plan table",
  generated: true,
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

function viewExecution(view: ViewCatalogEntry) {
  return {
    view,
    variant: "table",
    state: {},
    columns: [{ field: "title", label: "Title" }],
    rows: [
      {
        ref: { notePath: planNote.path, kind: "NOTE" },
        path: planNote.path,
        title: planNote.title,
        resolvedType: "Plan",
      },
    ],
    pageInfo: { total: 1, offset: 0, first: 25, returned: 1, hasMore: false },
    definitionFingerprint: "definition",
    sourceFingerprint: "source",
    executionFingerprint: "execution",
  };
}

function publicNode(ref: string): PublicNode {
  const hash = ref.indexOf("#");
  const notePath = hash >= 0 ? ref.slice(0, hash) : ref;
  const suffix = hash >= 0 ? ref.slice(hash + 1) : "";
  const nodeId = suffix.startsWith("node:") ? suffix.slice("node:".length) : "";
  const fragment = nodeId ? "" : suffix;
  const kind = nodeId ? "EMBEDDED" : fragment ? "SECTION" : "NOTE";

  return {
    ref: {
      ref,
      kind,
      notePath,
      path: notePath,
      fragment: fragment || null,
      nodeId: nodeId || null,
    },
    nodeId: nodeId || `note:${notePath}`,
    nodeKind: kind,
    path: notePath,
    title: notePath,
    content: "",
    locator: {
      sourceLocator: ref,
      status: "linkable",
      exists: true,
      requiresFix: false,
    },
  };
}

function requestedRef(request: FakeFetchRequest): string {
  const ref = graphQLRequestBody(request).variables?.ref;

  return isString(ref) ? ref : "";
}

function nodeDetailReply(ref: string): PublicGraphQLResult<PublicNodeDetailData> {
  return { data: { node: publicNode(ref) } };
}

describe("NotesShell", () => {
  const http = withFakeFetch();
  withFakeEventSource();

  beforeEach(() => {
    window.history.replaceState({}, "", "/notes");
    window.localStorage.clear();
    window.sessionStorage.clear();

    http
      .json("GET", "/api/v1/ontology/summary", summary)
      .json("GET", "/api/v1/ontology/types/__all__", allNotes)
      .json("GET", "/api/v1/ontology/types/Plan", typeDetail("Plan", [planNote]))
      .json("GET", "/api/v1/graphs/global", emptyGraph)
      .json("GET", "/api/v1/views", { views: [] })
      .json("GET", "/api/v2/validate", { status: "never_ran", generation: 0 })
      .onGraphQL("PublicNodeDetail", (request) =>
        jsonReply(nodeDetailReply(requestedRef(request))),
      );
  });

  it("loads rail and home data and retains each opened file tab", async () => {
    render(<NotesShell />);

    expect(await screen.findByRole("heading", { name: "All notes" })).toBeVisible();
    await screen.findByRole("button", { name: "All notes (2)" });
    expect(screen.getByRole("button", { name: "All notes (2)" })).toBeVisible();
    expect(screen.getByRole("button", { name: /Demo plan.*plans\/demo-plan\.md/i })).toBeVisible();
    expect(screen.getByRole("button", { name: /Demo spec.*specs\/demo-spec\.md/i })).toBeVisible();

    fireEvent.click(screen.getByRole("button", { name: /Demo plan.*plans\/demo-plan\.md/i }));
    await waitFor(() => {
      expect(window.location.search).toBe("?note=plans%2Fdemo-plan.md");
    });
    expect(await screen.findByRole("tab", { name: /demo-plan\.md/i })).toBeVisible();

    fireEvent.click(screen.getByRole("button", { name: /Demo spec.*specs\/demo-spec\.md/i }));
    await waitFor(() => {
      expect(window.location.search).toBe("?note=specs%2Fdemo-spec.md");
      expect(screen.getByRole("tab", { name: /demo-plan\.md/i })).toBeVisible();
      expect(screen.getByRole("tab", { name: /demo-spec\.md/i })).toBeVisible();
    });
  });

  it("closes the active note tab when the desktop app asks, but never Home", async () => {
    render(<NotesShell />);

    fireEvent.click(
      await screen.findByRole("button", { name: /Demo plan.*plans\/demo-plan\.md/i }),
    );
    expect(await screen.findByRole("tab", { name: /demo-plan\.md/i })).toBeVisible();

    act(() => {
      window.dispatchEvent(new CustomEvent("rhizome:close-tab"));
    });
    await waitFor(() => expect(screen.queryByRole("tab", { name: /demo-plan\.md/i })).toBeNull());

    act(() => {
      window.dispatchEvent(new CustomEvent("rhizome:close-tab"));
    });
    expect(screen.getByRole("tab", { name: "Home" })).toBeVisible();
  });

  it("leaves note tabs alone when the desktop app asks while Notes is not on screen", async () => {
    const shell = render(<NotesShell />);

    fireEvent.click(
      await screen.findByRole("button", { name: /Demo plan.*plans\/demo-plan\.md/i }),
    );
    expect(await screen.findByRole("tab", { name: /demo-plan\.md/i })).toBeVisible();
    shell.rerender(<NotesShell active={false} />);

    act(() => {
      window.dispatchEvent(new CustomEvent("rhizome:close-tab"));
    });
    expect(screen.getByRole("tab", { name: /demo-plan\.md/i, hidden: true })).toBeInTheDocument();
  });

  it("opens Problems in its own reusable tab from the left rail", async () => {
    http.json("GET", "/api/v1/ontology/types/__issues__", typeDetail("Issues", []));
    render(<NotesShell />);

    fireEvent.click(await screen.findByRole("button", { name: "Problems" }));
    expect(await screen.findByRole("tab", { name: "Problems" })).toHaveAttribute(
      "aria-selected",
      "true",
    );
    expect(window.location.pathname).toBe("/notes/issues");

    fireEvent.click(screen.getByRole("tab", { name: "Home" }));
    await waitFor(() => expect(window.location.pathname).toBe("/notes"));
    fireEvent.click(screen.getByRole("tab", { name: "Problems" }));
    await waitFor(() => expect(window.location.pathname).toBe("/notes/issues"));
    expect(screen.getAllByRole("tab", { name: "Problems" })).toHaveLength(1);
  });

  it("keeps the Problems scope in its retained panel while a note tab is active", async () => {
    window.history.replaceState(
      {},
      "",
      "/notes/issues?issueScopeKind=note&issueScopeKey=specs%2Fdemo-spec.md",
    );
    http.json("GET", "/api/v1/ontology/types/__issues__", typeDetail("Issues", []));
    render(<NotesShell />);

    const problemsPanel = document.getElementById("panel-collection:issues");
    expect(problemsPanel).not.toBeNull();
    expect(await within(problemsPanel!).findByText("note · specs/demo-spec.md")).toBeVisible();

    act(() => {
      window.history.pushState({}, "", "/notes/issues?note=plans%2Fdemo-plan.md");
      window.dispatchEvent(new PopStateEvent("popstate"));
    });
    await waitFor(() =>
      expect(screen.getByRole("tab", { name: /demo-plan\.md/i })).toHaveAttribute(
        "aria-selected",
        "true",
      ),
    );
    expect(within(problemsPanel!).getByText("note · specs/demo-spec.md")).toBeInTheDocument();
  });

  it("shows an unavailable home with retry when the summary request fails", async () => {
    let attempts = 0;
    http.on("GET", "/api/v1/ontology/summary", () => {
      attempts += 1;

      return attempts === 1
        ? jsonReply({ error: "Summary request failed" }, 500)
        : jsonReply(summary);
    });

    render(<NotesShell />);

    expect(await screen.findByText(/Workspace data is unavailable/)).toBeVisible();
    expect(screen.queryByText("0 notes")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Retry connection" }));
    await waitFor(() => expect(attempts).toBe(2));
    await waitFor(() => expect(screen.queryByText(/Workspace data is unavailable/)).toBeNull());
    expect(document.querySelector(".type-workspace-header h2")).toHaveTextContent("All notes");
  });

  it("shows a retryable type failure and does not present zero notes as loaded data", async () => {
    window.history.replaceState({}, "", "/notes/spec");
    let attempts = 0;
    http.on("GET", "/api/v1/ontology/types/Spec", () => {
      attempts += 1;

      return attempts === 1
        ? jsonReply({ error: "Specs request failed" }, 500)
        : jsonReply(typeDetail("Spec", [note("specs/recovered.md", "Recovered spec", "Spec")]));
    });

    render(<NotesShell />);

    expect((await screen.findAllByText("Specs request failed")).length).toBeGreaterThan(0);
    expect(screen.getAllByRole("button", { name: "Retry" }).length).toBeGreaterThan(0);
    fireEvent.click(screen.getAllByRole("button", { name: "Retry" })[0]);

    await waitFor(() => expect(attempts).toBe(2));
    expect((await screen.findAllByText("Recovered spec")).length).toBeGreaterThan(0);
  });

  it("keeps cached type notes visible when a refresh fails", async () => {
    window.history.replaceState({}, "", "/notes/spec");
    let attempts = 0;
    http.on("GET", "/api/v1/ontology/types/Spec", () => {
      attempts += 1;

      return attempts === 1
        ? jsonReply(typeDetail("Spec", [note("specs/cached.md", "Cached spec", "Spec")]))
        : jsonReply({ error: "Refresh failed" }, 500);
    });

    const { queryClient } = render(<NotesShell />);
    expect((await screen.findAllByText("Cached spec")).length).toBeGreaterThan(0);

    await act(async () => {
      await queryClient.invalidateQueries({ queryKey: queryKeys.ontology.type("Spec") });
    });
    await waitFor(() => expect(attempts).toBe(2));

    expect(screen.getAllByText("Cached spec").length).toBeGreaterThan(0);
    expect(screen.queryByText("Could not load this note list")).toBeNull();
  });

  it("drops the prior type list while the newly selected type is loading", async () => {
    window.history.replaceState({}, "", "/notes/plan");
    const specReply = deferredReply<OntologyTypeResponse>();
    http
      .json("GET", "/api/v1/ontology/types/Plan", typeDetail("Plan", [planNote]))
      .on("GET", "/api/v1/ontology/types/Spec", () => specReply.promise);

    render(<NotesShell />);
    expect((await screen.findAllByText("Demo plan")).length).toBeGreaterThan(0);

    fireEvent.click(await screen.findByRole("button", { name: "Spec (1)" }));
    await waitFor(() => expect(http.count("GET", "/api/v1/ontology/types/Spec")).toBe(1));
    expect(screen.queryByText("Demo plan")).toBeNull();

    specReply.resolve(typeDetail("Spec", [specNote]));
    expect((await screen.findAllByText("Demo spec")).length).toBeGreaterThan(0);
    expect(screen.queryByText("Demo plan")).toBeNull();
  });

  it("shows type loading and ignores a late response from the previous selection", async () => {
    window.history.replaceState({}, "", "/notes/plan");
    const planReply = deferredReply<OntologyTypeResponse>();
    const specReply = deferredReply<OntologyTypeResponse>();
    http
      .on("GET", "/api/v1/ontology/types/Plan", () => planReply.promise)
      .on("GET", "/api/v1/ontology/types/Spec", () => specReply.promise);

    render(<NotesShell />);
    await waitFor(() => expect(http.count("GET", "/api/v1/ontology/types/Plan")).toBe(1));
    expect(screen.getByText("Loading notes…")).toBeVisible();

    fireEvent.click(await screen.findByRole("button", { name: "Spec (1)" }));
    await waitFor(() => expect(http.count("GET", "/api/v1/ontology/types/Spec")).toBe(1));
    specReply.resolve(typeDetail("Spec", [specNote]));
    expect((await screen.findAllByText("Demo spec")).length).toBeGreaterThan(0);

    planReply.resolve(typeDetail("Plan", [planNote]));
    await waitFor(() => expect(screen.queryByText("Demo plan")).toBeNull());
  });

  it("keeps one local note-list filter without issuing a project search", async () => {
    window.history.replaceState({}, "", "/notes/plan");
    const rendered = render(<NotesShell />);
    await screen.findByRole("button", { name: "Plan (1)" });
    expect(screen.queryByLabelText("Search notes")).toBeNull();

    const filter = screen.getByRole("searchbox", { name: "Filter note list" });
    fireEvent.change(filter, { target: { value: "plan" } });

    const noteList = screen.getByRole("region", { name: "Note list" });
    expect(within(noteList).getByText("Demo plan")).toBeVisible();
    expect(within(noteList).queryByText("Demo spec")).toBeNull();
    expect(http.count("GET", "/api/v1/search/notes")).toBe(0);

    act(() => {
      window.history.pushState({}, "", "/explorer?file=pkg%2Fsearch%2Fservice.go");
      window.dispatchEvent(new PopStateEvent("popstate"));
      rendered.rerender(<NotesShell active={false} />);
    });
    act(() => {
      window.history.pushState({}, "", "/notes/plan");
      window.dispatchEvent(new PopStateEvent("popstate"));
      rendered.rerender(<NotesShell active />);
    });
    expect(screen.getByRole("searchbox", { name: "Filter note list" })).toHaveValue("plan");
  });

  it("opens a type's generated table by default despite a legacy table preference", async () => {
    window.localStorage.setItem("rhizome:notes:viewModeByType", JSON.stringify({ Plan: "table" }));
    window.history.replaceState({}, "", "/notes");
    http
      .json("GET", "/api/v1/views", {
        views: [planTable],
        targets: [
          {
            kind: "type",
            name: "Plan",
            defaultChoiceId: "plan-table",
            choices: [
              { id: "builtin:overview", name: "Overview", renderer: "overview" },
              {
                id: "plan-table",
                name: "Table",
                renderer: "table",
                viewId: planTable.id,
                variant: "table",
              },
            ],
          },
        ],
      })
      .json("POST", "/api/v1/views/plans.default/execute", viewExecution(planTable));

    const rendered = render(<NotesShell />);
    fireEvent.click(await screen.findByRole("button", { name: "Expand Other" }));
    fireEvent.click(await screen.findByRole("button", { name: "Plan (1)" }));

    await waitFor(() => expect(http.count("POST", "/api/v1/views/plans.default/execute")).toBe(1));
    expect(window.location.search).not.toContain("view=");
    expect(viewSegment("Table")).toHaveAttribute("aria-pressed", "true");
    // A generated view is named for its type.
    expect(screen.getByRole("region", { name: "Plan view" })).toBeVisible();

    // The table lists the records, so the rail's copy starts collapsed until the reader opens it.
    const listToggle = screen.getByRole("button", { name: "Plan notes" });
    await waitFor(() => expect(listToggle).toHaveAttribute("aria-expanded", "false"));
    expect(screen.queryByRole("searchbox", { name: "Filter note list" })).toBeNull();
    fireEvent.click(listToggle);
    expect(screen.getByRole("searchbox", { name: "Filter note list" })).toBeVisible();

    rendered.rerender(<NotesShell active={false} />);
    await act(async () => {
      await rendered.queryClient.invalidateQueries({ queryKey: queryKeys.views.all() });
    });
    expect(http.count("POST", "/api/v1/views/plans.default/execute")).toBe(1);
  });
});
