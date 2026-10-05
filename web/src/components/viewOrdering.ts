import type {
  OntologyEditOp,
  ViewExecuteResponse,
  ViewFieldCapability,
  ViewTableRow,
} from "../api/types";
import { configuredTableRowKey } from "./ConfiguredTableCell";
import {
  editOpForCell,
  firstScalar,
  rawFieldValue,
  stringifyCell,
} from "./ConfiguredTableCellHelpers";
import { capabilityFor } from "./ConfiguredViewModel";

// Manual ordering (SPEC-0108): a drop sets the fields the view sorts by, so
// the item lands where it was dropped. The sort stays the only source of order.

type SortSpec = NonNullable<ViewExecuteResponse["state"]["sort"]>[number];

export type OrderKey = {
  field: string;
  editField: string;
  descending: boolean;
  kind: "int" | "real" | "enum" | "bool";
  /** Whether the server sorts this field from the index: missing values last, text normalized. */
  indexed: boolean;
};

export type DropSide = "before" | "after";

/** The leading sort keys a drop can set, in sort order; empty when the view is not orderable. */
export function orderableKeys(
  sort: SortSpec[] | undefined,
  capabilities: ViewFieldCapability[],
): OrderKey[] {
  const keys: OrderKey[] = [];

  for (const spec of sort ?? []) {
    const capability = capabilityFor(capabilities, spec.field);
    const edit = capability?.edit;

    if (!edit || edit.operation !== "setField" || edit.list) break;
    const kind = orderKind(edit.kind, edit.valueKind || capability?.valueKind);

    // Booleans never sort from the index; see indexedScalarSort in pkg/app/views.
    const indexed = Boolean(capability?.indexedSortable) && kind !== "bool";

    // Placing between numbers assumes missing values sort last, as indexed fields do.
    if (!kind || (isNumericKind(kind) && !indexed)) break;
    keys.push({
      field: spec.field,
      editField: edit.field,
      descending: spec.direction === "desc",
      kind,
      indexed,
    });
  }

  return keys;
}

function orderKind(editKind: string, valueKind: string | undefined): OrderKey["kind"] | null {
  if (editKind === "enum") return "enum";

  if (editKind === "boolean" || valueKind === "bool") return "bool";

  if (editKind === "number" && (valueKind === "int" || valueKind === "real")) return valueKind;

  return null;
}

/**
 * Why a view cannot be reordered right now, or null when it can. Placement
 * needs every neighbor, so a truncated or locally filtered result is read-only.
 */
export function orderingBlockedReason(
  execution: ViewExecuteResponse,
  rows: ViewTableRow[],
  keys: OrderKey[],
  canStage: boolean,
): string | null {
  if (keys.length === 0 || !canStage) return "unsupported";

  const pageInfo = execution.pageInfo;

  const truncated =
    Boolean(pageInfo?.hasMore) ||
    (pageInfo?.offset ?? 0) > 0 ||
    (pageInfo?.total ?? 0) > execution.rows.length;

  if (truncated || rows.length !== execution.rows.length) {
    return "Show all items to reorder";
  }

  return null;
}

/** A sort value; numbers are kept in canonical decimal form so equal values compare equal. */
type Value = string | null;

function valueOf(row: ViewTableRow, key: OrderKey): Value {
  const raw = firstScalar(rawFieldValue(row, key.field));

  if (raw === undefined || raw === null || raw === "") return null;

  if (key.kind === "int" || key.kind === "real") {
    const number = Number(stringifyCell(raw));

    return Number.isFinite(number) ? String(number) : null;
  }

  return stringifyCell(raw);
}

/**
 * Orders two values as the server's view sort does (pkg/app/views sortRows).
 * Indexed fields keep missing values last in either direction and compare
 * normalized text; other fields, here booleans and unindexed enums, sort
 * missing values first and compare text as written.
 */
function compareValues(a: Value, b: Value, key: OrderKey) {
  if (a === b) return 0;

  if (key.indexed && (a === null || b === null)) return a === null ? 1 : -1;

  const order = a === null ? -1 : b === null ? 1 : compareText(a, b, key);

  return key.descending ? -order : order;
}

function compareText(a: string, b: string, key: OrderKey) {
  if (isNumeric(key)) return Number(a) - Number(b);
  const left = key.indexed ? indexedSortText(a) : a;
  const right = key.indexed ? indexedSortText(b) : b;

  return left < right ? -1 : left > right ? 1 : 0;
}

/** Mirrors indexedSortText in pkg/app/views: lowercase, trimmed, link wrapper and alias removed. */
function indexedSortText(value: string) {
  const text = value.trim().toLowerCase().replace(/^\[\[/, "").replace(/\]\]$/, "");

  return (text.split("|", 1)[0] ?? "").trim();
}

/** Re-sorts rows by the orderable keys so staged placements show before the server re-executes. */
export function sortRowsByKeys(rows: ViewTableRow[], keys: OrderKey[]) {
  if (keys.length === 0) return rows;

  // Array.prototype.sort is stable, so later sort keys keep the server's order.
  return [...rows].sort((left, right) => {
    for (const key of keys) {
      const order = compareValues(valueOf(left, key), valueOf(right, key), key);

      if (order !== 0) return order;
    }

    return 0;
  });
}

export type PlacementRequest = {
  keys: OrderKey[];
  moving: ViewTableRow;
  /** The destination group's rows in display order; may include the moving row. */
  groupRows: ViewTableRow[];
  /** The row the drop is beside, or null to drop at the end of the group. */
  target: ViewTableRow | null;
  side: DropSide;
};

/**
 * The field values that place `moving` at the drop point, as setField ops.
 * Returns an empty list when the drop leaves the item where it is.
 */
export function placementOps(request: PlacementRequest): OntologyEditOp[] {
  const updates = placementValues(request);
  const ops: OntologyEditOp[] = [];

  const rowsByKey = new Map(
    [request.moving, ...request.groupRows].map((row) => [configuredTableRowKey(row), row]),
  );

  for (const [rowKey, values] of updates) {
    const row = rowsByKey.get(rowKey);

    if (!row) continue;

    for (const [field, value] of values) {
      const key = request.keys.find((candidate) => candidate.field === field);

      if (!key) continue;

      const op = editOpForCell(row, key.field, key.editField, "setField", value ?? "", false);

      if (op) ops.push(op);
    }
  }

  return ops;
}

/** Row key to field to new value, only for values that change. Exported for tests. */
export function placementValues({
  keys,
  moving,
  groupRows,
  target,
  side,
}: PlacementRequest): Map<string, Map<string, Value>> {
  const movingKey = configuredTableRowKey(moving);

  // A drop on the item itself leaves it where it is.
  if (target && configuredTableRowKey(target) === movingKey) return new Map();
  const originalIndex = groupRows.findIndex((row) => configuredTableRowKey(row) === movingKey);
  const others = groupRows.filter((row) => configuredTableRowKey(row) !== movingKey);

  const targetIndex = target
    ? others.findIndex((row) => configuredTableRowKey(row) === configuredTableRowKey(target))
    : -1;

  const insertAt =
    target === null || targetIndex === -1
      ? others.length
      : side === "before"
        ? targetIndex
        : targetIndex + 1;

  const updates = new Map<string, Map<string, Value>>();

  if (originalIndex === insertAt) return updates;

  const current = (row: ViewTableRow, key: OrderKey) => {
    const staged = updates.get(configuredTableRowKey(row))?.get(key.field);

    return staged !== undefined ? staged : valueOf(row, key);
  };

  const set = (row: ViewTableRow, key: OrderKey, value: Value) => {
    // Values the server sorts as equal, such as "high" and " High ", need no edit.
    if (compareValues(valueOf(row, key), value, key) === 0) {
      updates.get(configuredTableRowKey(row))?.delete(key.field);

      return;
    }

    const rowKey = configuredTableRowKey(row);
    const values = updates.get(rowKey) ?? new Map<string, Value>();
    values.set(key.field, value);
    updates.set(rowKey, values);
  };

  let previous: ViewTableRow | null = others[insertAt - 1] ?? null;
  let next: ViewTableRow | null = others[insertAt] ?? null;
  // The row the pointer was over decides enum values when the neighbors differ.
  const anchorIsPrevious = target === null || side === "after";

  for (const [index, key] of keys.entries()) {
    const p = previous ? current(previous, key) : null;
    const n = next ? current(next, key) : null;
    const last = index === keys.length - 1;

    if (key.kind === "enum" || key.kind === "bool") {
      if (previous && next && compareValues(p, n, key) === 0) {
        set(moving, key, p);
        continue;
      }

      const anchor: ViewTableRow | null = anchorIsPrevious
        ? (previous ?? next)
        : (next ?? previous);

      if (anchor) set(moving, key, current(anchor, key));

      // Rows holding another value are outside the run the item joins.
      if (anchor === previous) next = null;
      else previous = null;
      continue;
    }

    if (previous && next && p !== null && p === n && !last) {
      set(moving, key, p);
      continue;
    }

    const own = valueOf(moving, key);

    const value =
      own !== null && fitsBetween(own, p, n, previous, next, key)
        ? own
        : between(p, n, previous, next, key);

    if (value !== undefined) {
      set(moving, key, String(value));
    } else {
      renumber(key, keys.slice(0, index), others, insertAt, moving, current, set);
    }

    break;
  }

  for (const [rowKey, values] of updates) {
    if (values.size === 0) updates.delete(rowKey);
  }

  return updates;
}

/** Whether a value already sorts between the neighbors. */
function fitsBetween(
  value: string,
  p: Value,
  n: Value,
  previous: ViewTableRow | null,
  next: ViewTableRow | null,
  key: OrderKey,
) {
  if (previous && p === null) return false;

  if (![value, p, n].every((candidate) => exactNumber(candidate, key))) return false;
  const afterPrevious = !previous || compareValues(p, value, key) < 0;
  const beforeNext = !next || compareValues(value, n, key) < 0;

  return afterPrevious && beforeNext;
}

/**
 * A number strictly between the neighbors in sort direction, or undefined
 * when none fits and the group must be renumbered.
 */
function between(
  p: Value,
  n: Value,
  previous: ViewTableRow | null,
  next: ViewTableRow | null,
  key: OrderKey,
): number | undefined {
  // A previous row without a value sorts last, so nothing numbered fits after it.
  if (previous && p === null) return undefined;

  // Ranks past what a double holds exactly cannot be placed between; renumber instead.
  if (!exactNumber(p, key) || !exactNumber(n, key)) return undefined;
  const value = candidateBetween(p, n, previous, next, key);

  if (value === undefined || !exactNumber(String(value), key)) return undefined;

  return value === Number(p ?? NaN) || value === Number(n ?? NaN) ? undefined : value;
}

function candidateBetween(
  p: Value,
  n: Value,
  previous: ViewTableRow | null,
  next: ViewTableRow | null,
  key: OrderKey,
): number | undefined {
  const step = key.descending ? -1 : 1;

  if (p !== null && n !== null) {
    const low = Math.min(Number(p), Number(n));
    const high = Math.max(Number(p), Number(n));

    if (key.kind === "int") {
      return high - low >= 2 ? Math.floor((low + high) / 2) : undefined;
    }

    return shortestBetween(low, high);
  }

  if (p !== null) return Number(p) + step;

  if (n !== null) return Number(n) - step;

  // No numbered neighbors: dropped at the top of unnumbered rows, or alone.
  return next || !previous ? 1 : undefined;
}

/** Whether a rank is held exactly: a safe integer for Int keys, a finite number for Real. */
function exactNumber(value: Value, key: OrderKey) {
  if (value === null) return true;

  return key.kind === "int" ? Number.isSafeInteger(Number(value)) : Number.isFinite(Number(value));
}

/** The shortest decimal strictly between two numbers, nearest their midpoint. */
export function shortestBetween(low: number, high: number): number | undefined {
  if (!(high > low)) return undefined;
  const middle = (low + high) / 2;

  for (let digits = 0; digits <= 12; digits += 1) {
    const candidate = Number(middle.toFixed(digits));

    if (candidate > low && candidate < high) return candidate;
  }

  return undefined;
}

/**
 * Gives whole numbers in sort direction to the rows that share the item's
 * earlier sort values, from the first through the later of the item and the
 * last row that already has a value. Rows past that stay unnumbered.
 */
function renumber(
  key: OrderKey,
  earlierKeys: OrderKey[],
  others: ViewTableRow[],
  insertAt: number,
  moving: ViewTableRow,
  current: (row: ViewTableRow, key: OrderKey) => Value,
  set: (row: ViewTableRow, key: OrderKey, value: Value) => void,
) {
  const order = [...others.slice(0, insertAt), moving, ...others.slice(insertAt)];

  const run = order.filter((row) =>
    earlierKeys.every(
      (earlier) => compareValues(current(row, earlier), current(moving, earlier), earlier) === 0,
    ),
  );

  const movingIndex = run.indexOf(moving);
  let end = movingIndex;

  run.forEach((row, index) => {
    if (row !== moving && current(row, key) !== null) end = Math.max(end, index);
  });

  for (let index = 0; index <= end; index += 1) {
    const row = run[index];

    if (row) set(row, key, String(key.descending ? end + 1 - index : index + 1));
  }
}

function isNumeric(key: OrderKey) {
  return isNumericKind(key.kind);
}

function isNumericKind(kind: OrderKey["kind"] | null) {
  return kind === "int" || kind === "real";
}
