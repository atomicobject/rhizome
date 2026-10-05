/**
 * Server reads that reflect the active edit session.
 *
 * Any query whose answer can change with staged edits goes through
 * `useStagedQuery`. It keys the query by the session fingerprint, hands the
 * session to the fetch (the API client sends it as a read overlay), and keeps
 * the previous result for the same subject on screen while a refinement or the
 * session changes, so layout holds during the refetch (SPEC-0074).
 *
 * `subject` names what is read (a view, a node's graph, a type); a different
 * subject starts empty. `refinement` narrows it (view state, a version) and may
 * change in place.
 */
import {
  hashKey,
  useQuery,
  type QueryClient,
  type QueryKey,
  type UseQueryOptions,
} from "@tanstack/react-query";

import { useRef } from "react";

import type { OntologyEditSessionResponse } from "../api/types";

export type StagedSession = OntologyEditSessionResponse | null;

/** Persistence outcomes are separate from the dirty session and display-only saved edits. */
export type EditReadLifecycle = {
  revision: number;
  outcome: "saved" | "discarded" | "failed" | "conflicted" | null;
  savedSession: StagedSession;
};

export const INITIAL_EDIT_READ_LIFECYCLE: EditReadLifecycle = {
  revision: 0,
  outcome: null,
  savedSession: null,
};

// Distinctive so staged committed-state entries are recognizable by key alone.
const COMMITTED_FINGERPRINT = "edit-session:committed";

/**
 * Changes only when the server acknowledges session state (its revision), not
 * when an edit is applied locally: a read issued before the stage lands would
 * return rows without that edit. Until the server knows the session, reads see
 * committed state.
 */
export function editSessionFingerprint(session: StagedSession) {
  if (!session?.revision) return COMMITTED_FINGERPRINT;

  return [
    session.sessionId,
    session.revision,
    session.status,
    session.hasUncommittedChanges ? "dirty" : "clean",
  ].join(":");
}

/**
 * Call when a save succeeds, before the session clears. Committed-state
 * results cached before the save are stale; dropping them lets each staged
 * read hold its staged result while committed data refetches, instead of
 * flashing pre-save values.
 */
export function forgetCommittedStagedReads(queryClient: QueryClient) {
  queryClient.removeQueries({
    predicate: (query) => query.queryKey.at(-1) === COMMITTED_FINGERPRINT,
  });
}

export function stagedQueryKey<Refinement>(
  subject: QueryKey,
  refinement: Refinement,
  session: StagedSession,
  readRevision = 0,
) {
  return [
    ...subject,
    refinement ?? null,
    ...(readRevision ? [readRevision] : []),
    editSessionFingerprint(session),
  ] as const;
}

/**
 * `placeholderData` that keeps the previous result on screen while a query for
 * the same subject refetches, and starts a different subject empty.
 */
export function holdSameSubject(subject: QueryKey) {
  const subjectHash = hashKey(subject);

  return <T>(previous: T | undefined, previousQuery?: { queryKey: QueryKey }) =>
    previousQuery && hashKey(previousQuery.queryKey.slice(0, subject.length)) === subjectHash
      ? previous
      : undefined;
}

export function useStagedQuery<T>({
  subject,
  refinement,
  session,
  enabled = true,
  queryFn,
  staleTime,
  retry,
  lifecycle,
  options,
}: {
  subject: QueryKey;
  refinement?: unknown;
  /** Required so every staged read decides which session it reflects. */
  session: StagedSession;
  enabled?: boolean;
  queryFn: (context: { signal: AbortSignal; editSession: StagedSession }) => Promise<T>;
  staleTime?: number;
  retry?: boolean;
  lifecycle?: EditReadLifecycle;
  options?: Omit<UseQueryOptions<T, Error, T, QueryKey>, "queryKey" | "queryFn">;
}) {
  const subjectHash = hashKey(subject);

  const held = useRef<{
    subjectHash: string;
    data: T | undefined;
    revision: number;
    savedSession: StagedSession;
  }>({ subjectHash, data: undefined, revision: -1, savedSession: null });

  if (held.current.subjectHash !== subjectHash) {
    held.current = { subjectHash, data: undefined, revision: -1, savedSession: null };
  }

  if (held.current.revision !== (lifecycle?.revision ?? 0)) {
    const saved = lifecycle?.savedSession;

    if (saved) {
      held.current.savedSession = {
        ...saved,
        ops: [...(held.current.savedSession?.ops ?? []), ...(saved.ops ?? [])],
        refLineage: [...(held.current.savedSession?.refLineage ?? []), ...(saved.refLineage ?? [])],
      };
    } else if (lifecycle?.outcome !== "discarded") {
      held.current.savedSession = null;
    }

    held.current.revision = lifecycle?.revision ?? 0;
  }

  const queryOptions: UseQueryOptions<T, Error, T, QueryKey> = {
    placeholderData: holdSameSubject(subject),
    enabled,
    ...options,
    queryKey: stagedQueryKey(subject, refinement, session, lifecycle?.revision),
    queryFn: ({ signal }) => queryFn({ signal, editSession: session }),
  };

  // An explicit undefined would replace the app defaults, so set only what the caller chose.
  if (staleTime !== undefined) queryOptions.staleTime = staleTime;

  if (retry !== undefined) queryOptions.retry = retry;

  // initialData can otherwise make a new post-save identity look fresh and
  // suppress its canonical request for the caller's entire staleTime.
  if (held.current.savedSession) queryOptions.staleTime = 0;

  const query = useQuery(queryOptions);

  // Only the new request identity can acknowledge saved display edits. Late
  // responses belong to their old query key and cannot clear this boundary.
  if (query.isSuccess && query.isFetched && !query.isPlaceholderData)
    held.current.savedSession = null;

  if (query.data !== undefined) held.current.data = query.data;
  const savedSession = held.current.savedSession;
  const savedEditsPending = Boolean(savedSession);

  const displaySession = savedSession
    ? {
        ...savedSession,
        ...session,
        ops: [...(savedSession.ops ?? []), ...(session?.ops ?? [])],
        refLineage: [...(savedSession.refLineage ?? []), ...(session?.refLineage ?? [])],
      }
    : session;

  return {
    ...query,
    displayData: query.data ?? (savedEditsPending ? held.current.data : undefined),
    displaySession,
    savedEditsPending,
  };
}
