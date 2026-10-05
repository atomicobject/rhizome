import { useCallback, useEffect, useRef } from "react";

import type { ViewExecuteRequest, ViewExecuteResponse } from "../api/types";

/** Rows each scroll-triggered load adds to the table. */
const PAGE_STEP = 200;

/** Distance from the bottom, in pixels, at which the next rows start loading. */
const LOAD_MORE_THRESHOLD = 240;

type PageRequest = NonNullable<ViewExecuteRequest["page"]>;

/**
 * The table grows one window from the top as the reader scrolls instead of
 * paging (SPEC-0112). `loadMore` is set only while more rows remain and no
 * request is in flight; `loadingMore` marks a grown refetch, during which the
 * loaded rows stay on screen.
 */
export function growingWindow(
  enabled: boolean,
  state: ViewExecuteRequest,
  pageInfo: ViewExecuteResponse["pageInfo"] | undefined,
  loading: boolean,
  onPage: (page: PageRequest) => void,
) {
  const first = pageInfo?.first ?? state.page?.first ?? 25;
  const offset = pageInfo?.offset ?? state.page?.offset ?? 0;
  const loadingMore = enabled && loading && Boolean(pageInfo) && (state.page?.first ?? 0) > first;

  // ponytail: refetches the whole window from offset 0; switch to offset pages
  // merged client-side if large windows get slow. Not virtualized either.
  const loadMore =
    enabled && pageInfo?.hasMore && !loading
      ? () => onPage({ offset: 0, first: offset + first + PAGE_STEP })
      : undefined;

  return { loadMore, loadingMore };
}

/**
 * Loads the next rows whenever the end of the table is within reach: as the
 * reader scrolls, and again after each load settles, because rows that do not
 * fill the scroll area leave nothing to scroll. Returns the scroll
 * container's ref and scroll handler.
 */
export function useLoadOnScroll(onLoadMore: (() => void) | undefined) {
  const ref = useRef<HTMLDivElement>(null);

  const check = useCallback(() => {
    const element = ref.current;

    // A hidden table has no height to fill; it loads once it shows.
    if (!element || !onLoadMore || element.clientHeight === 0) return;

    if (element.scrollHeight - element.scrollTop - element.clientHeight < LOAD_MORE_THRESHOLD) {
      onLoadMore();
    }
  }, [onLoadMore]);

  // onLoadMore comes back once a load settles; check whether its rows filled the area.
  useEffect(check, [check]);

  return { ref, onScroll: check };
}
