// useGroupModel() and useCollectionModel(): load the invocation's display group,
// or its type or interface collection, and build a GroupModel. A collection is
// modeled as a group of one member: its node in the display-groups tree,
// without the display children the rail nests under it. Reads, all through
// public APIs:
//
// - display groups (`useDisplayGroups`) for membership and every group's roots;
// - type documentation (`useTypeDocs`) for every member, whose profile names
//   its field roles, and every concrete member type;
// - one GraphQL query for the records of those types and the guide, narrowed
//   by what the caller reads (`GroupRead`); for a collection it also reads
//   each reverse field's count and a capped sample of its targets, and a
//   capped list of only the notes that link in;
// - `GET /api/v1/views` for the authored views the switcher offers;
// - `GET /api/v1/ontology/types` for labels of types outside the group.
//
// The kit refreshes all of it on vault, schema, and validation events, so the
// model stays current without polling. While a refresh runs, the previous
// model stays on screen with `refreshing: true`.
import {
  keepPreviousData,
  useQuery,
  useQueryClient,
  type QueryClient,
} from "@tanstack/react-query";
import { useMemo, useRef } from "react";

import {
  useDisplayGroups,
  useGraphQL,
  useTypeDocs,
  useViewContext,
  type DisplayGroup,
  type TypeDoc,
  type ViewContext,
} from "@rhizome/kit";

import {
  parseRecords,
  parseTableChoice,
  parseTargetViews,
  parseTypeLabels,
  RECORD_CAP,
  recordsQuery,
  type JsonObject,
  type JsonValue,
  type RecordsOptions,
  type SubjectKind,
} from "./api.ts";
import {
  buildGroupModel,
  collectionGroup,
  pickGuide,
  planMembers,
  type GroupModel,
  type MemberPlan,
} from "./model.ts";

export type GroupModelState =
  | { status: "loading" }
  /** The view was not opened for its kind of subject, or the subject no longer exists. */
  | { status: "missing"; name: string | null }
  | { status: "error"; error: Error; retry: () => void }
  /** `refreshError`: the last refresh failed, so the model is the last one loaded. */
  | { status: "ready"; model: GroupModel; refreshing: boolean; refreshError: Error | null };

export async function getJSON(path: string, signal: AbortSignal): Promise<JsonValue> {
  const response = await fetch(path, { signal, headers: { accept: "application/json" } });

  if (!response.ok) throw new Error(`${path} answered ${response.status}`);

  return response.json();
}

/** Query keys for the group views' own REST reads; the kit refreshes them as data. */
export const groupViewKeys = {
  catalog: ["group-views", "catalog"],
  typeLabels: ["group-views", "type-labels"],
} as const;

const NO_DOCS: readonly TypeDoc[] = [];

const EMPTY_DATA: JsonObject = {};

const NO_TYPE_LABELS: JsonValue = [];

const NO_CATALOG: JsonValue = {};

const COLLECTION_RECORDS: GroupRead = { reverse: true, neighbors: "INBOUND" };

/**
 * What a view or block reads of the group's records. `perMember` caps the
 * records of each member, shared by its concrete types, and `perType` those
 * of each type; `records: false` reads only the guide note.
 */
export type GroupRead = Omit<RecordsOptions, "first"> & {
  perMember?: number;
  perType?: number;
  records?: boolean;
};

/** Refetch whichever type documentation failed to load. */
export const refetchFailedTypeDocs = (queryClient: QueryClient) =>
  queryClient.refetchQueries({
    predicate: (query) => query.state.status === "error" && query.queryKey.includes("type-doc"),
  });

/** Records per concrete type a read takes; undefined when it reads `RECORD_CAP` of each. */
function recordCaps(plans: readonly MemberPlan[], read: GroupRead) {
  const { perMember, perType } = read;

  if (perMember === undefined && perType === undefined) return undefined;

  return new Map(
    plans.flatMap((plan) =>
      plan.concreteTypes.map((type): [string, number] => [
        type,
        perMember === undefined
          ? (perType ?? RECORD_CAP)
          : Math.max(1, Math.floor(perMember / plan.concreteTypes.length)),
      ]),
    ),
  );
}

type Subject = { kind: SubjectKind; name: string };

function contextSubject(context: ViewContext): Subject | null {
  switch (context.kind) {
    case "group":
      return { kind: "group", name: context.group };
    case "type":
      return { kind: "type", name: context.type };
    case "interface":
      return { kind: "interface", name: context.interface };
    default:
      return null;
  }
}

/** The group a subject's model is built from; null when it is not in the tree. */
function subjectGroup(subject: Subject, groups: readonly DisplayGroup[]): DisplayGroup | null {
  return subject.kind === "group"
    ? (groups.find((group) => group.name === subject.name) ?? null)
    : collectionGroup(groups, subject.name);
}

function useSubjectModel(
  kinds: readonly SubjectKind[],
  read: GroupRead,
  withViews = true,
): GroupModelState {
  const queryClient = useQueryClient();
  const context = useViewContext();

  const subject = useMemo(() => {
    const found = contextSubject(context);

    return found && kinds.includes(found.kind) ? found : null;
  }, [context, kinds]);

  const name = subject?.name ?? null;
  const groups = useDisplayGroups();

  const group = useMemo(
    () => (subject && groups.groups ? subjectGroup(subject, groups.groups) : null),
    [subject, groups.groups],
  );

  const plans = useMemo(() => (group ? planMembers(group) : []), [group]);
  const typeNames = useMemo(() => plans.flatMap((plan) => plan.concreteTypes), [plans]);

  // An interface member's profile is on the interface's own documentation.
  const docNames = useMemo(
    () => [...new Set([...plans.map((plan) => plan.name), ...typeNames])],
    [plans, typeNames],
  );

  const docs = useTypeDocs(docNames);
  const docsReady = docNames.every((name) => docs.docs[name] !== undefined);

  const orderedDocs = useMemo(
    () => (docsReady ? typeNames.map((type) => docs.docs[type]) : NO_DOCS),
    [docsReady, typeNames, docs.docs],
  );

  const guidePath = useMemo(() => pickGuide(plans, docs.docs)?.path ?? null, [plans, docs.docs]);

  const options = useMemo(
    (): RecordsOptions => ({ ...read, first: recordCaps(plans, read) }),
    [read, plans],
  );

  const readDocs = read.records === false ? NO_DOCS : orderedDocs;

  const document =
    group && docsReady && (readDocs.length || (guidePath && options.guide !== false))
      ? recordsQuery(readDocs, guidePath, options)
      : "";

  // A record missing a required field answers with an error beside its data;
  // validation already reports it, so the views keep the data.
  const records = useGraphQL<JsonObject>(document, guidePath ? { guide: guidePath } : undefined, {
    enabled: document !== "",
    placeholderData: keepPreviousData,
    // A record missing a required field still belongs on the page; its issue count says why.
    partial: true,
  });

  const catalog = useQuery({
    queryKey: groupViewKeys.catalog,
    queryFn: ({ signal }) => getJSON("/api/v1/views", signal),
    enabled: withViews,
  });

  const catalogData = withViews ? catalog.data : NO_CATALOG;

  // Labels for types outside the group are a nicety: a failure falls back to
  // type names rather than holding up the page.
  const typeLabels = useQuery({
    queryKey: groupViewKeys.typeLabels,
    queryFn: ({ signal }) => getJSON("/api/v1/ontology/types", signal),
    retry: false,
  });

  const labelData = typeLabels.data ?? (typeLabels.error ? NO_TYPE_LABELS : undefined);

  // A group whose members have no concrete types has nothing to query.
  const recordData = document === "" && docsReady ? EMPTY_DATA : records.data;

  const built = useMemo(() => {
    // Until every member type's documentation loads, the records cannot be read.
    if (!docsReady || !subject) return null;

    if (!group || !groups.groups || !recordData || catalogData === undefined) return null;

    if (labelData === undefined) return null;

    return buildGroupModel({
      group,
      groups: groups.groups,
      docs: docs.docs,
      records: parseRecords(recordData, readDocs, options),
      views: parseTargetViews(catalogData, subject.kind, subject.name),
      tableChoice: parseTableChoice(catalogData, subject.kind, subject.name),
      typeLabels: parseTypeLabels(labelData),
    });
  }, [
    docsReady,
    subject,
    group,
    groups.groups,
    docs.docs,
    recordData,
    readDocs,
    options,
    catalogData,
    labelData,
  ]);

  // While a type joining the group loads its documentation, keep showing the
  // last model rather than a group without records.
  const last = useRef<{ name: string | null; model: GroupModel } | null>(null);

  if (built) last.current = { name, model: built };
  const model = built ?? (last.current?.name === name ? last.current.model : null);

  const error = groups.error ?? docs.error ?? records.error ?? catalog.error;

  if (model) {
    return {
      status: "ready",
      model,
      refreshing: !built || records.isFetching || groups.isFetching,
      refreshError: error ?? null,
    };
  }

  if (error)
    return {
      status: "error",
      error,
      retry: () => {
        void groups.refetch();
        void refetchFailedTypeDocs(queryClient);
        void records.refetch();

        if (withViews) void catalog.refetch();
      },
    };

  if (name === null || (groups.groups && !group)) return { status: "missing", name };

  return { status: "loading" };
}

const GROUP: readonly SubjectKind[] = ["group"];

const COLLECTION: readonly SubjectKind[] = ["type", "interface"];

const ALL_RECORDS: GroupRead = {};

/**
 * The model of the display group the view was opened for, from what `read`
 * reads of its records (every record and field by default). Without `views`,
 * it neither reads nor waits for the view catalog, and lists no views. Pass
 * a module constant as `read`: the model rebuilds when it changes.
 */
export function useGroupModel({
  views = true,
  read = ALL_RECORDS,
}: { views?: boolean; read?: GroupRead } = {}): GroupModelState {
  return useSubjectModel(GROUP, read, views);
}

/**
 * The model of the type or interface collection the view was opened for: one
 * member, with its reverse fields and the notes that link in.
 */
export function useCollectionModel(): GroupModelState {
  return useSubjectModel(COLLECTION, COLLECTION_RECORDS);
}
