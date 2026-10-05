import { noteTabID } from "./noteTabIdentity";
import { isNodeRef, isFiniteNumber, isJsonObject, isString } from "../api/parse";
import { normalizeNotePath } from "./notesRoute";
import { normalizeSearchFilters, searchTabIdentity } from "./searchState";
import type { NodeRef } from "../api/types";
import type { NoteTab, SearchTab, Tab, ViewTab } from "./useNoteTabs";

const STORAGE_VERSION = 3;

const LEGACY_STORAGE_VERSION = 2;

type StoredTabs = {
  v: number;
  tabs: Array<{
    kind?: "note" | "search" | "view";
    id?: string;
    path?: string;
    title?: string;
    fragment?: string | null;
    nodeRef?: NodeRef;
    presentation?: string | null;
    nodeId?: string | null;
    nodeKind?: string | null;
    structural?: string | null;
    query?: string;
    scope?: string;
    noteType?: string | null;
    folder?: string | null;
    scrollTop?: number;
    viewId?: string;
  }>;
};

export function storedTabsForVault(vaultName: string): string {
  return `rhizome:notes:tabs:v${STORAGE_VERSION}:${vaultName}`;
}

export function legacyStoredTabsForVault(vaultName: string): string {
  return `rhizome:notes:tabs:v${LEGACY_STORAGE_VERSION}:${vaultName}`;
}

export function serializeTabs(tabs: Tab[]): string {
  const payload: StoredTabs = {
    v: STORAGE_VERSION,
    tabs: tabs.flatMap((tab) => {
      if (tab.kind === "note") {
        const storedTab: StoredTabs["tabs"][number] = {
          path: tab.path,
        };

        if (tab.title !== undefined) storedTab.title = tab.title;

        if (tab.query) storedTab.query = tab.query;

        if (tab.fragment) storedTab.fragment = tab.fragment;
        storedTab.nodeRef = tab.nodeRef;

        if (tab.presentation) storedTab.presentation = tab.presentation;

        if (tab.nodeId) storedTab.nodeId = tab.nodeId;

        if (tab.nodeKind) storedTab.nodeKind = tab.nodeKind;
        storedTab.structural = tab.structural;
        storedTab.kind = "note";

        return [storedTab];
      }

      if (tab.kind === "view") {
        return [{ kind: "view" as const, viewId: tab.viewId, title: tab.title }];
      }

      if (tab.kind === "search") {
        return [
          {
            kind: "search",
            id: tab.id,
            query: tab.query,
            scope: tab.filters.scope,
            noteType: tab.filters.noteType,
            folder: tab.filters.folder,
            scrollTop: tab.scrollTop,
          },
        ];
      }

      return [];
    }),
  };

  return JSON.stringify(payload);
}

export function parseStoredTabs(raw: string | null): Array<NoteTab | SearchTab | ViewTab> {
  if (!raw) return [];
  let parsed: unknown;

  try {
    parsed = JSON.parse(raw);
  } catch {
    return [];
  }

  if (
    !isJsonObject(parsed) ||
    (parsed.v !== LEGACY_STORAGE_VERSION && parsed.v !== STORAGE_VERSION) ||
    !Array.isArray(parsed.tabs)
  ) {
    return [];
  }

  const tabs: Array<NoteTab | SearchTab | ViewTab> = [];
  const paths = new Set<string>();
  const searchIdentities = new Set<string>();
  const searchIDs = new Set<string>();
  const viewIDs = new Set<string>();

  for (const value of parsed.tabs) {
    if (!isJsonObject(value)) continue;

    if (value.kind === "view") {
      if (!isString(value.viewId) || !value.viewId || viewIDs.has(value.viewId)) continue;
      viewIDs.add(value.viewId);
      tabs.push({
        id: `view:${value.viewId}`,
        kind: "view",
        viewId: value.viewId,
        title: isString(value.title) && value.title ? value.title : value.viewId,
      });
      continue;
    }

    if (value.kind === "search") {
      if (!isString(value.query) || !value.query.trim()) continue;
      const storedScope = value.scope;

      const scope =
        storedScope === "all" || storedScope === "notes" || storedScope === "code"
          ? storedScope
          : undefined;

      const filters = normalizeSearchFilters({
        scope,
        noteType: isString(value.noteType) ? value.noteType : null,
        folder: isString(value.folder) ? value.folder : null,
      });

      const identity = searchTabIdentity(value.query, filters);

      if (searchIdentities.has(identity)) continue;
      let id = isString(value.id) && value.id.startsWith("search-tab:") ? value.id : "";

      if (!id || searchIDs.has(id)) {
        let index = searchIDs.size + 1;

        while (searchIDs.has(`search-tab:${index}`)) index += 1;
        id = `search-tab:${index}`;
      }

      tabs.push({
        id,
        kind: "search",
        query: value.query.trim().replace(/\s+/g, " "),
        filters,
        scrollTop: isFiniteNumber(value.scrollTop) && value.scrollTop >= 0 ? value.scrollTop : 0,
      });
      searchIdentities.add(identity);
      searchIDs.add(id);
      continue;
    }

    if (!isString(value.path) || !value.path) continue;
    const path = normalizeNotePath(value.path);
    const ref = isNodeRef(value.nodeRef) ? value.nodeRef : undefined;
    const id = noteTabID(path, ref);

    if (paths.has(id)) continue;
    const title = isString(value.title) ? value.title : undefined;

    const tab: NoteTab = {
      id,
      kind: "note",
      path,
      dirty: false,
    };

    if (title !== undefined) tab.title = title;

    if (isString(value.query) && value.query) tab.query = value.query;

    if (isString(value.fragment) && value.fragment) tab.fragment = value.fragment;

    if (isNodeRef(value.nodeRef)) tab.nodeRef = value.nodeRef;

    if (isString(value.presentation)) tab.presentation = value.presentation;

    if (isString(value.nodeId)) tab.nodeId = value.nodeId;

    if (isString(value.nodeKind)) tab.nodeKind = value.nodeKind;

    if (isString(value.structural)) tab.structural = value.structural;
    tabs.push(tab);
    paths.add(id);
  }

  return tabs;
}
