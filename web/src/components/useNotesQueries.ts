import { useQuery } from "@tanstack/react-query";

import {
  getGlobalGraph,
  getNodeWorkspaceGraph,
  getOntologySummary,
  getOntologyType,
  searchNotes,
} from "../api/client";
import { executePublicView, getPublicValidate, listPublicViews } from "../api/publicClient";
import { queryKeys } from "../api/queryKeys";
import type {
  NodeRef,
  OntologySummaryResponse,
  ValidateEnvelope,
  ViewExecuteRequest,
} from "../api/types";
import { useStagedQuery, type EditReadLifecycle, type StagedSession } from "../staging/stagedQuery";

const FOCUSED_GRAPH_OPTIONS = { notesOnly: true } as const;

/**
 * Whether a summary's counts are real. A rebuilding summary with no published
 * snapshot yet, as during the first index after a start, has none.
 */
export function summaryCountsKnown(summary: OntologySummaryResponse | null | undefined) {
  return Boolean(summary) && !(summary?.rebuilding && summary.totalNotes === 0);
}

export function useOntologySummaryQuery(enabled = true) {
  return useQuery({
    queryKey: queryKeys.ontology.summary(),
    queryFn: ({ signal }) => getOntologySummary({ signal }),
    enabled,
  });
}

export function useOntologyTypeQuery(
  typeName: string | null | undefined,
  session: StagedSession,
  enabled = true,
) {
  return useStagedQuery({
    subject: queryKeys.ontology.type(typeName ?? ""),
    session,
    queryFn: ({ signal, editSession }) => getOntologyType(typeName ?? "", { editSession, signal }),
    enabled: enabled && Boolean(typeName && typeName !== "__modified__"),
  });
}

export function useNoteSearchQuery(query: string, typeName: string | undefined) {
  const options = { type: typeName, limit: 40 } as const;

  return useQuery({
    queryKey: queryKeys.search.notes(query, options),
    queryFn: ({ signal }) => searchNotes(query, options, { signal }),
    enabled: query.length >= 3,
  });
}

export function useGlobalNotesGraphQuery(enabled = true) {
  const options = { notesOnly: true } as const;

  return useQuery({
    queryKey: queryKeys.graph.global(options),
    queryFn: ({ signal }) => getGlobalGraph(options, { signal }),
    enabled,
  });
}

export function useViewCatalogQuery(enabled = true) {
  return useQuery({
    queryKey: queryKeys.views.catalog(),
    queryFn: ({ signal }) => listPublicViews({ signal }),
    enabled,
  });
}

export function useValidationQuery(enabled = true) {
  return useQuery({
    queryKey: queryKeys.validation(),
    queryFn: ({ signal }): Promise<ValidateEnvelope> => getPublicValidate({ signal }),
    refetchInterval: (query) =>
      query.state.data?.refreshPending ||
      ["running", "stale", "never_checked"].includes(query.state.data?.health ?? "")
        ? 2000
        : false,
    enabled,
  });
}

export function useViewExecutionQuery(
  viewID: string | null,
  state: ViewExecuteRequest,
  session: StagedSession,
  enabled = true,
  lifecycle?: EditReadLifecycle,
) {
  return useStagedQuery({
    lifecycle,
    subject: queryKeys.views.execution(viewID ?? ""),
    refinement: state,
    session,
    queryFn: ({ signal, editSession }) =>
      executePublicView(viewID ?? "", state, { editSession, signal }),
    enabled: enabled && Boolean(viewID),
  });
}

export function useFocusedWorkspaceGraphQuery(
  ref: string | NodeRef | null,
  session: StagedSession,
  workspaceVersion = "",
  enabled = true,
) {
  return useStagedQuery({
    subject: queryKeys.graph.local(ref ?? "", 500, FOCUSED_GRAPH_OPTIONS),
    refinement: workspaceVersion,
    session,
    queryFn: ({ signal, editSession }) => getNodeWorkspaceGraph(ref ?? "", { editSession, signal }),
    enabled: Boolean(ref) && enabled,
  });
}
