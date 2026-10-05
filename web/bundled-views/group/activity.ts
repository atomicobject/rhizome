// What a group's records say about its work: what needs attention, what is in
// motion, what changed recently, and which notes outside the group touch it
// (SPEC-0111 Briefing). Pure; `now` is passed in.
//
// - needsAttention(model, graph, now): signals with counts and records.
// - inMotion(model): active-stage records by member, newest first.
// - recentChanges(model, limit): changes by day and minute, bursts collapsed.
// - boundary(model): notes outside the group linked with its records.
import type { EnumValueDoc } from "@rhizome/kit";

import { linkedMembers, type LinkGraph } from "./graph.ts";
import {
  fieldValue,
  fileTitle,
  isEmptyValue,
  isOutside,
  keyText,
  lifecycleValue,
  recordField,
  reverseField,
  type FieldValue,
  type GroupModel,
  type GroupRecord,
  type LinkedNote,
  type Member,
} from "./model.ts";

export const STALE_DAYS = 30;

/** Records changed in the same minute collapse into one line from this many. */
export const BURST_SIZE = 4;

const DAY = 24 * 60 * 60 * 1000;

const ATTENTION_TONES = ["danger", "risk", "warning"] as const;

export type AttentionTone = (typeof ATTENTION_TONES)[number];

const isAttentionTone = (tone: string | undefined): tone is AttentionTone =>
  ATTENTION_TONES.some((candidate) => candidate === tone);

export type MemberRecords = { member: string; records: readonly GroupRecord[] };

export type AttentionSignal =
  /** Records with validation issues. */
  | { kind: "issues"; records: readonly GroupRecord[] }
  /** Records holding a warning, risk, or danger value of one enum field, across members. */
  | {
      kind: "tone";
      field: string;
      value: EnumValueDoc;
      tone: AttentionTone;
      byMember: readonly MemberRecords[];
      records: readonly GroupRecord[];
    }
  /** Records of one member in an active-stage lifecycle value, unchanged for `STALE_DAYS`; oldest first. */
  | { kind: "stale"; member: string; records: readonly GroupRecord[] }
  /** A profile gap field left empty on some of a member's records that declare it. */
  | {
      kind: "gap";
      member: string;
      field: string;
      policyReason: string | null;
      records: readonly GroupRecord[];
      /** Records of the member whose type declares the field. */
      total: number;
    }
  /** Records of linked members that link with nothing else in the group. */
  | { kind: "unlinked"; byMember: readonly MemberRecords[]; records: readonly GroupRecord[] }
  /** A profile reverse field more than half a member's records fill, empty on the rest. */
  | {
      kind: "reverse";
      member: string;
      field: string;
      /** The type the linking records belong to. */
      targetType: string;
      records: readonly GroupRecord[];
      /** Records of the member whose type declares the field. */
      total: number;
    };

function byMember(model: GroupModel, records: readonly GroupRecord[]): MemberRecords[] {
  return model.members.flatMap((member) => {
    const own = records.filter((record) => record.member === member.name);

    return own.length ? [{ member: member.name, records: own }] : [];
  });
}

const isActive = (member: Member, record: GroupRecord) => {
  const value = lifecycleValue(member, record);

  return member.lifecycle?.values.find((entry) => entry.name === value)?.stage === "active";
};

/** A single enum value, or one of a list enum's values. */
const holds = (value: FieldValue | undefined, name: string) =>
  Array.isArray(value) ? value.includes(name) : value === name;

function toneSignals(model: GroupModel): AttentionSignal[] {
  const found = new Map<
    string,
    { field: string; value: EnumValueDoc; tone: AttentionTone; records: GroupRecord[] }
  >();

  for (const member of model.members) {
    for (const field of member.fields) {
      for (const value of field.values) {
        const tone = value.tone;

        if (!isAttentionTone(tone)) continue;

        const holding = member.records.filter((record) =>
          holds(fieldValue(record, field), value.name),
        );

        if (!holding.length) continue;
        const key = `${field.name}\u0000${value.name}`;
        const signal = found.get(key) ?? { field: field.name, value, tone, records: [] };
        signal.records.push(...holding);
        found.set(key, signal);
      }
    }
  }

  const severity = (tone: AttentionTone) => (tone === "warning" ? 1 : 0);

  return [...found.values()]
    .sort((a, b) => severity(a.tone) - severity(b.tone) || b.records.length - a.records.length)
    .map((signal) => ({ kind: "tone", ...signal, byMember: byMember(model, signal.records) }));
}

function staleSignals(model: GroupModel, now: number): AttentionSignal[] {
  return model.members.flatMap((member): AttentionSignal[] => {
    const records = member.records.filter(
      (record) =>
        isActive(member, record) &&
        record.updatedAt !== null &&
        now - record.updatedAt > STALE_DAYS * DAY,
    );

    records.sort((a, b) => (a.updatedAt ?? 0) - (b.updatedAt ?? 0));

    return records.length ? [{ kind: "stale", member: member.name, records }] : [];
  });
}

// Implementors can declare one name differently; each declaration counts its
// own records, and the name reports once.
function gapSignals(member: Member): AttentionSignal[] {
  const byName = new Map<
    string,
    { reasons: Set<string | null>; records: GroupRecord[]; total: number }
  >();

  for (const field of member.fields) {
    if (!member.gapFields.includes(field.name)) continue;
    const declaring = member.records.filter((record) => field.declaredBy.includes(record.type));
    const entry = byName.get(field.name) ?? { reasons: new Set(), records: [], total: 0 };
    const empty = declaring.filter((record) => isEmptyValue(recordField(record, field)));

    // A reason shows only when it applies to every listed record.
    if (empty.length) entry.reasons.add(field.policyReason);
    entry.records.push(...empty);
    entry.total += declaring.length;
    byName.set(field.name, entry);
  }

  return [...byName].flatMap(([field, entry]): AttentionSignal[] =>
    entry.records.length
      ? [
          {
            kind: "gap",
            member: member.name,
            field,
            policyReason: entry.reasons.size === 1 ? [...entry.reasons][0] : null,
            records: entry.records,
            total: entry.total,
          },
        ]
      : [],
  );
}

/**
 * Reverse fields that more than half of a member's records fill but some do
 * not, each with the records nothing links in to. Only a model read with
 * reverse fields (a collection's) has them to count.
 */
export function reverseGaps(member: Member): AttentionSignal[] {
  return (member.profile?.reverseFields ?? []).flatMap((name): AttentionSignal[] => {
    const field = reverseField(member, name);

    if (!field) return [];
    const declaring = member.records.filter((record) => field.declaredBy.includes(record.type));
    const empty = declaring.filter((record) => !record.reverseCounts.get(name));
    const filled = declaring.length - empty.length;

    return empty.length && filled * 2 > declaring.length
      ? [
          {
            kind: "reverse",
            member: member.name,
            field: name,
            targetType: field.targetType,
            records: empty,
            total: declaring.length,
          },
        ]
      : [];
  });
}

/** Keys of records with a parent or children through their member's PARENT-role field. */
function inHierarchy(model: GroupModel) {
  const keys = new Set<string>();

  for (const member of model.members) {
    if (member.parentField === null) continue;

    for (const record of member.records) {
      for (const parent of record.links.get(member.parentField) ?? []) {
        if (parent.key === record.key) continue;
        keys.add(record.key);
        keys.add(parent.key);
      }
    }
  }

  return keys;
}

/**
 * What needs attention, in this order: validation issues; warning, risk, and
 * danger values (risk and danger first); stale active-stage records; empty
 * gap fields from each member's profile;
 * and, when the group has links, records linked to nothing else in it, a
 * record with a parent or children counting as linked.
 */
export function needsAttention(
  model: GroupModel,
  graph: LinkGraph,
  now: number,
): AttentionSignal[] {
  const records = [...model.records.values()];
  const issues = records.filter((record) => record.issueCount > 0);
  const signals: AttentionSignal[] = issues.length ? [{ kind: "issues", records: issues }] : [];

  signals.push(...toneSignals(model), ...staleSignals(model, now));

  for (const member of model.members) signals.push(...gapSignals(member));

  if (graph.linked) {
    // Parent links join records of one member, so the link graph leaves them out.
    const tree = inHierarchy(model);

    const unlinked = records.filter(
      (record) =>
        linkedMembers(graph, record.member).size > 0 &&
        !graph.adjacent.has(record.key) &&
        !tree.has(record.key),
    );

    if (unlinked.length)
      signals.push({ kind: "unlinked", byMember: byMember(model, unlinked), records: unlinked });
  }

  return signals;
}

export type MotionEntry = {
  record: GroupRecord;
  /** The first filled key text field, labeled, else the summary unlabeled. */
  note: { label: string | null; text: string } | null;
};

export type MotionGroup = { member: string; entries: readonly MotionEntry[] };

export type InMotion = {
  groups: readonly MotionGroup[];
  /** Members with records whose lifecycle has no `active` stage. */
  withoutActiveStage: readonly string[];
  /** Members with records but no lifecycle. */
  withoutLifecycle: readonly string[];
};

function motionNote(member: Member, record: GroupRecord): MotionEntry["note"] {
  const text = keyText(member, record);

  if (text) return text;

  return record.summary ? { label: null, text: record.summary } : null;
}

/** Records whose lifecycle value is in the `active` stage, by member and newest first. */
export function inMotion(model: GroupModel): InMotion {
  const populated = model.members.filter((member) => member.records.length > 0);

  return {
    groups: populated.flatMap((member) => {
      const moving = member.records.filter((record) => isActive(member, record));

      moving.sort((a, b) => (b.updatedAt ?? 0) - (a.updatedAt ?? 0));

      return moving.length
        ? [
            {
              member: member.name,
              entries: moving.map((record) => ({ record, note: motionNote(member, record) })),
            },
          ]
        : [];
    }),
    withoutActiveStage: populated.flatMap((member) =>
      member.lifecycle && !member.lifecycle.values.some((value) => value.stage === "active")
        ? [member.name]
        : [],
    ),
    withoutLifecycle: populated.flatMap((member) => (member.lifecycle ? [] : [member.name])),
  };
}

export type ChangeMoment = {
  /** The newest change time in the minute, in epoch milliseconds. */
  time: number;
  records: readonly GroupRecord[];
  /** `BURST_SIZE` or more records changed in this minute. */
  burst: boolean;
  byMember: readonly { member: string; count: number }[];
  /** The records by concrete type, in first-seen order, for a member that is an interface. */
  byType: readonly { type: string; count: number }[];
};

export type ChangeDay = {
  /** The local calendar day, `YYYY-MM-DD`. */
  day: string;
  moments: readonly ChangeMoment[];
};

const MINUTE = 60_000;

function localDay(time: number) {
  const date = new Date(time);
  const pad = (part: number) => String(part).padStart(2, "0");

  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
}

function countByType(records: readonly GroupRecord[]) {
  const counts = new Map<string, number>();

  for (const record of records) counts.set(record.type, (counts.get(record.type) ?? 0) + 1);

  return [...counts].map(([type, count]) => ({ type, count }));
}

/**
 * Changed records newest first, by local day and minute. A minute with
 * `BURST_SIZE` or more changes is one burst line. `limit` counts lines: a
 * burst is one, other moments one per record.
 */
export function recentChanges(model: GroupModel, limit = 20): ChangeDay[] {
  const changed = [...model.records.values()].filter((record) => record.updatedAt !== null);

  changed.sort((a, b) => (b.updatedAt ?? 0) - (a.updatedAt ?? 0));

  const days: { day: string; moments: ChangeMoment[] }[] = [];
  const minutes: { at: number; records: GroupRecord[] }[] = [];

  for (const record of changed) {
    const at = Math.floor((record.updatedAt ?? 0) / MINUTE);
    const minute = minutes.at(-1);

    if (minute?.at === at) minute.records.push(record);
    else minutes.push({ at, records: [record] });
  }

  let budget = limit;

  for (const { records } of minutes) {
    if (budget <= 0) break;
    const burst = records.length >= BURST_SIZE;
    const time = records[0]?.updatedAt ?? 0;
    const day = localDay(time);
    const shown = burst ? records : records.slice(0, budget);

    budget -= burst ? 1 : shown.length;

    const moment: ChangeMoment = {
      time,
      records: shown,
      burst,
      byMember: byMember(model, shown).map((entry) => ({
        member: entry.member,
        count: entry.records.length,
      })),
      byType: countByType(shown),
    };

    const last = days.at(-1);

    if (last?.day === day) last.moments.push(moment);
    else days.push({ day, moments: [moment] });
  }

  return days;
}

export type BoundaryNote = LinkedNote & {
  /** Keys of the group records it links to or from. */
  touches: readonly string[];
};

export type Boundary = {
  /** Most connected first. */
  notes: readonly BoundaryNote[];
  /** Counts by resolved type, untyped (`type: null`) last. */
  byType: readonly { type: string | null; count: number }[];
  /** Some record has more neighbors than were read, so the counts are a lower bound. */
  incomplete: boolean;
};

/** Internal fallback types name untyped notes. */
const publicType = (type: string | null) => (type && !type.startsWith("_") ? type : null);

/**
 * Notes outside the group that link to or from its records, excluding the
 * guide: the records' neighbors and, unless `links` is false, their typed link
 * targets, which covers records read without neighbors. A note counts once
 * however many links join it, and a section counts as the note holding it:
 * listed under the note's own title and type when the note itself links too,
 * else under its file name and no type.
 */
export function boundary(model: GroupModel, { links = true }: { links?: boolean } = {}): Boundary {
  const notes = new Map<string, { note: LinkedNote; touches: Set<string> }>();

  for (const record of model.records.values()) {
    const targets = links ? [...record.links.values()].flat() : [];

    for (const note of [...record.neighbors, ...targets]) {
      const path = note.path.split("#", 1)[0];

      if (!isOutside(model, note) || path === model.guide?.path) continue;
      const holder = { key: path, path, title: fileTitle(path), type: null };
      const entry = notes.get(path) ?? { note: holder, touches: new Set<string>() };

      if (note.path === path) entry.note = note;
      entry.touches.add(record.key);
      notes.set(path, entry);
    }
  }

  const list = [...notes.values()].map(({ note, touches }) => ({
    ...note,
    type: publicType(note.type),
    touches: [...touches],
  }));

  list.sort((a, b) => b.touches.length - a.touches.length || a.title.localeCompare(b.title));

  const counts = new Map<string | null, number>();

  for (const note of list) counts.set(note.type, (counts.get(note.type) ?? 0) + 1);

  const byType = [...counts].map(([type, count]) => ({ type, count }));

  byType.sort((a, b) => Number(a.type === null) - Number(b.type === null) || b.count - a.count);

  const incomplete = [...model.records.values()].some((record) => record.neighborsTruncated);

  return { notes: list, byType, incomplete };
}
