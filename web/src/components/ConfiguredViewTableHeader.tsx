import type { KeyboardEvent, PointerEvent } from "react";

import type { ViewFieldCapability, ViewTableColumn } from "../api/types";
import { capabilityFor, columnLabel, isIssueColumn } from "./ConfiguredViewModel";
import { columnClassName, type SortSpec, sortDirection } from "./ConfiguredViewTableModel";

const MIN_COLUMN_WIDTH = 56;

const MAX_COLUMN_WIDTH = 960;

/** Pixels each arrow key press on a resize handle adds or removes. */
const KEY_STEP = 16;

function clampWidth(width: number) {
  return Math.min(MAX_COLUMN_WIDTH, Math.max(MIN_COLUMN_WIDTH, Math.round(width)));
}

type Props = {
  columns: ViewTableColumn[];
  capabilities: ViewFieldCapability[];
  /** The effective sort: the reader's, else the one the view ran with. */
  sort: SortSpec[];
  issueCounts?: ReadonlyMap<string, number>;
  /** Shift-activation adds the column as another sort key. */
  onSort: (column: ViewTableColumn, additive?: boolean) => void;
  /** Set when the title column takes the slack beside other prose columns. */
  primaryColumnIndex: number | null;
  columnWidths?: Record<string, number>;
  onColumnWidthsChange?: (widths: Record<string, number>) => void;
  allChecked: boolean;
  onToggleAll: () => void;
};

/** The table's header row: select-all, sortable column labels, and resize handles. */
export function ConfiguredViewTableHeader({
  columns,
  capabilities,
  sort,
  issueCounts,
  onSort,
  primaryColumnIndex,
  columnWidths,
  onColumnWidthsChange,
  allChecked,
  onToggleAll,
}: Props) {
  const widths = columnWidths ?? {};

  const resize = (field: string, width: number) =>
    onColumnWidthsChange?.({ ...widths, [field]: width });

  // A drag sizes the header cell directly and commits once, on release, so
  // the table does not re-render on every pointer move.
  const startResize = (event: PointerEvent<HTMLSpanElement>, field: string) => {
    const header = event.currentTarget.closest("th");

    if (!header || !onColumnWidthsChange) return;
    event.preventDefault();
    event.stopPropagation();
    const startX = event.clientX;
    const startWidth = header.getBoundingClientRect().width;
    const before = header.style.width;
    let width: number | null = null;

    const move = (moveEvent: globalThis.PointerEvent) => {
      width = clampWidth(startWidth + moveEvent.clientX - startX);
      header.style.width = `${width}px`;
    };

    const stop = () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", commit);
      window.removeEventListener("pointercancel", cancel);
    };

    const commit = () => {
      stop();

      if (width !== null) resize(field, width);
    };

    const cancel = () => {
      stop();
      header.style.width = before;
    };

    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", commit);
    window.addEventListener("pointercancel", cancel);
  };

  const resizeByKey = (event: KeyboardEvent<HTMLSpanElement>, field: string) => {
    const step = event.key === "ArrowRight" ? KEY_STEP : event.key === "ArrowLeft" ? -KEY_STEP : 0;
    const header = event.currentTarget.closest("th");

    if (!step || !header) return;
    event.preventDefault();
    resize(field, clampWidth(header.getBoundingClientRect().width + step));
  };

  return (
    <thead>
      <tr>
        <th scope="col" className="configured-view__check-cell" aria-label="Select rows">
          <input
            type="checkbox"
            aria-label="Select all shown rows"
            checked={allChecked}
            onChange={onToggleAll}
          />
        </th>
        {columns.map((column, columnIndex) => {
          const direction = sortDirection(sort, column.field);
          const sortIndex = sort.findIndex((item) => item.field === column.field);
          const label = columnLabel(column, capabilities);

          const sortable =
            !isIssueColumn(column.field, issueCounts) &&
            capabilityFor(capabilities, column.field)?.sortable !== false;

          return (
            <th
              key={column.field}
              scope="col"
              aria-sort={
                sortable
                  ? direction === "asc"
                    ? "ascending"
                    : direction === "desc"
                      ? "descending"
                      : "none"
                  : undefined
              }
              className={`${columnClassName(column, capabilities)}${
                columnIndex === primaryColumnIndex ? " configured-view__cell--primary" : ""
              }`}
              title={sortable ? `${label} (Shift-click to add a sort key)` : label}
              // Cells announce this as their header; the sort button's
              // action wording would otherwise become the column name.
              aria-label={label}
              style={widths[column.field] ? { width: widths[column.field] } : undefined}
            >
              {!sortable ? (
                <span className="configured-view__header-label">{label}</span>
              ) : (
                <button
                  type="button"
                  onClick={(event) => onSort(column, event.shiftKey)}
                  aria-label={`Sort ${label} ${direction === "asc" ? "descending" : "ascending"}`}
                >
                  <span className="configured-view__header-label">{label}</span>
                  {direction && (
                    <span className="configured-view__sort-arrow" aria-hidden="true">
                      {direction === "asc" ? "↑" : "↓"}
                      {sort.length > 1 && <sub>{sortIndex + 1}</sub>}
                    </span>
                  )}
                </button>
              )}
              {onColumnWidthsChange && (
                <span
                  className="configured-view__column-resize"
                  role="separator"
                  aria-orientation="vertical"
                  aria-label={`Resize ${label}`}
                  aria-valuenow={widths[column.field]}
                  aria-valuemin={MIN_COLUMN_WIDTH}
                  aria-valuemax={MAX_COLUMN_WIDTH}
                  tabIndex={0}
                  title="Drag or use the arrow keys to resize"
                  onPointerDown={(event) => startResize(event, column.field)}
                  onKeyDown={(event) => resizeByKey(event, column.field)}
                />
              )}
            </th>
          );
        })}
      </tr>
    </thead>
  );
}
