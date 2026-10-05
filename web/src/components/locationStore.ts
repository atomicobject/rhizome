import { useSyncExternalStore } from "react";

function subscribe(onStoreChange: () => void) {
  window.addEventListener("popstate", onStoreChange);

  return () => window.removeEventListener("popstate", onStoreChange);
}

function snapshot() {
  return `${window.location.pathname}${window.location.search}${window.location.hash}`;
}

export function useLocationSnapshot() {
  const value = useSyncExternalStore(subscribe, snapshot, snapshot);
  const url = new URL(value, window.location.origin);

  return {
    pathname: url.pathname,
    search: url.search,
    hash: url.hash,
  };
}

export function notifyLocationChange() {
  window.dispatchEvent(new PopStateEvent("popstate"));
}
