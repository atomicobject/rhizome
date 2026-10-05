import { type CSSProperties, Fragment } from "react";

import type {
  OntologyEditOp,
  OntologyEditSessionResponse,
  ValidationScope,
  ViewFieldCapability,
  ViewTableColumn,
  ViewTableRow,
} from "../api/types";
import type { OpenMode } from "./useNoteTabs";
import {
  ConfiguredTableCellContent,
  configuredTableRowHasChanges,
  configuredTableRowKey,
} from "./ConfiguredTableCell";
import { fieldValue } from "./ConfiguredTableCellHelpers";
import { ChangedCell } from "./ConfiguredTableCellReadOnly";
import {
  capabilityFor,
  GroupValueMark,
  groupValueLabel,
  isIssueColumn,
} from "./ConfiguredViewModel";
import {
  type ConfiguredTableGroup,
  columnClassName,
  groupTone,
  primaryColumn,
} from "./ConfiguredViewTableModel";
import { moveTableFocus } from "./focusManagement";
import { reorderDelta, type ViewReorder } from "./useViewReorder";
import { useTypeLabel } from "./typeLabels";

export type TableRowProps = {
  columns: ViewTableColumn[];
  capabilities: ViewFieldCapability[];
  viewID: string;
  editSession?: OntologyEditSessionResponse | null;
  vaultKey?: string | null;
  onStageOps?: (ops: OntologyEditOp[]) => Promise<void> | undefined;
  onOpenRow: (row: ViewTableRow, mode?: OpenMode) => void;
  issueCounts?: ReadonlyMap<string, number>;
  onOpenIssues?: (scope: ValidationScope) => void;
  selectedRowKey: string | null;
  tabbableRowKey: string | undefined;
  onSelectRow: (key: string) => void;
  reorder: ViewReorder;
  checked: ReadonlySet<string>;
  onToggleChecked: (key: string, range?: boolean) => void;
  onClearSelection: () => void;
  /** Set when rows show this field as a clamped second line under the title. */
  summaryField?: string;
  /** Enum fields without lifecycle stages; their values read as plain labels. */
  categoryFields: ReadonlySet<string>;
};

export function ConfiguredViewTableGroup({
  group,
  rowsForGroup,
  columnSpan,
  rowProps,
  isExpanded,
  onToggle,
}: {
  group: ConfiguredTableGroup;
  rowsForGroup: (group: ConfiguredTableGroup) => ViewTableRow[];
  columnSpan: number;
  rowProps: TableRowProps;
  isExpanded: (group: ConfiguredTableGroup) => boolean;
  onToggle: (group: ConfiguredTableGroup) => void;
}) {
  const expanded = isExpanded(group);

  const capability = capabilityFor(rowProps.capabilities, group.field);
  const label = groupValueLabel(capability, group.value, group.label);

  const leaf = group.children.length === 0;
  const { reorder } = rowProps;
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
            />
          ))
        : null}
    </Fragment>
  );
}

export function ConfiguredViewTableRow({
  row,
  columns,
  capabilities,
  viewID,
  editSession,
  vaultKey,
  onStageOps,
  onOpenRow,
  issueCounts,
  onOpenIssues,
  selectedRowKey,
  tabbableRowKey,
  onSelectRow,
  reorder,
  checked,
  onToggleChecked,
  onClearSelection,
  summaryField,
  categoryFields,
  groupId,
}: TableRowProps & { row: ViewTableRow; groupId: string }) {
  const rowKey = configuredTableRowKey(row);
  const dirty = configuredTableRowHasChanges(row, editSession);
  const primaryColumnIndex = primaryColumn(columns, capabilities);
  const dragging = reorder.dragKey === rowKey;
  const source = reorder.dragSource(row);
  const showHandle = reorder.enabled || reorder.blockedReason !== "unsupported";

  const handle = showHandle ? (
    <span
      className={`configured-view__drag-handle${reorder.enabled ? "" : " is-disabled"}`}
      aria-hidden="true"
      title={
        reorder.enabled
          ? "Drag to reorder, or press Alt+Up or Alt+Down"
          : (reorder.blockedReason ?? "")
      }
      {...(reorder.enabled ? source : {})}
      onDragStart={(event) => {
        // Drag the whole row's image, not the small handle.
        const rowElement = event.currentTarget.closest("tr");

        if (rowElement) event.dataTransfer.setDragImage(rowElement, 12, 14);
        source.onDragStart(event);
      }}
    />
  ) : null;

  const typeLabel = useTypeLabel();
  const isChecked = checked.has(rowKey);
  const summary = summaryField ? fieldValue(row, summaryField) : "";

  return (
    <tr
      className={`configured-view__row${selectedRowKey === rowKey ? " is-selected" : ""}${isChecked ? " is-checked" : ""}${dirty ? " is-dirty" : ""}${dragging ? " is-dragging" : ""}${reorder.dropClass(groupId, row)}`}
      data-row-key={rowKey}
      data-group-key={groupId}
      tabIndex={rowKey === tabbableRowKey ? 0 : -1}
      aria-keyshortcuts={`Enter ArrowUp ArrowDown ArrowRight X Escape${reorder.enabled ? " Alt+ArrowUp Alt+ArrowDown" : ""}`}
      aria-busy={reorder.pendingKeys.has(rowKey) || undefined}
      {...(reorder.dragKey ? reorder.dropTarget(groupId, row) : {})}
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

        if (delta !== null && reorder.enabled) {
          event.preventDefault();
          void reorder.moveByKeyboard(row, delta, groupId).then(() => focusRow(rowKey, groupId));

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
      {columns.map((column, columnIndex) => {
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

        const capability = capabilityFor(capabilities, column.field);
        const value = fieldValue(row, column.field);

        if (column.field === "updatedAt") {
          return (
            <td key={column.field} className={columnClassName(column, capabilities)}>
              <ChangedCell value={row.updatedAt} />
            </td>
          );
        }

        // An interface's rows name their implementing type by its schema label.
        if (column.field === "resolvedType") {
          return (
            <td key={column.field} className={columnClassName(column, capabilities)} title={value}>
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
            onStageOps={onStageOps}
            onOpenRow={columnIndex === primaryColumnIndex ? onOpenRow : undefined}
            onOpenRelation={onOpenRow}
          />
        );

        return (
          <td
            key={column.field}
            className={`${columnClassName(column, capabilities)}${capability?.semanticRole === "count" && value === "0" ? " is-zero" : ""}${categoryFields.has(column.field) ? " is-category" : ""}`}
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
}

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
