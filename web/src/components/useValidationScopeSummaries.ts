import { useQueries, useQuery } from "@tanstack/react-query";
import { useMemo } from "react";

import {
  getPublicValidationIssueGroups,
  getPublicValidationScopeSummaries,
} from "../api/publicClient";
import { queryKeys } from "../api/queryKeys";
import type {
  ValidationDiagnosticFilter,
  ValidationScope,
  ValidationScopeSummary,
  ValidationScopeSummaryResponse,
} from "../api/types";

export function validationScopeKey(scope: ValidationScope) {
  return `${scope.kind}:${scope.key ?? ""}`;
}

/**
 * Validation summaries for scopes, read in batches of 200 that are each their
 * own query. A list that grows (a table loading more rows) keeps the keys of
 * its earlier, full batches, so only the batches it adds or extends fetch.
 * Keys carry the published generation, so a new generation reads everything
 * again and summaries never mix generations.
 */
export function useValidationScopeSummaries(
  generation: number | null,
  scopes: ValidationScope[],
  filter?: ValidationDiagnosticFilter,
) {
  // Stable identities matter: rails pass thousands of scopes and memoize on the result.
  const requestedScopes = useMemo(() => deduplicateScopes(scopes), [scopes]);
  const scopeBatches = useMemo(() => batchScopes(requestedScopes), [requestedScopes]);
  const published = generation ?? 0;
  const enabled = Boolean(generation && generation > 0);

  return useQueries({
    queries: scopeBatches.map((batch) => ({
      queryKey: queryKeys.validationScopeSummaries(published, batch, filter),
      queryFn: ({ signal }: { signal: AbortSignal }) =>
        getPublicValidationScopeSummaries(
          { generation: published, scopes: batch, filter },
          { signal },
        ),
      enabled,
      // A batch that grows keeps its earlier counts, from the same generation,
      // until the larger read arrives.
      placeholderData: (
        previous: ValidationScopeSummaryResponse | undefined,
        previousQuery?: { queryKey: readonly unknown[] },
      ) => (previousQuery?.queryKey[3] === published ? previous : undefined),
    })),
    combine: combineSummaries,
  });
}

// Module-level so useQueries reuses the combined result while no batch changes.
function combineSummaries(
  results: ReadonlyArray<{
    data?: ValidationScopeSummaryResponse;
    isLoading: boolean;
    error: Error | null;
  }>,
) {
  const summaries: ReadonlyMap<string, ValidationScopeSummary> = new Map(
    results.flatMap((result) =>
      (result.data?.summaries ?? []).map(
        (summary) => [validationScopeKey(summary.scope), summary] as const,
      ),
    ),
  );

  return {
    summaries,
    isLoading: results.some((result) => result.isLoading),
    error: results.find((result) => result.error)?.error ?? null,
  };
}

/** Issue counts per kind and variant for one scope and filter, from one grouped read. */
export function useValidationIssueGroups(
  generation: number | null,
  scope: ValidationScope,
  filter: ValidationDiagnosticFilter,
  enabled = true,
) {
  return useQuery({
    queryKey: queryKeys.validationIssueGroups(generation ?? 0, scope, filter),
    queryFn: ({ signal }) =>
      getPublicValidationIssueGroups({ generation: generation ?? 0, scope, filter }, { signal }),
    enabled: enabled && Boolean(generation && generation > 0),
    // Filter edits keep the same generation and scope on screen until the new counts arrive.
    placeholderData: (previous, previousQuery) =>
      previousQuery?.queryKey[3] === (generation ?? 0) &&
      JSON.stringify(previousQuery.queryKey[4]) === JSON.stringify(scope)
        ? previous
        : undefined,
  });
}

function deduplicateScopes(scopes: ValidationScope[]) {
  return [...new Map(scopes.map((scope) => [validationScopeKey(scope), scope])).values()];
}

function batchScopes(scopes: ValidationScope[]) {
  const batches: ValidationScope[][] = [];

  for (let index = 0; index < scopes.length; index += 200) {
    batches.push(scopes.slice(index, index + 200));
  }

  return batches;
}

export function noteIssueCounts(
  summaries: ReadonlyMap<string, ValidationScopeSummary> | undefined,
): Map<string, number> {
  const counts = new Map<string, number>();

  for (const summary of summaries?.values() ?? []) {
    if (summary.scope.kind === "note" && summary.scope.key) {
      counts.set(summary.scope.key, summary.issueCount);
    }
  }

  return counts;
}
