import type {
  OntologyEditSessionResponse,
  TypeProfile,
  ViewExecuteResponse,
  ViewFieldCapability,
  ViewTableRow,
} from "../api/types";
import { stagedFieldValue } from "../staging/stagedState";
import { configuredTableRowKey } from "./ConfiguredTableCell";
import {
  cellValues,
  firstScalar,
  rawFieldValue,
  stagedTargetForRow,
  stringifyCell,
} from "./ConfiguredTableCellHelpers";
import { fieldCapability } from "./ConfiguredViewModel";

type BoardLayout = NonNullable<ViewExecuteResponse["board"]>;

export type BoardColumn = BoardLayout["columns"][number];

export type BoardLane = NonNullable<BoardLayout["lanes"]>[number];

export type BoardLanes = {
  field: string;
  lanes: BoardLane[];
  /** Lane keys per row key; a row whose lane field holds several targets sits in each. */
  membership: Map<string, string[]>;
};

/** The lane field that turns lanes off; an unset choice keeps the view's or the profile's. */
export const NO_LANES = "none";

/**
 * The board's lanes as the server laid them out (SPEC-0112). The execution
 * request carries the reader's lane field, so the board only reads them,
 * except a card whose lane field is staged but not yet re-executed, which
 * sits in the staged lanes now and counts there.
 */
export function boardLanes(
  execution: ViewExecuteResponse,
  editField: string | undefined,
  editSession: OntologyEditSessionResponse | null | undefined,
): BoardLanes | null {
  const board = execution.board;

  if (!board?.laneField || !board.lanes) return null;
  const lanes = board.lanes;
  const membership = new Map<string, string[]>();
  const countDelta = new Map<string, number>();

  for (const lane of lanes) {
    for (const index of lane.cells.flatMap((cell) => cell.rows)) {
      const row = execution.rows[index];

      if (!row) continue;
      const key = configuredTableRowKey(row);
      membership.set(key, [...(membership.get(key) ?? []), lane.key]);
    }
  }

  for (const row of execution.rows) {
    const staged = editField
      ? stagedFieldValue(editSession, stagedTargetForRow(row), editField)
      : null;

    if (!staged) continue;
    const values = cellValues(staged.value);

    const keys = lanes
      .filter((lane) => (values.length > 0 ? values.includes(lane.value) : lane.value === ""))
      .map((lane) => lane.key);

    // A staged value with no lane on this page keeps the card where the server put it.
    if (keys.length === 0) continue;
    const rowKey = configuredTableRowKey(row);
    const before = membership.get(rowKey) ?? [];

    for (const key of before) if (!keys.includes(key)) bump(countDelta, key, -1);

    for (const key of keys) if (!before.includes(key)) bump(countDelta, key, 1);
    membership.set(rowKey, keys);
  }

  // Server counts may include rows beyond the page, so staged moves adjust them.
  return {
    field: board.laneField,
    lanes: lanes.map((lane) =>
      countDelta.has(lane.key)
        ? { ...lane, count: lane.count + (countDelta.get(lane.key) ?? 0) }
        : lane,
    ),
    membership,
  };
}

function bump(counts: Map<string, number>, key: string, by: number) {
  counts.set(key, (counts.get(key) ?? 0) + by);
}

function resolved(capabilities: ViewFieldCapability[], fields: Array<string | undefined>) {
  const out: ViewFieldCapability[] = [];

  for (const field of fields) {
    const capability = fieldCapability(capabilities, field);

    if (capability && !out.includes(capability)) out.push(capability);
  }

  return out;
}

/** Column fields a board can switch among: the lifecycle and ordered fields. */
export function boardColumnFieldOptions(
  profile: TypeProfile | undefined,
  capabilities: ViewFieldCapability[],
  current: string | undefined,
) {
  return resolved(capabilities, [
    profile?.lifecycleField,
    ...(profile?.orderedFields ?? []),
    current,
  ]);
}

/** Lane fields: ordered, people, and relation fields other than the column field. */
export function boardLaneFieldOptions(
  profile: TypeProfile | undefined,
  capabilities: ViewFieldCapability[],
  columnField: string | undefined,
  current: string | undefined,
) {
  const column = fieldCapability(capabilities, columnField);

  return resolved(capabilities, [
    ...(profile?.orderedFields ?? []),
    ...(profile?.peopleFields ?? []),
    ...(profile?.relationFields ?? []),
    current === NO_LANES ? undefined : current,
  ]).filter((capability) => capability !== column);
}

type SortSpec = NonNullable<ViewExecuteResponse["state"]["sort"]>[number];

/**
 * Puts each card in its server column, except a card whose column field is
 * staged but not yet re-executed, which moves to the staged column now, at
 * the position the view's sort will give it.
 */
export function placeStagedCards(
  board: BoardLayout,
  rows: ViewTableRow[],
  sort: SortSpec[],
  editField: string | undefined,
  editSession: OntologyEditSessionResponse | null | undefined,
) {
  const placed = new Map<string, ViewTableRow[]>();
  const countDelta = new Map<string, number>();
  const moved: Array<{ row: ViewTableRow; target: string }> = [];

  for (const column of board.columns) {
    const columnRows: ViewTableRow[] = [];

    for (const row of rows.slice(column.rowStart, column.rowEnd)) {
      const staged = editField
        ? stagedFieldValue(editSession, stagedTargetForRow(row), editField)
        : null;

      const stagedValue = staged ? stringifyCell(firstScalar(staged.value)) : null;

      const target = board.columns.find(
        (candidate) => candidate.key !== column.key && candidate.value === stagedValue,
      );

      if (!target) {
        columnRows.push(row);
        continue;
      }

      moved.push({ row, target: target.key });
      countDelta.set(column.key, (countDelta.get(column.key) ?? 0) - 1);
      countDelta.set(target.key, (countDelta.get(target.key) ?? 0) + 1);
    }

    placed.set(column.key, columnRows);
  }

  for (const { row, target } of moved) {
    const columnRows = placed.get(target);

    if (!columnRows) continue;
    const index = columnRows.findIndex((candidate) => compareRowsBySort(row, candidate, sort) < 0);
    columnRows.splice(index === -1 ? columnRows.length : index, 0, row);
  }

  return { rows: placed, countDelta };
}

// ponytail: text compare with numeric runs, not the server's typed sort; it
// only orders a staged card among its neighbors until the refetch lands.
function compareRowsBySort(left: ViewTableRow, right: ViewTableRow, sort: SortSpec[]) {
  for (const spec of sort) {
    const a = stringifyCell(firstScalar(rawFieldValue(left, spec.field)));
    const b = stringifyCell(firstScalar(rawFieldValue(right, spec.field)));

    if (a === b) continue;

    // Missing values sort last in either direction, matching the server.
    if (!a || !b) return a ? -1 : 1;
    const order = a.localeCompare(b, undefined, { numeric: true });

    return spec.direction === "desc" ? -order : order;
  }

  return sort.length > 0 ? 0 : -1;
}
