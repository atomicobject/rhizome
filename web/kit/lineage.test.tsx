import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { PropsWithChildren } from "react";
import { afterEach, expect, it, vi } from "vitest";

import type { OntologyEditSessionResponse } from "../src/api/types";
import {
  EDIT_SESSION_MESSAGE,
  STAGE_OPS_MESSAGE,
  STAGE_RESULT_MESSAGE,
  type HostMessage,
} from "../src/lib/customViewMessages";
import { deferredReply, graphQLRequestBody, jsonReply, withFakeFetch } from "../src/test/fakeFetch";

const http = withFakeFetch();

const parentDescriptor = Object.getOwnPropertyDescriptor(window, "parent")!;

afterEach(() => {
  Object.defineProperty(window, "parent", parentDescriptor);
  document.body.replaceChildren();
  vi.restoreAllMocks();
});

it("binds strong edits to each query's retained read lineage", async () => {
  const original = {
    notePath: "a.md",
    fragment: "item-100",
    nodeId: "a.md#item-100",
    kind: "EMBEDDED",
    structuralFingerprint: "original",
  };

  const preview = {
    ...original,
    fragment: "item-130",
    nodeId: "a.md#item-130",
    structuralFingerprint: "saved",
  };

  const saved: OntologyEditSessionResponse = {
    sessionId: "kit-save",
    revision: 1,
    status: "clean",
    hasUncommittedChanges: false,
    createdAt: "2026-10-02T00:00:00Z",
    updatedAt: "2026-10-02T00:00:00Z",
    ops: [],
    refLineage: [{ original, preview }],
  };

  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });

  const wrapper = ({ children }: PropsWithChildren) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );

  const frame = document.createElement("iframe");
  document.body.append(frame);
  const parent = frame.contentWindow!;
  Object.defineProperty(window, "parent", { configurable: true, value: parent });
  const sent = vi.spyOn(parent, "postMessage");

  const answer = (data: HostMessage) =>
    act(() => {
      window.dispatchEvent(
        new MessageEvent("message", { source: parent, origin: window.location.origin, data }),
      );
    });

  vi.resetModules();
  const { useGraphQL, useSetField } = await import("./index");

  type Data = { ref: typeof original };

  http.on("POST", "/api/v1/graphql", () => jsonReply({ data: { ref: original } }), {
    when: (request) => graphQLRequestBody(request).query === "query Retained { ref }",
  });
  http.on("POST", "/api/v1/graphql", () => jsonReply({ data: { ref: original } }), {
    when: (request) => graphQLRequestBody(request).query === "query Canonical { ref }",
  });

  const { result } = renderHook(
    () => {
      const retained = useGraphQL<Data>("query Retained { ref }");
      const canonical = useGraphQL<Data>("query Canonical { ref }");

      return {
        retained,
        canonical,
        retainedWrite: useSetField(retained.displaySession),
        canonicalWrite: useSetField(canonical.displaySession),
      };
    },
    { wrapper },
  );

  await waitFor(() => {
    expect(result.current.retained.data?.ref).toEqual(original);
    expect(result.current.canonical.data?.ref).toEqual(original);
  });
  const pending = deferredReply<{ data: Data }>();
  http.on("POST", "/api/v1/graphql", () => pending.promise, {
    when: (request) => graphQLRequestBody(request).query === "query Retained { ref }",
  });
  // The independent canonical reader sees a legitimately restored old strong ref.
  answer({
    type: EDIT_SESSION_MESSAGE,
    session: null,
    readLifecycle: { revision: 1, outcome: "saved", savedSession: saved },
  });
  await waitFor(() => {
    expect(result.current.retained.savedEditsPending).toBe(true);
    expect(result.current.canonical.savedEditsPending).toBe(false);
  });

  for (const [reader, expected] of [
    ["retained", preview],
    ["canonical", original],
  ] as const) {
    const write =
      reader === "retained" ? result.current.retainedWrite : result.current.canonicalWrite;

    const target = result.current[reader].data!.ref;
    const before = sent.mock.calls.length;
    const input = { target, field: "status", value: "blocked" };
    const onSuccess = vi.fn();
    const onSettled = vi.fn();
    let staged: Promise<unknown> | undefined;
    act(() => {
      if (reader === "retained") staged = write.mutateAsync(input, { onSuccess, onSettled });
      else write.mutate(input, { onSuccess, onSettled });
    });
    await waitFor(() =>
      expect(
        sent.mock.calls.slice(before).some(([message]) => message.type === STAGE_OPS_MESSAGE),
      ).toBe(true),
    );

    const [request] = sent.mock.calls
      .slice(before)
      .find(([message]) => message.type === STAGE_OPS_MESSAGE)!;

    expect(request.ops).toEqual([
      expect.objectContaining({
        path: `${expected.notePath}#${expected.fragment}`,
        nodeId: expected.nodeId,
        structuralFingerprint: expected.structuralFingerprint,
      }),
    ]);
    answer({ type: STAGE_RESULT_MESSAGE, requestId: request.requestId });
    await act(async () => {
      await staged;
    });
    await waitFor(() => expect(onSuccess).toHaveBeenCalledOnce());
    expect(onSuccess.mock.calls[0][1]).toBe(input);
    expect(onSettled.mock.calls[0][2]).toBe(input);

    const currentWrite =
      reader === "retained" ? result.current.retainedWrite : result.current.canonicalWrite;

    expect(currentWrite.variables).toBe(input);
  }

  await act(async () => pending.resolve({ data: { ref: preview } }));
});
