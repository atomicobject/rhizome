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
  rebuilding?: boolean;
};

// IndexReadyBanner renders a non-blocking "Preparing index…" affordance under
// the AppShell nav whenever `/api/v1/status` reports `indexState ===
// "initializing"`, or the shared ontology summary reports republication. When
// status also reports `indexError`, it shows that failure instead.
// Startup readiness stays true during later indexing, so it cannot describe
// whether the current index snapshot is available.
export function IndexReadyBanner({
  status,
  onStatusChange,
  rebuilding = false,
}: IndexReadyBannerProps = {}) {
  const controlled = status !== undefined;
  const [fetched, setFetched] = useState<StatusResponse | null>(null);
  const shown = controlled ? status : fetched;
  const state = shown?.indexState ?? null;
  // A startup index that failed (one this build cannot read, say) never
  // becomes ready, so it reads as a failure, not as endless preparation.
  const failure = state === "initializing" ? shown?.indexError : undefined;

  useEffect(() => {
    let cancelled = false;
    let source: EventSource | null = null;
    // Apply a reply only if no newer reply has landed, so a stale reply cannot
    // undo a newer one and a failed newer refresh does not discard an older one.
    let issuedRefresh = 0;
    let appliedRefresh = 0;

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
      const ticket = ++issuedRefresh;
      getStatus()
        .then((status) => {
          if (cancelled || ticket < appliedRefresh) return;
          appliedRefresh = ticket;
          const nextState = status.indexState ?? "ready";

          if (!controlled) setFetched(status);
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

  if (state !== "initializing" && !rebuilding) return null;

  if (failure) {
    return (
      <div
        className="index-ready-banner index-ready-banner--failed"
        role="alert"
        data-testid="index-ready-banner"
      >
        <span className="index-ready-banner__label">Index unavailable</span>
        <span className="index-ready-banner__hint">{failure}</span>
      </div>
    );
  }

  return (
    <div
      className="index-ready-banner"
      role="status"
      aria-live="polite"
      data-testid="index-ready-banner"
    >
      <span className="index-ready-banner__spinner" aria-hidden="true" />
      <span className="index-ready-banner__label">
        {state === "initializing" ? "Preparing index…" : "Updating index…"}
      </span>
      {state === "initializing" && (
        <span className="index-ready-banner__hint">
          First-launch reads will retry automatically.
        </span>
      )}
    </div>
  );
}
