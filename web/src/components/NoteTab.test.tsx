import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { isString } from "../api/parse";
import type {
  PublicGraphQLResult,
  PublicNode,
  PublicNodeDetailData,
} from "../api/publicGraphQLTypes";
import {
  deferredReply,
  type FakeFetchRequest,
  graphQLRequestBody,
  jsonReply,
  withFakeFetch,
} from "../test/fakeFetch";
import { withFakeEventSource } from "../test/fakeEventSource";
import { NoteTab } from "./NoteTab";
import { parseNotesLocation } from "./notesRoute";
import { useNoteTabs } from "./useNoteTabs";
import { viewSegment } from "../test/workspaceView";

type NodeReply = PublicGraphQLResult<PublicNodeDetailData>;

function requestedRef(request: FakeFetchRequest): string {
  const ref = graphQLRequestBody(request).variables?.ref;

  return isString(ref) ? ref : "";
}

function publicNode(ref: string): PublicNode {
  const [notePath, fragment] = ref.split("#", 2);
  const kind = fragment ? "SECTION" : "NOTE";

  return {
    ref: {
      ref,
      kind,
      notePath,
      path: notePath,
      fragment: fragment || null,
      nodeId: fragment ? ref : `note:${notePath}`,
    },
    nodeId: fragment ? ref : `note:${notePath}`,
    nodeKind: kind,
    path: notePath,
    title: fragment || "Alpha",
    resolvedType: fragment ? "_FallbackSection" : "_FallbackNote",
    content: fragment
      ? `Body for ${fragment}\n\n[File root](rhizome://note/notes%2Fa.md)`
      : "# Alpha\n\n[Section B](rhizome://note/notes%2Fa.md%23section-b)",
    locator: { sourceLocator: ref, status: "linkable", exists: true, requiresFix: false },
  };
}

function reply(ref: string): NodeReply {
  return { data: { node: publicNode(ref) } };
}

function htmlReply(ref: string): NodeReply {
  const result = reply(ref);
  const node = result.data?.node;

  if (!node) throw new Error("HTML fixture node is missing");
  node.format = "html";
  node.content = "<html><body>Prototype</body></html>";
  node.sourceCapabilities = ["active_content_viewing"];

  return result;
}

function Harness() {
  const tabs = useNoteTabs("test-vault");

  const location = parseNotesLocation(
    window.location.pathname,
    window.location.search,
    window.location.hash,
  );

  return (
    <>
      <button onClick={() => tabs.activate("home")}>Home</button>
      <button onClick={() => tabs.activate("note:notes/a.md")}>Activate Alpha</button>
      <button onClick={() => tabs.open("notes/a.md")}>Open Alpha root</button>
      {tabs.tabs.map(
        (tab) =>
          tab.kind === "note" && (
            <section key={tab.id} hidden={tabs.activeId !== tab.id} aria-label={tab.path}>
              <NoteTab
                tab={tab}
                active={tabs.activeId === tab.id}
                anchor={tabs.activeId === tab.id ? location.fragment : null}
                editSession={null}
                vaultKey="/test/vault"
                editing={false}
                onOpen={(target, mode) => tabs.open(target, { mode })}
                onTitle={tabs.setTitle}
                onFocusedTarget={tabs.setFocusedTarget}
                registerContext={vi.fn()}
                onStageOps={async () => {}}
              />
            </section>
          ),
      )}
    </>
  );
}

function navigate(target: string) {
  act(() => {
    window.history.pushState({}, "", target);
    window.dispatchEvent(new PopStateEvent("popstate"));
  });
}

describe("NoteTab navigation", () => {
  const http = withFakeFetch();
  withFakeEventSource();
  beforeEach(() => {
    window.sessionStorage.clear();
    window.localStorage.clear();
    window.history.replaceState({}, "", "/notes?note=notes%2Fa.md");
  });
  const reads = () => http.requests("POST", "/api/v1/graphql").map(requestedRef);

  function serve() {
    http.onGraphQL("PublicNodeDetail", (request) => jsonReply(reply(requestedRef(request))));
  }

  it("defers note reads until the retained tab becomes active", async () => {
    serve();

    const props = {
      tab: {
        id: "note:notes/a.md",
        kind: "note" as const,
        path: "notes/a.md",
        dirty: false,
      },
      anchor: null,
      editSession: null,
      editing: false,
      onOpen: vi.fn(),
      onTitle: vi.fn(),
      registerContext: vi.fn(),
      onStageOps: async () => {},
    };

    const { rerender } = render(<NoteTab {...props} active={false} />);
    expect(reads()).toEqual([]);

    rerender(<NoteTab {...props} active />);
    await waitFor(() => expect(reads()).toEqual(["notes/a.md"]));
  });

  it("preserves the document query in the HTML browser tab route", async () => {
    http.onGraphQL("PublicNodeDetail", () => jsonReply(htmlReply("notes/a.md")));
    http.on("POST", "/api/v1/html-viewers", () =>
      jsonReply(
        {
          id: "viewer-1",
          url: "https://viewer-1.content.example.test/notes/a.md?mode=wide",
          nonce: "nonce-1",
          expiresAt: "2030-01-01T00:00:00Z",
        },
        201,
      ),
    );
    http.on("DELETE", "/api/v1/html-viewers/viewer-1", () => new Response(null, { status: 204 }));
    window.history.replaceState({}, "", "/notes?note=notes%2Fa.md&noteQuery=mode%3Dwide");
    render(<Harness />);

    expect(await screen.findByRole("link", { name: "Open in browser tab" })).toHaveAttribute(
      "href",
      "/notes?note=notes%2Fa.md&noteQuery=mode%3Dwide&bare=1",
    );
  });

  it("preserves a drill and its URL on ordinary tab activation without refetching", async () => {
    serve();
    render(<Harness />);
    fireEvent.click(await screen.findByRole("link", { name: "Section B" }));
    await screen.findByText("Body for section-b");
    expect(screen.getByRole("navigation", { name: "Node path" })).toHaveTextContent(
      "Alpha›section-b",
    );
    expect(window.location.hash).toBe("#section-b");
    fireEvent.click(screen.getByRole("button", { name: "Home" }));
    fireEvent.click(screen.getByRole("button", { name: "Activate Alpha" }));
    expect(screen.getByText("Body for section-b")).toBeVisible();
    expect(window.location.hash).toBe("#section-b");
    expect(reads()).toEqual(["notes/a.md", "notes/a.md#section-b"]);
  });
  it("loads a deep-linked section above its root and returns through the cached breadcrumb", async () => {
    serve();
    window.history.replaceState({}, "", "/notes?note=notes%2Fa.md#section-b");
    render(<Harness />);
    await screen.findByText("Body for section-b");
    fireEvent.click(
      within(screen.getByRole("navigation", { name: "Node path" })).getByRole("button", {
        name: "Alpha",
      }),
    );
    expect(await screen.findByRole("link", { name: "Section B" })).toBeVisible();
    expect(window.location.hash).toBe("");
    expect(reads()).toEqual(["notes/a.md", "notes/a.md#section-b"]);
  });
  it("returns to the file root from a same-file link and an explicit root open", async () => {
    serve();
    render(<Harness />);
    fireEvent.click(await screen.findByRole("link", { name: "Section B" }));
    fireEvent.click(await screen.findByRole("link", { name: "File root" }));
    fireEvent.click(await screen.findByRole("link", { name: "Section B" }));
    await screen.findByText("Body for section-b");
    fireEvent.click(screen.getByRole("button", { name: "Open Alpha root" }));
    expect(await screen.findByRole("link", { name: "Section B" })).toBeVisible();
    expect(window.location.hash).toBe("");
  });
  it("resolves an issue node locator without replacing the private issue fragment", async () => {
    serve();
    window.history.replaceState(
      {},
      "",
      "/notes/issues?note=notes%2Fa.md&issueScopeKind=global&issue=issue-1#issue:node=REQ-1&field=status",
    );
    render(<Harness />);

    await screen.findByText("Body for node:REQ-1");
    expect(window.location.hash).toBe("#issue:node=REQ-1&field=status");
    expect(new URLSearchParams(window.location.search).get("issue")).toBe("issue-1");
    expect(reads()).toEqual(["notes/a.md", "notes/a.md#node:REQ-1"]);
  });

  it("returns to the note with a clear fallback when an issue node disappears", async () => {
    http.onGraphQL("PublicNodeDetail", (request) => {
      const ref = requestedRef(request);

      return ref.includes("#")
        ? jsonReply({ error: "Node not found" }, 404)
        : jsonReply(reply(ref));
    });
    window.history.replaceState({}, "", "/notes?note=notes%2Fa.md#issue:node=removed");
    render(<Harness />);

    expect(
      await screen.findByText(
        "The exact issue location is no longer available. Showing the nearest note context.",
      ),
    ).toBeVisible();
    expect(await screen.findByRole("link", { name: "Section B" })).toBeVisible();
    expect(window.location.hash).toBe("#issue:node=removed");
  });
  it("retains a beside section target for an already-open note without moving the current body", async () => {
    serve();
    render(<Harness />);
    const sectionLink = await screen.findByRole("link", { name: "Section B" });
    fireEvent.click(sectionLink, { ctrlKey: true });
    expect(window.location.hash).toBe("");
    expect(screen.getByRole("link", { name: "Section B" })).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Home" }));
    fireEvent.click(screen.getByRole("button", { name: "Activate Alpha" }));
    expect(await screen.findByText("Body for section-b")).toBeVisible();
    expect(window.location.hash).toBe("#section-b");
    expect(reads()).toEqual(["notes/a.md", "notes/a.md#section-b"]);
  });
  it("waits for a delayed root without consuming or publishing an obsolete anchor", async () => {
    const root = deferredReply<NodeReply>();
    http.on("POST", "/api/v1/graphql", () => root.promise, {
      when: (request) => requestedRef(request) === "notes/a.md",
    });
    serve();
    render(<Harness />);
    await waitFor(() => expect(reads()).toHaveLength(1));
    navigate("/notes?note=notes%2Fa.md#section-a");
    root.resolve(reply("notes/a.md"));
    expect(await screen.findByText("Body for section-a")).toBeVisible();
    expect(window.location.hash).toBe("#section-a");
    expect(reads()).toEqual(["notes/a.md", "notes/a.md#section-a"]);
  });
  it("publishes a stable structural ref without another read and follows browser root navigation", async () => {
    http.onGraphQL("PublicNodeDetail", (request) => {
      const ref = requestedRef(request);
      const node = publicNode(ref);

      if (ref.includes("#")) node.ref.structural = "stable-section";

      return jsonReply({ data: { node } });
    });
    window.history.replaceState({}, "", "/notes?note=notes%2Fa.md#section-b");
    render(<Harness />);
    await screen.findByText("Body for section-b");
    await waitFor(() => expect(window.location.hash).toBe("#struct%3Astable-section"));
    expect(reads()).toHaveLength(2);
    navigate("/notes?note=notes%2Fa.md");
    expect(await screen.findByRole("link", { name: "Section B" })).toBeVisible();
    expect(reads()).toHaveLength(2);
  });
  it("ignores a late section response after explicit navigation back to the root", async () => {
    const section = deferredReply<NodeReply>();
    http.on("POST", "/api/v1/graphql", () => section.promise, {
      when: (request) => requestedRef(request) === "notes/a.md#section-b",
    });
    serve();
    render(<Harness />);
    fireEvent.click(await screen.findByRole("link", { name: "Section B" }));
    await waitFor(() => expect(reads()).toHaveLength(2));
    fireEvent.click(screen.getByRole("button", { name: "Open Alpha root" }));
    await act(async () => section.resolve(reply("notes/a.md#section-b")));
    expect(screen.getByRole("link", { name: "Section B" })).toBeVisible();
    expect(window.location.hash).toBe("");
    expect(screen.queryByRole("navigation", { name: "Node path" })).toBeNull();
  });

  it("keeps the cached file breadcrumb available when a section cannot be resolved", async () => {
    http.onGraphQL("PublicNodeDetail", (request) => {
      const ref = requestedRef(request);

      return ref.includes("#")
        ? jsonReply({ error: "Section not found" }, 404)
        : jsonReply(reply(ref));
    });
    window.history.replaceState({}, "", "/notes?note=notes%2Fa.md#missing-section");
    render(<Harness />);
    await screen.findByRole("alert");
    fireEvent.click(
      within(screen.getByRole("navigation", { name: "Node path" })).getByRole("button", {
        name: "Alpha",
      }),
    );
    expect(await screen.findByRole("link", { name: "Section B" })).toBeVisible();
    expect(window.location.hash).toBe("");
    expect(reads()).toEqual(["notes/a.md", "notes/a.md#missing-section"]);
  });

  it("retries an initial file failure in place", async () => {
    let attempts = 0;
    http.onGraphQL("PublicNodeDetail", (request) => {
      attempts += 1;

      return attempts === 1
        ? jsonReply({ error: "Temporarily unavailable" }, 500)
        : jsonReply(reply(requestedRef(request)));
    });
    render(<Harness />);
    fireEvent.click(await screen.findByRole("button", { name: "Retry" }));
    await waitFor(() => expect(screen.getByRole("link", { name: "Section B" })).toBeVisible());
    expect(reads()).toEqual(["notes/a.md", "notes/a.md"]);
  });

  it("preserves literal percent escapes through an internal link, browser URL, and API read", async () => {
    http.onGraphQL("PublicNodeDetail", (request) => {
      const ref = requestedRef(request);
      const node = publicNode(ref);

      if (!ref.includes("#"))
        node.content = "[Literal heading](rhizome://note/notes%2Fa.md%23literal%2520heading)";

      return jsonReply({ data: { node } });
    });
    render(<Harness />);
    fireEvent.click(await screen.findByRole("link", { name: "Literal heading" }));
    expect(await screen.findByText("Body for literal%20heading")).toBeVisible();
    expect(window.location.hash).toBe("#literal%2520heading");
    expect(reads()).toEqual(["notes/a.md", "notes/a.md#literal%20heading"]);
  });

  it("remembers Markdown mode for the note within the browser session", async () => {
    serve();
    const first = render(<Harness />);
    fireEvent.click(await waitFor(() => viewSegment("Markdown")));
    await waitFor(() => expect(viewSegment("Markdown")).toHaveAttribute("aria-pressed", "true"));
    first.unmount();
    render(<Harness />);
    await waitFor(() => expect(viewSegment("Markdown")).toHaveAttribute("aria-pressed", "true"));
  });
});
