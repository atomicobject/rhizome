// What a collection's records say about its fields (SPEC-0112 Briefing): how
// they spread across each enum field, how the primary date falls by month,
// and how each people, relation, and reverse field is filled. Pure; `now` is
// passed in. Each reads a collection's single member, whose profile names the
// fields, and touches every loaded record once per field.
//
// - distributions(member): counts per value of the lifecycle, ordered, and
//   category fields.
// - dateMonths(member, now, span): records per month of the primary date.
// - fieldConnections(member): fill counts and most common targets per field.
import { orderedEnumValues, type EnumValueDoc } from "@rhizome/kit";

import {
  fieldValue,
  isEmptyValue,
  linkTargets,
  reverseField,
  valueText,
  type FieldValue,
  type GroupRecord,
  type LinkedNote,
  type Member,
  type MemberField,
} from "./model.ts";

export type FieldRole = "lifecycle" | "ordered" | "category";

export type Distribution = {
  field: string;
  role: FieldRole;
  /** Values some record holds, in enum order, with their record counts. */
  values: readonly { value: EnumValueDoc; count: number }[];
  /** Every value of the field's enum, for placing a value among them. */
  options: readonly EnumValueDoc[];
  /** Records whose type declares the field and leave it empty. */
  empty: number;
  /** Values records hold that the enum does not list, such as a hand-edited one. */
  other: number;
  /** Records whose type declares the field. */
  total: number;
};

/** Enum declarations of `name`, one per distinct enum across implementors. */
const enumDeclarations = (member: Member, name: string) =>
  member.fields.filter((field) => field.name === name && field.kind === "enum");

/** The record's value of a field, read from whichever declaration its type has. */
function valueOf(record: GroupRecord, declarations: readonly MemberField[]) {
  for (const field of declarations) {
    if (field.declaredBy.includes(record.type))
      return { declared: true, value: fieldValue(record, field) };
  }

  return { declared: false, value: undefined };
}

/** The enum value names a record holds: a list enum's values, or a single value. */
const valueNames = (value: FieldValue | undefined): readonly string[] =>
  Array.isArray(value) ? value : isEmptyValue(value) ? [] : [valueText(value)];

function distribution(member: Member, field: string, role: FieldRole): Distribution | null {
  const declarations = enumDeclarations(member, field);

  if (!declarations.length) return null;
  const values = new Map<string, EnumValueDoc>();

  for (const declaration of declarations) {
    for (const value of declaration.values)
      if (!values.has(value.name)) values.set(value.name, value);
  }

  const counts = new Map<string, number>();
  let empty = 0;
  let total = 0;

  for (const record of member.records) {
    const { declared, value } = valueOf(record, declarations);

    if (!declared) continue;
    total += 1;
    const names = valueNames(value);

    if (!names.length) empty += 1;

    for (const name of names) counts.set(name, (counts.get(name) ?? 0) + 1);
  }

  const options = orderedEnumValues([...values.values()]);
  let other = 0;

  for (const [name, count] of counts) if (!values.has(name)) other += count;

  return {
    field,
    role,
    options,
    values: options.flatMap((value) => {
      const count = counts.get(value.name) ?? 0;

      return count ? [{ value, count }] : [];
    }),
    empty,
    other,
    total,
  };
}

/** The lifecycle, then the profile's ordered fields, then its category fields. */
export function distributions(member: Member): Distribution[] {
  const profile = member.profile;

  if (!profile) return [];

  const spread = (role: FieldRole) => (name: string) => distribution(member, name, role) ?? [];

  return [
    ...(profile.lifecycleField ? [profile.lifecycleField] : []).flatMap(spread("lifecycle")),
    ...profile.orderedFields.flatMap(spread("ordered")),
    ...profile.categoryFields.flatMap(spread("category")),
  ];
}

export type DateMonths = {
  field: string;
  /** Calendar months as `YYYY-MM`, oldest first, each with its record count. */
  months: readonly { month: string; count: number }[];
  /** The earliest and latest dates any record holds. */
  first: string;
  last: string;
  /** Dated records before the first charted month. */
  earlier: number;
  /** Records whose type declares the field and leave it empty. */
  undated: number;
};

/** Months the chart spans at most. */
export const DATE_MONTHS = 36;

const DATE = /^(\d{4})-(\d{2})-\d{2}/;

const monthKey = (year: number, month: number) => `${year}-${String(month + 1).padStart(2, "0")}`;

const monthIndex = (key: string) => Number(key.slice(0, 4)) * 12 + Number(key.slice(5, 7)) - 1;

/**
 * Records per calendar month of the primary date. The chart ends at `now`'s
 * month, or the latest date's when that is later, and starts at the earliest
 * dated month, at most `span` months before its end. Null without a primary
 * date or any dated record.
 */
export function dateMonths(member: Member, now: number, span = DATE_MONTHS): DateMonths | null {
  const name = member.profile?.primaryDateField;
  const field = name ? member.fields.find((candidate) => candidate.name === name) : undefined;

  if (!name || !field) return null;
  const dates: string[] = [];
  let undated = 0;

  for (const record of member.records) {
    if (!field.declaredBy.includes(record.type)) continue;
    const match = DATE.exec(valueText(fieldValue(record, field)));

    if (match) dates.push(match[0]);
    else undated += 1;
  }

  if (!dates.length) return null;
  dates.sort();
  const today = new Date(now);
  const first = dates[0];
  const last = dates[dates.length - 1];
  const thisMonth = monthIndex(monthKey(today.getFullYear(), today.getMonth()));
  const end = Math.max(thisMonth, monthIndex(last));
  const start = Math.max(end - span + 1, monthIndex(first));

  const counts = new Map<number, number>();
  let earlier = 0;

  for (const date of dates) {
    const index = monthIndex(date);

    if (index < start) earlier += 1;
    else counts.set(index, (counts.get(index) ?? 0) + 1);
  }

  const months = Array.from({ length: end - start + 1 }, (_, offset) => {
    const index = start + offset;

    return { month: monthKey(Math.floor(index / 12), index % 12), count: counts.get(index) ?? 0 };
  });

  return { field: name, months, first, last, earlier, undated };
}

export type FieldConnection = {
  field: string;
  /** Forward links the records hold, or reverse links other records hold to them. */
  direction: "forward" | "reverse";
  /** A people field: it links to the core identity type. */
  people: boolean;
  /** The declared target type, or the type of the records linking in. */
  targetType: string;
  /** Records with at least one target. */
  filled: number;
  /** Records whose type declares the field. */
  total: number;
  /** Distinct targets across the records. */
  distinct: number;
  /** The targets most records share, most first, then by title; at most `TOP_TARGETS`. */
  top: readonly { note: LinkedNote; count: number }[];
  /**
   * Some record has more reverse links than its sample holds, so `distinct`
   * and the top counts are lower bounds; `filled` counts every record.
   */
  sampled: boolean;
};

/** Most common targets listed per field. */
export const TOP_TARGETS = 4;

/** A record's targets for one field, and how many it has when they were sampled. */
type FieldTargets = { targets: readonly LinkedNote[]; count: number };

function connection(
  field: string,
  base: Pick<FieldConnection, "direction" | "people" | "targetType">,
  records: readonly GroupRecord[],
  read: (record: GroupRecord) => FieldTargets | null,
): FieldConnection {
  const counts = new Map<string, { note: LinkedNote; count: number }>();
  let filled = 0;
  let total = 0;
  let sampled = false;

  for (const record of records) {
    const found = read(record);

    if (found === null) continue;
    total += 1;

    if (found.count > 0) filled += 1;
    sampled ||= found.count > found.targets.length;

    for (const note of new Map(found.targets.map((entry) => [entry.path, entry])).values()) {
      const entry = counts.get(note.path) ?? { note, count: 0 };
      entry.count += 1;
      counts.set(note.path, entry);
    }
  }

  const top = [...counts.values()].sort(
    (a, b) => b.count - a.count || a.note.title.localeCompare(b.note.title),
  );

  return {
    field,
    ...base,
    filled,
    total,
    distinct: counts.size,
    top: top.slice(0, TOP_TARGETS),
    sampled,
  };
}

/** People fields, then relation fields, then reverse fields, each in profile order. */
export function fieldConnections(member: Member): FieldConnection[] {
  const profile = member.profile;

  if (!profile) return [];

  const forward = (name: string, people: boolean) => {
    const links = member.links.filter((link) => link.field === name);

    if (!links.length) return [];
    const declaredBy = links.flatMap((link) => link.declaredBy);

    return [
      connection(
        name,
        { direction: "forward", people, targetType: links[0].targetType },
        member.records,
        (record) => {
          if (!declaredBy.includes(record.type)) return null;
          const targets = linkTargets(record, { field: name, declaredBy });

          return { targets, count: targets.length };
        },
      ),
    ];
  };

  const reverse = (name: string) => {
    const field = reverseField(member, name);

    if (!field) return [];

    return [
      connection(
        name,
        { direction: "reverse", people: false, targetType: field.targetType },
        member.records,
        (record) =>
          field.declaredBy.includes(record.type)
            ? {
                targets: record.reverse.get(name) ?? [],
                count: record.reverseCounts.get(name) ?? 0,
              }
            : null,
      ),
    ];
  };

  return [
    ...profile.peopleFields.flatMap((name) => forward(name, true)),
    ...profile.relationFields.flatMap((name) => forward(name, false)),
    ...profile.reverseFields.flatMap(reverse),
  ];
}
