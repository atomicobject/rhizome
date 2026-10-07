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

/** The folder filter for notes at the vault root, which sit in no folder. */
export const ROOT_FOLDER = "/";

/** A folder filter without surrounding slashes; slashes alone name the vault root. */
function normalizeFolder(folder: string | null | undefined) {
  const trimmed = folder?.trim();

  return trimmed ? trimmed.replace(/^\/+|\/+$/g, "") || ROOT_FOLDER : null;
}

/** Whether a note path lies in a folder: directly at the root for the root folder, else anywhere under it. */
export const inFolder = (path: string, folder: string) =>
  folder === ROOT_FOLDER ? !path.includes("/") : path.startsWith(`${folder}/`);

/** A folder as the workspace names it, with a trailing slash; the root is `/`. */
export const folderLabel = (folder: string) => (folder === ROOT_FOLDER ? "/" : `${folder}/`);

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
    folder: normalizeFolder(filters?.folder),
  };
}

export function searchTabIdentity(query: string, filters?: Partial<SearchFilters>): string {
  const normalizedQuery = normalizeSearchQuery(query).toLocaleLowerCase();
  const effective = normalizeSearchFilters(filters);

  return `search:${encodeURIComponent(JSON.stringify([normalizedQuery, effective]))}`;
}

export function searchTabLabel(query: string, folder?: string | null): string {
  return normalizeSearchQuery(query) || (folder ? folderLabel(folder) : "Search");
}
