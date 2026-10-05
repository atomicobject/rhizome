// The type Briefing (SPEC-0112): for one type or interface collection, what
// needs attention, what is in motion, and what changed; how its records spread
// across each enum field and by month; how each relation and reverse field is
// filled; the notes of other types that link in; and its guide. It is the group
// Briefing's loader and blocks over a one-member model, in the same three
// columns.
import {
  HighlightProvider,
  StatusMark,
  openNote,
  openView,
  useViewContext,
  type ViewContext,
  type ViewModuleProps,
} from "@rhizome/kit";
import { useMemo } from "react";

import { boundary, inMotion, needsAttention, reverseGaps } from "./activity.ts";
import { REVERSE_SAMPLE } from "./api.ts";
import { MotionRow, OutsideNotes, RecentChanges } from "./briefing-activity.tsx";
import { NeedsAttention, type MemberAction } from "./briefing-attention.tsx";
import { Implementors } from "./briefing-contents.tsx";
import { Block, MoreToggle, lower, plural } from "./briefing-parts.tsx";
import {
  dateMonths,
  distributions,
  fieldConnections,
  type DateMonths,
  type Distribution,
  type FieldConnection,
} from "./collection.ts";
import { GroupFacts, GroupPage } from "./components.tsx";
import { linkGraph } from "./graph.ts";
import { useCollectionModel } from "./load.ts";
import { fieldLabel, type GroupModel, type Member } from "./model.ts";
import { useRememberedOpen } from "./remembered.ts";

/** In-motion records listed before "+N more". */
export const MOTION_RECORDS = 8;

function InMotion({ model, member }: { model: GroupModel; member: Member }) {
  const motion = useMemo(() => inMotion(model), [model]);
  const entries = motion.groups[0]?.entries ?? [];
  const expanded = useRememberedOpen("briefing.expanded");
  const open = expanded.isOpen("in-motion", false);
  const setOpen = (next: boolean) => expanded.setOpen("in-motion", next);
  const shown = open ? entries : entries.slice(0, MOTION_RECORDS);

  return (
    <Block
      title="In motion"
      count={entries.length}
      caption="Records whose lifecycle value is in the active stage, newest change first."
    >
      {entries.length === 0 && <p className="gv-quiet">Nothing in an active stage.</p>}
      <ul className="gv-list">
        {shown.map((entry) => (
          <MotionRow key={entry.record.key} member={member} entry={entry} />
        ))}
      </ul>
      {entries.length > MOTION_RECORDS && (
        <MoreToggle
          hidden={entries.length - MOTION_RECORDS}
          open={open}
          onToggle={() => setOpen(!open)}
        />
      )}
    </Block>
  );
}

const valueLabel = (value: { name: string; label?: string }) =>
  value.label ?? fieldLabel(value.name);

/** One enum field's spread: a bar of its values, in enum order, and their counts. */
function DistributionRow({ distribution }: { distribution: Distribution }) {
  const { field, role, values, options, empty, other } = distribution;
  const category = role === "category";

  return (
    <div className="gv-spread">
      <h3 className="gv-spread-head">
        {fieldLabel(field)} <span className="gv-sub">{role}</span>
      </h3>
      <div className="gv-dist gv-dist-wide" aria-hidden="true">
        {values.map(({ value, count }) => (
          <i
            key={value.name}
            data-tone={category ? "category" : (value.tone ?? "neutral")}
            style={{ flexGrow: count }}
            title={`${valueLabel(value)}: ${count}`}
          />
        ))}
        {other > 0 && <i data-other="" style={{ flexGrow: other }} title={`Other: ${other}`} />}
        {empty > 0 && <i data-empty="" style={{ flexGrow: empty }} title={`Empty: ${empty}`} />}
      </div>
      <p className="gv-counts gv-spread-key">
        {values.map(({ value, count }) => (
          <span key={value.name}>
            {category ? valueLabel(value) : <StatusMark value={value.name} values={options} />}
            <span className="gv-num">{count}</span>
          </span>
        ))}
        {other > 0 && (
          <span className="gv-faint" title="Values the enum does not list">
            Other <span className="gv-num">{other}</span>
          </span>
        )}
        {empty > 0 && (
          <span className="gv-faint">
            Empty <span className="gv-num">{empty}</span>
          </span>
        )}
      </p>
    </div>
  );
}

const monthLabel = (month: string) =>
  new Date(Number(month.slice(0, 4)), Number(month.slice(5, 7)) - 1, 1).toLocaleDateString(
    undefined,
    { month: "short", year: "numeric" },
  );

/** Records per month of the primary date, as bars from the first month to the last. */
function MonthChart({ chart }: { chart: DateMonths }) {
  const { months, earlier, undated } = chart;
  const busiest = months.reduce((best, entry) => (entry.count > best.count ? entry : best));
  const label = fieldLabel(chart.field);
  const first = monthLabel(months[0].month);
  const last = monthLabel(months[months.length - 1].month);

  return (
    <div className="gv-spread">
      <h3 className="gv-spread-head">
        {label} <span className="gv-sub">per month</span>
      </h3>
      <div
        className="gv-hist"
        role="img"
        aria-label={`${label} per month, ${first} to ${last}: busiest ${monthLabel(busiest.month)} with ${busiest.count}`}
      >
        {months.map((entry) => (
          <i
            key={entry.month}
            style={{ height: `${(entry.count / busiest.count) * 100}%` }}
            title={`${monthLabel(entry.month)}: ${entry.count}`}
          />
        ))}
      </div>
      <p className="gv-hist-axis">
        <span>{first}</span>
        <span>
          busiest {monthLabel(busiest.month)} · {busiest.count}
        </span>
        <span>{last}</span>
      </p>
      {(earlier > 0 || undated > 0) && (
        <p className="gv-cap">
          {[earlier > 0 && `${earlier} earlier`, undated > 0 && `${undated} undated`]
            .filter(Boolean)
            .join(" · ")}
        </p>
      )}
    </div>
  );
}

function SpreadBlock({ model, member }: { model: GroupModel; member: Member }) {
  const spreads = useMemo(() => distributions(member), [member]);
  const months = useMemo(() => dateMonths(member, Date.now()), [member]);
  const records = member.records.length;

  return (
    <Block
      title="Shape"
      caption={`How the ${records} ${plural(records, "record", "records")} spread across each enum field${months ? " and by month" : ""}.`}
    >
      {member.kind === "interface" && (
        <div className="gv-spread">
          <h3 className="gv-spread-head">Implementing types</h3>
          <Implementors model={model} member={member} />
        </div>
      )}
      {spreads.map((spread) => (
        <DistributionRow key={spread.field} distribution={spread} />
      ))}
      {months && <MonthChart chart={months} />}
      {!spreads.length && !months && <p className="gv-quiet">No enum or date fields to chart.</p>}
    </Block>
  );
}

/** A field's target type by its plural label, or the type name humanized. */
function targetLabel(model: GroupModel, type: string) {
  return model.typeLabels.get(type)?.pluralLabel ?? fieldLabel(type);
}

function ConnectionRow({ model, connection }: { model: GroupModel; connection: FieldConnection }) {
  const { field, direction, filled, total, top, distinct, sampled } = connection;
  const share = total ? filled / total : 0;

  return (
    <li className="gv-rel">
      <div className="gv-rel-top">
        <b>{fieldLabel(field)}</b>
        <span className="gv-sub">
          {direction === "forward" ? "→" : "←"} {targetLabel(model, connection.targetType)}
        </span>
        <span className="gv-num">
          {filled}/{total}
        </span>
      </div>
      <div className="gv-fill" aria-hidden="true">
        <i data-thin={share < 0.5 || undefined} style={{ width: `${share * 100}%` }} />
      </div>
      {top.length > 0 ? (
        <p className="gv-tops">
          {top.map(({ note, count }) => (
            <span key={note.path}>
              <button type="button" className="gv-inline" onClick={() => openNote(note.path)}>
                {note.title}
              </button>{" "}
              <span className="gv-num">{count}</span>
            </span>
          ))}
          {distinct > top.length && <span className="gv-sub">+{distinct - top.length} more</span>}
          {sampled && (
            <span className="gv-sub">counted from the first {REVERSE_SAMPLE} of each record</span>
          )}
        </p>
      ) : (
        <p className="gv-rel-none">
          {direction === "forward"
            ? "No record fills it yet."
            : "The schema allows these links; no record has one yet."}
        </p>
      )}
    </li>
  );
}

function Connections({ model, member }: { model: GroupModel; member: Member }) {
  const connections = useMemo(() => fieldConnections(member), [member]);

  return (
    <Block
      title="Connections"
      caption="Each people, relation, and reverse field: how many records fill it, and its most common targets."
    >
      {connections.length === 0 && <p className="gv-quiet">No relation or reverse fields.</p>}
      <ul className="gv-list">
        {connections.map((connection) => (
          <ConnectionRow key={connection.field} model={model} connection={connection} />
        ))}
      </ul>
    </Block>
  );
}

function Guide({ model, context }: { model: GroupModel; context: ViewContext }) {
  const { guide, views } = model;

  return (
    <Block title="Guide">
      {guide ? (
        <>
          <p className="gv-res">
            <button type="button" className="gv-link" onClick={() => openNote(guide.path)}>
              {guide.title}
            </button>
            <span className="gv-sub">companion doc</span>
          </p>
          {guide.summary && <p className="gv-typ-desc">{guide.summary}</p>}
        </>
      ) : (
        <p className="gv-quiet">No companion doc.</p>
      )}
      {views.map((view) => (
        <p key={view.id} className="gv-res">
          <button type="button" className="gv-link" onClick={() => openView(view.id, context)}>
            {view.name}
          </button>
          <span className="gv-sub">authored view</span>
        </p>
      ))}
    </Block>
  );
}

function BriefingBody({ model, member }: { model: GroupModel; member: Member }) {
  const context = useViewContext();
  const graph = useMemo(() => linkGraph(model), [model]);

  const signals = useMemo(
    () => [...needsAttention(model, graph, Date.now()), ...reverseGaps(member)],
    [model, graph, member],
  );

  const linkedIn = useMemo(() => boundary(model, { links: false }), [model]);
  const { tableChoice } = model;

  // The collection is already open, so a signal's action opens its Table.
  const openTable: MemberAction | undefined = tableChoice
    ? { label: () => "Open table", run: () => openView(tableChoice, context) }
    : undefined;

  return (
    <HighlightProvider>
      <GroupFacts model={model} collection />
      <div className="gv-brief">
        <div className="gv-brief-col">
          <NeedsAttention
            model={model}
            signals={signals}
            caption="Validation issues and declared warning values."
            memberAction={openTable}
          />
          {/* A lifecycle without an active stage (a contract) has nothing in motion by design. */}
          {member.lifecycle?.values.some((value) => value.stage === "active") && (
            <InMotion model={model} member={member} />
          )}
          <RecentChanges
            model={model}
            caption="Newest first. Four or more records changed in the same minute collapse into one line."
          />
        </div>
        <div className="gv-brief-col">
          <SpreadBlock model={model} member={member} />
          <OutsideNotes
            model={model}
            outside={linkedIn}
            title="Linked from outside"
            caption={`Notes of other types that link to these ${lower(member.pluralLabel)}, most connected first.`}
            empty="No notes of other types link here."
          />
        </div>
        <div className="gv-brief-col gv-brief-side">
          <Connections model={model} member={member} />
          <Guide model={model} context={context} />
        </div>
      </div>
    </HighlightProvider>
  );
}

export default function TypeBriefing(_props: ViewModuleProps) {
  const state = useCollectionModel();

  return (
    <GroupPage state={state} subject="collection">
      {(model) => model.members[0] && <BriefingBody model={model} member={model.members[0]} />}
    </GroupPage>
  );
}
