import { useEffect, useMemo, useRef, useState } from "react";

import type { EditReadLifecycle } from "../staging/stagedQuery";
import type {
  NodeRef,
  OntologyEditOp,
  OntologyEditSessionResponse,
  ValidationScope,
  ViewCatalogEntry,
} from "../api/types";
import {
  EDIT_SESSION_MESSAGE,
  VAULT_EVENT_MESSAGE,
  VIEW_PARAMS_MESSAGE,
  type HostMessage,
} from "../lib/customViewMessages";
import { subscribeVaultEvents } from "../query/vaultEvents";
import { useLocationSnapshot } from "./locationStore";
import { NOTES_ROOT_PATH } from "./notesRoute";
import type { OpenMode } from "./useNoteTabs";

import { customViewHref, type OpenNodeOptions, type ViewContext } from "../views/context";
import {
  readViewParams,
  viewParamsIdentity,
  withViewParams,
  type ViewParamValue,
} from "../views/params";
import {
  isServiceRequest,
  pushViewParams,
  runServiceRequest,
  viewRequest,
  type OpenSearchRequest,
} from "../views/viewBridge";

type Props = {
  context?: ViewContext;
  /** An independent mount of the view in this context; its preferences are separate. */
  preferenceSlot?: string;
  onOpenNode?: (ref: NodeRef, options: OpenNodeOptions) => void;
  onOpenView?: (id: string, context: ViewContext, params?: Record<string, ViewParamValue>) => void;
  view: ViewCatalogEntry;
  session: OntologyEditSessionResponse | null;
  readLifecycle?: EditReadLifecycle;
  onOpenNote: (path: string, mode?: OpenMode) => void;
  onStageOps: (ops: OntologyEditOp[]) => Promise<void>;
  onOpenIssues?: (scope?: ValidationScope) => void;
  onSelectCollection?: (name: string) => void;
  onOpenSearch?: (search: OpenSearchRequest) => void;
};

const isNotesPath = (pathname: string) =>
  pathname === NOTES_ROOT_PATH || pathname.startsWith(`${NOTES_ROOT_PATH}/`);

/** The longest a loading view stays hidden behind the one it replaces. */
const SWAP_LIMIT_MS = 1000;

/** Calls `done` after `view` paints twice, so its first render is on screen. */
function afterPaint(view: Window | null, done: () => void) {
  if (!view?.requestAnimationFrame) return done();
  view.requestAnimationFrame(() => view.requestAnimationFrame(done));
}

/**
 * Frames a custom view (SPEC-0105). The view is repository code served from
 * this origin at /views/<id>, the same URL that works as a standalone page, so
 * the frame adds no sandbox: isolation here is only for styles and React.
 *
 * The frame also bridges the workspace edit session: the view stages edits
 * into it and reads through it, and the workspace saves them. It forwards
 * every vault stream event the host receives, so the view opens no stream.
 */
export function CustomViewFrame({
  view,
  context = { kind: "standalone" },
  preferenceSlot,
  session,
  readLifecycle,
  onOpenNote,
  onOpenNode,
  onOpenView,
  onStageOps,
  onOpenIssues,
  onSelectCollection,
  onOpenSearch,
}: Props) {
  const frameRef = useRef<HTMLIFrameElement>(null);
  const framesRef = useRef<HTMLDivElement>(null);

  // Every mounted frame: while a view loads, the one on screen still runs and
  // may come back on screen, so it keeps getting replies, sessions, and events.
  const mountedViews = () =>
    [...(framesRef.current?.querySelectorAll("iframe") ?? [])].flatMap((frame) =>
      frame.contentWindow ? [frame.contentWindow] : [],
    );

  const postAll = (message: HostMessage) => {
    for (const target of mountedViews()) target.postMessage(message, window.location.origin);
  };

  // Declared params live on the workspace URL (SPEC-0110). Another app route
  // leaves the last workspace query in place, so a kept-alive frame is untouched.
  const declarations = useMemo(() => view.definition.params ?? [], [view.definition.params]);
  const browser = useLocationSnapshot();
  const searchRef = useRef(browser.search);

  if (isNotesPath(browser.pathname)) searchRef.current = browser.search;
  const search = searchRef.current;
  const params = useMemo(() => readViewParams(declarations, search), [declarations, search]);
  const paramsRef = useRef(params);
  paramsRef.current = params;
  const paramsJSON = JSON.stringify(params);

  const href = customViewHref(view.id, context, { hosted: true });
  const base = preferenceSlot ? `${href}&${new URLSearchParams({ preferenceSlot })}` : href;
  // Identity params are part of the invocation, so changing one loads a new
  // frame; a frame keeps the src it first loaded and gets UI params as messages.
  const key = `${base}#${viewParamsIdentity(declarations, params)}`;
  const mounted = useRef({ key: "", src: "" });

  if (mounted.current.key !== key) mounted.current = { key, src: withViewParams(base, params) };

  // A new frame loads hidden behind the frame on screen and replaces it once
  // its view has drawn, so switching views never flashes an empty frame. The
  // newest frame gets the ref; requests come from any mounted frame.
  const current = { key, src: mounted.current.src, title: view.name };
  const [shown, setShown] = useState(current);
  const currentRef = useRef(current);
  currentRef.current = current;

  const reveal = (frame: typeof current) => {
    if (currentRef.current.key === frame.key)
      setShown((previous) => (previous.key === frame.key ? previous : frame));
  };

  const revealRef = useRef(reveal);
  revealRef.current = reveal;

  useEffect(() => {
    if (shown.key === key) return;
    const limit = window.setTimeout(() => setShown(currentRef.current), SWAP_LIMIT_MS);

    return () => window.clearTimeout(limit);
  }, [shown.key, key]);

  const sessionRef = useRef({ session, readLifecycle });
  sessionRef.current = { session, readLifecycle };

  useEffect(() => {
    const services = {
      onOpenNote,
      onOpenNode,
      onOpenView,
      onStageOps,
      onOpenIssues,
      onSelectCollection,
      onOpenSearch,
    };

    const onMessage = (event: MessageEvent) => {
      if (event.origin !== window.location.origin) return;
      const source = mountedViews().find((view) => view === event.source);

      if (!source) return;
      const request = viewRequest(event.data, declarations);

      if (!request) return;

      if (request.kind === "rendered" && source === frameRef.current?.contentWindow) {
        const frame = currentRef.current;
        afterPaint(source, () => revealRef.current(frame));
      }

      // Only the newest frame's params match the declarations.
      if (request.kind === "params" && source === frameRef.current?.contentWindow)
        pushViewParams(declarations, request.patch);

      if (isServiceRequest(request))
        runServiceRequest(request, { ...services, ...sessionRef.current }, (message) => {
          if (mountedViews().includes(source)) source.postMessage(message, window.location.origin);
        });
    };

    window.addEventListener("message", onMessage);

    return () => window.removeEventListener("message", onMessage);
  }, [
    declarations,
    onOpenNote,
    onOpenNode,
    onOpenView,
    onStageOps,
    onOpenIssues,
    onSelectCollection,
    onOpenSearch,
  ]);

  useEffect(
    () => subscribeVaultEvents((event) => postAll({ type: VAULT_EVENT_MESSAGE, ...event })),
    [],
  );

  useEffect(() => {
    postAll({ type: EDIT_SESSION_MESSAGE, session, readLifecycle });
  }, [session, readLifecycle]);

  // A UI param reaches the newest frame as a message; a new frame reads it
  // from its src.
  useEffect(() => {
    if (declarations.length === 0) return;
    const message: HostMessage = { type: VIEW_PARAMS_MESSAGE, params: JSON.parse(paramsJSON) };
    frameRef.current?.contentWindow?.postMessage(message, window.location.origin);
  }, [declarations, paramsJSON]);

  return (
    <div ref={framesRef} className="custom-view-frames">
      {(shown.key === key ? [current] : [shown, current]).map((frame) => {
        const incoming = frame.key !== shown.key;

        return (
          <iframe
            key={frame.key}
            ref={frame.key === key ? frameRef : undefined}
            className="custom-view-frame"
            data-incoming={incoming || undefined}
            aria-hidden={incoming || undefined}
            title={frame.title}
            src={frame.src}
            onLoad={(event) => {
              const loaded = event.currentTarget.contentWindow;

              // Params that changed while the newest frame loaded.
              if (declarations.length > 0 && frame.key === currentRef.current.key) {
                const message: HostMessage = {
                  type: VIEW_PARAMS_MESSAGE,
                  params: paramsRef.current,
                };

                loaded?.postMessage(message, window.location.origin);
              }

              if (!incoming) return;

              // A kit view still starting announces its first render;
              // any other page has drawn what it will by its load.
              if (loaded?.document.documentElement?.dataset.rhizomeView === "starting") return;
              afterPaint(loaded, () => reveal(frame));
            }}
          />
        );
      })}
    </div>
  );
}
