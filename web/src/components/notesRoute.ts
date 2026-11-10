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
  /** The shown view's param values: every query key the route does not own (SPEC-0135 R6a). */
  viewParams?: string | null;
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
  viewParams?: string | null;
};

/** The query keys the Notes route owns; every other key is a view param. */
const ROUTE_KEYS = new Set([
  "note",
  "noteQuery",
  "view",
  "presentation",
  "nodeId",
  "kind",
  "structural",
  "issueScopeKind",
  "issueScopeKey",
  "issue",
  "search",
  "scope",
  "noteType",
  "folder",
  "searchTab",
]);

/** How much of a tab's view params a tab keeps, like its other stored state. */
const VIEW_PARAMS_LIMIT = { keys: 32, length: 2048 };

/** View params as a tab keeps them: route keys removed, bounded, empty as null. */
export function boundedViewParams(value: string | null | undefined): string | null {
  if (!value) return null;
  const kept = new URLSearchParams();

  for (const [key, entry] of new URLSearchParams(value)) {
    if (ROUTE_KEYS.has(key) || kept.size >= VIEW_PARAMS_LIMIT.keys) continue;
    kept.append(key, entry);
  }

  const query = kept.toString();

  return query && query.length <= VIEW_PARAMS_LIMIT.length ? query : null;
}

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

  const searchQuery =
    note || !params.has("search") ? null : normalizeSearchQuery(params.get("search") || "");

  const location: NotesLocation = {
    selection: parseNotesRoute(pathname),
    note,
    query,
    fragment: !note ? null : decodeURLFragment(hash).replace(/^#/, "") || null,
    view,
  };

  if (params.has("presentation")) location.presentation = params.get("presentation");

  const viewParams = boundedViewParams(search);

  if (viewParams) location.viewParams = viewParams;

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

  const rawScope = params.get("scope");

  const searchFilters = normalizeSearchFilters({
    scope: rawScope === "all" || rawScope === "notes" || rawScope === "code" ? rawScope : undefined,
    noteType: params.get("noteType"),
    folder: params.get("folder"),
  });

  // An empty query is a folder listing, so it needs a folder.
  if (searchQuery !== null && (searchQuery || searchFilters.folder)) {
    location.search = searchQuery;
    location.searchFilters = searchFilters;
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
  viewParams,
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

  const filters = normalizeSearchFilters({ scope: scope ?? undefined, noteType, folder });

  if (!note && search != null && (normalizeSearchQuery(search) || filters.folder)) {
    params.set("search", normalizeSearchQuery(search));

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

  for (const [key, value] of new URLSearchParams(boundedViewParams(viewParams) ?? ""))
    params.append(key, value);

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
