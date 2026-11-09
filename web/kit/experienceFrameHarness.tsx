// The test harness for experienceFrame*.test.tsx: an experience frame rendered
// in jsdom, a host that posts it messages, and a record of what it sends.
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render } from "@testing-library/react";
import type { ComponentType } from "react";
import { afterEach, beforeEach, vi } from "vitest";

import {
  LOCATION_MESSAGE,
  type HostMessage,
  type ViewMessage,
} from "../src/lib/customViewMessages";
import { withFakeFetch } from "../src/test/fakeFetch";
import type { ViewContext } from "../src/views/context";
import type { ViewParamDeclaration, ViewParams } from "../src/views/params";
import { shareResponse } from "./coverage";
import { ExperiencePages, createExperienceFrame } from "./experienceFrame";
import { connectFreshness } from "./freshness";
import { createFrameGeneration } from "./generation";
import type { ViewModule } from "./startView";
import { useViewConfiguration, useViewContext } from "./viewContext";
import { useViewLocation } from "./viewParams";

export type Location = {
  view: string;
  context?: ViewContext;
  params?: ViewParams;
  declarations?: ViewParamDeclaration[];
  generation?: string;
  configuration?: { [key: string]: string };
};

export const viewModule = (View: ComponentType): (() => Promise<ViewModule>) => {
  const module: Promise<ViewModule> = Promise.resolve({ default: () => <View /> });

  return () => module;
};

export const record = (path: string): ViewContext => ({
  kind: "node",
  type: "Spec",
  ref: { notePath: path, kind: "NOTE" },
});

/** Shows the page's subject, configured title, and tab. */
export function Subject() {
  const context = useViewContext();
  const location = useViewLocation();
  const configuration = useViewConfiguration();
  const label = context.kind === "node" ? context.ref.notePath : context.kind;

  return (
    <p data-testid={`page-${location.tabId}`}>
      {label} {String(configuration?.title ?? "")}
    </p>
  );
}

/** Call once at a test file's top level. */
export function withExperienceFrame() {
  const http = withFakeFetch();
  let posted: ViewMessage[] = [];
  let stops: Array<() => void> = [];
  let modules: Record<string, () => Promise<ViewModule>> = {};

  const harness = {
    http,

    start(
      views: Record<string, ComponentType>,
      loaders: Record<string, () => Promise<ViewModule>> = {},
    ) {
      for (const [id, view] of Object.entries(views)) modules[id] = viewModule(view);

      Object.assign(modules, loaders);

      const generation = createFrameGeneration();

      const frame = createExperienceFrame({
        shellView: { id: "shell", name: "Shell" },
        shellLoad: viewModule(() => null),
        loadFailureDetail: async (error) => error.message,
        generation,
        importModule: (entry) => modules[entry](),
      });

      const client = new QueryClient({
        defaultOptions: {
          queries: { staleTime: Infinity, retry: false, structuralSharing: shareResponse },
        },
      });

      stops.push(
        connectFreshness(client, { hosted: true, generation }),
        frame.stop,
        generation.stop,
      );
      render(
        <QueryClientProvider client={client}>
          <ExperiencePages frame={frame} />
        </QueryClientProvider>,
      );

      return { frame, generation };
    },

    host: (data: HostMessage) =>
      act(async () => {
        window.dispatchEvent(
          new MessageEvent("message", { source: window, origin: window.location.origin, data }),
        );
        await Promise.resolve();
      }),

    locate: (tabId: string, navigationId: string, location: Location) =>
      harness.host({
        type: LOCATION_MESSAGE,
        tabId,
        navigationId,
        params: location.params ?? {},
        generation: location.generation,
        invocation: {
          view: {
            id: location.view,
            name: location.view,
            origin: "repository",
            experienceKey: "x",
          },
          context: location.context ?? { kind: "standalone" },
          configuration: location.configuration ?? {},
          params: location.declarations ?? [],
          vaultKey: "test-vault",
          entry: location.view,
        },
      }),

    sent: (type: string) => posted.filter((message) => message.type === type),
  };

  beforeEach(() => {
    posted = [];
    stops = [];
    modules = {};
    vi.spyOn(window, "postMessage").mockImplementation((message: ViewMessage) => {
      posted.push(message);
    });
  });

  afterEach(() => {
    for (const stop of stops) stop();
    vi.useRealTimers();
  });

  return harness;
}
