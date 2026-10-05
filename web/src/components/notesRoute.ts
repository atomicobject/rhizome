import type {
  OntologyInterfaceSummary,
  OntologySummaryResponse,
  OntologyTypeSummary,
  ValidationScope,
} from "../api/types";
import { formatTypeName } from "../lib/typeNames";
import { decodeURLFragment, encodeURLFragment } from "../lib/noteDeepLink";
import {
  normalizeSearchFilters,
  normalizeSearchQuery,
  type SearchFilters,
  type SearchScope,
} from "./searchState";

export { normalizeNotePath } from "../lib/content";

export const NOTES_ROOT_PATH = "/notes";

const NOTES_ISSUES_SLUG = "issues";

const NOTES_MODIFIED_SLUG = "modified";

export const PSEUDO_TYPE_ALL = "__all__";

export const PSEUDO_TYPE_ISSUES = "__issues__";

export const PSEUDO_TYPE_MODIFIED = "__modified__";

export type NotesRouteSelection =
  | { kind: "all" }
  | { kind: "issues" }
  | { kind: "modified" }
  | { kind: "group"; group: string }
  | { kind: "type"; slug: string };

export type NotesLocation = {
  selection: NotesRouteSelection;
  note: string | null;
  /** Query authored by the active document, kept separate from app routing params. */
  query: string | null;
  fragment: string | null;
  view: string | null;
  presentation?: string | null;
  nodeId?: string | null;
  nodeKind?: string | null;
  structural?: string | null;
  search?: string | null;
  searchFilters?: SearchFilters;
  searchTabID?: string | null;
  issueScope?: ValidationScope | null;
  issueKey?: string | null;
};

export type NotesLocationSelection = NotesRouteSelection | { kind: "type"; typeName: string };

export type NotesLocationOptions = {
  selection: NotesLocationSelection;
  note?: string | null;
  /** Query authored by the active document, without the leading `?`. */
  query?: string | null;
  fragment?: string | null;
  view?: string | null;
  presentation?: string | null;
  nodeId?: string | null;
  nodeKind?: string | null;
  structural?: string | null;
  search?: string | null;
  scope?: SearchScope | null;
  noteType?: string | null;
  folder?: string | null;
  searchTabID?: string | null;
  issueScope?: ValidationScope | null;
  issueKey?: string | null;
};

export type NoteTarget = {
  path: string;
  /** Query authored by the target document, without the leading `?`. */
  query: string | null;
  fragment: string | null;
};

export function parseNotesRoute(pathname: string): NotesRouteSelection {
  if (!pathname.startsWith(NOTES_ROOT_PATH)) {
    return { kind: "all" };
  }

  const suffix = pathname.slice(NOTES_ROOT_PATH.length).replace(/^\/+|\/+$/g, "");

  if (!suffix) return { kind: "all" };

  if (suffix.startsWith("group/")) {
    try {
      return { kind: "group", group: decodeURIComponent(suffix.slice(6)) };
    } catch {
      return { kind: "all" };
    }
  }

  if (suffix === NOTES_ISSUES_SLUG) return { kind: "issues" };

  if (suffix === NOTES_MODIFIED_SLUG) return { kind: "modified" };

  return { kind: "type", slug: suffix };
}

export function buildNotesPath(
  selection: Exclude<NotesRouteSelection, { kind: "type" }> | { kind: "type"; typeName: string },
): string {
  switch (selection.kind) {
    case "all":
      return NOTES_ROOT_PATH;
    case "issues":
      return `${NOTES_ROOT_PATH}/${NOTES_ISSUES_SLUG}`;
    case "modified":
      return `${NOTES_ROOT_PATH}/${NOTES_MODIFIED_SLUG}`;
    case "group":
      return `${NOTES_ROOT_PATH}/group/${encodeURIComponent(selection.group)}`;
    case "type":
      return `${NOTES_ROOT_PATH}/${formatTypeName(selection.typeName)}`;
  }
}

export function isPseudoType(name: string | null | undefined): boolean {
  return name === PSEUDO_TYPE_ALL || name === PSEUDO_TYPE_ISSUES || name === PSEUDO_TYPE_MODIFIED;
}

function notesPathForSelection(selection: NotesLocationSelection): string {
  if (selection.kind === "type" && "slug" in selection) {
    return `${NOTES_ROOT_PATH}/${selection.slug}`;
  }

  return buildNotesPath(selection);
}

export function parseNotesLocation(pathname: string, search: string, hash: string): NotesLocation {
  const params = new URLSearchParams(search);
  const note = params.get("note") || null;
  const query = !note ? null : params.get("noteQuery");
  const view = params.get("view") || null;
  const rawSearch = normalizeSearchQuery(params.get("search") || "");
  const searchQuery = note || !rawSearch ? null : rawSearch;

  const location: NotesLocation = {
    selection: parseNotesRoute(pathname),
    note,
    query,
    fragment: !note ? null : decodeURLFragment(hash).replace(/^#/, "") || null,
    view,
  };

  if (params.has("presentation")) location.presentation = params.get("presentation");

  if (note && params.has("nodeId")) location.nodeId = params.get("nodeId");

  if (note && params.has("kind")) location.nodeKind = params.get("kind");

  if (note && params.has("structural")) location.structural = params.get("structural");

  const issueScope = parseIssueScope(
    params.get("issueScopeKind"),
    params.get("issueScopeKey") || undefined,
  );

  if (issueScope) location.issueScope = issueScope;
  const issueKey = params.get("issue") || null;

  if (issueKey) location.issueKey = issueKey;

  if (searchQuery) {
    location.search = searchQuery;
    const rawScope = params.get("scope");

    const scope =
      rawScope === "all" || rawScope === "notes" || rawScope === "code" ? rawScope : undefined;

    location.searchFilters = normalizeSearchFilters({
      scope,
      noteType: params.get("noteType"),
      folder: params.get("folder"),
    });
    const stableSearchTabID = normalizeSearchTabID(params.get("searchTab"));

    if (stableSearchTabID) location.searchTabID = stableSearchTabID;
  }

  return location;
}

export function normalizeSearchTabID(value: string | null | undefined): string | null {
  if (!value || !/^search-tab:[1-9]\d*$/.test(value)) return null;

  return value;
}

export function buildNotesLocation({
  selection,
  note,
  query,
  fragment,
  view,
  presentation,
  nodeId,
  nodeKind,
  structural,
  search,
  scope,
  noteType,
  folder,
  searchTabID,
  issueScope,
  issueKey,
}: NotesLocationOptions): string {
  const params = new URLSearchParams();

  if (presentation) params.set("presentation", presentation);

  if (note && nodeId) params.set("nodeId", nodeId);

  if (note && nodeKind) params.set("kind", nodeKind);

  if (note && structural) params.set("structural", structural);

  if (view) {
    params.set("view", view);
  }

  if (issueScope) {
    params.set("issueScopeKind", issueScope.kind);

    if (issueScope.key) params.set("issueScopeKey", issueScope.key);
  }

  if (issueKey) params.set("issue", issueKey);

  if (note) {
    params.set("note", note);
  }

  if (!note && search && normalizeSearchQuery(search)) {
    params.set("search", normalizeSearchQuery(search));
    const filters = normalizeSearchFilters({ scope: scope ?? undefined, noteType, folder });

    if (filters.scope !== "all") params.set("scope", filters.scope);

    if (filters.noteType) params.set("noteType", filters.noteType);

    if (filters.folder) params.set("folder", filters.folder);
    const stableSearchTabID = normalizeSearchTabID(searchTabID);

    if (stableSearchTabID) params.set("searchTab", stableSearchTabID);
  }

  if (note && query) {
    // Keep authored document state out of the application's `note` identity
    // and out of its own routing query parameters.
    params.set("noteQuery", query);
  }

  const queryString = params.toString();
  const suffix = queryString ? `?${queryString}` : "";
  const noteFragment = !note ? "" : encodeURLFragment(fragment || "");

  return `${notesPathForSelection(selection)}${suffix}${noteFragment}`;
}

function parseIssueScope(kind: string | null, key: string | undefined): ValidationScope | null {
  if (kind === "global") return { kind: "global" };

  if (
    kind === "file" ||
    kind === "note" ||
    kind === "node" ||
    kind === "type" ||
    kind === "interface"
  ) {
    return key ? { kind, key } : null;
  }

  return null;
}

export function splitNoteTarget(target: string): NoteTarget {
  const hashIndex = target.indexOf("#");
  const beforeFragment = hashIndex === -1 ? target : target.slice(0, hashIndex);
  const queryIndex = beforeFragment.indexOf("?");

  if (queryIndex === -1) {
    return {
      path: beforeFragment,
      query: null,
      fragment: hashIndex === -1 ? null : target.slice(hashIndex + 1) || null,
    };
  }

  return {
    path: beforeFragment.slice(0, queryIndex),
    query: beforeFragment.slice(queryIndex + 1) || null,
    fragment: hashIndex === -1 ? null : target.slice(hashIndex + 1) || null,
  };
}

export function resolveTypeNameFromSlug(
  slug: string,
  types: OntologyTypeSummary[],
  interfaces: OntologyInterfaceSummary[] = [],
): string | null {
  const matchedType = types.find((type) => formatTypeName(type.name) === slug);

  if (matchedType) return matchedType.name;
  const matchedIface = interfaces.find((iface) => formatTypeName(iface.name) === slug);

  return matchedIface?.name || null;
}

/** The type or pseudo type a selection names, once the summary can resolve its slug. */
export function selectionType(
  selection: NotesLocation["selection"],
  summary: Pick<OntologySummaryResponse, "types" | "interfaces"> | null | undefined,
): string | null {
  if (selection.kind === "all") return PSEUDO_TYPE_ALL;

  if (selection.kind === "issues") return PSEUDO_TYPE_ISSUES;

  if (selection.kind === "modified") return PSEUDO_TYPE_MODIFIED;

  if (selection.kind === "group") return null;

  return resolveTypeNameFromSlug(selection.slug, summary?.types ?? [], summary?.interfaces ?? []);
}

export function selectionForType(typeName: string): NotesLocationSelection {
  if (typeName === PSEUDO_TYPE_ALL) return { kind: "all" };

  if (typeName === PSEUDO_TYPE_ISSUES) return { kind: "issues" };

  if (typeName === PSEUDO_TYPE_MODIFIED) return { kind: "modified" };

  return { kind: "type", typeName };
}
