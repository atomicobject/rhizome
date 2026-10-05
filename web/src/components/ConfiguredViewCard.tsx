import type { DragEvent, KeyboardEvent, ReactNode } from "react";

import type {
  OntologyEditSessionResponse,
  ValidationScope,
  ViewExecuteResponse,
  ViewFieldCapability,
  ViewTableRow,
} from "../api/types";
import { publicWorkspaceRef } from "../api/client";
import { openModeFor, type OpenMode } from "./useNoteTabs";
import {
  ConfiguredTableCellContent,
  configuredTableRowHasChanges,
  configuredTableRowKey,
} from "./ConfiguredTableCell";
import { displayTitle } from "../lib/labels";
import { fieldValue } from "./ConfiguredTableCellHelpers";
import { relationTargetRow } from "./ConfiguredTableCellReadOnly";
import { capabilityFor, columnLabel, isIssueColumn } from "./ConfiguredViewModel";
import { useNotePreviewTrigger } from "./notePreview/NoteLinkPreview";
import { reorderDelta } from "./useViewReorder";

type CardLayout = NonNullable<ViewExecuteResponse["card"]>;

type Props = {
  row: ViewTableRow;
  layout?: CardLayout;
  mode: "board" | "cards";
  capabilities: ViewFieldCapability[];
  viewID: string;
  editSession?: OntologyEditSessionResponse | null;
  vaultKey?: string | null;
  onOpenRow: (row: ViewTableRow, mode?: OpenMode) => void;
  issueCounts?: ReadonlyMap<string, number>;
  onOpenIssues?: (scope: ValidationScope) => void;
  actions?: ReactNode;
  busy?: boolean;
  draggable?: boolean;
  dragging?: boolean;
  onMoveShortcut?: () => void;
  onDragStart?: (event: DragEvent<HTMLElement>) => void;
  onDragEnd?: (event: DragEvent<HTMLElement>) => void;
  /** Drop handlers for placing another card beside this one. */
  dropTarget?: {
    onDragOver: (event: DragEvent<HTMLElement>) => void;
    onDragLeave: (event: DragEvent<HTMLElement>) => void;
    onDrop: (event: DragEvent<HTMLElement>) => void;
  };
  /** Extra classes, such as the drop indicator. */
  className?: string;
  /** Moves the card one place earlier or later; Alt+Arrow keys call it. */
  onReorder?: (delta: -1 | 1) => void;
  /** A field the surrounding layout already shows, such as the board column field. */
  omitField?: string;
  /** Profile-driven content above and below the title, replacing the card layout's slots. */
  header?: ReactNode;
  body?: ReactNode;
  /** Title and markers only, for crowded board columns. */
  compact?: boolean;
};

export function ConfiguredViewCard({
  row,
  layout,
  mode,
  capabilities,
  viewID,
  editSession,
  vaultKey,
  onOpenRow,
  issueCounts,
  onOpenIssues,
  actions,
  busy = false,
  draggable = false,
  dragging = false,
  onMoveShortcut,
  onDragStart,
  onDragEnd,
  dropTarget,
  className = "",
  onReorder,
  omitField,
  header,
  body,
  compact = false,
}: Props) {
  const custom = header !== undefined || body !== undefined;
  // A compact card keeps its title; the layout's slots would crowd the column.
  const layoutSlots = !custom && !compact;
  const titleField = layout?.title.field ?? "title";
  const title = displayTitle(fieldValue(row, titleField) || row.title) || "Untitled";
  const eyebrow = layout?.eyebrow;
  const eyebrowValue = eyebrow ? fieldValue(row, eyebrow.field) : "";
  const preview = layout?.preview;
  const previewValue = preview ? fieldValue(row, preview.field) : "";
  const dirty = configuredTableRowHasChanges(row, editSession);
  const titleTarget = publicWorkspaceRef(row.ref);

  const titlePreview = useNotePreviewTrigger<HTMLButtonElement>({
    target: titleTarget,
    from: row.ref.notePath || row.path,
    open: (target, openMode) =>
      onOpenRow(
        target === titleTarget ? row : relationTargetRow(target),
        openMode === "beside" ? "beside" : "activate",
      ),
  });

  // Empty values, the omitted field, and a hasIssues flag the issue count
  // below replaces would only add labels with nothing to say.
  const visibleFields = (layout?.fields ?? []).filter(
    (field) =>
      field.field !== omitField &&
      fieldValue(row, field.field) !== "" &&
      !isIssueColumn(field.field, issueCounts),
  );

  const path = row.ref.notePath || row.path || "";
  const issueCount = issueCounts?.get(path);

  function handleKeyDown(event: KeyboardEvent<HTMLElement>) {
    if (event.target !== event.currentTarget || busy) return;

    if (event.key === "Enter") {
      event.preventDefault();
      onOpenRow(row);

      return;
    }

    // Grid cards flow left to right; board cards stack in a column.
    const delta = onReorder ? reorderDelta(event, mode === "cards") : null;

    if (delta !== null) {
      event.preventDefault();
      onReorder?.(delta);

      return;
    }

    const modified = event.metaKey || event.ctrlKey || event.altKey;

    if (event.key.toLowerCase() === "m" && !modified && onMoveShortcut) {
      event.preventDefault();
      onMoveShortcut();
    }
  }

  return (
    <article
      className={`configured-card configured-card--${mode}${compact ? " configured-card--compact" : ""}${dirty ? " is-staged" : ""}${dragging ? " is-dragging" : ""}${className}`}
      data-configured-card={configuredTableRowKey(row)}
      tabIndex={busy ? -1 : 0}
      inert={busy ? true : undefined}
      draggable={draggable && !busy}
      aria-busy={busy || undefined}
      onKeyDown={handleKeyDown}
      onDragStart={(event) => {
        const target = event.target;

        if (!(target instanceof Node) || !event.currentTarget.contains(target)) return;

        if (target instanceof Element && target.closest("[data-configured-relation-link]")) {
          event.preventDefault();
          event.stopPropagation();

          return;
        }

        onDragStart?.(event);
      }}
      onDragEnd={onDragEnd}
      {...dropTarget}
    >
      <div
        // Only the hover-revealed actions may float over the title.
        className={`configured-card__topline${(custom ? header : layoutSlots && eyebrowValue) || dirty ? "" : " configured-card__topline--bare"}`}
      >
        {custom && header}
        {layoutSlots && eyebrow && eyebrowValue && (
          <span className="configured-card__eyebrow">
            <ConfiguredTableCellContent
              viewID={viewID}
              vaultKey={vaultKey}
              row={row}
              field={eyebrow.field}
              columnIndex={0}
              capability={capabilityFor(capabilities, eyebrow.field)}
              editSession={editSession}
              onOpenRelation={onOpenRow}
            />
          </span>
        )}
        {dirty && <span className="configured-card__staged">staged</span>}
        {actions}
      </div>
      <button
        {...titlePreview.triggerProps}
        type="button"
        className="configured-card__title"
        onClick={(event) => {
          titlePreview.close();
          onOpenRow(row, openModeFor(event));
        }}
        disabled={busy}
      >
        {title}
      </button>
      {titlePreview.preview}
      {custom && body}
      {custom && issueCount !== undefined && issueCount > 0 && (
        <span className="configured-card__issues">
          <button
            type="button"
            aria-label={`Open ${issueCount} ${issueCount === 1 ? "problem" : "problems"} for ${path}`}
            onClick={() => onOpenIssues?.({ kind: "note", key: path })}
          >
            {issueCount} {issueCount === 1 ? "issue" : "issues"}
          </button>
        </span>
      )}
      {layoutSlots && preview && previewValue && (
        <p className="configured-card__preview">
          <ConfiguredTableCellContent
            viewID={viewID}
            vaultKey={vaultKey}
            row={row}
            field={preview.field}
            columnIndex={1}
            capability={capabilityFor(capabilities, preview.field)}
            editSession={editSession}
            onOpenRelation={onOpenRow}
          />
        </p>
      )}
      {layoutSlots &&
        (visibleFields.length > 0 || (issueCount !== undefined && issueCount > 0)) && (
          <dl className="configured-card__fields">
            {visibleFields.map((field, index) => (
              <div className="configured-card__field" key={`${field.field}:${index}`}>
                {mode === "cards" && <dt>{columnLabel(field, capabilities)}</dt>}
                <dd title={mode === "board" ? columnLabel(field, capabilities) : undefined}>
                  <ConfiguredTableCellContent
                    viewID={viewID}
                    vaultKey={vaultKey}
                    row={row}
                    field={field.field}
                    columnIndex={index}
                    capability={capabilityFor(capabilities, field.field)}
                    editSession={editSession}
                    onOpenRelation={onOpenRow}
                  />
                </dd>
              </div>
            ))}
            {issueCount !== undefined && issueCount > 0 && (
              <div className="configured-card__field configured-card__issues">
                {mode === "cards" && <dt>Issues</dt>}
                <dd>
                  <button
                    type="button"
                    aria-label={`Open ${issueCount} ${issueCount === 1 ? "problem" : "problems"} for ${path}`}
                    onClick={() => onOpenIssues?.({ kind: "note", key: path })}
                  >
                    {issueCount} {issueCount === 1 ? "issue" : "issues"}
                  </button>
                </dd>
              </div>
            )}
          </dl>
        )}
    </article>
  );
}

/** Keeps keyboard focus on a moved card once it re-renders in its new place. */
export function focusCard(rowKey: string) {
  window.requestAnimationFrame(() =>
    document.querySelector<HTMLElement>(`[data-configured-card="${CSS.escape(rowKey)}"]`)?.focus(),
  );
}
