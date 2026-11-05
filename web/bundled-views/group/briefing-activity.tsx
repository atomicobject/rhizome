// Activity blocks both Briefings share: an in-motion row, the hints for
// lifecycles that cannot list work in motion, Recent changes, and the notes
// outside the records that link with them.
import { DisclosureIcon, RelativeTime, openNote, openSearch, typeLabel } from "@rhizome/kit";
import { Fragment, useMemo, useState, type ReactNode } from "react";

import { recentChanges, type Boundary, type ChangeMoment, type MotionEntry } from "./activity.ts";
import { Block, MoreToggle, counted, lower, memberOf, plural } from "./briefing-parts.tsx";
import { commonMotionMetadata } from "./briefing-motion.tsx";
import { RecordMark, openRecord } from "./components.tsx";
import { useRememberedOpen } from "./remembered.ts";
import {
  fieldLabel,
  lifecycleValue,
  memberLabel,
  type GroupModel,
  type GroupRecord,
  type Member,
} from "./model.ts";

/** Lines in Recent changes; a collapsed burst is one line. */
export const CHANGE_LINES = 18;

/** Notes listed outside the records before "+N more". */
export const OUTSIDE_NOTES = 8;

/** One flat activity list, shared by all, group, and type Briefings. */
export function MotionList({
  model,
  entries,
}: {
  model: GroupModel;
  entries: readonly MotionEntry[];
}) {
  const expanded = useRememberedOpen("briefing.expanded");
  const open = expanded.isOpen("in-motion", false);
  const shown = open ? entries : entries.slice(0, 8);
  const metadata = useMotionMetadata(model, entries);
  const lines = shown.map(({ record }) => recordLine(model, record));

  const mixed =
    new Set(
      entries.map(({ record }) => {
        const member = memberOf(model, record.member);

        return member ? lifecycleValue(member, record) : null;
      }),
    ).size > 1;

  return (
    <>
      <ul className="gv-list gv-changes">
        {shown.map(({ record }, index) => {
          const member = memberOf(model, record.member);

          return (
            <ChangeRow
              key={record.key}
              line={recordLine(model, record)}
              compact={
                !!metadata &&
                lines[index].type.toLowerCase() === metadata.type.toLowerCase() &&
                lines[index].folder === metadata.folder
              }
              showType={lines.filter((line) => line.title === record.title).length > 1}
              repeatAge={
                index > 0 &&
                lines[index - 1].time !== null &&
                ageKey(lines[index - 1].time) === ageKey(record.updatedAt)
              }
              mark={mixed && member ? <RecordMark member={member} record={record} /> : undefined}
            />
          );
        })}
      </ul>
      {entries.length > 8 && (
        <MoreToggle
          hidden={entries.length - 8}
          open={open}
          onToggle={() => expanded.setOpen("in-motion", !open)}
        />
      )}
    </>
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

  const unstatused = withoutLifecycle
    .flatMap((name) => memberOf(model, name) ?? [])
    .map((member) => memberLabel(model, member, { plural: true }));

  if (byEnum.size === 0 && unstatused.length === 0) return null;

  const stageless = [...byEnum.values()].flat();

  // Match the section heading's info mark while keeping the native disclosure.
  return (
    <div className="gv-stage-note">
      <details className="gv-hint">
        <summary className="rz-info" aria-label="Why some types are not listed">
          <svg viewBox="0 0 16 16" aria-hidden="true">
            <circle cx="8" cy="8" r="6.25" fill="none" stroke="currentColor" strokeWidth="1.25" />
            <path
              d="M8 7.2v3.8M8 5.1v.1"
              stroke="currentColor"
              strokeWidth="1.5"
              strokeLinecap="round"
            />
          </svg>
        </summary>
        <div className="gv-hint-panel">
          <p className="gv-hint-lead">
            {[
              stageless.length > 0 && `${stageless.join(", ")} have no active stage configured.`,
              unstatused.length > 0 && `${unstatused.join(", ")} have no lifecycle.`,
            ]
              .filter(Boolean)
              .join(" ")}
          </p>
          {byEnum.size > 0 && (
            <p>
              To list them here, mark a value of{" "}
              {[...byEnum.keys()].map((name, index) => (
                <Fragment key={name}>
                  {index > 0 && " or "}
                  <code>{name}</code>
                </Fragment>
              ))}{" "}
              with <code>@view(stage: active)</code> in the schema.
            </p>
          )}
          {unstatused.length > 0 && <p>Records without a lifecycle field cannot be in motion.</p>}
        </div>
      </details>
    </div>
  );
}

/** One line in Recent changes: what changed, its type and folder, and how long ago. */
export type ChangeLine = {
  key: string;
  title: string;
  /** The note's type label, or "Untyped". */
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
export function ChangeRow({
  line,
  children,
  mark,
  compact = false,
  showType = false,
  repeatAge = false,
}: {
  line: ChangeLine;
  children?: ReactNode;
  mark?: ReactNode;
  compact?: boolean;
  showType?: boolean;
  repeatAge?: boolean;
}) {
  return (
    <li className={`gv-change${compact ? " gv-change-compact" : ""}`}>
      <span className="gv-change-main">
        <span className="gv-change-heading">
          {mark}
          <button
            type="button"
            className="gv-inline gv-change-title"
            title={line.title}
            onClick={line.onOpen}
          >
            {line.title}
          </button>
          {showType && <span className="gv-change-type">{line.type}</span>}
        </span>
        {!compact && (
          <span className="gv-change-meta">
            {!showType && line.type}
            {line.folder && (
              <>
                {!showType && " · "}
                <span className="gv-path" title={`${line.folder}/`}>
                  {line.folder}/
                </span>
              </>
            )}
          </span>
        )}
        {children}
      </span>
      {line.time !== null && (
        <RelativeTime
          value={line.time}
          className={`gv-change-age${repeatAge ? " gv-age-repeat" : ""}`}
        />
      )}
    </li>
  );
}

export function ageKey(time: number | null) {
  if (time === null) return null;
  const age = Math.max(0, Date.now() - time);

  if (age < 60_000) return "now";

  if (age < 3_600_000) return `${Math.round(age / 60_000)}m`;

  if (age < 86_400_000) return `${Math.floor(age / 3_600_000)}h`;

  if (age < 60 * 86_400_000) return `${Math.round(age / 86_400_000)}d`;

  return new Date(time).toDateString();
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

export function useMotionMetadata(model: GroupModel, entries: readonly MotionEntry[]) {
  const expanded = useRememberedOpen("briefing.expanded");
  const shown = expanded.isOpen("in-motion", false) ? entries : entries.slice(0, 8);

  return commonMotionMetadata(shown.map(({ record }) => recordLine(model, record)));
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
    <li className="gv-burst">
      <div className="gv-change">
        <span className="gv-change-main">
          <button
            type="button"
            className="gv-inline gv-change-title gv-burst-toggle"
            aria-expanded={open}
            onClick={() => setOpen(!open)}
          >
            <DisclosureIcon open={open} />
            {count} records changed together
          </button>
          <span className="gv-change-meta">{burstCounts(model, moment)}</span>
        </span>
        <RelativeTime value={moment.time} className="gv-change-age" />
      </div>
      {open && (
        <ul className="gv-list gv-burst-members" aria-label="Records changed together">
          {moment.records.map((record) => (
            <ChangeRow key={record.key} line={recordLine(model, record)} />
          ))}
        </ul>
      )}
    </li>
  );
}

export function RecentChanges({ model, info }: { model: GroupModel; info?: string }) {
  const moments = useMemo(
    () => recentChanges(model, CHANGE_LINES).flatMap((day) => day.moments),
    [model],
  );

  const records = moments.flatMap((moment) => moment.records);

  return (
    <Block title="Recent changes" info={info ? { text: info, display: "title" } : undefined}>
      {moments.length === 0 && <p className="gv-quiet">No change times recorded.</p>}
      <ul className="gv-list gv-changes">
        {moments.map((moment, index) =>
          moment.burst ? (
            <Burst key={moment.time} model={model} moment={moment} />
          ) : (
            moment.records.map((record) => (
              <ChangeRow
                key={record.key}
                line={recordLine(model, record)}
                showType={records.filter((item) => item.title === record.title).length > 1}
                repeatAge={index > 0 && ageKey(moments[index - 1].time) === ageKey(moment.time)}
              />
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

/** A label at the start of a line, in sentence case: "Technical spec". */
const sentence = (label: string) => label.charAt(0).toUpperCase() + label.slice(1);

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
  limit = OUTSIDE_NOTES,
}: {
  model: GroupModel;
  outside: Boundary;
  title: string;
  info: string;
  empty: string;
  /** Notes listed before "Show N more". */
  limit?: number;
}) {
  const expanded = useRememberedOpen("briefing.expanded");
  const open = expanded.isOpen("outside", false);
  const setOpen = (next: boolean) => expanded.setOpen("outside", next);
  const shown = open ? outside.notes : outside.notes.slice(0, limit);

  return (
    <Block
      title={title}
      count={`${outside.incomplete ? "at least " : ""}${outside.notes.length} ${plural(outside.notes.length, "note", "notes")}`}
      info={{
        text: `${info}${
          outside.incomplete
            ? " Some records have more links than were read, so this list is incomplete."
            : ""
        }`,
        display: "title",
      }}
    >
      {outside.byType.length > 0 && (
        <p className={`gv-outsum${title === "Linked from outside" ? " gv-outside-totals" : ""}`}>
          {outside.byType.map((entry) => (
            <span key={entry.type ?? ""}>
              <b className="gv-num">{entry.count}</b>{" "}
              <span>
                {title === "Linked from outside"
                  ? sentence(outsideLabel(model, entry.type, 1))
                  : outsideLabel(model, entry.type, entry.count)}
              </span>
            </span>
          ))}
        </p>
      )}
      {outside.incomplete && (
        <p className="gv-quiet">
          More notes may link to this group than are listed. Viewed records and their links are
          limited.{" "}
          <button
            type="button"
            className="gv-inline gv-link"
            onClick={() => openSearch({ folder: "" })}
          >
            Search all notes
          </button>
        </p>
      )}
      {outside.notes.length === 0 && <p className="gv-quiet">{empty}</p>}
      <ul className="gv-list gv-changes">
        {shown.map((note) => {
          // A timestamped file name reads by its slug; the file name stays as evidence.
          const readable = readableTitle(note.title);

          return (
            <li key={note.path} className="gv-change">
              <span className="gv-change-main">
                <button
                  type="button"
                  className="gv-inline gv-change-title"
                  title={note.path}
                  onClick={() => openNote(note.path)}
                >
                  {readable ?? note.title}
                </button>
                <span className="gv-change-meta">
                  {sentence(outsideLabel(model, note.type, 1))}
                  {(readable || folderOf(note.path)) && (
                    <>
                      {" · "}
                      <span className="gv-path">
                        {readable ? note.path : `${folderOf(note.path)}/`}
                      </span>
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
          );
        })}
      </ul>
      {outside.notes.length > limit && (
        <MoreToggle
          hidden={outside.notes.length - limit}
          open={open}
          onToggle={() => setOpen(!open)}
        />
      )}
    </Block>
  );
}

const STAMPED = /^\d{4}-\d{2}-\d{2}(?:[-_T]\d{2}[-:]?\d{2})?[-_ ]+(?=\S)/;

/** "2026-09-05-11-42-starter-skill-consolidation" reads "Starter skill consolidation"; other titles stay. */
export function readableTitle(title: string) {
  if (!STAMPED.test(title)) return null;
  const rest = title.replace(STAMPED, "").replace(/\.md$/i, "").replace(/[-_]+/g, " ").trim();

  return rest ? rest.charAt(0).toUpperCase() + rest.slice(1) : null;
}
