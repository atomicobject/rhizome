// The host frame layer (SPEC-0136 R2): experience frames live here, over the
// workbench content area, and are never moved in the DOM, because moving an
// iframe reloads it. A tab shows an experience page through a placeholder; the
// layer lays the experience's frame over the active placeholder.
import {
  createContext,
  use,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  useSyncExternalStore,
  type CSSProperties,
  type ReactNode,
} from "react";

import { getStatus, getViewInvocation } from "../api/client";
import type { OntologyEditSessionResponse, ViewCatalogEntry } from "../api/types";
import { subscribeVaultEvents } from "../query/vaultEvents";
import type { EditReadLifecycle } from "../staging/stagedQuery";
import type { ViewContext } from "../views/context";
import { readViewParams } from "../views/params";
import type { BridgeServices } from "../views/viewBridge";
import { useLocationSnapshot } from "./locationStore";
import { ExperienceFrames, type ExperienceShowing } from "./experienceFrames";
import { NOTES_ROOT_PATH } from "./notesRoute";

const LayerContext = createContext<ExperienceFrames | null>(null);

/** The workbench tab a subtree renders in, so an experience page knows its tab. */
export const ExperienceTabContext = createContext<string | null>(null);

/** The frame layer and tab an experience page would show in, or null outside the workbench. */
export function useExperienceLayer() {
  const frames = use(LayerContext);
  const tabId = use(ExperienceTabContext);

  return frames && tabId !== null ? { frames, tabId } : null;
}

const fetchInvocation = async (showing: ExperienceShowing) =>
  getViewInvocation(showing.viewId, showing.context, { preferenceSlot: showing.preferenceSlot });

const fetchGeneration = async () => (await getStatus()).generation;

/** The store a window's experience frames and sidebar panel frames share. */
export const newExperienceFrames = () => new ExperienceFrames({ fetchInvocation, fetchGeneration });

type LayerProps = {
  children: ReactNode;
  session: OntologyEditSessionResponse | null;
  readLifecycle?: EditReadLifecycle;
  /** Every open tab; a page whose tab closed is dropped. */
  openTabIds: ReadonlySet<string>;
  /** Frames belong to one vault; another vault starts none of them again. */
  vaultKey: string | null;
  /** The store, when the shell shares it with the sidebar's panel slot. */
  frames?: ExperienceFrames;
};

export function ExperienceFrameLayer({
  children,
  session,
  readLifecycle,
  openTabIds,
  vaultKey,
  frames: shared,
}: LayerProps) {
  const [own] = useState(() => shared ?? newExperienceFrames());
  const frames = shared ?? own;
  const vaultRef = useRef(vaultKey);

  useEffect(() => {
    if (vaultRef.current === vaultKey) return;
    vaultRef.current = vaultKey;
    frames.clear();
  }, [frames, vaultKey]);

  useEffect(() => frames.setSession({ session, readLifecycle }), [frames, session, readLifecycle]);
  useEffect(() => frames.retainTabs(openTabIds), [frames, openTabIds]);
  useEffect(() => subscribeVaultEvents((event) => frames.vaultEvent(event)), [frames]);

  useEffect(() => {
    const onMessage = (event: MessageEvent) => frames.receive(event);
    window.addEventListener("message", onMessage);

    return () => {
      window.removeEventListener("message", onMessage);
      frames.clear();
    };
  }, [frames]);

  return (
    <LayerContext value={frames}>
      {children}
      <FrameLayer frames={frames} />
    </LayerContext>
  );
}

type Rect = Pick<CSSProperties, "top" | "left" | "width" | "height">;

/** The active placeholder's box in the layer, kept current through resizes and scrolling. */
function useCoverRect(layer: HTMLElement | null, placeholder: HTMLElement | null) {
  const [rect, setRect] = useState<Rect | null>(null);

  useLayoutEffect(() => {
    if (!layer || !placeholder) return;
    let frame = 0;

    const measure = () => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(() => {
        const outer = layer.getBoundingClientRect();
        const inner = placeholder.getBoundingClientRect();

        setRect((current) => {
          const next = {
            top: inner.top - outer.top,
            left: inner.left - outer.left,
            width: inner.width,
            height: inner.height,
          };

          return current &&
            current.top === next.top &&
            current.left === next.left &&
            current.width === next.width &&
            current.height === next.height
            ? current
            : next;
        });
      });
    };

    measure();
    // A placeholder moves when anything around it resizes, so every box from
    // it up to the layer's container is watched.
    const observer = new ResizeObserver(measure);

    for (let node: HTMLElement | null = placeholder; node && node !== layer.parentElement;) {
      observer.observe(node);
      node = node.parentElement;
    }

    observer.observe(layer);
    window.addEventListener("resize", measure);
    document.addEventListener("scroll", measure, true);

    return () => {
      cancelAnimationFrame(frame);
      observer.disconnect();
      window.removeEventListener("resize", measure);
      document.removeEventListener("scroll", measure, true);
    };
  }, [layer, placeholder]);

  return rect;
}

/**
 * The frames of the workbench, or (`panel`) of the sidebar's panel slot, laid
 * over the active placeholder. `onFrameLoad` sees each frame's new document.
 */
export function FrameLayer({
  frames,
  panel = false,
  onFrameLoad,
}: {
  frames: ExperienceFrames;
  panel?: boolean;
  onFrameLoad?: (frame: HTMLIFrameElement) => void;
}) {
  const all = useSyncExternalStore(frames.subscribe, frames.views);
  const views = useMemo(() => all.filter((each) => each.panel === panel), [all, panel]);
  const [layer, setLayer] = useState<HTMLDivElement | null>(null);

  const placeholder = views.some((each) => each.visible || each.starting)
    ? frames.activePlaceholder(panel)
    : null;

  const rect = useCoverRect(layer, placeholder);

  return (
    <div ref={setLayer} className={panel ? "panel-frame-layer" : "experience-frame-layer"}>
      {views.map((view) => {
        const placed = (view.visible || view.starting) && rect;

        return (
          <iframe
            key={view.id}
            // A frame's window outlives its ref's detach within a commit, in
            // which a placeholder may already show the frame.
            ref={(element) => {
              if (element) frames.attach(view.id, element.contentWindow);
            }}
            className={panel ? "panel-frame" : "experience-frame"}
            data-experience={view.experienceKey}
            data-shown={view.visible || undefined}
            title={view.title}
            src={view.src}
            onLoad={onFrameLoad && ((event) => onFrameLoad(event.currentTarget))}
            style={placed ? rect : undefined}
            aria-hidden={view.visible ? undefined : true}
            inert={view.visible ? undefined : true}
          />
        );
      })}
    </div>
  );
}

const isNotesPath = (pathname: string) =>
  pathname === NOTES_ROOT_PATH || pathname.startsWith(`${NOTES_ROOT_PATH}/`);

type PageProps = {
  layer: NonNullable<ReturnType<typeof useExperienceLayer>>;
  /** The experience's sidebar panel: its page takes the declared defaults and never the workspace URL. */
  panel?: boolean;
  experienceKey: string;
  view: ViewCatalogEntry;
  context: ViewContext;
  preferenceSlot?: string;
  services: Omit<BridgeServices, "session" | "readLifecycle">;
};

/**
 * Where an experience page shows in its tab. The page itself renders in its
 * experience's frame, which the layer lays over this box.
 */
export function ExperiencePlaceholder({
  layer: { frames, tabId },
  panel = false,
  experienceKey,
  view,
  context,
  preferenceSlot,
  services,
}: PageProps) {
  const box = useRef<HTMLDivElement>(null);
  const declarations = useMemo(() => view.definition.params ?? [], [view.definition.params]);

  // Declared params live on the workspace URL (SPEC-0110); another app route
  // leaves the last workspace query in place.
  const browser = useLocationSnapshot();
  const searchRef = useRef(browser.search);

  if (isNotesPath(browser.pathname)) searchRef.current = browser.search;
  const paramsJSON = JSON.stringify(readViewParams(declarations, panel ? "" : searchRef.current));
  const contextJSON = JSON.stringify(context);

  // Messages reach the services this tab last rendered with.
  const servicesRef = useRef(services);
  servicesRef.current = services;

  const stableServices = useMemo<PageProps["services"]>(
    () => ({
      onOpenNote: (...args) => servicesRef.current.onOpenNote(...args),
      onOpenNode: (...args) => servicesRef.current.onOpenNode?.(...args),
      onOpenView: (...args) => servicesRef.current.onOpenView?.(...args),
      onStageOps: (...args) => servicesRef.current.onStageOps(...args),
      onOpenIssues: (...args) => servicesRef.current.onOpenIssues?.(...args),
      onSelectCollection: (...args) => servicesRef.current.onSelectCollection?.(...args),
      onOpenSearch: (...args) => servicesRef.current.onOpenSearch?.(...args),
    }),
    [],
  );

  useLayoutEffect(() => {
    frames.show(
      {
        tabId,
        experienceKey,
        viewId: view.id,
        viewName: view.name,
        declarations,
        // SAFETY: serialized from a ViewContext in this render.
        context: JSON.parse(contextJSON) as ViewContext,
        preferenceSlot,
        params: JSON.parse(paramsJSON),
        services: stableServices,
        ownsLocation: !panel && isNotesPath(window.location.pathname),
        panel,
      },
      box.current,
    );
  }, [
    contextJSON,
    declarations,
    experienceKey,
    frames,
    panel,
    paramsJSON,
    preferenceSlot,
    stableServices,
    tabId,
    view.id,
    view.name,
  ]);

  useLayoutEffect(() => () => frames.hide(tabId), [frames, tabId]);

  const shown = useSyncExternalStore(
    frames.subscribe,
    () =>
      frames.views().find((each) => each.experienceKey === experienceKey && each.panel === panel)
        ?.visible ?? false,
  );

  const error = useSyncExternalStore(frames.subscribe, () => frames.error(tabId));

  return (
    <div
      ref={box}
      className="custom-view-frames experience-placeholder"
      data-experience={experienceKey}
      aria-busy={shown ? undefined : true}
    >
      {error ? (
        <p role="alert" className="experience-placeholder__status">
          {view.name} could not open. {error}
        </p>
      ) : shown ? null : (
        <p role="status" className="experience-placeholder__status delayed-status">
          Loading view…
        </p>
      )}
    </div>
  );
}
