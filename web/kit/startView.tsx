// How a mounted view starts. Its reload path comes first, before its module
// loads, so a module that fails to load, mounts itself, or loads while its
// folder changes still reloads once its source changes.
import { QueryClient } from "@tanstack/react-query";
import { StrictMode, type ComponentType, type ReactElement } from "react";

import type { ViewModuleProps } from "../src/views/context";
import { connectFreshness } from "./freshness";
import type { ViewOrigin } from "./types";
import { getViewInvocation, type KitViewInvocation } from "./viewContext";
import { ViewError, ViewRoot } from "./viewRoot";

export type ViewConfig = {
  id: string;
  name: string;
  origin?: ViewOrigin;
  stamp?: string;
  stampValue?: string;
  check?: string;
  /** The definition's folder under .rhizome/views, named by views.changed. Absent for bundled views. */
  folder?: string;
};

export type ViewModule = { default?: ComponentType<ViewModuleProps & { view: ViewConfig }> };

export type ViewStartup = {
  /** True when the workspace frames the view and forwards its events. */
  hosted: boolean;
  /** Called at most once, with the view or the reason it could not start. */
  render: (element: ReactElement) => void;
  reload: () => void;
  queryClient?: QueryClient;
};

const STAMP_READ_TIMEOUT_MS = 10_000;

async function readStamp(stampURL: string, signal: AbortSignal) {
  if (signal.aborted) return null;

  const controller = new AbortController();
  const abort = () => controller.abort(signal.reason);
  signal.addEventListener("abort", abort, { once: true });
  const timer = window.setTimeout(() => controller.abort(), STAMP_READ_TIMEOUT_MS);
  let finishAbort = () => {};

  const aborted = new Promise<null>((resolve) => {
    finishAbort = () => resolve(null);
    controller.signal.addEventListener("abort", finishAbort, { once: true });
  });

  try {
    const read = async () => {
      const response = await fetch(stampURL, { signal: controller.signal });

      if (!response.ok || controller.signal.aborted) return null;

      const stamp = await response.text();

      return controller.signal.aborted ? null : stamp;
    };

    // Aborting frees the serial check even if a body reader settles late.
    return await Promise.race([read(), aborted]);
  } catch {
    return null;
  } finally {
    window.clearTimeout(timer);
    signal.removeEventListener("abort", abort);
    controller.signal.removeEventListener("abort", finishAbort);
  }
}

// The served baseline catches edits made before the listener or first poll.
// Hosted checks retain it; standalone polls adopt each successful stamp.
function watchStamp(
  stampURL: string,
  baseline: string | undefined,
  hosted: boolean,
  reload: () => void,
) {
  const controller = new AbortController();
  let last = baseline;
  let pending = false;
  let checkAgain = false;

  const check = async () => {
    if (controller.signal.aborted) return;

    if (pending) {
      // A read started before reconnect cannot cover events missed after it.
      // Coalesce reconnects into a check that starts after the latest one.
      if (hosted) checkAgain = true;

      return;
    }

    pending = true;

    try {
      do {
        checkAgain = false;
        const next = await readStamp(stampURL, controller.signal);

        if (controller.signal.aborted) return;

        if (next !== null) {
          if (last !== undefined && next !== last) reload();

          if (!hosted) last = next;
        }
      } while (checkAgain && !controller.signal.aborted);
    } finally {
      pending = false;
    }
  };

  // ponytail: standalone repository pages keep the one-second stamp fallback
  // for views.changed; hidden pages and ticks during a pending read skip it.
  const timer = hosted
    ? undefined
    : window.setInterval(() => {
        if (!document.hidden) void check();
      }, 1000);

  return {
    check,
    stop: () => {
      window.clearInterval(timer);
      controller.abort();
    },
  };
}

const STAMP_PREFIX = "/views/_stamp/";

// The server names a repository view's folder; older shells only carry it in the stamp URL.
function viewFolder(view: ViewConfig) {
  if (view.folder !== undefined) return view.folder;

  return view.stamp?.startsWith(STAMP_PREFIX)
    ? decodeURIComponent(view.stamp.slice(STAMP_PREFIX.length))
    : null;
}

const asError = (cause: unknown) => (cause instanceof Error ? cause : new Error(String(cause)));

// A browser reports a script that failed to transform as a failed import, or as
// a missing export in its importer. The folder check has the transform errors;
// the browser's error still matters when the cause is elsewhere (a runtime
// throw, a mistyped import path), so both are shown.
async function loadFailureDetail(error: Error, checkURL: string | undefined) {
  if (!checkURL) return error.message;

  const response = await fetch(checkURL).catch(() => null);
  const diagnostics = response?.ok ? (await response.text()).trim() : "";

  return diagnostics ? `${diagnostics}\n\nBrowser error: ${error.message}` : error.message;
}

/**
 * Start a view: listen for freshness and source changes, then load its module
 * and render its default export inside the kit's providers. A module without a
 * default export is left to mount itself. Bundled views never reload. Returns
 * a function that stops listening.
 */
export async function startView(
  load: () => Promise<ViewModule>,
  view: ViewConfig,
  startup: ViewStartup,
) {
  const { hosted, render, reload } = startup;

  const queryClient =
    startup.queryClient ?? new QueryClient({ defaultOptions: { queries: { staleTime: 5_000 } } });

  const { stamp, stampValue } = view;

  const stamps =
    stamp && view.origin !== "bundled" ? watchStamp(stamp, stampValue, hosted, reload) : undefined;

  // views.changed is not replayed, so after a reconnect a hosted view compares
  // its folder with the version it was served.
  const onReconnect =
    hosted && stamps && stampValue !== undefined ? () => void stamps.check() : undefined;

  const stops = [
    connectFreshness(queryClient, {
      origin: view.origin,
      folder: viewFolder(view),
      hosted,
      reload,
      onReconnect,
    }),
  ];

  const stop = () => {
    for (const each of stops) each();
  };

  if (stamps) {
    stops.push(stamps.stop);

    if (hosted && stampValue !== undefined) void stamps.check();
  }

  let invocation: KitViewInvocation;

  try {
    invocation = getViewInvocation();
  } catch (error) {
    render(<ViewError title={`${view.name} could not start`} detail={asError(error).message} />);

    return stop;
  }

  let module: ViewModule;

  try {
    module = await load();
  } catch (error) {
    const detail = await loadFailureDetail(asError(error), view.check);
    render(<ViewError title={`${view.name} failed to load`} detail={detail} />);

    return stop;
  }

  const View = module.default;

  // A module with no default export mounts itself, so the kit renders nothing.
  if (!View) return stop;

  render(
    <StrictMode>
      <ViewRoot queryClient={queryClient}>
        <View view={view} context={invocation.context} configuration={invocation.configuration} />
      </ViewRoot>
    </StrictMode>,
  );

  return stop;
}
