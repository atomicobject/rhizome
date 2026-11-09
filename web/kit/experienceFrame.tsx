// An experience frame (SPEC-0136 R2, R3): one long-lived document that renders
// an experience's pages, one per workbench tab. The host moves it between
// pages with location messages; the frame keeps recently shown pages mounted
// but hidden, so returning to a tab shows it as it was.
import {
  Activity,
  Suspense,
  use,
  useEffect,
  useLayoutEffect,
  useSyncExternalStore,
  type ReactNode,
} from "react";

import { isPlainObject, isString } from "../src/api/parse";
import {
  CLOSE_PAGE_MESSAGE,
  LOCATION_ACCEPTED_MESSAGE,
  LOCATION_MESSAGE,
  LOCATION_RENDERED_MESSAGE,
  PANEL_TAB_ID,
  PHASE_MESSAGE,
  RESUME_MESSAGE,
  SUSPEND_MESSAGE,
  type ExperiencePhase,
  type ViewMessage,
} from "../src/lib/customViewMessages";
import { isViewParamValues, readViewParams, viewParamsIdentity } from "../src/views/params";
import { frameGeneration, type FrameGeneration } from "./generation";
import {
  PageActiveContext,
  PageBindingContext,
  activePageBinding,
  pageStamp,
  setActivePageBinding,
  type PageBinding,
} from "./pageBinding";
import type { ViewConfig, ViewModule } from "./startView";
import { parseViewInvocation } from "./viewContext";
import { ViewErrorBoundary } from "./viewRoot";

/** P: how many hidden pages a frame keeps mounted; the least recently shown beyond it are dropped. */
export const RETAINED_PAGES = 8;

type Page = {
  tabId: string;
  /** View, context, and identity params; the panel's page is keyed by view only. */
  key: string;
  binding: PageBinding;
  view: ViewConfig;
  load: () => Promise<ViewModule>;
  /** Scroll offset saved when the page was hidden. */
  scroll: number;
  /** When the page was last shown, for retention order. */
  shownAt: number;
  /** Advances with every location for the page, so each gets a rendered message. */
  serial: number;
};

// A location message before its fields are checked; structured clone keeps undefined fields.
type LocationFields = {
  tabId?: unknown;
  navigationId?: unknown;
  invocation?: unknown;
  params?: unknown;
  generation?: unknown;
};

type FrameState = { pages: readonly Page[]; activeTabId: string | null; suspended: boolean };

export type ExperienceFrame = ReturnType<typeof createExperienceFrame>;

const now = () => performance.timeOrigin + performance.now();

const post = (message: ViewMessage) => window.parent.postMessage(message, window.location.origin);

// Phase marks go to a framing workspace only.
let reportPhases = false;

/** Send phase marks to the host from now on; the kit calls it when the workspace frames the page. */
export function reportPhasesToHost() {
  reportPhases = true;
}

export function postPhase(phase: ExperiencePhase, binding: PageBinding | null = null) {
  if (reportPhases) post({ type: PHASE_MESSAGE, phase, at: now(), ...pageStamp(binding) });
}

const readied = new Set<string>();

/**
 * Call when the page's required reads have resolved. Telemetry uses it as the
 * page's data-ready mark; only the first call per navigation counts.
 */
export function markReady() {
  const binding = activePageBinding();
  const key = binding ? `${binding.tabId}\n${binding.navigationId}` : "";

  if (readied.has(key)) return;
  readied.add(key);
  postPhase("data-ready", binding);
}

// `use` needs the same promise on every render, so a failure is made once.
function failing(error: Error) {
  const failure = Promise.reject(error);
  failure.catch(() => undefined);

  return () => failure;
}

const scrollOffset = () => (document.scrollingElement ?? document.documentElement).scrollTop;

type FrameOptions = {
  /** The view the shell loaded, and its loader, so its first page imports nothing twice. */
  shellView: ViewConfig;
  shellLoad: () => Promise<ViewModule>;
  loadFailureDetail: (error: Error, check: string | undefined) => Promise<string>;
  generation?: FrameGeneration;
  /** How an entry URL becomes a module; tests pass their own. */
  importModule?: (entry: string) => Promise<ViewModule>;
};

const importEntry = (entry: string): Promise<ViewModule> => import(/* @vite-ignore */ entry);

export function createExperienceFrame(options: FrameOptions) {
  reportPhasesToHost();
  const generation = options.generation ?? frameGeneration;
  const importModule = options.importModule ?? importEntry;
  const modules = new Map<string, Promise<ViewModule>>();
  const listeners = new Set<() => void>();
  let state: FrameState = { pages: [], activeTabId: null, suspended: false };
  let clock = 0;

  const publish = (next: FrameState) => {
    state = next;
    const active = next.pages.find((page) => page.tabId === next.activeTabId);
    setActivePageBinding(active?.binding ?? null);

    for (const listener of listeners) listener();
  };

  const loader = (view: ViewConfig, entry: string | undefined, check: string | undefined) => {
    const key = view.id === options.shellView.id ? `shell:${view.id}` : entry;

    if (!key) return failing(new Error(`${view.name} has no module entry`));

    return () => {
      let module = modules.get(key);

      if (!module) {
        const load = key === `shell:${view.id}` ? options.shellLoad : () => importModule(key);

        module = load().catch(async (cause: unknown) => {
          const error = cause instanceof Error ? cause : new Error(String(cause));

          throw new Error(await options.loadFailureDetail(error, check));
        });
        modules.set(key, module);
      }

      return module;
    };
  };

  // Save the scroll of the page leaving the screen; it is still the one shown.
  const leaving = (pages: readonly Page[]) =>
    pages.map((page) =>
      page.tabId === state.activeTabId && !state.suspended
        ? { ...page, scroll: scrollOffset() }
        : page,
    );

  const retain = (pages: Page[], activeTabId: string) => {
    const hidden = pages
      .filter((page) => page.tabId !== activeTabId)
      .sort((a, b) => b.shownAt - a.shownAt)
      .slice(0, RETAINED_PAGES);

    return pages.filter((page) => page.tabId === activeTabId || hidden.includes(page));
  };

  const locate = (message: LocationFields) => {
    const { tabId, navigationId } = message;

    if (!isString(tabId) || !isString(navigationId)) return;
    post({ type: LOCATION_ACCEPTED_MESSAGE, tabId, navigationId });

    if (isString(message.generation)) generation.set(message.generation);

    const raw = isPlainObject(message.invocation) ? message.invocation : {};
    let page: Page;

    try {
      const invocation = parseViewInvocation(raw);
      const declarations = invocation.params ?? [];
      const query = new URLSearchParams();

      if (isViewParamValues(message.params)) {
        for (const [name, value] of Object.entries(message.params)) query.set(name, String(value));
      }

      const params = readViewParams(declarations, query);

      const key =
        tabId === PANEL_TAB_ID
          ? invocation.view.id
          : JSON.stringify([
              invocation.view.id,
              invocation.context,
              viewParamsIdentity(declarations, params),
            ]);

      const binding = { tabId, navigationId, invocation, params };
      const existing = state.pages.find((each) => each.tabId === tabId);
      const view: ViewConfig = { ...invocation.view };

      page =
        existing?.key === key
          ? { ...existing, binding, serial: existing.serial + 1 }
          : {
              tabId,
              key,
              binding,
              view,
              load: loader(
                view,
                isString(raw.entry) ? raw.entry : undefined,
                isString(raw.check) ? raw.check : undefined,
              ),
              scroll: 0,
              shownAt: 0,
              serial: 0,
            };
    } catch (error) {
      const view = { id: "", name: "This view" };

      page = {
        tabId,
        key: navigationId,
        binding: {
          tabId,
          navigationId,
          invocation: { view, context: { kind: "standalone" } },
          params: {},
        },
        view,
        load: failing(error instanceof Error ? error : new Error(String(error))),
        scroll: 0,
        shownAt: 0,
        serial: 0,
      };
    }

    const showing = state.activeTabId !== tabId;

    if (showing) {
      clock += 1;
      page = { ...page, shownAt: clock };
    }

    const others = leaving(state.pages).filter((each) => each.tabId !== tabId);
    publish({ ...state, pages: retain([...others, page], tabId), activeTabId: tabId });

    // A page shown again refetches only what is behind the generation.
    if (showing) generation.schedule();
  };

  const onMessage = ({ origin, source, data }: MessageEvent) => {
    if (origin !== window.location.origin || source !== window.parent || !isPlainObject(data))
      return;

    if (data.type === LOCATION_MESSAGE) locate(data);

    if (data.type === SUSPEND_MESSAGE && !state.suspended)
      publish({ ...state, pages: leaving(state.pages), suspended: true });

    if (data.type === RESUME_MESSAGE && state.suspended) {
      if (isString(data.generation)) generation.set(data.generation);
      publish({ ...state, suspended: false });
      generation.schedule();
    }

    if (data.type === CLOSE_PAGE_MESSAGE && isString(data.tabId)) {
      const pages = state.pages.filter((page) => page.tabId !== data.tabId);
      const activeTabId = state.activeTabId === data.tabId ? null : state.activeTabId;
      publish({ ...state, pages, activeTabId });
    }
  };

  window.addEventListener("message", onMessage);

  const rendered = new Set<string>();

  return {
    state: () => state,
    /** True the first time a navigation renders, for its first-render mark. */
    firstRender: (binding: PageBinding) => {
      const key = `${binding.tabId}\n${binding.navigationId}`;
      const first = !rendered.has(key);
      rendered.add(key);

      return first;
    },
    subscribe: (listener: () => void) => {
      listeners.add(listener);

      return () => listeners.delete(listener);
    },
    stop: () => {
      window.removeEventListener("message", onMessage);
      setActivePageBinding(null);
    },
  };
}

function PageView({
  frame,
  page,
  visible,
}: {
  frame: ExperienceFrame;
  page: Page;
  visible: boolean;
}) {
  const module = use(page.load());
  const { binding } = page;

  useEffect(() => {
    if (frame.firstRender(binding)) postPhase("first-render", binding);
  }, [frame, binding]);

  useEffect(() => {
    if (!visible) return;

    const frame = requestAnimationFrame(() =>
      post({
        type: LOCATION_RENDERED_MESSAGE,
        tabId: binding.tabId,
        navigationId: binding.navigationId,
        at: now(),
      }),
    );

    return () => cancelAnimationFrame(frame);
    // One rendered message per location.
    // oxlint-disable-next-line react-hooks/exhaustive-deps
  }, [visible, page.serial]);

  const View = module.default;

  if (!View) throw new Error(`${page.view.name} has no default export`);

  return (
    <View
      view={page.view}
      context={binding.invocation.context}
      configuration={binding.invocation.configuration}
    />
  );
}

/** Renders a frame's pages: the active one visible, the retained ones hidden. */
export function ExperiencePages({ frame }: { frame: ExperienceFrame }): ReactNode {
  const state = useSyncExternalStore(frame.subscribe, frame.state);
  const active = state.pages.find((page) => page.tabId === state.activeTabId);

  // Restore the shown page's scroll once its content is back on screen.
  useLayoutEffect(() => {
    if (active && !state.suspended) window.scrollTo(0, active.scroll);
    // oxlint-disable-next-line react-hooks/exhaustive-deps
  }, [active?.tabId, active?.shownAt, state.suspended]);

  return state.pages.map((page) => {
    const visible = !state.suspended && page.tabId === state.activeTabId;

    return (
      <Activity key={page.tabId} mode={visible ? "visible" : "hidden"}>
        <PageBindingContext value={page.binding}>
          <PageActiveContext value={visible}>
            <ViewErrorBoundary key={page.key}>
              <Suspense fallback={null}>
                <PageView frame={frame} page={page} visible={visible} />
              </Suspense>
            </ViewErrorBoundary>
          </PageActiveContext>
        </PageBindingContext>
      </Activity>
    );
  });
}
