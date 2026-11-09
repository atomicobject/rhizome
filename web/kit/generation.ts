// The index generation a frame last saw, and the rule that keeps every visible
// query at it (SPEC-0136 R4).
//
// The generation moves with every event, resume message, and applied
// response. A response read while it moved, at a different generation, is
// dropped and read once more; that second response is applied whatever it
// says, so a burst of writes delays a query but never starves it. Whenever the
// generation moves, every visible idle query answered at another generation
// refetches once, coalesced into one batch.
import type { Query, QueryClient } from "@tanstack/react-query";

import { isValidationSnapshotQuery } from "../src/api/queryKeys";
import { responseGeneration } from "./coverage";

/**
 * A burst refreshes once this long after its last change. A refresh cancels
 * in-flight fetches, so waiting for the gap keeps a steady burst from
 * restarting a slow query forever.
 */
export const EVENT_COALESCE_MS = 150;

/** A burst that never pauses still refreshes this often. */
export const EVENT_MAX_WAIT_MS = 1000;

export type GenerationRead<T> = { data: T; generation?: string };

export type FrameGeneration = {
  /** The generation the frame last saw; undefined until the server names one. */
  current: () => string | undefined;
  /** Adopt a generation from an event, resume or location message, or reconnect. */
  set: (generation: string | undefined) => void;
  /** Read through the rule: drop a response that raced a newer generation and read once more. */
  read: <T>(fetch: () => Promise<GenerationRead<T>>, signal?: AbortSignal) => Promise<T>;
  /** Check visible queries in the next batch, as when a hidden page is shown. */
  schedule: () => void;
  /** Keep this client's visible queries at the current generation; returns a detach. */
  attach: (queryClient: QueryClient) => () => void;
  stop: () => void;
};

const abortError = (signal: AbortSignal) =>
  signal.reason instanceof Error ? signal.reason : new DOMException("Aborted", "AbortError");

export function createFrameGeneration(): FrameGeneration {
  let current: string | undefined;
  // When the generation last moved, for queries whose data carries none.
  let movedAt = 0;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let burstStart: number | undefined;
  let waiters: Array<() => void> = [];
  const clients = new Set<QueryClient>();

  const behind = (query: Query) => {
    if (isValidationSnapshotQuery(query)) return false;

    const answered = responseGeneration(query.state.data);

    if (answered !== undefined) return answered !== current;

    return Math.max(query.state.dataUpdatedAt, query.state.errorUpdatedAt) < movedAt;
  };

  const flush = () => {
    timer = undefined;
    burstStart = undefined;
    const ready = waiters;
    waiters = [];

    for (const resolve of ready) resolve();

    for (const client of clients) {
      // A query with a fetch in flight has its response judged on arrival.
      void client.refetchQueries(
        {
          type: "active",
          predicate: (query) => query.state.fetchStatus === "idle" && behind(query),
        },
        { cancelRefetch: false },
      );
    }
  };

  const schedule = () => {
    burstStart ??= Date.now();
    clearTimeout(timer);
    const untilMaxWait = burstStart + EVENT_MAX_WAIT_MS - Date.now();
    timer = setTimeout(flush, Math.max(0, Math.min(EVENT_COALESCE_MS, untilMaxWait)));
  };

  const set = (generation: string | undefined) => {
    if (generation === undefined || generation === current) return;
    current = generation;
    movedAt = Date.now();
    schedule();
  };

  const nextBatch = (signal?: AbortSignal) =>
    new Promise<void>((resolve, reject) => {
      if (signal?.aborted) return reject(abortError(signal));

      const onAbort = () => {
        if (signal) reject(abortError(signal));
      };

      signal?.addEventListener("abort", onAbort, { once: true });
      waiters.push(() => {
        signal?.removeEventListener("abort", onAbort);
        resolve();
      });
      schedule();
    });

  const read = async <T>(fetch: () => Promise<GenerationRead<T>>, signal?: AbortSignal) => {
    const sent = current;
    const first = await fetch();

    if (first.generation === undefined || current === sent || first.generation === current) {
      set(first.generation);

      return first.data;
    }

    // Stale: the generation moved while this read was out, to another value.
    await nextBatch(signal);
    const next = await fetch();

    // Applied after a drop, it leaves the generation alone and its query
    // checks again in the next batch.
    if (next.generation !== undefined && next.generation !== current) schedule();

    return next.data;
  };

  return {
    current: () => current,
    set,
    read,
    schedule,
    attach: (queryClient) => {
      clients.add(queryClient);

      return () => clients.delete(queryClient);
    },
    stop: () => {
      clearTimeout(timer);
      timer = undefined;
      burstStart = undefined;
    },
  };
}

/** The generation of this frame; every kit read and freshness listener shares it. */
export const frameGeneration = createFrameGeneration();
