// What the All notes Briefing reads (SPEC-0117 US5), decoded. Pure.
//
// - parseIssueGroups(json): validation issues by check, code, and variant,
//   from `POST /api/v1/validation/groups`.
// - motionTypes(members, docs): each note type whose lifecycle has active
//   values, with those values.
// - activeRecordsQuery(types): one GraphQL query for the newest records of
//   each type in an active value, capped per type.
// - parseRecentNotes(json): the newest notes from
//   `GET /api/v1/ontology/types/__all__?limit=N`, untyped notes included.
import type { EnumValueDoc, TypeDoc } from "@rhizome/kit";

import type { MemberFigures } from "./aggregate.ts";
import { isJsonObject, isNumber, isString, type JsonValue, type RecordRef } from "./api.ts";

/** Recent changes listed at All notes. */
export const RECENT_NOTES = 12;

/** In-motion records listed per type; one more is read to know there are more. */
export const MOTION_PER_TYPE = 5;

export type IssueGroup = {
  key: string;
  check: string;
  code: string | null;
  /** The variant's label, else the code, else the check. */
  label: string;
  issueCount: number;
  affectedFileCount: number;
  /** On a code's roll-up row, how many more variants it holds. */
  otherVariants: number;
};

const objects = (value: JsonValue | undefined) =>
  Array.isArray(value) ? value.filter(isJsonObject) : [];

const text = (value: JsonValue | undefined) => (isString(value) ? value : null);

const count = (value: JsonValue | undefined) => (isNumber(value) ? value : 0);

/** Issue groups in the API's order: by code, then by issue count within a code. */
export function parseIssueGroups(json: JsonValue): IssueGroup[] {
  const groups = isJsonObject(json) ? objects(json.groups) : [];

  return groups.map((group) => {
    const check = text(group.check) ?? "";
    const code = text(group.code);
    const variant = isJsonObject(group.variant) ? group.variant : null;
    const variantKey = text(variant?.key);

    return {
      key: [check, code ?? "", variantKey ?? ""].join("\u0000"),
      check,
      code,
      label: text(variant?.label) ?? code ?? check,
      issueCount: count(group.issueCount),
      affectedFileCount: count(group.affectedFileCount),
      otherVariants: count(group.otherVariants),
    };
  });
}

export type MotionType = {
  type: string;
  field: string;
  /** The lifecycle's values, for marks; `active` names the ones in motion. */
  values: readonly EnumValueDoc[];
  active: readonly string[];
};

/** Note types whose lifecycle declares active values, most records first. */
export function motionTypes(
  members: MemberFigures,
  docs: Readonly<Record<string, TypeDoc>>,
): MotionType[] {
  return [...members.types.values()]
    .sort((a, b) => b.count - a.count || a.name.localeCompare(b.name))
    .flatMap((figures) => {
      const field = figures.lifecycle?.field;
      const declared = docs[figures.name]?.fields.find((entry) => entry.name === field);
      const values = declared?.enum?.values ?? [];
      const active = values.flatMap((value) => (value.stage === "active" ? [value.name] : []));

      return field && figures.count > 0 && active.length
        ? [{ type: figures.name, field, values, active }]
        : [];
    });
}

const lowerFirst = (name: string) => name.charAt(0).toLowerCase() + name.slice(1);

/**
 * Each type's newest records whose lifecycle value is active, aliased by type
 * name, with the selection `parseRecords` decodes.
 */
export function activeRecordsQuery(types: readonly MotionType[]) {
  const selections = types.map(
    ({ type, field, active }) =>
      `  ${type}: ${lowerFirst(type)}(first: ${MOTION_PER_TYPE + 1}, filters: [{ field: ${JSON.stringify(field)}, op: in, values: ${JSON.stringify(active)} }], sort: [{ field: "updatedAt", direction: desc }]) {\n` +
      `    ref { notePath kind fragment nodeId typeName structuralFingerprint: structural }\n` +
      `    path title updatedAt issueCount ${field}\n  }`,
  );

  return types.length ? `query ActiveRecords {\n${selections.join("\n")}\n}` : "";
}

export type RecentNote = {
  ref: RecordRef;
  path: string;
  title: string;
  /** The note's type; null for an untyped note. */
  type: string | null;
  /** Epoch milliseconds. */
  updatedAt: number | null;
  /** The folder holding the note, without a trailing slash; empty at the top level. */
  folder: string;
};

const folderOf = (path: string) => path.split("/").slice(0, -1).join("/");

/** Internal fallback types name untyped notes. */
const publicType = (type: string | null) => (type && !type.startsWith("_") ? type : null);

/** The note list's items, newest first as the server sends them. */
export function parseRecentNotes(json: JsonValue): RecentNote[] {
  const notes = isJsonObject(json) ? objects(json.notes) : [];

  return notes.flatMap((note) => {
    const path = text(note.path);

    if (path === null) return [];
    const ref = isJsonObject(note.ref) ? note.ref : {};

    const parsed: RecordRef = {
      notePath: text(ref.notePath) ?? path,
      kind: text(ref.kind) ?? "NOTE",
    };

    for (const name of ["fragment", "nodeId", "typeName"] as const) {
      const value = text(ref[name]);

      if (value) parsed[name] = value;
    }

    return [
      {
        ref: parsed,
        path,
        title: text(note.title) ?? path,
        type: publicType(text(note.resolvedType)),
        updatedAt: count(note.updatedAt) > 0 ? count(note.updatedAt) * 1000 : null,
        folder: folderOf(parsed.notePath),
      },
    ];
  });
}
