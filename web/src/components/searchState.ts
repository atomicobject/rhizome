import { useEffectEvent, useLayoutEffect, useRef } from "react";
import type { SearchTab } from "./useNoteTabs";

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

export function useSearchScrollRestoration(tab: SearchTab, active: boolean, ready: boolean) {
  const scrollRef = useRef<HTMLElement>(null);

  const restoreScroll = useEffectEvent(() => {
    if (scrollRef.current) scrollRef.current.scrollTop = tab.scrollTop;
  });

  // Restore on activation or refinement, after content loads. Scroll events only save
  // position: replaying them can rewind native scrolling before React catches up.
  useLayoutEffect(() => {
    if (active && ready) restoreScroll();
  }, [
    active,
    ready,
    tab.id,
    tab.query,
    tab.filters.folder,
    tab.filters.noteType,
    tab.filters.scope,
  ]);

  return scrollRef;
}
