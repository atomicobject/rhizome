import { act, renderHook, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { PublicGraphQLResult, PublicNodeDetailData } from "../api/publicGraphQLTypes";
import type { NodeRef } from "../api/types";
import { publishVaultEvent } from "../query/vaultEvents";
import { advanceVaultIndexRevision } from "../query/vaultIndexRevision";
import { withFakeEventSource } from "../test/fakeEventSource";
import { deferredReply, graphQLRequestBody, jsonReply, withFakeFetch } from "../test/fakeFetch";
import { usePaneStack } from "./usePaneStack";

type NodeDetailReply = PublicGraphQLResult<PublicNodeDetailData>;

function nodeDetailReply(ref: NodeRef): NodeDetailReply {
  return {
    data: {
      node: {
        ref: {
          ref: ref.notePath,
          notePath: ref.notePath,
          fragment: ref.fragment,
          nodeId: ref.nodeId,
          structural: ref.structuralFingerprint,
          kind: ref.kind || "NOTE",
        },
        nodeId: ref.nodeId || `note:${ref.notePath}`,
        nodeKind: ref.kind || "NOTE",
        path: ref.notePath,
        title: "Target node",
        content: "Target body",
        locator: {
          sourceLocator: ref.notePath,
          status: "linkable",
          exists: true,
          requiresFix: false,
        },
      },
    },
  };
}

describe("pane request lifecycle", () => {
  const http = withFakeFetch();
  const sources = withFakeEventSource();

  it.each(["Before", null])(
    "refreshes a successful pane to committed type %s after a raw watch read",
    async (resolvedType) => {
      const ref = { notePath: "notes/foo.md", kind: "NOTE" };
      let committed = false;
      let reads = 0;
      http.onGraphQL("PublicNodeDetail", () => {
        reads += 1;
        const reply = nodeDetailReply(ref);

        if (!reply.data?.node) throw new Error("missing fixture node");
        reply.data.node.resolvedType = committed ? resolvedType : "After";
        reply.data.node.content = reads === 1 ? "Old body" : "New body";

        return jsonReply(reply);
      });
      const { result } = renderHook(() => usePaneStack());
      await act(async () => result.current.openRootPane(ref.notePath));
      await waitFor(() => expect(sources.instances).toHaveLength(1));
      act(() => sources.latest().emitJSON({ id: "raw", kind: "node.changed", ref }));
      await waitFor(() =>
        expect(result.current.paneStack[0]?.workspace?.content.markdown).toBe("New body"),
      );
      expect(result.current.paneStack[0]?.workspace?.node.resolvedType).toBe("After");

      act(() =>
        publishVaultEvent({
          event: "node.changed",
          data: JSON.stringify({ data: { paths: ["notes/unaffected.md"] } }),
        }),
      );
      expect(reads).toBe(2);

      committed = true;
      act(() =>
        publishVaultEvent({
          event: "node.changed",
          data: JSON.stringify({ data: { paths: [ref.notePath] } }),
        }),
      );
      await waitFor(() =>
        expect(result.current.paneStack[0]?.workspace?.node.resolvedType).toBe(
          resolvedType || undefined,
        ),
      );
      expect(result.current.paneStack[0]?.workspace?.content.resolvedType).toBe(
        resolvedType || undefined,
      );
      expect(result.current.paneStack[0]?.workspace?.content.markdown).toBe("New body");
    },
  );

  it("replaces an initial read still pending when committed metadata arrives", async () => {
    const ref = { notePath: "notes/foo.md", kind: "NOTE" };
    const initial = deferredReply<NodeDetailReply>();
    let reads = 0;
    http.onGraphQL("PublicNodeDetail", () => {
      if (++reads === 1) return initial.promise;
      const reply = nodeDetailReply(ref);

      if (!reply.data?.node) throw new Error("missing fixture node");
      reply.data.node.resolvedType = "Before";
      reply.data.node.content = "Committed body";

      return jsonReply(reply);
    });
    const { result } = renderHook(() => usePaneStack());
    let opened: Promise<void> | undefined;
    act(() => {
      opened = result.current.openRootPane(ref.notePath);
    });
    await waitFor(() => expect(reads).toBe(1));
    act(() =>
      publishVaultEvent({
        event: "node.changed",
        data: JSON.stringify({ data: { paths: [ref.notePath] } }),
      }),
    );
    await waitFor(() =>
      expect(result.current.paneStack[0]?.workspace?.node.resolvedType).toBe("Before"),
    );
    const stale = nodeDetailReply(ref);

    if (!stale.data?.node) throw new Error("missing fixture node");
    stale.data.node.resolvedType = "After";
    stale.data.node.content = "Unpublished body";
    await act(async () => {
      initial.resolve(stale);
      await opened;
    });
    expect(result.current.paneStack[0]?.workspace?.node.resolvedType).toBe("Before");
    expect(result.current.paneStack[0]?.workspace?.content.markdown).toBe("Committed body");
  });

  it.each(["success", "failure"])(
    "keeps a deleted pane cleared after a pending refresh settles with %s",
    async (settlement) => {
      const ref = { notePath: "notes/foo.md", kind: "NOTE" };
      const refresh = deferredReply<NodeDetailReply>();
      let reads = 0;
      http.onGraphQL("PublicNodeDetail", () =>
        ++reads === 1 ? jsonReply(nodeDetailReply(ref)) : refresh.promise,
      );
      const { result } = renderHook(() => usePaneStack());
      await act(async () => result.current.openRootPane(ref.notePath));
      await waitFor(() => expect(sources.instances).toHaveLength(1));
      const source = sources.latest();

      act(() => source.emitJSON({ id: "changed", kind: "node.changed", ref }));
      await waitFor(() => expect(reads).toBe(2));
      const signal = http.calls.at(-1)?.signal;
      expect(signal?.aborted).toBe(false);

      act(() => source.emitJSON({ id: "deleted", kind: "node.deleted", ref }));
      expect(source.closed).toBe(true);

      await act(async () => {
        if (settlement === "success") refresh.resolve(nodeDetailReply(ref));
        else refresh.reject(new Error("late read failed"));
        await refresh.promise.catch(() => {});
      });

      expect(result.current.paneStack[0]?.workspace).toBeNull();
      expect(result.current.paneStack[0]?.rendered).toBeNull();
      expect(result.current.paneStack[0]?.resolvedRef).toBeUndefined();
      expect(result.current.paneStack[0]?.loading).toBe(false);
      expect(result.current.paneStack[0]?.error).toBe("notes/foo.md was deleted or moved on disk.");
      expect(signal?.aborted).toBe(true);
      expect(sources.instances).toHaveLength(1);
      expect(reads).toBe(2);
    },
  );

  it.each([
    {
      name: "structural fingerprint",
      target: {
        notePath: "notes/foo.md",
        nodeId: "item-1",
        structuralFingerprint: "fp-1",
        kind: "EMBEDDED",
      },
      locator: "notes/foo.md#struct:fp-1",
    },
    {
      name: "node ID",
      target: { notePath: "notes/foo.md", nodeId: "item-1", kind: "EMBEDDED" },
      locator: "notes/foo.md#node:item-1",
    },
    {
      name: "structural fingerprint with source fragment",
      target: {
        notePath: "notes/foo.md",
        fragment: "item-1",
        nodeId: "item-1",
        structuralFingerprint: "fp-1",
        kind: "EMBEDDED",
      },
      locator: "notes/foo.md#struct:fp-1",
    },
  ])("retries the original $name after initial failure", async ({ target, locator }) => {
    let reads = 0;
    http.onGraphQL("PublicNodeDetail", (request) => {
      if (++reads === 1) return jsonReply({ message: "temporarily unavailable" }, 500);
      const ref = graphQLRequestBody(request).variables?.ref;

      return jsonReply(
        nodeDetailReply(ref === locator ? target : { notePath: target.notePath, kind: "NOTE" }),
      );
    });
    const { result } = renderHook(() => usePaneStack());
    await act(async () => result.current.openRootPane(target));
    expect(result.current.paneStack[0]?.error).toBeTruthy();
    expect(result.current.paneStack[0]?.path).toBe(target.notePath);
    expect(result.current.paneStack[0]?.anchor).toBe(target.fragment || null);

    act(() => advanceVaultIndexRevision());
    await waitFor(() => expect(result.current.paneStack[0]?.workspace).toBeTruthy());
    expect(http.calls.map((request) => graphQLRequestBody(request).variables?.ref)).toEqual([
      locator,
      locator,
    ]);
    expect(result.current.paneStack[0]?.resolvedRef).toMatchObject(target);
    expect(result.current.paneStack[0]?.error).toBeNull();
  });
});
