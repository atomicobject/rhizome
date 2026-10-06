// The All notes Briefing (SPEC-0117 US5): what needs attention, what is in
// motion, and what changed across the whole vault, without reading every
// note. Needs attention and In motion share one column, Recent changes the
// other. Each block paints its heading at once and loads, fails, and retries
// on its own. Reads, all through public APIs:
//
// - the published validation generation (`useValidationSummaries`) and
//   `POST /api/v1/validation/groups` for issues by variant, untyped and
//   ambiguous notes included;
// - the aggregate's members part, type documentation, and one GraphQL query
//   for each type's newest records in an active lifecycle value;
// - `GET /api/v1/ontology/types/__all__?limit=N` for the newest notes.
import {
  RelativeTime,
  StatusMark,
  openCollection,
  openIssues,
  openNode,
  useGraphQL,
  useTypeDocs,
  useValidationSummaries,
  type ViewModuleProps,
} from "@rhizome/kit";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo } from "react";

import { isString, parseRecords, type JsonObject, type JsonValue } from "./api.ts";
import { Block, BlockBody, Rebuilding, lower, plural } from "./briefing-parts.tsx";
import "./components.tsx";
import { getJSON, refetchFailedTypeDocs } from "./load.ts";
import {
  blockStatus,
  readOf,
  useAggregatePart,
  useTypeSummaries,
  type Read,
} from "./scope-load.ts";
import {
  MOTION_PER_TYPE,
  RECENT_NOTES,
  activeRecordsQuery,
  motionTypes,
  parseIssueGroups,
  parseRecentNotes,
} from "./workspace-activity.ts";

const GLOBAL = [{ kind: "global" as const }];

const fmt = (count: number) => count.toLocaleString("en-US");

/** A generation-bound read answered 410 Gone: a newer generation replaced it. */
class GenerationExpired extends Error {}

async function postJSON(path: string, body: JsonObject, signal: AbortSignal): Promise<JsonValue> {
  const response = await fetch(path, {
    method: "POST",
    signal,
    headers: { accept: "application/json", "content-type": "application/json" },
    body: JSON.stringify(body),
  });

  if (response.status === 410) throw new GenerationExpired(`${path} answered 410`);

  if (!response.ok) throw new Error(`${path} answered ${response.status}`);

  return response.json();
}

function NeedsAttention() {
  const queryClient = useQueryClient();
  const validation = useValidationSummaries(GLOBAL);
  const generation = validation.generation ?? 0;
  const { refreshGeneration } = validation;

  const groups = useQuery({
    queryKey: ["group-views", "issue-groups", generation],
    queryFn: async ({ signal }) =>
      parseIssueGroups(
        await postJSON(
          "/api/v1/validation/groups",
          { generation, scope: { kind: "global" } },
          signal,
        ),
      ),
    enabled: generation > 0,
    retry: (failures, error) => !(error instanceof GenerationExpired) && failures < 3,
  });

  // An expired generation means a newer one was published: reread it once,
  // and the new generation keys a fresh read.
  const expired = groups.error instanceof GenerationExpired;

  useEffect(() => {
    if (expired) void refreshGeneration();
  }, [expired, refreshGeneration]);

  const reads: Read[] = [
    {
      pending: validation.isLoading,
      error: validation.error,
      retry: () =>
        queryClient.refetchQueries({
          predicate: (query) =>
            query.state.status === "error" && query.queryKey.includes("validation"),
        }),
    },
    ...(generation > 0
      ? [
          {
            ...readOf(groups),
            retry: async () => {
              if (expired) await refreshGeneration();

              await groups.refetch();
            },
          },
        ]
      : []),
  ];

  const status = blockStatus(reads);
  const total = (groups.data ?? []).reduce((sum, group) => sum + group.issueCount, 0);

  return (
    <Block
      title="Needs attention"
      count={status.status === "ready" ? fmt(total) : undefined}
      caption="Validation issues by variant, untyped and ambiguous notes included."
    >
      <BlockBody status={status} what="what needs attention">
        {() => {
          if (generation === 0)
            return <p className="gv-quiet">Validation has not published results yet.</p>;

          const list = groups.data ?? [];

          return (
            <ul className="gv-list">
              {list.map((group) => (
                <li key={group.key} className="gv-sig" data-severity="risk">
                  <span className="gv-sig-count">{fmt(group.issueCount)}</span>
                  <span className="gv-sig-text">
                    <b>{group.label}</b>{" "}
                    <span className="gv-sub">
                      in {fmt(group.affectedFileCount)}{" "}
                      {plural(group.affectedFileCount, "file", "files")}
                      {group.otherVariants > 0 && `, across ${group.otherVariants} more variants`}
                    </span>
                  </span>
                  <button type="button" className="gv-link gv-sig-act" onClick={() => openIssues()}>
                    Open issues
                  </button>
                </li>
              ))}
              {!list.length && (
                <li className="gv-sig" data-severity="ok">
                  <span className="gv-sig-count">
                    <span aria-hidden="true">✓</span>
                  </span>
                  <span className="gv-sig-text">No validation issues</span>
                  <span />
                </li>
              )}
            </ul>
          );
        }}
      </BlockBody>
    </Block>
  );
}

function InMotion() {
  const queryClient = useQueryClient();
  const members = useAggregatePart("members");
  const summaries = useTypeSummaries();
  // While the index rebuilds, its figures are empty: read nothing until it finishes.
  const rebuilding = members.data?.facts.rebuilding ?? false;
  const figures = rebuilding ? undefined : members.data?.members;

  const names = useMemo(
    () =>
      figures
        ? [...figures.types.values()].flatMap((type) =>
            type.lifecycle && type.count ? [type.name] : [],
          )
        : [],
    [figures],
  );

  const docs = useTypeDocs(names);
  const docsReady = names.every((name) => docs.docs[name] !== undefined);

  const types = useMemo(
    () => (figures && docsReady ? motionTypes(figures, docs.docs) : []),
    [figures, docsReady, docs.docs],
  );

  const document = activeRecordsQuery(types);

  const records = useGraphQL<JsonObject>(document, undefined, {
    enabled: document !== "",
    partial: true,
  });

  const pages = useMemo(
    () =>
      records.data
        ? parseRecords(
            records.data,
            types.flatMap((entry) => docs.docs[entry.type] ?? []),
          ).pages
        : null,
    [records.data, types, docs.docs],
  );

  const status = blockStatus([
    members.read,
    summaries.read,
    {
      pending: !rebuilding && (!figures || (!docsReady && !docs.error)),
      error: docs.error,
      retry: () => refetchFailedTypeDocs(queryClient),
    },
    ...(document ? [readOf(records)] : []),
  ]);

  const label = (type: string, many: boolean) => {
    const summary = summaries.data?.get(type);

    return many ? (summary?.pluralLabel ?? type) : (summary?.label ?? type);
  };

  return (
    <Block
      title="In motion"
      caption="Records in an active lifecycle stage, any type, newest change first."
    >
      <BlockBody status={status} what="work in motion">
        {() => {
          if (rebuilding) return <Rebuilding />;

          const moving = types.flatMap((entry) => {
            const page = pages?.get(entry.type)?.records ?? [];

            return page.length ? [{ ...entry, records: page }] : [];
          });

          if (!moving.length) return <p className="gv-quiet">Nothing in an active stage.</p>;

          return moving.map((entry) => {
            const more = entry.records.length > MOTION_PER_TYPE;

            return (
              <div key={entry.type} className="gv-mo-group">
                <h3 className="gv-mo-type">
                  {label(entry.type, true)}{" "}
                  <span className="gv-num">
                    {Math.min(entry.records.length, MOTION_PER_TYPE)}
                    {more ? "+" : ""}
                  </span>
                </h3>
                <ul className="gv-list">
                  {entry.records.slice(0, MOTION_PER_TYPE).map((record) => {
                    const value = record.values.get(entry.field);

                    return (
                      <li key={record.key} className="gv-mo">
                        <div className="gv-title">
                          {isString(value) ? (
                            <StatusMark
                              value={value}
                              values={entry.values}
                              hideLabel
                              className="gv-mark"
                            />
                          ) : (
                            <span className="gv-mark-slot" aria-hidden="true" />
                          )}
                          <button
                            type="button"
                            className="gv-record"
                            onClick={() => openNode(record.ref)}
                          >
                            {record.title}
                          </button>
                          {record.updatedAt !== null && (
                            <RelativeTime value={record.updatedAt} className="gv-num gv-mo-time" />
                          )}
                        </div>
                      </li>
                    );
                  })}
                </ul>
                {more && (
                  <button
                    type="button"
                    className="gv-link gv-more gv-mo-more"
                    onClick={() => openCollection(entry.type)}
                  >
                    More in {lower(label(entry.type, true))}
                  </button>
                )}
              </div>
            );
          });
        }}
      </BlockBody>
    </Block>
  );
}

function RecentChanges() {
  const summaries = useTypeSummaries();

  const recent = useQuery({
    queryKey: ["group-views", "recent-notes", RECENT_NOTES],
    queryFn: ({ signal }) =>
      getJSON(`/api/v1/ontology/types/__all__?limit=${RECENT_NOTES}`, signal),
    select: parseRecentNotes,
  });

  const status = blockStatus([readOf(recent), summaries.read]);

  return (
    <Block title="Recent changes" caption="Every note, untyped included, newest first.">
      <BlockBody status={status} what="recent changes">
        {() =>
          recent.data?.length ? (
            <ul className="gv-list">
              {recent.data.map((note) => (
                <li key={note.path} className="gv-out">
                  <span>
                    <button type="button" className="gv-inline" onClick={() => openNode(note.ref)}>
                      {note.title}
                    </button>{" "}
                    <span className="gv-sub">
                      {note.type ? (summaries.data?.get(note.type)?.label ?? note.type) : "untyped"}
                      {note.folder && ` · ${note.folder}/`}
                    </span>
                  </span>
                  {note.updatedAt !== null && (
                    <RelativeTime value={note.updatedAt} className="gv-num" />
                  )}
                </li>
              ))}
            </ul>
          ) : (
            <p className="gv-quiet">No change times recorded.</p>
          )
        }
      </BlockBody>
    </Block>
  );
}

export default function WorkspaceBriefing(_props: ViewModuleProps) {
  return (
    <main className="gv-page">
      <div className="gv-brief gv-brief-two">
        <div className="gv-brief-col">
          <NeedsAttention />
          <InMotion />
        </div>
        <div className="gv-brief-col">
          <RecentChanges />
        </div>
      </div>
    </main>
  );
}
