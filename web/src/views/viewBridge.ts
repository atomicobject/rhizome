// The host half of the view message protocol (lib/customViewMessages.ts):
// parsing what a framed view sends and carrying out the requests every host
// frame shares. CustomViewFrame, the experience frame layer, and the panel
// slot each own their frames and route messages here.
import type { NodeRef, OntologyEditOp } from "../api/types";
import {
  EDIT_SESSION_HELLO_MESSAGE,
  EDIT_SESSION_MESSAGE,
  FRAME_READY_MESSAGE,
  LOCATION_ACCEPTED_MESSAGE,
  LOCATION_RENDERED_MESSAGE,
  OPEN_COLLECTION_MESSAGE,
  OPEN_ISSUES_MESSAGE,
  OPEN_NODE_MESSAGE,
  OPEN_NOTE_MESSAGE,
  OPEN_SEARCH_MESSAGE,
  OPEN_VIEW_MESSAGE,
  PARAMS_CHANGED_MESSAGE,
  PHASE_MESSAGE,
  STAGE_OPS_MESSAGE,
  STAGE_RESULT_MESSAGE,
  VIEW_RENDERED_MESSAGE,
  type ExperiencePhase,
  type HostMessage,
  type IssueScope,
  type PageStamp,
} from "../lib/customViewMessages";
import { isPseudoType } from "../components/notesRoute";
import { notifyLocationChange } from "../components/locationStore";
import {
  parseViewContext,
  parseViewNodeRef,
  type OpenNodeOptions,
  type ViewContext,
} from "./context";
import {
  applyViewParamsPatch,
  isViewParamValues,
  viewParamsPatchError,
  type ViewParamDeclaration,
  type ViewParamsPatch,
  type ViewParamValue,
} from "./params";
import type { ViewServices } from "./ViewHost";

export type OpenSearchRequest = { folder?: string; query?: string };

export type ViewRequest =
  | { kind: "open-node"; ref: NodeRef; options: OpenNodeOptions }
  | {
      kind: "open-view";
      id: string;
      context: ViewContext;
      params?: Record<string, ViewParamValue>;
    }
  | { kind: "params"; patch: ViewParamsPatch }
  | { kind: "open-note"; path: string; mode: "activate" | "beside" }
  | { kind: "hello" }
  | { kind: "rendered" }
  | { kind: "stage"; requestId: string; ops: OntologyEditOp[] }
  | { kind: "open-issues"; scope?: IssueScope }
  | { kind: "open-collection"; name: string }
  | { kind: "open-search"; search: OpenSearchRequest }
  | { kind: "frame-ready" }
  | { kind: "location-accepted"; tabId: string; navigationId: string }
  | { kind: "location-rendered"; tabId: string; navigationId: string; at: number }
  | { kind: "phase"; phase: ExperiencePhase; at: number };

/** The requests every host carries out the same way; the rest are frame plumbing. */
export type ServiceRequest = Extract<
  ViewRequest,
  {
    kind:
      | "open-node"
      | "open-view"
      | "open-note"
      | "hello"
      | "stage"
      | "open-issues"
      | "open-collection"
      | "open-search";
  }
>;

export type BridgeServices = Pick<
  ViewServices,
  | "session"
  | "readLifecycle"
  | "onOpenNote"
  | "onOpenNode"
  | "onOpenView"
  | "onStageOps"
  | "onOpenIssues"
  | "onSelectCollection"
  | "onOpenSearch"
>;

const PHASES: readonly string[] = ["shell-start", "module-loaded", "first-render", "data-ready"];

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

const nonblank = (value: unknown): value is string =>
  typeof value === "string" && value.trim() !== "";

/** The page a message names: its tab and navigation, sent by experience and panel frames only. */
export function messageStamp(data: unknown): PageStamp {
  if (typeof data !== "object" || data === null) return {};
  const stamp: PageStamp = {};

  if ("tabId" in data && typeof data.tabId === "string") stamp.tabId = data.tabId;

  if ("navigationId" in data && typeof data.navigationId === "string")
    stamp.navigationId = data.navigationId;

  return stamp;
}

/** A message from a view, checked; null (with a console warning) when it is malformed. */
export function viewRequest(
  data: unknown,
  params: readonly ViewParamDeclaration[],
): ViewRequest | null {
  if (typeof data !== "object" || data === null || !("type" in data))
    return rejectViewRequest(undefined, "Expected a message object with a type");

  const reject = (reason: string) => rejectViewRequest(data.type, reason);

  if (data.type === EDIT_SESSION_HELLO_MESSAGE) return { kind: "hello" };

  if (data.type === VIEW_RENDERED_MESSAGE) return { kind: "rendered" };

  if (data.type === FRAME_READY_MESSAGE) return { kind: "frame-ready" };

  if (data.type === LOCATION_ACCEPTED_MESSAGE || data.type === LOCATION_RENDERED_MESSAGE) {
    const { tabId, navigationId } = messageStamp(data);

    if (!tabId || !navigationId) return reject("Expected tabId and navigationId strings");

    if (data.type === LOCATION_ACCEPTED_MESSAGE)
      return { kind: "location-accepted", tabId, navigationId };
    const at = "at" in data && typeof data.at === "number" ? data.at : NaN;

    return { kind: "location-rendered", tabId, navigationId, at };
  }

  if (data.type === PHASE_MESSAGE) {
    if (!("phase" in data) || typeof data.phase !== "string" || !PHASES.includes(data.phase))
      return reject("Invalid phase");

    if (!("at" in data) || typeof data.at !== "number")
      return reject("Invalid at: expected a number");

    // SAFETY: checked against PHASES above.
    return { kind: "phase", phase: data.phase as ExperiencePhase, at: data.at };
  }

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
    if (!("name" in data) || !nonblank(data.name))
      return reject("Invalid name: expected a nonblank collection name");

    if (isPseudoType(data.name)) return reject("Invalid name: reserved collection name");

    return { kind: "open-collection", name: data.name };
  }

  if (data.type === OPEN_SEARCH_MESSAGE) {
    const folder = "folder" in data ? data.folder : undefined;
    const query = "query" in data ? data.query : undefined;

    if (folder !== undefined && !nonblank(folder))
      return reject("Invalid folder: expected a nonblank string");

    if (query !== undefined && typeof query !== "string")
      return reject("Invalid query: expected a string");

    // A search with neither lists nothing.
    if (folder === undefined && !nonblank(query))
      return reject("Expected a folder or a nonblank query");

    const search: OpenSearchRequest = {};

    if (folder !== undefined) search.folder = folder;

    if (query !== undefined) search.query = query;

    return { kind: "open-search", search };
  }

  return reject("Unsupported message type");
}
/* oxlint-enable anti-slop/no-runtime-typeof, anti-slop/no-unknown-parameters */

export function isServiceRequest(request: ViewRequest): request is ServiceRequest {
  return (
    request.kind === "open-node" ||
    request.kind === "open-view" ||
    request.kind === "open-note" ||
    request.kind === "hello" ||
    request.kind === "stage" ||
    request.kind === "open-issues" ||
    request.kind === "open-collection" ||
    request.kind === "open-search"
  );
}

/**
 * Carry out a request through the host's services. `reply` posts to the frame
 * that sent it, if that frame is still mounted; every stage request gets a
 * result, so no edit is lost silently.
 */
export function runServiceRequest(
  request: ServiceRequest,
  services: BridgeServices,
  reply: (message: HostMessage) => void,
) {
  switch (request.kind) {
    case "open-note":
      services.onOpenNote(request.path, request.mode);

      return;
    case "open-node":
      services.onOpenNode?.(request.ref, request.options);

      return;
    case "open-view":
      // Params are passed only when the view sent them.
      if (request.params) services.onOpenView?.(request.id, request.context, request.params);
      else services.onOpenView?.(request.id, request.context);

      return;
    case "hello":
      reply({
        type: EDIT_SESSION_MESSAGE,
        session: services.session,
        readLifecycle: services.readLifecycle,
      });

      return;
    case "stage":
      void stageAndReply(services.onStageOps, request, reply);

      return;
    case "open-issues":
      services.onOpenIssues?.(request.scope);

      return;
    case "open-collection":
      services.onSelectCollection?.(request.name);

      return;
    case "open-search":
      services.onOpenSearch?.(request.search);

      return;
    default: {
      const exhaustive: never = request;

      return exhaustive;
    }
  }
}

async function stageAndReply(
  stage: BridgeServices["onStageOps"],
  { requestId, ops }: { requestId: string; ops: OntologyEditOp[] },
  reply: (message: HostMessage) => void,
) {
  try {
    await stage(ops);
    reply({ type: STAGE_RESULT_MESSAGE, requestId });
  } catch (error) {
    const message = error instanceof Error ? error.message : "Failed to stage edits";
    reply({ type: STAGE_RESULT_MESSAGE, requestId, error: message });
  }
}

/** The answer to a stage request from a page whose navigation is no longer current. */
export function staleStageResult(requestId: string): HostMessage {
  return {
    type: STAGE_RESULT_MESSAGE,
    requestId,
    error: "This page changed before its edit arrived, so the edit was not staged. Try again.",
  };
}

/**
 * A params change from a view becomes a workspace history entry; the location
 * change then updates the view in place or replaces it.
 */
export function pushViewParams(params: readonly ViewParamDeclaration[], patch: ViewParamsPatch) {
  const url = new URL(window.location.href);
  applyViewParamsPatch(params, url.searchParams, patch);
  const next = `${url.pathname}${url.search}${url.hash}`;

  if (next === `${window.location.pathname}${window.location.search}${window.location.hash}`)
    return;
  window.history.pushState({}, "", next);
  notifyLocationChange();
}
