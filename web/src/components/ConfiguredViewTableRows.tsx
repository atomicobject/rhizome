import { type CSSProperties, type DragEvent, Fragment, memo, type RefObject } from "react";

import type {
  OntologyEditOp,
  OntologyEditSessionResponse,
  ValidationScope,
  ViewFieldCapability,
  ViewTableColumn,
  ViewTableRow,
} from "../api/types";
import type { OpenMode } from "./useNoteTabs";
import { ConfiguredTableCellContent, configuredTableRowKey } from "./ConfiguredTableCell";
import { fieldValue } from "./ConfiguredTableCellHelpers";
import { ChangedCell } from "./ConfiguredTableCellReadOnly";
import {
  capabilityFor,
  GroupValueMark,
  groupValueLabel,
  isIssueColumn,
} from "./ConfiguredViewModel";
import { type ConfiguredTableGroup, groupTone } from "./ConfiguredViewTableModel";
import { moveTableFocus } from "./focusManagement";
import { reorderDelta, type ViewReorder } from "./useViewReorder";
import { useTypeLabel } from "./typeLabels";

/** A column with its capability and class resolved once per table, not per cell. */
export type TableColumnCell = {
  column: ViewTableColumn;
  capability: ViewFieldCapability | undefined;
  className: string;
};

/**
 * Props every row shares. The table keeps them stable while only selection,
 * checks, or drag state change, so memoized rows skip those renders.
 */
export type TableRowProps = {
  columnCells: TableColumnCell[];
  primaryColumnIndex: number;
  capabilities: ViewFieldCapability[];
  viewID: string;
  editSession?: OntologyEditSessionResponse | null;
  vaultKey?: string | null;
  onStageOps?: (ops: OntologyEditOp[]) => Promise<void> | undefined;
  onOpenRow: (row: ViewTableRow, mode?: OpenMode) => void;
  issueCounts?: ReadonlyMap<string, number>;
  onOpenIssues?: (scope: ValidationScope) => void;
  onSelectRow: (key: string) => void;
  /** The latest reorder state; rows call its handlers when an event fires. */
  reorderRef: RefObject<ViewReorder>;
  reorderEnabled: boolean;
  reorderBlockedReason: ViewReorder["blockedReason"];
  /** True while any row is being dragged, so rows accept drops. */
  dragActive: boolean;
  onToggleChecked: (key: string, range?: boolean) => void;
  onClearSelection: () => void;
  /** Set when rows show this field as a clamped second line under the title. */
  summaryField?: string;
  /** Enum fields without lifecycle stages; their values read as plain labels. */
  categoryFields: ReadonlySet<string>;
};

/** What one row shows for the current selection, checks, drag, and staged edits. */
export type TableRowState = {
  selected: boolean;
  tabbable: boolean;
  checked: boolean;
  dragging: boolean;
  pending: boolean;
  dropClass: string;
  changedFields: ReadonlySet<string>;
};

export function ConfiguredViewTableGroup({
  group,
  rowsForGroup,
  columnSpan,
  rowProps,
  rowState,
  isExpanded,
  onToggle,
}: {
  group: ConfiguredTableGroup;
  rowsForGroup: (group: ConfiguredTableGroup) => ViewTableRow[];
  columnSpan: number;
  rowProps: TableRowProps;
  rowState: (row: ViewTableRow, groupId: string) => TableRowState;
  isExpanded: (group: ConfiguredTableGroup) => boolean;
  onToggle: (group: ConfiguredTableGroup) => void;
}) {
  const expanded = isExpanded(group);

  const capability = capabilityFor(rowProps.capabilities, group.field);
  const label = groupValueLabel(capability, group.value, group.label);

  const leaf = group.children.length === 0;
  const reorder = rowProps.reorderRef.current;
  // Dropping on a header puts the item first in the group, beside the header.
  const firstRow = leaf ? (rowsForGroup(group)[0] ?? null) : null;

  return (
    <Fragment>
      <tr
        {...(leaf && reorder.dragKey ? reorder.dropTarget(group.key, firstRow, "before") : {})}
        className={`configured-view__group-row${rowProps.categoryFields.has(group.field) ? " is-category" : ""}`}
        data-depth={group.depth}
        // SAFETY: React forwards this numeric custom property unchanged.
        style={{ "--group-depth": group.depth } as CSSProperties}
      >
        <th colSpan={columnSpan} scope="rowgroup">
          <button
            type="button"
            className="configured-view__group-toggle"
            aria-label={`${label} ${group.count}`}
            aria-expanded={expanded}
            onClick={() => onToggle(group)}
          >
            <span className="configured-view__group-caret" aria-hidden="true" />
            <GroupValueMark
              capability={capability}
              tone={group.tone ?? groupTone(rowProps.capabilities, group)}
              label={label}
            />
            <span className="configured-view__group-count">{group.count}</span>
          </button>
        </th>
      </tr>
      {expanded && group.children.length > 0
        ? group.children.map((child) => (
            <ConfiguredViewTableGroup
              key={child.key}
              group={child}
              rowsForGroup={rowsForGroup}
              columnSpan={columnSpan}
              rowProps={rowProps}
              rowState={rowState}
              isExpanded={isExpanded}
              onToggle={onToggle}
            />
          ))
        : null}
      {expanded && leaf
        ? rowsForGroup(group).map((row) => (
            <ConfiguredViewTableRow
              key={configuredTableRowKey(row)}
              row={row}
              groupId={group.key}
              {...rowProps}
              {...rowState(row, group.key)}
            />
          ))
        : null}
    </Fragment>
  );
}

// Memoized: a row renders again only when its own state or the shared props
// change. Reorder handlers run through `reorderRef` when an event fires, so a
// row that skipped a render never acts on stale drag state.
export const ConfiguredViewTableRow = memo(function ConfiguredViewTableRow({
  row,
  columnCells,
  primaryColumnIndex,
  viewID,
  editSession,
  vaultKey,
  onStageOps,
  onOpenRow,
  issueCounts,
  onOpenIssues,
  onSelectRow,
  reorderRef,
  reorderEnabled,
  reorderBlockedReason,
  dragActive,
  onToggleChecked,
  onClearSelection,
  summaryField,
  categoryFields,
  groupId,
  selected,
  tabbable,
  checked: isChecked,
  dragging,
  pending,
  dropClass,
  changedFields,
}: TableRowProps & TableRowState & { row: ViewTableRow; groupId: string }) {
  const rowKey = configuredTableRowKey(row);
  const dirty = changedFields.size > 0;
  const showHandle = reorderEnabled || reorderBlockedReason !== "unsupported";
  const source = () => reorderRef.current.dragSource(row);
  const target = () => reorderRef.current.dropTarget(groupId, row);

  const handle = showHandle ? (
    <span
      className={`configured-view__drag-handle${reorderEnabled ? "" : " is-disabled"}`}
      aria-hidden="true"
      title={
        reorderEnabled
          ? "Drag to reorder, or press Alt+Up or Alt+Down"
          : (reorderBlockedReason ?? "")
      }
      {...(reorderEnabled
        ? {
            draggable: !pending,
            onDragStart: (event: DragEvent<HTMLSpanElement>) => {
              // Drag the whole row's image, not the small handle.
              const rowElement = event.currentTarget.closest("tr");

              if (rowElement) event.dataTransfer.setDragImage(rowElement, 12, 14);
              source().onDragStart(event);
            },
            onDragEnd: () => source().onDragEnd(),
          }
        : {})}
    />
  ) : null;

  const typeLabel = useTypeLabel();
  const summary = summaryField ? fieldValue(row, summaryField) : "";

  return (
    <tr
      className={`configured-view__row${selected ? " is-selected" : ""}${isChecked ? " is-checked" : ""}${dirty ? " is-dirty" : ""}${dragging ? " is-dragging" : ""}${dropClass}`}
      data-row-key={rowKey}
      data-group-key={groupId}
      tabIndex={tabbable ? 0 : -1}
      aria-keyshortcuts={`Enter ArrowUp ArrowDown ArrowRight X Escape${reorderEnabled ? " Alt+ArrowUp Alt+ArrowDown" : ""}`}
      aria-busy={pending || undefined}
      {...(dragActive
        ? {
            onDragOver: (event: DragEvent<HTMLTableRowElement>) => target().onDragOver(event),
            onDragLeave: (event: DragEvent<HTMLTableRowElement>) => target().onDragLeave(event),
            onDrop: (event: DragEvent<HTMLTableRowElement>) => target().onDrop(event),
          }
        : {})}
      onClick={(event) => {
        if (event.shiftKey) onToggleChecked(rowKey, true);
        onSelectRow(rowKey);
      }}
      onDoubleClick={(event) => {
        if (rowOpenTarget(event.target)) onOpenRow(row, "activate");
      }}
      onFocus={() => onSelectRow(rowKey)}
      onKeyDown={(event) => {
        const onRow = event.target === event.currentTarget;

        if (onRow && event.key === "Enter") {
          event.preventDefault();
          onOpenRow(row);

          return;
        }

        if (onRow && event.key.toLowerCase() === "x" && !event.metaKey && !event.ctrlKey) {
          event.preventDefault();
          onToggleChecked(rowKey, event.shiftKey);

          return;
        }

        if (onRow && event.key === "Escape") {
          event.preventDefault();
          onClearSelection();

          return;
        }

        const delta = event.target === event.currentTarget ? reorderDelta(event) : null;

        if (delta !== null && reorderEnabled) {
          event.preventDefault();
          void reorderRef.current
            .moveByKeyboard(row, delta, groupId)
            .then(() => focusRow(rowKey, groupId));

          return;
        }

        if (moveTableFocus(event.currentTarget, event.target, event.key)) event.preventDefault();
      }}
    >
      <td className="configured-view__check-cell">
        {handle}
        <input
          type="checkbox"
          data-row-select
          // Rows stay one Tab stop; X toggles the focused row's selection.
          tabIndex={-1}
          aria-label={`Select ${row.title || row.path || rowKey}`}
          checked={isChecked}
          onClick={(event) => {
            // The row's own click would treat a shift-click as a second toggle.
            event.stopPropagation();
            onToggleChecked(rowKey, event.shiftKey);
            onSelectRow(rowKey);
          }}
          onChange={() => {}}
        />
      </td>
      {columnCells.map(({ column, capability, className }, columnIndex) => {
        if (isIssueColumn(column.field, issueCounts)) {
          const path = row.ref.notePath || row.path || "";
          const count = issueCounts?.get(path);

          return (
            <td
              key={column.field}
              className="configured-view__issue-cell configured-view__cell--numeric"
            >
              {count !== undefined && count > 0 ? (
                <button
                  type="button"
                  aria-label={`Open ${count} ${count === 1 ? "problem" : "problems"} for ${path}`}
                  onClick={() => onOpenIssues?.({ kind: "note", key: path })}
                >
                  {count}
                </button>
              ) : (
                ""
              )}
            </td>
          );
        }

        const value = fieldValue(row, column.field);

        if (column.field === "updatedAt") {
          return (
            <td key={column.field} className={className}>
              <ChangedCell value={row.updatedAt} />
            </td>
          );
        }

        // An interface's rows name their implementing type by its schema label.
        if (column.field === "resolvedType") {
          return (
            <td key={column.field} className={className} title={value}>
              {typeLabel(row.resolvedType)}
            </td>
          );
        }

        const cellContent = (
          <ConfiguredTableCellContent
            viewID={viewID}
            vaultKey={vaultKey}
            row={row}
            field={column.field}
            columnIndex={columnIndex}
            capability={capability}
            editSession={editSession}
            changedFields={changedFields}
            onStageOps={onStageOps}
            onOpenRow={columnIndex === primaryColumnIndex ? onOpenRow : undefined}
            onOpenRelation={onOpenRow}
          />
        );

        return (
          <td
            key={column.field}
            className={`${className}${capability?.semanticRole === "count" && value === "0" ? " is-zero" : ""}${categoryFields.has(column.field) ? " is-category" : ""}`}
            title={value || undefined}
          >
            {columnIndex === primaryColumnIndex && dirty ? (
              // The line keeps the dot beside the truncated title, not past it.
              <span className="configured-view__cell-line">
                {cellContent}
                <span
                  className="configured-view__staged-dot"
                  role="img"
                  aria-label="Staged changes"
                />
              </span>
            ) : (
              cellContent
            )}
            {columnIndex === primaryColumnIndex && summary && (
              <span className="configured-view__row-summary">{summary}</span>
            )}
          </td>
        );
      })}
    </tr>
  );
});

/** A row's title or plain cells open it on double-click; its other controls act on their own. */
function rowOpenTarget(target: EventTarget) {
  const control =
    target instanceof Element && target.closest("button, input, select, textarea, a, label");

  return !control || control.hasAttribute("data-row-title");
}

/**
 * Keeps keyboard focus on a moved row once it re-renders in its new place. A
 * row grouped under several values, such as a list of links, appears in each
 * group, so the copy in `groupId` wins when the row is still there.
 */
export function focusRow(rowKey: string, groupId?: string) {
  const row = `tr[data-row-key="${CSS.escape(rowKey)}"]`;
  const inGroup = groupId ? `${row}[data-group-key="${CSS.escape(groupId)}"]` : row;

  window.requestAnimationFrame(() =>
    (
      document.querySelector<HTMLElement>(inGroup) ?? document.querySelector<HTMLElement>(row)
    )?.focus(),
  );
}
