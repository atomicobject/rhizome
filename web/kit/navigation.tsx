// Navigation from a view: open nodes, views, notes, collections, issues, and
// searches in the framing workspace, or at their own URLs when standalone.
// In an experience frame every message names a page: the page on screen for
// the imperative exports, the component's own page for `usePageNavigation`.
import { useMemo, type ComponentProps } from "react";

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
import { isViewParamValues, type ViewParamValue, type ViewParamsPatch } from "../src/views/params";
import { embedded } from "./editSession";
import { activePageBinding, pageStamp, usePageBinding, type PageBinding } from "./pageBinding";
import { setPageViewParams } from "./viewParams";

type Page = PageBinding | null;

const send = (page: Page, message: ViewMessage) =>
  window.parent.postMessage({ ...message, ...pageStamp(page) }, window.location.origin);

/** Post a message stamped with the page on screen. */
export const postToHost = (message: ViewMessage) => send(activePageBinding(), message);

// Navigation that names `page()` as its sender.
function pageNavigation(page: () => Page) {
  return {
    openNode(ref: ViewNodeRef, options: OpenNodeOptions = {}) {
      if (embedded) {
        const navigation = { ...options };

        if (navigation.view === undefined) delete navigation.view;

        // A view's ref may carry nulls from GraphQL; the host parses it.
        window.parent.postMessage(
          { type: OPEN_NODE_MESSAGE, ref, ...navigation, ...pageStamp(page()) },
          window.location.origin,
        );

        return;
      }

      if (options.beside) window.open(nodeHref(ref, options), "_blank", "noopener");
      else window.location.assign(nodeHref(ref, options));
    },

    openView(
      id: string,
      context: ViewContextInput = { kind: "standalone" },
      params?: Record<string, ViewParamValue>,
    ) {
      const parsedContext = parseViewContext(context);

      if (!parsedContext) throw new Error("Invalid view context");

      if (params !== undefined && !isViewParamValues(params))
        throw new Error("Invalid view params");

      if (embedded) {
        send(
          page(),
          params
            ? { type: OPEN_VIEW_MESSAGE, id, context: parsedContext, params }
            : { type: OPEN_VIEW_MESSAGE, id, context: parsedContext },
        );

        return;
      }

      window.location.assign(workspaceViewHref(id, parsedContext, params));
    },

    openIssues(scope?: IssueScope) {
      if (embedded) {
        send(page(), scope ? { type: OPEN_ISSUES_MESSAGE, scope } : { type: OPEN_ISSUES_MESSAGE });

        return;
      }

      window.location.assign(
        buildNotesLocation({ selection: { kind: "issues" }, issueScope: scope }),
      );
    },

    openCollection(name: string) {
      if (embedded) {
        send(page(), { type: OPEN_COLLECTION_MESSAGE, name });

        return;
      }

      window.location.assign(buildNotesPath({ kind: "type", typeName: name }));
    },

    openSearch({ folder, query }: { folder?: string; query?: string } = {}) {
      if (embedded) {
        const message: ViewMessage = { type: OPEN_SEARCH_MESSAGE };

        if (folder !== undefined) message.folder = folder;

        if (query) message.query = query;
        send(page(), message);

        return;
      }

      window.location.assign(
        buildNotesLocation({ selection: { kind: "all" }, search: query ?? "", folder }),
      );
    },

    openNote(path: string, options: { beside?: boolean } = {}) {
      if (embedded) {
        send(page(), { type: OPEN_NOTE_MESSAGE, path, beside: options.beside ?? false });

        return;
      }

      if (options.beside) window.open(noteHref(path), "_blank", "noopener");
      else window.location.assign(noteHref(path));
    },

    /** See `setViewParams`; checked against this page's declarations. */
    setViewParams: (patch: ViewParamsPatch) => setPageViewParams(page(), patch),
  };
}

// The imperative exports name the page on screen in an experience frame, so a
// hidden page's late call would carry another page's stamp. Components use
// `usePageNavigation`, which names the page they render in.
const onScreen = pageNavigation(activePageBinding);

/** Open a canonical note or embedded node, optionally in a named presentation. */
export const openNode = onScreen.openNode;

/**
 * Open another registered view with an explicit subject, optionally setting
 * its declared params.
 */
export const openView = onScreen.openView;

/**
 * Open the issues panel, optionally scoped to a type, interface, or note path.
 * Without a scope, a view presenting a note opens that note's issues; a view
 * anywhere else, or a standalone page, opens every issue.
 */
export const openIssues = onScreen.openIssues;

/** Open a type or interface collection in the workspace. */
export const openCollection = onScreen.openCollection;

/**
 * Open a project search tab, optionally with a query and filtered to a vault
 * folder. `folder: "/"` lists the notes at the vault root, which sit in no
 * folder; without a folder the search covers the whole vault.
 */
export const openSearch = onScreen.openSearch;

/** Open a note in the surrounding workspace, or navigate there when standalone. */
export const openNote = onScreen.openNote;

export type PageNavigation = ReturnType<typeof pageNavigation>;

/**
 * The `open*` functions and `setViewParams`, bound to the page this component
 * renders in. In an experience frame a hidden page's late call, such as a
 * debounced param change after a tab switch, still names its own page, so the
 * host can refuse it; the imperative exports name the page on screen.
 */
export function usePageNavigation(): PageNavigation {
  const page = usePageBinding();

  return useMemo(() => pageNavigation(() => page), [page]);
}

export function noteHref(path: string) {
  return `/notes?note=${encodeURIComponent(path)}`;
}

/** A link to a note that opens in the workspace when the view is embedded. */
export function NoteLink({
  path,
  children,
  className,
  onClick,
  ...props
}: { path: string } & Omit<ComponentProps<"a">, "href">) {
  const { openNote } = usePageNavigation();

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
  const { openNode } = usePageNavigation();

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
