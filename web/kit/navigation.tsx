// Navigation from a view: open nodes, views, notes, collections, issues, and
// searches in the framing workspace, or at their own URLs when standalone.
// Every message names the page on screen in an experience frame.
import type { ComponentProps } from "react";

import {
  OPEN_COLLECTION_MESSAGE,
  OPEN_ISSUES_MESSAGE,
  OPEN_NODE_MESSAGE,
  OPEN_NOTE_MESSAGE,
  OPEN_SEARCH_MESSAGE,
  OPEN_VIEW_MESSAGE,
  type IssueScope,
  type ViewMessage,
} from "../src/lib/customViewMessages";
import { buildNotesLocation, buildNotesPath } from "../src/components/notesRoute";
import {
  nodeHref,
  parseViewContext,
  workspaceViewHref,
  type OpenNodeOptions,
  type ViewContextInput,
  type ViewNodeRef,
} from "../src/views/context";
import { isViewParamValues, type ViewParamValue } from "../src/views/params";
import { embedded } from "./editSession";
import { pageStamp } from "./pageBinding";

export const postToHost = (message: ViewMessage) =>
  window.parent.postMessage({ ...message, ...pageStamp() }, window.location.origin);

/** Open a canonical note or embedded node, optionally in a named presentation. */
export function openNode(ref: ViewNodeRef, options: OpenNodeOptions = {}) {
  if (embedded) {
    const navigation = { ...options };

    if (navigation.view === undefined) delete navigation.view;

    // A view's ref may carry nulls from GraphQL; the host parses it.
    window.parent.postMessage(
      { type: OPEN_NODE_MESSAGE, ref, ...navigation, ...pageStamp() },
      window.location.origin,
    );

    return;
  }

  if (options.beside) window.open(nodeHref(ref, options), "_blank", "noopener");
  else window.location.assign(nodeHref(ref, options));
}

/**
 * Open another registered view with an explicit subject, optionally setting
 * its declared params.
 */
export function openView(
  id: string,
  context: ViewContextInput = { kind: "standalone" },
  params?: Record<string, ViewParamValue>,
) {
  const parsedContext = parseViewContext(context);

  if (!parsedContext) throw new Error("Invalid view context");

  if (params !== undefined && !isViewParamValues(params)) throw new Error("Invalid view params");

  if (embedded) {
    postToHost(
      params
        ? { type: OPEN_VIEW_MESSAGE, id, context: parsedContext, params }
        : { type: OPEN_VIEW_MESSAGE, id, context: parsedContext },
    );

    return;
  }

  window.location.assign(workspaceViewHref(id, parsedContext, params));
}

/**
 * Open the issues panel, optionally scoped to a type, interface, or note path.
 * Without a scope, a view presenting a note opens that note's issues; a view
 * anywhere else, or a standalone page, opens every issue.
 */
export function openIssues(scope?: IssueScope) {
  if (embedded) {
    postToHost(scope ? { type: OPEN_ISSUES_MESSAGE, scope } : { type: OPEN_ISSUES_MESSAGE });

    return;
  }

  window.location.assign(buildNotesLocation({ selection: { kind: "issues" }, issueScope: scope }));
}

/** Open a type or interface collection in the workspace. */
export function openCollection(name: string) {
  if (embedded) {
    postToHost({ type: OPEN_COLLECTION_MESSAGE, name });

    return;
  }

  window.location.assign(buildNotesPath({ kind: "type", typeName: name }));
}

/**
 * Open a project search tab, optionally with a query and filtered to a vault
 * folder. `folder: "/"` lists the notes at the vault root, which sit in no
 * folder; without a folder the search covers the whole vault.
 */
export function openSearch({ folder, query }: { folder?: string; query?: string } = {}) {
  if (embedded) {
    const message: ViewMessage = { type: OPEN_SEARCH_MESSAGE };

    if (folder !== undefined) message.folder = folder;

    if (query) message.query = query;
    postToHost(message);

    return;
  }

  window.location.assign(
    buildNotesLocation({ selection: { kind: "all" }, search: query ?? "", folder }),
  );
}

export function noteHref(path: string) {
  return `/notes?note=${encodeURIComponent(path)}`;
}

/** Open a note in the surrounding workspace, or navigate there when standalone. */
export function openNote(path: string, options: { beside?: boolean } = {}) {
  if (embedded) {
    postToHost({ type: OPEN_NOTE_MESSAGE, path, beside: options.beside ?? false });

    return;
  }

  if (options.beside) window.open(noteHref(path), "_blank", "noopener");
  else window.location.assign(noteHref(path));
}

/** A link to a note that opens in the workspace when the view is embedded. */
export function NoteLink({
  path,
  children,
  className,
  onClick,
  ...props
}: { path: string } & Omit<ComponentProps<"a">, "href">) {
  return (
    <a
      {...props}
      className={className ? `rz-note-link ${className}` : "rz-note-link"}
      href={noteHref(path)}
      onClick={(event) => {
        onClick?.(event);

        if (event.defaultPrevented || event.button !== 0 || event.shiftKey || event.altKey) return;
        event.preventDefault();
        openNote(path, { beside: event.metaKey || event.ctrlKey });
      }}
    >
      {children ?? path}
    </a>
  );
}

/**
 * A link to a canonical note or embedded node, optionally in a named
 * presentation, styled like `NoteLink`. Cmd or Ctrl click opens it beside.
 */
export function NodeLink({
  node,
  view,
  children,
  className,
  onClick,
  ...props
}: { node: ViewNodeRef; view?: string } & Omit<ComponentProps<"a">, "href">) {
  return (
    <a
      {...props}
      className={className ? `rz-note-link ${className}` : "rz-note-link"}
      href={nodeHref(node, { view })}
      onClick={(event) => {
        onClick?.(event);

        if (event.defaultPrevented || event.button !== 0 || event.shiftKey || event.altKey) return;
        event.preventDefault();
        openNode(node, { view, beside: event.metaKey || event.ctrlKey });
      }}
    >
      {children}
    </a>
  );
}
