// Activity blocks both Briefings share: an in-motion row, the hints for
// lifecycles that cannot list work in motion, Recent changes, and the notes
// outside the records that link with them.
import { RelativeTime, openNote, typeLabel } from "@rhizome/kit";
import { Fragment, useMemo, useState, type ReactNode } from "react";

import { recentChanges, type Boundary, type ChangeMoment, type MotionEntry } from "./activity.ts";
import { Block, MoreToggle, counted, lower, memberOf, plural } from "./briefing-parts.tsx";
import { IssueCount, RecordMark, RecordTitle, openRecord } from "./components.tsx";
import { useRememberedOpen } from "./remembered.ts";
import {
  fieldLabel,
  memberLabel,
  type GroupModel,
  type GroupRecord,
  type Member,
} from "./model.ts";

/** Lines in Recent changes; a collapsed burst is one line. */
export const CHANGE_LINES = 18;

/** Notes listed outside the records before "+N more". */
export const OUTSIDE_NOTES = 8;

export function MotionRow({ member, entry }: { member: Member; entry: MotionEntry }) {
  const { record, note } = entry;

  return (
    <li className="gv-mo">
      <div className="gv-title">
        <RecordMark member={member} record={record} />
        <RecordTitle record={record} />
        <IssueCount record={record} />
        {record.updatedAt !== null && (
          <RelativeTime value={record.updatedAt} className="gv-age gv-mo-time" />
        )}
      </div>
      {note && (
        <div className="gv-mo-note">
          {note.label && <em>{note.label}</em>}
          {note.text}
        </div>
      )}
    </li>
  );
}

function enumName(member: Member) {
  const field = member.fields.find((candidate) => candidate.name === member.lifecycle?.field);

  return field?.typeName ?? member.lifecycle?.field ?? "";
}

/**
 * Hints for members whose records cannot be in motion: lifecycles without an
 * `active` stage, by enum, and members without a lifecycle.
 */
export function StageHints({
  model,
  withoutActiveStage,
  withoutLifecycle,
}: {
  model: GroupModel;
  withoutActiveStage: readonly string[];
  withoutLifecycle: readonly string[];
}) {
  const byEnum = new Map<string, string[]>();

  for (const member of withoutActiveStage.flatMap((name) => memberOf(model, name) ?? [])) {
    const name = enumName(member);
    byEnum.set(name, [...(byEnum.get(name) ?? []), memberLabel(model, member, { plural: true })]);
  }

  const unstatused = withoutLifecycle.flatMap((name) => memberOf(model, name) ?? []);

  return (
    <>
      {byEnum.size > 0 && (
        <p className="gv-hint">
          {[...byEnum].map(([name, labels], index) => (
            <Fragment key={name}>
              {index > 0 && " and "}
              <code>{name}</code> ({labels.join(", ")})
            </Fragment>
          ))}{" "}
          {plural(byEnum.size, "has", "have")} no active stage; declare an active stage with{" "}
          <code>@view(stage: active)</code> on a value to list its records here.
        </p>
      )}
      {unstatused.length > 0 && (
        <p className="gv-hint">
          No lifecycle:{" "}
          {unstatused.map((member) => memberLabel(model, member, { plural: true })).join(", ")}.
        </p>
      )}
    </>
  );
}

/** One line in Recent changes: what changed, its type and folder, and how long ago. */
export type ChangeLine = {
  key: string;
  title: string;
  /** The note's type label, or "untyped". */
  type: string;
  /** The folder holding the note, without a trailing slash. */
  folder: string;
  time: number | null;
  onOpen: () => void;
};

/**
 * The one Recent changes list every scope uses: a title line, a secondary
 * line with the type and the folder in mono, and the age in a fixed column
 * at the right, so ages align down the list.
 */
export function ChangeRow({ line, children }: { line: ChangeLine; children?: ReactNode }) {
  return (
    <li className="gv-change">
      <span className="gv-change-main">
        <button type="button" className="gv-inline gv-change-title" onClick={line.onOpen}>
          {line.title}
        </button>
        <span className="gv-change-meta">
          {line.type}
          {line.folder && (
            <>
              {" · "}
              <span className="gv-path">{line.folder}/</span>
            </>
          )}
        </span>
        {children}
      </span>
      {line.time !== null && <RelativeTime value={line.time} className="gv-change-age" />}
    </li>
  );
}

const folderOf = (path: string) => path.split("/").slice(0, -1).join("/");

function recordLine(model: GroupModel, record: GroupRecord): ChangeLine {
  return {
    key: record.key,
    title: record.title,
    type: typeLabelOf(model, record),
    folder: folderOf(record.path),
    time: record.updatedAt,
    onOpen: () => openRecord(record),
  };
}

/** A record's type label: its concrete type's, else its member's. */
function typeLabelOf(model: GroupModel, record: GroupRecord) {
  const labels = model.typeLabels.get(record.type);

  if (labels) return typeLabel(labels, { count: 1 });
  const member = memberOf(model, record.member);

  return member ? memberLabel(model, member, { count: 1 }) : fieldLabel(record.type);
}

/** A burst's counts by member, or by implementing type when the one member is an interface. */
function burstCounts(model: GroupModel, moment: ChangeMoment) {
  if (model.members.length === 1 && model.members[0]?.kind === "interface")
    return moment.byType
      .map((entry) => `${entry.count} ${typeName(model, entry.type, entry.count)}`)
      .join(", ");

  return moment.byMember.map((entry) => counted(model, entry.member, entry.count)).join(", ");
}

/** Records changed in the same minute: one line that expands to the records. */
function Burst({ model, moment }: { model: GroupModel; moment: ChangeMoment }) {
  const [open, setOpen] = useState(false);
  const count = moment.records.length;

  return (
    <>
      <li className="gv-change">
        <span className="gv-change-main">
          <button
            type="button"
            className="gv-inline gv-change-title"
            aria-expanded={open}
            onClick={() => setOpen(!open)}
          >
            {count} records changed together
          </button>
          <span className="gv-change-meta">{burstCounts(model, moment)}</span>
        </span>
        <RelativeTime value={moment.time} className="gv-change-age" />
      </li>
      {open &&
        moment.records.map((record) => (
          <ChangeRow key={record.key} line={recordLine(model, record)} />
        ))}
    </>
  );
}

export function RecentChanges({ model, info }: { model: GroupModel; info?: string }) {
  const moments = useMemo(
    () => recentChanges(model, CHANGE_LINES).flatMap((day) => day.moments),
    [model],
  );

  return (
    <Block title="Recent changes" info={info}>
      {moments.length === 0 && <p className="gv-quiet">No change times recorded.</p>}
      <ul className="gv-list gv-changes">
        {moments.map((moment) =>
          moment.burst ? (
            <Burst key={moment.time} model={model} moment={moment} />
          ) : (
            moment.records.map((record) => (
              <ChangeRow key={record.key} line={recordLine(model, record)} />
            ))
          ),
        )}
      </ul>
    </Block>
  );
}

/** A note type's label, lowercased and counted. */
function typeName(model: GroupModel, type: string, count: number) {
  const labels = model.typeLabels.get(type);

  return lower(labels ? typeLabel(labels, { count }) : fieldLabel(type));
}

/** A note type's label outside the records, or "untyped note". */
function outsideLabel(model: GroupModel, type: string | null, count: number) {
  return type === null
    ? plural(count, "untyped note", "untyped notes")
    : typeName(model, type, count);
}

/**
 * Notes outside the records that link with them, as `boundary()` finds them:
 * counts by type, then the most connected notes.
 */
export function OutsideNotes({
  model,
  outside,
  title,
  info,
  empty,
}: {
  model: GroupModel;
  outside: Boundary;
  title: string;
  info: string;
  empty: string;
}) {
  const expanded = useRememberedOpen("briefing.expanded");
  const open = expanded.isOpen("outside", false);
  const setOpen = (next: boolean) => expanded.setOpen("outside", next);
  const shown = open ? outside.notes : outside.notes.slice(0, OUTSIDE_NOTES);

  return (
    <Block
      title={title}
      count={`${outside.incomplete ? "at least " : ""}${outside.notes.length} ${plural(outside.notes.length, "note", "notes")}`}
      info={`${info}${
        outside.incomplete
          ? " Some records have more links than were read, so this list is incomplete."
          : ""
      }`}
    >
      {outside.byType.length > 0 && (
        <p className="gv-outsum">
          {outside.byType.map((entry) => (
            <span key={entry.type ?? ""}>
              <b>{entry.count}</b> {outsideLabel(model, entry.type, entry.count)}
            </span>
          ))}
        </p>
      )}
      {outside.notes.length === 0 && <p className="gv-quiet">{empty}</p>}
      <ul className="gv-list gv-changes">
        {shown.map((note) => (
          <li key={note.path} className="gv-change">
            <span className="gv-change-main">
              <button
                type="button"
                className="gv-inline gv-change-title"
                title={note.path}
                onClick={() => openNote(note.path)}
              >
                {note.title}
              </button>
              <span className="gv-change-meta">
                {outsideLabel(model, note.type, 1)}
                {folderOf(note.path) && (
                  <>
                    {" · "}
                    <span className="gv-path">{folderOf(note.path)}/</span>
                  </>
                )}
              </span>
            </span>
            <span className="gv-change-age">
              {note.touches.length}
              <span className="sr-only">
                {" "}
                linked {plural(note.touches.length, "record", "records")}
              </span>
            </span>
          </li>
        ))}
      </ul>
      {outside.notes.length > OUTSIDE_NOTES && (
        <MoreToggle
          hidden={outside.notes.length - OUTSIDE_NOTES}
          open={open}
          onToggle={() => setOpen(!open)}
        />
      )}
    </Block>
  );
}
