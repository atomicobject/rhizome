import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";

import type {
  OntologyEditSessionResponse,
  OntologyNoteListItem,
  OntologySummaryResponse,
  OntologyTypeResponse,
  ViewCatalogEntry,
  ViewExecuteResponse,
  ViewFieldCapability,
  ViewTableRow,
} from "../api/types";
import { jsonReply, withFakeFetch } from "../test/fakeFetch";
import { renderWithQueryClient as render } from "../test/renderWithQueryClient";
import { NotesShell } from "./NotesShell";
import { viewSegment } from "../test/workspaceView";

const http = withFakeFetch();

const emptyGraph = { nodes: [], edges: [], truncated: false };

const productType = {
  name: "ProductSpec",
  label: "ProductSpec",
  count: 1,
  issueCount: 0,
};

const interfaceSummary = {
  name: "SpecLike",
  label: "SpecLike",
  count: 1,
  issueCount: 0,
  implementors: ["ProductSpec"],
};

function summary(overrides: Partial<OntologySummaryResponse> = {}): OntologySummaryResponse {
  return {
    schemaPresent: true,
    totalNotes: 1,
    typedNotes: 1,
    untypedNotes: 0,
    ambiguousNotes: 0,
    issueNotes: 0,
    types: [],
    interfaces: [],
    ...overrides,
  };
}

function typeResponse(name = "ProductSpec"): OntologyTypeResponse {
  const row = noteRow(name === "ProductSpec" ? "specs/alpha.md" : "notes/all.md");

  return {
    count: 1,
    issueCount: 0,
    type: { name, label: name, fields: [] },
    notes: [row],
  };
}

function noteRow(path: string, title = "Alpha Spec"): ViewTableRow & OntologyNoteListItem {
  return {
    ref: { notePath: path, kind: "NOTE" },
    path,
    title,
    resolvedType: "ProductSpec",
    updatedAt: 1_713_000_000,
    hasIssues: false,
    fields: {},
  };
}

function viewEntry(
  id: string,
  name: string,
  mount: ViewCatalogEntry["mount"],
  field = "title",
): ViewCatalogEntry {
  const source: ViewCatalogEntry["source"] = {
    kind: mount.kind === "standalone" ? "query_recipe" : "ontology_type",
  };

  if (mount.type) source.type = mount.type;

  if (mount.interface) source.interface = mount.interface;

  const variants = {
    table: { columns: [{ field, label: field === "status" ? "Status" : "Title" }] },
  };

  return {
    id,
    name,
    source,
    mount,
    defaults: { variant: "table" },
    variants,
    definition: {
      apiVersion: "rhizome.view.v1",
      id,
      name,
      source,
      mount,
      variants,
    },
  };
}

function capability(field: string, edit?: ViewFieldCapability["edit"]): ViewFieldCapability {
  const result: ViewFieldCapability = {
    key: field,
    label: field === "status" ? "Status" : "Title",
    valueKind: field === "status" ? "enum" : "string",
    sortable: true,
    groupable: false,
    filterOps: ["eq", "contains"],
  };

  if (edit) result.edit = edit;

  return result;
}

function execution(
  view: ViewCatalogEntry,
  rows: ViewTableRow[],
  capabilities: ViewFieldCapability[] = [],
): ViewExecuteResponse {
  return {
    view,
    variant: "table",
    state: {},
    capabilities,
    columns: view.variants.table?.columns ?? [],
    rows,
    pageInfo: {
      total: rows.length,
      offset: 0,
      first: 25,
      returned: rows.length,
      hasMore: false,
    },
    definitionFingerprint: "definition",
    sourceFingerprint: "source",
    executionFingerprint: `execution-${rows.map((row) => row.title).join(",")}`,
  };
}

function editSession(
  status: "dirty" | "clean",
  hasUncommittedChanges: boolean,
): OntologyEditSessionResponse {
  return {
    sessionId: "edit-1",
    status,
    revision: 1,
    ops: [],
    hasUncommittedChanges,
    touchedPaths: hasUncommittedChanges ? ["specs/alpha.md"] : [],
    createdAt: "2026-09-06T00:00:00Z",
    updatedAt: "2026-09-06T00:00:01Z",
  };
}

function registerReads(catalog: ViewCatalogEntry[], summaryResponse = summary()) {
  http
    .json("GET", "/api/v1/ontology/summary", summaryResponse)
    .json("GET", "/api/v1/ontology/types/__all__", typeResponse("All"))
    .json("POST", "/api/v1/ontology/types/__all__", typeResponse("All"))
    .json("GET", "/api/v1/ontology/types/ProductSpec", typeResponse())
    .json("POST", "/api/v1/ontology/types/ProductSpec", typeResponse())
    .json("GET", "/api/v1/graphs/global", emptyGraph)
    .json("GET", "/api/v1/views", {
      views: catalog,
      targets: catalog.map((view) => ({
        kind: view.mount.kind,
        name: view.mount.type ?? view.mount.interface ?? view.id,
        defaultChoiceId: "table:" + view.id,
        choices: [
          { id: "builtin:overview", name: "Overview", renderer: "overview" },
          {
            id: "table:" + view.id,
            name: "Table",
            renderer: "table",
            variant: "table",
            viewId: view.id,
          },
        ],
      })),
    })
    .json("GET", "/api/v2/validate", { status: "never_ran", generation: 0 });
}

describe("NotesShell configured views", () => {
  beforeEach(() => {
    window.history.replaceState({}, "", "/notes");
    window.localStorage.clear();
    window.sessionStorage.clear();
  });

  it("opens a standalone configured view as its own retained tab", async () => {
    const backlog = viewEntry("planning.backlog", "Planning backlog", {
      kind: "standalone",
      group: "Planning",
      order: 1,
    });

    registerReads([backlog]);
    http.json(
      "POST",
      "/api/v1/views/planning.backlog/execute",
      execution(backlog, [noteRow("planning/next.md", "Next planning item")]),
    );

    render(<NotesShell />);
    const railRow = await screen.findByRole("button", { name: "Planning backlog" });
    fireEvent.click(railRow);

    await waitFor(() => expect(window.location.search).toBe("?view=planning.backlog"));
    const tab = screen.getByRole("tab", { name: "Planning backlog" });
    expect(tab).toHaveAttribute("aria-selected", "true");

    const panel = screen.getByRole("tabpanel", { name: "Planning backlog" });
    expect(panel).toHaveAttribute("id", "panel-view:planning.backlog");
    expect(await within(panel).findByText("Next planning item")).toBeVisible();
    expect(http.count("POST", "/api/v1/views/planning.backlog/execute")).toBe(1);

    // The rail follows the active tab: the view row is selected, no collection is.
    expect(railRow.className).toContain("is-selected");
    expect(screen.getByRole("button", { name: /^All notes/ }).className).not.toContain(
      "is-selected",
    );
  });

  it("shows interface implementors and opens the configured default in the type workspace", async () => {
    const productTable = viewEntry("products.default", "Product table", {
      kind: "type",
      type: "ProductSpec",
      default: true,
    });

    registerReads(
      [productTable],
      summary({ types: [productType], interfaces: [interfaceSummary] }),
    );
    http.json(
      "POST",
      "/api/v1/views/products.default/execute",
      execution(productTable, [noteRow("specs/alpha.md")]),
    );

    render(<NotesShell />);
    fireEvent.click(await screen.findByRole("button", { name: "Expand Other" }));
    fireEvent.click(await screen.findByRole("button", { name: "Expand SpecLike" }));
    expect(await screen.findByRole("button", { name: "ProductSpec (1)" })).toBeVisible();

    fireEvent.click(screen.getByRole("button", { name: "ProductSpec (1)" }));
    await waitFor(() => expect(window.location.pathname).toBe("/notes/product-spec"));
    expect(window.location.search).toBe("");
    const panel = screen.getByRole("tabpanel", { name: "Home" });
    await waitFor(() =>
      expect(viewSegment("Table", within(panel))).toHaveAttribute("aria-pressed", "true"),
    );
    expect(await within(panel).findByText("Alpha Spec")).toBeVisible();
  });

  it("shows a staged edit and keeps the table on screen while the view refetches", async () => {
    const editable = viewEntry(
      "editable.notes",
      "Editable notes",
      { kind: "standalone", group: "Planning", order: 1 },
      "status",
    );

    const statusCapability = capability("status", {
      kind: "enum",
      operation: "setField",
      field: "status",
      options: ["draft", "published"],
    });

    const row = { ...noteRow("specs/alpha.md"), fields: { status: "draft" } };
    let releaseRefetch = () => {};

    const refetchGate = new Promise<void>((resolve) => (releaseRefetch = resolve));
    let executions = 0;
    registerReads([editable]);
    http.on("POST", "/api/v1/views/editable.notes/execute", async () => {
      executions += 1;

      if (executions === 1) return jsonReply(execution(editable, [row], [statusCapability]));
      await refetchGate;

      return jsonReply(
        execution(editable, [{ ...row, fields: { status: "published" } }], [statusCapability]),
      );
    });
    http.on("POST", "/api/v1/edit-sessions", (request) => {
      const body = JSON.parse(request.body || "null");

      return jsonReply({ ...editSession("dirty", true), ops: body.ops || [] });
    });

    window.history.replaceState({}, "", "/notes?view=editable.notes");
    render(<NotesShell />);
    fireEvent.click(await screen.findByRole("button", { name: "Edit Status" }));
    fireEvent.change(await screen.findByRole("combobox", { name: "Edit Status" }), {
      target: { value: "published" },
    });

    expect(await screen.findByText("Refreshing view…")).toBeVisible();
    expect(screen.queryByText("Loading view…")).toBeNull();
    expect(screen.getByRole("button", { name: "Edit Status" })).toHaveTextContent("Published");
    // The footer stays in place; a single page shows its row count, not a pager.
    expect(document.querySelector(".configured-view__footer")).toHaveTextContent("1 row");

    releaseRefetch();
    await waitFor(() => expect(screen.queryByText("Refreshing view…")).toBeNull());
    expect(screen.getByRole("button", { name: "Edit Status" })).toHaveTextContent("Published");
  });

  it("refreshes the active configured view after saving an edit session", async () => {
    const editable = viewEntry(
      "editable.notes",
      "Editable notes",
      {
        kind: "standalone",
        group: "Planning",
        order: 1,
      },
      "status",
    );

    const statusCapability = capability("status", {
      kind: "enum",
      operation: "setField",
      field: "status",
      options: ["draft", "published"],
    });

    const initialRow = { ...noteRow("specs/alpha.md"), fields: { status: "draft" } };
    const savedRow = { ...initialRow, fields: { status: "published" } };
    let committed = false;
    registerReads([editable]);
    http.on("POST", "/api/v1/views/editable.notes/execute", () =>
      jsonReply(execution(editable, [committed ? savedRow : initialRow], [statusCapability])),
    );
    http.json("POST", "/api/v1/edit-sessions", editSession("dirty", true));
    http.on("POST", "/api/v1/edit-sessions/edit-1/stage", (request) => {
      const body = JSON.parse(request.body || "null");

      return jsonReply({ ...editSession("dirty", true), ops: body.ops || [], revision: 2 });
    });
    http.on("POST", "/api/v1/edit-sessions/edit-1/commit", () => {
      committed = true;

      return jsonReply(editSession("clean", false));
    });

    window.history.replaceState({}, "", "/notes?view=editable.notes");
    render(<NotesShell />);
    fireEvent.click(await screen.findByRole("button", { name: "Edit Status" }));
    const statusEditor = await screen.findByRole("combobox", { name: "Edit Status" });
    fireEvent.change(statusEditor, { target: { value: "published" } });
    await waitFor(() => expect(screen.getByRole("button", { name: "Save" })).not.toBeDisabled());

    const executionsBeforeSave = http.count("POST", "/api/v1/views/editable.notes/execute");
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Edit Status" })).toHaveTextContent("Published"),
    );
    expect(http.count("POST", "/api/v1/edit-sessions/edit-1/commit")).toBe(1);
    expect(http.count("POST", "/api/v1/views/editable.notes/execute")).toBeGreaterThan(
      executionsBeforeSave,
    );
  });
});
