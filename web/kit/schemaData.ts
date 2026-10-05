// Hooks for display-group membership, type documentation, and validation
// summaries. Their query keys belong to the freshness classes in freshness.ts.
import { useQueries, useQuery, type UseQueryResult } from "@tanstack/react-query";

import { fetchJSON, getPublicValidate } from "../src/api/client";
import type { JsonValue } from "../src/api/parse";
import type { ValidationScope } from "../src/api/types";
import { useValidationScopeSummaries } from "../src/components/useValidationScopeSummaries";
import { kitQueryKeys } from "./freshness";
import { parseDisplayGroups, parseTypeDocResponse } from "./schemaParse";
import type { DisplayGroup, TypeDoc } from "./types";
import { getViewContext } from "./viewContext";

export { validationScopeKey } from "../src/components/useValidationScopeSummaries";

/** Every effective display group, sorted by name. */
export function useDisplayGroups() {
  const query = useQuery({
    queryKey: kitQueryKeys.displayGroups(),
    queryFn: async ({ signal }) =>
      parseDisplayGroups(await fetchJSON<JsonValue>("/api/v1/display-groups", { signal })),
  });

  return { ...query, groups: query.data?.groups };
}

/**
 * One display group's members, defaulting to the group the view was opened
 * for. `group` is null once loaded when no such group exists.
 */
export function useDisplayGroup(name?: string) {
  const context = getViewContext();
  const wanted = name ?? (context.kind === "group" ? context.group : undefined);
  const query = useDisplayGroups();

  const group: DisplayGroup | null | undefined = query.groups
    ? (query.groups.find((candidate) => candidate.name === wanted) ?? null)
    : undefined;

  return { ...query, group };
}

type NamedTypeDoc = readonly [name: string, doc: TypeDoc];

// Module scope keeps `combine` stable, so `docs` keeps its identity until a
// result changes.
function combineTypeDocs(results: Array<UseQueryResult<NamedTypeDoc>>) {
  const docs: Record<string, TypeDoc> = {};

  for (const { data } of results) if (data) docs[data[0]] = data[1];

  return {
    docs,
    isLoading: results.some((result) => result.isLoading),
    error: results.find((result) => result.error)?.error ?? null,
  };
}

/**
 * Type documentation for each named type or interface, without its notes.
 * `docs` holds the names that have loaded; `error` is the first failure.
 */
export function useTypeDocs(names: readonly string[]) {
  return useQueries({
    queries: names.map((name) => ({
      queryKey: kitQueryKeys.typeDoc(name),
      queryFn: async ({ signal }: { signal: AbortSignal }): Promise<NamedTypeDoc> => {
        const response = await fetchJSON<JsonValue>(
          `/api/v1/ontology/types/${encodeURIComponent(name)}?notes=none`,
          { signal },
        );

        const doc = parseTypeDocResponse(response, name);

        if (!doc) throw new Error(`Rhizome has no documentation for type ${name}`);

        return [name, doc];
      },
    })),
    combine: combineTypeDocs,
  });
}

/**
 * Issue counts for each scope from the current validation generation. Look a
 * scope up in `summaries` with `validationScopeKey(scope)`.
 */
export function useValidationSummaries(scopes: ValidationScope[]) {
  const envelope = useQuery({
    queryKey: kitQueryKeys.validationEnvelope(),
    queryFn: ({ signal }) => getPublicValidate({ signal }),
  });

  const summaries = useValidationScopeSummaries(envelope.data?.generation ?? null, scopes);

  return {
    summaries: summaries.summaries,
    generation: envelope.data?.generation ?? null,
    isLoading: envelope.isLoading || summaries.isLoading,
    error: envelope.error ?? summaries.error,
  };
}
