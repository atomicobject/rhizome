import type { QueryClient } from "@tanstack/react-query";

import { inDesktop } from "../components/desktopHost";
import {
  recordError,
  recordFrameGap,
  takeBatch,
  type ClientBatch,
  type RouteKind,
} from "./clientMetrics";
import { observeRefetches } from "./refreshChurn";

export const CLIENT_DIAGNOSTICS_URL = "/api/v1/diagnostics/client";

const FLUSH_INTERVAL_MS = 10_000;

const ROUTE_KINDS: RouteKind[] = ["notes", "ontology", "explorer", "agent", "graphql"];

/** The first path segment when it names an app section; never the path itself. */
export function routeKind(pathname: string): RouteKind {
  const segment = pathname.split("/")[1] ?? "";

  if (segment === "") return "notes";

  return ROUTE_KINDS.find((kind) => kind === segment) ?? "other";
}

// Vite names the entry chunk `index-<content hash>.js`; that hash identifies
// this web build without build-time plumbing. Development serves source files.
function buildVersion(): string {
  return /([\w-]{8})\.js$/.exec(new URL(import.meta.url).pathname)?.[1] ?? "dev";
}

function newSessionID(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(16));

  return Array.from(bytes, (byte) => byte.toString(16).padStart(2, "0")).join("");
}

function send(batch: ClientBatch) {
  // Never retried: a failed or disabled send drops this window.
  void fetch(CLIENT_DIAGNOSTICS_URL, {
    method: "POST",
    keepalive: true,
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(batch),
  }).catch(() => undefined);
}

/**
 * Starts browser measurement for this page load and flushes one batch every ten
 * seconds while visible, plus a final batch when the page is hidden. Returns a
 * stop function for tests.
 */
export function startClientDiagnostics(queryClient: QueryClient): () => void {
  const identity = {
    session: newSessionID(),
    build: buildVersion(),
    surface: inDesktop() ? ("desktop" as const) : ("browser" as const),
  };

  const visible = () => document.visibilityState === "visible";

  const flush = () => {
    const batch = takeBatch({ ...identity, route: routeKind(window.location.pathname) });

    if (batch) send(batch);
  };

  // Frame stalls: gaps between animation frames, measured only while visible
  // because hidden pages throttle or stop animation frames.
  let frame: number | undefined;
  let lastFrame: number | undefined;

  const onFrame = (time: number) => {
    if (lastFrame !== undefined) recordFrameGap(time - lastFrame);
    lastFrame = time;
    frame = requestAnimationFrame(onFrame);
  };

  const startFrames = () => {
    if (frame !== undefined || !visible()) return;
    lastFrame = undefined;
    frame = requestAnimationFrame(onFrame);
  };

  const stopFrames = () => {
    if (frame !== undefined) cancelAnimationFrame(frame);
    frame = undefined;
    lastFrame = undefined;
  };

  const onVisibility = () => {
    if (visible()) {
      startFrames();

      return;
    }

    stopFrames();
    flush();
  };

  // Only the name of an Error is read; messages and stacks never leave here.
  const onError = ({ error }: ErrorEvent) =>
    recordError("errors", error instanceof Error ? error.name : null);

  const onRejection = ({ reason }: PromiseRejectionEvent) =>
    recordError("rejections", reason instanceof Error ? reason.name : null);

  const stopRefetches = observeRefetches(queryClient.getQueryCache());

  const timer = setInterval(() => {
    if (visible()) flush();
  }, FLUSH_INTERVAL_MS);

  document.addEventListener("visibilitychange", onVisibility);
  window.addEventListener("pagehide", flush);
  window.addEventListener("error", onError);
  window.addEventListener("unhandledrejection", onRejection);
  startFrames();

  return () => {
    stopFrames();
    stopRefetches();
    clearInterval(timer);
    document.removeEventListener("visibilitychange", onVisibility);
    window.removeEventListener("pagehide", flush);
    window.removeEventListener("error", onError);
    window.removeEventListener("unhandledrejection", onRejection);
  };
}
