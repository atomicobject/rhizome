import { isString } from "./parse";
import type { NodeRef, ValidationDiagnosticFilter, ValidationScope } from "./types";

function refIdentity(ref: string | NodeRef) {
  if (isString(ref)) return ref;

  return {
    notePath: ref.notePath,
    fragment: ref.fragment ?? null,
    nodeId: ref.nodeId ?? null,
    structuralFingerprint: ref.structuralFingerprint ?? null,
    kind: ref.kind ?? null,
  } as const;
}

export const queryKeys = {
  all: ["rhizome"] as const,
  status: () => [...queryKeys.all, "status"] as const,
  ontology: {
    all: () => [...queryKeys.all, "ontology"] as const,
    summary: () => [...queryKeys.ontology.all(), "summary"] as const,
    atlas: () => [...queryKeys.ontology.all(), "atlas"] as const,
    type: (typeName: string) => [...queryKeys.ontology.all(), "type", typeName] as const,
  },
  search: {
    all: () => [...queryKeys.all, "search"] as const,
    notes: (
      query: string,
      options: {
        type?: string;
        pathPrefix?: string;
        tag?: string;
        limit?: number;
        offset?: number;
      },
    ) => [...queryKeys.search.all(), "notes", query, options] as const,
    suggestions: (query: string, limit: number) =>
      [...queryKeys.search.all(), "suggestions", query, limit] as const,
    paths: (query: string, limit: number) =>
      [...queryKeys.search.all(), "paths", query, limit] as const,
    workspace: (
      query: string,
      options: {
        scope?: "all" | "notes" | "code";
        noteType?: string | null;
        folder?: string | null;
        limit?: number;
        offset?: number;
        continuationToken?: string | null;
      },
    ) => [...queryKeys.search.all(), "workspace", query, options] as const,
  },
  views: {
    all: () => [...queryKeys.all, "views"] as const,
    catalog: () => [...queryKeys.views.all(), "catalog"] as const,
    detail: (viewID: string) => [...queryKeys.views.all(), "detail", viewID] as const,
    execution: (viewID: string) => [...queryKeys.views.all(), "execution", viewID] as const,
    fieldCandidates: (viewID: string, field: string) =>
      [...queryKeys.views.all(), "field-candidates", viewID, field] as const,
  },
  files: {
    all: () => [...queryKeys.all, "files"] as const,
    tree: (path: string | undefined, limit: number) =>
      [...queryKeys.files.all(), "tree", path ?? "", limit] as const,
    detail: (path: string) => [...queryKeys.files.all(), "detail", path] as const,
  },
  graph: {
    all: () => [...queryKeys.all, "graph"] as const,
    global: (options: { notesOnly?: boolean; diagnostics?: boolean }) =>
      [...queryKeys.graph.all(), "global", options] as const,
    local: (
      ref: string | NodeRef,
      limit: number,
      options: { notesOnly?: boolean; diagnostics?: boolean },
    ) => [...queryKeys.graph.all(), "local", refIdentity(ref), limit, options] as const,
    expanded: (modulePath: string, limit: number) =>
      [...queryKeys.graph.all(), "expanded", modulePath, limit] as const,
  },
  nodes: {
    preview: (target: string, from: string | undefined) =>
      [...queryKeys.all, "nodes", "preview", target, from ?? null] as const,
  },
  agent: {
    all: () => [...queryKeys.all, "agent"] as const,
    sessions: () => [...queryKeys.agent.all(), "sessions"] as const,
    session: (sessionID: string) => [...queryKeys.agent.all(), "session", sessionID] as const,
    settings: () => [...queryKeys.agent.all(), "settings"] as const,
    models: () => [...queryKeys.agent.all(), "models"] as const,
  },
  validationAll: () => [...queryKeys.all, "validation"] as const,
  validation: (editFingerprint = "committed") =>
    [...queryKeys.validationAll(), editFingerprint] as const,
  validationDiagnostics: (generation: number, filters: Record<string, string>) =>
    [...queryKeys.validationAll(), "diagnostics", generation, filters] as const,
  validationScopeSummaries: (
    generation: number,
    scopes: ValidationScope[],
    filter?: ValidationDiagnosticFilter,
  ) =>
    [...queryKeys.validationAll(), "scope-summaries", generation, scopes, filter ?? null] as const,
  validationIssueGroups: (
    generation: number,
    scope: ValidationScope,
    filter: ValidationDiagnosticFilter,
  ) => [...queryKeys.validationAll(), "issue-groups", generation, scope, filter] as const,
  validationRepairReview: (id: string) =>
    [...queryKeys.validationAll(), "repair-review", id] as const,
} as const;

// Diagnostic pages and scope counts are immutable within their generation.
// Freshness events update the envelope, which selects a new generation key.
export function isValidationSnapshotQuery(query: { queryKey: readonly unknown[] }) {
  const key = query.queryKey;

  return (
    key[0] === "rhizome" &&
    key[1] === "validation" &&
    (key[2] === "diagnostics" || key[2] === "scope-summaries" || key[2] === "issue-groups")
  );
}
