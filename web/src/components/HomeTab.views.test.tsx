import { act, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { OntologySummaryResponse, ViewCatalog, ViewCatalogEntry } from "../api/types";
import { deferredReply, withFakeFetch } from "../test/fakeFetch";
import { renderWithQueryClient } from "../test/renderWithQueryClient";
import { HomeTab } from "./HomeTab";
import { parseNotesLocation } from "./notesRoute";
import { viewSelectionKey } from "../views/useViewSelection";
import { viewSegment } from "../test/workspaceView";
import { preferenceServer } from "../viewPreferences/testServer";

const http = withFakeFetch();

const summary: OntologySummaryResponse = {
  schemaPresent: true,
  totalNotes: 0,
  typedNotes: 0,
  untypedNotes: 0,
  ambiguousNotes: 0,
  issueNotes: 0,
  types: [],
  interfaces: [],
};

const custom: ViewCatalogEntry = {
  id: "group.generic",
  name: "Dashboard",
  source: { kind: "custom", entry: "index.html" },
  mount: { kind: "group", group: "*", default: true },
  variants: {},
  defaults: {},
  definition: {
    variants: {},
    apiVersion: "rhizome.view.v1",
    id: "group.generic",
    name: "Dashboard",
    source: { kind: "custom", entry: "index.html" },
    mount: { kind: "group", group: "*", default: true },
  },
};

function registerReads() {
  http
    .json("GET", "/api/v1/ontology/summary", summary)
    .json("GET", "/api/v2/validate", { status: "never_ran", generation: 0 });
}

function renderAll() {
  const location = parseNotesLocation("/notes", "", "");

  return renderWithQueryClient(
    <HomeTab
      summary={summary}
      selection={location.selection}
      selectedType="__all__"
      location={location}
      editSession={{ session: null, replaceOps: async () => undefined, vaultKey: "vault" }}
      onOpenNote={vi.fn()}
      onSelectCollection={vi.fn()}
      onStageOps={async () => undefined}
    />,
  );
}

function renderGroup(group: string) {
  const location = parseNotesLocation(`/notes/group/${encodeURIComponent(group)}`, "", "");

  return renderWithQueryClient(
    <HomeTab
      summary={summary}
      selection={location.selection}
      selectedType={null}
      location={location}
      editSession={{ session: null, replaceOps: async () => undefined, vaultKey: "vault" }}
      onOpenNote={vi.fn()}
      onSelectCollection={vi.fn()}
      onStageOps={async () => undefined}
    />,
  );
}

describe("mounted collection defaults", () => {
  it("waits for the catalog and launches a generic group view with the concrete group", async () => {
    registerReads();
    const pending = deferredReply<ViewCatalog>();
    http.on("GET", "/api/v1/views", () => pending.promise);
    renderGroup("Delivery");
    expect(screen.getByRole("status")).toHaveTextContent("Loading views");
    expect(screen.queryByTitle("Dashboard")).toBeNull();
    expect(http.count("GET", "/api/v1/graphs/global")).toBe(0);
    await act(async () =>
      pending.resolve({
        views: [custom],
        targets: [
          {
            kind: "group",
            name: "Delivery",
            defaultChoiceId: "dashboard",
            choices: [
              { id: "builtin:overview", name: "Overview", renderer: "overview" },
              { id: "dashboard", name: "Dashboard", renderer: "custom", viewId: custom.id },
            ],
          },
        ],
      }),
    );
    const frame = await screen.findByTitle("Dashboard");
    const params = new URL(frame.getAttribute("src")!, "http://localhost").searchParams;
    expect(JSON.parse(params.get("context")!)).toEqual({ kind: "group", group: "Delivery" });
    expect(http.count("GET", "/api/v1/graphs/global")).toBe(0);
    await waitFor(() => expect(viewSegment("Dashboard")).toHaveAttribute("aria-pressed", "true"));
  });

  it("waits for the summary before migrating an interface's legacy preference", async () => {
    const preferences = preferenceServer(http);
    const pending = deferredReply<OntologySummaryResponse>();
    http.on("GET", "/api/v1/ontology/summary", () => pending.promise);
    http.json("GET", "/api/v2/validate", { status: "never_ran", generation: 0 });
    http.json("GET", "/api/v1/views", { views: [], targets: [] });
    http.json("GET", "/api/v1/ontology/types/Story", { count: 0, notes: [] });
    http.json("GET", "/api/v1/graphs/global", { nodes: [], edges: [], truncated: false });
    window.localStorage.setItem("rhizome:notes:viewModeByType", JSON.stringify({ Story: "home" }));
    const location = parseNotesLocation("/notes/story", "", "");
    renderWithQueryClient(
      <HomeTab
        summary={null}
        selection={location.selection}
        selectedType="Story"
        location={location}
        editSession={{ session: null, replaceOps: async () => undefined, vaultKey: "vault" }}
        onOpenNote={vi.fn()}
        onSelectCollection={vi.fn()}
        onStageOps={async () => undefined}
      />,
    );
    await waitFor(() => expect(http.count("GET", "/api/v1/views")).toBe(1));
    // Let the catalog response settle while the summary is still pending.
    await act(() => new Promise((resolve) => setTimeout(resolve, 0)));
    expect(window.localStorage.getItem("rhizome:notes:viewModeByType")).toBe('{"Story":"home"}');
    expect(http.count("POST", "/api/v1/view-preferences/import")).toBe(0);
    await act(async () =>
      pending.resolve({ ...summary, interfaces: [{ name: "Story", count: 0, implementors: [] }] }),
    );
    await waitFor(() =>
      expect(
        preferences.read({
          viewId: "$selection",
          context: { kind: "interface", interface: "Story" },
        }).values.choice,
      ).toBe("builtin:overview"),
    );
    expect(
      preferences.read({ viewId: "$selection", context: { kind: "type", type: "Story" } }).values,
    ).toEqual({});
    expect(
      window.localStorage.getItem(viewSelectionKey("vault", { kind: "type", name: "Story" })),
    ).toBeNull();
  });

  it("leaves a type's note and link counts to its Briefing", async () => {
    registerReads();

    const briefing: ViewCatalogEntry = {
      ...custom,
      id: "type.briefing",
      name: "Briefing",
      mount: { kind: "type", type: "*" },
      definition: { ...custom.definition, id: "type.briefing", mount: { kind: "type", type: "*" } },
    };

    http.json("GET", "/api/v1/views", {
      views: [briefing],
      targets: [
        {
          kind: "type",
          name: "Story",
          defaultChoiceId: "briefing",
          choices: [
            { id: "briefing", name: "Briefing", renderer: "custom", viewId: briefing.id },
            { id: "table", name: "Table", renderer: "table", viewId: "generated.type.Story" },
          ],
        },
      ],
    });
    http.json("GET", "/api/v1/ontology/types/Story", {
      count: 2,
      notes: [{ path: "a.md", title: "A", relationCount: 3 }],
    });
    const location = parseNotesLocation("/notes/story", "", "");
    renderWithQueryClient(
      <HomeTab
        summary={{ ...summary, types: [{ name: "Story", count: 2 }] }}
        selection={location.selection}
        selectedType="Story"
        location={location}
        editSession={{ session: null, replaceOps: async () => undefined, vaultKey: "vault" }}
        onOpenNote={vi.fn()}
        onSelectCollection={vi.fn()}
        onStageOps={async () => undefined}
      />,
    );
    await screen.findByTitle("Briefing");
    await waitFor(() =>
      expect(http.count("GET", "/api/v1/ontology/types/Story")).toBeGreaterThan(0),
    );
    expect(screen.queryByText("avg links")).toBeNull();
    expect(screen.queryByText("notes", { selector: ".ontology-home__stat span" })).toBeNull();
  });

  it("explains a summary failure on a group overview and retries", async () => {
    http.json("GET", "/api/v1/ontology/summary", { error: "offline" }, 503);
    http.json("GET", "/api/v2/validate", { status: "never_ran", generation: 0 });
    http.json("GET", "/api/v1/views", { views: [], targets: [] });
    renderGroup("Empty");
    expect(await screen.findByText(/Check that the Rhizome server is reachable/)).toBeVisible();
    expect(screen.queryByText(/No types are in/)).toBeNull();
    const before = http.count("GET", "/api/v1/ontology/summary");
    act(() => screen.getByRole("button", { name: "Retry connection" }).click());
    await waitFor(() =>
      expect(http.count("GET", "/api/v1/ontology/summary")).toBeGreaterThan(before),
    );
  });

  it("says so when a group overview has no types", async () => {
    registerReads();
    http.json("GET", "/api/v1/views", { views: [], targets: [] });
    renderGroup("Empty");
    expect(await screen.findByText("No types are in Empty.")).toBeVisible();
  });

  it("opens All notes on the workspace target's default with a workspace context", async () => {
    registerReads();

    const briefing: ViewCatalogEntry = {
      ...custom,
      id: "workspace.briefing",
      name: "Briefing",
      mount: { kind: "workspace" },
      definition: { ...custom.definition, id: "workspace.briefing", mount: { kind: "workspace" } },
    };

    http.json("GET", "/api/v1/views", {
      views: [briefing],
      targets: [
        {
          kind: "workspace",
          name: "",
          defaultChoiceId: "briefing",
          choices: [
            { id: "briefing", name: "Briefing", renderer: "custom", viewId: briefing.id },
            { id: "overview", name: "Overview", renderer: "custom", viewId: "workspace.overview" },
          ],
        },
      ],
    });
    renderAll();

    const frame = await screen.findByTitle("Briefing");
    const params = new URL(frame.getAttribute("src")!, "http://localhost").searchParams;
    expect(JSON.parse(params.get("context")!)).toEqual({ kind: "workspace" });
    await waitFor(() => expect(viewSegment("Briefing")).toHaveAttribute("aria-pressed", "true"));
    expect(viewSegment("Overview")).toHaveAttribute("aria-pressed", "false");
    expect(http.count("GET", "/api/v1/graphs/global")).toBe(0);
    expect(http.count("GET", "/api/v1/ontology/types/__all__")).toBe(0);
  });

  it("shows an error with retry instead of a graph when All notes cannot load its views", async () => {
    registerReads();
    http.json("GET", "/api/v1/views", { error: "offline" }, 503);
    renderAll();

    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("All notes views could not be loaded.");
    expect(http.count("GET", "/api/v1/graphs/global")).toBe(0);
    const before = http.count("GET", "/api/v1/views");
    act(() => within(alert).getByRole("button", { name: "Retry" }).click());
    await waitFor(() => expect(http.count("GET", "/api/v1/views")).toBeGreaterThan(before));
  });

  it("labels a group's built-in fallback Types when the catalog cannot load", async () => {
    registerReads();
    http.json("GET", "/api/v1/views", { error: "offline" }, 503);
    renderGroup("Delivery");

    expect(await screen.findByText(/Types remains available/)).toBeVisible();
    expect(screen.getByText("No types are in Delivery.")).toBeVisible();
    expect(screen.queryByText(/Overview/)).toBeNull();
  });
});
