import type { KeyboardEvent, MouseEvent } from "react";

import type { ViewFieldCapability, ViewTableRow } from "../api/types";
import type { OpenMode } from "./useNoteTabs";
import { publicWorkspaceRef } from "../api/client";
import { displayTitle, enumLabel } from "../lib/labels";
import {
  type EditCandidate,
  candidateForInput,
  cellValues,
  fieldValue,
  rawFieldValue,
} from "./ConfiguredTableCellHelpers";
import { NoteLinkPreview, useNotePreviewTrigger } from "./notePreview/NoteLinkPreview";
import { StatusMark } from "./StatusMark";
import { viewFieldControl } from "./viewFieldControls";

export function renderReadOnlyCell(
  row: ViewTableRow,
  field: string,
  columnIndex: number,
  valueOverride?: string,
  capability?: ViewFieldCapability,
  onOpenRow?: (row: ViewTableRow, mode?: OpenMode) => void,
  onOpenRelation?: (row: ViewTableRow, mode?: OpenMode) => void,
) {
  const value = valueOverride === undefined ? fieldValue(row, field) : valueOverride;
  const control = viewFieldControl(capability);
  const displayValue = control.kind === "relation" ? relationDisplayValue(value) : value;

  if (control.kind === "relation") {
    const relationValues = relationValuesForField(row, field, capability);

    if (relationValues.length > 0) {
      return (
        <span className="configured-view__relation-value">
          {relationValues.map((relation, index) => {
            const target = relation.ref ? publicWorkspaceRef(relation.ref) : "";
            const text = displayTitle(relation.title) || relationDisplayValue(relation.value);

            return (
              <span
                key={`${relation.value}:${target}:${index}`}
                data-configured-relation-link
                onClick={(event) => event.stopPropagation()}
                onFocus={(event) => event.stopPropagation()}
                onDragStart={(event) => {
                  event.preventDefault();
                  event.stopPropagation();
                }}
              >
                {index > 0 && ", "}
                {target && onOpenRelation ? (
                  <NoteLinkPreview
                    target={target}
                    from={row.ref.notePath || row.path}
                    open={(nextTarget, mode) =>
                      onOpenRelation(
                        relationTargetRow(
                          nextTarget,
                          nextTarget === target ? relation.title : undefined,
                          nextTarget === target ? relation.ref : undefined,
                        ),
                        mode === "beside" ? "beside" : "activate",
                      )
                    }
                  >
                    {text}
                  </NoteLinkPreview>
                ) : (
                  text
                )}
              </span>
            );
          })}
        </span>
      );
    }
  }

  if (onOpenRow) {
    const path = row.path || row.ref.notePath;
    const display = displayTitle(displayValue || row.title) || path || "Untitled";

    return <PrimaryReadOnlyCell row={row} path={path} display={display} onOpenRow={onOpenRow} />;
  }

  if (control.kind === "enum" && value) {
    const rawValues =
      valueOverride === undefined ? cellValues(rawFieldValue(row, field)) : [valueOverride];

    const values = rawValues.length > 0 ? rawValues : [value];

    return (
      <span>
        {values.map((enumValue, index) => {
          const option = capability?.enumValues?.find((item) => item.value === enumValue);

          return (
            <span key={`${enumValue}:${index}`}>
              {index > 0 && ", "}
              <StatusMark tone={option?.tone} label={enumLabel(enumValue, option?.label)} />
            </span>
          );
        })}
      </span>
    );
  }

  if (control.kind === "boolean" && (value === "true" || value === "false")) {
    return value === "true" ? "Yes" : "No";
  }

  if (control.kind === "relation" && value) {
    return <span className="configured-view__relation-value">{displayValue}</span>;
  }

  // Staged reads can return a date as a midnight timestamp.
  if (control.kind === "date" && /^\d{4}-\d{2}-\d{2}T00:00:00(\.0+)?Z$/.test(value)) {
    return value.slice(0, 10);
  }

  if (control.kind === "datetime" && value.length > 10) {
    return <time title={value}>{value.slice(0, 10)}</time>;
  }

  return value;
}

function PrimaryReadOnlyCell({
  row,
  path,
  display,
  onOpenRow,
}: {
  row: ViewTableRow;
  path: string;
  display: string;
  onOpenRow: (row: ViewTableRow, mode?: OpenMode) => void;
}) {
  const target = publicWorkspaceRef(row.ref);

  const previewTrigger = useNotePreviewTrigger<HTMLButtonElement>({
    target,
    from: row.ref.notePath || row.path,
    open: (nextTarget, mode) =>
      onOpenRow(
        nextTarget === target ? row : relationTargetRow(nextTarget),
        mode === "beside" ? "beside" : "activate",
      ),
  });

  const gestures = rowTitleGestures((mode) => {
    previewTrigger.close();
    onOpenRow(row, mode);
  });

  return (
    <span className="configured-view__primary-cell">
      <button
        {...previewTrigger.triggerProps}
        type="button"
        className="configured-view__title-button"
        data-row-title
        aria-label={`Open ${display}`}
        title={display}
        onClick={gestures.onClick}
        onKeyDown={(event) => {
          previewTrigger.triggerProps.onKeyDown(event);

          if (!event.defaultPrevented) gestures.onKeyDown(event);
        }}
      >
        {display}
      </button>
      {(!row.title || row.title === path) && display !== path && path ? (
        <small>{path}</small>
      ) : null}
      {previewTrigger.preview}
    </span>
  );
}

/**
 * A table row title's gestures (SPEC-0112): a plain click falls through to
 * the row, which selects it, and so does a double-click, which the row opens.
 * Cmd/Ctrl-click and Enter open the note from the title itself.
 */
export function rowTitleGestures(open: (mode: OpenMode) => void) {
  return {
    onClick: (event: MouseEvent) => {
      if (event.metaKey || event.ctrlKey) open("beside");
    },
    onKeyDown: (event: KeyboardEvent) => {
      if (event.key !== "Enter") return;
      event.preventDefault();
      open("activate");
    },
  };
}

export function hasResolvedRelations(
  row: ViewTableRow,
  field: string,
  capability?: ViewFieldCapability,
) {
  return relationValuesForField(row, field, capability).length > 0;
}

/**
 * Fills a row's relation values from edit candidates when the server sent
 * none, so the cell can still show titled links instead of raw paths.
 */
export function withCandidateRelations(
  row: ViewTableRow,
  field: string,
  capability: ViewFieldCapability | undefined,
  candidates: EditCandidate[],
): ViewTableRow {
  if (candidates.length === 0 || hasResolvedRelations(row, field, capability)) return row;

  const values = cellValues(rawFieldValue(row, field)).map((value) => {
    const candidate = candidateForInput(candidates, value);

    return candidate ? { value, title: candidate.label, ref: candidate.ref } : { value };
  });

  if (!values.some((value) => value.ref)) return row;

  return { ...row, relationValues: { ...row.relationValues, [field]: values } };
}

export function relationValuesForField(
  row: ViewTableRow,
  field: string,
  capability?: ViewFieldCapability,
) {
  const keys = [field, capability?.canonicalField, ...(capability?.sourceKeys || [])];

  for (const key of keys) {
    if (!key) continue;
    const values = row.relationValues?.[key];

    if (values?.length) return values;
  }

  return [];
}

export function relationTargetRow(
  target: string,
  title?: string,
  ref?: ViewTableRow["ref"],
): ViewTableRow {
  const fragmentIndex = target.indexOf("#");

  const fallbackRef = {
    notePath: fragmentIndex > 0 ? target.slice(0, fragmentIndex) : target,
    fragment: fragmentIndex > 0 ? target.slice(fragmentIndex + 1) : undefined,
    kind: fragmentIndex > 0 ? ("EMBEDDED" as const) : ("NOTE" as const),
  };

  return { ref: ref || fallbackRef, path: target, title: title || target };
}

export function relationDisplayValue(value: string) {
  return value
    .replace(
      /\[\[([^|\]]+)(?:\|([^\]]+))?\]\]/g,
      // A link names a file; show its name, not the folders above it.
      (_match, target: string, alias?: string) => alias ?? target.replace(/^.*\//, ""),
    )
    .split(",")
    .map((item) => item.trim())
    .join(", ");
}

/** A change time as its age ("4h", "3d"), with the full local time on hover. */
export function ChangedCell({
  value,
  now = Date.now(),
}: {
  value: number | undefined;
  now?: number;
}) {
  if (!value) return null;
  const date = new Date(value < 1e12 ? value * 1000 : value);

  return (
    <time dateTime={date.toISOString()} title={date.toLocaleString()}>
      {relativeAge(date.getTime(), now)}
    </time>
  );
}

/**
 * A compact age such as "15h" or "3d". Every view reads ages through this one
 * helper so a table's Changed column and a board card agree.
 */
export function relativeAge(millis: number, now = Date.now()) {
  const minutes = Math.max(0, Math.round((now - millis) / 60_000));

  if (minutes < 1) return "now";

  if (minutes < 60) return `${minutes}m`;
  const hours = Math.round(minutes / 60);

  if (hours < 48) return `${hours}h`;
  const days = Math.round(hours / 24);

  if (days < 60) return `${days}d`;
  const months = Math.round(days / 30);

  return months < 24 ? `${months}mo` : `${Math.round(days / 365)}y`;
}
