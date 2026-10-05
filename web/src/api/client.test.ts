import { describe, expect, it, vi } from "vitest";
import type { ApiError } from "./client";
import {
  executePublicView,
  fetchJSON,
  getExpandedModuleGraph,
  getGlobalGraph,
  getLocalGraph,
  getNodeWorkspace,
  getNodeWorkspaceGraph,
  getOntologyAtlas,
  getOntologyInspect,
  getOntologyQuerySchema,
  getOntologySummary,
  getOntologyType,
  getPublicNodeDetail,
  getPublicValidate,
  getPublicView,
  getRenderedFile,
  getStatus,
  getSuggestions,
  getTree,
  listPublicViewFieldCandidates,
  listPublicViews,
  previewOntologyEditSession,
  publicOntologyListItemRef,
  queryOntology,
  queryPublicGraphQLOperation,
  savePublicView,
  searchNotes,
  searchPaths,
} from "./client";
import { PUBLIC_LOCAL_GRAPH_QUERY, PUBLIC_NODE_DETAIL_QUERY } from "./graphql/operations";

function publicWorkspaceResponse(ref: string) {
  return {
    data: {
      node: {
        ref: { ref, kind: "NOTE", notePath: ref, path: ref },
        nodeId: `note:${ref}`,
        nodeKind: "NOTE",
        path: ref,
        title: "Spec",
        locator: {
          sourceLocator: ref,
          status: "linkable",
          exists: true,
          requiresFix: false,
        },
        content: "# Spec\n",
        localGraph: { nodes: [], edges: [], truncated: false },
      },
    },
  };
}

describe("api client", () => {
  it("uses canonical structural identity for ontology list items", () => {
    expect(
      publicOntologyListItemRef({
        path: "docs/spec.md#item-11146",
        ref: {
          notePath: "docs/spec.md",
          fragment: "item-11146",
          nodeId: "docs/spec.md#item-11146",
          kind: "EMBEDDED",
          structuralFingerprint: "criterion-fingerprint",
        },
      }),
    ).toBe("docs/spec.md#struct:criterion-fingerprint");
  });

  it("throws on non-ok responses", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: false,
        status: 503,
      }),
    );

    await expect(fetchJSON("/api/status")).rejects.toThrow("Request failed: 503");
  });

  it.each([200, 400])("preserves cancellation while reading a %i response body", async (status) => {
    let bodyController!: ReadableStreamDefaultController<Uint8Array>;

    const body = new ReadableStream<Uint8Array>({
      start(controller) {
        bodyController = controller;
      },
    });

    const response = new Response(body, {
      status,
      headers: { "content-type": "application/json" },
    });

    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response));
    const pending = fetchJSON("/api/v1/ontology/types/Person");
    const cancellation = new DOMException("The operation was aborted.", "AbortError");

    bodyController.error(cancellation);

    await expect(pending).rejects.toBe(cancellation);
  });

  it.each([
    [200, "application/json"],
    [400, "application/json"],
    [200, "text/html"],
    [502, "text/html"],
  ])(
    "preserves a custom abort reason during a pending %i %s body read",
    async (status, contentType) => {
      const controller = new AbortController();
      const cancellation = new Error("route changed");

      const body = new ReadableStream<Uint8Array>({
        start(bodyController) {
          controller.signal.addEventListener(
            "abort",
            () => {
              bodyController.error(controller.signal.reason);
            },
            { once: true },
          );
        },
      });

      const response = new Response(body, {
        status,
        headers: { "content-type": contentType },
      });

      const fetchMock = vi.fn().mockResolvedValue(response);

      vi.stubGlobal("fetch", fetchMock);
      const pending = fetchJSON("/api/status", { signal: controller.signal });
      const rejected = expect(pending).rejects.toBe(cancellation);

      await vi.waitFor(() => expect(response.bodyUsed).toBe(true));
      controller.abort(cancellation);

      await rejected;
      expect(fetchMock).toHaveBeenCalledTimes(1);
    },
  );

  it.each([
    [200, "Error", "Invalid JSON response"],
    [400, "ApiError", "Invalid JSON error response"],
  ])("still reports malformed JSON in a %i response", async (status, name, message) => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue(
          new Response("{", { status, headers: { "content-type": "application/json" } }),
        ),
    );

    await expect(fetchJSON("/api/status")).rejects.toMatchObject({
      name,
      message: `${message} from /api/status`,
    });
  });

  it("retries one bounded Retry-After response", async () => {
    const errorBody = {
      error: "index is initializing",
      code: "INDEX_INITIALIZING",
    };

    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce({
        ok: false,
        status: 503,
        headers: new Headers({
          "content-type": "application/json",
          "retry-after": "0",
        }),
        clone: vi.fn().mockReturnValue({
          headers: new Headers({ "content-type": "application/json" }),
          json: vi.fn().mockResolvedValue(errorBody),
        }),
        text: vi.fn().mockResolvedValue(""),
      })
      .mockResolvedValueOnce({
        ok: true,
        headers: new Headers({ "content-type": "application/json" }),
        json: vi.fn().mockResolvedValue({ indexState: "ready" }),
      });

    vi.stubGlobal("fetch", fetchMock);

    await expect(fetchJSON("/api/v1/status")).resolves.toEqual({
      indexState: "ready",
    });
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it.each([
    ["second initializing response", 0, 2],
    ["delay above the cap", 11, 1],
  ])("stops retrying after %s", async (_name, retryAfter, requestCount) => {
    if (retryAfter === 11) vi.useFakeTimers();

    const initializing = () =>
      new Response(JSON.stringify({ error: "index is initializing", code: "INDEX_INITIALIZING" }), {
        status: 503,
        headers: { "content-type": "application/json", "retry-after": String(retryAfter) },
      });

    const fetchMock = vi.fn().mockImplementation(initializing);
    vi.stubGlobal("fetch", fetchMock);

    const outcome = fetchJSON("/api/v1/status").then(
      () => "resolved",
      (error: ApiError) => error,
    );

    try {
      if (retryAfter === 11) {
        for (let i = 0; i < 30; i++) await Promise.resolve();

        expect(vi.getTimerCount()).toBe(0);
      }

      expect(await outcome).toMatchObject({ status: 503, code: "INDEX_INITIALIZING" });
      expect(fetchMock).toHaveBeenCalledTimes(requestCount);
    } finally {
      if (retryAfter === 11) {
        await vi.runAllTimersAsync();
        await outcome;
        vi.useRealTimers();
      }
    }
  });

  it("cancels the retry delay without issuing a second request", async () => {
    vi.useFakeTimers();

    try {
      const fetchMock = vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            error: "index is initializing",
            code: "INDEX_INITIALIZING",
          }),
          {
            status: 503,
            headers: { "content-type": "application/json", "retry-after": "5" },
          },
        ),
      );

      vi.stubGlobal("fetch", fetchMock);
      const controller = new AbortController();
      const cancellation = new Error("route changed");
      const pending = fetchJSON("/api/v1/graphs/global", { signal: controller.signal });

      const outcome = pending.then(
        () => null,
        (error: Error) => error,
      );

      for (let i = 0; i < 30 && vi.getTimerCount() === 0; i++) await Promise.resolve();
      expect(vi.getTimerCount()).toBe(1);
      controller.abort(cancellation);
      expect(vi.getTimerCount()).toBe(0);
      expect(await outcome).toBe(cancellation);
      await vi.advanceTimersByTimeAsync(5000);
      expect(fetchMock).toHaveBeenCalledTimes(1);
    } finally {
      await vi.runAllTimersAsync();
      vi.useRealTimers();
    }
  });

  it("does not replay unrelated 503 responses", async () => {
    const errorBody = { error: "service unavailable", code: "UPSTREAM_DOWN" };

    const fetchMock = vi.fn().mockResolvedValue({
      ok: false,
      status: 503,
      headers: new Headers({
        "content-type": "application/json",
        "retry-after": "0",
      }),
      clone: vi.fn().mockReturnValue({
        headers: new Headers({ "content-type": "application/json" }),
        json: vi.fn().mockResolvedValue(errorBody),
      }),
      json: vi.fn().mockResolvedValue(errorBody),
    });

    vi.stubGlobal("fetch", fetchMock);

    await expect(fetchJSON("/api/v1/views/action-items.open/execute")).rejects.toMatchObject({
      status: 503,
      code: "UPSTREAM_DOWN",
    });
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it("retries a read-only GraphQL POST after initialization", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({ error: "index is initializing", code: "INDEX_INITIALIZING" }),
          {
            status: 503,
            headers: { "content-type": "application/json", "retry-after": "0" },
          },
        ),
      )
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ data: { node: null } }), {
          headers: { "content-type": "application/json" },
        }),
      );

    vi.stubGlobal("fetch", fetchMock);
    await expect(getPublicNodeDetail("notes/new.md")).resolves.toEqual({ data: { node: null } });
    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(fetchMock.mock.calls[0]).toEqual(fetchMock.mock.calls[1]);
  });

  it("does not replay mutations while the index is initializing", async () => {
    const errorBody = {
      error: "index is initializing",
      code: "INDEX_INITIALIZING",
    };

    const fetchMock = vi.fn().mockResolvedValue({
      ok: false,
      status: 503,
      headers: new Headers({
        "content-type": "application/json",
        "retry-after": "0",
      }),
      clone: vi.fn().mockReturnValue({
        headers: new Headers({ "content-type": "application/json" }),
        json: vi.fn().mockResolvedValue(errorBody),
      }),
      json: vi.fn().mockResolvedValue(errorBody),
    });

    vi.stubGlobal("fetch", fetchMock);

    await expect(fetchJSON("/api/v1/edit-sessions", { method: "POST" })).rejects.toMatchObject({
      status: 503,
      code: "INDEX_INITIALIZING",
    });
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it("preserves public API error codes and details", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: false,
        status: 400,
        headers: new Headers({ "content-type": "application/json" }),
        json: vi.fn().mockResolvedValue({
          error: "view failed",
          code: "invalid_request",
          details: { field: "done", op: "contains" },
        }),
      }),
    );

    await expect(fetchJSON("/api/v1/views/action-items.open/execute")).rejects.toMatchObject({
      name: "ApiError",
      message: "view failed",
      status: 400,
      code: "invalid_request",
      details: { field: "done", op: "contains" },
    } satisfies Partial<ApiError>);
  });

  it("returns conflict payloads for edit session preview", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: false,
      status: 409,
      json: vi.fn().mockResolvedValue({
        sessionId: "sess-1",
        status: "conflicted",
        conflicts: [{ kind: "collection_drift" }],
      }),
    });

    vi.stubGlobal("fetch", fetchMock);

    await expect(previewOntologyEditSession("sess-1")).resolves.toMatchObject({
      sessionId: "sess-1",
      status: "conflicted",
      conflicts: [{ kind: "collection_drift" }],
    });
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/edit-sessions/sess-1/preview",
      expect.objectContaining({ method: "POST" }),
    );
  });

  it("rejects html fallback responses before JSON parsing", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: true,
        headers: new Headers({ "content-type": "text/html" }),
        text: vi.fn().mockResolvedValue("<!doctype html><div>app shell</div>"),
        json: vi.fn(),
      }),
    );

    await expect(fetchJSON("/api/agent/settings")).rejects.toThrow(
      "Expected JSON from /api/agent/settings, but received text/html",
    );
  });

  it("posts typed public GraphQL operations through the v1 endpoint", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      headers: new Headers({ "content-type": "application/json" }),
      json: vi.fn().mockResolvedValue({ data: { node: null } }),
    });

    vi.stubGlobal("fetch", fetchMock);

    await queryPublicGraphQLOperation<{ node: null }>(
      "query Test($ref: String!) { node(ref: $ref) { title } }",
      { ref: "docs/spec.md" },
      "Test",
    );

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/graphql",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({
          query: "query Test($ref: String!) { node(ref: $ref) { title } }",
          variables: { ref: "docs/spec.md" },
          operationName: "Test",
        }),
      }),
    );
  });

  it("injects edit-session snapshots into public read requests", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      headers: new Headers({ "content-type": "application/json" }),
      json: vi.fn().mockResolvedValue({ data: { node: null } }),
    });

    vi.stubGlobal("fetch", fetchMock);

    const editSession = {
      sessionId: "sess-1",
      status: "dirty" as const,
      revision: 1,
      ops: [
        {
          kind: "setField",
          path: "docs/spec.md",
          field: "status",
          value: "active",
        },
      ],
      baseFingerprints: { "docs/spec.md": "base-fp" },
      baseDocuments: [{ notePath: "docs/spec.md", fingerprint: "base-fp", content: "# Before" }],
      hasUncommittedChanges: true,
      createdAt: "2026-05-06T00:00:00Z",
      updatedAt: "2026-05-06T00:00:01Z",
    };

    await queryPublicGraphQLOperation<{ node: null }>(
      'query Test { node(ref: "docs/spec.md") { title } }',
      undefined,
      "Test",
      { editSession },
    );
    await executePublicView(
      "specs.default",
      { search: "active" },
      {
        editSession,
      },
    );

    const readRequest = {
      sessionId: "sess-1",
      snapshot: {
        version: 3,
        revision: 1,
        sessionId: "sess-1",
        ops: [{ kind: "setField", path: "docs/spec.md", field: "status", value: "active" }],
        baseFingerprints: { "docs/spec.md": "base-fp" },
        baseDocuments: [{ notePath: "docs/spec.md", fingerprint: "base-fp", content: "# Before" }],
      },
    };

    expect(fetchMock).toHaveBeenNthCalledWith(
      1,
      "/api/v1/graphql",
      expect.objectContaining({
        body: JSON.stringify({
          query: 'query Test { node(ref: "docs/spec.md") { title } }',
          operationName: "Test",
          editSession: readRequest,
        }),
      }),
    );
    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      "/api/v1/views/specs.default/execute",
      expect.objectContaining({
        body: JSON.stringify({
          search: "active",
          editSession: readRequest,
        }),
      }),
    );
    await listPublicViewFieldCandidates("specs.default", "owner", {
      editSession,
      limit: 50,
    });
    expect(fetchMock).toHaveBeenNthCalledWith(
      3,
      "/api/v1/views/specs.default/field-candidates?field=owner&limit=50",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({ editSession: readRequest }),
      }),
    );
  });

  it.each([
    [
      "unacknowledged",
      {
        sessionId: "sess-local",
        status: "dirty" as const,
        revision: undefined,
        hasUncommittedChanges: true,
      },
      undefined,
    ],
    [
      "conflicted",
      {
        sessionId: "sess-conflicted",
        status: "conflicted" as const,
        revision: 3,
        hasUncommittedChanges: true,
      },
      {
        sessionId: "sess-conflicted",
        snapshot: {
          version: 3,
          revision: 3,
          sessionId: "sess-conflicted",
          ops: [{ kind: "setField", path: "docs/spec.md", field: "summary", value: "Draft" }],
          baseFingerprints: { "docs/spec.md": "base-fp" },
          baseDocuments: [],
        },
      },
    ],
    [
      "clean",
      {
        sessionId: "sess-clean",
        status: "clean" as const,
        revision: 1,
        hasUncommittedChanges: false,
      },
      undefined,
    ],
  ])(
    "sends the appropriate %s edit overlay on public GraphQL reads",
    async (_name, state, expectedOverlay) => {
      const fetchMock = vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ data: { node: null } }), {
          headers: { "content-type": "application/json" },
        }),
      );

      vi.stubGlobal("fetch", fetchMock);

      const editSession = {
        ...state,
        ops: [{ kind: "setField", path: "docs/spec.md", field: "summary", value: "Draft" }],
        baseFingerprints: { "docs/spec.md": "base-fp" },
        createdAt: "2026-05-06T00:00:00Z",
        updatedAt: "2026-05-06T00:00:01Z",
      };

      await queryPublicGraphQLOperation<{ node: null }>(
        'query Test { node(ref: "docs/spec.md") { title } }',
        undefined,
        "Test",
        { editSession },
      );

      expect(fetchMock).toHaveBeenCalledTimes(1);
      const [, init] = fetchMock.mock.calls[0];

      const expectedBody = {
        query: 'query Test { node(ref: "docs/spec.md") { title } }',
        operationName: "Test",
      };

      const expectedRequest = expectedOverlay
        ? { ...expectedBody, editSession: expectedOverlay }
        : expectedBody;

      expect(JSON.parse(init.body)).toEqual(expectedRequest);
      expect(editSession.ops).toHaveLength(1);
    },
  );

  it("routes compatibility-named generic helpers to public v1 endpoints", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      headers: new Headers({ "content-type": "application/json" }),
      json: vi.fn().mockResolvedValue({ data: { notes: [] } }),
    });

    vi.stubGlobal("fetch", fetchMock);

    await queryOntology({ query: 'query { notes(type: "Spec") { path } }' });
    await getStatus();
    await getPublicValidate();
    await getTree("docs", 25);
    await getRenderedFile("docs/spec.md");
    await getSuggestions("spe", 7);
    await searchPaths("spec", 9);
    await searchNotes("spec", { type: "Spec", limit: 11 });
    await getOntologySummary();
    await getOntologyType("Spec");
    await getOntologyAtlas();
    await getOntologyInspect("docs/spec.md");
    await getOntologyQuerySchema();
    await getGlobalGraph({ notesOnly: true });
    await getLocalGraph("docs/spec.md", 25, { diagnostics: true });
    await getExpandedModuleGraph("src/app", 33);

    expect(fetchMock).toHaveBeenNthCalledWith(
      1,
      "/api/v1/graphql",
      expect.objectContaining({ method: "POST" }),
    );
    expect(fetchMock).toHaveBeenNthCalledWith(2, "/api/v1/status", undefined);
    expect(fetchMock).toHaveBeenNthCalledWith(3, "/api/v2/validate", undefined);
    expect(fetchMock).toHaveBeenNthCalledWith(
      4,
      "/api/v1/files/tree?path=docs&limit=25",
      undefined,
    );
    expect(fetchMock).toHaveBeenNthCalledWith(
      5,
      "/api/v1/files/rendered?path=docs%2Fspec.md",
      undefined,
    );
    expect(fetchMock).toHaveBeenNthCalledWith(6, "/api/v1/suggest?q=spe&limit=7", undefined);
    expect(fetchMock).toHaveBeenNthCalledWith(7, "/api/v1/search?q=spec&limit=9", undefined);
    expect(fetchMock).toHaveBeenNthCalledWith(
      8,
      "/api/v1/search/notes?q=spec&limit=11&offset=0&type=Spec",
      undefined,
    );
    expect(fetchMock).toHaveBeenNthCalledWith(9, "/api/v1/ontology/summary", undefined);
    expect(fetchMock).toHaveBeenNthCalledWith(10, "/api/v1/ontology/types/Spec", undefined);
    expect(fetchMock).toHaveBeenNthCalledWith(11, "/api/v1/ontology/atlas", undefined);
    expect(fetchMock).toHaveBeenNthCalledWith(
      12,
      "/api/v1/ontology/inspect?path=docs%2Fspec.md",
      undefined,
    );
    expect(fetchMock).toHaveBeenNthCalledWith(13, "/api/v1/ontology/query-schema", undefined);
    expect(fetchMock).toHaveBeenNthCalledWith(
      14,
      "/api/v1/graphs/global?notesOnly=true",
      undefined,
    );
    expect(fetchMock).toHaveBeenNthCalledWith(
      15,
      "/api/v1/graphs/local?limit=25&path=docs%2Fspec.md&diagnostics=true",
      undefined,
    );
    expect(fetchMock).toHaveBeenNthCalledWith(
      16,
      "/api/v1/graphs/expand?module=src%2Fapp&limit=33",
      undefined,
    );
  });

  it("routes configured view helpers through public v1 endpoints", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      headers: new Headers({ "content-type": "application/json" }),
      json: vi.fn().mockResolvedValue({ views: [] }),
    });

    vi.stubGlobal("fetch", fetchMock);

    await listPublicViews();
    await getPublicView("view/with space");
    await executePublicView("view/with space", {
      search: "ready",
      page: { first: 25 },
    });

    expect(fetchMock).toHaveBeenNthCalledWith(1, "/api/v1/views", undefined);
    expect(fetchMock).toHaveBeenNthCalledWith(2, "/api/v1/views/view%2Fwith%20space", undefined);
    expect(fetchMock).toHaveBeenNthCalledWith(
      3,
      "/api/v1/views/view%2Fwith%20space/execute",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({
          search: "ready",
          page: { first: 25 },
        }),
      }),
    );
  });

  it("posts a view save with its state and definition fingerprint", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      headers: new Headers({ "content-type": "application/json" }),
      json: vi.fn().mockResolvedValue({ id: "ip ideas", path: "views/ip.yaml", created: false }),
    });

    vi.stubGlobal("fetch", fetchMock);

    const body = {
      definitionFingerprint: "fp-1",
      state: {
        variant: "kanban",
        filters: [{ field: "owner", op: "eq", value: "people/drew.md" }],
        sort: [{ field: "title", direction: "desc" }],
        laneField: "priority",
      },
    };

    await expect(savePublicView("ip ideas", body)).resolves.toEqual({
      id: "ip ideas",
      path: "views/ip.yaml",
      created: false,
    });
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/views/ip%20ideas/save",
      expect.objectContaining({ method: "POST", body: JSON.stringify(body) }),
    );
  });

  it("rejects a view save against a changed file with a 409 ApiError, once", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: false,
      status: 409,
      headers: new Headers({ "content-type": "application/json" }),
      json: vi.fn().mockResolvedValue({ error: "view file changed", code: "conflict" }),
    });

    vi.stubGlobal("fetch", fetchMock);

    await expect(
      savePublicView("ip-ideas", { definitionFingerprint: "stale", state: { variant: "table" } }),
    ).rejects.toMatchObject({
      name: "ApiError",
      status: 409,
      message: "view file changed",
    } satisfies Partial<ApiError>);
    // A write is not replayed.
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it.each([
    [
      "global graph",
      (signal: AbortSignal) => getGlobalGraph({}, { signal }),
      "/api/v1/graphs/global",
      undefined,
    ],
    ["views", (signal: AbortSignal) => listPublicViews({ signal }), "/api/v1/views", undefined],
    [
      "validation",
      (signal: AbortSignal) => getPublicValidate({ signal }),
      "/api/v2/validate",
      undefined,
    ],
    [
      "view execution",
      (signal: AbortSignal) => executePublicView("planning.backlog", {}, { signal }),
      "/api/v1/views/planning.backlog/execute",
      "POST",
    ],
  ])("forwards cancellation through %s reads", async (_name, read, url, method) => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ views: [] }), {
        headers: { "content-type": "application/json" },
      }),
    );

    vi.stubGlobal("fetch", fetchMock);
    const controller = new AbortController();

    await read(controller.signal);

    const expectedInit: RequestInit = { signal: controller.signal };

    if (method) expectedInit.method = method;

    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(fetchMock).toHaveBeenCalledWith(url, expect.objectContaining(expectedInit));
  });

  it("routes full node workspace reads through public GraphQL", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      headers: new Headers({ "content-type": "application/json" }),
      json: vi.fn().mockResolvedValue(publicWorkspaceResponse("docs/spec.md")),
    });

    vi.stubGlobal("fetch", fetchMock);

    const workspace = await getNodeWorkspace("docs/spec.md");

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/graphql",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({
          query: PUBLIC_NODE_DETAIL_QUERY,
          variables: { ref: "docs/spec.md" },
          operationName: "PublicNodeDetail",
        }),
      }),
    );
    expect(workspace.node.title).toBe("Spec");
    expect(workspace.content.rendered?.rendered).toContain("# Spec");
  });

  it("normalizes structured workspace refs for public GraphQL", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      headers: new Headers({ "content-type": "application/json" }),
      json: vi
        .fn()
        .mockResolvedValueOnce(publicWorkspaceResponse("docs/spec.md"))
        .mockResolvedValueOnce(publicWorkspaceResponse("docs/spec.md#^story-a")),
    });

    vi.stubGlobal("fetch", fetchMock);

    await getNodeWorkspace("docs/spec.md");
    await getNodeWorkspace({
      notePath: "docs/spec.md",
      fragment: "^story-a",
      nodeId: "story-a",
      kind: "EMBEDDED",
      structuralFingerprint: "abc123",
    });

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/graphql",
      expect.objectContaining({
        method: "POST",
        body: expect.stringContaining('"ref":"docs/spec.md"'),
      }),
    );
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/graphql",
      expect.objectContaining({
        method: "POST",
        body: expect.stringContaining('"ref":"docs/spec.md#^story-a"'),
      }),
    );
  });

  it("uses structural identity for an unanchored embedded workspace ref", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      headers: new Headers({ "content-type": "application/json" }),
      json: vi.fn().mockResolvedValue(publicWorkspaceResponse("docs/spec.md#struct:abc123")),
    });

    vi.stubGlobal("fetch", fetchMock);

    await getNodeWorkspace({
      notePath: "docs/spec.md",
      fragment: "item-11146",
      nodeId: "docs/spec.md#item-11146",
      kind: "EMBEDDED",
      structuralFingerprint: "abc123",
    });

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/graphql",
      expect.objectContaining({
        body: expect.stringContaining('"ref":"docs/spec.md#struct:abc123"'),
      }),
    );
  });

  it("routes node workspace graph reads through public GraphQL", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      headers: new Headers({ "content-type": "application/json" }),
      json: vi.fn().mockResolvedValue({
        data: {
          node: {
            ref: {
              ref: "docs/spec.md",
              kind: "NOTE",
              notePath: "docs/spec.md",
              path: "docs/spec.md",
            },
            nodeId: "docs/spec.md",
            nodeKind: "NOTE",
            path: "docs/spec.md",
            title: "Spec",
            resolvedType: "ProductSpec",
            content: "# Spec",
            locator: {
              sourceLocator: "docs/spec.md",
              status: "ok",
              wikilink: "[[docs/spec|Spec]]",
              exists: true,
              requiresFix: false,
              linkTarget: null,
            },
            localGraph: {
              nodes: [
                {
                  id: "docs/spec.md",
                  ref: {
                    ref: "docs/spec.md",
                    kind: "NOTE",
                    notePath: "docs/spec.md",
                  },
                  nodeKind: "NOTE",
                  title: "Spec",
                  typeName: "ProductSpec",
                  path: "docs/spec.md",
                  notePath: "docs/spec.md",
                  sourceLocator: "docs/spec.md",
                },
              ],
              edges: [],
              truncated: false,
            },
          },
        },
      }),
    });

    vi.stubGlobal("fetch", fetchMock);

    const graph = await getNodeWorkspaceGraph({
      notePath: "docs/spec.md",
      kind: "EMBEDDED",
      nodeId: "story-a",
      structuralFingerprint: "abc123",
    });

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/graphql",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({
          query: PUBLIC_LOCAL_GRAPH_QUERY,
          variables: {
            ref: "docs/spec.md#struct:abc123",
            nodeLimit: 500,
            edgeLimit: 1000,
          },
          operationName: "PublicLocalGraph",
        }),
      }),
    );
    expect(graph.nodes?.map((node) => node.id)).toEqual(["docs/spec.md"]);
    expect(graph.views?.localGraph?.nodeIds).toEqual(["docs/spec.md"]);
  });
});
