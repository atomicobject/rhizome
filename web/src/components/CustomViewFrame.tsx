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
  EDIT_SESSION_HELLO_MESSAGE,
  EDIT_SESSION_MESSAGE,
  OPEN_COLLECTION_MESSAGE,
  OPEN_ISSUES_MESSAGE,
  OPEN_NOTE_MESSAGE,
  OPEN_NODE_MESSAGE,
  OPEN_SEARCH_MESSAGE,
  OPEN_VIEW_MESSAGE,
  PARAMS_CHANGED_MESSAGE,
  STAGE_OPS_MESSAGE,
  STAGE_RESULT_MESSAGE,
  VAULT_EVENT_MESSAGE,
  VIEW_PARAMS_MESSAGE,
  VIEW_RENDERED_MESSAGE,
  type HostMessage,
  type IssueScope,
} from "../lib/customViewMessages";
import { subscribeVaultEvents } from "../query/vaultEvents";
import { notifyLocationChange, useLocationSnapshot } from "./locationStore";
import { NOTES_ROOT_PATH, isPseudoType } from "./notesRoute";
import type { OpenMode } from "./useNoteTabs";

import {
  customViewHref,
  parseViewContext,
  parseViewNodeRef,
  type OpenNodeOptions,
  type ViewContext,
} from "../views/context";
import {
  applyViewParamsPatch,
  isViewParamValues,
  readViewParams,
  viewParamsIdentity,
  viewParamsPatchError,
  withViewParams,
  type ViewParamDeclaration,
  type ViewParamsPatch,
  type ViewParamValue,
} from "../views/params";

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
  onOpenSearch?: (search: { folder: string; query?: string }) => void;
};

type ViewRequest =
  | { kind: "open-node"; ref: NodeRef; options: OpenNodeOptions }
  | {
      kind: "open-view";
      id: string;
      context: ViewContext;
      params?: Record<string, ViewParamValue>;
    }
  | { kind: "params"; patch: ViewParamsPatch }
  | { kind: "open-note"; path: string; mode: OpenMode }
  | { kind: "hello" }
  | { kind: "rendered" }
  | { kind: "stage"; requestId: string; ops: OntologyEditOp[] }
  | { kind: "open-issues"; scope?: IssueScope }
  | { kind: "open-collection"; name: string }
  | { kind: "open-search"; search: { folder: string; query?: string } };

/* oxlint-disable anti-slop/no-runtime-typeof, anti-slop/no-unknown-parameters -- These helpers validate values at the postMessage trust boundary. */
function issueScope(value: unknown): IssueScope | null {
  if (typeof value !== "object" || value === null) return null;

  if (!("kind" in value) || !("key" in value)) return null;

  if (value.kind !== "type" && value.kind !== "interface" && value.kind !== "note") return null;

  if (typeof value.key !== "string" || !value.key.trim()) return null;

  return { kind: value.kind, key: value.key };
}

function rejectViewRequest(type: unknown, reason: string): null {
  console.warn(
    `[Rhizome custom view] Rejected ${typeof type === "string" ? type : "<missing or invalid type>"}`,
    reason,
  );

  return null;
}

function viewRequest(data: unknown, params: readonly ViewParamDeclaration[]): ViewRequest | null {
  if (typeof data !== "object" || data === null || !("type" in data))
    return rejectViewRequest(undefined, "Expected a message object with a type");

  const reject = (reason: string) => rejectViewRequest(data.type, reason);

  if (data.type === EDIT_SESSION_HELLO_MESSAGE) return { kind: "hello" };

  if (data.type === VIEW_RENDERED_MESSAGE) return { kind: "rendered" };

  if (data.type === OPEN_NOTE_MESSAGE) {
    if (!("path" in data) || typeof data.path !== "string" || data.path === "")
      return reject("Invalid path: expected a nonempty string");
    const beside = "beside" in data && data.beside === true;

    return { kind: "open-note", path: data.path, mode: beside ? "beside" : "activate" };
  }

  if (data.type === OPEN_NODE_MESSAGE) {
    const ref = parseViewNodeRef("ref" in data ? data.ref : undefined);

    if (!ref)
      return reject("Invalid ref: expected nonblank notePath/kind and valid optional fields");

    if ("view" in data && typeof data.view !== "string")
      return reject("Invalid view: expected a string");

    return {
      kind: "open-node",
      ref,
      options: {
        view: "view" in data && typeof data.view === "string" ? data.view : undefined,
        beside: "beside" in data && data.beside === true,
      },
    };
  }

  if (data.type === OPEN_VIEW_MESSAGE) {
    if (!("id" in data) || typeof data.id !== "string" || !data.id)
      return reject("Invalid id: expected a nonempty string");

    const context = parseViewContext("context" in data ? data.context : undefined);

    if (!context)
      return reject("Invalid context: expected a concrete view subject with a valid ref for nodes");

    if (!("params" in data) || data.params === undefined)
      return { kind: "open-view", id: data.id, context };

    if (!isViewParamValues(data.params))
      return reject("Invalid params: expected string, integer, or boolean values");

    return { kind: "open-view", id: data.id, context, params: data.params };
  }

  if (data.type === PARAMS_CHANGED_MESSAGE) {
    const patch = "params" in data ? data.params : undefined;
    const error = viewParamsPatchError(params, patch);

    // SAFETY: viewParamsPatchError accepted every key and value.
    return error
      ? reject(`Invalid params: ${error}`)
      : { kind: "params", patch: patch as ViewParamsPatch };
  }

  if (data.type === STAGE_OPS_MESSAGE) {
    if (!("requestId" in data) || typeof data.requestId !== "string")
      return reject("Invalid requestId: expected a string");

    // The server validates each op; the host only checks the envelope.
    if (!("ops" in data) || !Array.isArray(data.ops) || data.ops.length === 0)
      return reject("Invalid ops: expected a nonempty array");

    if (!data.ops.every((op) => typeof op === "object" && op !== null))
      return reject("Invalid ops: expected objects");

    return { kind: "stage", requestId: data.requestId, ops: data.ops };
  }

  if (data.type === OPEN_ISSUES_MESSAGE) {
    if (!("scope" in data) || data.scope === undefined) return { kind: "open-issues" };
    const scope = issueScope(data.scope);

    return scope
      ? { kind: "open-issues", scope }
      : reject("Invalid scope: expected type/interface/note and a nonblank key");
  }

  if (data.type === OPEN_COLLECTION_MESSAGE) {
    if (!("name" in data) || typeof data.name !== "string" || !data.name.trim())
      return reject("Invalid name: expected a nonblank collection name");

    if (isPseudoType(data.name)) return reject("Invalid name: reserved collection name");

    return { kind: "open-collection", name: data.name };
  }

  if (data.type === OPEN_SEARCH_MESSAGE) {
    if (!("folder" in data) || typeof data.folder !== "string" || !data.folder.trim())
      return reject("Invalid folder: expected a nonblank string");

    if (!("query" in data) || data.query === undefined)
      return { kind: "open-search", search: { folder: data.folder } };

    if (typeof data.query !== "string") return reject("Invalid query: expected a string");

    return { kind: "open-search", search: { folder: data.folder, query: data.query } };
  }

  return reject("Unsupported message type");
}
/* oxlint-enable anti-slop/no-runtime-typeof, anti-slop/no-unknown-parameters */

const isNotesPath = (pathname: string) =>
  pathname === NOTES_ROOT_PATH || pathname.startsWith(`${NOTES_ROOT_PATH}/`);

// A params change from the frame becomes a workspace history entry; the
// location change then remounts the frame or updates it in place.
function pushViewParams(params: readonly ViewParamDeclaration[], patch: ViewParamsPatch) {
  const url = new URL(window.location.href);
  applyViewParamsPatch(params, url.searchParams, patch);
  const next = `${url.pathname}${url.search}${url.hash}`;

  if (next === `${window.location.pathname}${window.location.search}${window.location.hash}`)
    return;
  window.history.pushState({}, "", next);
  notifyLocationChange();
}

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
    const reply = (target: Window, message: HostMessage) => {
      if (mountedViews().includes(target)) target.postMessage(message, window.location.origin);
    };

    const stage = async (target: Window, requestId: string, ops: OntologyEditOp[]) => {
      try {
        await onStageOps(ops);
        reply(target, { type: STAGE_RESULT_MESSAGE, requestId });
      } catch (error) {
        const message = error instanceof Error ? error.message : "Failed to stage edits";
        reply(target, { type: STAGE_RESULT_MESSAGE, requestId, error: message });
      }
    };

    const onMessage = (event: MessageEvent) => {
      if (event.origin !== window.location.origin) return;
      const source = mountedViews().find((view) => view === event.source);

      if (!source) return;
      const request = viewRequest(event.data, declarations);

      if (request?.kind === "rendered" && source === frameRef.current?.contentWindow) {
        const frame = currentRef.current;
        afterPaint(source, () => revealRef.current(frame));
      }

      if (request?.kind === "open-note") onOpenNote(request.path, request.mode);

      if (request?.kind === "open-node") onOpenNode?.(request.ref, request.options);

      // Params are passed only when the view sent them.
      if (request?.kind === "open-view" && request.params)
        onOpenView?.(request.id, request.context, request.params);
      else if (request?.kind === "open-view") onOpenView?.(request.id, request.context);

      // Only the newest frame's params match the declarations.
      if (request?.kind === "params" && source === frameRef.current?.contentWindow)
        pushViewParams(declarations, request.patch);

      if (request?.kind === "hello")
        reply(source, { type: EDIT_SESSION_MESSAGE, ...sessionRef.current });

      if (request?.kind === "stage") void stage(source, request.requestId, request.ops);

      if (request?.kind === "open-issues") onOpenIssues?.(request.scope);

      if (request?.kind === "open-collection") onSelectCollection?.(request.name);

      if (request?.kind === "open-search") onOpenSearch?.(request.search);
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
