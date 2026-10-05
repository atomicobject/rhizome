import {
  INITIAL_EDIT_READ_LIFECYCLE,
  type EditReadLifecycle,
  forgetCommittedStagedReads,
} from "../staging/stagedQuery";
import { changeSummary } from "../staging/stagedState";
import { useQueryClient } from "@tanstack/react-query";
import {
  createContext,
  createElement,
  type PropsWithChildren,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";

import {
  ApiError,
  commitOntologyEditSession,
  createOntologyEditSession,
  deleteOntologyEditSession,
  previewOntologyEditSession,
  stageOntologyEditSession,
} from "../api/publicClient";
import { queryKeys } from "../api/queryKeys";
import type { NodeWorkspace, OntologyEditOp, OntologyEditSessionResponse } from "../api/types";
import {
  cloneOps,
  editCommitSucceeded,
  compactOperations,
  consolidateSourceOperations,
  mergeBaseDocuments,
  newEditRequestID,
  newEditSessionID,
  operationKey,
  persistSnapshot,
  readStoredSnapshot,
  rebaseCommittedFieldWitnesses,
  remapCommittedEditOps,
  toSnapshot,
  type PendingCommitSubmission,
} from "./editing/editSessionState";
import { clearPersistedEditorDraft, EDITOR_REJECTED_EVENT } from "./editing/draftStorage";

function useOntologyEditSessionState(vaultKey: string | null) {
  const queryClient = useQueryClient();
  const [session, setSession] = useState<OntologyEditSessionResponse | null>(null);
  const [editing, setEditing] = useState(false);
  const [pendingCount, setPendingCount] = useState(0);

  const [restoring, setRestoring] = useState(() =>
    Boolean(readStoredSnapshot(vaultKey)?.sessionId),
  );

  const [restoredVaultKey, setRestoredVaultKey] = useState(vaultKey);
  const restoringRef = useRef(restoring);
  const [saving, setSaving] = useState(false);

  const [readLifecycle, setReadLifecycle] = useState<EditReadLifecycle>(
    INITIAL_EDIT_READ_LIFECYCLE,
  );

  const [error, setError] = useState<string | null>(null);
  const [warnings, setWarnings] = useState<string[]>([]);
  const [storageWarning, setStorageWarning] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const sessionRef = useRef<OntologyEditSessionResponse | null>(null);
  const localOpsRef = useRef<OntologyEditOp[]>([]);
  const localRevisionsRef = useRef(new Map<string, number>());
  const localRevisionClockRef = useRef(0);
  const serverRevisionRef = useRef(0);
  const transportDirtyRef = useRef(false);
  // Workspaces the last save returned, built from the written files. The index
  // catches up asynchronously, so panes apply these instead of re-reading.
  const [savedWorkspaces, setSavedWorkspaces] = useState<NodeWorkspace[]>(NO_WORKSPACES);
  // Rendered mirror of transportDirtyRef: local edits the server rows do not reflect yet.
  const [transportDirty, setTransportDirty] = useState(false);
  const pendingCommitRef = useRef<PendingCommitSubmission | undefined>(undefined);
  const actionChainRef = useRef(Promise.resolve());
  const mountedRef = useRef(true);

  useEffect(() => {
    mountedRef.current = true;

    return () => {
      mountedRef.current = false;
    };
  }, []);

  const beginOperation = useCallback(() => {
    if (mountedRef.current) setPendingCount((count) => count + 1);
  }, []);

  const finishOperation = useCallback(() => {
    if (mountedRef.current) setPendingCount((count) => Math.max(0, count - 1));
  }, []);

  const applySession = useCallback(
    (next: OntologyEditSessionResponse | null) => {
      sessionRef.current = next;

      if (next === null) serverRevisionRef.current = 0;

      if (!mountedRef.current) return;
      setSession(next);
      setStorageWarning(
        persistSnapshot(
          vaultKey,
          next,
          pendingCommitRef.current,
          Object.fromEntries(localRevisionsRef.current),
        ),
      );
    },
    [vaultKey],
  );

  const applyServerSession = useCallback(
    (next: OntologyEditSessionResponse) => {
      const nextRevision = next.revision || 0;

      if (nextRevision < serverRevisionRef.current) return;
      serverRevisionRef.current = nextRevision;
      const localOps = localOpsRef.current;

      const serverOps = new Map(
        (next.ops || []).map((op) => [operationKey(op), JSON.stringify(op)]),
      );

      transportDirtyRef.current = localOps.some(
        (op) => serverOps.get(operationKey(op)) !== JSON.stringify(op),
      );
      setTransportDirty(transportDirtyRef.current);
      applySession(
        localOps.length > 0 && next.hasUncommittedChanges
          ? { ...next, ops: compactOperations(next.ops || [], localOps) }
          : next,
      );
    },
    [applySession],
  );

  const acknowledgeServerSession = useCallback(
    (next: OntologyEditSessionResponse, submittedRevisions: Map<string, number>) => {
      const authoritative = new Map((next.ops || []).map((op) => [operationKey(op), op]));
      localOpsRef.current = localOpsRef.current.flatMap((op) => {
        const key = operationKey(op);
        const submittedRevision = submittedRevisions.get(key);
        const localRevision = localRevisionsRef.current.get(key) || 0;

        if (submittedRevision === undefined || localRevision > submittedRevision) return [op];
        const canonical = authoritative.get(key);

        if (canonical) return [canonical];
        localRevisionsRef.current.delete(key);

        return [];
      });
      applyServerSession(next);
    },
    [applyServerSession],
  );

  useEffect(() => {
    let cancelled = false;
    const snapshot = readStoredSnapshot(vaultKey);

    if (!snapshot?.sessionId) {
      restoringRef.current = false;
      setRestoring(false);
      setRestoredVaultKey(vaultKey);

      return () => {
        cancelled = true;
      };
    }

    restoringRef.current = true;
    setRestoring(true);
    const { sessionId } = snapshot;
    localOpsRef.current = snapshot.ops || [];
    localRevisionsRef.current = new Map(Object.entries(snapshot.localRevisions || {}));
    localRevisionClockRef.current = Math.max(0, ...localRevisionsRef.current.values());
    pendingCommitRef.current =
      snapshot.pendingCommit ||
      (snapshot.pendingCommitRequestId
        ? {
            requestId: snapshot.pendingCommitRequestId,
            expectedRevision: snapshot.revision || 0,
            snapshot,
            boundaryOps: cloneOps(snapshot.ops || []),
            boundaryRevisions: Object.fromEntries(localRevisionsRef.current),
          }
        : undefined);
    serverRevisionRef.current = snapshot.revision || 0;
    setEditing(true);
    beginOperation();

    const restored = actionChainRef.current
      .catch(() => undefined)
      .then(async () => {
        const submission = pendingCommitRef.current;

        const next = submission
          ? await commitOntologyEditSession(
              submission.snapshot.sessionId || sessionId,
              submission.snapshot,
              {
                requestId: submission.requestId,
                expectedRevision: submission.expectedRevision,
              },
            )
          : await previewOntologyEditSession(sessionId, toSnapshot(snapshot));

        if (mountedRef.current && !cancelled) {
          serverRevisionRef.current = Math.max(serverRevisionRef.current, next.revision || 0);

          if (submission && editCommitSucceeded(next)) {
            pendingCommitRef.current = undefined;

            const remaining = remapCommittedEditOps(
              rebaseCommittedFieldWitnesses(
                localOpsRef.current.filter(
                  (op) =>
                    (localRevisionsRef.current.get(operationKey(op)) || 0) >
                    (submission.boundaryRevisions[operationKey(op)] || 0),
                ),
                submission.boundaryOps,
              ),
              next.refLineage ?? [],
            );

            for (const [key, revision] of Object.entries(submission.boundaryRevisions)) {
              if ((localRevisionsRef.current.get(key) || 0) <= revision) {
                localRevisionsRef.current.delete(key);
              }
            }

            localOpsRef.current = remaining;
            setReadLifecycle((before) => ({
              revision: before.revision + 1,
              outcome: "saved",
              savedSession: { ...next, ops: submission.boundaryOps },
            }));
            forgetCommittedStagedReads(queryClient);

            if (remaining.length === 0) {
              setSavedWorkspaces(next.workspaces ?? NO_WORKSPACES);
              applySession(null);
              setNotice("Saved");
            } else {
              applySession({
                ...next,
                status: "dirty",
                outcome: undefined,
                ops: remaining,
                hasUncommittedChanges: true,
              });
              setNotice("Saved earlier changes");
            }
          } else {
            applyServerSession(submission ? { ...next, hasUncommittedChanges: true } : next);

            if (submission)
              setReadLifecycle((before) => ({
                ...before,
                outcome: next.outcome === "conflicted" ? "conflicted" : "failed",
              }));
          }

          setError(
            submission && next.outcome === "failed"
              ? "Couldn't save. Your changes are still staged."
              : null,
          );
          setWarnings(next.warnings || []);
        }
      })
      .catch((err) => {
        if (mountedRef.current && !cancelled) {
          setError(err instanceof Error ? err.message : "Failed to restore edits");
        }
      })
      .finally(() => {
        finishOperation();

        if (cancelled || !mountedRef.current) return;
        restoringRef.current = false;
        setRestoring(false);
        setRestoredVaultKey(vaultKey);
      });

    actionChainRef.current = restored.then(
      () => undefined,
      () => undefined,
    );

    return () => {
      cancelled = true;
    };
  }, [applyServerSession, applySession, beginOperation, finishOperation, queryClient, vaultKey]);

  const stageOps = useCallback(
    async (ops: OntologyEditOp[], options: { replace?: boolean } = {}) => {
      if (ops.length === 0) return sessionRef.current;
      setSavedWorkspaces(NO_WORKSPACES);
      const previous = sessionRef.current;

      const consolidated = consolidateSourceOperations(
        localOpsRef.current.length > 0 ? localOpsRef.current : previous?.ops || [],
        ops,
        previous?.baseDocuments,
      );

      const sourceConsolidated = consolidated.incoming.some((op) => op.kind === "setSource");

      const optimisticOps = options.replace
        ? [...consolidated.incoming]
        : compactOperations(consolidated.current, consolidated.incoming);

      const opsBefore = localOpsRef.current.length > 0 ? localOpsRef.current : previous?.ops || [];
      const revisionsBefore = new Map(localRevisionsRef.current);
      localOpsRef.current = optimisticOps;

      for (const op of consolidated.incoming) {
        localRevisionClockRef.current += 1;
        localRevisionsRef.current.set(operationKey(op), localRevisionClockRef.current);
      }

      const stagedRevisions = new Map(localRevisionsRef.current);

      // The server refused these edits, so retrying cannot help. Put back what
      // they replaced, unless a newer local edit already did, so the rest of
      // the session still saves and the edited controls show the saved value.
      const rollBack = () => {
        const beforeByKey = new Map(opsBefore.map((op) => [operationKey(op), op]));
        const afterKeys = new Set(optimisticOps.map(operationKey));

        const keys = new Set([
          ...consolidated.incoming.map(operationKey),
          ...[...beforeByKey.keys()].filter((key) => !afterKeys.has(key)),
        ]);

        const superseded = (key: string) =>
          localRevisionsRef.current.get(key) !== stagedRevisions.get(key);

        // Restore replaced edits in place so the session replays in the same order.
        const restored = localOpsRef.current.flatMap((op) => {
          const key = operationKey(op);

          if (!keys.has(key) || superseded(key)) return [op];
          const before = beforeByKey.get(key);

          return before ? [before] : [];
        });

        const restoredKeys = new Set(restored.map(operationKey));

        for (const key of keys) {
          if (superseded(key)) continue;
          const before = beforeByKey.get(key);

          if (before && !restoredKeys.has(key)) restored.push(before);
          const revision = revisionsBefore.get(key);

          if (revision === undefined) localRevisionsRef.current.delete(key);
          else localRevisionsRef.current.set(key, revision);
          clearPersistedEditorDraft(vaultKey, key);
          window.dispatchEvent(
            new CustomEvent(EDITOR_REJECTED_EVENT, { detail: { operationTarget: key } }),
          );
        }

        localOpsRef.current = restored;
        const current = sessionRef.current;

        applySession(
          restored.length === 0 && !previous?.sessionId
            ? null
            : current && {
                ...current,
                ops: restored,
                hasUncommittedChanges: restored.length > 0,
              },
        );
      };

      const submittedOps =
        !previous?.sessionId || sourceConsolidated ? optimisticOps : consolidated.incoming;

      const submittedRevisions = new Map(
        submittedOps.map((op) => [
          operationKey(op),
          localRevisionsRef.current.get(operationKey(op)) || 0,
        ]),
      );

      const optimisticSessionID = previous?.sessionId || newEditSessionID();
      const now = new Date().toISOString();
      applySession({
        ...previous,
        sessionId: optimisticSessionID,
        status: "dirty",
        ops: optimisticOps,
        hasUncommittedChanges: true,
        baseDocuments: mergeBaseDocuments(previous?.baseDocuments, optimisticOps),
        createdAt: previous?.createdAt || now,
        updatedAt: now,
      });
      beginOperation();

      const pending = actionChainRef.current
        .catch(() => undefined)
        .then(async () => {
          const currentSession = sessionRef.current;
          const pendingByKey = new Map(localOpsRef.current.map((op) => [operationKey(op), op]));

          const transportOps = (
            sourceConsolidated || !previous?.sessionId ? optimisticOps : consolidated.incoming
          ).map((op) => {
            const pending = pendingByKey.get(operationKey(op));

            // A save ahead of this queued stage already rebound its local op.
            // New reader input arrives with its own current strong reference.
            if (!pending) return op;

            const rebound = {
              ...op,
              path: pending.path,
              nodeId: pending.nodeId,
              structuralFingerprint: pending.structuralFingerprint,
            };

            if (pending.expected) rebound.expected = pending.expected;

            return rebound;
          });

          let next: OntologyEditSessionResponse;

          if (!previous?.sessionId) {
            next = await createOntologyEditSession(transportOps, optimisticSessionID);
          } else {
            if (!currentSession?.sessionId) {
              throw new Error("Edit session was cleared before pending changes were staged");
            }

            const requestId = newEditRequestID("stage");

            try {
              next = await stageOntologyEditSession(currentSession.sessionId, {
                requestId,
                expectedRevision: serverRevisionRef.current,
                ops: transportOps,
                replace: options.replace || sourceConsolidated,
              });
            } catch {
              // A server restart may drop the in-memory session. Retry once
              // with the verified local snapshot so ordinary stage requests
              // stay compact while recovery retains its original witnesses.
              next = await stageOntologyEditSession(currentSession.sessionId, {
                requestId,
                expectedRevision: serverRevisionRef.current,
                ops: transportOps,
                replace: options.replace || sourceConsolidated,
                snapshot: toSnapshot(currentSession),
              });
            }
          }

          acknowledgeServerSession(next, submittedRevisions);
          setError(null);
          setWarnings(next.warnings || []);

          return next;
        })
        .catch((err) => {
          if (
            err instanceof ApiError &&
            err.status >= 400 &&
            err.status < 500 &&
            err.status !== 408 &&
            err.status !== 429
          ) {
            rollBack();
            setError(`Couldn't stage ${describeOps(consolidated.incoming)}: ${err.message}`);
            throw err;
          }

          transportDirtyRef.current = true;
          setTransportDirty(true);
          setError(err instanceof Error ? err.message : "Failed to stage edits");
          throw err;
        })
        .finally(finishOperation);

      actionChainRef.current = pending.then(
        () => undefined,
        () => undefined,
      );

      return pending;
    },
    [acknowledgeServerSession, applySession, beginOperation, finishOperation, vaultKey],
  );

  const preview = useCallback(async () => {
    if (!sessionRef.current?.sessionId) return null;
    beginOperation();

    const pending = actionChainRef.current
      .catch(() => undefined)
      .then(async () => {
        const currentSession = sessionRef.current;

        if (!currentSession?.sessionId) return null;

        const next = await previewOntologyEditSession(
          currentSession.sessionId,
          toSnapshot(currentSession),
        );

        applyServerSession(next);
        setError(null);
        setWarnings(next.warnings || []);

        return next;
      })
      .catch((err) => {
        setError(err instanceof Error ? err.message : "Failed to preview edits");

        return sessionRef.current;
      })
      .finally(finishOperation);

    actionChainRef.current = pending.then(
      () => undefined,
      () => undefined,
    );

    return pending;
  }, [applyServerSession, beginOperation, finishOperation]);

  const commit = useCallback(async () => {
    if (!sessionRef.current?.sessionId) return null;
    const boundaryOps = cloneOps(localOpsRef.current);

    const boundaryRevisions = Object.fromEntries(
      boundaryOps.map((op) => [
        operationKey(op),
        localRevisionsRef.current.get(operationKey(op)) || 0,
      ]),
    );

    setSaving(true);
    beginOperation();

    const pending = actionChainRef.current
      .catch(() => undefined)
      .then(async () => {
        const currentSession = sessionRef.current;

        if (!currentSession?.sessionId) return null;
        let commitSession = currentSession;
        let submission = pendingCommitRef.current;

        if (!submission && transportDirtyRef.current) {
          commitSession = await stageOntologyEditSession(currentSession.sessionId, {
            requestId: newEditRequestID("flush"),
            expectedRevision: serverRevisionRef.current,
            ops: boundaryOps,
            replace: true,
            snapshot: toSnapshot(currentSession),
          });
          applyServerSession(commitSession);
        }

        if (!submission) {
          const snapshot = toSnapshot(commitSession);

          if (!snapshot) throw new Error("Edit session snapshot is unavailable");
          submission = {
            requestId: newEditRequestID("commit"),
            expectedRevision: commitSession.revision || serverRevisionRef.current,
            snapshot,
            boundaryOps,
            boundaryRevisions,
          };
          pendingCommitRef.current = submission;
          applySession(sessionRef.current);
        }

        const next = await commitOntologyEditSession(
          submission.snapshot.sessionId || commitSession.sessionId,
          submission.snapshot,
          {
            requestId: submission.requestId,
            expectedRevision: submission.expectedRevision,
          },
        );

        serverRevisionRef.current = Math.max(serverRevisionRef.current, next.revision || 0);
        pendingCommitRef.current = undefined;
        let visibleResult = next;

        if (editCommitSucceeded(next)) {
          const remaining = remapCommittedEditOps(
            rebaseCommittedFieldWitnesses(
              localOpsRef.current.filter(
                (op) =>
                  (localRevisionsRef.current.get(operationKey(op)) || 0) >
                  (submission.boundaryRevisions[operationKey(op)] || 0),
              ),
              submission.boundaryOps,
            ),
            next.refLineage ?? [],
          );

          for (const [key, revision] of Object.entries(submission.boundaryRevisions)) {
            if ((localRevisionsRef.current.get(key) || 0) <= revision) {
              localRevisionsRef.current.delete(key);
            }
          }

          localOpsRef.current = remaining;
          setReadLifecycle((before) => ({
            revision: before.revision + 1,
            outcome: "saved",
            savedSession: { ...next, ops: submission.boundaryOps },
          }));
          forgetCommittedStagedReads(queryClient);

          if (remaining.length === 0) {
            setSavedWorkspaces(next.workspaces ?? NO_WORKSPACES);
            applySession(null);
            setNotice("Saved");
          } else {
            visibleResult = {
              ...next,
              status: "dirty",
              outcome: undefined,
              ops: remaining,
              hasUncommittedChanges: true,
            };
            applySession(visibleResult);
            setNotice("Saved earlier changes");
          }
        } else {
          applyServerSession({ ...next, hasUncommittedChanges: true });
          setReadLifecycle((before) => ({
            ...before,
            outcome: next.outcome === "conflicted" ? "conflicted" : "failed",
          }));
        }

        setError(
          next.outcome === "failed" ? "Couldn't save. Your changes are still staged." : null,
        );
        setWarnings(next.warnings || []);
        void queryClient.invalidateQueries({ queryKey: queryKeys.all });

        return visibleResult;
      })
      .catch((err) => {
        const reason = err instanceof Error ? err.message : "the server did not respond";
        setError(`Couldn't save: ${reason}. Your changes are still staged.`);
        setReadLifecycle((before) => ({ ...before, outcome: "failed" }));

        return sessionRef.current;
      })
      .finally(() => {
        setSaving(false);
        finishOperation();
      });

    actionChainRef.current = pending.then(
      () => undefined,
      () => undefined,
    );

    return pending;
  }, [applyServerSession, applySession, beginOperation, finishOperation, queryClient]);

  const discard = useCallback(async () => {
    beginOperation();

    const pending = actionChainRef.current
      .catch(() => undefined)
      .then(async () => {
        const currentSession = sessionRef.current;

        if (currentSession?.sessionId) {
          try {
            await deleteOntologyEditSession(currentSession.sessionId);
          } catch (err) {
            setWarnings([
              err instanceof Error
                ? `Local edits were cleared, but the server session could not be removed: ${err.message}`
                : "Local edits were cleared, but the server session could not be removed.",
            ]);
          }
        }

        setSavedWorkspaces(NO_WORKSPACES);
        setReadLifecycle((before) => ({
          revision: before.revision + 1,
          outcome: "discarded",
          savedSession: null,
        }));
        applySession(null);
        localOpsRef.current = [];
        localRevisionsRef.current.clear();
        pendingCommitRef.current = undefined;
        setError(null);
      })
      .finally(finishOperation);

    actionChainRef.current = pending.then(
      () => undefined,
      () => undefined,
    );

    return pending;
  }, [applySession, beginOperation, finishOperation]);

  const replaceOps = useCallback(
    async (nextOps: OntologyEditOp[]) => {
      if (nextOps.length === 0) {
        await discard();

        return null;
      }

      localOpsRef.current = nextOps;

      return stageOps(nextOps, { replace: true });
    },
    [discard, stageOps],
  );

  const summary = useMemo(() => changeSummary(session), [session]);

  const startEditing = useCallback(() => {
    setNotice(null);
    setEditing(true);
  }, []);

  const stopEditing = useCallback(() => setEditing(false), []);

  return {
    vaultKey,
    session,
    editing,
    busy: restoredVaultKey !== vaultKey || pendingCount > 0 || restoringRef.current || restoring,
    savedWorkspaces,
    readLifecycle,
    /** Local edits the server has not accepted, e.g. after a failed stage request. */
    unacknowledged: transportDirty && Boolean(session?.hasUncommittedChanges),
    saving,
    error,
    warnings: storageWarning ? [...warnings, storageWarning] : warnings,
    notice,
    summary,
    stageOps,
    replaceOps,
    preview,
    commit,
    discard,
    startEditing,
    stopEditing,
    setSession: applySession,
  };
}

const NO_WORKSPACES: NodeWorkspace[] = [];

function describeOps(ops: OntologyEditOp[]) {
  const fields = [...new Set(ops.flatMap((op) => (op.field ? [op.field] : [])))];

  return fields.length > 0 ? `the change to ${fields.join(", ")}` : "this change";
}

type OntologyEditSessionController = ReturnType<typeof useOntologyEditSessionState>;

const OntologyEditSessionContext = createContext<OntologyEditSessionController | null>(null);

export function OntologyEditSessionProvider({
  vaultKey,
  children,
}: PropsWithChildren<{ vaultKey: string | null }>) {
  const controller = useOntologyEditSessionState(vaultKey);

  return createElement(OntologyEditSessionContext.Provider, { value: controller }, children);
}

/**
 * The active edit session for staged reads, or null outside the provider
 * (tests and isolated renders read committed state).
 */
export function useActiveEditSession() {
  return useContext(OntologyEditSessionContext)?.session ?? null;
}

export function useOntologyEditSession() {
  const controller = useContext(OntologyEditSessionContext);

  if (!controller) {
    throw new Error("useOntologyEditSession requires OntologyEditSessionProvider");
  }

  return controller;
}
