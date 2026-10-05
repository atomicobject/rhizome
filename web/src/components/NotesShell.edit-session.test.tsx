import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterAll, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import { nodeWorkspaceFromPublicGraphQL } from "../api/nodeWorkspaceAdapter";
import { decodeJson, isJsonObject, isString } from "../api/parse";
import type { PublicGraphQLResult, PublicNodeDetailData } from "../api/publicGraphQLTypes";
import type { OntologyEditSessionResponse, OntologyEditSessionSnapshot } from "../api/types";
import { deferredReply, graphQLRequestBody, jsonReply, withFakeFetch } from "../test/fakeFetch";
import { withFakeEventSource } from "../test/fakeEventSource";
import { renderWithQueryClient as render } from "../test/renderWithQueryClient";
import { writePersistedEditorDraft } from "./editing/draftStorage";
import { EDITOR_FLUSH_EVENT } from "./editing/editorFlush";
import { NotesShell } from "./NotesShell";

const path = "notes/alpha.md";

const snapshot: OntologyEditSessionSnapshot = {
  sessionId: "restored-1",
  ops: [{ kind: "setField", path, field: "title", value: "Saved Alpha" }],
  baseFingerprints: { [path]: "original" },
};

function session(status: "rebased" | "conflicted" | "clean"): OntologyEditSessionResponse {
  const dirty = status !== "clean";

  return {
    sessionId: "restored-1",
    status,
    revision: dirty ? 1 : 2,
    ops: dirty ? snapshot.ops : [],
    touchedPaths: [path],
    touchedNodes: [path],
    hasUncommittedChanges: dirty,
    createdAt: "2026-09-06T00:00:00Z",
    updatedAt: status === "clean" ? "2026-09-06T00:00:02Z" : "2026-09-06T00:00:01Z",
    conflicts:
      status === "conflicted"
        ? [
            {
              kind: "field_drift",
              message: "Title changed on disk",
              notePath: path,
              field: "title",
            },
          ]
        : [],
  };
}

function nodeReply(title = "Alpha"): PublicGraphQLResult<PublicNodeDetailData> {
  return {
    data: {
      node: {
        ref: { ref: path, kind: "NOTE", notePath: path, path, nodeId: `note:${path}` },
        nodeId: `note:${path}`,
        nodeKind: "NOTE",
        path,
        title,
        content: "# Alpha\n\nA note being edited.",
        locator: { sourceLocator: path, status: "linkable", exists: true, requiresFix: false },
      },
    },
  };
}

const nodeStatus = {
  dirty: false,
  validation: { issueCount: 0 },
  freshness: {},
  session: {},
  hasWarnings: false,
};

function editableNodeData(
  statusValue: string,
  body: string,
  notePath = path,
  title = "Alpha",
): PublicNodeDetailData {
  const ref = {
    ref: notePath,
    kind: "NOTE",
    notePath,
    path: notePath,
    nodeId: `note:${notePath}`,
    typeName: "Spec",
  };

  return {
    node: {
      ref,
      nodeId: `note:${notePath}`,
      nodeKind: "NOTE",
      path: notePath,
      title,
      resolvedType: "Spec",
      content: `# Alpha\n\n${body}`,
      locator: { sourceLocator: notePath, status: "linkable", exists: true, requiresFix: false },
      workspace: {
        fields: [],
        collections: [],
        structure: [],
        relationGroups: [],
        capabilities: {
          canEdit: true,
          canEditFields: true,
          canEditCollections: true,
          canNavigateChildren: true,
          canSubscribe: true,
        },
        status: nodeStatus,
        version: statusValue,
        assessment: {
          notePath,
          declaredType: "Spec",
          resolvedType: "Spec",
          candidateTypes: [],
          issues: [],
          fields: [
            {
              name: "specStatus",
              kind: "FRONTMATTER_FIELD",
              typeName: "SpecStatus",
              required: true,
              list: false,
              present: true,
              values: [statusValue],
              validValues: ["server-ready", "staged-done"],
              issues: [],
            },
          ],
          relations: [],
        },
        bodies: [
          {
            ref,
            title,
            resolvedType: "Spec",
            locator: "FILE",
            markdown: body,
            fields: [
              {
                name: "specStatus",
                kind: "FRONTMATTER_FIELD",
                capability: {
                  ownerRef: ref,
                  ownerType: "Spec",
                  typeName: "SpecStatus",
                  valueKind: "enum",
                  list: false,
                  required: true,
                  enumValues: ["server-ready", "staged-done"],
                  sourceKind: "FRONTMATTER_FIELD",
                  valueOrigin: "authored",
                  identifier: false,
                  preferredIdentifier: false,
                  displayImportance: "NORMAL",
                  writeOperation: "setField",
                },
                present: true,
                status: nodeStatus,
                range: { start: 0, end: statusValue.length },
                valueRanges: [{ start: 0, end: statusValue.length }],
                values: [statusValue],
                sectionNodes: [],
              },
            ],
            collections: [],
            blocks: [
              {
                kind: "narrative",
                range: { start: 0, end: body.length },
                markdown: body,
                childRefs: [],
              },
            ],
          },
        ],
        loaded: { rendered: true, assessment: true, structure: true, relations: true },
      },
    },
  };
}

function statusOp(notePath: string, value: string) {
  return {
    kind: "setField" as const,
    path: notePath,
    nodeId: `note:${notePath}`,
    field: "specStatus",
    value,
  };
}

const emptyAllType = {
  count: 0,
  issueCount: 0,
  type: { name: "All", fields: [] },
  notes: [],
};

function dirtySession(
  sessionId: string,
  ops: ReturnType<typeof statusOp>[],
  workspaces: OntologyEditSessionResponse["workspaces"],
  revision = 1,
): OntologyEditSessionResponse {
  const paths = [...new Set(ops.map((op) => op.path))];

  return {
    sessionId,
    status: "dirty",
    revision,
    ops,
    touchedPaths: paths,
    touchedNodes: paths.map((notePath) => `note:${notePath}`),
    touchedNodeRefs: paths.map((notePath) => ({
      notePath,
      nodeId: `note:${notePath}`,
      kind: "NOTE",
    })),
    hasUncommittedChanges: true,
    createdAt: "2026-09-06T00:00:00Z",
    updatedAt: "2026-09-06T00:00:01Z",
    workspaces,
  };
}

const http = withFakeFetch();

withFakeEventSource();

beforeAll(() => {
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
});

afterAll(() => vi.unstubAllGlobals());

beforeEach(() => {
  window.history.replaceState({}, "", `/notes?note=${encodeURIComponent(path)}`);
  window.localStorage.clear();
  window.sessionStorage.clear();
  window.sessionStorage.setItem("rhizome:ontology-edit-session:tab-id", "shell-test");
  window.localStorage.setItem(
    "rhizome:ontology-edit-session:%2Ftest-vault:shell-test",
    JSON.stringify(snapshot),
  );
  http
    .json("GET", "/api/v1/ontology/summary", {
      schemaPresent: true,
      totalNotes: 1,
      typedNotes: 0,
      untypedNotes: 1,
      ambiguousNotes: 0,
      issueNotes: 0,
      types: [],
      interfaces: [],
    })
    .json("GET", "/api/v1/ontology/types/__all__", emptyAllType)
    .json("POST", "/api/v1/ontology/types/__all__", emptyAllType)
    .json("GET", "/api/v1/graphs/global", { nodes: [], edges: [], truncated: false })
    .json("GET", "/api/v1/views", { views: [] })
    .json("GET", "/api/v2/validate", { status: "never_ran", generation: 0 });
});

describe("NotesShell restored edit sessions", () => {
  it("keeps the retained tab on Edit, applies staged workspaces, and reloads after discard", async () => {
    window.localStorage.clear();
    const diskData = editableNodeData("server-ready", "Disk body.");
    const stagedData = editableNodeData("staged-done", "Staged body.");
    const graphQLBodies: ReturnType<typeof graphQLRequestBody>[] = [];
    http.onGraphQL("PublicNodeDetail", (request) => {
      graphQLBodies.push(graphQLRequestBody(request));

      return jsonReply({ data: diskData });
    });
    http.json(
      "POST",
      "/api/v1/edit-sessions",
      dirtySession(
        "created-1",
        [statusOp(path, "staged-done")],
        [nodeWorkspaceFromPublicGraphQL(stagedData, path)],
      ),
    );
    http.json("DELETE", "/api/v1/edit-sessions/created-1", {});

    render(<NotesShell />);
    const notePanel = within(await screen.findByRole("tabpanel", { name: /Alpha/ }));
    await notePanel.findByText("Disk body.");
    const tabs = within(screen.getByRole("tablist", { name: "Open notes" }));
    const noteTab = tabs.getByRole("tab", { name: "Alpha" });
    expect(noteTab).not.toHaveClass("notes-tab--preview");

    fireEvent.click(screen.getByRole("button", { name: "Edit" }));
    expect(noteTab).not.toHaveClass("notes-tab--preview");
    expect(http.count("POST", "/api/v1/edit-sessions")).toBe(0);

    fireEvent.click(notePanel.getAllByRole("button", { name: "Staged done" })[0]);
    await waitFor(() =>
      expect(
        notePanel
          .getAllByRole("button", { name: "Staged done" })
          .every((button) => button.getAttribute("aria-pressed") === "true"),
      ).toBe(true),
    );
    await waitFor(() =>
      expect(notePanel.getByRole("textbox", { name: "Narrative markdown" })).toHaveTextContent(
        "Staged body.",
      ),
    );
    const context = within(screen.getByRole("complementary", { name: "Note context" }));
    expect(context.queryByText("Staged done")).not.toBeInTheDocument();
    expect(http.count("POST", "/api/v1/edit-sessions")).toBe(1);
    expect(http.count("POST", "/api/v1/edit-sessions/created-1/stage")).toBe(0);
    expect(graphQLBodies).toHaveLength(1);

    window.localStorage.setItem("rhizome:edit-draft:narrative:older-fingerprint", "discarded");
    const discardButton = screen.getByRole("button", { name: "Discard" });
    expect(fireEvent.mouseDown(discardButton)).toBe(false);
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(true);
    fireEvent.click(discardButton);
    expect(confirm).toHaveBeenCalledWith(
      "Discard 1 staged change to 1 note? This cannot be undone.",
    );
    confirm.mockRestore();
    await notePanel.findByText("Disk body.");
    expect(notePanel.queryByText("Staged body.")).not.toBeInTheDocument();
    expect(notePanel.getAllByText("Server ready").length).toBeGreaterThan(0);
    expect(context.queryByText("Server ready")).not.toBeInTheDocument();
    expect(
      Object.keys(window.localStorage).filter((key) => key.startsWith("rhizome:edit-draft:")),
    ).toEqual([]);
    expect(graphQLBodies).toHaveLength(2);
    expect(graphQLBodies[1]?.variables).not.toHaveProperty("editSession");
  });

  it("reloads canonical values when staging prunes the final operation", async () => {
    window.localStorage.clear();
    const diskData = editableNodeData("server-ready", "Disk body.");
    const stagedData = editableNodeData("staged-done", "Staged body.");
    const graphQLBodies: ReturnType<typeof graphQLRequestBody>[] = [];
    http.onGraphQL("PublicNodeDetail", (request) => {
      graphQLBodies.push(graphQLRequestBody(request));

      return jsonReply({ data: diskData });
    });
    http.json(
      "POST",
      "/api/v1/edit-sessions",
      dirtySession(
        "created-1",
        [statusOp(path, "staged-done")],
        [nodeWorkspaceFromPublicGraphQL(stagedData, path)],
      ),
    );
    http.json("POST", "/api/v1/edit-sessions/created-1/stage", {
      sessionId: "created-1",
      status: "clean",
      revision: 2,
      ops: [],
      touchedPaths: [],
      touchedNodes: [],
      touchedNodeRefs: [],
      hasUncommittedChanges: false,
      createdAt: "2026-09-06T00:00:00Z",
      updatedAt: "2026-09-06T00:00:02Z",
      workspaces: [],
    } satisfies OntologyEditSessionResponse);

    render(<NotesShell />);
    const notePanel = within(await screen.findByRole("tabpanel", { name: /Alpha/ }));
    await notePanel.findByText("Disk body.");
    fireEvent.click(screen.getByRole("button", { name: "Edit" }));
    fireEvent.click(notePanel.getAllByRole("button", { name: "Staged done" })[0]);
    await waitFor(() =>
      expect(notePanel.getByRole("textbox", { name: "Narrative markdown" })).toHaveTextContent(
        "Staged body.",
      ),
    );

    fireEvent.click(notePanel.getAllByRole("button", { name: "Server ready" })[0]);
    await waitFor(() =>
      expect(notePanel.getByRole("textbox", { name: "Narrative markdown" })).toHaveTextContent(
        "Disk body.",
      ),
    );
    expect(graphQLBodies).toHaveLength(2);
    expect(graphQLBodies[1]?.variables).not.toHaveProperty("editSession");
  });

  it("refreshes removed notes while preserving surviving overlays after a partial replacement", async () => {
    const betaPath = "notes/beta.md";
    const alphaOp = statusOp(path, "staged-done");
    const betaOp = statusOp(betaPath, "staged-done");

    const replacementSnapshot: OntologyEditSessionSnapshot = {
      sessionId: "restored-2",
      ops: [alphaOp, betaOp],
      baseFingerprints: { [path]: "alpha-base", [betaPath]: "beta-base" },
    };

    window.localStorage.setItem(
      "rhizome:ontology-edit-session:%2Ftest-vault:shell-test",
      JSON.stringify(replacementSnapshot),
    );
    const alphaDisk = editableNodeData("server-ready", "Alpha disk body.");
    const alphaStaged = editableNodeData("staged-done", "Alpha staged body.");
    const betaStaged = editableNodeData("staged-done", "Beta staged body.", betaPath, "Beta");

    const initialSession = {
      ...dirtySession(
        "restored-2",
        [alphaOp, betaOp],
        [
          nodeWorkspaceFromPublicGraphQL(alphaStaged, path),
          nodeWorkspaceFromPublicGraphQL(betaStaged, betaPath),
        ],
      ),
      status: "rebased",
    } satisfies OntologyEditSessionResponse;

    const remainingSession = dirtySession(
      "restored-2",
      [betaOp],
      [nodeWorkspaceFromPublicGraphQL(betaStaged, betaPath)],
      2,
    );

    http.json("POST", "/api/v1/edit-sessions/restored-2/preview", initialSession);
    http.onGraphQL("PublicNodeDetail", (request) => {
      const body = graphQLRequestBody(request);
      const ref = body.variables?.ref;
      const overlay = decodeJson(request.body ?? "null", isJsonObject)?.editSession;

      const overlaySnapshot = isJsonObject(overlay) ? overlay.snapshot : null;

      const alphaData =
        isJsonObject(overlaySnapshot) && overlaySnapshot.revision === initialSession.revision
          ? alphaStaged
          : alphaDisk;

      return jsonReply({ data: ref === betaPath ? betaStaged : alphaData });
    });
    http.json("POST", "/api/v1/edit-sessions/restored-2/diff", {
      sessionId: "restored-2",
      totals: { notes: 2, ops: 2, setField: 2 },
      notes: [
        {
          path,
          title: "Alpha",
          hasMaterialChange: true,
          ops: [
            {
              kind: "setField",
              nodeRef: { notePath: path, nodeId: `note:${path}`, kind: "NOTE" },
              field: "specStatus",
              previousValue: "server-ready",
              value: "staged-done",
            },
          ],
        },
        {
          path: betaPath,
          title: "Beta",
          hasMaterialChange: true,
          ops: [
            {
              kind: "setField",
              nodeRef: { notePath: betaPath, nodeId: `note:${betaPath}`, kind: "NOTE" },
              field: "specStatus",
              previousValue: "server-ready",
              value: "staged-done",
            },
          ],
        },
      ],
      updatedAt: "2026-09-06T00:00:01Z",
    });
    http.json("POST", "/api/v1/edit-sessions/restored-2/stage", remainingSession);

    render(<NotesShell />);
    const alphaPanel = within(await screen.findByRole("tabpanel", { name: /Alpha/ }));
    await waitFor(() =>
      expect(alphaPanel.getByRole("textbox", { name: "Narrative markdown" })).toHaveTextContent(
        "Alpha staged body.",
      ),
    );
    act(() => {
      window.history.pushState({}, "", `/notes?note=${encodeURIComponent(betaPath)}`);
      window.dispatchEvent(new PopStateEvent("popstate"));
    });
    const betaPanel = within(await screen.findByRole("tabpanel", { name: /beta/i }));
    await waitFor(() =>
      expect(betaPanel.getByRole("textbox", { name: "Narrative markdown" })).toHaveTextContent(
        "Beta staged body.",
      ),
    );

    fireEvent.click(screen.getByRole("button", { name: "Review" }));
    fireEvent.click(await screen.findByRole("button", { name: `Discard all changes to ${path}` }));

    const tabs = within(screen.getByRole("tablist", { name: "Open notes" }));
    fireEvent.click(await tabs.findByRole("tab", { name: "Alpha" }));
    await waitFor(() =>
      expect(alphaPanel.getByRole("textbox", { name: "Narrative markdown" })).toHaveTextContent(
        "Alpha disk body.",
      ),
    );
    fireEvent.click(await tabs.findByRole("tab", { name: /Beta, unsaved changes/i }));
    expect(betaPanel.getByRole("textbox", { name: "Narrative markdown" })).toHaveTextContent(
      "Beta staged body.",
    );
  });

  it("blocks controls during restoration, retains the dirty tab, and refreshes its canonical note after save", async () => {
    const restore = deferredReply<OntologyEditSessionResponse>();
    let committed = false;
    const nodeRefs: string[] = [];
    http.on("POST", "/api/v1/edit-sessions/restored-1/preview", (request) => {
      expect(JSON.parse(request.body || "null")).toEqual({
        snapshot: { ...snapshot, version: 3, revision: 0, baseDocuments: [] },
      });

      return restore.promise;
    });
    http.onGraphQL("PublicNodeDetail", (request) => {
      const ref = graphQLRequestBody(request).variables?.ref;

      if (isString(ref)) nodeRefs.push(ref);

      return jsonReply(nodeReply(committed ? "Saved Alpha" : "Alpha"));
    });
    http.on("POST", "/api/v1/edit-sessions/restored-1/commit", () => {
      committed = true;

      return jsonReply(session("clean"));
    });
    render(<NotesShell />);
    expect(await screen.findByRole("button", { name: "Save" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Done" })).toBeDisabled();
    await within(await screen.findByRole("tabpanel", { name: /Alpha/ })).findByRole("heading", {
      name: "Alpha",
      level: 2,
    });
    restore.resolve(session("rebased"));
    const tabs = within(screen.getByRole("tablist", { name: "Open notes" }));
    const dirtyTab = await tabs.findByRole("tab", { name: "Alpha, unsaved changes" });
    expect(dirtyTab).not.toHaveClass("notes-tab--preview");
    await waitFor(() => expect(screen.getByRole("button", { name: "Save" })).toBeEnabled());
    const readsBeforeSave = nodeRefs.length;
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await within(await screen.findByRole("tabpanel", { name: /Saved Alpha/ })).findByRole(
      "heading",
      { name: "Saved Alpha" },
    );
    expect(nodeRefs.length).toBeGreaterThan(readsBeforeSave);
    expect(nodeRefs.at(-1)).toBe(path);
    expect(window.location.search).toBe(`?note=${encodeURIComponent(path)}`);
    expect(await tabs.findByRole("tab", { name: "Saved Alpha" })).not.toHaveClass(
      "notes-tab--preview",
    );
    expect(screen.queryByRole("tab", { name: /unsaved changes/ })).not.toBeInTheDocument();
  });

  it("re-reads an open note with the restored session once restoration finishes", async () => {
    const restore = deferredReply<OntologyEditSessionResponse>();
    http.on("POST", "/api/v1/edit-sessions/restored-1/preview", () => restore.promise);
    http.onGraphQL("PublicNodeDetail", (request) => {
      const staged = "editSession" in JSON.parse(request.body || "{}");

      return jsonReply(nodeReply(staged ? "Saved Alpha" : "Alpha"));
    });

    render(<NotesShell />);
    const notePanel = within(await screen.findByRole("tabpanel", { name: /Alpha/ }));
    await notePanel.findByRole("heading", { name: "Alpha", level: 2 });

    restore.resolve(session("rebased"));
    expect(await notePanel.findByRole("heading", { name: "Saved Alpha", level: 2 })).toBeVisible();
  });

  it("waits for registered editor staging before starting a save", async () => {
    const restore = deferredReply<OntologyEditSessionResponse>();
    http.on("POST", "/api/v1/edit-sessions/restored-1/preview", () => restore.promise);
    http.onGraphQL("PublicNodeDetail", () => jsonReply(nodeReply()));
    http.json("POST", "/api/v1/edit-sessions/restored-1/commit", session("clean"));
    let releaseFlush: (() => void) | undefined;
    const pendingFlush = new Promise<void>((resolve) => (releaseFlush = resolve));

    const registerPendingFlush = (event: Event) => {
      // SAFETY: this listener is registered only for Rhizome's internal editor
      // flush event, whose dispatcher supplies the waitUntil detail contract.
      const detail = (event as CustomEvent<{ waitUntil: (pending: Promise<void>) => void }>).detail;
      detail.waitUntil(pendingFlush);
    };

    window.addEventListener(EDITOR_FLUSH_EVENT, registerPendingFlush);

    render(<NotesShell />);
    await within(await screen.findByRole("tabpanel", { name: /Alpha/ })).findByRole("heading", {
      name: "Alpha",
      level: 2,
    });
    restore.resolve(session("rebased"));
    await waitFor(() => expect(screen.getByRole("button", { name: "Save" })).toBeEnabled());
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await act(async () => Promise.resolve());
    expect(http.count("POST", "/api/v1/edit-sessions/restored-1/commit")).toBe(0);

    releaseFlush?.();
    await waitFor(() =>
      expect(http.count("POST", "/api/v1/edit-sessions/restored-1/commit")).toBe(1),
    );
    window.removeEventListener(EDITOR_FLUSH_EVENT, registerPendingFlush);
  });

  it("does not start a save when editor staging fails", async () => {
    const restore = deferredReply<OntologyEditSessionResponse>();
    http.on("POST", "/api/v1/edit-sessions/restored-1/preview", () => restore.promise);
    http.onGraphQL("PublicNodeDetail", () => jsonReply(nodeReply()));
    http.json("POST", "/api/v1/edit-sessions/restored-1/commit", session("clean"));

    const rejectFlush = (event: Event) => {
      // SAFETY: this listener is registered only for Rhizome's internal editor
      // flush event, whose dispatcher supplies the waitUntil detail contract.
      const detail = (event as CustomEvent<{ waitUntil: (pending: Promise<void>) => void }>).detail;
      detail.waitUntil(Promise.reject(new Error("offline")));
    };

    window.addEventListener(EDITOR_FLUSH_EVENT, rejectFlush);

    render(<NotesShell />);
    await within(await screen.findByRole("tabpanel", { name: /Alpha/ })).findByRole("heading", {
      name: "Alpha",
      level: 2,
    });
    restore.resolve(session("rebased"));
    await waitFor(() => expect(screen.getByRole("button", { name: "Save" })).toBeEnabled());
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await act(async () => Promise.resolve());

    expect(http.count("POST", "/api/v1/edit-sessions/restored-1/commit")).toBe(0);
    expect(screen.getByRole("button", { name: "Save" })).toBeEnabled();
    window.removeEventListener(EDITOR_FLUSH_EVENT, rejectFlush);
  });

  it("stays in edit mode when a newer local draft appears during a successful save", async () => {
    const restore = deferredReply<OntologyEditSessionResponse>();
    const save = deferredReply<OntologyEditSessionResponse>();
    let committed = false;
    http.on("POST", "/api/v1/edit-sessions/restored-1/preview", () => restore.promise);
    http.onGraphQL("PublicNodeDetail", () =>
      jsonReply(nodeReply(committed ? "Saved Alpha" : "Alpha")),
    );
    http.on("POST", "/api/v1/edit-sessions/restored-1/commit", () => save.promise);

    render(<NotesShell />);
    await within(await screen.findByRole("tabpanel", { name: /Alpha/ })).findByRole("heading", {
      name: "Alpha",
      level: 2,
    });
    restore.resolve(session("rebased"));
    await waitFor(() => expect(screen.getByRole("button", { name: "Save" })).toBeEnabled());

    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() =>
      expect(http.count("POST", "/api/v1/edit-sessions/restored-1/commit")).toBe(1),
    );
    writePersistedEditorDraft("/test-vault", "field:notes/alpha.md:due", "2026-09-12");
    committed = true;
    save.resolve(session("clean"));

    await waitFor(() => expect(screen.getByText("Draft")).toBeVisible());
    expect(screen.getByRole("button", { name: "Save" })).toBeEnabled();
    expect(screen.getByRole("button", { name: "Discard" })).toBeEnabled();
  });

  it("shows restored conflicts, prevents saving, and respects dirty-close confirmation", async () => {
    http.json("POST", "/api/v1/edit-sessions/restored-1/preview", session("conflicted"));
    http.onGraphQL("PublicNodeDetail", () => jsonReply(nodeReply()));
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(false);
    render(<NotesShell />);
    await screen.findByText("Title changed on disk");
    expect(screen.getByText("1 conflict")).toBeVisible();
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
    const tabs = within(screen.getByRole("tablist", { name: "Open notes" }));
    const dirtyTab = await tabs.findByRole("tab", { name: "Alpha, unsaved changes" });
    fireEvent.click(within(dirtyTab).getByRole("button", { name: "Close Alpha" }));
    expect(confirm).toHaveBeenCalledWith(
      "Alpha has unsaved changes. Close it? They stay staged in Changes.",
    );
    expect(dirtyTab).toBeInTheDocument();
    confirm.mockReturnValue(true);
    fireEvent.click(within(dirtyTab).getByRole("button", { name: "Close Alpha" }));
    await waitFor(() => expect(tabs.getAllByRole("tab")).toHaveLength(1));
    expect(tabs.getByRole("tab", { name: "Home" })).toHaveAttribute("aria-selected", "true");
    expect(window.location.search).toBe("");
    expect(http.count("DELETE", "/api/v1/edit-sessions/restored-1")).toBe(0);
  });
});
