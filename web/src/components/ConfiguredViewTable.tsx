import { type CSSProperties, useCallback, useEffect, useMemo, useRef, useState } from "react";

import type {
  OntologyEditOp,
  OntologyEditSessionResponse,
  TypeProfile,
  ValidationScope,
  ViewExecuteRequest,
  ViewExecuteResponse,
  ViewFieldCapability,
  ViewTableColumn,
  ViewTableRow,
} from "../api/types";
import type { ViewPreferenceScope } from "../viewPreferences/types";
import type { OpenMode } from "./useNoteTabs";
import { configuredTableRowKey } from "./ConfiguredTableCell";
import { fieldValue, stagedTargetForRow } from "./ConfiguredTableCellHelpers";
import { ConfiguredViewBulkBar } from "./ConfiguredViewBulkBar";
import { ConfiguredViewRecord } from "./ConfiguredViewRecord";
import { ConfiguredViewTableHeader } from "./ConfiguredViewTableHeader";
import {
  type ConfiguredTableGroup,
  columnClassName,
  countCollapsedGroups,
  effectiveSort,
  FIXED_WIDTH_CELL,
  leafReorderGroups,
  normalizeTableGroups,
  primaryColumn,
  renderedGroupRows,
  UNGROUPED,
} from "./ConfiguredViewTableModel";
import {
  ConfiguredViewTableGroup,
  ConfiguredViewTableRow,
  focusRow,
  type TableRowProps,
  type TableRowState,
} from "./ConfiguredViewTableRows";
import { capabilityFor } from "./ConfiguredViewModel";
import { changedFields, changedFieldsByKey } from "../staging/stagedState";
import { groupIdentity } from "./ConfiguredViewSave";
import { RecordRailPortal } from "./recordRail";
import { useRememberedExpansion } from "./useRememberedExpansion";
import { useViewReorder } from "./useViewReorder";
import { useRowSelection } from "./useRowSelection";
import { useLoadOnScroll } from "./useScrollLoading";

type Props = {
  execution: ViewExecuteResponse;
  rows: ViewTableRow[];
  columns: ViewTableColumn[];
  capabilities: ViewFieldCapability[];
  state: ViewExecuteRequest;
  viewID: string;
  loading: boolean;
  /** Renderers show staged values and placements while this is true. */
  showStagedEdits?: boolean;
  editSession?: OntologyEditSessionResponse | null;
  vaultKey?: string | null;
  onStageOps?: (ops: OntologyEditOp[]) => Promise<void> | undefined;
  onOpenRow: (row: ViewTableRow, mode?: OpenMode) => void;
  issueCounts?: ReadonlyMap<string, number>;
  onOpenIssues?: (scope: ValidationScope) => void;
  /** Shift-activation adds the column as another sort key. */
  onSort: (column: ViewTableColumn, additive?: boolean) => void;
  onCollapsedGroupCountChange: (count: number) => void;
  profile?: TypeProfile;
  density?: "two-line" | "one-line";
  /** Loads the next rows; set only while more remain and nothing is loading. */
  onLoadMore?: () => void;
  columnWidths?: Record<string, number>;
  onColumnWidthsChange?: (widths: Record<string, number>) => void;
  /** Only the active view shows its selected record in the workspace rail. */
  active?: boolean;
  /** The view instance whose expansion choices this layout remembers. */
  preferenceScope?: ViewPreferenceScope | null;
};

export type { ConfiguredTableGroup };

export function ConfiguredViewTable({
  execution,
  rows,
  columns,
  capabilities,
  state,
  viewID,
  loading,
  showStagedEdits,
  editSession,
  vaultKey,
  onStageOps,
  onOpenRow,
  issueCounts,
  onOpenIssues,
  onSort,
  onCollapsedGroupCountChange,
  profile,
  density = "one-line",
  onLoadMore,
  columnWidths,
  onColumnWidthsChange,
  active = true,
  preferenceScope,
}: Props) {
  const groups = useMemo(
    () => normalizeTableGroups(execution.groups ?? [], rows),
    [execution.groups, rows],
  );

  const reorderGroups = useMemo(
    () => leafReorderGroups(groups, rows, capabilities),
    [capabilities, groups, rows],
  );

  const reorder = useViewReorder({
    execution,
    rows,
    capabilities,
    onStageOps,
    groups: reorderGroups,
    showStagedEdits,
    titleFor: (row) =>
      fieldValue(row, columns[primaryColumn(columns, capabilities)]?.field ?? "title"),
    axis: "vertical",
  });

  const rowsForGroup = (group: ConfiguredTableGroup) =>
    reorder.groups.find((candidate) => candidate.id === group.key)?.rows ??
    rows.slice(group.rowStart, group.rowEnd);

  const expansion = useRememberedExpansion(
    preferenceScope,
    vaultKey,
    "native.tableGroups",
    groupIdentity(execution.state.group),
  );

  const [selectedRowKey, setSelectedRowKey] = useState<string | null>(null);
  const primaryColumnIndex = primaryColumn(columns, capabilities);

  // A title width only helps against other prose columns; beside fixed-width
  // columns alone it would make the table spread the slack across them.
  const widenPrimary =
    columns.filter((column) => !FIXED_WIDTH_CELL.test(columnClassName(column, capabilities)))
      .length > 1;

  // Checks, ranges, and select-all cover the rows on screen in their rendered
  // order, so rows in collapsed groups are never checked. The focused record
  // and checked rows survive re-execution, such as after staging an edit.
  const renderedRows =
    groups.length > 0
      ? renderedGroupRows(groups, rowsForGroup, groupIsExpanded)
      : (reorder.groups[0]?.rows ?? rows);

  const orderedKeys = renderedRows.map(configuredTableRowKey);
  const selection = useRowSelection(orderedKeys);
  const { clear: clearSelection } = selection;

  useEffect(() => {
    setSelectedRowKey(null);
    clearSelection();
  }, [clearSelection, viewID]);

  const checkedRows = renderedRows.filter((row, index) => {
    const key = orderedKeys[index];

    // A row grouped by a list field repeats; count it once.
    return selection.checked.has(key) && orderedKeys.indexOf(key) === index;
  });

  const selectedRow = rows.find((row) => configuredTableRowKey(row) === selectedRowKey);
  const summaryField = density === "two-line" ? profile?.summaryField : undefined;
  const scroll = useLoadOnScroll(onLoadMore);

  const collapsedGroupCount = countCollapsedGroups(groups, expansion.choices);
  useEffect(() => {
    onCollapsedGroupCountChange(collapsedGroupCount);
  }, [collapsedGroupCount, onCollapsedGroupCountChange]);

  function groupIsExpanded(group: ConfiguredTableGroup) {
    return expansion.isExpanded(group.key, !group.collapsedByDefault);
  }

  function toggleGroup(group: ConfiguredTableGroup) {
    expansion.toggle(group.key, !group.collapsedByDefault);
  }

  // Rows are one Tab stop: the selected row, else the first shown.
  const tabbableRowKey =
    selectedRowKey && orderedKeys.includes(selectedRowKey) ? selectedRowKey : orderedKeys[0];

  // Rows read the latest reorder state when an event fires (see TableRowProps).
  const reorderRef = useRef(reorder);
  reorderRef.current = reorder;

  const checkedCount = useRef(0);
  checkedCount.current = selection.checked.size;

  const clearRowSelection = useCallback(() => {
    if (checkedCount.current > 0) clearSelection();
    else setSelectedRowKey(null);
  }, [clearSelection]);

  const categoryFields = useMemo(
    () =>
      new Set(
        (profile?.categoryFields ?? []).flatMap((field) => [
          field,
          ...capabilities.flatMap((capability) =>
            capability.canonicalField === field ? [capability.key] : [],
          ),
        ]),
      ),
    [capabilities, profile?.categoryFields],
  );

  const columnCells = useMemo(
    () =>
      columns.map((column) => ({
        column,
        capability: capabilityFor(capabilities, column.field),
        className: columnClassName(column, capabilities),
      })),
    [capabilities, columns],
  );

  // Staged fields for every row, once per session revision instead of per cell.
  const changedByRow = useMemo(
    () =>
      changedFieldsByKey(
        editSession,
        rows.map((row) => [configuredTableRowKey(row), stagedTargetForRow(row)] as const),
      ),
    [editSession, rows],
  );

  const dragActive = reorder.dragKey !== null;

  const rowProps = useMemo<TableRowProps>(
    () => ({
      columnCells,
      primaryColumnIndex,
      capabilities,
      viewID,
      editSession,
      vaultKey,
      onStageOps,
      onOpenRow,
      issueCounts,
      onOpenIssues,
      onSelectRow: setSelectedRowKey,
      reorderRef,
      reorderEnabled: reorder.enabled,
      reorderBlockedReason: reorder.blockedReason,
      dragActive,
      onToggleChecked: selection.toggle,
      onClearSelection: clearRowSelection,
      summaryField,
      categoryFields,
    }),
    [
      capabilities,
      categoryFields,
      clearRowSelection,
      columnCells,
      dragActive,
      editSession,
      issueCounts,
      onOpenIssues,
      onOpenRow,
      onStageOps,
      primaryColumnIndex,
      reorder.blockedReason,
      reorder.enabled,
      selection.toggle,
      summaryField,
      vaultKey,
      viewID,
    ],
  );

  const rowState = (row: ViewTableRow, groupId: string): TableRowState => {
    const key = configuredTableRowKey(row);

    return {
      selected: selectedRowKey === key,
      tabbable: tabbableRowKey === key,
      checked: selection.checked.has(key),
      dragging: reorder.dragKey === key,
      pending: reorder.pendingKeys.has(key),
      dropClass: reorder.dropClass(groupId, row),
      changedFields: changedByRow.get(key) ?? changedFields(editSession, stagedTargetForRow(row)),
    };
  };

  const columnSpan = columns.length + 1;
  const allChecked = orderedKeys.length > 0 && checkedRows.length === new Set(orderedKeys).size;

  return (
    <div
      ref={scroll.ref}
      className={`configured-view__table-wrap${selection.checked.size > 0 ? " has-checked" : ""}`}
      onScroll={scroll.onScroll}
    >
      {loading && (
        <div className="configured-view__loading-overlay" aria-live="polite">
          <span>Refreshing view…</span>
        </div>
      )}
      {reorder.error && (
        <div className="configured-view__status configured-view__status--error" role="alert">
          {reorder.error}
        </div>
      )}
      <table
        className={`configured-view__table${summaryField ? " configured-view__table--two-line" : ""}`}
        // SAFETY: React forwards this numeric custom property unchanged.
        style={{ "--column-count": columns.length } as CSSProperties}
      >
        <ConfiguredViewTableHeader
          columns={columns}
          capabilities={capabilities}
          sort={effectiveSort(state, execution.state)}
          issueCounts={issueCounts}
          onSort={onSort}
          primaryColumnIndex={widenPrimary ? primaryColumnIndex : null}
          columnWidths={columnWidths}
          onColumnWidthsChange={onColumnWidthsChange}
          allChecked={allChecked}
          onToggleAll={() => (allChecked ? selection.clear() : selection.setAll(orderedKeys))}
        />
        <tbody>
          {groups.length > 0
            ? groups.map((group) => (
                <ConfiguredViewTableGroup
                  key={group.key}
                  group={group}
                  rowsForGroup={rowsForGroup}
                  columnSpan={columnSpan}
                  rowProps={rowProps}
                  rowState={rowState}
                  isExpanded={groupIsExpanded}
                  onToggle={toggleGroup}
                />
              ))
            : (reorder.groups[0]?.rows ?? rows).map((row) => (
                <ConfiguredViewTableRow
                  key={configuredTableRowKey(row)}
                  row={row}
                  groupId={UNGROUPED}
                  {...rowProps}
                  {...rowState(row, UNGROUPED)}
                />
              ))}
        </tbody>
      </table>
      <div className="sr-only" aria-live="polite">
        {reorder.announcement}
      </div>
      {checkedRows.length > 0 && onStageOps && (
        <ConfiguredViewBulkBar
          rows={checkedRows}
          profile={profile}
          capabilities={capabilities}
          viewID={viewID}
          editSession={editSession}
          onStageOps={onStageOps}
          onClear={() => {
            selection.clear();

            // The bar goes away with its focused control; focus returns to the rows.
            if (tabbableRowKey) focusRow(tabbableRowKey);
          }}
        />
      )}
      {/* The rail keeps the record's place while the table shows, so selecting a
          row never reflows the table under the pointer mid double-click. */}
      <RecordRailPortal show={active}>
        {selectedRow ? (
          <ConfiguredViewRecord
            row={selectedRow}
            profile={profile}
            capabilities={capabilities}
            source={execution.view.source}
            viewID={viewID}
            vaultKey={vaultKey}
            editSession={editSession}
            onStageOps={onStageOps}
            onOpenRow={onOpenRow}
          />
        ) : (
          <p className="view-record view-record__empty">Select a row to see its record here.</p>
        )}
      </RecordRailPortal>
    </div>
  );
}
