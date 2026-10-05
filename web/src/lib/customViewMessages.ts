// Messages between a framed custom view (`@rhizome/kit`) and the Notes frame
// that hosts it. Both sides accept them only from the same origin, and the
// host only from the frame it created.
import type { EditReadLifecycle } from "../staging/stagedQuery";
import type { NodeRef, OntologyEditOp, OntologyEditSessionResponse } from "../api/types";
import type { OpenNodeOptions, ViewContext } from "../views/context";

export const OPEN_NODE_MESSAGE = "rhizome:open-node";

export const OPEN_VIEW_MESSAGE = "rhizome:open-view";

/** View → host: open a note in the workspace. `{ path, beside? }` */
export const OPEN_NOTE_MESSAGE = "rhizome:open-note";

/** View → host: ask for the current edit session; the host answers with EDIT_SESSION_MESSAGE. */
export const EDIT_SESSION_HELLO_MESSAGE = "rhizome:edit-session-hello";

/** Host → view: the workspace edit session changed. `{ session }`, null when none. */
export const EDIT_SESSION_MESSAGE = "rhizome:edit-session";

/** View → host: stage ops in the workspace edit session. `{ requestId, ops }` */
export const STAGE_OPS_MESSAGE = "rhizome:stage-ops";

/** Host → view: the stage request settled. `{ requestId, error? }` */
export const STAGE_RESULT_MESSAGE = "rhizome:stage-result";

/** Host → view: an event from the host's vault stream. `{ event, data? }`, data as the raw payload text. */
export const VAULT_EVENT_MESSAGE = "rhizome:event";

/** View → host: open the issues panel, optionally scoped. `{ scope? }` */
export const OPEN_ISSUES_MESSAGE = "rhizome:open-issues";

/** View → host: open a type or interface collection. `{ name }` */
export const OPEN_COLLECTION_MESSAGE = "rhizome:open-collection";

/** The validation scopes a view may open the issues panel at. */
export type IssueScope = { kind: "type" | "interface" | "note"; key: string };

export type ViewMessage =
  | ({ type: typeof OPEN_NODE_MESSAGE; ref: NodeRef } & OpenNodeOptions)
  | { type: typeof OPEN_VIEW_MESSAGE; id: string; context: ViewContext }
  | { type: typeof OPEN_NOTE_MESSAGE; path: string; beside?: boolean }
  | { type: typeof EDIT_SESSION_HELLO_MESSAGE }
  | { type: typeof STAGE_OPS_MESSAGE; requestId: string; ops: OntologyEditOp[] }
  | { type: typeof OPEN_ISSUES_MESSAGE; scope?: IssueScope }
  | { type: typeof OPEN_COLLECTION_MESSAGE; name: string };

export type HostMessage =
  | {
      type: typeof EDIT_SESSION_MESSAGE;
      session: OntologyEditSessionResponse | null;
      readLifecycle?: EditReadLifecycle;
    }
  | { type: typeof STAGE_RESULT_MESSAGE; requestId: string; error?: string }
  | { type: typeof VAULT_EVENT_MESSAGE; event: string; data?: string };
