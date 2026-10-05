import { QueryClient, QueryClientProvider, QueryObserver } from "@tanstack/react-query";
import { act, render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { getGlobalGraph } from "../api/client";
import { queryKeys } from "../api/queryKeys";
import type { GraphResponse } from "../api/types";
import { withFakeEventSource } from "../test/fakeEventSource";
import { deferredReply, jsonReply, withFakeFetch } from "../test/fakeFetch";
import { VaultInvalidationBridge } from "./VaultInvalidationBridge";

function graph(label: string): GraphResponse {
  return { nodes: [{ id: label, label, kind: "note" }], edges: [], truncated: false };
}

describe("global graph freshness", () => {
  const http = withFakeFetch();
  const sources = withFakeEventSource();

  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(10_000);
  });
  afterEach(() => vi.useRealTimers());

  async function beginRead(cached: boolean, active = true) {
    const options = { notesOnly: true };
    const key = queryKeys.graph.global(options);

    const client = new QueryClient({
      defaultOptions: { queries: { retry: false, gcTime: Infinity } },
    });

    if (cached) client.setQueryData(key, graph("cached"));
    const reply = deferredReply<GraphResponse>();
    const starts: number[] = [];
    const accepted: number[] = [];
    http.on("GET", "/api/v1/graphs/global", () => {
      starts.push(Date.now());

      return starts.length === 1 ? reply.promise : jsonReply(graph("current"));
    });

    const observer = new QueryObserver(client, {
      queryKey: key,
      queryFn: ({ signal }) => {
        accepted.push(Date.now());

        return getGlobalGraph(options, { signal });
      },
      staleTime: Infinity,
    });

    const unsubscribe = active ? observer.subscribe(() => {}) : () => {};

    const reading = observer.refetch();

    const view = render(
      <QueryClientProvider client={client}>
        <VaultInvalidationBridge />
      </QueryClientProvider>,
    );

    await act(async () => vi.advanceTimersByTimeAsync(0));

    return {
      client,
      key,
      observer,
      reading,
      reply,
      source: sources.latest(),
      starts,
      accepted,
      view,
      dispose: () => {
        reply.resolve(graph("obsolete"));
        unsubscribe();
        view.unmount();
        client.clear();
      },
    };
  }

  it.each(
    [false, true].flatMap((cached) =>
      [false, true].flatMap((burst) =>
        [false, true].map((late) => ({
          cached,
          burst,
          late,
          name: `${cached ? "cached refetch" : "first load"}, ${burst ? "event burst" : "single event"}, reply ${late ? "after" : "before"} deadline`,
        })),
      ),
    ),
  )("starts one corrective read for $name", async ({ cached, burst, late }) => {
    const read = await beginRead(cached);

    try {
      act(() => read.source.emit("", "index.changed"));
      let elapsed = 0;

      if (burst) {
        act(() => {
          vi.advanceTimersByTime(1000);
          read.source.emit("", "index.invalidated");
          vi.advanceTimersByTime(1000);
          read.source.emit("", "index.changed");
        });
        elapsed = 2000;
      }

      if (!late) {
        await act(async () => {
          await vi.advanceTimersByTimeAsync(3000 - elapsed);
          read.reply.resolve(graph("obsolete"));
          await read.reading;
        });
        elapsed = 3000;
      }

      await act(async () => vi.advanceTimersByTimeAsync(4999 - elapsed));
      expect(read.starts).toEqual([10_000]);
      await act(async () => vi.advanceTimersByTimeAsync(1));
      expect(read.starts).toEqual([10_000, 15_000]);

      if (late) {
        expect(http.calls[0]?.signal?.aborted).toBe(true);
        await act(async () => {
          read.reply.resolve(graph("obsolete"));
          await read.reply.promise;
          await read.reading;
          await vi.advanceTimersByTimeAsync(0);
        });
      }

      expect(read.client.getQueryData(read.key)).toEqual(graph("current"));
      expect(read.client.getQueryState(read.key)?.isInvalidated).toBe(false);
      await act(async () => vi.advanceTimersByTimeAsync(10_000));
      expect(read.starts).toHaveLength(2);
    } finally {
      read.dispose();
    }
  });

  it.each([false, true])(
    "keeps an inactive graph dirty until observed (cached: %s)",
    async (cached) => {
      const read = await beginRead(cached, false);
      let stop = () => {};

      try {
        act(() => read.source.emit("", "index.changed"));
        await act(async () => vi.advanceTimersByTimeAsync(5000));
        expect(read.starts).toHaveLength(1);
        expect(http.calls[0]?.signal?.aborted).toBe(true);
        await act(async () => {
          read.reply.resolve(graph("obsolete"));
          await read.reply.promise;
          await read.reading;
        });
        expect(read.client.getQueryState(read.key)?.isInvalidated).toBe(true);

        await act(async () => {
          stop = read.observer.subscribe(() => {});
          await vi.advanceTimersByTimeAsync(0);
        });
        expect(read.starts).toHaveLength(2);
        expect(read.client.getQueryData(read.key)).toEqual(graph("current"));
      } finally {
        stop();
        read.dispose();
      }
    },
  );

  it.each([false, true])(
    "reconnect corrects a pending graph and clears its timer (cached: %s)",
    async (cached) => {
      const read = await beginRead(cached);

      try {
        act(() => {
          read.source.open();
          read.source.emit("", "index.changed");
          vi.advanceTimersByTime(1000);
          read.source.open();
        });
        await act(async () => {
          read.reply.resolve(graph("obsolete"));
          await read.reply.promise;
          await read.reading;
          await vi.advanceTimersByTimeAsync(10_000);
        });
        expect(http.calls[0]?.signal?.aborted).toBe(true);
        expect(read.starts).toEqual([10_000, 11_000]);
        expect(read.client.getQueryData(read.key)).toEqual(graph("current"));
      } finally {
        read.dispose();
      }
    },
  );

  it.each([4999, 5000])("schedules no further reads after cleanup at %s ms", async (elapsed) => {
    const read = await beginRead(false);
    let acceptedReads = 0;

    try {
      act(() => {
        read.source.emit("", "index.changed");
        vi.advanceTimersByTime(elapsed);
        acceptedReads = read.accepted.length;
        read.view.unmount();
      });
      await act(async () => {
        read.reply.resolve(graph("obsolete"));
        await read.reply.promise;
        await read.reading;
        await vi.advanceTimersByTimeAsync(10_000);
      });
      expect(read.source.closed).toBe(true);
      expect(read.accepted).toHaveLength(acceptedReads);
      expect(read.starts).toHaveLength(acceptedReads);
    } finally {
      read.dispose();
    }
  });
});
