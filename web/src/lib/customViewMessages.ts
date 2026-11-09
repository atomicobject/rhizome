// Messages between a framed custom view (`@rhizome/kit`) and the Notes frame
// that hosts it. Both sides accept them only from the same origin, and the
// host only from the frame it created.
//
// An experience frame (SPEC-0136 R2) renders one page per tab. The host moves
// it between pages with LOCATION_MESSAGE, and every message a page sends
// carries its `tabId` and `navigationId` (PageStamp), so the host can reject a
// message from a page whose navigation is no longer current. Frames outside
// experiences send neither.
import type { JsonObject } from "../api/parse";
import type { EditReadLifecycle } from "../staging/stagedQuery";
import type { NodeRef, OntologyEditOp, OntologyEditSessionResponse } from "../api/types";
import type { OpenNodeOptions, ViewContext } from "../views/context";
import type { ViewParams, ViewParamsPatch, ViewParamValue } from "../views/params";

export const OPEN_NODE_MESSAGE = "rhizome:open-node";

/** View → host: open a view. `{ id, context, params? }` */
export const OPEN_VIEW_MESSAGE = "rhizome:open-view";

/** View → host: change declared view params. `{ params }`, null resets one to its default. */
export const PARAMS_CHANGED_MESSAGE = "rhizome:params-changed";

/** Host → view: the view's params after a change that did not remount it. `{ params }` */
export const VIEW_PARAMS_MESSAGE = "rhizome:view-params";

/** View → host: open a note in the workspace. `{ path, beside? }` */
export const OPEN_NOTE_MESSAGE = "rhizome:open-note";

/** A view's first render is on screen, so a host may show it. */
export const VIEW_RENDERED_MESSAGE = "rhizome:view-rendered";

/** View → host: ask for the current edit session; the host answers with EDIT_SESSION_MESSAGE. */
export const EDIT_SESSION_HELLO_MESSAGE = "rhizome:edit-session-hello";

/** Host → view: the workspace edit session changed. `{ session }`, null when none. */
export const EDIT_SESSION_MESSAGE = "rhizome:edit-session";

/**
 * View → host: stage ops in the workspace edit session. `{ requestId, ops }`.
 * The host answers every request with STAGE_RESULT_MESSAGE, with an `error`
 * when it rejects a page's stale navigation ID, so no edit is lost silently.
 */
export const STAGE_OPS_MESSAGE = "rhizome:stage-ops";

/** Host → view: the stage request settled. `{ requestId, error? }` */
export const STAGE_RESULT_MESSAGE = "rhizome:stage-result";

/** Host → view: an event from the host's vault stream. `{ event, data? }`, data as the raw payload text. */
export const VAULT_EVENT_MESSAGE = "rhizome:event";

/** View → host: open the issues panel, optionally scoped. `{ scope? }` */
export const OPEN_ISSUES_MESSAGE = "rhizome:open-issues";

/** View → host: open a type or interface collection. `{ name }` */
export const OPEN_COLLECTION_MESSAGE = "rhizome:open-collection";

/** View → host: open a project search tab, optionally filtered to a folder. `{ folder?, query? }` */
export const OPEN_SEARCH_MESSAGE = "rhizome:open-search";

/**
 * Host → experience frame: show a tab's page at a location (ExperienceLocation).
 * The kit answers LOCATION_ACCEPTED_MESSAGE on receipt; without that answer
 * within LOCATION_ACCEPT_TIMEOUT_MS the host replaces the frame.
 */
export const LOCATION_MESSAGE = "rhizome:location";

/** Experience frame → host: a location arrived. `{ tabId, navigationId }` */
export const LOCATION_ACCEPTED_MESSAGE = "rhizome:location-accepted";

/** Experience frame → host: a location's page painted, for telemetry only. `{ tabId, navigationId, at }` */
export const LOCATION_RENDERED_MESSAGE = "rhizome:location-rendered";

/** Host → experience or panel frame: hidden and inert; every page stops working. `{}` */
export const SUSPEND_MESSAGE = "rhizome:suspend";

/** Host → experience or panel frame: shown again. `{ generation? }`, the current index generation. */
export const RESUME_MESSAGE = "rhizome:resume";

/** Host → experience frame: a tab closed, so its page is dropped. `{ tabId }` */
export const CLOSE_PAGE_MESSAGE = "rhizome:close-page";

/** Host → any frame: the current index generation, after the event stream reconnects. `{ generation }` */
export const GENERATION_MESSAGE = "rhizome:generation";

/** Any frame → host: a startup phase mark (SPEC-0136 R10). `{ phase, at, tabId?, navigationId? }` */
export const PHASE_MESSAGE = "rhizome:phase";

/** The tab ID of a panel frame's one page (SPEC-0136 R6). */
export const PANEL_TAB_ID = "panel";

/** How long the host waits for LOCATION_ACCEPTED_MESSAGE before replacing the frame. */
export const LOCATION_ACCEPT_TIMEOUT_MS = 1000;

/**
 * Startup phases, as epoch milliseconds (`performance.timeOrigin +
 * performance.now()`) so host and frame clocks compare. `shell-start` and
 * `module-loaded` for the shell's own view are frame-wide; the rest name a page.
 */
export type ExperiencePhase = "shell-start" | "module-loaded" | "first-render" | "data-ready";

/** Which page sent a message, in an experience or panel frame. */
export type PageStamp = { tabId?: string; navigationId?: string };

/**
 * One tab's location in an experience frame. A location whose view, context,
 * and identity params match the tab's page updates that page in place and
 * reuses its navigation ID; any other location replaces the page and carries
 * a new navigation ID. The panel's page (`tabId` PANEL_TAB_ID) is keyed by
 * view only, so a new subject updates it in place with a new navigation ID.
 */
export type ExperienceLocation = {
  navigationId: string;
  tabId: string;
  /**
   * The body of `GET /api/v1/views/{id}/invocation` for this view and
   * context, verbatim: `view`, `context`, `configuration`, `params`
   * (declarations), `vaultKey`, `preferenceSlot`, plus `entry`, the module
   * URL the frame imports, and `check`, the folder check URL for load errors.
   */
  invocation: JsonObject;
  /** The tab's view param values. */
  params: ViewParams;
  /** The current index generation; a new frame's first location takes it from the invocation's Rhizome-Generation header. */
  generation?: string;
};

/** The validation scopes a view may open the issues panel at. */
export type IssueScope = { kind: "type" | "interface" | "note"; key: string };

export type ViewMessage = PageStamp &
  (
    | ({ type: typeof OPEN_NODE_MESSAGE; ref: NodeRef } & OpenNodeOptions)
    | {
        type: typeof OPEN_VIEW_MESSAGE;
        id: string;
        context: ViewContext;
        params?: Record<string, ViewParamValue>;
      }
    | { type: typeof PARAMS_CHANGED_MESSAGE; params: ViewParamsPatch }
    | { type: typeof OPEN_NOTE_MESSAGE; path: string; beside?: boolean }
    | { type: typeof EDIT_SESSION_HELLO_MESSAGE }
    | { type: typeof VIEW_RENDERED_MESSAGE }
    | { type: typeof STAGE_OPS_MESSAGE; requestId: string; ops: OntologyEditOp[] }
    | { type: typeof OPEN_ISSUES_MESSAGE; scope?: IssueScope }
    | { type: typeof OPEN_COLLECTION_MESSAGE; name: string }
    | { type: typeof OPEN_SEARCH_MESSAGE; folder?: string; query?: string }
    | { type: typeof LOCATION_ACCEPTED_MESSAGE; tabId: string; navigationId: string }
    | { type: typeof LOCATION_RENDERED_MESSAGE; tabId: string; navigationId: string; at: number }
    | { type: typeof PHASE_MESSAGE; phase: ExperiencePhase; at: number }
  );

export type HostMessage =
  | {
      type: typeof EDIT_SESSION_MESSAGE;
      session: OntologyEditSessionResponse | null;
      readLifecycle?: EditReadLifecycle;
    }
  | { type: typeof STAGE_RESULT_MESSAGE; requestId: string; error?: string }
  | {
      type: typeof VAULT_EVENT_MESSAGE;
      event: string;
      data?: string;
      /** The event's index generation, when the host parsed it; else the kit reads `generation` from `data`. */
      generation?: string;
    }
  | { type: typeof VIEW_PARAMS_MESSAGE; params: ViewParams }
  | ({ type: typeof LOCATION_MESSAGE } & ExperienceLocation)
  | { type: typeof SUSPEND_MESSAGE }
  | { type: typeof RESUME_MESSAGE; generation?: string }
  | { type: typeof CLOSE_PAGE_MESSAGE; tabId: string }
  | { type: typeof GENERATION_MESSAGE; generation: string };
