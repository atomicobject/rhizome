// React pieces the group views share. Styling lives in group.css.
//
// - GroupPage: loading, missing, and error states around a ready model.
// - GroupFacts: the strip under the host's title (records, members, issues,
//   last change, guide, truncation, a failed refresh), with view-specific
//   text on the left.
// - RecordMark, RecordTitle, LifecycleLabel, LifecycleCounts, IssueCount,
//   LinkedTitles, Dash: record- and member-level atoms.
// - openRecord(record), openMemberIssues(member): navigation helpers.
import {
  RelativeTime,
  StatusMark,
  openCollection,
  openIssues,
  openNode,
  openNote,
  orderedEnumValues,
} from "@rhizome/kit";
import { createContext, useContext, type ReactNode } from "react";

import "./group.css";
import { RECORD_CAP } from "./api.ts";
import type { GroupModelState } from "./load.ts";
import {
  lifecycleValue,
  memberLabel,
  type GroupModel,
  type GroupRecord,
  type LinkedNote,
  type Member,
} from "./model.ts";

export function openRecord(record: GroupRecord) {
  openNode(record.ref);
}

export function openMemberIssues(member: Member) {
  openIssues({ kind: member.kind, key: member.name });
}

/** Why the last refresh failed while an earlier model stays on screen; read by GroupFacts. */
const RefreshError = createContext<Error | null>(null);

const STATE_TEXT = {
  group: {
    loading: "Loading the group…",
    outside: "Open this view from a display group.",
    missing: (name: string) => `No display group named ${name}.`,
    failed: "Could not load the group",
  },
  collection: {
    loading: "Loading the collection…",
    outside: "Open this view from a type or interface.",
    missing: (name: string) => `No type or interface named ${name} in navigation.`,
    failed: "Could not load the collection",
  },
} as const;

/**
 * Renders `children` once the model is ready, and a plain state line otherwise,
 * worded for a display group or a type or interface `collection`.
 */
export function GroupPage({
  state,
  subject = "group",
  children,
}: {
  state: GroupModelState;
  subject?: keyof typeof STATE_TEXT;
  children: (model: GroupModel) => ReactNode;
}) {
  if (state.status === "ready") {
    return (
      <main className="gv-page" aria-busy={state.refreshing || undefined}>
        <RefreshError.Provider value={state.refreshError}>
          {children(state.model)}
        </RefreshError.Provider>
      </main>
    );
  }

  const text = STATE_TEXT[subject];

  const message =
    state.status === "loading"
      ? text.loading
      : state.status === "missing"
        ? state.name === null
          ? text.outside
          : text.missing(state.name)
        : `${text.failed}: ${state.error.message}`;

  return (
    <main className="gv-page">
      <p className="gv-state" role={state.status === "error" ? "alert" : "status"}>
        {message}
      </p>
    </main>
  );
}

/** An empty value: a dash on screen, "None" to assistive technology. */
export const Dash = () => (
  <span className="gv-dash">
    <span aria-hidden="true">—</span>
    <span className="sr-only">None</span>
  </span>
);

/**
 * The record's lifecycle mark, a plain square for a member without a
 * lifecycle, or an empty slot when the record has no lifecycle value.
 */
export function RecordMark({ member, record }: { member: Member; record: GroupRecord }) {
  if (!member.lifecycle) return <span className="gv-square" aria-hidden="true" />;
  const value = lifecycleValue(member, record);

  if (value === null) return <span className="gv-mark-slot" aria-hidden="true" />;

  return (
    <StatusMark value={value} values={member.lifecycle.values} hideLabel className="gv-mark" />
  );
}

/** The record's lifecycle value with its label, or a dash. */
export function LifecycleLabel({ member, record }: { member: Member; record: GroupRecord }) {
  const value = lifecycleValue(member, record);

  if (!member.lifecycle || value === null) return <Dash />;

  return <StatusMark value={value} values={member.lifecycle.values} />;
}

/** A record title that opens the record. */
export function RecordTitle({ record }: { record: GroupRecord }) {
  return (
    <button type="button" className="gv-record" onClick={() => openRecord(record)}>
      {record.title}
    </button>
  );
}

/** A record's issue count that opens its issues; nothing when it has none. */
export function IssueCount({ record }: { record: GroupRecord }) {
  if (record.issueCount === 0) return null;

  return (
    <button
      type="button"
      className="gv-issues"
      onClick={() => openIssues({ kind: "note", key: record.path })}
    >
      {record.issueCount} {record.issueCount === 1 ? "issue" : "issues"}
    </button>
  );
}

/** Link targets as titles that open them. */
export function LinkedTitles({ notes }: { notes: readonly LinkedNote[] }) {
  if (!notes.length) return <Dash />;

  return (
    <span className="gv-clamp">
      {notes.map((note, index) => (
        <span key={note.path}>
          {index > 0 && ", "}
          <button type="button" className="gv-link" onClick={() => openNote(note.path)}>
            {note.title}
          </button>
        </span>
      ))}
    </span>
  );
}

/** How many of a member's records hold each lifecycle value, in lifecycle order. */
export function LifecycleCounts({ member }: { member: Member }) {
  if (!member.lifecycle) return null;
  const { values } = member.lifecycle;
  const counts = new Map<string, number>();

  for (const record of member.records) {
    const value = lifecycleValue(member, record);

    if (value !== null && value !== undefined)
      counts.set(String(value), (counts.get(String(value)) ?? 0) + 1);
  }

  const present = orderedEnumValues(values).filter((value) => counts.has(value.name));

  if (!present.length) return null;

  return (
    <span className="gv-counts">
      {present.map((value) => (
        <span key={value.name}>
          <StatusMark value={value.name} values={values} />
          <span className="gv-num">{counts.get(value.name)}</span>
        </span>
      ))}
    </span>
  );
}

/**
 * The strip under the host's title: what the view shows, then the group's
 * facts. A `collection` strip leaves out the type count and the guide, which
 * the type Briefing's Guide block shows.
 */
export function GroupFacts({
  model,
  collection = false,
  children,
}: {
  model: GroupModel;
  collection?: boolean;
  children?: ReactNode;
}) {
  const records = [...model.records.values()];
  const withIssues = records.filter((record) => record.issueCount > 0).length;
  const latest = Math.max(0, ...records.map((record) => record.updatedAt ?? 0));
  const truncated = model.members.filter((member) => member.truncated);
  const refreshError = useContext(RefreshError);

  return (
    <div className="gv-facts">
      <span className="gv-facts-note">
        {children}
        {model.guide && !collection && (
          <>
            {children && " "}
            Guide:{" "}
            <button
              type="button"
              className="gv-link"
              onClick={() => openNote(model.guide?.path ?? "")}
            >
              {model.guide.title}
            </button>
            {model.members.length > 1
              ? `, shared by ${model.guide.sharedBy} of ${model.members.length} types.`
              : "."}
          </>
        )}
      </span>
      <span className="gv-facts-list">
        <span>
          <b>{records.length}</b> {records.length === 1 ? "record" : "records"}
        </span>
        {!collection && (
          <span>
            <b>{model.members.length}</b> {model.members.length === 1 ? "type" : "types"}
          </span>
        )}
        {withIssues ? (
          <span className="gv-bad">
            {withIssues} {withIssues === 1 ? "record" : "records"} with issues
          </span>
        ) : (
          <span className="gv-ok">✓ no issues</span>
        )}
        {latest > 0 && (
          <span>
            changed <RelativeTime value={latest} className="gv-strong" />
          </span>
        )}
      </span>
      {refreshError && (
        <span className="gv-facts-warn" role="status">
          Could not refresh: {refreshError.message}. Showing what loaded last.
        </span>
      )}
      {truncated.length > 0 && (
        <span className="gv-facts-warn" role="note">
          Showing the {RECORD_CAP} most recently changed records of each type;{" "}
          {truncated.map((member) => memberLabel(model, member, { plural: true })).join(", ")}{" "}
          {truncated.length === 1 ? "has" : "have"} more.
        </span>
      )}
    </div>
  );
}

/** A member's label as a button opening its collection. */
export function MemberHeading({ model, member }: { model: GroupModel; member: Member }) {
  return (
    <button type="button" className="gv-member" onClick={() => openCollection(member.name)}>
      {memberLabel(model, member, { plural: true })}
    </button>
  );
}
