// Reads for the scope pages (SPEC-0117 US7): the Overview and the All notes
// Briefing. Every block asks for its own data through these hooks, so each
// one loads, fails, and retries on its own; blocks reading the same data share
// one cached query. Reads, all through public APIs:
//
// - `GET /api/v1/ontology/shape?parts=<part>`, one request per part;
// - `GET /api/v1/ontology/types` for type labels, roles, and descriptions,
//   under the same query the group model reads labels from;
// - display groups and type documentation through the kit.
//
// - readOf(query), blockStatus(reads): one block's loading, failure, and retry.
// - useAggregatePart(part), useTypeSummaries(), useScopeModel(scope, expanded),
//   useScopeDocs(model): the queries, each with its read.
import { useDisplayGroups, useTypeDocs, useValidationSummaries, type TypeDoc } from "@rhizome/kit";
import { keepPreviousData, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMemo } from "react";

import {
  aggregatePath,
  parseAggregate,
  parseTypeSummaries,
  type AggregatePart,
} from "./aggregate.ts";
import { getJSON, groupViewKeys, refetchFailedTypeDocs } from "./load.ts";
import { scopeModel, type Scope, type ScopeModel } from "./scope.ts";

/** One query's part in a block's state. */
export type Read = { pending: boolean; error: Error | null; retry: () => void };

export type BlockStatus =
  | { status: "loading" }
  | { status: "error"; error: Error; retry: () => void }
  | { status: "ready" };

export const readOf = (query: {
  isPending: boolean;
  error: Error | null;
  refetch: () => void;
}): Read => ({ pending: query.isPending, error: query.error, retry: query.refetch });

/** Failed when any read failed, retrying every failed read; loading while any is pending. */
export function blockStatus(reads: readonly Read[]): BlockStatus {
  const failed = reads.filter((read) => read.error);

  if (failed.length)
    return {
      status: "error",
      error: failed[0].error ?? new Error("Request failed"),
      retry: () => {
        for (const read of failed) void read.retry();
      },
    };

  return reads.some((read) => read.pending) ? { status: "loading" } : { status: "ready" };
}

const NO_SCOPES: Parameters<typeof useValidationSummaries>[0] = [];

export function useAggregatePart(part: AggregatePart) {
  // Issue counts come from published validation, which data events do not
  // announce, so the members part rereads when a new generation publishes.
  const { generation } = useValidationSummaries(NO_SCOPES);

  const query = useQuery({
    queryKey: ["group-views", "aggregate", part, ...(part === "members" ? [generation] : [])],
    queryFn: async ({ signal }) => parseAggregate(await getJSON(aggregatePath(part), signal)),
    placeholderData: keepPreviousData,
  });

  return { ...query, read: readOf(query) };
}

export function useTypeSummaries() {
  const query = useQuery({
    queryKey: groupViewKeys.typeLabels,
    queryFn: ({ signal }) => getJSON("/api/v1/ontology/types", signal),
    select: parseTypeSummaries,
  });

  return { ...query, read: readOf(query) };
}

/**
 * The scope's model, from the members part, display groups, and type
 * summaries. `missing` is true once loaded when the group no longer exists.
 */
export function useScopeModel(scope: Scope, expanded: ReadonlySet<string>) {
  const members = useAggregatePart("members");
  const groups = useDisplayGroups();
  const summaries = useTypeSummaries();
  const figures = members.data?.members;

  const model = useMemo(
    () =>
      figures && groups.groups && summaries.data
        ? scopeModel({
            scope,
            groups: groups.groups,
            members: figures,
            summaries: summaries.data,
            expanded,
          })
        : null,
    [scope, figures, groups.groups, summaries.data, expanded],
  );

  const reads = [members.read, readOf(groups), summaries.read];
  const loaded = !!figures && !!groups.groups && !!summaries.data;

  return {
    model,
    missing: loaded && model === null,
    facts: members.data?.facts ?? null,
    summaries: summaries.data ?? null,
    reads,
  };
}

const NO_DOCS: Readonly<Record<string, TypeDoc>> = {};

/**
 * Documentation for every member, its concrete types, and an interface
 * member's own profile. Retrying refetches whichever documentation failed.
 */
export function useScopeDocs(model: ScopeModel | null) {
  const queryClient = useQueryClient();

  const names = useMemo(() => {
    if (!model) return [];

    const nodes =
      model.scope.kind === "group"
        ? model.nodes
        : model.sections.flatMap((section) => section.members);

    return [
      ...new Set(
        nodes.flatMap((node) => [...(node.kind === "interface" ? [node.name] : []), ...node.types]),
      ),
    ];
  }, [model]);

  const docs = useTypeDocs(names);
  const ready = names.every((name) => docs.docs[name] !== undefined);

  const read: Read = {
    pending: !model || (!ready && !docs.error),
    error: docs.error,
    retry: () => refetchFailedTypeDocs(queryClient),
  };

  return { docs: ready ? docs.docs : NO_DOCS, read };
}
