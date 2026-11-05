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
  StatusMark,
  openCollection,
  openIssues,
  openNode,
  useGraphQL,
  useTypeDocs,
  useValidationSummaries,
  validationScopeKey,
  type ViewModuleProps,
} from "@rhizome/kit";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo } from "react";

import { isString, parseRecords, type JsonObject, type JsonValue } from "./api.ts";
import { ChangeRow } from "./briefing-activity.tsx";
import { Block, BlockBody, MoreToggle, Rebuilding, lower, plural } from "./briefing-parts.tsx";
import "./components.tsx";
import { LastChanged, NoIssues, ValidationFreshness } from "./validation-freshness.tsx";
import { getJSON, refetchFailedTypeDocs } from "./load.ts";
import { useRememberedOpen } from "./remembered.ts";
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
  const info = "Validation issues by variant, untyped and ambiguous notes included.";

  // Green only for a current, clean run; a stale or unfinished one keeps the block.
  if (
    status.status === "ready" &&
    generation > 0 &&
    groups.data?.length === 0 &&
    validation.health === "current_clean"
  )
    return null;

  return (
    <Block
      title="Needs attention"
      count={status.status === "ready" ? fmt(total) : undefined}
      info={info}
    >
      <BlockBody status={status} what="what needs attention">
        {() => {
          if (generation === 0)
            return <p className="gv-quiet">Validation has not published results yet.</p>;

          if (!groups.data?.length)
            return (
              <p className="gv-quiet">The last validation run is out of date or unfinished.</p>
            );

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
            </ul>
          );
        }}
      </BlockBody>
    </Block>
  );
}

function InMotion() {
  const queryClient = useQueryClient();
  const expanded = useRememberedOpen("briefing.expanded");
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
      className="gv-motion"
      controls={<span className="gv-motion-age-label">Last changed</span>}
      count={
        status.status === "ready" && !rebuilding
          ? types.reduce((sum, type) => sum + type.total, 0)
          : undefined
      }
      info={{
        text: "Records in an active lifecycle stage, any type, newest change first.",
        display: "title",
      }}
    >
      <BlockBody status={status} what="work in motion">
        {() => {
          if (rebuilding) return <Rebuilding />;

          const moving = types.flatMap((entry) => {
            const page = pages?.get(entry.type)?.records ?? [];

            return page.length ? [{ ...entry, records: page }] : [];
          });

          if (!moving.length) return <p className="gv-quiet">Nothing in an active stage.</p>;

          const entries = moving
            .flatMap((entry) => entry.records.map((record) => ({ entry, record })))
            .sort((a, b) => (b.record.updatedAt ?? 0) - (a.record.updatedAt ?? 0));

          const open = expanded.isOpen("in-motion", false);
          const shown = open ? entries : entries.slice(0, MOTION_PER_TYPE);

          const mixed =
            new Set(entries.map(({ entry, record }) => record.values.get(entry.field))).size > 1;

          return (
            <>
              <ul className="gv-list gv-changes">
                {shown.map(({ entry, record }) => {
                  const value = record.values.get(entry.field);

                  return (
                    <ChangeRow
                      key={record.key}
                      line={{
                        key: record.key,
                        title: record.title,
                        type: label(entry.type, false),
                        folder: record.path.split("/").slice(0, -1).join("/"),
                        time: record.updatedAt,
                        onOpen: () => openNode(record.ref),
                      }}
                      mark={
                        mixed && isString(value) ? (
                          <StatusMark
                            value={value}
                            values={entry.values}
                            hideLabel
                            className="gv-mark"
                          />
                        ) : undefined
                      }
                    />
                  );
                })}
              </ul>
              {entries.length > MOTION_PER_TYPE && (
                <MoreToggle
                  hidden={entries.length - MOTION_PER_TYPE}
                  open={open}
                  onToggle={() => expanded.setOpen("in-motion", !open)}
                />
              )}
              {open &&
                moving
                  .filter((entry) => entry.total > entry.records.length)
                  .map((entry) => (
                    <div key={entry.type} className="gv-mo-more">
                      <button
                        type="button"
                        className="gv-link gv-more"
                        onClick={() => openCollection(entry.type)}
                      >
                        Open {lower(label(entry.type, true))}
                      </button>
                    </div>
                  ))}
            </>
          );
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
    <Block
      title="Recent changes"
      info={{ text: "Every note, untyped included, newest first.", display: "title" }}
    >
      <BlockBody status={status} what="recent changes">
        {() =>
          recent.data?.length ? (
            <ul className="gv-list gv-changes">
              {recent.data.map((note) => (
                <ChangeRow
                  key={note.path}
                  line={{
                    key: note.path,
                    title: note.title,
                    type: note.type
                      ? (summaries.data?.get(note.type)?.label ?? note.type)
                      : "Untyped",
                    folder: note.folder,
                    time: note.updatedAt,
                    onOpen: () => openNode(note.ref),
                  }}
                />
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

function WorkspaceFacts() {
  const members = useAggregatePart("members");
  const validation = useValidationSummaries(GLOBAL);

  const recent = useQuery({
    queryKey: ["group-views", "recent-notes", RECENT_NOTES],
    queryFn: ({ signal }) =>
      getJSON(`/api/v1/ontology/types/__all__?limit=${RECENT_NOTES}`, signal),
    select: parseRecentNotes,
  });

  const latest = recent.data?.[0]?.updatedAt;
  const issues = validation.summaries.get(validationScopeKey(GLOBAL[0]))?.issueCount;

  return (
    <div className="gv-facts">
      <span className="gv-facts-list" title="Validation includes untyped and ambiguous notes.">
        {members.data && !members.data.facts.rebuilding && (
          <span>
            <b>{fmt(members.data.facts.totalNotes)}</b> notes
          </span>
        )}
        {issues !== undefined &&
          (issues > 0 ? (
            <span className="gv-bad">
              <b>{fmt(issues)}</b> issues
            </span>
          ) : (
            <NoIssues />
          ))}
        <ValidationFreshness />
        {latest && <LastChanged value={latest} />}
      </span>
    </div>
  );
}

export default function WorkspaceBriefing(_props: ViewModuleProps) {
  return (
    <main className="gv-page">
      <WorkspaceFacts />
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
