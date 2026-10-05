export type SearchScope = "all" | "notes" | "code";

export type SearchFilters = {
  scope: SearchScope;
  noteType: string | null;
  folder: string | null;
};

export const DEFAULT_SEARCH_FILTERS: SearchFilters = {
  scope: "all",
  noteType: null,
  folder: null,
};

export function normalizeSearchQuery(query: string): string {
  return query.trim().replace(/\s+/g, " ");
}

export function normalizeSearchFilters(filters?: Partial<SearchFilters>): SearchFilters {
  const noteType = filters?.noteType?.trim() || null;

  const requestedScope =
    filters?.scope === "notes" || filters?.scope === "code" ? filters.scope : "all";

  const scope = noteType && requestedScope !== "code" ? "notes" : requestedScope;

  return {
    scope,
    noteType: scope === "code" ? null : noteType,
    folder: filters?.folder?.trim().replace(/^\/+|\/+$/g, "") || null,
  };
}

export function searchTabIdentity(query: string, filters?: Partial<SearchFilters>): string {
  const normalizedQuery = normalizeSearchQuery(query).toLocaleLowerCase();
  const effective = normalizeSearchFilters(filters);

  return `search:${encodeURIComponent(JSON.stringify([normalizedQuery, effective]))}`;
}

export function searchTabLabel(query: string): string {
  return normalizeSearchQuery(query) || "Search";
}
