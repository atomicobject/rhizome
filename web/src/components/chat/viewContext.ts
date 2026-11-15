import { useSyncExternalStore } from "react";

import type { AgentViewContext } from "../../api/types";
import { useLocationSnapshot } from "../locationStore";
import { parseNotesLocation } from "../notesRoute";
import { isOntologyPath, parseOntologyRoute } from "../ontologyRoute";
import { useViewCatalogQuery } from "../useNotesQueries";

/** How a context chip names each kind. */
export const CONTEXT_KIND_LABELS: Record<AgentViewContext["kind"], string> = {
  note: "Note",
  view: "View",
  type: "Type",
  group: "Group",
  ontology: "Ontology",
  notes: "Notes",
  section: "Section",
};

const byteLength = (value: string) => new TextEncoder().encode(value).length;

const runes = (value: string, max: number) => Array.from(value).slice(0, max).join("");

/**
 * Fits a context to the server's bounds (SPEC-0140), so a long title or a
 * view with many params never fails a send. An identifier too long to keep
 * whole is left out rather than cut.
 */
export function boundedContext(context: AgentViewContext): AgentViewContext {
  const bounded: AgentViewContext = {
    kind: context.kind,
    title: runes(context.title, 200),
    route: context.route.slice(0, 2048),
  };

  if (context.path && byteLength(context.path) <= 1024) bounded.path = context.path;

  if (context.viewId && byteLength(context.viewId) <= 1024) bounded.viewId = context.viewId;

  if (context.detail) bounded.detail = runes(context.detail, 2000);

  return bounded;
}

/** The same page: two notes titled alike in different folders are not. */
export function sameContext(a?: AgentViewContext | null, b?: AgentViewContext | null) {
  return (
    a?.kind === b?.kind && a?.title === b?.title && a?.path === b?.path && a?.viewId === b?.viewId
  );
}

/** What the route alone says the user is looking at. */
export function describeLocation(pathname: string, search: string, hash: string): AgentViewContext {
  const route = `${pathname}${search}`;

  if (isOntologyPath(pathname)) {
    const selection = parseOntologyRoute(pathname);

    return selection.kind === "type"
      ? {
          kind: "type",
          title: selection.name,
          route,
          detail: "Ontology · schema, fields, relations",
        }
      : { kind: "ontology", title: "Atlas", route, detail: "All types and how they relate" };
  }

  const section = pathname.split("/")[1] || "notes";

  if (section !== "notes") {
    return { kind: "section", title: section[0].toUpperCase() + section.slice(1), route };
  }

  const location = parseNotesLocation(pathname, search, hash);

  if (location.note) {
    const name = location.note.split("/").pop() ?? location.note;

    return {
      kind: "note",
      title: name.replace(/\.md$/i, ""),
      path: location.note,
      route,
      detail: location.fragment ? `Section ${location.fragment}` : undefined,
    };
  }

  if (location.view) return { kind: "view", title: location.view, viewId: location.view, route };

  const selection = location.selection;

  switch (selection.kind) {
    case "type":
      return { kind: "type", title: selection.slug, route };
    case "group":
      return { kind: "group", title: selection.group, route };
    case "issues":
      return { kind: "notes", title: "Problems", route };
    case "modified":
      return { kind: "notes", title: "Changes", route };
    default:
      return { kind: "notes", title: "All notes", route };
  }
}

// The Notes tab strip's title for the shown note or view, which the URL
// cannot give: it names a file or a view id.
let notesTitle: string | null = null;

const titleListeners = new Set<() => void>();

export function publishNotesTitle(title: string | null) {
  if (title === notesTitle) return;
  notesTitle = title;

  for (const listener of titleListeners) listener();
}

function subscribeTitle(listener: () => void) {
  titleListeners.add(listener);

  return () => titleListeners.delete(listener);
}

/** What the user is looking at, with the shown note's or view's real title. */
export function useViewContext(): AgentViewContext {
  const { pathname, search, hash } = useLocationSnapshot();
  const located = describeLocation(pathname, search, hash);
  const title = useSyncExternalStore(subscribeTitle, () => notesTitle);
  const titled = located.kind === "note" || located.kind === "view";
  const catalog = useViewCatalogQuery(located.kind === "view" && !title);

  if (titled && title) return { ...located, title };

  const viewName = catalog.data?.views.find((view) => view.id === located.viewId)?.name;

  return viewName ? { ...located, title: viewName } : located;
}
