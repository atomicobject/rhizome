// Overview (SPEC-0117): what a scope holds, how its members connect, and how
// complete they are. One entry serves `workspace.overview` at All notes and
// `group.overview` for every display group. The facts strip and every block's
// heading paint at once; each block reads its own data and fills in, or fails
// with a retry, on its own.
//
// - Map, or its Matrix: members sized by records and the links between them.
// - Members: one comparable row per member.
// - At All notes: untyped notes by folder and the coverage line. On a group:
//   relations declared but unused, notes outside the group, guide and views.
import {
  openIssues,
  useValidationSummaries,
  useViewContext,
  useViewPreference,
  validationScopeKey,
  type ViewModuleProps,
} from "@rhizome/kit";
import { useMemo } from "react";

import { Block, BlockBody, Rebuilding, plural } from "./briefing-parts.tsx";
import "./components.tsx";
import { memberRows } from "./members.ts";
import { ScopeMap } from "./overview-map.tsx";
import { CoverageLine, DeclaredUnused, FolderBlock, GroupRecordsBlocks } from "./overview-side.tsx";
import { MemberTable, ScopeMatrix } from "./overview-tables.tsx";
import { useRememberedOpen } from "./remembered.ts";
import { declaredRelations, linkModel, scopeOf, type Scope } from "./scope.ts";
import {
  blockStatus,
  useAggregatePart,
  useScopeDocs,
  useScopeModel,
  type BlockStatus,
} from "./scope-load.ts";

type Mode = "map" | "matrix";

const MAP: Mode = "map";

const MODE = {
  defaultValue: MAP,
  validate: (value: unknown): value is Mode => value === "map" || value === "matrix",
};

const GLOBAL = [{ kind: "global" as const }];

const fmt = (count: number) => count.toLocaleString("en-US");

type ScopeData = ReturnType<typeof useScopeModel>;

type Expansion = {
  expanded: ReadonlySet<string>;
  onToggle: (group: string, open: boolean) => void;
};

function WorkspaceFacts({ data }: { data: ScopeData }) {
  const validation = useValidationSummaries(GLOBAL);
  const issues = validation.summaries.get(validationScopeKey(GLOBAL[0]))?.issueCount;
  const facts = data.facts;

  if (!facts || !data.model) return null;
  const typed = facts.totalNotes ? Math.round((facts.typedNotes / facts.totalNotes) * 100) : 0;

  return (
    <span className="gv-facts-list">
      <span>
        <b>{fmt(facts.totalNotes)}</b> notes
      </span>
      <span>
        <b>{typed}%</b> typed
      </span>
      <span>
        <b>{data.model.types.size}</b> note {plural(data.model.types.size, "type", "types")}
      </span>
      <button type="button" className="gv-issues" onClick={() => openIssues()}>
        {fmt(facts.ambiguousNotes)} ambiguous
      </button>
      {issues !== undefined && (
        <button type="button" className="gv-issues" onClick={() => openIssues()}>
          {fmt(issues)} {plural(issues, "issue", "issues")}
        </button>
      )}
    </span>
  );
}

function GroupFactsList({ data }: { data: ScopeData }) {
  if (!data.model) return null;
  const records = data.model.totals.count;
  const issues = data.model.totals.issueCount;

  return (
    <span className="gv-facts-list">
      <span>
        <b>{fmt(records)}</b> {plural(records, "record", "records")}
      </span>
      <span>
        <b>{data.model.nodes.length}</b> {plural(data.model.nodes.length, "type", "types")}
      </span>
      {issues ? (
        <span className="gv-bad">
          {fmt(issues)} {plural(issues, "issue", "issues")}
        </span>
      ) : (
        <span className="gv-ok">✓ no issues</span>
      )}
    </span>
  );
}

/** The scope's facts, each once on the page; the page's only loading state. */
function ScopeFacts({
  scope,
  data,
  status,
}: {
  scope: Scope;
  data: ScopeData;
  status: BlockStatus;
}) {
  const name = scope.kind === "workspace" ? "All notes" : scope.group;

  if (status.status === "error")
    return (
      <div className="gv-facts">
        <span className="gv-facts-warn" role="alert">
          Could not load {name}: {status.error.message}{" "}
          <button type="button" className="gv-link" onClick={status.retry}>
            Retry
          </button>
        </span>
      </div>
    );

  if (status.status === "loading" || data.missing || data.facts?.rebuilding)
    return (
      <div className="gv-facts">
        <span className="gv-facts-note" role="status">
          {status.status === "loading"
            ? `Loading ${name}…`
            : data.missing
              ? `No display group named ${name}.`
              : "The index is rebuilding; counts appear when it finishes."}
        </span>
      </div>
    );

  return (
    <div className="gv-facts">
      <span className="gv-facts-note">
        {scope.kind === "workspace"
          ? "Groups and types sized by records; edges count links between their notes."
          : "Member types sized by records; edges count record links."}
      </span>
      {scope.kind === "workspace" ? <WorkspaceFacts data={data} /> : <GroupFactsList data={data} />}
    </div>
  );
}

function MapBlock({ data, expanded, onToggle }: { data: ScopeData } & Expansion) {
  const mode = useViewPreference("overview.mode", MODE);
  const links = useAggregatePart("links");
  const group = data.model?.scope.kind === "group";
  const docs = useScopeDocs(group ? data.model : null);
  const status = blockStatus([...data.reads, links.read, ...(group ? [docs.read] : [])]);
  const summaries = data.summaries;
  const pairs = links.data?.links;

  const linked = useMemo(
    () => (data.model && pairs && summaries ? linkModel(data.model, pairs, summaries) : null),
    [data.model, pairs, summaries],
  );

  const relations = useMemo(
    () => (group && data.model && pairs ? declaredRelations(data.model, docs.docs, pairs) : []),
    [group, data.model, pairs, docs.docs],
  );

  const choose = (next: Mode) => void mode.set(next).catch(() => {});

  return (
    <Block
      title="Map"
      className="gv-slot-map"
      controls={
        <span className="gv-toggle" role="group" aria-label="Map or matrix">
          {(["map", "matrix"] as const).map((choice) => (
            <button
              key={choice}
              type="button"
              aria-pressed={mode.value === choice}
              onClick={() => choose(choice)}
            >
              {choice === "map" ? "Map" : "Matrix"}
            </button>
          ))}
        </span>
      }
    >
      <BlockBody status={status} what="the map">
        {() => {
          if (data.facts?.rebuilding || links.data?.facts.rebuilding) return <Rebuilding />;

          if (!data.model || !linked || !summaries) return null;

          return mode.value === "matrix" ? (
            <ScopeMatrix model={data.model} links={linked} relations={relations} />
          ) : (
            <ScopeMap
              model={data.model}
              links={linked}
              relations={relations}
              summaries={summaries}
              expanded={expanded}
              onExpand={onToggle}
            />
          );
        }}
      </BlockBody>
    </Block>
  );
}

function MembersBlock({ data, expanded, onToggle }: { data: ScopeData } & Expansion) {
  const links = useAggregatePart("links");
  const docs = useScopeDocs(data.model);
  const status = blockStatus([...data.reads, links.read, docs.read]);
  const figures = useAggregatePart("members").data?.members;

  const rows = useMemo(
    () =>
      data.model && figures && links.data?.links && data.summaries
        ? memberRows(data.model, {
            members: figures,
            pairs: links.data.links,
            docs: docs.docs,
            summaries: data.summaries,
            expanded,
          })
        : [],
    [data.model, figures, links.data, docs.docs, data.summaries, expanded],
  );

  return (
    <Block title="Members" className="gv-slot-members">
      <BlockBody status={status} what="members">
        {() =>
          data.facts?.rebuilding ? (
            <Rebuilding />
          ) : data.model ? (
            <MemberTable model={data.model} rows={rows} onToggle={onToggle} />
          ) : null
        }
      </BlockBody>
    </Block>
  );
}

function OverviewPage({ scope }: { scope: Scope }) {
  const remembered = useRememberedOpen("overview.expanded");

  const expanded = useMemo(
    () =>
      new Set(Object.entries(remembered.choices).flatMap(([group, open]) => (open ? [group] : []))),
    [remembered.choices],
  );

  const data = useScopeModel(scope, expanded);
  const status = blockStatus(data.reads);
  const expansion: Expansion = { expanded, onToggle: remembered.setOpen };

  return (
    <main className="gv-page">
      <ScopeFacts scope={scope} data={data} status={status} />
      {!data.missing && (
        <>
          <div className="gv-ov">
            <div className="gv-ov-main">
              <MapBlock data={data} {...expansion} />
            </div>
            <div className="gv-ov-side">
              {scope.kind === "workspace" ? (
                <>
                  <FolderBlock />
                  <CoverageLine />
                </>
              ) : (
                <>
                  <DeclaredUnused model={data.model} reads={data.reads} />
                  <GroupRecordsBlocks />
                </>
              )}
            </div>
          </div>
          <div className="gv-ov-members">
            <MembersBlock data={data} {...expansion} />
          </div>
        </>
      )}
    </main>
  );
}

export default function Overview(_props: ViewModuleProps) {
  const context = useViewContext();
  const scope = useMemo(() => scopeOf(context), [context]);

  if (!scope)
    return (
      <main className="gv-page">
        <p className="gv-state" role="status">
          Open this view from All notes or a display group.
        </p>
      </main>
    );

  return <OverviewPage scope={scope} />;
}
