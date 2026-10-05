// Activity blocks both Briefings share: an in-motion row, the hints for
// lifecycles that cannot list work in motion, Recent changes, and the notes
// outside the records that link with them.
import { RecordChip, RelativeTime, openNote, typeLabel } from "@rhizome/kit";
import { Fragment, useMemo, useState } from "react";

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
          <RelativeTime value={record.updatedAt} className="gv-num gv-mo-time" />
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

const DAY = 24 * 60 * 60 * 1000;

function startOfDay(time: number) {
  const date = new Date(time);
  date.setHours(0, 0, 0, 0);

  return date.getTime();
}

/** "Today", "Yesterday, Oct 2", or "Thu, Oct 1". */
function dayLabel(time: number, now: number) {
  const days = Math.round((startOfDay(now) - startOfDay(time)) / DAY);
  const date = new Date(time);

  if (days === 0) return "Today";

  if (days === 1)
    return `Yesterday, ${date.toLocaleDateString(undefined, { month: "short", day: "numeric" })}`;

  return date.toLocaleDateString(undefined, {
    weekday: "short",
    month: "short",
    day: "numeric",
    year: date.getFullYear() === new Date(now).getFullYear() ? undefined : "numeric",
  });
}

const clockTime = (time: number) =>
  new Date(time).toLocaleTimeString(undefined, {
    hour: "2-digit",
    minute: "2-digit",
    hourCycle: "h23",
  });

function ChangeChips({ model, records }: { model: GroupModel; records: readonly GroupRecord[] }) {
  return (
    <span className="gv-chips">
      {records.map((record) => {
        const member = memberOf(model, record.member);

        return (
          <RecordChip
            key={record.key}
            recordKey={record.key}
            mark={member && <RecordMark member={member} record={record} />}
            onOpen={() => openRecord(record)}
          >
            {record.title}
          </RecordChip>
        );
      })}
    </span>
  );
}

/** A burst's counts by member, or by implementing type when the one member is an interface. */
function burstCounts(model: GroupModel, moment: ChangeMoment) {
  if (model.members.length === 1 && model.members[0]?.kind === "interface")
    return moment.byType
      .map((entry) => `${entry.count} ${typeName(model, entry.type, entry.count)}`)
      .join(", ");

  return moment.byMember.map((entry) => counted(model, entry.member, entry.count)).join(", ");
}

function Moment({ model, moment }: { model: GroupModel; moment: ChangeMoment }) {
  const [open, setOpen] = useState(false);

  return (
    <li className="gv-evt">
      <time className="gv-num" dateTime={new Date(moment.time).toISOString()}>
        {clockTime(moment.time)}
      </time>
      <div>
        {moment.burst && (
          <span className="gv-burst">
            <b>{moment.records.length} records</b> changed together: {burstCounts(model, moment)}.{" "}
            <button
              type="button"
              className="gv-link"
              aria-expanded={open}
              aria-label={`${open ? "Collapse" : "Expand"} the ${moment.records.length} records changed at ${clockTime(moment.time)}`}
              onClick={() => setOpen(!open)}
            >
              {open ? "Collapse" : "Expand"}
            </button>
          </span>
        )}
        {(!moment.burst || open) && <ChangeChips model={model} records={moment.records} />}
      </div>
    </li>
  );
}

export function RecentChanges({ model, caption }: { model: GroupModel; caption?: string }) {
  const days = useMemo(() => recentChanges(model, CHANGE_LINES), [model]);
  const now = Date.now();

  return (
    <Block title="Recent changes" caption={caption}>
      {days.length === 0 && <p className="gv-quiet">No change times recorded.</p>}
      {days.map((day) => (
        <div key={day.day} className="gv-day">
          <h3 className="gv-day-head">{dayLabel(day.moments[0]?.time ?? now, now)}</h3>
          <ul className="gv-list">
            {day.moments.map((moment) => (
              <Moment key={moment.time} model={model} moment={moment} />
            ))}
          </ul>
        </div>
      ))}
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
  caption,
  empty,
}: {
  model: GroupModel;
  outside: Boundary;
  title: string;
  caption: string;
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
      caption={`${caption}${
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
      <ul className="gv-list">
        {shown.map((note) => (
          <li key={note.path} className="gv-out">
            <span>
              <button type="button" className="gv-inline" onClick={() => openNote(note.path)}>
                {note.title}
              </button>{" "}
              <span className="gv-sub">{outsideLabel(model, note.type, 1)}</span>
            </span>
            <span className="gv-num">
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
