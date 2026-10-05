import { noteTabID, locationNodeRef } from "./noteTabIdentity";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import type { ViewCatalogEntry, NodeRef } from "../api/types";
import {
  legacyStoredTabsForVault,
  parseStoredTabs,
  serializeTabs,
  storedTabsForVault,
} from "./noteTabStorage";
import {
  buildNotesLocation,
  normalizeNotePath,
  normalizeSearchTabID,
  parseNotesLocation,
  splitNoteTarget,
  type NotesLocation,
} from "./notesRoute";
import { notifyLocationChange, useLocationSnapshot } from "./locationStore";
import {
  DEFAULT_SEARCH_FILTERS,
  normalizeSearchFilters,
  normalizeSearchQuery,
  searchTabIdentity,
  type SearchFilters,
} from "./searchState";

export type HomeTab = { id: "home"; kind: "home" };

export type CollectionKind = "issues" | "modified";

type ProblemsTab = {
  id: "collection:issues";
  kind: "collection";
  collection: "issues";
  issueScope: NotesLocation["issueScope"];
  issueKey: NotesLocation["issueKey"];
};

type ModifiedTab = {
  id: "collection:modified";
  kind: "collection";
  collection: "modified";
};

export type CollectionTab = ProblemsTab | ModifiedTab;

export type NoteTab = {
  id: string;
  kind: "note";
  path: string;
  dirty: boolean;
  title?: string;
  query?: string | null;
  fragment?: string | null;
  nodeRef?: NodeRef;
  presentation?: string | null;
  nodeId?: string | null;
  nodeKind?: string | null;
  structural?: string | null;
};

export type SearchTab = {
  id: string;
  kind: "search";
  query: string;
  filters: SearchFilters;
  scrollTop: number;
};

export type ViewTab = {
  id: string;
  kind: "view";
  viewId: string;
  title: string;
};

export type Tab = HomeTab | CollectionTab | NoteTab | SearchTab | ViewTab;

export type OpenMode = "activate" | "beside";

/** ⌘/Ctrl-click opens beside the current tab, as links do elsewhere in the app. */
export function openModeFor(event: { metaKey: boolean; ctrlKey: boolean }): OpenMode {
  return event.metaKey || event.ctrlKey ? "beside" : "activate";
}

export type NoteTabsApi = {
  tabs: Tab[];
  activeId: string;
  activeTab: Tab;
  homeLocation: Pick<NotesLocation, "selection" | "presentation">;
  open: (
    target: string,
    opts?: {
      nodeRef?: NodeRef;
      mode?: OpenMode;
      presentation?: string | null;
      nodeId?: string | null;
      nodeKind?: string | null;
      structural?: string | null;
    },
  ) => void;
  openCollection: (collection: CollectionKind) => void;
  openView: (view: ViewCatalogEntry) => void;
  openSearch: (query: string, filters?: Partial<SearchFilters>) => void;
  refineSearch: (id: string, filters: Partial<SearchFilters>) => void;
  setSearchScroll: (id: string, scrollTop: number) => void;
  close: (id: string) => void;
  activate: (id: string) => void;
  markDirty: (path: string, dirty: boolean) => void;
  setTitle: (id: string, title: string) => void;
  setPresentation: (id: string, presentation: string | null) => void;
  setFocusedTarget: (id: string, target: string, requestedTarget: string) => void;
  findByPath: (path: string) => NoteTab | undefined;
};

const HOME_TAB: HomeTab = { id: "home", kind: "home" };

const SEARCH_TAB_SEQUENCE_KEY = "rhizome:notes:search-tab-sequence";

let fallbackSearchTabSequence = 1;

type OpenIntoResult = {
  tabs: Tab[];
  activeId: string;
};

function makeNoteTab(
  path: string,
  query: string | null = null,
  fragment: string | null = null,
  title?: string,
  ref?: NodeRef,
): NoteTab {
  const tab: NoteTab = {
    id: noteTabID(path, ref),
    kind: "note",
    path,
    dirty: false,
  };

  if (ref) {
    tab.nodeRef = ref;
    tab.nodeId = ref.nodeId;
    tab.nodeKind = ref.kind;
    tab.structural = ref.structuralFingerprint;
  }

  if (title !== undefined) tab.title = title;

  if (query) tab.query = query;

  if (fragment) tab.fragment = fragment;

  return tab;
}

function numericSearchTabSequence(id: string): number | null {
  const match = /^search-tab:([1-9]\d*)$/.exec(id);

  if (!match) return null;
  const value = Number(match[1]);

  return Number.isSafeInteger(value) ? value : null;
}

function storedSearchTabSequence(): number | null {
  try {
    const value = Number(window.sessionStorage.getItem(SEARCH_TAB_SEQUENCE_KEY));

    return Number.isSafeInteger(value) && value > 0 ? value : 1;
  } catch {
    return null;
  }
}

function reserveSearchTabID(id: string): void {
  const sequence = numericSearchTabSequence(id);

  if (sequence === null) return;
  const next = sequence + 1;
  fallbackSearchTabSequence = Math.max(fallbackSearchTabSequence, next);

  try {
    const stored = storedSearchTabSequence();

    if (stored !== null && next > stored) {
      window.sessionStorage.setItem(SEARCH_TAB_SEQUENCE_KEY, String(next));
    }
  } catch {
    // Session storage is optional; the process-local high-water mark still avoids live reuse.
  }
}

function nextSearchTabID(tabs: Tab[]): string {
  const openHighWater = tabs.reduce((highest, tab) => {
    const sequence = numericSearchTabSequence(tab.id);

    return sequence === null ? highest : Math.max(highest, sequence + 1);
  }, 1);

  const stored = storedSearchTabSequence();
  let sequence = Math.max(stored ?? 1, fallbackSearchTabSequence, openHighWater);

  while (tabs.some((tab) => tab.id === `search-tab:${sequence}`)) sequence += 1;
  const id = `search-tab:${sequence}`;
  reserveSearchTabID(id);

  return id;
}

function makeSearchTab(
  tabs: Tab[],
  query: string,
  filters?: Partial<SearchFilters>,
  id = nextSearchTabID(tabs),
): SearchTab {
  reserveSearchTabID(id);
  const normalizedQuery = normalizeSearchQuery(query);
  const effectiveFilters = normalizeSearchFilters(filters);

  return {
    id,
    kind: "search",
    query: normalizedQuery,
    filters: effectiveFilters,
    scrollTop: 0,
  };
}

type ProblemsState = Pick<NotesLocation, "issueScope" | "issueKey">;

function collectionTab(collection: CollectionKind, problems?: ProblemsState): CollectionTab {
  if (collection === "issues") {
    return {
      id: "collection:issues",
      kind: "collection",
      collection,
      issueScope: problems?.issueScope ?? null,
      issueKey: problems?.issueKey ?? null,
    };
  }

  return { id: "collection:modified", kind: "collection", collection };
}

function collectionForSelection(selection: NotesLocation["selection"]): CollectionKind | null {
  if (selection.kind === "issues") return "issues";

  if (selection.kind === "modified") return "modified";

  return null;
}

function collectionAvailable(
  collection: CollectionKind,
  changesAvailable: boolean | null,
): boolean {
  return collection === "issues" || changesAvailable !== false;
}

function isNoteTab(tab: Tab): tab is NoteTab {
  return tab.kind === "note";
}

function isSearchTab(tab: Tab): tab is SearchTab {
  return tab.kind === "search";
}

function isCollectionTab(tab: Tab): tab is CollectionTab {
  return tab.kind === "collection";
}

function isViewTab(tab: Tab): tab is ViewTab {
  return tab.kind === "view";
}

function viewTabID(viewId: string): string {
  return `view:${viewId}`;
}

function openViewInto(tabs: Tab[], viewId: string, title?: string): OpenIntoResult {
  const id = viewTabID(viewId);
  const existing = tabs.find((tab) => tab.id === id);

  if (existing) {
    if (!title || !isViewTab(existing) || existing.title === title) {
      return { tabs, activeId: id };
    }

    return {
      tabs: tabs.map((tab) => (tab.id === id ? { ...existing, title } : tab)),
      activeId: id,
    };
  }

  const next: ViewTab = { id, kind: "view", viewId, title: title || viewId };

  return { tabs: [...tabs, next], activeId: id };
}

function viewLocation(tab: ViewTab): string {
  return buildNotesLocation({ selection: { kind: "all" }, view: tab.viewId });
}

function noteIndex(tabs: Tab[], path: string, ref?: NodeRef): number {
  const normalizedPath = normalizeNotePath(path);

  return tabs.findIndex((tab) => isNoteTab(tab) && tab.id === noteTabID(normalizedPath, ref));
}

function activeIdForLocation(tabs: Tab[], location: NotesLocation): string {
  const { note, search, searchFilters, searchTabID } = location;

  if (note) {
    const normalizedPath = normalizeNotePath(note);

    return (
      tabs.find(
        (tab) => isNoteTab(tab) && tab.id === noteTabID(normalizedPath, locationNodeRef(location)),
      )?.id || HOME_TAB.id
    );
  }

  if (search) {
    const stableID = normalizeSearchTabID(searchTabID);

    if (stableID && tabs.some((tab) => isSearchTab(tab) && tab.id === stableID)) {
      return stableID;
    }

    const identity = searchTabIdentity(search, searchFilters || DEFAULT_SEARCH_FILTERS);

    return (
      tabs.find((tab) => isSearchTab(tab) && searchTabIdentity(tab.query, tab.filters) === identity)
        ?.id || HOME_TAB.id
    );
  }

  if (location.view) {
    const id = viewTabID(location.view);

    if (tabs.some((tab) => tab.id === id)) return id;
  }

  const collection = collectionForSelection(location.selection);

  if (collection) {
    return (
      tabs.find((tab) => isCollectionTab(tab) && tab.collection === collection)?.id || HOME_TAB.id
    );
  }

  return HOME_TAB.id;
}

function openCollectionInto(
  tabs: Tab[],
  collection: CollectionKind,
  problems?: ProblemsState,
): OpenIntoResult {
  const existing = tabs.find(
    (tab): tab is CollectionTab => isCollectionTab(tab) && tab.collection === collection,
  );

  if (existing) {
    if (existing.collection !== "issues" || problems === undefined) {
      return { tabs, activeId: existing.id };
    }

    const issueScope = problems.issueScope ?? null;
    const issueKey = problems.issueKey ?? null;

    if (
      existing.issueScope?.kind === issueScope?.kind &&
      existing.issueScope?.key === issueScope?.key &&
      existing.issueKey === issueKey
    ) {
      return { tabs, activeId: existing.id };
    }

    return {
      tabs: tabs.map((tab) =>
        tab.id === existing.id ? { ...existing, issueScope, issueKey } : tab,
      ),
      activeId: existing.id,
    };
  }

  const next = collectionTab(collection, problems);

  return { tabs: [...tabs, next], activeId: next.id };
}

function collectionLocation(tab: CollectionTab): string {
  return buildNotesLocation({
    selection: { kind: tab.collection },
    issueScope: tab.collection === "issues" ? tab.issueScope : null,
    issueKey: tab.collection === "issues" ? tab.issueKey : null,
  });
}

function tabLocation(tab: Tab, selection: NotesLocation["selection"]): string {
  switch (tab.kind) {
    case "home":
      return buildNotesLocation({ selection });
    case "collection":
      return collectionLocation(tab);
    case "view":
      return viewLocation(tab);
    case "note":
      return buildNotesLocation({
        selection,
        note: tab.path,
        query: tab.query,
        fragment: tab.fragment,
        presentation: tab.presentation,
        nodeId: tab.nodeId,
        nodeKind: tab.nodeKind,
        structural: tab.structural,
      });
    case "search":
      return buildNotesLocation({
        selection,
        search: tab.query,
        scope: tab.filters.scope,
        noteType: tab.filters.noteType,
        folder: tab.filters.folder,
        searchTabID: tab.id,
      });
    default: {
      const exhaustive: never = tab;

      return exhaustive;
    }
  }
}

function tabsForLocation(
  location: NotesLocation,
  changesAvailable: boolean | null,
  stored: Array<NoteTab | SearchTab | ViewTab> = [],
): Tab[] {
  const collection = collectionForSelection(location.selection);

  const base =
    collection && collectionAvailable(collection, changesAvailable)
      ? openCollectionInto([HOME_TAB], collection, collection === "issues" ? location : undefined)
          .tabs
      : [HOME_TAB];

  const restored = [...base, ...stored];
  const withView = location.view ? openViewInto(restored, location.view).tabs : restored;

  if (location.note)
    return openInto(
      withView,
      HOME_TAB.id,
      targetString(location.note, location.query, location.fragment),
      "activate",
      locationNodeRef(location),
    ).tabs;

  if (location.search) {
    return openSearchInto(withView, location.search, location.searchFilters, location.searchTabID)
      .tabs;
  }

  return withView;
}

function sameTabs(left: Tab[], right: Tab[]): boolean {
  if (left.length !== right.length) return false;

  return left.every((tab, index) => {
    const other = right[index];

    if (tab.kind !== other.kind || tab.id !== other.id) return false;

    if (tab.kind === "home" || other.kind === "home") return true;

    if (isCollectionTab(tab) && isCollectionTab(other)) {
      if (tab.collection !== other.collection) return false;

      if (tab.collection === "modified" || other.collection === "modified") return true;

      return (
        tab.issueScope?.kind === other.issueScope?.kind &&
        tab.issueScope?.key === other.issueScope?.key &&
        tab.issueKey === other.issueKey
      );
    }

    if (isViewTab(tab) && isViewTab(other)) {
      return tab.viewId === other.viewId && tab.title === other.title;
    }

    if (isSearchTab(tab) && isSearchTab(other)) {
      return (
        tab.query === other.query &&
        tab.filters.scope === other.filters.scope &&
        tab.filters.noteType === other.filters.noteType &&
        tab.filters.folder === other.filters.folder &&
        tab.scrollTop === other.scrollTop
      );
    }

    if (!isNoteTab(tab) || !isNoteTab(other)) return false;

    return (
      tab.path === other.path &&
      tab.dirty === other.dirty &&
      tab.title === other.title &&
      tab.query === other.query &&
      tab.fragment === other.fragment &&
      tab.presentation === other.presentation &&
      tab.nodeId === other.nodeId &&
      tab.nodeKind === other.nodeKind &&
      tab.structural === other.structural
    );
  });
}

function mergeInitialTabs(
  current: Tab[],
  stored: Array<NoteTab | SearchTab | ViewTab>,
  location: NotesLocation,
): Tab[] {
  const merged: Tab[] = [HOME_TAB];
  const paths = new Set<string>();
  const searches = new Set<string>();
  const collections = new Set<CollectionKind>();
  const tabIDs = new Set<string>([HOME_TAB.id]);

  const append = (tab: NoteTab) => {
    if (paths.has(tab.id)) return;
    paths.add(tab.id);
    tabIDs.add(tab.id);
    merged.push(tab);
  };

  const appendSearch = (tab: SearchTab) => {
    const identity = searchTabIdentity(tab.query, tab.filters);

    if (searches.has(identity)) return;
    const nextTab = tabIDs.has(tab.id) ? { ...tab, id: nextSearchTabID(merged) } : tab;
    searches.add(identity);
    tabIDs.add(nextTab.id);
    merged.push(nextTab);
  };

  const appendCollection = (tab: CollectionTab) => {
    if (collections.has(tab.collection)) return;
    collections.add(tab.collection);
    merged.push(tab);
  };

  const appendView = (tab: ViewTab) => {
    if (tabIDs.has(tab.id)) return;
    tabIDs.add(tab.id);
    merged.push(tab);
  };

  for (const tab of current) {
    if (isNoteTab(tab)) append(tab);
    else if (isSearchTab(tab)) appendSearch(tab);
    else if (isCollectionTab(tab)) appendCollection(tab);
    else if (isViewTab(tab)) appendView(tab);
  }

  for (const tab of stored) {
    if (isNoteTab(tab)) append(tab);
    else if (isSearchTab(tab)) appendSearch(tab);
    else if (isViewTab(tab)) appendView(tab);
  }

  const withView = location.view ? openViewInto(merged, location.view).tabs : merged;

  if (
    location.note &&
    !paths.has(noteTabID(normalizeNotePath(location.note), locationNodeRef(location)))
  ) {
    return openInto(
      withView,
      HOME_TAB.id,
      targetString(location.note, location.query, location.fragment),
      "activate",
      locationNodeRef(location),
    ).tabs;
  }

  if (
    location.search &&
    !searches.has(searchTabIdentity(location.search, location.searchFilters))
  ) {
    return openSearchInto(withView, location.search, location.searchFilters, location.searchTabID)
      .tabs;
  }

  return withView;
}

function nextActiveAfterClose(tabs: Tab[], closedIndex: number): string {
  if (closedIndex < 1 || closedIndex >= tabs.length) return HOME_TAB.id;

  return tabs[closedIndex + 1]?.id || tabs[closedIndex - 1]?.id || HOME_TAB.id;
}

function openInto(
  tabs: Tab[],
  activeId: string,
  path: string,
  mode: OpenMode,
  ref?: NodeRef,
): OpenIntoResult {
  const parsed = splitNoteTarget(path);
  const normalizedPath = normalizeNotePath(parsed.path);

  if (!normalizedPath) return { tabs, activeId };
  const existingIndex = noteIndex(tabs, normalizedPath, ref);

  if (existingIndex >= 0) {
    const existing = tabs[existingIndex];

    if (!isNoteTab(existing)) return { tabs, activeId };

    const nextTabs = withTarget(
      tabs,
      normalizedPath,
      parsed.query,
      parsed.fragment,
      parsed.query === null && parsed.fragment !== null,
      existing.id,
    );

    return {
      tabs: sameTabs(nextTabs, tabs) ? tabs : nextTabs,
      activeId: mode === "beside" ? activeId : existing.id,
    };
  }

  const nextTab = makeNoteTab(normalizedPath, parsed.query, parsed.fragment, undefined, ref);

  if (mode === "beside") {
    const activeIndex = tabs.findIndex((tab) => tab.id === activeId);
    const insertAt = activeIndex < 0 ? tabs.length : activeIndex + 1;

    return {
      tabs: [...tabs.slice(0, insertAt), nextTab, ...tabs.slice(insertAt)],
      activeId,
    };
  }

  return { tabs: [...tabs, nextTab], activeId: nextTab.id };
}

function openSearchInto(
  tabs: Tab[],
  query: string,
  filters?: Partial<SearchFilters>,
  preferredID?: string | null,
): OpenIntoResult {
  const normalizedQuery = normalizeSearchQuery(query);
  const effectiveFilters = normalizeSearchFilters(filters);
  const stableID = normalizeSearchTabID(preferredID);

  if (stableID) reserveSearchTabID(stableID);

  if (stableID) {
    const preferredIndex = tabs.findIndex((tab) => isSearchTab(tab) && tab.id === stableID);

    if (preferredIndex >= 0) {
      const preferred = tabs[preferredIndex];

      if (isSearchTab(preferred)) {
        const changed =
          preferred.query !== normalizedQuery ||
          preferred.filters.scope !== effectiveFilters.scope ||
          preferred.filters.noteType !== effectiveFilters.noteType ||
          preferred.filters.folder !== effectiveFilters.folder;

        if (!changed) return { tabs, activeId: stableID };

        return {
          tabs: tabs.map((tab, index) =>
            index === preferredIndex
              ? { ...preferred, query: normalizedQuery, filters: effectiveFilters, scrollTop: 0 }
              : tab,
          ),
          activeId: stableID,
        };
      }
    }
  }

  const identity = searchTabIdentity(normalizedQuery, effectiveFilters);

  const existingIndex = tabs.findIndex(
    (tab) => tab.kind === "search" && searchTabIdentity(tab.query, tab.filters) === identity,
  );

  if (existingIndex >= 0) {
    return { tabs, activeId: tabs[existingIndex]?.id || HOME_TAB.id };
  }

  const nextTab = makeSearchTab(tabs, normalizedQuery, effectiveFilters, stableID || undefined);

  return { tabs: [...tabs, nextTab], activeId: nextTab.id };
}

function noteForId(tabs: Tab[], id: string): NoteTab | undefined {
  const tab = tabs.find((candidate) => candidate.id === id);

  return tab && isNoteTab(tab) ? tab : undefined;
}

function withTarget(
  tabs: Tab[],
  path: string,
  query: string | null,
  fragment: string | null,
  preserveQuery = false,
  tabID = noteTabID(path),
): Tab[] {
  const targetQuery = (tab: NoteTab) =>
    preserveQuery && query === null ? (tab.query ?? null) : query;

  return tabs.map((tab) =>
    tab.kind === "note" &&
    tab.id === tabID &&
    ((tab.query ?? null) !== targetQuery(tab) || (tab.fragment ?? null) !== fragment)
      ? { ...tab, query: targetQuery(tab), fragment }
      : tab,
  );
}

function withPresentation(
  tabs: Tab[],
  tabID: string,
  value: Pick<NoteTab, "nodeRef" | "presentation" | "nodeId" | "nodeKind" | "structural">,
): Tab[] {
  return tabs.map((tab) =>
    tab.kind === "note" && tab.id === tabID
      ? {
          ...tab,
          nodeRef:
            value.nodeRef &&
            value.nodeRef.nodeId === tab.nodeRef?.nodeId &&
            value.nodeRef.kind === tab.nodeRef?.kind
              ? { ...tab.nodeRef, ...value.nodeRef }
              : value.nodeRef,
          presentation:
            value.presentation === undefined ? (tab.presentation ?? null) : value.presentation,
          nodeId: value.nodeId ?? value.nodeRef?.nodeId ?? null,
          nodeKind: value.nodeKind ?? value.nodeRef?.kind ?? null,
          structural: value.structural ?? value.nodeRef?.structuralFingerprint,
        }
      : tab,
  );
}

function tabTarget(tab: NoteTab): string {
  return targetString(tab.path, tab.query ?? null, tab.fragment ?? null);
}

function targetString(path: string, query: string | null, fragment: string | null): string {
  const documentQuery = query ? `?${query}` : "";
  const documentFragment = fragment ? `#${fragment}` : "";

  return `${path}${documentQuery}${documentFragment}`;
}

function currentLocationValue(): string {
  return `${window.location.pathname}${window.location.search}${window.location.hash}`;
}

export function useNoteTabs(
  vaultName: string | null,
  changesAvailable: boolean | null = false,
): NoteTabsApi {
  const snapshot = useLocationSnapshot();

  const location = useMemo<NotesLocation>(
    () => parseNotesLocation(snapshot.pathname, snapshot.search, snapshot.hash),
    [snapshot.hash, snapshot.pathname, snapshot.search],
  );

  const [tabs, setTabs] = useState<Tab[]>(() => tabsForLocation(location, changesAvailable));
  const tabsRef = useRef(tabs);

  const homeLocationRef = useRef<Pick<NotesLocation, "selection" | "presentation">>(
    collectionForSelection(location.selection) || location.view
      ? { selection: { kind: "all" } }
      : {
          selection: location.selection,
          presentation: location.note ? null : location.presentation,
        },
  );

  const hydratedVaultRef = useRef<string | null>(null);
  const hasHydratedRef = useRef(false);
  const preHydrationInteractionRef = useRef(false);
  const preHydrationClosedPathsRef = useRef(new Set<string>());
  const preHydrationClosedTabIDsRef = useRef(new Set<string>());
  const preHydrationClosedSearchIdentitiesRef = useRef(new Set<string>());
  const [hydratedVault, setHydratedVault] = useState<string | null>(null);

  tabsRef.current = tabs;

  if (!collectionForSelection(location.selection) && !location.view) {
    homeLocationRef.current = {
      selection: location.selection,
      presentation: location.note ? homeLocationRef.current?.presentation : location.presentation,
    };
  }

  const commitTabs = useCallback(
    (nextTabs: Tab[], userInteraction = false) => {
      if (userInteraction && !hasHydratedRef.current && vaultName === null) {
        preHydrationInteractionRef.current = true;

        for (const tab of tabsRef.current) {
          if (isNoteTab(tab) && !nextTabs.some((next) => next.id === tab.id)) {
            preHydrationClosedPathsRef.current.add(tab.id);
          }

          if (isSearchTab(tab) && !nextTabs.some((next) => next.id === tab.id)) {
            preHydrationClosedTabIDsRef.current.add(tab.id);
            preHydrationClosedSearchIdentitiesRef.current.add(
              searchTabIdentity(tab.query, tab.filters),
            );
          }

          if (isViewTab(tab) && !nextTabs.some((next) => next.id === tab.id)) {
            preHydrationClosedTabIDsRef.current.add(tab.id);
          }
        }
      }

      tabsRef.current = nextTabs;
      setTabs(nextTabs);
    },
    [vaultName],
  );

  const writeLocation = useCallback((next: string, replace = false) => {
    if (next === currentLocationValue()) return;
    const method = replace ? "replaceState" : "pushState";
    window.history[method]({}, "", next);
    notifyLocationChange();
  }, []);

  useEffect(() => {
    if (vaultName === null) {
      if (hydratedVaultRef.current !== null) {
        hydratedVaultRef.current = null;
        setHydratedVault(null);
        commitTabs(tabsForLocation(location, changesAvailable));
      }

      return;
    }

    if (hydratedVaultRef.current === vaultName) return;

    let stored: Array<NoteTab | SearchTab | ViewTab> = [];

    try {
      const currentKey = storedTabsForVault(vaultName);
      const currentRaw = window.sessionStorage.getItem(currentKey);
      const legacyKey = legacyStoredTabsForVault(vaultName);
      const legacyRaw = window.sessionStorage.getItem(legacyKey);
      stored = parseStoredTabs(currentRaw || legacyRaw);

      if (!currentRaw && legacyRaw !== null) {
        window.sessionStorage.setItem(currentKey, serializeTabs([HOME_TAB, ...stored]));
        window.sessionStorage.removeItem(legacyKey);
      }
    } catch {
      // Keep tabs already read if the optional migration write or cleanup fails.
    }

    const nextTabs =
      !hasHydratedRef.current && preHydrationInteractionRef.current
        ? mergeInitialTabs(
            tabsRef.current,
            stored.filter((tab) => {
              if (isNoteTab(tab)) return !preHydrationClosedPathsRef.current.has(tab.id);

              if (preHydrationClosedTabIDsRef.current.has(tab.id)) return false;

              return (
                !isSearchTab(tab) ||
                !preHydrationClosedSearchIdentitiesRef.current.has(
                  searchTabIdentity(tab.query, tab.filters),
                )
              );
            }),
            location,
          )
        : tabsForLocation(location, changesAvailable, stored);

    // Initial node reads may finish before the vault status read. Keep their
    // live titles while restoring stored tab order.
    const hydratedTabs = hasHydratedRef.current
      ? nextTabs
      : nextTabs.map((tab) => {
          if (!isNoteTab(tab)) return tab;
          const title = noteForId(tabsRef.current, tab.id)?.title;

          return title === undefined ? tab : { ...tab, title };
        });

    hydratedVaultRef.current = vaultName;
    hasHydratedRef.current = true;
    preHydrationInteractionRef.current = false;
    preHydrationClosedPathsRef.current.clear();
    preHydrationClosedTabIDsRef.current.clear();
    preHydrationClosedSearchIdentitiesRef.current.clear();
    commitTabs(
      location.note
        ? withTarget(
            hydratedTabs,
            normalizeNotePath(location.note),
            location.query,
            location.fragment,
            false,
            noteTabID(normalizeNotePath(location.note), locationNodeRef(location)),
          )
        : hydratedTabs,
    );
    setHydratedVault(vaultName);
  }, [changesAvailable, commitTabs, location, vaultName]);

  useEffect(() => {
    const selectedCollection = collectionForSelection(location.selection);
    const requested = location.note || location.search ? null : selectedCollection;

    let next = tabsRef.current.filter(
      (tab) => !isCollectionTab(tab) || collectionAvailable(tab.collection, changesAvailable),
    );

    if (requested && collectionAvailable(requested, changesAvailable)) {
      next = openCollectionInto(
        next,
        requested,
        requested === "issues"
          ? { issueScope: location.issueScope, issueKey: location.issueKey }
          : undefined,
      ).tabs;
    }

    if (!sameTabs(next, tabsRef.current)) commitTabs(next);

    if (selectedCollection === "modified" && changesAvailable === false) {
      writeLocation(buildNotesLocation(homeLocationRef.current), true);
    }
  }, [
    changesAvailable,
    commitTabs,
    location.issueKey,
    location.issueScope,
    location.note,
    location.search,
    location.selection,
    writeLocation,
  ]);

  useEffect(() => {
    if (vaultName === null || hydratedVault !== vaultName) return;

    try {
      window.sessionStorage.setItem(storedTabsForVault(vaultName), serializeTabs(tabs));
    } catch {
      // Session storage is optional and can be unavailable in private contexts.
    }
  }, [hydratedVault, tabs, vaultName]);

  useEffect(() => {
    if (!location.note) return;

    const result = openInto(
      tabsRef.current,
      HOME_TAB.id,
      targetString(location.note, location.query, location.fragment),
      "activate",
      locationNodeRef(location),
    );

    const targeted = withTarget(
      result.tabs,
      normalizeNotePath(location.note),
      location.query,
      location.fragment,
      false,
      noteTabID(normalizeNotePath(location.note), locationNodeRef(location)),
    );

    const next = withPresentation(
      targeted,
      noteTabID(normalizeNotePath(location.note), locationNodeRef(location)),
      {
        ...location,
        presentation: location.presentation ?? null,
        nodeRef: location.nodeKind
          ? {
              notePath: location.note,
              kind: location.nodeKind,
              nodeId: location.nodeId ?? undefined,
              fragment: location.fragment ?? undefined,
              structuralFingerprint: location.structural ?? undefined,
            }
          : undefined,
      },
    );

    if (!sameTabs(next, tabsRef.current)) commitTabs(next);
  }, [
    commitTabs,
    location.fragment,
    location.note,
    location.query,
    location.presentation,
    location.nodeId,
    location.nodeKind,
    location.structural,
  ]);

  useEffect(() => {
    if (!location.view) return;
    const result = openViewInto(tabsRef.current, location.view);

    if (!sameTabs(result.tabs, tabsRef.current)) commitTabs(result.tabs);
  }, [commitTabs, location.view]);

  useEffect(() => {
    if (!location.search) return;

    const result = openSearchInto(
      tabsRef.current,
      location.search,
      location.searchFilters,
      location.searchTabID,
    );

    if (!sameTabs(result.tabs, tabsRef.current)) commitTabs(result.tabs);

    if (!location.searchTabID) {
      const filters = location.searchFilters ?? DEFAULT_SEARCH_FILTERS;

      const canonicalLocation = buildNotesLocation({
        selection: location.selection,
        search: location.search,
        scope: filters.scope,
        noteType: filters.noteType,
        folder: filters.folder,
        searchTabID: result.activeId,
      });

      if (canonicalLocation !== currentLocationValue()) {
        window.history.replaceState({}, "", canonicalLocation);
        notifyLocationChange();
      }
    }
  }, [
    commitTabs,
    location.search,
    location.searchFilters,
    location.searchTabID,
    location.selection,
  ]);

  const open = useCallback(
    (
      target: string,
      opts?: {
        nodeRef?: NodeRef;
        mode?: OpenMode;
        presentation?: string | null;
        nodeId?: string | null;
        nodeKind?: string | null;
        structural?: string | null;
      },
    ) => {
      const parsed = splitNoteTarget(target);
      const path = normalizeNotePath(parsed.path);

      if (!path) return;
      const mode = opts?.mode || "activate";

      const ref =
        opts?.nodeRef ??
        locationNodeRef({
          note: path,
          fragment: parsed.fragment,
          nodeKind: opts?.nodeKind,
          nodeId: opts?.nodeId,
          structural: opts?.structural,
        });

      const { tabs: openedTabs } = openInto(
        tabsRef.current,
        activeIdForLocation(tabsRef.current, location),
        targetString(path, parsed.query, parsed.fragment),
        mode,
        ref,
      );

      const nextTabs = withPresentation(openedTabs, noteTabID(path, ref), {
        ...opts,
        nodeRef: ref,
      });

      commitTabs(nextTabs, true);

      if (mode === "beside") return;

      const opened = nextTabs.find(
        (tab): tab is NoteTab => isNoteTab(tab) && tab.id === noteTabID(path, ref),
      );

      writeLocation(
        buildNotesLocation({
          selection: location.selection,
          note: path,
          query: opened?.query ?? parsed.query,
          fragment: parsed.fragment,
          presentation: opened?.presentation,
          nodeId: opened?.nodeId,
          nodeKind: opened?.nodeKind,
          structural: opened?.structural,
        }),
      );
    },
    [commitTabs, location, writeLocation],
  );

  const openSearch = useCallback(
    (query: string, filters?: Partial<SearchFilters>) => {
      const normalizedQuery = normalizeSearchQuery(query);

      if (!normalizedQuery) return;
      const effectiveFilters = normalizeSearchFilters(filters);
      const result = openSearchInto(tabsRef.current, normalizedQuery, effectiveFilters);
      commitTabs(result.tabs, true);
      writeLocation(
        buildNotesLocation({
          selection: location.selection,
          search: normalizedQuery,
          scope: effectiveFilters.scope,
          noteType: effectiveFilters.noteType,
          folder: effectiveFilters.folder,
          searchTabID: result.activeId,
        }),
      );
    },
    [commitTabs, location.selection, writeLocation],
  );

  const openCollection = useCallback(
    (collection: CollectionKind) => {
      if (collection === "modified" && changesAvailable !== true) return;
      const result = openCollectionInto(tabsRef.current, collection);
      commitTabs(result.tabs, true);

      const tab = result.tabs.find(
        (candidate): candidate is CollectionTab =>
          isCollectionTab(candidate) && candidate.collection === collection,
      );

      if (tab) writeLocation(collectionLocation(tab));
    },
    [changesAvailable, commitTabs, writeLocation],
  );

  const openView = useCallback(
    (view: ViewCatalogEntry) => {
      const result = openViewInto(tabsRef.current, view.id, view.name);
      commitTabs(result.tabs, true);

      const tab = result.tabs.find(
        (candidate): candidate is ViewTab =>
          isViewTab(candidate) && candidate.id === result.activeId,
      );

      if (tab) writeLocation(viewLocation(tab));
    },
    [commitTabs, writeLocation],
  );

  const refineSearch = useCallback(
    (id: string, filters: Partial<SearchFilters>) => {
      const currentTabs = tabsRef.current;
      const index = currentTabs.findIndex((tab) => tab.id === id);
      const current = currentTabs[index];

      if (index < 0 || !current || !isSearchTab(current)) return;
      const effectiveFilters = normalizeSearchFilters(filters);
      const identity = searchTabIdentity(current.query, effectiveFilters);

      const collision = currentTabs.find(
        (tab) =>
          isSearchTab(tab) &&
          tab.id !== id &&
          searchTabIdentity(tab.query, tab.filters) === identity,
      );

      const nextTabs = collision
        ? currentTabs.filter((tab) => tab.id !== id)
        : currentTabs.map((tab) =>
            tab.id === id && isSearchTab(tab)
              ? { ...tab, filters: effectiveFilters, scrollTop: 0 }
              : tab,
          );

      commitTabs(nextTabs, true);
      writeLocation(
        buildNotesLocation({
          selection: location.selection,
          search: current.query,
          scope: effectiveFilters.scope,
          noteType: effectiveFilters.noteType,
          folder: effectiveFilters.folder,
          searchTabID: collision?.id ?? id,
        }),
      );
    },
    [commitTabs, location.selection, writeLocation],
  );

  const activate = useCallback(
    (id: string) => {
      const tab = tabsRef.current.find((candidate) => candidate.id === id);

      if (!tab || activeIdForLocation(tabsRef.current, location) === id) return;

      const selection =
        tab.kind === "home" ? homeLocationRef.current.selection : location.selection;

      writeLocation(
        tab.kind === "home"
          ? buildNotesLocation(homeLocationRef.current)
          : tabLocation(tab, selection),
      );
    },
    [location, writeLocation],
  );

  const close = useCallback(
    (id: string) => {
      const currentTabs = tabsRef.current;
      const closedIndex = currentTabs.findIndex((tab) => tab.id === id);

      if (closedIndex < 1) return;
      const wasActive = activeIdForLocation(currentTabs, location) === id;
      const nextActiveId = nextActiveAfterClose(currentTabs, closedIndex);
      const nextTabs = currentTabs.filter((_, index) => index !== closedIndex);
      commitTabs(nextTabs, true);

      if (!wasActive) return;
      const nextTab = nextTabs.find((candidate) => candidate.id === nextActiveId);
      const closedTab = currentTabs[closedIndex];

      const selection =
        isCollectionTab(closedTab) || isViewTab(closedTab)
          ? homeLocationRef.current.selection
          : location.selection;

      writeLocation(
        !nextTab || nextTab.kind === "home"
          ? buildNotesLocation(homeLocationRef.current)
          : tabLocation(nextTab, selection),
        true,
      );
    },
    [commitTabs, location, writeLocation],
  );

  const markDirty = useCallback(
    (target: string, dirty: boolean) => {
      const path = normalizeNotePath(splitNoteTarget(target).path);

      if (!path) return;
      const currentTabs = tabsRef.current;

      const nextTabs = currentTabs.map((tab) =>
        isNoteTab(tab) && tab.path === path && tab.dirty !== dirty ? { ...tab, dirty } : tab,
      );

      if (!sameTabs(nextTabs, currentTabs)) commitTabs(nextTabs, true);
    },
    [commitTabs],
  );

  const setTitle = useCallback(
    (id: string, title: string) => {
      const currentTabs = tabsRef.current;
      const index = currentTabs.findIndex((tab) => tab.id === id);
      const tab = currentTabs[index];

      if (index < 0 || !tab || (!isNoteTab(tab) && !isViewTab(tab)) || tab.title === title) return;
      const nextTabs = [...currentTabs];
      nextTabs[index] = { ...tab, title };
      commitTabs(nextTabs);
    },
    [commitTabs],
  );

  const setSearchScroll = useCallback(
    (id: string, scrollTop: number) => {
      const index = tabsRef.current.findIndex((candidate) => candidate.id === id);
      const tab = tabsRef.current[index];

      if (index < 0 || !tab || !isSearchTab(tab) || tab.scrollTop === scrollTop) return;
      const nextTabs = [...tabsRef.current];
      nextTabs[index] = { ...tab, scrollTop };
      commitTabs(nextTabs);
    },
    [commitTabs],
  );

  const setPresentation = useCallback(
    (id: string, presentation: string | null) => {
      const tab = noteForId(tabsRef.current, id);

      if (!tab) return;

      const next = tabsRef.current.map((candidate) =>
        candidate.id === id ? { ...tab, presentation } : candidate,
      );

      commitTabs(next);

      const current = parseNotesLocation(
        window.location.pathname,
        window.location.search,
        window.location.hash,
      );

      if (
        current.note &&
        noteTabID(normalizeNotePath(current.note), locationNodeRef(current)) === tab.id
      )
        writeLocation(buildNotesLocation({ ...current, presentation }), true);
    },
    [commitTabs, writeLocation],
  );

  const setFocusedTarget = useCallback(
    (id: string, target: string, requestedTarget: string) => {
      const tab = noteForId(tabsRef.current, id);

      if (!tab || tabTarget(tab) !== requestedTarget) return;
      const parsed = splitNoteTarget(target);

      if (normalizeNotePath(parsed.path) !== tab.path) return;
      // Focus resolution may only return a canonical fragment. Preserve the
      // active document query when the resolver omits it.
      const query = parsed.query ?? tab.query ?? null;
      const next = withTarget(tabsRef.current, tab.path, query, parsed.fragment, false, id);

      if (!sameTabs(next, tabsRef.current)) commitTabs(next);

      const current = parseNotesLocation(
        window.location.pathname,
        window.location.search,
        window.location.hash,
      );

      if (
        current.note &&
        noteTabID(normalizeNotePath(current.note), locationNodeRef(current)) === tab.id &&
        targetString(tab.path, current.query, current.fragment) === requestedTarget
      ) {
        writeLocation(
          buildNotesLocation({
            selection: current.selection,
            note: tab.path,
            query,
            fragment: parsed.fragment,
            presentation: current.presentation,
            nodeId: current.nodeId,
            nodeKind: current.nodeKind,
            structural: current.structural,
          }),
          true,
        );
      }
    },
    [commitTabs, writeLocation],
  );

  const findByPath = useCallback((target: string) => {
    const path = normalizeNotePath(splitNoteTarget(target).path);

    for (const tab of tabsRef.current) {
      if (isNoteTab(tab) && tab.path === path) return tab;
    }

    return undefined;
  }, []);

  const activeId = activeIdForLocation(tabs, location);

  const activeTab = useMemo(
    () => tabs.find((tab) => tab.id === activeId) || HOME_TAB,
    [activeId, tabs],
  );

  const homeLocation = homeLocationRef.current;

  return useMemo(
    () => ({
      tabs,
      activeId,
      activeTab,
      homeLocation,
      open,
      openCollection,
      openSearch,
      openView,
      refineSearch,
      setSearchScroll,
      close,
      activate,
      markDirty,
      setTitle,
      setFocusedTarget,
      setPresentation,
      findByPath,
    }),
    [
      activeId,
      activeTab,
      activate,
      close,
      findByPath,
      homeLocation,
      markDirty,
      open,
      openCollection,
      openSearch,
      openView,
      refineSearch,
      setSearchScroll,
      setTitle,
      setFocusedTarget,
      setPresentation,
      tabs,
    ],
  );
}
