// Where a custom view's writes go and which edit session its reads reflect.
//
// In "workspace" mode (the default) a view framed by the Notes workspace stages
// its writes in the workspace edit session, through the frame, and reads
// through that session; the user saves them with everything else. A view that
// is its own page commits each write instead. "immediate" mode always commits.
import { useQueryClient } from "@tanstack/react-query";
import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useSyncExternalStore,
  type ReactNode,
} from "react";

import {
  commitOntologyEditSession,
  createOntologyEditSession,
  deleteOntologyEditSession,
} from "../src/api/client";
import type { OntologyEditOp } from "../src/api/types";
import {
  EDIT_SESSION_HELLO_MESSAGE,
  EDIT_SESSION_MESSAGE,
  STAGE_OPS_MESSAGE,
  STAGE_RESULT_MESSAGE,
  type ViewMessage,
} from "../src/lib/customViewMessages";
import {
  INITIAL_EDIT_READ_LIFECYCLE,
  type EditReadLifecycle,
  editSessionFingerprint,
  forgetCommittedStagedReads,
  type StagedSession,
} from "../src/staging/stagedQuery";

export type EditMode = "workspace" | "immediate";

/** A node's `ref { notePath fragment nodeId structuralFingerprint }` selection from GraphQL. */
export type NodeTarget = {
  notePath: string;
  fragment?: string | null;
  nodeId?: string | null;
  structuralFingerprint?: string | null;
};

export type FieldValue = string | string[] | null;

// Rhizome frames views from its own origin. A cross-origin parent, such as an
// IDE preview, refuses to show its location, so that page runs standalone.
function framedBySameOrigin() {
  if (window.parent === window) return false;

  try {
    return Boolean(window.parent.location.href);
  } catch {
    return false;
  }
}

/** True when the Notes workspace frames this view; false when it is the page. */
export const embedded = framedBySameOrigin();

export function setFieldOp(target: NodeTarget, field: string, value: FieldValue): OntologyEditOp {
  const fieldValue =
    value === null
      ? { kind: "unset" as const }
      : Array.isArray(value)
        ? { kind: "list" as const, items: value }
        : { kind: "scalar" as const, scalar: value };

  // Embedded nodes are addressed as notePath#fragment, the way the workspace does.
  const path = target.fragment ? `${target.notePath}#${target.fragment}` : target.notePath;

  return {
    kind: "setField",
    path,
    nodeId: target.nodeId ?? undefined,
    structuralFingerprint: target.structuralFingerprint ?? undefined,
    field,
    fieldValue,
  };
}

/** Commit ops in a one-off edit session. Throws when Rhizome does not commit them. */
export async function commitOps(ops: OntologyEditOp[]) {
  const session = await createOntologyEditSession(ops);
  let committed;

  // Sessions have no expiry and reserve their note, so the session is deleted
  // whether the commit succeeded, conflicted, or threw.
  try {
    committed = await commitOntologyEditSession(session.sessionId);
  } finally {
    await deleteOntologyEditSession(session.sessionId).catch(() => null);
  }

  if (committed.outcome === "committed" || committed.outcome === "unchanged") return committed;

  const conflicts = (committed.conflicts ?? []).map((conflict) => conflict.message).join("; ");

  throw new Error(
    `Rhizome did not commit the edit (${committed.outcome ?? "no outcome"})${conflicts ? `: ${conflicts}` : ""}`,
  );
}

type HostState = { answered: boolean; session: StagedSession; readLifecycle: EditReadLifecycle };

const NO_HOST: HostState = {
  answered: false,
  session: null,
  readLifecycle: INITIAL_EDIT_READ_LIFECYCLE,
};

export type WorkspaceHost = {
  state: () => HostState;
  subscribe: (listener: () => void) => () => void;
  stage: (ops: OntologyEditOp[]) => Promise<void>;
};

/**
 * Connects to the Notes frame hosting this view. Stages wait for the host's
 * first answer, so a write made while the view is still loading is staged
 * rather than committed; a host that never answers fails the write.
 */
export function connectWorkspaceHost(parent: Window, answerTimeoutMs = 5_000): WorkspaceHost {
  let state = NO_HOST;
  let requests = 0;
  const listeners = new Set<() => void>();
  const waiting = new Set<() => void>();
  const pending = new Map<string, { resolve: () => void; reject: (error: Error) => void }>();
  const post = (message: ViewMessage) => parent.postMessage(message, window.location.origin);

  window.addEventListener("message", (event) => {
    if (event.origin !== window.location.origin || event.source !== parent) return;
    const { data } = event;

    if (data?.type === EDIT_SESSION_MESSAGE) {
      state = {
        answered: true,
        session: data.session ?? null,
        readLifecycle: data.readLifecycle ?? INITIAL_EDIT_READ_LIFECYCLE,
      };

      for (const resolve of waiting) resolve();
      waiting.clear();

      for (const listener of listeners) listener();
    }

    if (data?.type === STAGE_RESULT_MESSAGE) {
      const request = pending.get(data.requestId);
      pending.delete(data.requestId);

      if (data.error) request?.reject(new Error(data.error));
      else request?.resolve();
    }
  });
  post({ type: EDIT_SESSION_HELLO_MESSAGE });

  const answered = () =>
    new Promise<void>((resolve, reject) => {
      if (state.answered) return resolve();

      const timer = window.setTimeout(() => {
        waiting.delete(done);
        reject(
          new Error(
            'The Rhizome workspace framing this view did not answer, so the edit was not staged. Wrap writes in <EditSession mode="immediate"> to commit them instead.',
          ),
        );
      }, answerTimeoutMs);

      const done = () => {
        window.clearTimeout(timer);
        resolve();
      };

      waiting.add(done);
      // Ask again in case the first request went out before the host listened.
      post({ type: EDIT_SESSION_HELLO_MESSAGE });
    });

  return {
    state: () => state,
    subscribe: (listener) => {
      listeners.add(listener);

      return () => listeners.delete(listener);
    },
    stage: async (ops) => {
      await answered();

      return new Promise((resolve, reject) => {
        requests += 1;
        const requestId = `stage-${requests}`;
        pending.set(requestId, { resolve, reject });
        post({ type: STAGE_OPS_MESSAGE, requestId, ops });
      });
    },
  };
}

let host: WorkspaceHost | undefined;

// One connection per page: the frame's host is the same for every hook.
function workspaceHost() {
  host ??= connectWorkspaceHost(window.parent);

  return host;
}

export type EditWriter = {
  /** Where writes go: staged in the workspace session, or committed one by one. */
  target: "workspace" | "immediate";
  /** The session reads reflect; null reads committed state. */
  session: StagedSession;
  readLifecycle: EditReadLifecycle;
  write: (ops: OntologyEditOp[]) => Promise<void>;
};

const IMMEDIATE: EditWriter = {
  target: "immediate",
  session: null,
  readLifecycle: INITIAL_EDIT_READ_LIFECYCLE,
  write: async (ops) => {
    await commitOps(ops);
  },
};

const EditModeContext = createContext<EditMode>("workspace");

const noSubscription = () => () => undefined;

const noHostState = () => NO_HOST;

/**
 * Where kit writes and reads inside it go, for this React tree. Outside any
 * `EditSession` the mode is "workspace"; wrap a view or part of one in
 * `<EditSession mode="immediate">` to commit each write as it happens.
 */
export function EditSession({ mode, children }: { mode: EditMode; children: ReactNode }) {
  return <EditModeContext.Provider value={mode}>{children}</EditModeContext.Provider>;
}

export function useEditWriter(): EditWriter {
  const mode = useContext(EditModeContext);
  const queryClient = useQueryClient();
  const connection = mode === "workspace" && embedded ? workspaceHost() : null;

  const subscribe = useCallback(
    (listener: () => void) => {
      if (!connection) return noSubscription();
      let before = editSessionFingerprint(connection.state().session);

      return connection.subscribe(() => {
        const after = editSessionFingerprint(connection.state().session);

        // After a save, committed reads cached before it are stale. Dropping
        // them keeps staged results on screen while committed state refetches.
        if (after === editSessionFingerprint(null) && before !== after) {
          forgetCommittedStagedReads(queryClient);
        }

        before = after;
        listener();
      });
    },
    [connection, queryClient],
  );

  const state = useSyncExternalStore(subscribe, connection?.state ?? noHostState);

  return useMemo<EditWriter>(
    () =>
      connection
        ? {
            target: "workspace",
            session: state.session,
            readLifecycle: state.readLifecycle,
            write: connection.stage,
          }
        : IMMEDIATE,
    [connection, state],
  );
}
