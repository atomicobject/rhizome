// @rhizome/kit: what a custom view needs from Rhizome. Everything here works
// the same whether the view is framed by the Notes workspace or opened at its
// own URL as a standalone tool.
import {
  replaceEqualDeep,
  useMutation,
  useQueryClient,
  type UseQueryOptions,
  type MutateOptions,
} from "@tanstack/react-query";
import type { ComponentProps } from "react";
import { createRoot } from "react-dom/client";

import { executePublicView, queryPublicGraphQLOperation } from "../src/api/client";
import type { JsonObject } from "../src/api/parse";
import type { PublicGraphQLError } from "../src/api/publicGraphQLTypes";
import type {
  NodeRef,
  OntologyEditOp,
  ViewExecuteRequest,
  ViewExecuteResponse,
} from "../src/api/types";
import { remapCommittedEditOps } from "../src/components/editing/editSessionState";
import {
  workspaceViewHref,
  nodeHref,
  type OpenNodeOptions,
  type ViewContext,
} from "../src/views/context";
import { startView, type ViewConfig, type ViewModule } from "./startView";
import { getViewInvocation } from "./viewContext";

export * from "./viewContext";

export * from "./preferences";

import {
  OPEN_COLLECTION_MESSAGE,
  OPEN_ISSUES_MESSAGE,
  OPEN_NODE_MESSAGE,
  OPEN_VIEW_MESSAGE,
  OPEN_NOTE_MESSAGE,
  OPEN_SEARCH_MESSAGE,
  type IssueScope,
  type ViewMessage,
} from "../src/lib/customViewMessages";
import { buildNotesLocation, buildNotesPath } from "../src/components/notesRoute";
import { useStagedQuery, type StagedSession } from "../src/staging/stagedQuery";
import {
  commitOps,
  embedded,
  setFieldOp,
  useEditWriter,
  type FieldValue,
  type NodeTarget,
} from "./editSession";

export {
  EditSession,
  embedded,
  type EditMode,
  type FieldValue,
  type NodeTarget,
} from "./editSession";

export * from "./types";

export {
  useDisplayGroup,
  useDisplayGroups,
  useTypeDocs,
  useValidationSummaries,
  validationScopeKey,
} from "./schemaData";

export type { IssueScope };

export {
  StatusMark,
  isTerminalValue,
  orderedEnumValues,
  statusPosition,
  type StatusPosition,
} from "./statusMark";

export { HighlightProvider, RecordChip } from "./recordChip";

export { RelativeTime, sharedLabelPrefix, typeLabel } from "./format";

export type { ViewConfig, ViewModule } from "./startView";

export { OPEN_NOTE_MESSAGE };

/** Run a read-only GraphQL query against the vault. Throws on GraphQL errors. */
export function graphql<TData>(query: string, variables?: JsonObject): Promise<TData> {
  return readGraphQL<TData>(query, variables, null);
}

/** A GraphQL error, as `useGraphQL` reports in `partialErrors`. */
export type GraphQLError = PublicGraphQLError;

const NO_ERRORS: readonly GraphQLError[] = [];

// GraphQL data is always an object.
function isWeakKey<T>(value: T): value is T & WeakKey {
  return typeof value === "object" && value !== null;
}

// The errors a partial read tolerated, by the data object they arrived with.
const toleratedErrors = new WeakMap<WeakKey, readonly GraphQLError[]>();

const partialErrorsOf = <TData,>(data: TData) =>
  (isWeakKey(data) && toleratedErrors.get(data)) || NO_ERRORS;

/**
 * Every error names a field inside a root field that still returned data,
 * such as a required field missing from one record. GraphQL nulls the failing
 * field, or its nearest nullable parent when the field is non-null, which can
 * be a whole record or list inside the root. A failed root or an error without
 * a path is not partial data.
 */
function insideRoots<TData>(
  data: TData | null | undefined,
  errors: readonly { path?: string[] }[],
) {
  if (data === undefined || data === null) return false;
  const roots = new Map(Object.entries(data));

  return errors.every(
    ({ path }) => path !== undefined && path.length >= 2 && roots.get(path[0]) != null,
  );
}

async function readGraphQL<TData>(
  query: string,
  variables: JsonObject | undefined,
  editSession: StagedSession,
  signal?: AbortSignal,
  partial = false,
): Promise<TData> {
  const result = await queryPublicGraphQLOperation<TData>(query, variables, undefined, {
    editSession,
    signal,
  });

  if (result.errors?.length && !(partial && insideRoots(result.data, result.errors))) {
    throw new Error(result.errors.map((error) => error.message).join("\n"));
  }

  if (result.data === undefined) throw new Error("GraphQL response had no data");

  if (result.errors?.length && isWeakKey(result.data))
    toleratedErrors.set(result.data, result.errors);

  return result.data;
}

/**
 * `useQuery` over `graphql`, reading through the edit session writes stage in,
 * so staged edits show before they are saved. The previous result stays on
 * screen while the session changes. Every query shares the "graphql" key prefix.
 */
export function useGraphQL<TData>(
  query: string,
  variables?: JsonObject,
  options?: Omit<UseQueryOptions<TData>, "queryKey" | "queryFn"> & {
    /** Display-only updater; Rhizome never guesses the shape of your query. */
    optimistic?: (data: TData, session: StagedSession) => TData;
    /**
     * Keep the data when every error lies inside a root field that returned
     * data, such as a record missing a required field. GraphQL nulls that
     * field, or its nearest nullable parent when the field is non-null, and
     * the tolerated errors are in `partialErrors`. A failed root field or an
     * error without a path still fails.
     */
    partial?: boolean;
  },
) {
  const { session, readLifecycle } = useEditWriter();
  const { optimistic, partial = false, ...queryOptions } = options ?? {};

  const result = useStagedQuery<TData>({
    subject: ["graphql", query, variables ?? {}, ...(partial ? ["partial"] : [])],
    session,
    lifecycle: readLifecycle,
    options: partial
      ? {
          // An equal response keeps the previous data object; carry the newer
          // response's errors over to it.
          structuralSharing: (previous, next) => {
            const shared = replaceEqualDeep(previous, next);

            if (shared !== next && isWeakKey(shared)) {
              toleratedErrors.set(shared, partialErrorsOf(next));
            }

            return shared;
          },
          ...queryOptions,
        }
      : queryOptions,
    queryFn: ({ signal, editSession }) =>
      readGraphQL<TData>(query, variables, editSession, signal, partial),
  });

  return {
    ...result,
    data:
      result.displayData !== undefined && optimistic && result.displaySession
        ? optimistic(result.displayData, result.displaySession)
        : result.displayData,
    /** Errors a `partial` read tolerated for the data on screen; empty otherwise. */
    partialErrors: partialErrorsOf(result.displayData),
    editOutcome: readLifecycle.outcome,
  };
}

/**
 * Commit one field on a note or embedded node. `target` is the node's GraphQL
 * `ref { notePath fragment nodeId structuralFingerprint }`. `null` unsets the field; an array writes
 * a list. Checkbox-backed fields take "true" or "false". Throws when Rhizome
 * does not commit the edit. This always commits; `useSetField` follows the
 * surrounding `EditSession`.
 */
export function setField(target: NodeTarget, field: string, value: FieldValue) {
  return commitOps([setFieldOp(target, field, value)]);
}

type SetFieldInput = { target: NodeTarget; field: string; value: FieldValue };

type SetFieldOptions = MutateOptions<void, Error, SetFieldInput>;

/**
 * Write one field through the surrounding `EditSession`: staged in the
 * workspace session when framed there, otherwise committed. Pass a query's
 * `displaySession` when its displayed rows supply the edit targets.
 */
export function useSetField(readSession?: StagedSession) {
  const queryClient = useQueryClient();
  const writer = useEditWriter();

  const mutation = useMutation<void, Error, { input: SetFieldInput; ops: OntologyEditOp[] }>({
    mutationFn: ({ ops }) => writer.write(ops),
    onSettled: () => {
      if (writer.target === "immediate") void queryClient.invalidateQueries();
    },
  });

  // Capture the reader's lineage when invoked, before an async mutation or a
  // workspace queue can outlive that reader's retained display result.
  const capture = (input: SetFieldInput) => ({
    input,
    ops: remapCommittedEditOps(
      [setFieldOp(input.target, input.field, input.value)],
      readSession?.refLineage ?? [],
    ),
  });

  const callbacks = (options?: SetFieldOptions): Parameters<typeof mutation.mutate>[1] =>
    options && {
      onSuccess: (data, { input }, result, context) =>
        options.onSuccess?.(data, input, result, context),
      onError: (error, { input }, result, context) =>
        options.onError?.(error, input, result, context),
      onSettled: (data, error, { input }, result, context) =>
        options.onSettled?.(data, error, input, result, context),
    };

  return {
    ...mutation,
    variables: mutation.variables?.input,
    mutate: (input: SetFieldInput, options?: SetFieldOptions) =>
      mutation.mutate(capture(input), callbacks(options)),
    mutateAsync: (input: SetFieldInput, options?: SetFieldOptions) =>
      mutation.mutateAsync(capture(input), callbacks(options)),
  };
}

/** Execute an existing native view definition through Rhizome's shared row executor. */
export function executeView(id: string, request: ViewExecuteRequest = {}) {
  return executePublicView(id, request);
}

/** Native rows with the same staged edit-session and query-cache behavior as GraphQL reads. */
export function useViewRows(
  id: string,
  request: ViewExecuteRequest = {},
  options?: Omit<UseQueryOptions<ViewExecuteResponse>, "queryKey" | "queryFn"> & {
    optimistic?: (data: ViewExecuteResponse, session: StagedSession) => ViewExecuteResponse;
  },
) {
  const { session, readLifecycle } = useEditWriter();
  const { optimistic, ...queryOptions } = options ?? {};
  const context = getViewInvocation().context;
  const subject = ["view-rows", id, context, request];

  const result = useStagedQuery<ViewExecuteResponse>({
    subject,
    session,
    lifecycle: readLifecycle,
    options: queryOptions,
    queryFn: ({ signal, editSession }) => executePublicView(id, request, { signal, editSession }),
  });

  return {
    ...result,
    data:
      result.displayData !== undefined && optimistic && result.displaySession
        ? optimistic(result.displayData, result.displaySession)
        : result.displayData,
    editOutcome: readLifecycle.outcome,
  };
}

/** Open a canonical note or embedded node, optionally in a named presentation. */
export function openNode(ref: NodeRef, options: OpenNodeOptions = {}) {
  if (embedded) {
    window.parent.postMessage({ type: OPEN_NODE_MESSAGE, ref, ...options }, window.location.origin);

    return;
  }

  if (options.beside) window.open(nodeHref(ref, options), "_blank", "noopener");
  else window.location.assign(nodeHref(ref, options));
}

/** Open another registered view with an explicit subject. */
export function openView(id: string, context: ViewContext = { kind: "standalone" }) {
  if (embedded) {
    window.parent.postMessage({ type: OPEN_VIEW_MESSAGE, id, context }, window.location.origin);

    return;
  }

  window.location.assign(workspaceViewHref(id, context));
}

const postToHost = (message: ViewMessage) =>
  window.parent.postMessage(message, window.location.origin);

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
 * Open a project search tab filtered to a vault folder, optionally with a
 * query. `folder: "/"` lists the notes at the vault root, which sit in no folder.
 */
export function openSearch({ folder, query }: { folder: string; query?: string }) {
  if (embedded) {
    postToHost(
      query ? { type: OPEN_SEARCH_MESSAGE, folder, query } : { type: OPEN_SEARCH_MESSAGE, folder },
    );

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
    window.parent.postMessage(
      { type: OPEN_NOTE_MESSAGE, path, beside: options.beside ?? false },
      window.location.origin,
    );

    return;
  }

  if (options.beside) window.open(noteHref(path), "_blank", "noopener");
  else window.location.assign(noteHref(path));
}

/** A link to a note that opens in the workspace when the view is embedded. */
export function NoteLink({
  path,
  children,
  ...props
}: { path: string } & Omit<ComponentProps<"a">, "href">) {
  return (
    <a
      {...props}
      href={noteHref(path)}
      onClick={(event) => {
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
 * Called by the shell that rzm serves at /views/<id>. Renders the entry module's
 * default export inside the kit's providers. A module without a default export
 * is left to mount itself into #root.
 */
export async function mountView(load: () => Promise<ViewModule>, view: ViewConfig) {
  const container = document.getElementById("root");

  if (!container) throw new Error("mountView needs an element with id root");

  await startView(load, view, {
    hosted: embedded,
    render: (element) => createRoot(container).render(element),
    reload: () => window.location.reload(),
  });
}
