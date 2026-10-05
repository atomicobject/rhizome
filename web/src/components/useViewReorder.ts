import { type DragEvent, useEffect, useMemo, useState } from "react";

import type {
  OntologyEditOp,
  ViewExecuteResponse,
  ViewFieldCapability,
  ViewTableRow,
} from "../api/types";
import { configuredTableRowKey } from "./ConfiguredTableCell";
import { editOpForCell, rawFieldValue, stringifyCell } from "./ConfiguredTableCellHelpers";
import { capabilityFor } from "./ConfiguredViewModel";
import {
  type DropSide,
  orderableKeys,
  orderingBlockedReason,
  placementOps,
  sortRowsByKeys,
} from "./viewOrdering";

/** One group a drop can land in: a table or card group, or a board column. */
export type ReorderGroup = {
  id: string;
  label: string;
  /** Group field values from the outermost group in; empty when ungrouped. */
  path: Array<{ field: string; value: string }>;
  rows: ViewTableRow[];
};

type Drop = { groupId: string; rowKey: string | null; side: DropSide };

type Options = {
  execution: ViewExecuteResponse;
  rows: ViewTableRow[];
  capabilities: ViewFieldCapability[];
  onStageOps?: (ops: OntologyEditOp[]) => Promise<void> | undefined;
  /** Groups in display order, with rows in display order. */
  groups: ReorderGroup[];
  /** Show staged values before the server re-executes: regroup moved rows, re-sort each group. */
  showStagedEdits?: boolean;
  titleFor: (row: ViewTableRow) => string;
  /** Which half of a target decides before or after: top and bottom, or left and right. */
  axis: "vertical" | "horizontal";
  /** Whether Alt+Arrow at a group's edge moves the item into the neighboring group. */
  keyboardCrossesGroups?: boolean;
};

const DRAG_TYPE = "application/x-rhizome-view-row";

/**
 * Drag and keyboard reordering for configured views (SPEC-0108). A drop
 * stages the field values that place the item there under the view's sort.
 */
export function useViewReorder({
  execution,
  rows,
  capabilities,
  onStageOps,
  groups,
  showStagedEdits,
  titleFor,
  axis,
  keyboardCrossesGroups = true,
}: Options) {
  const keys = useMemo(
    () => orderableKeys(execution.state.sort, capabilities),
    [capabilities, execution.state.sort],
  );

  const blockedReason = orderingBlockedReason(execution, rows, keys, Boolean(onStageOps));
  const enabled = blockedReason === null;
  const [dragKey, setDragKey] = useState<string | null>(null);
  const [drop, setDrop] = useState<Drop | null>(null);
  const [pendingKeys, setPendingKeys] = useState<Set<string>>(() => new Set());
  const [error, setError] = useState<string | null>(null);
  const [announcement, setAnnouncement] = useState("");

  useEffect(() => {
    setDragKey(null);
    setDrop(null);
  }, [execution.executionFingerprint]);

  const displayGroups = useMemo(
    () =>
      enabled && showStagedEdits
        ? regroupStagedRows(groups).map((group) => ({
            ...group,
            rows: sortRowsByKeys(group.rows, keys),
          }))
        : groups,
    [enabled, groups, keys, showStagedEdits],
  );

  const inGroup = (group: ReorderGroup, rowKey: string) =>
    group.rows.some((candidate) => configuredTableRowKey(candidate) === rowKey);

  /** Ops that move `row` into `target`'s group beside the target row; null when not allowed. */
  function opsFor(
    row: ViewTableRow,
    targetGroupId: string,
    target: ViewTableRow | null,
    side: DropSide,
  ) {
    const rowKey = configuredTableRowKey(row);
    const destination = displayGroups.find((group) => group.id === targetGroupId);

    // A row grouped under several values, such as a list of links, appears in
    // each of their groups; a drop in any of them is a move within it.
    const source =
      destination && inGroup(destination, rowKey)
        ? destination
        : displayGroups.find((group) => inGroup(group, rowKey));

    if (!source || !destination) return null;

    const groupOps =
      source.id === destination.id ? [] : groupMoveOps(row, source, destination, capabilities);

    if (!groupOps) return null;

    const ops = [
      ...groupOps,
      ...placementOps({ keys, moving: row, groupRows: destination.rows, target, side }),
    ];

    // A group field that is also a sort key yields the same op twice; keep one per id.
    return [...new Map(ops.map((op) => [op.id, op])).values()];
  }

  async function stage(row: ViewTableRow, ops: OntologyEditOp[], groupId: string) {
    if (!onStageOps || ops.length === 0) return;
    const rowKey = configuredTableRowKey(row);
    const title = titleFor(row);
    const group = displayGroups.find((candidate) => candidate.id === groupId);
    setPendingKeys((current) => new Set(current).add(rowKey));
    setError(null);

    try {
      await Promise.resolve(onStageOps(ops));
      const where = group?.label ? ` in ${group.label}` : "";
      setAnnouncement(`Moved ${title}${where}, staged`);
    } catch (caught) {
      const reason = caught instanceof Error && caught.message ? `: ${caught.message}` : "";
      setError(`Could not move ${title}${reason}`);
    } finally {
      setPendingKeys((current) => {
        const next = new Set(current);
        next.delete(rowKey);

        return next;
      });
    }
  }

  function findRow(rowKey: string) {
    return rows.find((candidate) => configuredTableRowKey(candidate) === rowKey) ?? null;
  }

  /** Ops for a drop of `movingKey` beside `targetKey`; null when the item cannot go there. */
  function opsAt(groupId: string, targetKey: string | null, side: DropSide, movingKey: string) {
    const moving = findRow(movingKey);
    const target = targetKey ? findRow(targetKey) : null;

    return moving ? opsFor(moving, groupId, target, side) : null;
  }

  /** Stages a drop; null when it would stage nothing, so the caller can do something else. */
  function dropAt(groupId: string, targetKey: string | null, side: DropSide, movingKey: string) {
    const moving = findRow(movingKey);
    const ops = opsAt(groupId, targetKey, side, movingKey);

    return moving && ops && ops.length > 0 ? stage(moving, ops, groupId) : null;
  }

  function sideFor(event: DragEvent<HTMLElement>): DropSide {
    const rect = event.currentTarget.getBoundingClientRect();

    return axis === "horizontal"
      ? event.clientX < rect.left + rect.width / 2
        ? "before"
        : "after"
      : event.clientY < rect.top + rect.height / 2
        ? "before"
        : "after";
  }

  return {
    enabled,
    blockedReason,
    groups: displayGroups,
    dragKey,
    pendingKeys,
    error,
    announcement,

    /** Props for the element a drag starts from. */
    dragSource(row: ViewTableRow) {
      const rowKey = configuredTableRowKey(row);

      return {
        draggable: enabled && !pendingKeys.has(rowKey),
        onDragStart(event: DragEvent<HTMLElement>) {
          event.dataTransfer.effectAllowed = "move";
          event.dataTransfer.setData(DRAG_TYPE, rowKey);
          event.dataTransfer.setData("text/plain", titleFor(row));
          setDragKey(rowKey);
          setError(null);
        },
        onDragEnd() {
          setDragKey(null);
          setDrop(null);
        },
      };
    },

    /**
     * Props for a row or card that accepts drops beside it; a null row means
     * the group's end. A fixed side ignores which half the pointer is over.
     */
    dropTarget(groupId: string, row: ViewTableRow | null, fixedSide?: DropSide) {
      const rowKey = row ? configuredTableRowKey(row) : null;

      return {
        onDragOver(event: DragEvent<HTMLElement>) {
          if (!dragKey) return;
          const side = fixedSide ?? (rowKey ? sideFor(event) : "after");
          const ops = opsAt(groupId, rowKey, side, dragKey);

          // Another handler may still take a drop this group cannot.
          if (!ops) return;
          event.stopPropagation();

          // A drop that changes nothing, such as inside a run of equal values, is not offered.
          if (ops.length === 0) {
            setDrop(null);

            return;
          }

          event.preventDefault();
          event.dataTransfer.dropEffect = "move";

          setDrop((current) =>
            current?.groupId === groupId && current.rowKey === rowKey && current.side === side
              ? current
              : { groupId, rowKey, side },
          );
        },
        onDragLeave(event: DragEvent<HTMLElement>) {
          const related = event.relatedTarget;

          if (related instanceof Node && event.currentTarget.contains(related)) return;
          setDrop((current) =>
            current?.groupId === groupId && current.rowKey === rowKey ? null : current,
          );
        },
        onDrop(event: DragEvent<HTMLElement>) {
          if (!dragKey) return;
          event.preventDefault();
          event.stopPropagation();
          const side = fixedSide ?? (rowKey ? sideFor(event) : "after");
          const movingKey = dragKey;
          setDragKey(null);
          setDrop(null);
          void dropAt(groupId, rowKey, side, movingKey);
        },
      };
    },

    /** The class showing where a drop would land, for a row or a group end. */
    dropClass(groupId: string, row: ViewTableRow | null) {
      const rowKey = row ? configuredTableRowKey(row) : null;

      if (!drop || drop.groupId !== groupId || drop.rowKey !== rowKey) return "";

      return rowKey ? ` is-drop-${drop.side}` : " is-drop-end";
    },

    /**
     * Stages a drop at the start of a group, where a board column shows its
     * drop slot; null when the item cannot be placed there.
     */
    dropAtStart(groupId: string, movingKey: string) {
      const first = displayGroups.find((group) => group.id === groupId)?.rows[0];

      return dropAt(groupId, first ? configuredTableRowKey(first) : null, "before", movingKey);
    },

    /**
     * Moves a row one place earlier or later, crossing into the next group at
     * an edge. `groupId` names which copy moves when the row is in several groups.
     */
    moveByKeyboard(row: ViewTableRow, delta: -1 | 1, groupId?: string) {
      if (!enabled) return Promise.resolve();
      const rowKey = configuredTableRowKey(row);

      const shown = displayGroups.findIndex(
        (group) => group.id === groupId && inGroup(group, rowKey),
      );

      const groupIndex =
        shown >= 0 ? shown : displayGroups.findIndex((group) => inGroup(group, rowKey));

      const group = displayGroups[groupIndex];

      if (!group) return Promise.resolve();

      const index = group.rows.findIndex(
        (candidate) => configuredTableRowKey(candidate) === rowKey,
      );

      // Skip neighbors whose drop changes nothing, such as the rest of a run of equal values.
      for (let next = index + delta; next >= 0 && next < group.rows.length; next += delta) {
        const neighbor = group.rows[next];
        const side = delta < 0 ? "before" : "after";

        const staged = neighbor && dropAt(group.id, configuredTableRowKey(neighbor), side, rowKey);

        if (staged) return staged;
      }

      if (!keyboardCrossesGroups) return Promise.resolve();

      // At a group edge, join the end of the previous group or the start of the next.
      for (let next = groupIndex + delta; next >= 0 && next < displayGroups.length; next += delta) {
        const candidate = displayGroups[next];

        if (!candidate) break;
        const moving = findRow(rowKey);

        if (!moving || !groupMoveOps(moving, group, candidate, capabilities)) continue;
        const edge = delta < 0 ? candidate.rows.at(-1) : candidate.rows[0];

        return (
          dropAt(
            candidate.id,
            edge ? configuredTableRowKey(edge) : null,
            delta < 0 ? "after" : "before",
            rowKey,
          ) ?? Promise.resolve()
        );
      }

      return Promise.resolve();
    },
  };
}

export type ViewReorder = ReturnType<typeof useViewReorder>;

/**
 * Moves each row whose staged group values match another group, and no longer
 * its own, into that group so a staged move shows there. Rows whose values
 * match no group path, such as links or lists, stay where they are.
 */
function regroupStagedRows(groups: ReorderGroup[]): ReorderGroup[] {
  const matches = (row: ViewTableRow, group: ReorderGroup) =>
    group.path.every((step) => stringifyCell(rawFieldValue(row, step.field)) === step.value);

  const arrivals = new Map<string, ViewTableRow[]>();

  const kept = groups.map((group) => ({
    ...group,
    rows: group.rows.filter((row) => {
      if (matches(row, group)) return true;
      const home = groups.find((candidate) => candidate !== group && matches(row, candidate));

      if (!home) return true;
      arrivals.set(home.id, [...(arrivals.get(home.id) ?? []), row]);

      return false;
    }),
  }));

  return kept.map((group) => ({
    ...group,
    rows: [...group.rows, ...(arrivals.get(group.id) ?? [])],
  }));
}

/**
 * The ops that change a row's group values to another group's; null when a
 * differing group field has no safe single-valued edit.
 */
function groupMoveOps(
  row: ViewTableRow,
  source: ReorderGroup,
  destination: ReorderGroup,
  capabilities: ViewFieldCapability[],
): OntologyEditOp[] | null {
  const ops: OntologyEditOp[] = [];

  for (const [index, step] of destination.path.entries()) {
    if (source.path[index]?.value === step.value) continue;
    const op = groupValueOp(row, step.field, capabilityFor(capabilities, step.field), step.value);

    if (!op) return null;
    ops.push(op);
  }

  return ops;
}

/**
 * The op that gives a row a group's value, such as a board column's or
 * lane's; null unless the field has a single-valued enum, boolean, or link edit.
 */
export function groupValueOp(
  row: ViewTableRow,
  field: string,
  capability: ViewFieldCapability | undefined,
  value: string,
) {
  const edit = capability?.edit;

  if (!edit || edit.list || !["enum", "boolean", "node"].includes(edit.kind)) return null;

  return editOpForCell(row, field, edit.field, edit.operation, value, false);
}

/** Alt+Arrow keys that move an item, for a vertical or grid layout. */
export function reorderDelta(
  event: {
    key: string;
    altKey: boolean;
    metaKey: boolean;
    ctrlKey: boolean;
    shiftKey: boolean;
  },
  horizontal = false,
): -1 | 1 | null {
  if (!event.altKey || event.metaKey || event.ctrlKey || event.shiftKey) return null;

  if (event.key === "ArrowUp" || (horizontal && event.key === "ArrowLeft")) return -1;

  if (event.key === "ArrowDown" || (horizontal && event.key === "ArrowRight")) return 1;

  return null;
}
