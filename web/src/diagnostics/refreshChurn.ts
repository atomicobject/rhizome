import type { Query, QueryCache, QueryFilters } from "@tanstack/react-query";

import { recordRefetch, recordRefreshCycle } from "./clientMetrics";

// Data each invalidated query held when the bridge invalidated it. Structural
// sharing keeps that same reference when a refetch returns equal data.
const pending = new WeakMap<Query, unknown>();

/** Call at the invalidation point, before invalidating `filters`. */
export function trackRefresh(cache: QueryCache, filters: QueryFilters[]) {
  recordRefreshCycle();

  for (const query of filters.flatMap((filter) => cache.findAll(filter))) {
    // A first load is not a refetch.
    if (query.state.data !== undefined) pending.set(query, query.state.data);
  }
}

/** Counts how refetches of invalidated queries resolve. Returns unsubscribe. */
export function observeRefetches(cache: QueryCache): () => void {
  return cache.subscribe((event) => {
    if (event.type !== "updated" || !pending.has(event.query)) return;
    const { action } = event;

    if (action.type === "error") {
      pending.delete(event.query);

      return;
    }

    if (action.type !== "success" || action.manual) return;
    recordRefetch(action.data === pending.get(event.query));
    pending.delete(event.query);
  });
}
