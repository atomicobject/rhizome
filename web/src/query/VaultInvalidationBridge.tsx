import { useQueryClient, type QueryKey } from "@tanstack/react-query";
import { useEffect } from "react";

import { getPublicEventsUrl } from "../api/client";
import { openReconnectingEventSource } from "../api/eventStream";
import { isCallable, isString } from "../api/parse";
import { isValidationSnapshotQuery, queryKeys } from "../api/queryKeys";
import { advanceVaultIndexRevision } from "./vaultIndexRevision";
import {
  INDEX_EVENTS,
  STREAM_RECONNECTED_EVENT,
  VALIDATION_EVENTS,
  VAULT_EVENTS,
  publishVaultEvent,
} from "./vaultEvents";

// The global graph response is multi-megabyte and re-laid out on arrival, so a
// burst of index epochs must not refetch it once per epoch.
// ponytail: time-based coalescing because no browser-visible endpoint exposes
// the server's graph_web_revision; compare revisions instead once one does.
const GLOBAL_GRAPH_INVALIDATE_DELAY_MS = 5_000;

// One save publishes several index events a few hundred milliseconds apart:
// the watcher batch, then one per derived domain. The first refetches at once;
// later ones in the window refetch once when it closes.
const INDEX_INVALIDATE_WINDOW_MS = 750;

const isGlobalGraphQuery = (query: { queryKey: QueryKey }) =>
  query.queryKey[1] === "graph" && query.queryKey[2] === "global";

/**
 * Converts the vault-wide SSE stream into cache invalidation. The stream is a
 * freshness signal only; HTTP/query responses remain the source of truth.
 */
export function VaultInvalidationBridge() {
  const queryClient = useQueryClient();

  useEffect(() => {
    if (typeof EventSource === "undefined") return;

    let globalGraphTimer: ReturnType<typeof setTimeout> | undefined;

    const globalGraphQueries = {
      queryKey: queryKeys.graph.all(),
      predicate: isGlobalGraphQuery,
    };

    const cancelGlobalGraphReads = () =>
      queryClient.cancelQueries({ ...globalGraphQueries, fetchStatus: "fetching" });

    const clearGlobalGraphTimer = () => {
      if (globalGraphTimer === undefined) return;
      clearTimeout(globalGraphTimer);
      globalGraphTimer = undefined;
    };

    const invalidateGlobalGraphSoon = () => {
      if (globalGraphTimer !== undefined) return;
      globalGraphTimer = setTimeout(() => {
        globalGraphTimer = undefined;
        void cancelGlobalGraphReads();
        void queryClient.invalidateQueries(globalGraphQueries);
      }, GLOBAL_GRAPH_INVALIDATE_DELAY_MS);
    };

    let indexWindow: ReturnType<typeof setTimeout> | undefined;
    let indexPending = false;

    const refetchIndexQueries = () => {
      void queryClient.invalidateQueries({
        queryKey: queryKeys.all,
        predicate: (query) => !isGlobalGraphQuery(query) && !isValidationSnapshotQuery(query),
      });
      invalidateGlobalGraphSoon();
    };

    const closeIndexWindow = () => {
      if (!indexPending) {
        indexWindow = undefined;

        return;
      }

      indexPending = false;
      refetchIndexQueries();
      indexWindow = setTimeout(closeIndexWindow, INDEX_INVALIDATE_WINDOW_MS);
    };

    const invalidateIndex = () => {
      advanceVaultIndexRevision();

      if (indexWindow !== undefined) {
        indexPending = true;

        return;
      }

      refetchIndexQueries();
      indexWindow = setTimeout(closeIndexWindow, INDEX_INVALIDATE_WINDOW_MS);
    };

    const invalidateValidation = () => {
      void queryClient.invalidateQueries({
        queryKey: queryKeys.validationAll(),
        predicate: (query) => !isValidationSnapshotQuery(query),
      });
      void queryClient.invalidateQueries({ queryKey: queryKeys.status() });
    };

    let connected = false;

    const invalidateAfterReconnect = () => {
      advanceVaultIndexRevision();

      if (!connected) {
        connected = true;

        return;
      }

      // A restarted runtime may use a rebuilt database with reused generation numbers.
      clearGlobalGraphTimer();
      void cancelGlobalGraphReads();
      void queryClient.invalidateQueries({ queryKey: queryKeys.all });
      publishVaultEvent({ event: STREAM_RECONNECTED_EVENT });
    };

    const stop = openReconnectingEventSource(getPublicEventsUrl(), (source) => {
      if (!isCallable(source.addEventListener)) {
        source.close();

        return;
      }

      source.addEventListener("open", invalidateAfterReconnect);

      for (const event of INDEX_EVENTS) {
        source.addEventListener(event, invalidateIndex);
      }

      for (const event of VALIDATION_EVENTS) {
        source.addEventListener(event, invalidateValidation);
      }

      // Framed custom views open no stream of their own; their frames forward these.
      for (const event of VAULT_EVENTS) {
        source.addEventListener(event, ({ data }: MessageEvent) =>
          publishVaultEvent(isString(data) && data ? { event, data } : { event }),
        );
      }
    });

    return () => {
      clearGlobalGraphTimer();
      clearTimeout(indexWindow);
      stop();
    };
  }, [queryClient]);

  return null;
}
