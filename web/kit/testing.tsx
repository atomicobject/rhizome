// Internal test harness for views written against the kit, including the
// bundled group views. It is not in the import map: repositories cannot import
// it yet. It starts a view module through mountView's own startup sequence, with
// a server invocation, faked HTTP, and vault events delivered by the workspace
// when the view is framed or by the page's stream when it is not.
import { QueryClient } from "@tanstack/react-query";
import { act, render } from "@testing-library/react";
import { onTestFinished, vi } from "vitest";

import type { JsonObject } from "../src/api/parse";
import { VAULT_EVENT_MESSAGE } from "../src/lib/customViewMessages";
import { FakeEventSource, installFakeEventSource } from "../src/test/fakeEventSource";
import {
  FakeFetch,
  jsonReply,
  type FakeFetchResponder,
  type JsonValue,
} from "../src/test/fakeFetch";
import type { ViewContext, ViewModuleProps } from "../src/views/context";
import { embedded } from "./editSession";
import { EVENT_COALESCE_MS } from "./freshness";
import { startView, type ViewConfig, type ViewModule } from "./startView";
import type { ViewOrigin } from "./types";
import { clearPreferenceStores } from "../src/viewPreferences/store";
import { preferenceServer } from "../src/viewPreferences/testServer";

/** A JSON body or a responder, keyed by `"METHOD /path"`, e.g. `"GET /api/v1/display-groups"`. */
export type ViewRoutes = Record<string, JsonValue | FakeFetchResponder>;

export type RenderViewOptions = {
  context?: ViewContext;
  configuration?: JsonObject;
  vaultKey?: string;
  preferenceSlot?: string;
  view?: ViewConfig;
  routes?: ViewRoutes;
  /** An already-installed fake, such as the one `withFakeFetch()` returns. */
  http?: FakeFetch;
};

const isResponder = (reply: JsonValue | FakeFetchResponder): reply is FakeFetchResponder =>
  reply instanceof Function;

function installRoutes(http: FakeFetch, routes: ViewRoutes) {
  for (const [route, reply] of Object.entries(routes)) {
    const [method, path] = route.split(" ");

    if (!method || !path) throw new Error(`renderView: route "${route}" is not "METHOD /path"`);
    http.on(method, path, isResponder(reply) ? reply : () => jsonReply(reply));
  }
}

function writeInvocation(invocation: ViewModuleProps & { view: { origin?: ViewOrigin } }) {
  const script = document.createElement("script");
  script.id = "rhizome-view-invocation";
  script.type = "application/json";
  script.textContent = JSON.stringify(invocation);
  document.getElementById(script.id)?.remove();
  document.head.append(script);

  return () => script.remove();
}

/**
 * Start a view module the way mountView does, with its invocation, kit
 * providers, and freshness, and wait for it to render. The view is hosted when
 * the test frames it (see `embedded`). Everything installed is restored when
 * the test finishes, and an unmatched request fails the test.
 */
export async function renderView(load: () => Promise<ViewModule>, options: RenderViewOptions = {}) {
  const view = options.view ?? { id: "test-view", name: "Test view" };
  const context = options.context ?? { kind: "standalone" };
  const hosted = embedded;
  const ownsHttp = !options.http;
  const http = options.http ?? new FakeFetch().install();

  if (ownsHttp) preferenceServer(http);
  const restoreEvents = hosted ? () => undefined : installFakeEventSource();

  const invocation: ViewModuleProps & { view: { origin?: ViewOrigin } } = {
    view: { id: view.id, name: view.name, origin: view.origin },
    context,
    configuration: options.configuration,
    vaultKey: options.vaultKey ?? "test-vault",
  };

  if (options.preferenceSlot) invocation.preferenceSlot = options.preferenceSlot;
  const removeInvocation = writeInvocation(invocation);

  installRoutes(http, options.routes ?? {});

  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });

  const reload = vi.fn();
  const result = render(<></>);
  let stopView: () => void = () => undefined;

  onTestFinished(() => {
    result.unmount();
    stopView();
    queryClient.clear();
    clearPreferenceStores();
    removeInvocation();
    restoreEvents();

    if (ownsHttp) http.restore();
  });

  await act(async () => {
    stopView = await startView(load, view, {
      hosted,
      queryClient,
      reload,
      render: (element) => result.rerender(element),
    });
  });

  /**
   * Deliver a vault event the way this view receives it and let the
   * coalesced refresh run. `data` is the raw payload, e.g. `{"folder":"x"}`.
   */
  const emit = async (event: string, data?: string) => {
    await act(async () => {
      if (!hosted) {
        FakeEventSource.latest().emit(data ?? "", event);
      } else {
        window.dispatchEvent(
          new MessageEvent("message", {
            source: window.parent,
            origin: window.location.origin,
            data:
              data === undefined
                ? { type: VAULT_EVENT_MESSAGE, event }
                : { type: VAULT_EVENT_MESSAGE, event, data },
          }),
        );
      }

      await new Promise((resolve) => setTimeout(resolve, EVENT_COALESCE_MS));
    });
  };

  return { ...result, queryClient, http, emit, reload };
}
