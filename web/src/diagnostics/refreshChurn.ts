import type { Query, QueryCache, QueryFilters } from "@tanstack/react-query";

import { finishRefetch, recordRefreshCycle, type RefreshCycle } from "./clientMetrics";

// Data each invalidated query held when the bridge invalidated it, and the
// refresh it belongs to. Structural sharing keeps that same reference when a
// refetch returns equal data.
const pending = new WeakMap<Query, { data: unknown; cycle: RefreshCycle }>();

/** Call at the invalidation point, before invalidating `filters`. */
export function trackRefresh(cache: QueryCache, filters: QueryFilters[]) {
  const cycle = recordRefreshCycle();

  for (const query of filters.flatMap((filter) => cache.findAll(filter))) {
    // A first load is not a refetch, and inactive queries are not refetched.
    if (query.state.data === undefined || !query.isActive()) continue;
    const earlier = pending.get(query);

    // A newer refresh takes over a refetch the earlier one still awaited.
    if (earlier) finishRefetch(earlier.cycle, null);
    pending.set(query, { data: query.state.data, cycle });
    cycle.pending++;
  }
}

/** Counts how refetches of invalidated queries resolve. Returns unsubscribe. */
export function observeRefetches(cache: QueryCache): () => void {
  return cache.subscribe((event) => {
    const entry = event.type === "updated" ? pending.get(event.query) : undefined;

    if (!entry || event.type !== "updated") return;
    const { action } = event;

    if (action.type === "error") {
      pending.delete(event.query);
      finishRefetch(entry.cycle, null);

      return;
    }

    if (action.type !== "success" || action.manual) return;
    pending.delete(event.query);
    finishRefetch(entry.cycle, action.data === entry.data);
  });
}
