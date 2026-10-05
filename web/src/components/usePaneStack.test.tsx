import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { VaultInvalidationBridge } from "../query/VaultInvalidationBridge";
import { StrictMode, startTransition, useState, type ReactNode } from "react";
import { flushSync } from "react-dom";
import { describe, expect, it } from "vitest";
import { nodeWorkspaceFromPublicGraphQL } from "../api/nodeWorkspaceAdapter";
import { isJsonObject, isString } from "../api/parse";
import type {
  PublicGraphQLResult,
  PublicNode,
  PublicNodeDetailData,
} from "../api/publicGraphQLTypes";
import type { NodeEvent, NodeRef, NodeWorkspace, OntologyEditSessionResponse } from "../api/types";
import {
  decodeJSONBody,
  type FakeFetchRequest,
  deferredReply,
  graphQLRequestBody,
  jsonReply,
  withFakeFetch,
} from "../test/fakeFetch";
import { withFakeEventSource } from "../test/fakeEventSource";
import { targetRefForNode, usePaneStack } from "./usePaneStack";

type NodeDetailReply = PublicGraphQLResult<PublicNodeDetailData>;

/**
 * Build the public GraphQL node the server would answer with for `ref`,
 * which is the workspace locator the client sends: `path`, `path#fragment`,
 * or `path#node:<nodeId>`.
 */
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

function nodeDetailReply(ref: string): NodeDetailReply {
  return { data: { node: publicNode(ref) } };
}

function workspaceFor(ref: string): NodeWorkspace {
  return nodeWorkspaceFromPublicGraphQL({ node: publicNode(ref) }, ref);
}

function requestedRef(request: FakeFetchRequest): string {
  const ref = graphQLRequestBody(request).variables?.ref;

  return isString(ref) ? ref : "";
}

type EditSessionRequestBody = { editSession?: { sessionId: string } };

function isEditSessionRequestBody(value: unknown): value is EditSessionRequestBody {
  if (!isJsonObject(value)) return false;
  const session = value.editSession;

  return session === undefined || (isJsonObject(session) && isString(session.sessionId));
}

function editSessionOf(request: FakeFetchRequest): string | undefined {
  return decodeJSONBody(request, isEditSessionRequestBody).editSession?.sessionId;
}

function noSession(): OntologyEditSessionResponse | null {
  return null;
}

function noWorkspaces(): NodeWorkspace[] {
  return [];
}

function editSession(sessionId: string): OntologyEditSessionResponse {
  return {
    sessionId,
    status: "dirty",
    revision: 1,
    hasUncommittedChanges: true,
    createdAt: "2026-09-05T00:00:00Z",
    updatedAt: "2026-09-05T00:00:01Z",
  };
}

function nodeChangedEvent(ref: NodeRef): NodeEvent {
  return { id: `event-${ref.notePath}`, kind: "node.changed", ref };
}

describe("usePaneStack", () => {
  const http = withFakeFetch();
  const sources = withFakeEventSource();

  it.each(["before failure", "after failure"])(
    "recovers a failed pane when indexing finishes %s",
    async (timing) => {
      const first = deferredReply<NodeDetailReply>();
      let requests = 0;
      http.onGraphQL("PublicNodeDetail", (request) => {
        requests += 1;

        return requests === 1 ? first.promise : jsonReply(nodeDetailReply(requestedRef(request)));
      });
      const client = new QueryClient();

      const wrapper = ({ children }: { children: ReactNode }) => (
        <QueryClientProvider client={client}>
          <VaultInvalidationBridge />
          {children}
        </QueryClientProvider>
      );

      const { result } = renderHook(() => usePaneStack(), { wrapper });
      act(() => {
        void result.current.openRootPane("notes/new.md#^story");
      });
      await waitFor(() => expect(requests).toBe(1));
      const events = sources.instances.find((source) => source.url.includes("/api/v1/events"));
      expect(events).toBeDefined();

      if (timing === "before failure") {
        act(() => events?.emitJSON({}, "index.changed"));
      }

      await act(async () => first.resolve({ data: { node: null } }));

      if (timing === "after failure") {
        await waitFor(() => expect(result.current.paneStack[0]?.error).toBeTruthy());
        act(() => events?.emitJSON({}, "index.changed"));
      }

      await waitFor(() => expect(result.current.paneStack[0]?.workspace).toBeTruthy());
      expect(requests).toBe(2);
      expect(result.current.paneStack[0]?.error).toBeNull();
      expect(result.current.paneStack[0]?.anchor).toBe("^story");
    },
  );

  /** Every workspace read answers from the requested ref, like the real server. */
  function serveWorkspaces() {
    http.onGraphQL("PublicNodeDetail", (request) =>
      jsonReply(nodeDetailReply(requestedRef(request))),
    );
  }

  /** Hold the workspace read for `ref` open so the test can order completions. */
  function deferWorkspace(ref: string) {
    const reply = deferredReply<NodeDetailReply>();
    http.on("POST", "/api/v1/graphql", () => reply.promise, {
      when: (request) => requestedRef(request) === ref,
    });

    return reply;
  }

  function workspaceReads(): FakeFetchRequest[] {
    return http.requests("POST", "/api/v1/graphql");
  }

  function readCount(): number {
    return workspaceReads().length;
  }

  it("preserves structural fingerprints when opening structural nodes", () => {
    expect(
      targetRefForNode({
        nodeId: "docs/spec.md#item-11146",
        fragment: "item-11146",
        title: "Criterion",
        locator: "EMBEDDED",
        notePath: "docs/spec.md",
        structuralFingerprint: "criterion-fingerprint",
      }),
    ).toEqual({
      notePath: "docs/spec.md",
      fragment: "item-11146",
      nodeId: "docs/spec.md#item-11146",
      structuralFingerprint: "criterion-fingerprint",
      kind: "EMBEDDED",
    });
  });

  it("focusOrPushNodeFromIndex pushes a new pane for a same-note section drill", async () => {
    serveWorkspaces();
    const { result } = renderHook(() => usePaneStack());

    await act(async () => {
      await result.current.openRootPane("notes/foo.md");
    });
    await waitFor(() => {
      expect(result.current.paneStack[0]?.path).toBe("notes/foo.md");
    });
    expect(result.current.paneStack).toHaveLength(1);

    // Path-only dedupe would match the source pane and refuse to push;
    // node-level dedupe must include the fragment.
    let outcome: { index: number; pushed: boolean } | undefined;
    act(() => {
      outcome = result.current.focusOrPushNodeFromIndex(0, {
        notePath: "notes/foo.md",
        fragment: "section-a",
        kind: "SECTION",
      });
    });
    expect(outcome).toEqual({ index: 1, pushed: true });
    await waitFor(() => {
      expect(result.current.paneStack).toHaveLength(2);
      expect(result.current.paneStack[1]?.anchor).toBe("section-a");
    });
  });

  it("focusOrPushNodeFromIndex pushes a new pane for a same-note block-link string", async () => {
    serveWorkspaces();
    const { result } = renderHook(() => usePaneStack());

    await act(async () => {
      await result.current.openRootPane("notes/foo.md");
    });
    await waitFor(() => {
      expect(result.current.paneStack[0]?.path).toBe("notes/foo.md");
    });

    let outcome: { index: number; pushed: boolean } | undefined;
    act(() => {
      outcome = result.current.focusOrPushNodeFromIndex(0, "notes/foo.md#^story-a");
    });

    expect(outcome).toEqual({ index: 1, pushed: true });
    await waitFor(() => {
      expect(result.current.paneStack).toHaveLength(2);
      expect(result.current.paneStack[1]?.anchor).toBe("^story-a");
    });
  });

  it("focusOrPushNodeFromIndex focuses the existing pane on repeat clicks", async () => {
    serveWorkspaces();
    const { result } = renderHook(() => usePaneStack());

    await act(async () => {
      await result.current.openRootPane("notes/foo.md");
    });
    await waitFor(() => {
      expect(result.current.paneStack[0]?.path).toBe("notes/foo.md");
    });

    await act(async () => {
      result.current.focusOrPushNodeFromIndex(0, {
        notePath: "notes/foo.md",
        fragment: "section-a",
        kind: "SECTION",
      });
    });
    await waitFor(() => {
      expect(result.current.paneStack[1]?.anchor).toBe("section-a");
    });

    let outcome: { index: number; pushed: boolean } | undefined;
    act(() => {
      outcome = result.current.focusOrPushNodeFromIndex(0, {
        notePath: "notes/foo.md",
        fragment: "section-a",
        kind: "SECTION",
      });
    });
    expect(outcome).toEqual({ index: 1, pushed: false });
    expect(result.current.paneStack).toHaveLength(2);
  });

  it("focusOrPushNodeFromIndex truncates stale right-side node panes before branching", async () => {
    serveWorkspaces();
    const { result } = renderHook(() => usePaneStack());

    await act(async () => {
      await result.current.openRootPane("notes/source.md");
    });
    act(() => {
      result.current.focusOrPushNodeFromIndex(0, "notes/old-child.md#old");
    });
    act(() => {
      result.current.focusOrPushNodeFromIndex(1, "notes/source.md#target");
    });
    await waitFor(() => {
      expect(result.current.paneStack).toHaveLength(3);
      expect(result.current.paneStack[2]?.anchor).toBe("target");
    });

    let outcome: { index: number; pushed: boolean } | undefined;
    act(() => {
      outcome = result.current.focusOrPushNodeFromIndex(0, {
        notePath: "notes/source.md",
        fragment: "target",
        kind: "SECTION",
      });
    });

    expect(outcome).toEqual({ index: 1, pushed: true });
    await waitFor(() => {
      expect(result.current.paneStack).toHaveLength(2);
      expect(result.current.paneStack[1]?.anchor).toBe("target");
    });
    const lastRead = workspaceReads().at(-1);
    expect(lastRead && requestedRef(lastRead)).toBe("notes/source.md#target");
    expect(lastRead?.signal).toBeInstanceOf(AbortSignal);
  });

  it("keeps one node subscription while the canonical ref set is unchanged", async () => {
    serveWorkspaces();
    const { result } = renderHook(() => usePaneStack());

    await act(async () => {
      await result.current.openRootPane("notes/foo.md");
    });
    await waitFor(() => expect(sources.instances).toHaveLength(1));

    const refresh = deferWorkspace("notes/foo.md");
    act(() => {
      sources.latest().emitJSON(nodeChangedEvent({ notePath: "notes/foo.md", kind: "NOTE" }));
    });

    expect(sources.instances).toHaveLength(1);
    expect(sources.instances[0]?.closed).toBe(false);

    await act(async () => {
      refresh.resolve(nodeDetailReply("notes/foo.md"));
      await refresh.promise;
    });
    expect(sources.instances).toHaveLength(1);
    expect(sources.instances[0]?.closed).toBe(false);
  });

  it("shows a deleted note as gone and stops following it instead of re-reading", async () => {
    serveWorkspaces();
    const { result } = renderHook(() => usePaneStack());

    await act(async () => {
      await result.current.openRootPane("notes/foo.md");
    });
    await waitFor(() => expect(sources.instances).toHaveLength(1));

    act(() => {
      sources.latest().emitJSON({
        id: "deleted",
        kind: "node.deleted",
        ref: { notePath: "notes/foo.md", kind: "NOTE", structuralFingerprint: "fp-old" },
      });
    });

    expect(result.current.paneStack[0]?.error).toBe("notes/foo.md was deleted or moved on disk.");
    expect(result.current.paneStack[0]?.workspace).toBeNull();
    expect(readCount()).toBe(1);
    expect(sources.latest().closed).toBe(true);
  });

  it("uses the current edit session when an existing subscription refreshes", async () => {
    serveWorkspaces();
    const firstSession = editSession("session-1");
    const currentSession = editSession("session-2");

    const { result, rerender } = renderHook(({ session }) => usePaneStack(session), {
      initialProps: { session: firstSession },
    });

    await act(async () => {
      await result.current.openRootPane("notes/foo.md");
    });
    await waitFor(() => expect(sources.instances).toHaveLength(1));

    rerender({ session: currentSession });
    act(() => {
      sources.latest().emitJSON(nodeChangedEvent({ notePath: "notes/foo.md", kind: "NOTE" }));
    });
    await waitFor(() => expect(readCount()).toBe(2));

    const refreshRead = workspaceReads()[1];
    expect(refreshRead && editSessionOf(refreshRead)).toBe("session-2");
    expect(sources.instances).toHaveLength(1);
  });

  it("reloads a pane from the canonical ref when a subscribed ref is renumbered", async () => {
    serveWorkspaces();
    const { result } = renderHook(() => usePaneStack());

    await act(async () => {
      await result.current.openRootPane("notes/foo.md#item-10");
    });
    await waitFor(() => expect(sources.instances).toHaveLength(1));

    act(() => {
      sources.latest().emitJSON({
        ...nodeChangedEvent({
          notePath: "notes/foo.md",
          fragment: "item-10",
          kind: "SECTION",
        }),
        canonicalRef: {
          notePath: "notes/foo.md",
          fragment: "item-42",
          kind: "SECTION",
        },
      });
    });

    await waitFor(() => expect(readCount()).toBe(2));
    const canonicalRead = workspaceReads()[1];
    expect(canonicalRead).toBeDefined();

    if (!canonicalRead) throw new Error("expected canonical workspace read");
    expect(requestedRef(canonicalRead)).toBe("notes/foo.md#item-42");
  });

  it("replaces the node subscription when canonical identity changes", async () => {
    serveWorkspaces();
    const { result } = renderHook(() => usePaneStack());

    await act(async () => {
      await result.current.openRootPane("notes/foo.md");
    });
    await waitFor(() => expect(sources.instances).toHaveLength(1));

    await act(async () => {
      await result.current.loadPane("pane-1", "notes/bar.md");
    });
    await waitFor(() => expect(sources.instances).toHaveLength(2));

    expect(sources.instances[0]?.closed).toBe(true);
    expect(sources.instances[1]?.closed).toBe(false);
  });

  it("aborts a superseded request and ignores its stale result", async () => {
    serveWorkspaces();
    const first = deferWorkspace("notes/first.md");
    const second = deferWorkspace("notes/second.md");
    const { result } = renderHook(() => usePaneStack());

    act(() => {
      void result.current.openRootPane("notes/first.md");
    });
    await waitFor(() => expect(readCount()).toBe(1));
    const firstSignal = workspaceReads()[0]?.signal;

    act(() => {
      void result.current.loadPane("pane-1", "notes/second.md");
    });
    expect(firstSignal?.aborted).toBe(true);

    await act(async () => {
      second.resolve(nodeDetailReply("notes/second.md"));
      await second.promise;
    });
    await act(async () => {
      first.resolve(nodeDetailReply("notes/first.md"));
      await first.promise;
    });

    await waitFor(() => expect(result.current.paneStack[0]?.path).toBe("notes/second.md"));
  });

  it("applies the workspaces a save returns instead of re-reading a lagging index", async () => {
    serveWorkspaces();

    const { result, rerender } = renderHook(({ session, saved }) => usePaneStack(session, saved), {
      initialProps: { session: noSession(), saved: noWorkspaces() },
    });

    await act(async () => {
      await result.current.openRootPane("notes/a.md");
    });

    const dirty = { ...editSession("session-1"), touchedPaths: ["notes/a.md"], workspaces: [] };
    act(() => rerender({ session: dirty, saved: noWorkspaces() }));
    await waitFor(() => expect(readCount()).toBe(2));

    const saved = workspaceFor("notes/a.md");
    saved.content = { ...saved.content, title: "Saved title" };
    act(() => rerender({ session: noSession(), saved: [saved] }));

    await waitFor(() =>
      expect(result.current.paneStack[0]?.workspace?.content.title).toBe("Saved title"),
    );
    expect(readCount()).toBe(2);
  });

  it("shows an error when a session refresh replaces the first load and fails", async () => {
    const first = deferredReply<NodeDetailReply>();
    let requests = 0;
    http.onGraphQL("PublicNodeDetail", () => {
      requests += 1;

      return requests === 1 ? first.promise : jsonReply({ error: "session lost" }, 400);
    });

    const { result, rerender } = renderHook(({ session }) => usePaneStack(session), {
      initialProps: { session: noSession() },
    });

    act(() => {
      void result.current.openRootPane("notes/a.md");
    });
    await waitFor(() => expect(requests).toBe(1));

    act(() => {
      rerender({ session: { ...editSession("session-1"), touchedPaths: ["notes/a.md"] } });
    });

    await waitFor(() => expect(result.current.paneStack[0]?.error).toBeTruthy());
    expect(result.current.paneStack[0]?.loading).toBe(false);
  });

  it("does not let a stale edit-session workspace cancel active navigation", async () => {
    serveWorkspaces();
    const navigation = deferWorkspace("notes/b.md");

    const { result, rerender } = renderHook(({ session }) => usePaneStack(session), {
      initialProps: { session: noSession() },
    });

    await act(async () => {
      await result.current.openRootPane("notes/a.md");
    });
    act(() => {
      void result.current.loadPane("pane-1", "notes/b.md");
    });
    await waitFor(() => expect(readCount()).toBe(2));
    const navigationSignal = workspaceReads()[1]?.signal;
    expect(result.current.paneStack[0]?.resolvedRef).toBeUndefined();

    act(() => {
      rerender({
        session: {
          ...editSession("session-1"),
          touchedNodes: [],
          touchedPaths: ["notes/a.md"],
          touchedNodeRefs: [{ notePath: "notes/a.md", kind: "NOTE" }],
          workspaces: [workspaceFor("notes/a.md")],
        },
      });
    });

    expect(navigationSignal?.aborted).toBe(false);
    await act(async () => {
      navigation.resolve(nodeDetailReply("notes/b.md"));
      await navigation.promise;
    });
    await waitFor(() => expect(result.current.paneStack[0]?.path).toBe("notes/b.md"));
  });

  it("does not let an old-ref SSE event restart active navigation", async () => {
    serveWorkspaces();
    const navigation = deferWorkspace("notes/b.md");
    const { result } = renderHook(() => usePaneStack());

    await act(async () => {
      await result.current.openRootPane("notes/a.md");
    });
    act(() => {
      void result.current.loadPane("pane-1", "notes/b.md");
    });
    await waitFor(() => expect(readCount()).toBe(2));
    const navigationSignal = workspaceReads()[1]?.signal;

    act(() => {
      sources.latest().emitJSON(nodeChangedEvent({ notePath: "notes/a.md", kind: "NOTE" }));
    });

    expect(readCount()).toBe(2);
    expect(navigationSignal?.aborted).toBe(false);
    await act(async () => {
      navigation.resolve(nodeDetailReply("notes/b.md"));
      await navigation.promise;
    });
    await waitFor(() => expect(result.current.paneStack[0]?.path).toBe("notes/b.md"));
  });

  it("does not let stale child updates cancel same-file root navigation", async () => {
    serveWorkspaces();

    const childRef: NodeRef = {
      notePath: "notes/a.md",
      nodeId: "child-1",
      kind: "EMBEDDED",
    };

    const rootNavigation = deferWorkspace("notes/a.md");

    const { result, rerender } = renderHook(({ session }) => usePaneStack(session), {
      initialProps: { session: noSession() },
    });

    await act(async () => {
      await result.current.openRootPane(childRef);
    });
    act(() => {
      void result.current.loadPane("pane-1", "notes/a.md");
    });
    await waitFor(() => expect(readCount()).toBe(2));
    const navigationSignal = workspaceReads()[1]?.signal;
    expect(result.current.paneStack[0]?.resolvedRef).toBeUndefined();

    act(() => {
      sources.latest().emitJSON(nodeChangedEvent(childRef));
    });
    expect(readCount()).toBe(2);

    act(() => {
      rerender({
        session: {
          ...editSession("session-child"),
          touchedNodes: [],
          touchedPaths: [],
          touchedNodeRefs: [childRef],
          workspaces: [workspaceFor("notes/a.md#node:child-1")],
        },
      });
    });

    expect(navigationSignal?.aborted).toBe(false);
    rootNavigation.resolve(nodeDetailReply("notes/a.md"));
    await act(async () => await rootNavigation.promise);
    await waitFor(() =>
      expect(result.current.paneStack[0]?.resolvedRef).toEqual({
        notePath: "notes/a.md",
        fragment: undefined,
        nodeId: undefined,
        typeName: undefined,
        structuralFingerprint: undefined,
        kind: "NOTE",
      }),
    );
  });

  it("aborts obsolete requests on close and unmount", async () => {
    serveWorkspaces();
    deferWorkspace("notes/replaced.md");
    deferWorkspace("notes/close.md");
    deferWorkspace("notes/unmount.md");
    const { result, unmount } = renderHook(() => usePaneStack());

    act(() => {
      void result.current.openRootPane("notes/replaced.md");
    });
    await waitFor(() => expect(readCount()).toBe(1));
    const replacedSignal = workspaceReads()[0]?.signal;
    act(() => {
      void result.current.openRootPane("notes/close.md");
    });
    expect(replacedSignal?.aborted).toBe(true);
    await waitFor(() => expect(readCount()).toBe(2));
    const closeSignal = workspaceReads()[1]?.signal;
    act(() => result.current.closePaneAt(0));
    expect(closeSignal?.aborted).toBe(true);
    expect(result.current.paneStack).toEqual([]);

    act(() => {
      void result.current.openRootPane("notes/unmount.md");
    });
    await waitFor(() => expect(readCount()).toBe(3));
    const unmountSignal = workspaceReads()[2]?.signal;
    unmount();
    expect(unmountSignal?.aborted).toBe(true);
  });

  it("preserves rapid same-turn pane transitions and cancels every obsolete request", async () => {
    serveWorkspaces();
    deferWorkspace("notes/root.md");
    deferWorkspace("notes/child.md");
    deferWorkspace("notes/replacement.md");

    const { result } = renderHook(() => usePaneStack(), {
      wrapper: StrictMode,
    });

    act(() => {
      void result.current.openRootPane("notes/root.md");
      result.current.focusOrPushNodeFromIndex(0, "notes/child.md");
      void result.current.loadPane("pane-1", "notes/replacement.md");
      result.current.closePaneAt(0);
    });

    expect(workspaceReads().map(requestedRef)).toEqual([
      "notes/root.md",
      "notes/child.md",
      "notes/replacement.md",
    ]);
    expect(result.current.paneStack).toEqual([]);

    for (const read of workspaceReads()) {
      expect(read.signal).toBeInstanceOf(AbortSignal);
      expect(read.signal).toMatchObject({ aborted: true });
    }
  });

  it("retains a queued drill across a higher-priority parent render", async () => {
    serveWorkspaces();
    const drillA = deferWorkspace("notes/root.md#a");
    const drillB = deferWorkspace("notes/root.md#b");

    const { result } = renderHook(() => {
      const [tick, setTick] = useState(0);

      return { ...usePaneStack(), tick, setTick };
    });

    await act(async () => {
      await result.current.openRootPane("notes/root.md");
    });
    await act(async () => {
      startTransition(() => {
        result.current.focusOrPushNodeFromIndex(0, "notes/root.md#a");
      });
      flushSync(() => result.current.setTick(1));
      result.current.focusOrPushNodeFromIndex(1, "notes/root.md#b");
    });

    expect(workspaceReads().map(requestedRef)).toEqual([
      "notes/root.md",
      "notes/root.md#a",
      "notes/root.md#b",
    ]);
    await act(async () => {
      drillB.resolve(nodeDetailReply("notes/root.md#b"));
      await drillB.promise;
    });
    await act(async () => {
      drillA.resolve(nodeDetailReply("notes/root.md#a"));
      await drillA.promise;
    });

    await waitFor(() =>
      expect(result.current.paneStack.map((pane) => pane.workspace?.node?.ref.fragment)).toEqual([
        undefined,
        "a",
        "b",
      ]),
    );
  });
});
