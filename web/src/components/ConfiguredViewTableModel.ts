import { type RefObject, useEffect, useState } from "react";
import type {
  ViewExecuteRequest,
  ViewExecuteResponse,
  ViewFieldCapability,
  ViewTableColumn,
  ViewTableRow,
} from "../api/types";
import { fieldValue } from "./ConfiguredTableCellHelpers";
import { capabilityFor, groupValueLabel } from "./ConfiguredViewModel";
import type { ReorderGroup } from "./useViewReorder";
import { viewFieldControl } from "./viewFieldControls";

type ServerGroup = NonNullable<ViewExecuteResponse["groups"]>[number];

export type ConfiguredTableGroup = {
  field: string;
  key: string;
  label: string;
  value: string;
  count: number;
  tone?: string;
  depth: number;
  rowStart: number;
  rowEnd: number;
  collapsedByDefault: boolean;
  children: ConfiguredTableGroup[];
};

export const UNGROUPED = "__all__";

/** Leaf groups with their group-field path, or one group holding every row. */
export function leafReorderGroups(
  groups: ConfiguredTableGroup[],
  rows: ViewTableRow[],
  capabilities: ViewFieldCapability[],
): ReorderGroup[] {
  if (groups.length === 0) return [{ id: UNGROUPED, label: "", path: [], rows }];

  const leaves: ReorderGroup[] = [];

  const visit = (group: ConfiguredTableGroup, path: ReorderGroup["path"]) => {
    const here = [...path, { field: group.field, value: group.value }];

    if (group.children.length > 0) {
      for (const child of group.children) visit(child, here);

      return;
    }

    leaves.push({
      id: group.key,
      label: groupValueLabel(capabilityFor(capabilities, group.field), group.value, group.label),
      path: here,
      rows: rows.slice(group.rowStart, group.rowEnd),
    });
  };

  for (const group of groups) visit(group, []);

  return leaves;
}

export function columnClassName(column: ViewTableColumn, capabilities: ViewFieldCapability[]) {
  const capability = capabilityFor(capabilities, column.field);
  const kind = viewFieldControl(capability).kind;
  const classes = ["configured-view__cell"];

  if (column.field === "notePath") {
    classes.push("configured-view__cell--source");
  } else if (
    capability?.semanticRole === "identifier" ||
    column.field === "path" ||
    /(^|\.)id$/i.test(column.field)
  ) {
    classes.push("configured-view__cell--identifier");

    if (column.field !== "path") classes.push("configured-view__cell--id");
  }

  if (kind === "date" || kind === "datetime" || column.field === "updatedAt") {
    classes.push("configured-view__cell--date");

    // A due date can be overdue, so it gets room for that label.
    if (isDeadlineField(capability?.canonicalField ?? column.field))
      classes.push("configured-view__cell--deadline");
  }

  // The change time renders as a relative age, so it needs less room than a date.
  if (column.field === "updatedAt") classes.push("configured-view__cell--age");

  if (
    kind === "number" ||
    column.field === "validationIssues" ||
    column.field === "hasIssues" ||
    /count$/i.test(column.field)
  ) {
    classes.push("configured-view__cell--numeric");
  }

  if (kind === "enum" || kind === "boolean" || kind === "relation") {
    classes.push(`configured-view__cell--${kind}`);
  }

  // One person's name needs less room than a linked record's title.
  if (kind === "relation" && capability?.edit?.targetType === "Person" && !capability.edit.list)
    classes.push("configured-view__cell--person");

  if (column.field === "hasIssues") classes.push("configured-view__cell--flag");

  return classes.join(" ");
}

/** A date field that names when something is due, such as `due` or `deadline`. */
export function isDeadlineField(field: string) {
  return /^(due|due[-_]?date|deadline)$/i.test(field.replace(/^.*\./, ""));
}

/**
 * True when an open row's due date is before today. A row is closed when its
 * `done` flag is set or a staged field, such as its lifecycle, is at a done or
 * dropped stage; a date-only value compares by calendar day.
 */
export function isOverdue(
  row: ViewTableRow,
  value: string,
  capabilities: ViewFieldCapability[],
  today = localDay(new Date()),
) {
  const done = row.fields?.done;

  if (done === true || done === "true") return false;

  const closed = capabilities.some((capability) => {
    const current = fieldValue(row, capability.key);
    const stage = capability.enumValues?.find((option) => option.value === current)?.stage;

    return stage === "done" || stage === "dropped";
  });

  if (closed) return false;

  return /^\d{4}-\d{2}-\d{2}/.test(value) && value.slice(0, 10) < today;
}

function localDay(date: Date) {
  const pad = (part: number) => String(part).padStart(2, "0");

  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
}

export function primaryColumn(columns: ViewTableColumn[], capabilities: ViewFieldCapability[]) {
  const semanticTitle = columns.findIndex(
    (column) => capabilityFor(capabilities, column.field)?.semanticRole === "title",
  );

  if (semanticTitle >= 0) return semanticTitle;
  const namedTitle = columns.findIndex((column) => ["title", "name"].includes(column.field));

  return namedTitle >= 0 ? namedTitle : 0;
}

export type SortSpec = NonNullable<ViewExecuteRequest["sort"]>[number];

/** The reader's sort, else the one the view ran with. */
export function effectiveSort(state: ViewExecuteRequest, executed: ViewExecuteRequest) {
  return state.sort ?? executed.sort ?? [];
}

export function sortDirection(sort: SortSpec[], field: string) {
  const direction = sort.find((item) => item.field === field)?.direction;

  return direction === "asc" || direction === "desc" ? direction : null;
}

/**
 * A plain header click sorts by that column alone, flipping its direction;
 * a shift-click adds it as the next key, flips it, or removes it after
 * descending.
 */
export function nextSort(sort: SortSpec[], field: string, additive: boolean): SortSpec[] {
  const current = sort.find((item) => item.field === field);

  if (!additive) return [{ field, direction: current?.direction === "asc" ? "desc" : "asc" }];

  if (!current) return [...sort, { field, direction: "asc" }];

  if (current.direction === "asc")
    return sort.map((item) => (item.field === field ? { ...item, direction: "desc" } : item));

  return sort.filter((item) => item.field !== field);
}

export function normalizeTableGroups(groups: ServerGroup[], rows: ViewTableRow[]) {
  return normalizeGroupsForRowCount(groups, rows.length);
}

function normalizeGroupsForRowCount(groups: ServerGroup[], rowCount: number) {
  return groups.flatMap((group, index) => {
    const normalized = normalizeGroup(group, rowCount, index);

    return normalized ? [normalized] : [];
  });
}

function normalizeGroup(
  group: ServerGroup,
  rowCount: number,
  fallbackIndex: number,
): ConfiguredTableGroup | null {
  if (!group.field) return null;
  const rowStart = clamp(group.rowStart, 0, rowCount);
  const rowEnd = clamp(group.rowEnd, rowStart, rowCount);

  if (rowStart >= rowEnd) return null;

  return {
    field: group.field,
    key: group.key || `${group.field}:${group.value}:${rowStart}:${rowEnd}:${fallbackIndex}`,
    label: group.label ?? "",
    value: group.value,
    count: group.totalCount ?? group.count,
    tone: group.tone,
    depth: group.depth,
    rowStart,
    rowEnd,
    collapsedByDefault: group.collapsedByDefault ?? false,
    children: normalizeGroupsForRowCount(group.children ?? [], rowCount),
  };
}

/** The rows a grouped table renders, in order; a collapsed group renders none. */
export function renderedGroupRows(
  groups: ConfiguredTableGroup[],
  rowsForGroup: (group: ConfiguredTableGroup) => ViewTableRow[],
  isExpanded: (group: ConfiguredTableGroup) => boolean,
): ViewTableRow[] {
  return groups.flatMap((group) => {
    if (!isExpanded(group)) return [];

    return group.children.length > 0
      ? renderedGroupRows(group.children, rowsForGroup, isExpanded)
      : rowsForGroup(group);
  });
}

export function countCollapsedGroups(
  groups: ConfiguredTableGroup[],
  expandedGroups: Record<string, boolean>,
): number {
  return groups.reduce((count, group) => {
    const expanded = expandedGroups[group.key] ?? !group.collapsedByDefault;

    return count + (expanded ? countCollapsedGroups(group.children, expandedGroups) : 1);
  }, 0);
}

export function groupTone(capabilities: ViewFieldCapability[], group: ConfiguredTableGroup) {
  const capability = capabilityFor(capabilities, group.field);

  return capability?.enumValues?.find((option) => option.value === group.value)?.tone;
}

function clamp(value: number, min: number, max: number) {
  return Math.max(min, Math.min(max, value));
}

/** The element's content width, or Infinity until it is measured. */
export function useElementWidth(ref: RefObject<HTMLElement | null>) {
  const [width, setWidth] = useState(Number.POSITIVE_INFINITY);

  useEffect(() => {
    const element = ref.current;

    if (!element) return;

    const observer = new ResizeObserver(() => {
      // A hidden table measures zero; it keeps its columns until it shows.
      if (element.clientWidth > 0) setWidth(element.clientWidth);
    });

    observer.observe(element);

    return () => observer.disconnect();
  }, [ref]);

  return width;
}
