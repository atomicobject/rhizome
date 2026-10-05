import { publicTypeName } from "../lib/typeNames";
import type {
  NodeFieldCapability,
  OntologyEditFieldValue,
  OntologyEditOp,
  OntologyEditSessionResponse,
  ViewFieldCapability,
  ViewTableRow,
  WorkspaceFieldNode,
} from "../api/types";
import { type JsonValue, isBoolean, isJsonObject } from "../api/parse";
import {
  changeSummary,
  editPathForRef,
  stagedFieldValue,
  type StagedTarget,
} from "../staging/stagedState";
import { humanizeName } from "../lib/labels";

export type EditCandidate = NonNullable<
  NonNullable<ViewFieldCapability["edit"]>["candidates"]
>[number];

export type CellEditValue = string | string[];

export const EMPTY_EDIT_CANDIDATES: EditCandidate[] = [];

export function fieldValue(row: ViewTableRow, field: string) {
  switch (field) {
    case "title":
      return row.title || "Untitled";
    case "path":
      return row.path || row.ref.notePath;
    case "resolvedType":
      return publicTypeName(row.resolvedType);
    case "updatedAt":
      return formatTimestamp(row.updatedAt);
    case "hasIssues":
      return row.hasIssues ? "Issues" : "";
    case "tags":
      return row.tags?.join(", ") ?? "";
    default:
      return stringifyCell(nestedFieldValue(row.fields, field));
  }
}

export function rawFieldValue(row: ViewTableRow, field: string) {
  switch (field) {
    case "title":
      return row.title;
    case "path":
      return row.path || row.ref.notePath;
    case "resolvedType":
      return row.resolvedType;
    case "updatedAt":
      return row.updatedAt;
    case "hasIssues":
      return row.hasIssues;
    case "tags":
      return row.tags;
    default:
      return nestedFieldValue(row.fields, field);
  }
}

export function scalarEditKind(kind: string) {
  return (
    kind === "scalar" ||
    kind === "text" ||
    kind === "number" ||
    kind === "date" ||
    kind === "datetime"
  );
}

export function listEditForCell(
  value: JsonValue | undefined,
  capability: ViewFieldCapability | undefined,
  edit: NonNullable<ViewFieldCapability["edit"]>,
) {
  if (edit.list !== undefined) return edit.list;

  if (Array.isArray(value)) return true;

  return [capability?.valueKind, edit.valueKind, edit.inputMode, edit.operation].some((kind) =>
    kind?.toLowerCase().includes("list"),
  );
}

export function workspaceFieldNodeForTableCell(
  row: ViewTableRow,
  capability: ViewFieldCapability | undefined,
  edit: NonNullable<ViewFieldCapability["edit"]>,
  value: JsonValue | undefined,
  list: boolean,
): WorkspaceFieldNode {
  const valueKind = editorValueKind(capability, edit);
  const enumValues = edit.options || capability?.enumValues?.map((option) => option.value) || [];
  const typeName = capability?.valueKind || edit.valueKind || valueKind;

  const fieldCapability: NodeFieldCapability = {
    ownerRef: row.ref,
    ownerType: row.resolvedType || row.ref.typeName || "",
    typeName,
    valueKind,
    list,
    required: capability?.required ?? false,
    enumValues,
    targetType: edit.targetType,
    sourceKind: capability?.sourceKind,
    valueOrigin: "authored",
    identifier: capability?.semanticRole === "identifier",
    preferredIdentifier: false,
    displayImportance: capability?.importance ?? "NORMAL",
    writeOperation: edit.operation,
  };

  return {
    id: `field:${encodeURIComponent(editPathForRow(row))}:${encodeURIComponent(row.ref.nodeId || "note")}:${encodeURIComponent(edit.field)}`,
    kind: "field",
    ref: row.ref,
    notePath: editPathForRow(row),
    status: {
      dirty: false,
      validation: { issueCount: 0 },
      freshness: {},
      session: {},
      hasWarnings: false,
    },
    capabilities: {
      canEdit: true,
      canEditFields: true,
      canEditCollections: false,
      canNavigateChildren: false,
      canSubscribe: false,
    },
    field: {
      name: `Edit ${capabilityLabel(capability)}`,
      valueKind,
      typeName,
      enumValues,
      present: value !== undefined && value !== null,
      values: cellValues(value),
      range: { start: 0, end: 0 },
      capability: fieldCapability,
    },
  };
}

function editorValueKind(
  capability: ViewFieldCapability | undefined,
  edit: NonNullable<ViewFieldCapability["edit"]>,
) {
  if (edit.kind === "enum") return "enum";

  if (edit.kind === "boolean") return "boolean";

  if (edit.kind === "node") return "relation";
  const valueKind = capability?.valueKind || edit.valueKind || edit.kind || "text";

  return valueKind === "real" ? "float" : valueKind;
}

export function editOpForCell(
  row: ViewTableRow,
  sourceField: string,
  field: string,
  operation: string,
  value: CellEditValue,
  listOverride?: boolean,
  expectedOverride?: OntologyEditOp["expected"],
): OntologyEditOp | null {
  const path = editPathForRow(row);

  if (!path || !field) return null;
  const current = rawFieldValue(row, sourceField);
  const link = operation === "setLinkField";
  const list = listOverride ?? Array.isArray(current);
  const values = cellEditValues(value);

  const expected = expectedOverride
    ? {
        ...expectedOverride,
        field: expectedOverride.field ?? fieldEditValueForCurrent(current, list || link),
      }
    : { field: fieldEditValueForCurrent(current, list || link) };

  const base: OntologyEditOp = {
    id: stableFieldOperationID(path, row.ref.nodeId, field),
    kind: operation,
    path,
    nodeId: row.ref.nodeId,
    // A note is addressed by its path. A staged row carries its preview
    // fingerprint, which stops matching once another edit to the note replays.
    structuralFingerprint: row.ref.fragment ? row.ref.structuralFingerprint : undefined,
    field,
    fieldValue: fieldEditValueForDraft(value, list, link),
    expected,
  };

  if (link || list) {
    return { ...base, values };
  }

  return { ...base, value: values[0] ?? "" };
}

function stableFieldOperationID(path: string, nodeID: string | undefined, field: string) {
  return `field:${encodeURIComponent(path)}:${encodeURIComponent(nodeID || "note")}:${encodeURIComponent(field)}`;
}

function fieldEditValueForDraft(
  value: CellEditValue,
  list: boolean,
  link: boolean,
): OntologyEditFieldValue {
  const values = cellEditValues(value);

  if (list) return { kind: "list", items: values };

  if (link) return values.length > 0 ? { kind: "list", items: values } : { kind: "unset" };

  if (values.length === 0) return { kind: "unset" };

  return { kind: "scalar", scalar: values[0] };
}

function fieldEditValueForCurrent(
  value: JsonValue | undefined,
  list: boolean,
): OntologyEditFieldValue {
  if (value === undefined || value === null) return { kind: "unset" };
  const values = cellValues(value);

  if (list) return { kind: "list", items: values };

  return { kind: "scalar", scalar: values[0] ?? "" };
}

function cellEditValues(value: CellEditValue): string[] {
  if (Array.isArray(value)) return [...value];

  return value === "" ? [] : [value];
}

export function cellValues(value: JsonValue | undefined): string[] {
  if (value === undefined || value === null) return [];

  if (Array.isArray(value)) return value.map((item) => stringifyCell(item));

  return [stringifyCell(value)];
}

export function editPathForRow(row: ViewTableRow) {
  return editPathForRef(row.ref, row.path);
}

/** A view row as a staged-state target; see `staging/stagedState`. */
export function stagedTargetForRow(row: ViewTableRow): StagedTarget {
  // An embedded row's path is its note's path, which keys the note's own fields.
  return { ref: row.ref, paths: row.ref.fragment ? [] : [row.path] };
}

/**
 * Shows staged edits in rows while the view re-executes with them, so an
 * edited cell does not flash its old value. Callers use it only during that
 * refetch; the server's rows stay canonical. Relation fields are left to the
 * server because their labels come from the target notes.
 */
export function rowsWithStagedValues(
  rows: ViewTableRow[],
  capabilities: ViewFieldCapability[],
  editSession: OntologyEditSessionResponse | null | undefined,
) {
  const editable = capabilities.filter(
    (capability) => capability.edit?.operation === "setField" && capability.edit.kind !== "node",
  );

  if (changeSummary(editSession).opCount === 0 || editable.length === 0) return rows;

  return rows.map((row) => {
    let patched: ViewTableRow | null = null;

    for (const capability of editable) {
      const staged = stagedFieldValue(
        editSession,
        stagedTargetForRow(row),
        capability.edit?.field ?? "",
      );

      if (!staged) continue;
      patched ??= { ...row, fields: { ...row.fields } };
      // null, not undefined: an undefined key falls through to the nested lookup.
      patched.fields = { ...patched.fields, [capability.key]: staged.value ?? null };

      // Views read the built-in title from the row itself, not its fields.
      if (capability.key === "title") patched.title = stringifyCell(staged.value);
    }

    return patched ?? row;
  });
}

export function cellHasChangedField(
  field: string,
  capability: ViewFieldCapability | undefined,
  changedFields: Set<string>,
) {
  const candidates = [
    field,
    capability?.key,
    capability?.canonicalField,
    capability?.edit?.field,
    ...(capability?.sourceKeys || []),
  ].flatMap((value) => {
    const trimmed = value?.trim();

    return trimmed ? [trimmed] : [];
  });

  return candidates.some((candidate) => changedFields.has(candidate));
}

export function booleanCellValue(value: JsonValue | undefined): boolean {
  if (isBoolean(value)) return value;

  if (Array.isArray(value)) return booleanCellValue(value[0]);

  return String(value ?? "").toLowerCase() === "true";
}

export function firstScalar(value: JsonValue | undefined): JsonValue | undefined {
  if (Array.isArray(value)) return value[0];

  return value;
}

export function candidateForInput(
  candidates: NonNullable<ViewFieldCapability["edit"]>["candidates"],
  value: string,
) {
  const trimmed = value.trim();
  const normalized = normalizeLinkTarget(trimmed);

  const exact = (candidates ?? []).find(
    (candidate) =>
      candidateInputValue(candidates ?? [], candidate) === trimmed ||
      candidate.label === trimmed ||
      candidate.value === trimmed ||
      candidate.path === trimmed ||
      candidate.label === normalized ||
      candidate.value === normalized ||
      candidate.path === normalized,
  );

  if (exact) return exact;

  // A bare wikilink names the file, which can differ from the note title.
  // Only trust it when exactly one candidate has that file name.
  const byFileName = (candidates ?? []).filter(
    (candidate) => (candidate.path ?? "").replace(/^.*\//, "").replace(/\.md$/i, "") === normalized,
  );

  return byFileName.length === 1 ? byFileName[0] : undefined;
}

export function candidateInputValue(
  candidates: NonNullable<ViewFieldCapability["edit"]>["candidates"],
  candidate: EditCandidate,
) {
  const duplicateLabel =
    (candidates ?? []).filter((item) => item.label === candidate.label).length > 1;

  if (!duplicateLabel) return candidate.label;

  return `${candidate.label} (${candidate.path || candidate.value})`;
}

function normalizeLinkTarget(value: string) {
  const trimmed = value.trim();
  const wikilink = trimmed.match(/^\[\[([^|\]]+)(?:\|[^\]]+)?\]\]$/);

  if (wikilink) return wikilink[1].trim();

  return trimmed;
}

export function stringifyCell(value: JsonValue | undefined): string {
  if (value === null || value === undefined) return "";

  if (Array.isArray(value)) return value.map(stringifyCell).join(", ");

  if (isJsonObject(value)) return JSON.stringify(value);

  return String(value);
}

function nestedFieldValue(fields: ViewTableRow["fields"], field: string): JsonValue | undefined {
  const root = isJsonObject(fields) ? fields : undefined;

  if (!root) return undefined;
  const direct = root[field];

  if (direct !== undefined) return direct;
  let current: JsonValue = root;

  for (const part of field.split(".")) {
    if (!isJsonObject(current)) return undefined;
    const next: JsonValue | undefined = current[part];

    if (next === undefined) return undefined;
    current = next;
  }

  return current;
}

function formatTimestamp(value: number | undefined) {
  if (!value) return "";
  const date = new Date(value < 1e12 ? value * 1000 : value);
  const pad = (part: number) => String(part).padStart(2, "0");

  // ISO order in local time, matching the authored dates beside it.
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
}

export function capabilityLabel(capability: ViewFieldCapability | undefined) {
  return humanizeLabel(capability?.label || capability?.key || "field");
}

export function humanizeLabel(label: string) {
  return humanizeName(label) || label;
}
