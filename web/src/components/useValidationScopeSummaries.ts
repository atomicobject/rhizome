import { useQuery } from "@tanstack/react-query";
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
} from "../api/types";

export function validationScopeKey(scope: ValidationScope) {
  return `${scope.kind}:${scope.key ?? ""}`;
}

export function useValidationScopeSummaries(
  generation: number | null,
  scopes: ValidationScope[],
  filter?: ValidationDiagnosticFilter,
) {
  // Stable identities matter: rails pass thousands of scopes and memoize on the result.
  const requestedScopes = useMemo(() => deduplicateScopes(scopes), [scopes]);
  const scopeBatches = useMemo(() => batchScopes(requestedScopes), [requestedScopes]);

  const query = useQuery({
    queryKey: queryKeys.validationScopeSummaries(generation ?? 0, requestedScopes, filter),
    queryFn: async ({ signal }: { signal: AbortSignal }) => {
      const responses = await Promise.all(
        scopeBatches.map((batch) =>
          getPublicValidationScopeSummaries(
            { generation: generation ?? 0, scopes: batch, filter },
            { signal },
          ),
        ),
      );

      return {
        generation: generation ?? 0,
        summaries: responses.flatMap((response) => response.summaries),
      };
    },
    enabled: Boolean(generation && generation > 0 && scopeBatches.length > 0),
  });

  const summaries = useMemo(() => {
    const byScope = new Map<string, ValidationScopeSummary>();

    for (const summary of query.data?.summaries ?? []) {
      byScope.set(validationScopeKey(summary.scope), summary);
    }

    return byScope;
  }, [query.data]);

  return {
    ...query,
    summaries,
    data: scopeBatches.length === 0 ? { generation: generation ?? 0, summaries: [] } : query.data,
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
