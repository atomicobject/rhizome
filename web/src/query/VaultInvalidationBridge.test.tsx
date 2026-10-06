import {
  InfiniteQueryObserver,
  QueryClient,
  QueryClientProvider,
  type QueryKey,
} from "@tanstack/react-query";
import { act, render, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { queryKeys } from "../api/queryKeys";

import { VaultInvalidationBridge } from "./VaultInvalidationBridge";
import { subscribeVaultEvents, type VaultEvent } from "./vaultEvents";

import { FakeEventSource } from "../test/fakeEventSource";

describe("VaultInvalidationBridge", () => {
  beforeEach(() => {
    FakeEventSource.instances.length = 0;
    vi.stubGlobal("EventSource", FakeEventSource);
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  const globalGraphKey = queryKeys.graph.global({ notesOnly: true });

  function mountWithCachedQueries() {
    const client = new QueryClient();
    client.setQueryData(queryKeys.ontology.summary(), { types: [] });
    client.setQueryData(globalGraphKey, { nodes: [], edges: [] });
    client.setQueryData(queryKeys.validation(), { status: "ok" });
    client.setQueryData(queryKeys.status(), { ready: true });
    render(
      <QueryClientProvider client={client}>
        <VaultInvalidationBridge />
      </QueryClientProvider>,
    );

    return { client, source: FakeEventSource.instances[0] };
  }

  const isInvalidated = (client: QueryClient, key: QueryKey) =>
    client.getQueryState(key)?.isInvalidated === true;

  it("invalidates index-backed queries and defers the global graph", () => {
    vi.useFakeTimers();
    const { client, source } = mountWithCachedQueries();
    expect(source.url).toBe("/api/v1/events");

    act(() => source.emit("", "index.invalidated"));

    expect(isInvalidated(client, queryKeys.ontology.summary())).toBe(true);
    expect(isInvalidated(client, queryKeys.status())).toBe(true);
    expect(isInvalidated(client, globalGraphKey)).toBe(false);

    act(() => vi.advanceTimersByTime(5000));
    expect(isInvalidated(client, globalGraphKey)).toBe(true);
  });

  it("refetches at once for the first event of a save's burst and once after it", () => {
    vi.useFakeTimers();
    const { client, source } = mountWithCachedQueries();
    const invalidate = vi.spyOn(client, "invalidateQueries");

    const indexCalls = () =>
      invalidate.mock.calls.filter(([filters]) => filters?.queryKey?.[1] !== "graph").length;

    act(() => source.emit("", "node.changed"));
    expect(indexCalls()).toBe(1);

    act(() => {
      for (const delay of [200, 150, 100, 200]) {
        vi.advanceTimersByTime(delay);
        source.emit("", "index.changed");
      }
    });
    expect(indexCalls()).toBe(1);

    act(() => vi.advanceTimersByTime(2000));
    expect(indexCalls()).toBe(2);

    act(() => source.emit("", "edit_session.invalidated"));
    expect(indexCalls()).toBe(3);
  });

  it("coalesces an index-event burst into one global graph invalidation", () => {
    vi.useFakeTimers();
    const { client, source } = mountWithCachedQueries();
    const invalidate = vi.spyOn(client, "invalidateQueries");

    act(() => {
      source.emit("", "index.invalidated");
      vi.advanceTimersByTime(1000);
      source.emit("", "index.changed");
      vi.advanceTimersByTime(1000);
      source.emit("", "index.invalidated");
      vi.advanceTimersByTime(4000);
    });

    const graphCalls = invalidate.mock.calls.filter(
      ([filters]) => filters?.queryKey?.[1] === "graph",
    );

    expect(graphCalls).toHaveLength(1);
  });

  it("drops the pending graph invalidation when the stream reconnects", () => {
    vi.useFakeTimers();
    const { client, source } = mountWithCachedQueries();
    const invalidate = vi.spyOn(client, "invalidateQueries");

    act(() => {
      source.open();
      source.emit("", "index.invalidated");
      source.open();
    });
    expect(isInvalidated(client, globalGraphKey)).toBe(true);
    act(() => vi.advanceTimersByTime(5000));

    const graphCalls = invalidate.mock.calls.filter(
      ([filters]) => filters?.queryKey?.[1] === "graph",
    );

    expect(graphCalls).toHaveLength(0);
  });

  it("invalidates graphs at the original deadline even if data arrived during the burst", () => {
    vi.useFakeTimers();
    const { client, source } = mountWithCachedQueries();
    const freshKey = queryKeys.graph.global({ notesOnly: false });

    act(() => {
      source.emit("", "index.invalidated");
      vi.advanceTimersByTime(1000);
      client.setQueryData(globalGraphKey, { nodes: [], edges: [] });
      vi.advanceTimersByTime(1000);
      source.emit("", "index.changed");
      vi.advanceTimersByTime(1000);
      client.setQueryData(freshKey, { nodes: [], edges: [] });
      vi.advanceTimersByTime(1999);
    });

    expect(isInvalidated(client, globalGraphKey)).toBe(false);
    act(() => vi.advanceTimersByTime(1));
    expect(isInvalidated(client, globalGraphKey)).toBe(true);
    expect(isInvalidated(client, freshKey)).toBe(true);
  });

  it("publishes each stream event and later reconnects to host listeners", () => {
    const { source } = mountWithCachedQueries();
    const seen: VaultEvent[] = [];
    const stop = subscribeVaultEvents((event) => seen.push(event));

    act(() => {
      source.open();
      source.emit('{"folder":"board"}', "views.changed");
      source.emit("", "validate.changed");
      source.emit("", "unknown.event");
      source.open();
    });
    stop();

    expect(seen).toEqual([
      { event: "views.changed", data: '{"folder":"board"}' },
      { event: "validate.changed" },
      { event: "stream.reconnected" },
    ]);
  });

  it("invalidates only validation state on validation events", () => {
    vi.useFakeTimers();
    const { client, source } = mountWithCachedQueries();

    act(() => source.emit("", "validation.invalidated"));

    expect(isInvalidated(client, queryKeys.validation())).toBe(true);
    expect(isInvalidated(client, queryKeys.status())).toBe(true);
    expect(isInvalidated(client, queryKeys.ontology.summary())).toBe(false);

    act(() => vi.advanceTimersByTime(10000));
    expect(isInvalidated(client, globalGraphKey)).toBe(false);
  });

  it("refreshes active vault queries on reconnect and closes on unmount", async () => {
    const client = new QueryClient();
    const diagnosticsKey = queryKeys.validationDiagnostics(1, {});
    const summariesKey = queryKeys.validationScopeSummaries(1, []);
    client.setQueryData(diagnosticsKey, { pages: [] });
    client.setQueryData(summariesKey, { summaries: [] });
    const invalidate = vi.spyOn(client, "invalidateQueries");

    const view = render(
      <QueryClientProvider client={client}>
        <VaultInvalidationBridge />
      </QueryClientProvider>,
    );

    const source = FakeEventSource.instances[0];

    act(() => source.open());
    expect(invalidate).not.toHaveBeenCalled();
    act(() => source.open());
    await waitFor(() => expect(invalidate).toHaveBeenCalledTimes(1));
    expect(isInvalidated(client, diagnosticsKey)).toBe(true);
    expect(isInvalidated(client, summariesKey)).toBe(true);

    const close = vi.spyOn(source, "close");
    view.unmount();
    expect(close).toHaveBeenCalledTimes(1);
  });

  it("reopens a stream the browser gave up on and refreshes everything once it is back", () => {
    vi.useFakeTimers();
    const { client, source } = mountWithCachedQueries();

    act(() => {
      source.open();
      source.fail();
      vi.advanceTimersByTime(3000);
    });

    const reopened = FakeEventSource.instances[1];
    expect(reopened?.url).toBe("/api/v1/events");

    act(() => reopened.open());
    expect(isInvalidated(client, queryKeys.ontology.summary())).toBe(true);
    expect(isInvalidated(client, globalGraphKey)).toBe(true);
  });
});

describe("validation snapshot pagination", () => {
  beforeEach(() => {
    FakeEventSource.instances.length = 0;
    vi.stubGlobal("EventSource", FakeEventSource);
  });

  afterEach(() => vi.unstubAllGlobals());

  it.each(["index.changed", "validate.changed", "validation.invalidated"])(
    "keeps a clicked page in flight through %s while refreshing the envelope",
    async (event) => {
      const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
      const diagnosticsKey = queryKeys.validationDiagnostics(7, {});
      const summaryKey = queryKeys.validationScopeSummaries(7, [{ kind: "file", key: "note.md" }]);
      const page = (index: number) => ({ diagnostics: [`issue-${index}`], nextCursor: index + 1 });
      client.setQueryData(diagnosticsKey, {
        pages: [0, 1, 2, 3].map(page),
        pageParams: [0, 1, 2, 3],
      });
      client.setQueryData(summaryKey, { generation: 7, summaries: [] });
      client.setQueryData(queryKeys.validation(), { generation: 7 });
      let releasePage: (value: ReturnType<typeof page>) => void = () => {};

      let activeSignal: AbortSignal | undefined;

      const fifthPage = new Promise<ReturnType<typeof page>>((resolve) => {
        releasePage = resolve;
      });

      const observer = new InfiniteQueryObserver(client, {
        queryKey: diagnosticsKey,
        queryFn: ({ pageParam, signal }) => {
          if (pageParam === 4) {
            activeSignal = signal;

            return fifthPage;
          }

          return Promise.resolve(page(pageParam));
        },
        initialPageParam: 0,
        getNextPageParam: (result) => result.nextCursor,
        staleTime: Infinity,
      });

      const unsubscribe = observer.subscribe(() => {});

      const view = render(
        <QueryClientProvider client={client}>
          <VaultInvalidationBridge />
        </QueryClientProvider>,
      );

      const source = FakeEventSource.instances[0];

      try {
        const loading = observer.fetchNextPage();
        await waitFor(() => expect(activeSignal).toBeDefined());
        act(() => {
          source.emit("", event);
        });
        expect(activeSignal?.aborted).toBe(false);
        expect(client.getQueryState(diagnosticsKey)?.isInvalidated).toBe(false);
        expect(client.getQueryState(summaryKey)?.isInvalidated).toBe(false);
        expect(client.getQueryState(queryKeys.validation())?.isInvalidated).toBe(true);
        releasePage(page(4));
        await loading;
        expect(observer.getCurrentResult().data?.pages).toHaveLength(5);
        expect(observer.getCurrentResult().data?.pages[4].diagnostics).toEqual(["issue-4"]);
      } finally {
        releasePage(page(4));
        unsubscribe();
        view.unmount();
        client.clear();
      }
    },
  );
});
