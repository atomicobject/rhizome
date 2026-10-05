import { type ComponentProps, type DragEvent, useEffect, useState } from "react";

import type { OntologyEditOp, ViewTableRow } from "../api/types";
import { configuredTableRowKey } from "./ConfiguredTableCell";
import { editOpForCell, fieldValue } from "./ConfiguredTableCellHelpers";
import { ConfiguredViewCard, focusCard } from "./ConfiguredViewCard";
import { BoardColumnHeaders, BoardMoveMenu, LaneMark } from "./ConfiguredViewBoardParts";
import { capabilityFor, fieldCapability, groupValueLabel } from "./ConfiguredViewModel";
import { BoardCardBody } from "./ConfiguredViewBrief";
import { ConfiguredViewTable } from "./ConfiguredViewTable";
import { type BoardColumn, type BoardLane, boardLanes, placeStagedCards } from "./boardLayout";
import { groupValueOp, useViewReorder } from "./useViewReorder";
import { useRememberedExpansion } from "./useRememberedExpansion";
import { isStaleRow } from "./viewProfile";

type Props = ComponentProps<typeof ConfiguredViewTable>;

type DraggedCard = { row: ViewTableRow; columnKey: string };

/** Columns holding more cards than this show compact cards. */
const COMPACT_ABOVE = 24;

export function ConfiguredViewBoard(props: Props) {
  const { execution, onCollapsedGroupCountChange, capabilities } = props;
  const board = execution.board;
  // Server columns and lanes already reflect acknowledged moves; bridge only the gap.
  const stagedSession = props.loading || props.showStagedEdits ? props.editSession : null;
  const laneCapability = fieldCapability(capabilities, board?.laneField);
  const lanes = boardLanes(execution, laneCapability?.edit?.field, stagedSession);

  const columnExpansion = useRememberedExpansion(
    props.preferenceScope,
    props.vaultKey,
    "native.boardColumns",
    board?.columnField ?? "",
  );

  const laneExpansion = useRememberedExpansion(
    props.preferenceScope,
    props.vaultKey,
    "native.boardLanes",
    board?.laneField ?? "",
  );

  const columnCollapsed = (column: BoardColumn) =>
    !columnExpansion.isExpanded(column.key, !column.collapsedByDefault);

  const [dragged, setDragged] = useState<DraggedCard | null>(null);
  const [dropCellKey, setDropCellKey] = useState<string | null>(null);
  const [menuCardKey, setMenuCardKey] = useState<string | null>(null);
  const [pendingRowKeys, setPendingRowKeys] = useState<Set<string>>(() => new Set());
  const [announcement, setAnnouncement] = useState("");
  const [moveError, setMoveError] = useState<string | null>(null);

  useEffect(() => {
    setDragged(null);
    setDropCellKey(null);
    setMenuCardKey(null);
    setMoveError(null);
  }, [execution.executionFingerprint, board?.columnField]);

  const collapsedCount = board?.columns.filter(columnCollapsed).length ?? 0;

  useEffect(() => {
    onCollapsedGroupCountChange(collapsedCount);
  }, [collapsedCount, onCollapsedGroupCountChange]);

  const columnCapability = capabilityFor(capabilities, board?.columnField ?? "");

  const placement = board
    ? placeStagedCards(
        board,
        execution.rows,
        execution.state.sort ?? [],
        columnCapability?.edit?.field,
        stagedSession,
      )
    : null;

  const titleFor = (row: ViewTableRow) =>
    fieldValue(row, execution.card?.title.field ?? "title") || row.title || "Untitled";

  const reorder = useViewReorder({
    execution,
    rows: props.rows,
    capabilities,
    onStageOps: props.onStageOps,
    groups: (board?.columns ?? []).map((column) => ({
      id: column.key,
      label: groupValueLabel(columnCapability, column.value, column.label),
      path: [{ field: board?.columnField ?? "", value: column.value }],
      rows: placement?.rows.get(column.key) ?? [],
    })),
    showStagedEdits: props.loading || props.showStagedEdits,
    titleFor,
    axis: "vertical",
    // Columns change through the Move menu; Alt+Arrow keys reorder within one.
    keyboardCrossesGroups: false,
  });

  if (!board || !placement) {
    return (
      <div className="configured-view__status configured-view__board-warning">
        Board layout is unavailable for this view.
      </div>
    );
  }

  const visibleRowKeys = new Set(props.rows.map(configuredTableRowKey));
  // Server totals describe the unfiltered set, so a local row filter shows its own counts.
  const locallyFiltered = props.rows.length !== execution.rows.length;
  const columnField = board.columnField;
  const canMove = Boolean(board.editable && props.onStageOps && columnCapability?.edit);
  const profile = execution.profile;
  const view = execution.view;
  // An authored card layout wins over the profile's; generated boards use the profile.
  const profileCards = profile && (view.generated || !view.variants.kanban?.card);
  const hiddenFields = [columnField, lanes?.field ?? ""];
  const now = Date.now();

  const labelFor = (column: BoardColumn) =>
    groupValueLabel(columnCapability, column.value, column.label);

  const columns = board.columns.map((column) => {
    const rows = (reorder.groups.find((group) => group.id === column.key)?.rows ?? []).filter(
      (row) => visibleRowKeys.has(configuredTableRowKey(row)),
    );

    const count = locallyFiltered
      ? rows.length
      : column.count + (placement.countDelta.get(column.key) ?? 0);

    const collapsed = columnCollapsed(column);

    return {
      column,
      rows,
      count,
      collapsed,
      // An uncollapsed value with no records still takes drops, as a narrow strip.
      strip: collapsed || count === 0,
      stale: profile ? rows.filter((row) => isStaleRow(row, profile, capabilities, now)).length : 0,
      compact: rows.length > COMPACT_ABOVE,
      label: labelFor(column),
    };
  });

  const inLane = (rowKey: string, lane: BoardLane | null) =>
    !lane || Boolean(lanes?.membership.get(rowKey)?.includes(lane.key));

  const columnOp = (row: ViewTableRow, target: BoardColumn) =>
    canMove && columnCapability?.edit
      ? editOpForCell(
          row,
          columnField,
          columnCapability.edit.field,
          columnCapability.edit.operation,
          target.value,
          false,
        )
      : null;

  /**
   * What a drop in one cell stages: the column change, and the lane value
   * for another lane. A card leaves its own lanes only when the lane field
   * takes a single value the board can set; otherwise the cell refuses it.
   */
  function dropOps(current: DraggedCard, column: BoardColumn, lane: BoardLane | null) {
    const ops: OntologyEditOp[] = [];

    if (current.columnKey !== column.key) {
      const op = columnOp(current.row, column);

      if (!op) return null;
      ops.push(op);
    }

    if (lane && !inLane(configuredTableRowKey(current.row), lane)) {
      const op = groupValueOp(current.row, laneCapability?.key ?? "", laneCapability, lane.value);

      if (!op) return null;
      ops.push(op);
    }

    return ops.length > 0 ? ops : null;
  }

  async function stage(row: ViewTableRow, ops: OntologyEditOp[], where: string) {
    if (!props.onStageOps) return;
    const rowKey = configuredTableRowKey(row);
    const title = titleFor(row);
    setPendingRowKeys((current) => new Set(current).add(rowKey));
    setMoveError(null);

    try {
      await Promise.resolve(props.onStageOps(ops));
      setAnnouncement(`Moved ${title} to ${where}, staged`);
    } catch (error) {
      // Say why instead of leaving a silent rejection.
      const reason = error instanceof Error && error.message ? `: ${error.message}` : "";
      setMoveError(`Could not move ${title} to ${where}${reason}`);
    } finally {
      setPendingRowKeys((current) => {
        const next = new Set(current);
        next.delete(rowKey);

        return next;
      });
    }
  }

  function toggleColumn(key: string) {
    const column = board?.columns.find((item) => item.key === key);

    if (column) columnExpansion.toggle(key, !column.collapsedByDefault);
  }

  /** Drop handlers for one cell: a column, or a column within a lane. */
  function dropProps(column: BoardColumn, lane: BoardLane | null, cellKey: string) {
    const accepts = () => dragged !== null && dropOps(dragged, column, lane) !== null;

    return {
      onDragEnter(event: DragEvent<HTMLElement>) {
        if (!accepts()) return;
        event.preventDefault();
        setDropCellKey(cellKey);
      },
      onDragOver(event: DragEvent<HTMLElement>) {
        if (!accepts()) return;
        event.preventDefault();
        event.dataTransfer.dropEffect = "move";
      },
      onDragLeave(event: DragEvent<HTMLElement>) {
        const related = event.relatedTarget;

        if (!(related instanceof Node) || !event.currentTarget.contains(related)) {
          setDropCellKey((current) => (current === cellKey ? null : current));
        }
      },
      onDrop(event: DragEvent<HTMLElement>) {
        event.preventDefault();
        const current = dragged;
        setDragged(null);
        setDropCellKey(null);
        // A drop back in the card's own cell leaves it where it is.
        const ops = current && dropOps(current, column, lane);

        if (!current || !ops) return;
        const rowKey = configuredTableRowKey(current.row);

        // An orderable board places the card in the top slot of its own lane's column.
        const placed =
          reorder.enabled && inLane(rowKey, lane) ? reorder.dropAtStart(column.key, rowKey) : null;

        const where = lane ? `${labelFor(column)}, ${lane.label} lane` : labelFor(column);

        if (!placed) void stage(current.row, ops, where);
      },
    };
  }

  // ponytail: fixed 480px cap keeps a lone column readable; make it a token if layouts need it.
  const template = columns.map((item) => (item.strip ? "34px" : "minmax(240px, 480px)")).join(" ");
  const laneList: Array<BoardLane | null> = lanes ? lanes.lanes : [null];

  function renderCard(row: ViewTableRow, item: (typeof columns)[number], lane: BoardLane | null) {
    const rowKey = configuredTableRowKey(row);
    const cardKey = lane ? `${lane.key}\u0000${rowKey}` : rowKey;
    const busy = pendingRowKeys.has(rowKey) || reorder.pendingKeys.has(rowKey);
    const source = reorder.dragSource(row);
    const { column } = item;
    const memberships = lanes?.membership.get(rowKey)?.length ?? 0;

    return (
      <ConfiguredViewCard
        key={cardKey}
        {...props}
        row={row}
        layout={execution.card}
        mode="board"
        omitField={columnField}
        busy={busy}
        compact={item.compact}
        body={
          profileCards && profile ? (
            <BoardCardBody
              row={row}
              profile={profile}
              capabilities={capabilities}
              hiddenFields={hiddenFields}
              otherLanes={Math.max(0, memberships - 1)}
              stale={isStaleRow(row, profile, capabilities, now)}
              compact={item.compact}
              showType={Boolean(view.source.interface)}
              onOpenRow={props.onOpenRow}
            />
          ) : undefined
        }
        draggable={canMove || source.draggable}
        dragging={dragged !== null && configuredTableRowKey(dragged.row) === rowKey}
        onMoveShortcut={canMove ? () => setMenuCardKey(cardKey) : undefined}
        onDragStart={(event: DragEvent<HTMLElement>) => {
          event.dataTransfer.effectAllowed = "move";
          event.dataTransfer.setData("text/plain", rowKey);

          if (source.draggable) source.onDragStart(event);

          if (canMove) setDragged({ row, columnKey: column.key });
        }}
        onDragEnd={() => {
          source.onDragEnd();
          setDragged(null);
          setDropCellKey(null);
        }}
        // Reordering stays within the dragged card's own lanes.
        dropTarget={
          reorder.dragKey && inLane(reorder.dragKey, lane)
            ? reorder.dropTarget(column.key, row)
            : undefined
        }
        className={reorder.dropClass(column.key, row)}
        onReorder={
          reorder.enabled
            ? (delta) => void reorder.moveByKeyboard(row, delta).then(() => focusCard(rowKey))
            : undefined
        }
        actions={
          canMove ? (
            <BoardMoveMenu
              title={titleFor(row)}
              open={menuCardKey === cardKey}
              busy={busy}
              targets={columns
                .filter((candidate) => candidate.column.key !== column.key)
                .map((candidate) => ({ key: candidate.column.key, label: candidate.label }))}
              onOpenChange={(open) => setMenuCardKey(open ? cardKey : null)}
              onChoose={(key) => {
                const target = board?.columns.find((candidate) => candidate.key === key);
                const op = target && columnOp(row, target);

                return target && op ? stage(row, [op], labelFor(target)) : Promise.resolve();
              }}
            />
          ) : undefined
        }
      />
    );
  }

  function renderCell(item: (typeof columns)[number], lane: BoardLane | null) {
    const { column, label } = item;
    const cellKey = lane ? `${lane.key}\u0000${column.key}` : column.key;
    const cellLabel = lane ? `${label} column, ${lane.label} lane` : `${label} column`;
    const isDropTarget = dropCellKey === cellKey;

    const rows = lane
      ? item.rows.filter((row) =>
          lanes?.membership.get(configuredTableRowKey(row))?.includes(lane.key),
        )
      : item.rows;

    if (item.strip) {
      return (
        <div
          key={cellKey}
          className={`configured-view__board-strip-cell${item.collapsed ? "" : " is-empty"}${isDropTarget ? " is-drop-target" : ""}`}
          aria-label={cellLabel}
          {...dropProps(column, lane, cellKey)}
        >
          <span className="configured-view__board-strip-label" aria-hidden>
            {label}
          </span>
        </div>
      );
    }

    const moreCount = lane ? 0 : Math.max(0, item.count - rows.length);

    return (
      <section
        key={cellKey}
        className={`configured-view__board-column${isDropTarget ? " is-drop-target" : ""}`}
        aria-label={cellLabel}
        {...dropProps(column, lane, cellKey)}
      >
        <div className="configured-view__board-stack">
          {isDropTarget && <div className="configured-view__board-drop-slot" aria-hidden />}
          {rows.length === 0 && !isDropTarget && !lane && (
            <p className="configured-view__board-empty">No items</p>
          )}
          {rows.map((row) => renderCard(row, item, lane))}
          {moreCount > 0 && <p className="configured-view__board-more">{moreCount} more</p>}
        </div>
      </section>
    );
  }

  return (
    <div className="configured-view__board" aria-label="Board">
      {props.loading && (
        <div className="configured-view__loading-overlay" aria-live="polite">
          Refreshing view…
        </div>
      )}
      {(moveError || reorder.error) && (
        <div className="configured-view__status configured-view__status--error" role="alert">
          {moveError || reorder.error}
        </div>
      )}
      <div className="configured-view__board-scroll">
        <div className="configured-view__board-columns" style={{ gridTemplateColumns: template }}>
          <BoardColumnHeaders
            items={columns}
            capability={columnCapability}
            onToggle={toggleColumn}
          />
        </div>
        {laneList.map((lane) => {
          const collapsed = lane ? !laneExpansion.isExpanded(lane.key, true) : false;

          return (
            <section
              key={lane?.key ?? "all"}
              className="configured-view__board-lane"
              aria-label={lane ? `${lane.label} lane` : undefined}
            >
              {lane && (
                <h3 className="configured-view__board-lane-header">
                  <button
                    type="button"
                    aria-expanded={!collapsed}
                    onClick={() => laneExpansion.toggle(lane.key, true)}
                  >
                    <span className="configured-view__card-section-caret" aria-hidden />
                    <LaneMark lane={lane} />
                    <span className="configured-view__board-count">
                      {locallyFiltered
                        ? props.rows.filter((row) =>
                            lanes?.membership.get(configuredTableRowKey(row))?.includes(lane.key),
                          ).length
                        : lane.count}
                    </span>
                  </button>
                </h3>
              )}
              {!collapsed && (
                <div
                  className="configured-view__board-cells"
                  style={{ gridTemplateColumns: template }}
                >
                  {columns.map((item) => renderCell(item, lane))}
                </div>
              )}
            </section>
          );
        })}
      </div>
      <div className="sr-only" aria-live="polite">
        {announcement}
      </div>
    </div>
  );
}
