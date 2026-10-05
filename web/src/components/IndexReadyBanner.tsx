import { useEffect, useState } from "react";
import { getPublicEventsUrl, getStatus } from "../api/client";
import { isCallable } from "../api/parse";
import type { StatusResponse } from "../api/types";

type IndexReadyBannerProps = {
  // AppShell already fetches status for the vault subtitle. Passing that
  // snapshot avoids a duplicate startup request; standalone consumers may
  // omit it and let this component own the fetch.
  status?: StatusResponse | null;
  onStatusChange?: (status: StatusResponse) => void;
};

// IndexReadyBanner renders a non-blocking "Preparing index…" affordance under
// the AppShell nav whenever `/api/v1/status` reports `indexState ===
// "initializing"`. It hides as soon as status flips to `ready` (or on the
// existing `index.changed` SSE invalidation event), so the gate transition
// surfaces in the UI without forcing a manual refresh. Routes render normally
// beneath the banner; gated endpoints surface their own in-pane loading
// states until the backend gate opens.
export function IndexReadyBanner({ status, onStatusChange }: IndexReadyBannerProps = {}) {
  const controlled = status !== undefined;
  const [fetchedState, setFetchedState] = useState<StatusResponse["indexState"] | null>(null);
  const state = controlled ? (status?.indexState ?? null) : fetchedState;

  useEffect(() => {
    let cancelled = false;
    let source: EventSource | null = null;

    const closeSource = () => {
      source?.close();
      source = null;
    };

    const subscribe = (refresh: () => void) => {
      if (source || typeof EventSource === "undefined") return;
      const nextSource = new EventSource(getPublicEventsUrl());
      source = nextSource;

      if (!isCallable(nextSource.addEventListener)) {
        closeSource();

        return;
      }

      nextSource.addEventListener("index.changed", refresh);
    };

    const refresh = () => {
      getStatus()
        .then((status) => {
          if (cancelled) return;
          const nextState = status.indexState ?? "ready";

          if (!controlled) setFetchedState(nextState);
          onStatusChange?.(status);

          if (nextState === "initializing") {
            subscribe(refresh);
          } else {
            closeSource();
          }
        })
        .catch(() => {
          // Status is cheap and ungated; a transient failure here means we
          // can't prove readiness either way. Leave the current state alone
          // rather than forcing a banner that might be wrong.
        });
    };

    if (!controlled) {
      refresh();
    } else if (status?.indexState === "initializing") {
      subscribe(refresh);
    }

    return () => {
      cancelled = true;
      closeSource();
    };
  }, [controlled, onStatusChange, status]);

  if (state !== "initializing") return null;

  return (
    <div
      className="index-ready-banner"
      role="status"
      aria-live="polite"
      data-testid="index-ready-banner"
    >
      <span className="index-ready-banner__spinner" aria-hidden="true" />
      <span className="index-ready-banner__label">Preparing index…</span>
      <span className="index-ready-banner__hint">First-launch reads will retry automatically.</span>
    </div>
  );
}
