import { useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore } from "react";

import * as ontologyApi from "../api/client";
import { decodeJson, isJsonObject, isString, isStringArray } from "../api/parse";
import type {
  NodeRef,
  NodeWorkspace,
  OntologyEditSessionResponse,
  RenderedFile,
  StructuralNode,
} from "../api/types";
import { getVaultIndexRevision, subscribeVaultIndexRevision } from "../query/vaultIndexRevision";
import { subscribeVaultEvents } from "../query/vaultEvents";
import { editSessionFingerprint } from "../staging/stagedQuery";
import { editPathForRef, isTouched, type StagedTarget } from "../staging/stagedState";
import { subscribeNodeEvents } from "./nodeEventHub";
import { canonicalNodeRefKey, nodeRefMatches } from "./nodeRef";

type PaneState = {
  id: string;
  requestedRef?: string | null;
  path: string | null;
  anchor: string | null;
  resolvedRef?: NodeRef;
  rendered?: RenderedFile | null;
  workspace: NodeWorkspace | null;
  loading: boolean;
  error: string | null;
  structuralNode?: StructuralNode | null;
};

function asNotePane(pane: PaneState, patch: Partial<Omit<PaneState, "id">>): PaneState {
  return {
    ...pane,
    ...patch,
    structuralNode: null,
  };
}

function emptyPane(id: string): PaneState {
  return {
    id,
    requestedRef: null,
    path: null,
    anchor: null,
    rendered: null,
    workspace: null,
    loading: false,
    error: null,
    structuralNode: null,
  };
}

function applyWorkspaceToPane(pane: PaneState, workspace: NodeWorkspace): PaneState {
  const resolvedRef = workspace.node?.ref;
  const requestedRef = pane.requestedRef || workspace.requestedRef || null;

  return asNotePane(pane, {
    requestedRef,
    path: workspace.node?.notePath || workspace.content.path || pane.path,
    anchor: resolvedRef?.fragment || pane.anchor,
    resolvedRef,
    rendered: workspace.content?.rendered || null,
    workspace,
    loading: false,
    error: null,
  });
}

/** A note target split into its path and `#fragment` parts. */
type NoteTarget = {
  path: string | null;
  anchor: string | null;
};

function splitNoteTarget(target: string | null): NoteTarget {
  if (!target) {
    return { path: null, anchor: null };
  }

  const hashIndex = target.indexOf("#");

  if (hashIndex === -1) {
    return { path: target, anchor: null };
  }

  return {
    path: target.slice(0, hashIndex),
    anchor: target.slice(hashIndex + 1) || null,
  };
}

export function targetRefForNode(node: StructuralNode): string | NodeRef {
  if (node.locator === "FILE") return node.notePath;

  return {
    notePath: node.notePath,
    fragment: node.fragment || undefined,
    nodeId: node.nodeId || undefined,
    structuralFingerprint: node.structuralFingerprint || undefined,
    kind: node.locator === "EMBEDDED" ? "EMBEDDED" : "SECTION",
  };
}

async function fetchPaneWorkspace(
  target: string | NodeRef,
  editSession?: OntologyEditSessionResponse | null,
  signal?: AbortSignal,
): Promise<NodeWorkspace> {
  return ontologyApi.getNodeWorkspace(target, { editSession, signal });
}

type PaneStackHandle = {
  paneStack: PaneState[];
  loadPane: (paneID: string, target: string | NodeRef) => Promise<void>;
  openRootPane: (target: string | NodeRef) => Promise<void>;
  /**
   * Return the index of a pane through `sourceIndex + 1` with the same node
   * identity (path + fragment) as `target`, or truncate after `sourceIndex` and
   * push a new pane. Fragments count, so drilling into a section of the source
   * note opens a distinct pane instead of re-focusing the source pane.
   */
  focusOrPushNodeFromIndex: (
    sourceIndex: number,
    target: string | NodeRef,
  ) => { index: number; pushed: boolean };
  closePaneAt: (index: number) => void;
};

/**
 * Fragment-aware dedupe key. Two targets with the same `path#fragment`
 * (or same bare path when neither has a fragment) collapse to the same
 * pane; differing fragments fork into distinct panes.
 */
function paneNodeKey(pane: PaneState): string | null {
  if (pane.path) {
    return pane.anchor ? `${pane.path}#${pane.anchor}` : pane.path;
  }

  return pane.requestedRef || null;
}

function nodeTargetKey(target: string | NodeRef): string | null {
  if (isString(target)) return target || null;

  if (!target.notePath) return null;

  return target.fragment ? `${target.notePath}#${target.fragment}` : target.notePath;
}

function targetMatchesResolvedRef(
  target: string | NodeRef,
  resolvedRef: NodeRef | null | undefined,
): boolean {
  if (!resolvedRef) return false;

  if (isString(target)) {
    const { path, anchor } = splitNoteTarget(target);

    if (path !== resolvedRef.notePath) return false;

    if (anchor) return anchor === resolvedRef.fragment;

    return (
      !resolvedRef.fragment &&
      !resolvedRef.nodeId &&
      !resolvedRef.structuralFingerprint &&
      resolvedRef.kind !== "SECTION" &&
      resolvedRef.kind !== "EMBEDDED"
    );
  }

  return canonicalNodeRefKey(target) === canonicalNodeRefKey(resolvedRef);
}

function isPaneStackUpdater(
  action: React.SetStateAction<PaneState[]>,
): action is (current: PaneState[]) => PaneState[] {
  return typeof action === "function";
}

/**
 * `savedWorkspaces` are the workspaces the last save returned; after a save the
 * session clears and touched panes take these instead of re-reading an index
 * that has not caught up with the written files.
 */
export function usePaneStack(
  editSession?: OntologyEditSessionResponse | null,
  savedWorkspaces: NodeWorkspace[] = NO_WORKSPACES,
): PaneStackHandle {
  const [paneStack, setPaneStackState] = useState<PaneState[]>([]);
  const paneStackRef = useRef<PaneState[]>([]);

  // Per-pane request generations prevent late async responses from reviving a
  // pane the user has already cleared or replaced.
  const indexRevision = useSyncExternalStore(subscribeVaultIndexRevision, getVaultIndexRevision);
  const paneRequestIndexRevision = useRef<Record<string, number>>({});
  const paneRequestVersion = useRef<Record<string, number>>({});
  const paneRequestControllers = useRef<Record<string, AbortController>>({});

  const beginPaneRequest = useCallback((paneID: string) => {
    paneRequestIndexRevision.current[paneID] = getVaultIndexRevision();
    paneRequestControllers.current[paneID]?.abort();
    const controller = new AbortController();
    paneRequestControllers.current[paneID] = controller;
    const requestVersion = (paneRequestVersion.current[paneID] || 0) + 1;
    paneRequestVersion.current[paneID] = requestVersion;

    return { controller, requestVersion };
  }, []);

  const abortPaneRequest = useCallback((paneID: string) => {
    paneRequestControllers.current[paneID]?.abort();
    delete paneRequestControllers.current[paneID];
    paneRequestVersion.current[paneID] = (paneRequestVersion.current[paneID] || 0) + 1;
  }, []);

  const setPaneStack = useCallback<React.Dispatch<React.SetStateAction<PaneState[]>>>(
    (action) => {
      const current = paneStackRef.current;
      const next = isPaneStackUpdater(action) ? action(current) : action;
      const nextPaneIDs = new Set(next.map((pane) => pane.id));
      current.forEach((pane) => {
        if (!nextPaneIDs.has(pane.id)) abortPaneRequest(pane.id);
      });
      paneStackRef.current = next;
      setPaneStackState(next);
    },
    [abortPaneRequest],
  );

  const paneSequence = useRef(0);

  const nextPaneID = useCallback(() => {
    paneSequence.current += 1;

    return `pane-${paneSequence.current}`;
  }, []);

  const loadPane = useCallback(
    async (paneID: string, target: string | NodeRef) => {
      const { controller, requestVersion } = beginPaneRequest(paneID);

      const requestedRef = ontologyApi.publicWorkspaceRef(target);

      const { path, anchor } = isString(target)
        ? splitNoteTarget(target)
        : { path: target.notePath || null, anchor: target.fragment || null };

      if (!path) {
        setPaneStack((current) =>
          current.map((pane) =>
            pane.id === paneID
              ? asNotePane(pane, {
                  requestedRef,
                  path: null,
                  anchor: null,
                  resolvedRef: undefined,
                  workspace: null,
                  loading: false,
                  error: "Failed to load note",
                })
              : pane,
          ),
        );

        return;
      }

      setPaneStack((current) =>
        current.map((pane) => {
          if (pane.id !== paneID) return pane;
          const refreshing = targetMatchesResolvedRef(target, pane.resolvedRef);

          return asNotePane(pane, {
            requestedRef,
            path,
            anchor,
            resolvedRef: refreshing ? pane.resolvedRef : undefined,
            rendered: refreshing ? pane.rendered : null,
            workspace: refreshing ? pane.workspace : null,
            loading: true,
            error: null,
          });
        }),
      );

      try {
        const workspace = await fetchPaneWorkspace(target, editSession, controller.signal);
        setPaneStack((current) =>
          current.map((pane) => {
            if (pane.id !== paneID) return pane;

            if (paneRequestVersion.current[paneID] !== requestVersion) {
              return pane;
            }

            const resolvedRef = workspace.node?.ref;

            return asNotePane(pane, {
              requestedRef,
              path: workspace.node?.notePath || workspace.content?.path || path,
              anchor: resolvedRef?.fragment || anchor,
              resolvedRef,
              rendered: workspace.content?.rendered || null,
              workspace,
              loading: false,
              error: null,
            });
          }),
        );
      } catch (error) {
        if (controller.signal.aborted) return;
        const message = error instanceof Error ? error.message : "Failed to load note";
        setPaneStack((current) =>
          current.map((pane) => {
            if (pane.id !== paneID) return pane;

            if (paneRequestVersion.current[paneID] !== requestVersion) {
              return pane;
            }

            const refreshing = targetMatchesResolvedRef(target, pane.resolvedRef);

            return asNotePane(pane, {
              requestedRef,
              path,
              anchor,
              resolvedRef: refreshing ? pane.resolvedRef : undefined,
              rendered: refreshing ? pane.rendered : null,
              workspace: refreshing ? pane.workspace : null,
              loading: false,
              error: message,
            });
          }),
        );
      }
    },
    [beginPaneRequest, editSession, setPaneStack],
  );

  // Reloads the panes the previous or current session touches, reading with
  // the current one; staged workspaces in the session are applied directly.
  const refreshTouchedPanes = useCallback(
    async (
      previous: OntologyEditSessionResponse | null,
      session: OntologyEditSessionResponse | null,
      workspaces: NodeWorkspace[],
    ) => {
      const reloadTargets = paneStackRef.current.filter((pane) => {
        if (!pane.requestedRef) return false;
        const target = paneStagedTarget(pane);

        return isTouched(previous, target) || isTouched(session, target);
      });

      const workspaceByKey = new Map<string, NodeWorkspace>();
      const noteWorkspaceByPath = new Map<string, NodeWorkspace>();

      for (const workspace of workspaces) {
        if (!workspace?.node?.ref) continue;
        workspaceByKey.set(canonicalNodeRefKey(workspace.node.ref), workspace);

        if (workspace.node.ref.kind === "NOTE" && workspace.node.notePath) {
          noteWorkspaceByPath.set(workspace.node.notePath, workspace);
        }
      }

      const committedWorkspaceForPane = (pane: PaneState) => {
        const exact = workspaceByKey.get(canonicalNodeRefKey(pane.resolvedRef));

        if (exact) return exact;

        if (pane.resolvedRef?.kind !== "NOTE") return undefined;

        return noteWorkspaceByPath.get(pane.resolvedRef.notePath || pane.path || "");
      };

      const satisfiedPaneIDs = new Set(
        reloadTargets.flatMap((pane) => (committedWorkspaceForPane(pane) ? [pane.id] : [])),
      );

      if (workspaceByKey.size > 0) {
        satisfiedPaneIDs.forEach(abortPaneRequest);
        setPaneStack((current) =>
          current.map((pane) => {
            if (!satisfiedPaneIDs.has(pane.id)) return pane;
            const workspace = committedWorkspaceForPane(pane);

            if (!workspace) return pane;

            return applyWorkspaceToPane(pane, workspace);
          }),
        );
      }

      const reloads = reloadTargets.flatMap((pane) => {
        if (satisfiedPaneIDs.has(pane.id)) return [];
        const { controller, requestVersion } = beginPaneRequest(pane.id);

        return [
          fetchPaneWorkspace(
            pane.resolvedRef || pane.requestedRef || "",
            session,
            controller.signal,
          )
            .then((workspace) => {
              setPaneStack((current) =>
                current.map((currentPane) =>
                  currentPane.id === pane.id &&
                  paneRequestVersion.current[pane.id] === requestVersion
                    ? applyWorkspaceToPane(currentPane, workspace)
                    : currentPane,
                ),
              );
            })
            .catch((error) => {
              if (controller.signal.aborted) return;
              // Nothing awaits a refresh, so the pane records the failure and keeps its content.
              const message = error instanceof Error ? error.message : "Failed to load note";
              setPaneStack((current) =>
                current.map((currentPane) =>
                  currentPane.id === pane.id &&
                  paneRequestVersion.current[pane.id] === requestVersion
                    ? asNotePane(currentPane, { loading: false, error: message })
                    : currentPane,
                ),
              );
            }),
        ];
      });

      await Promise.all(reloads);
    },
    [abortPaneRequest, beginPaneRequest, setPaneStack],
  );

  // Panes are staged reads (see src/staging): when the server acknowledges a
  // different session state (stage, commit, discard, restore, rebase), every
  // pane either session touches reloads, so no caller has to request it.
  const sessionFingerprint = editSessionFingerprint(editSession ?? null);

  const syncedSession = useRef({
    fingerprint: sessionFingerprint,
    session: editSession ?? null,
  });

  useEffect(() => {
    const previous = syncedSession.current;

    if (previous.fingerprint === sessionFingerprint) return;
    syncedSession.current = { fingerprint: sessionFingerprint, session: editSession ?? null };
    const session = editSession ?? null;
    void refreshTouchedPanes(
      previous.session,
      session,
      session ? (session.workspaces ?? []) : savedWorkspaces,
    );
  }, [editSession, refreshTouchedPanes, savedWorkspaces, sessionFingerprint]);

  const openRootPane = useCallback(
    async (target: string | NodeRef) => {
      const paneID = nextPaneID();
      setPaneStack([emptyPane(paneID)]);
      await loadPane(paneID, target);
    },
    [loadPane, nextPaneID, setPaneStack],
  );

  const pushPaneFromIndex = useCallback(
    async (sourceIndex: number, target: string | NodeRef) => {
      const paneID = nextPaneID();
      setPaneStack([...paneStackRef.current.slice(0, sourceIndex + 1), emptyPane(paneID)]);
      await loadPane(paneID, target);
    },
    [loadPane, nextPaneID, setPaneStack],
  );

  const closePaneAt = useCallback(
    (index: number) => {
      setPaneStack(paneStackRef.current.slice(0, index));
    },
    [setPaneStack],
  );

  const focusOrPushNodeFromIndex = useCallback(
    (sourceIndex: number, target: string | NodeRef) => {
      const key = nodeTargetKey(target);

      if (key) {
        const searchablePanes = paneStackRef.current.slice(0, sourceIndex + 2);

        const existing = searchablePanes.findIndex((pane) =>
          isString(target)
            ? paneNodeKey(pane) === key
            : targetMatchesResolvedRef(target, pane.resolvedRef),
        );

        if (existing >= 0) {
          return { index: existing, pushed: false };
        }
      }

      const newIndex = sourceIndex + 1;
      void pushPaneFromIndex(sourceIndex, target);

      return { index: newIndex, pushed: true };
    },
    [pushPaneFromIndex],
  );

  // --- SSE subscription ---

  const subscriptionKey = useMemo(
    () =>
      JSON.stringify(
        Array.from(
          new Set(
            paneStack.flatMap((pane) => {
              const key = canonicalNodeRefKey(pane.resolvedRef);

              return key ? [key] : [];
            }),
          ),
        ).sort(),
      ),
    [paneStack],
  );

  useEffect(() => {
    for (const pane of paneStack) {
      if (
        pane.error &&
        !pane.loading &&
        pane.requestedRef &&
        (paneRequestIndexRevision.current[pane.id] ?? indexRevision) < indexRevision
      ) {
        void loadPane(pane.id, pane.resolvedRef || pane.requestedRef);
      }
    }
  }, [indexRevision, paneStack, loadPane]);

  const loadPaneRef = useRef(loadPane);
  useEffect(() => {
    loadPaneRef.current = loadPane;
  }, [loadPane]);

  // Raw node watch events can precede catalog publication. A successful read
  // in that window still needs the committed type and metadata once ready.
  useEffect(
    () =>
      subscribeVaultEvents((event) => {
        if (event.event !== "node.changed") return;
        const envelope = decodeJson(event.data, isJsonObject);
        const data = envelope?.data;

        if (!isJsonObject(data) || !isStringArray(data.paths)) return;
        const paths = new Set(data.paths);

        for (const pane of paneStackRef.current) {
          if (!pane.requestedRef) continue;
          const path = pane.resolvedRef?.notePath || pane.path;

          if (path && paths.has(path)) {
            void loadPaneRef.current(pane.id, pane.resolvedRef || pane.requestedRef);
          }
        }
      }),
    [],
  );

  useEffect(() => {
    if (subscriptionKey === "[]") return;

    const refs = Array.from(
      paneStackRef.current
        .reduce((refsByKey, pane) => {
          if (pane.resolvedRef) {
            refsByKey.set(canonicalNodeRefKey(pane.resolvedRef), pane.resolvedRef);
          }

          return refsByKey;
        }, new Map<string, NodeRef>())
        .values(),
    );

    if (refs.length === 0) {
      return;
    }

    return subscribeNodeEvents(refs, (event) => {
      paneStackRef.current.forEach((pane) => {
        if (!nodeRefMatches(pane.resolvedRef, event.ref)) return;

        // Reading now can return the index's stale copy of the file, so the
        // pane stops following it until the user retries: no resolved ref
        // ends the subscription, no requested ref skips the automatic reload.
        if (event.kind === "node.deleted") {
          abortPaneRequest(pane.id);
          setPaneStack(
            paneStackRef.current.map((candidate) =>
              candidate.id === pane.id
                ? asNotePane(candidate, {
                    requestedRef: null,
                    resolvedRef: undefined,
                    rendered: null,
                    workspace: null,
                    loading: false,
                    error: `${candidate.path || "This note"} was deleted or moved on disk.`,
                  })
                : candidate,
            ),
          );

          return;
        }

        void loadPaneRef.current(pane.id, event.canonicalRef || event.ref);
      });
    });
  }, [abortPaneRequest, setPaneStack, subscriptionKey]);

  useEffect(
    () => () => {
      Object.values(paneRequestControllers.current).forEach((controller) => {
        controller.abort();
      });
      paneRequestControllers.current = {};
    },
    [],
  );

  return {
    paneStack,
    loadPane,
    openRootPane,
    focusOrPushNodeFromIndex,
    closePaneAt,
  };
}

const NO_WORKSPACES: NodeWorkspace[] = [];

function paneStagedTarget(pane: PaneState): StagedTarget {
  return {
    ref: pane.resolvedRef,
    paths: [pane.requestedRef, editPathForRef(pane.resolvedRef, pane.path || ""), pane.path],
  };
}
