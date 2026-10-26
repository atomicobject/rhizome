/**
 * The table's column policy (SPEC-0126 US4): the title column keeps a minimum
 * and takes all remaining width; every other column has a fixed width and
 * never shares the slack. When the container cannot fit every column at those
 * widths, relation columns leave first, then IDs, then dates, and the record
 * pane shows them for the selected row.
 */

export type ColumnKind =
  | "title"
  | "id"
  | "enum"
  | "date"
  | "number"
  | "flag"
  | "boolean"
  | "relation";

/** Fixed widths, padding included; short unclassed text sizes like an enum. */
export const COLUMN_WIDTHS: Record<Exclude<ColumnKind, "title">, number> = {
  id: 176,
  enum: 136,
  date: 104,
  number: 72,
  flag: 72,
  boolean: 72,
  relation: 180,
};

export const TITLE_MIN_ONE_LINE = 240;

export const TITLE_MIN_TWO_LINE = 320;

/** The select-and-drag cell before the first column. */
export const CHECK_CELL_WIDTH = 30;

/** Kinds that leave a narrow table, first to last. */
const OVERFLOW_ORDER: ColumnKind[] = ["relation", "id", "date"];

/** A column's kind, read from the cell classes `columnClassName` assigns. */
export function columnKind(className: string, isTitle: boolean): ColumnKind {
  if (isTitle) return "title";

  const has = (name: string) => new RegExp(`configured-view__cell--${name}\\b`).test(className);

  if (has("flag")) return "flag";

  if (has("numeric")) return "number";

  if (has("identifier")) return "id";

  if (has("date")) return "date";

  if (has("boolean")) return "boolean";

  if (has("relation")) return "relation";

  return "enum";
}

export type ColumnAllocation = {
  /** Fields that stay in the table, in table order. */
  shown: string[];
  /** Fields that moved to the record pane, in table order. */
  moved: string[];
  /** The table's minimum width: every shown column at its width, the title at its minimum. */
  minWidth: number;
};

/**
 * Allocates `available` pixels across `columns`. A user-resized width
 * replaces a column's fixed width, and on the title raises its minimum.
 * An unmeasured container (`Infinity`) keeps every column.
 */
export function allocateColumns(
  columns: ReadonlyArray<{ field: string; kind: ColumnKind }>,
  available: number,
  twoLine: boolean,
  userWidths: Readonly<Record<string, number>>,
): ColumnAllocation {
  const titleMin = twoLine ? TITLE_MIN_TWO_LINE : TITLE_MIN_ONE_LINE;

  const width = (column: { field: string; kind: ColumnKind }) =>
    column.kind === "title"
      ? Math.max(titleMin, userWidths[column.field] ?? 0)
      : (userWidths[column.field] ?? COLUMN_WIDTHS[column.kind]);

  let shown = [...columns];
  const total = () => CHECK_CELL_WIDTH + shown.reduce((sum, column) => sum + width(column), 0);

  for (const kind of OVERFLOW_ORDER) {
    while (total() > available) {
      const index = shown.map((column) => column.kind).lastIndexOf(kind);

      if (index < 0) break;
      shown = shown.filter((_, other) => other !== index);
    }
  }

  const kept = new Set(shown.map((column) => column.field));

  return {
    shown: shown.map((column) => column.field),
    moved: columns.flatMap((column) => (kept.has(column.field) ? [] : [column.field])),
    minWidth: total(),
  };
}
