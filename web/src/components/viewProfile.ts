import type { TypeProfile, ViewFieldCapability, ViewTableRow } from "../api/types";
import { isFiniteNumber } from "../api/parse";
import { enumLabel } from "../lib/labels";
import {
  capabilityLabel,
  cellValues,
  fieldValue,
  rawFieldValue,
} from "./ConfiguredTableCellHelpers";
import { relationDisplayValue, relationValuesForField } from "./ConfiguredTableCellReadOnly";
import { fieldCapability } from "./ConfiguredViewModel";

/** Reads a type profile (SPEC-0112) against a view's rows and capabilities. */

type ViewTableRelationValue = NonNullable<ViewTableRow["relationValues"]>[string][number];

/** The server's stale rule: an active lifecycle value unchanged for this many days. */
export const STALE_DAYS = 30;

const STALE_AFTER_MS = STALE_DAYS * 86_400_000;

function rowUpdatedMillis(row: ViewTableRow) {
  if (!row.updatedAt) return null;

  // The API sends seconds; older payloads sent milliseconds.
  return row.updatedAt < 1e12 ? row.updatedAt * 1000 : row.updatedAt;
}

export type EnumTag = {
  field: string;
  capability: ViewFieldCapability;
  value: string;
  label: string;
  tone?: string;
};

function enumTag(row: ViewTableRow, capability: ViewFieldCapability | undefined): EnumTag | null {
  if (!capability) return null;
  const value = cellValues(rawFieldValue(row, capability.key))[0];

  if (!value) return null;
  const option = capability.enumValues?.find((item) => item.value === value);

  return {
    field: capability.key,
    capability,
    value,
    label: enumLabel(value, option?.label),
    tone: option?.tone,
  };
}

/** The row's lifecycle value, with its stage when the enum declares or infers one. */
export function lifecycleTag(
  row: ViewTableRow,
  profile: TypeProfile | undefined,
  capabilities: ViewFieldCapability[],
) {
  return enumTag(row, fieldCapability(capabilities, profile?.lifecycleField));
}

/** A record holding an active-stage value that has not changed for STALE_DAYS, as the server counts. */
export function isStaleRow(
  row: ViewTableRow,
  profile: TypeProfile | undefined,
  capabilities: ViewFieldCapability[],
  now = Date.now(),
) {
  const capability = fieldCapability(capabilities, profile?.lifecycleField);
  const tag = lifecycleTag(row, profile, capabilities);
  const stage = capability?.enumValues?.find((item) => item.value === tag?.value)?.stage;
  const updated = rowUpdatedMillis(row);

  return stage === "active" && updated !== null && now - updated > STALE_AFTER_MS;
}

export type RelationFact = {
  field: string;
  label: string;
  values: ViewTableRelationValue[];
};

export type ReverseFact = RelationFact & { count: number };

/** What a board card or record brief shows for one row, in profile order. */
export type RecordFacts = {
  summary: string;
  keyTexts: Array<{ field: string; label: string; value: string }>;
  orderedTags: EnumTag[];
  relations: RelationFact[];
  reverse: ReverseFact[];
  people: RelationFact[];
};

export function recordFacts(
  row: ViewTableRow,
  profile: TypeProfile,
  capabilities: ViewFieldCapability[],
): RecordFacts {
  const resolve = (field: string) => fieldCapability(capabilities, field);
  const keyOf = (field: string) => resolve(field)?.key ?? field;
  const labelOf = (field: string) => capabilityLabel(resolve(field)) || field;

  const relationFact = (field: string): RelationFact | null => {
    const values = relationValues(row, keyOf(field), resolve(field));

    return values.length > 0 ? { field: keyOf(field), label: labelOf(field), values } : null;
  };

  return {
    summary: profile.summaryField ? fieldValue(row, keyOf(profile.summaryField)) : "",
    keyTexts: (profile.keyTextFields ?? []).flatMap((field) => {
      const value = fieldValue(row, keyOf(field));

      return value ? [{ field: keyOf(field), label: labelOf(field), value }] : [];
    }),
    orderedTags: (profile.orderedFields ?? []).flatMap((field) => {
      const tag = enumTag(row, resolve(field));

      return tag ? [tag] : [];
    }),
    relations: (profile.relationFields ?? []).flatMap((field) => relationFact(field) ?? []),
    people: (profile.peopleFields ?? []).flatMap((field) => relationFact(field) ?? []),
    reverse: (profile.reverseFields ?? []).flatMap((field) => {
      const values = relationValues(row, keyOf(field), resolve(field));
      // The listed targets stop at the server's cap, so the projected count is the total.
      const total = rawFieldValue(row, keyOf(field));
      const count = isFiniteNumber(total) ? Math.max(total, values.length) : values.length;

      return count > 0 ? [{ field: keyOf(field), label: labelOf(field), values, count }] : [];
    }),
  };
}

/** Titled targets for a link field, falling back to its raw values. */
function relationValues(
  row: ViewTableRow,
  field: string,
  capability: ViewFieldCapability | undefined,
): ViewTableRelationValue[] {
  const resolved = relationValuesForField(row, field, capability);

  if (resolved.length > 0) return resolved;
  const raw = rawFieldValue(row, field);

  // Numbers are projected counts, not targets.
  if (isFiniteNumber(raw)) return [];

  return cellValues(raw)
    .filter(Boolean)
    .map((value) => ({ value, title: relationDisplayValue(value) }));
}

export function relationTitle(value: ViewTableRelationValue) {
  return value.title || relationDisplayValue(value.value);
}

/** "Drew Colthorp" → "DC". */
export function initials(name: string) {
  return name
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((word) => word[0]?.toUpperCase() ?? "")
    .join("");
}
