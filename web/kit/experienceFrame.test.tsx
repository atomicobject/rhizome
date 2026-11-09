import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen } from "@testing-library/react";
import { useState, useSyncExternalStore, type ComponentType } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  CLOSE_PAGE_MESSAGE,
  LOCATION_ACCEPTED_MESSAGE,
  LOCATION_MESSAGE,
  LOCATION_RENDERED_MESSAGE,
  PANEL_TAB_ID,
  PHASE_MESSAGE,
  RESUME_MESSAGE,
  SUSPEND_MESSAGE,
  type HostMessage,
  type ViewMessage,
} from "../src/lib/customViewMessages";
import { VAULT_EVENT_MESSAGE } from "../src/lib/customViewMessages";
import { graphQLRequestBody, jsonReply, withFakeFetch } from "../src/test/fakeFetch";
import { useGraphQL } from "./index";
import type { ViewContext } from "../src/views/context";
import type { ViewParamDeclaration, ViewParams } from "../src/views/params";
import { useActionQuery } from "./actions";
import { shareResponse } from "./coverage";
import {
  ExperiencePages,
  RETAINED_PAGES,
  createExperienceFrame,
  markReady,
  type ExperienceFrame,
} from "./experienceFrame";
import { connectFreshness } from "./freshness";
import { createFrameGeneration, type FrameGeneration } from "./generation";
import { useViewPreference } from "./preferences";
import type { ViewModule } from "./startView";
import {
  getViewInvocation,
  useViewActive,
  useViewConfiguration,
  useViewContext,
} from "./viewContext";
import { getViewParams, useViewLocation, useViewParams } from "./viewParams";

const http = withFakeFetch();

let posted: ViewMessage[];

let frame: ExperienceFrame;

let generation: FrameGeneration;

let stops: Array<() => void>;

let modules: Record<string, () => Promise<ViewModule>>;

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

const viewModule = (View: ComponentType): (() => Promise<ViewModule>) => {
  const module: Promise<ViewModule> = Promise.resolve({ default: () => <View /> });

  return () => module;
};

function start(
  views: Record<string, ComponentType>,
  loaders: Record<string, () => Promise<ViewModule>> = {},
) {
  for (const [id, view] of Object.entries(views)) modules[id] = viewModule(view);

  Object.assign(modules, loaders);

  generation = createFrameGeneration();
  frame = createExperienceFrame({
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

  stops.push(connectFreshness(client, { hosted: true, generation }), frame.stop, generation.stop);
  render(
    <QueryClientProvider client={client}>
      <ExperiencePages frame={frame} />
    </QueryClientProvider>,
  );

  return client;
}

const host = (data: HostMessage) =>
  act(async () => {
    window.dispatchEvent(
      new MessageEvent("message", { source: window, origin: window.location.origin, data }),
    );
    await Promise.resolve();
  });

type Location = {
  view: string;
  context?: ViewContext;
  params?: ViewParams;
  declarations?: ViewParamDeclaration[];
  generation?: string;
  configuration?: { [key: string]: string };
};

const locate = (tabId: string, navigationId: string, location: Location) =>
  host({
    type: LOCATION_MESSAGE,
    tabId,
    navigationId,
    params: location.params ?? {},
    generation: location.generation,
    invocation: {
      view: { id: location.view, name: location.view, origin: "repository", experienceKey: "x" },
      context: location.context ?? { kind: "standalone" },
      configuration: location.configuration ?? {},
      params: location.declarations ?? [],
      vaultKey: "test-vault",
      entry: location.view,
    },
  });

const record = (path: string): ViewContext => ({
  kind: "node",
  type: "Spec",
  ref: { notePath: path, kind: "NOTE" },
});

const sent = (type: string) => posted.filter((message) => message.type === type);

function Subject() {
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

describe("per-page bindings", () => {
  it("keeps each hidden page on its own location when it re-renders", async () => {
    const tick = { value: 0, listeners: new Set<() => void>() };

    function Ticking() {
      useSyncExternalStore(
        (listener) => {
          tick.listeners.add(listener);

          return () => tick.listeners.delete(listener);
        },
        () => tick.value,
      );

      return <Subject />;
    }

    start({ record: Ticking });
    await locate("tab-1", "n-1", { view: "record", context: record("a.md") });
    await locate("tab-2", "n-2", { view: "record", context: record("b.md") });
    await act(async () => {
      tick.value += 1;

      for (const listener of tick.listeners) listener();
    });

    expect(screen.getByTestId("page-tab-1").textContent).toContain("a.md");
    expect(screen.getByTestId("page-tab-2").textContent).toContain("b.md");
    // Imperative getters read the page on screen.
    expect(getViewInvocation().context).toEqual(record("b.md"));
  });

  it("answers every location and reports its paint and phases with the page's navigation", async () => {
    function Ready() {
      markReady();

      return <Subject />;
    }

    start({ record: Ready });
    await locate("tab-1", "n-1", { view: "record", context: record("a.md") });
    await act(() => new Promise((resolve) => requestAnimationFrame(resolve)));

    expect(sent(LOCATION_ACCEPTED_MESSAGE)).toEqual([
      { type: LOCATION_ACCEPTED_MESSAGE, tabId: "tab-1", navigationId: "n-1" },
    ]);
    expect(sent(LOCATION_RENDERED_MESSAGE)).toMatchObject([
      { tabId: "tab-1", navigationId: "n-1" },
    ]);
    expect(
      sent(PHASE_MESSAGE).map((message) => message.type === PHASE_MESSAGE && message.phase),
    ).toEqual(expect.arrayContaining(["first-render", "data-ready"]));
  });

  it("drops a slow page's module when its tab moves on before it loads", async () => {
    let finish: (module: ViewModule) => void = () => undefined;
    const slow = new Promise<ViewModule>((resolve) => (finish = resolve));

    start({ fast: Subject }, { slow: () => slow });
    await locate("tab-1", "n-1", { view: "slow" });
    await locate("tab-1", "n-2", { view: "fast", context: record("b.md") });
    await act(async () => finish({ default: () => <p>slow page</p> }));
    await act(() => new Promise((resolve) => requestAnimationFrame(resolve)));

    expect(screen.queryByText("slow page")).toBeNull();
    expect(screen.getByTestId("page-tab-1").textContent).toContain("b.md");
    expect(
      sent(LOCATION_RENDERED_MESSAGE).map(
        (message) => message.type === LOCATION_RENDERED_MESSAGE && message.navigationId,
      ),
    ).toEqual(["n-2"]);
  });
});

describe("activation and replacement", () => {
  const declarations: ViewParamDeclaration[] = [
    { name: "tab", kind: "string", default: "facts" },
    { name: "record", kind: "string", identity: true },
  ];

  function Counter() {
    const [count, setCount] = useState(0);
    const params = useViewParams();

    return (
      <button type="button" onClick={() => setCount(count + 1)}>
        {count} {String(params.tab)} {String(params.record ?? "")}
      </button>
    );
  }

  it("updates a page in place for a UI param and replaces it for an identity param", async () => {
    start({ list: Counter });
    await locate("tab-1", "n-1", { view: "list", declarations, params: { record: "a" } });
    await act(async () => screen.getByRole("button").click());
    expect(screen.getByRole("button").textContent).toBe("1 facts a");

    await locate("tab-1", "n-1", {
      view: "list",
      declarations,
      params: { record: "a", tab: "body" },
    });
    expect(screen.getByRole("button").textContent).toBe("1 body a");
    expect(getViewParams()).toEqual({ record: "a", tab: "body" });

    await locate("tab-1", "n-2", { view: "list", declarations, params: { record: "b" } });
    expect(screen.getByRole("button").textContent).toBe("0 facts b");
  });

  it("resets the error boundary when a page is replaced", async () => {
    function Fragile() {
      const context = useViewContext();

      if (context.kind === "node" && context.ref.notePath === "broken.md") throw new Error("boom");

      return <Subject />;
    }

    vi.spyOn(console, "error").mockImplementation(() => undefined);
    start({ record: Fragile });
    await locate("tab-1", "n-1", { view: "record", context: record("broken.md") });
    expect(screen.getByRole("alert").textContent).toContain("boom");

    await locate("tab-1", "n-2", { view: "record", context: record("fine.md") });
    expect(screen.queryByRole("alert")).toBeNull();
    expect(screen.getByTestId("page-tab-1").textContent).toContain("fine.md");
  });

  it("saves a page's scroll when it is hidden and restores it when shown", async () => {
    const scrollTo = vi.spyOn(window, "scrollTo").mockImplementation(() => undefined);
    start({ record: Subject });
    await locate("tab-1", "n-1", { view: "record", context: record("a.md") });
    document.documentElement.scrollTop = 420;
    await locate("tab-2", "n-2", { view: "record", context: record("b.md") });
    expect(scrollTo).toHaveBeenLastCalledWith(0, 0);

    document.documentElement.scrollTop = 30;
    await locate("tab-1", "n-1", { view: "record", context: record("a.md") });
    expect(scrollTo).toHaveBeenLastCalledWith(0, 420);
    await locate("tab-2", "n-2", { view: "record", context: record("b.md") });
    expect(scrollTo).toHaveBeenLastCalledWith(0, 30);
  });

  it(`keeps at most ${RETAINED_PAGES} hidden pages and drops the least recently shown`, async () => {
    start({ record: Subject });

    for (let tab = 0; tab <= RETAINED_PAGES + 1; tab += 1)
      await locate(`tab-${tab}`, `n-${tab}`, { view: "record", context: record(`${tab}.md`) });

    expect(frame.state().pages).toHaveLength(RETAINED_PAGES + 1);
    expect(screen.queryByTestId("page-tab-0")).toBeNull();
    expect(screen.getByTestId("page-tab-1")).toBeTruthy();

    await host({ type: CLOSE_PAGE_MESSAGE, tabId: "tab-1" });
    expect(screen.queryByTestId("page-tab-1")).toBeNull();
  });
});

describe("hidden pages and suspended frames", () => {
  function Polling({ label }: { label: string }) {
    const active = useViewActive();
    const { data } = useActionQuery<string>("module/status", { label });

    return (
      <p data-testid={label}>
        {active ? "active" : "idle"} {data ?? ""}
      </p>
    );
  }

  it("stop action polling while hidden or suspended, and report useViewActive", async () => {
    vi.useFakeTimers({
      toFake: ["setTimeout", "setInterval", "clearTimeout", "clearInterval", "Date"],
    });
    http.on("POST", "/api/v1/actions/module/status", () =>
      jsonReply({ status: "ok", result: "up" }),
    );
    start({ a: () => <Polling label="a" />, b: () => <Polling label="b" /> });
    await locate("tab-a", "n-a", { view: "a" });
    await act(() => vi.advanceTimersByTimeAsync(10));
    await locate("tab-b", "n-b", { view: "b" });
    await act(() => vi.advanceTimersByTimeAsync(10));

    expect(screen.getByTestId("a").textContent).toContain("idle");
    expect(screen.getByTestId("b").textContent).toContain("active");
    const before = http.count("POST", "/api/v1/actions/module/status");
    await act(() => vi.advanceTimersByTimeAsync(15_000));
    // Only the shown page polled.
    expect(http.count("POST", "/api/v1/actions/module/status")).toBe(before + 1);

    await host({ type: SUSPEND_MESSAGE });
    expect(screen.getByTestId("b").textContent).toContain("idle");
    await act(() => vi.advanceTimersByTimeAsync(45_000));
    expect(http.count("POST", "/api/v1/actions/module/status")).toBe(before + 1);

    await host({ type: RESUME_MESSAGE, generation: "G1" });
    expect(screen.getByTestId("b").textContent).toContain("active");
    expect(generation.current()).toBe("G1");
  });
});

describe("reads in retained pages", () => {
  function Reading({ label }: { label: string }) {
    const { data } = useGraphQL<{ label: string }>(`{ label(name: "${label}") }`);

    return <p data-testid={label}>{data?.label ?? "…"}</p>;
  }

  const reads = (label: string) =>
    http
      .requests("POST", "/api/v1/graphql")
      .filter((request) => (graphQLRequestBody(request).query ?? "").includes(`"${label}"`)).length;

  it("refetch a hidden page only when it is shown behind the generation, and never at the same one", async () => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout", "Date"] });
    let generationNow = "G0";

    http.on("POST", "/api/v1/graphql", () =>
      jsonReply({ data: { label: generationNow }, extensions: { generation: generationNow } }),
    );
    start({ a: () => <Reading label="a" />, b: () => <Reading label="b" /> });
    await locate("tab-a", "n-a", { view: "a", generation: "G0" });
    await act(() => vi.advanceTimersByTimeAsync(200));
    await locate("tab-b", "n-b", { view: "b" });
    await act(() => vi.advanceTimersByTimeAsync(200));
    expect([reads("a"), reads("b")]).toEqual([1, 1]);

    // Back to a retained tab at the same generation: no read.
    await locate("tab-a", "n-a", { view: "a", generation: "G0" });
    await act(() => vi.advanceTimersByTimeAsync(200));
    expect([reads("a"), reads("b")]).toEqual([1, 1]);

    // A write while A shows refreshes A once; hidden B waits.
    generationNow = "G1";
    await host({ type: VAULT_EVENT_MESSAGE, event: "node.changed", generation: "G1" });
    await act(() => vi.advanceTimersByTimeAsync(200));
    expect([reads("a"), reads("b")]).toEqual([2, 1]);
    expect(screen.getByTestId("a").textContent).toBe("G1");

    // Shown again, B is behind and refetches once.
    await locate("tab-b", "n-b", { view: "b", generation: "G1" });
    await act(() => vi.advanceTimersByTimeAsync(200));
    expect([reads("a"), reads("b")]).toEqual([2, 2]);
    expect(screen.getByTestId("b").textContent).toBe("G1");
  });
});

describe("the panel page", () => {
  it("keeps one component across subjects with a new navigation each time", async () => {
    function Panel() {
      const [notes, setNotes] = useState("");
      const context = useViewContext();

      return (
        <label>
          {context.kind === "node" ? context.ref.notePath : context.kind}
          <input value={notes} onChange={(event) => setNotes(event.target.value)} />
        </label>
      );
    }

    start({ panel: Panel });
    await locate(PANEL_TAB_ID, "p-1", { view: "panel", context: record("a.md") });
    await act(async () => {
      const input = screen.getByRole("textbox");
      const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")?.set;
      setter?.call(input, "kept");
      input.dispatchEvent(new Event("input", { bubbles: true }));
    });

    await locate(PANEL_TAB_ID, "p-2", { view: "panel", context: record("b.md") });
    expect(screen.getByRole("textbox")).toHaveProperty("value", "kept");
    expect(screen.getByText("b.md")).toBeTruthy();
    expect(frame.state().pages[0]?.binding.navigationId).toBe("p-2");
  });

  it("renders a subject-independent read that was in flight across a subject change", async () => {
    let answer: (response: Response) => void = () => undefined;
    http.on(
      "POST",
      "/api/v1/actions/module/tree",
      () => new Promise((resolve) => (answer = resolve)),
    );

    function Panel() {
      const { data } = useActionQuery<string>("module/tree");
      const context = useViewContext();

      return (
        <p>
          {context.kind === "node" ? context.ref.notePath : ""} {data ?? "loading"}
        </p>
      );
    }

    start({ panel: Panel });
    await locate(PANEL_TAB_ID, "p-1", { view: "panel", context: record("a.md") });
    await locate(PANEL_TAB_ID, "p-2", { view: "panel", context: record("b.md") });
    await act(async () => answer(jsonReply({ status: "ok", result: "tree" })));

    expect((await screen.findByText(/tree/)).textContent).toContain("b.md");
  });

  it("scopes preferences to the experience's panel, not to the subject", async () => {
    function Panel() {
      const height = useViewPreference("height", {
        defaultValue: 200,
        validate: (value): value is number => Number.isFinite(value),
      });

      return <p>{String(height.value)}</p>;
    }

    start({ panel: Panel });
    await locate(PANEL_TAB_ID, "p-1", { view: "panel", context: record("a.md") });
    await locate(PANEL_TAB_ID, "p-2", { view: "panel", context: record("b.md") });
    await screen.findByText("200");

    const scopes = http
      .requests("GET", "/api/v1/view-preferences")
      .map((request) => JSON.parse(request.query.get("scope") ?? "{}"));

    expect(scopes.length).toBeGreaterThan(0);
    expect(new Set(scopes.map((scope) => JSON.stringify(scope)))).toEqual(
      new Set([JSON.stringify({ viewId: "panel", context: { kind: "standalone" } })]),
    );
  });
});
