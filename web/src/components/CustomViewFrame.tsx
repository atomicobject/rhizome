import { useEffect, useRef } from "react";

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
  OPEN_VIEW_MESSAGE,
  STAGE_OPS_MESSAGE,
  STAGE_RESULT_MESSAGE,
  VAULT_EVENT_MESSAGE,
  type HostMessage,
  type IssueScope,
} from "../lib/customViewMessages";
import { subscribeVaultEvents } from "../query/vaultEvents";
import { isPseudoType } from "./notesRoute";
import type { OpenMode } from "./useNoteTabs";

import {
  customViewHref,
  isViewContext,
  isViewNodeRef,
  type OpenNodeOptions,
  type ViewContext,
} from "../views/context";

type Props = {
  context?: ViewContext;
  /** An independent mount of the view in this context; its preferences are separate. */
  preferenceSlot?: string;
  onOpenNode?: (ref: NodeRef, options: OpenNodeOptions) => void;
  onOpenView?: (id: string, context: ViewContext) => void;
  view: ViewCatalogEntry;
  session: OntologyEditSessionResponse | null;
  readLifecycle?: EditReadLifecycle;
  onOpenNote: (path: string, mode?: OpenMode) => void;
  onStageOps: (ops: OntologyEditOp[]) => Promise<void>;
  onOpenIssues?: (scope?: ValidationScope) => void;
  onSelectCollection?: (name: string) => void;
};

type ViewRequest =
  | { kind: "open-node"; ref: NodeRef; options: OpenNodeOptions }
  | { kind: "open-view"; id: string; context: ViewContext }
  | { kind: "open-note"; path: string; mode: OpenMode }
  | { kind: "hello" }
  | { kind: "stage"; requestId: string; ops: OntologyEditOp[] }
  | { kind: "open-issues"; scope?: IssueScope }
  | { kind: "open-collection"; name: string };

/* oxlint-disable anti-slop/no-runtime-typeof, anti-slop/no-unknown-parameters -- These helpers validate values at the postMessage trust boundary. */
function issueScope(value: unknown): IssueScope | null {
  if (typeof value !== "object" || value === null) return null;

  if (!("kind" in value) || !("key" in value)) return null;

  if (value.kind !== "type" && value.kind !== "interface" && value.kind !== "note") return null;

  if (typeof value.key !== "string" || !value.key.trim()) return null;

  return { kind: value.kind, key: value.key };
}

function viewRequest(data: unknown): ViewRequest | null {
  if (typeof data !== "object" || data === null || !("type" in data)) return null;

  if (data.type === EDIT_SESSION_HELLO_MESSAGE) return { kind: "hello" };

  if (data.type === OPEN_NOTE_MESSAGE) {
    if (!("path" in data) || typeof data.path !== "string" || data.path === "") return null;
    const beside = "beside" in data && data.beside === true;

    return { kind: "open-note", path: data.path, mode: beside ? "beside" : "activate" };
  }

  if (data.type === OPEN_NODE_MESSAGE) {
    if (!("ref" in data) || !isViewNodeRef(data.ref)) return null;

    if ("view" in data && typeof data.view !== "string") return null;

    return {
      kind: "open-node",
      ref: data.ref,
      options: {
        view: "view" in data && typeof data.view === "string" ? data.view : undefined,
        beside: "beside" in data && data.beside === true,
      },
    };
  }

  if (data.type === OPEN_VIEW_MESSAGE) {
    if (!("id" in data) || typeof data.id !== "string" || !data.id) return null;

    if (!("context" in data) || !isViewContext(data.context)) return null;

    return { kind: "open-view", id: data.id, context: data.context };
  }

  if (data.type === STAGE_OPS_MESSAGE) {
    if (!("requestId" in data) || typeof data.requestId !== "string") return null;

    // The server validates each op; the host only checks the envelope.
    if (!("ops" in data) || !Array.isArray(data.ops) || data.ops.length === 0) return null;

    if (!data.ops.every((op) => typeof op === "object" && op !== null)) return null;

    return { kind: "stage", requestId: data.requestId, ops: data.ops };
  }

  if (data.type === OPEN_ISSUES_MESSAGE) {
    if (!("scope" in data) || data.scope === undefined) return { kind: "open-issues" };
    const scope = issueScope(data.scope);

    return scope ? { kind: "open-issues", scope } : null;
  }

  if (data.type === OPEN_COLLECTION_MESSAGE) {
    if (!("name" in data) || typeof data.name !== "string" || !data.name.trim()) return null;

    if (isPseudoType(data.name)) return null;

    return { kind: "open-collection", name: data.name };
  }

  return null;
}
/* oxlint-enable anti-slop/no-runtime-typeof, anti-slop/no-unknown-parameters */

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
}: Props) {
  const frameRef = useRef<HTMLIFrameElement>(null);
  const sessionRef = useRef({ session, readLifecycle });
  sessionRef.current = { session, readLifecycle };

  useEffect(() => {
    const post = (message: HostMessage) =>
      frameRef.current?.contentWindow?.postMessage(message, window.location.origin);

    const stage = async (requestId: string, ops: OntologyEditOp[]) => {
      const target = frameRef.current?.contentWindow;

      const respond = (message: HostMessage) => {
        if (target === frameRef.current?.contentWindow) post(message);
      };

      try {
        await onStageOps(ops);
        respond({ type: STAGE_RESULT_MESSAGE, requestId });
      } catch (error) {
        const message = error instanceof Error ? error.message : "Failed to stage edits";
        respond({ type: STAGE_RESULT_MESSAGE, requestId, error: message });
      }
    };

    const onMessage = (event: MessageEvent) => {
      if (event.origin !== window.location.origin) return;

      if (event.source !== frameRef.current?.contentWindow) return;
      const request = viewRequest(event.data);

      if (request?.kind === "open-note") onOpenNote(request.path, request.mode);

      if (request?.kind === "open-node") onOpenNode?.(request.ref, request.options);

      if (request?.kind === "open-view") onOpenView?.(request.id, request.context);

      if (request?.kind === "hello") post({ type: EDIT_SESSION_MESSAGE, ...sessionRef.current });

      if (request?.kind === "stage") void stage(request.requestId, request.ops);

      if (request?.kind === "open-issues") onOpenIssues?.(request.scope);

      if (request?.kind === "open-collection") onSelectCollection?.(request.name);
    };

    window.addEventListener("message", onMessage);

    return () => window.removeEventListener("message", onMessage);
  }, [onOpenNote, onOpenNode, onOpenView, onStageOps, onOpenIssues, onSelectCollection]);

  useEffect(
    () =>
      subscribeVaultEvents((event) => {
        const message: HostMessage = { type: VAULT_EVENT_MESSAGE, ...event };
        frameRef.current?.contentWindow?.postMessage(message, window.location.origin);
      }),
    [],
  );

  useEffect(() => {
    const message: HostMessage = { type: EDIT_SESSION_MESSAGE, session, readLifecycle };
    frameRef.current?.contentWindow?.postMessage(message, window.location.origin);
  }, [session, readLifecycle]);

  const href = customViewHref(view.id, context, { hosted: true });
  const src = preferenceSlot ? `${href}&${new URLSearchParams({ preferenceSlot })}` : href;

  return (
    <iframe key={src} ref={frameRef} className="custom-view-frame" title={view.name} src={src} />
  );
}
