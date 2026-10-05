import { useEffect, useRef, useState } from "react";
import type {
  OntologyEditFieldValue,
  OntologyEditOp,
  ViewFieldCapability,
  ViewTableRow,
} from "../api/types";
import { publicWorkspaceRef } from "../api/client";
import type { OpenMode } from "./useNoteTabs";
import { type JsonValue } from "../api/parse";
import { enumLabel } from "../lib/labels";
import type { CellEditValue } from "./ConfiguredTableCellHelpers";
import {
  capabilityLabel,
  cellValues,
  editPathForRow,
  firstScalar,
  stringifyCell,
  workspaceFieldNodeForTableCell,
} from "./ConfiguredTableCellHelpers";
import {
  relationTargetRow,
  renderReadOnlyCell,
  rowTitleGestures,
} from "./ConfiguredTableCellReadOnly";
import { StatusMark } from "./StatusMark";
import { ListValueEditor } from "./ConfiguredTableNodeEditor";
import { FieldEditor, fieldExpectedFromNode, fieldOperationTarget } from "./editing/FieldEditor";
import { useReturnFocusOnClose } from "./focusManagement";
import { useNotePreviewTrigger } from "./notePreview/NoteLinkPreview";

export function EnumEditableCell({
  capability,
  options,
  value,
  list,
  dirty,
  onStage,
}: {
  capability: ViewFieldCapability | undefined;
  options: string[];
  value: JsonValue | undefined;
  list: boolean;
  dirty: boolean;
  onStage: (value: CellEditValue, list?: boolean) => Promise<void> | undefined;
}) {
  const [editing, setEditing] = useState(false);
  const editorRef = useRef<HTMLSelectElement>(null);
  const triggerRef = useReturnFocusOnClose<HTMLButtonElement>(editing);
  const values = cellValues(value);
  const unknown = values.filter((item) => item && !options.includes(item));
  const primary = values[0] ?? "Unset";
  const enumValue = capability?.enumValues?.find((item) => item.value === primary);

  const optionLabel = (item: string) =>
    enumLabel(item, capability?.enumValues?.find((option) => option.value === item)?.label);

  useEffect(() => {
    if (editing) editorRef.current?.focus();
  }, [editing]);

  if (!editing) {
    return (
      <button
        ref={triggerRef}
        type="button"
        className={`configured-view__enum-trigger${dirty ? " is-dirty" : ""}`}
        aria-label={`Edit ${capabilityLabel(capability)}`}
        aria-description={values.map(optionLabel).join(", ") || "Unset"}
        onClick={() => setEditing(true)}
        onKeyDown={(event) => {
          if (event.key !== "F2") return;
          event.preventDefault();
          setEditing(true);
        }}
      >
        <StatusMark
          tone={enumValue?.tone}
          label={
            list && values.length > 1
              ? `${optionLabel(primary)} +${values.length - 1}`
              : optionLabel(primary)
          }
        />
      </button>
    );
  }

  if (list) {
    return (
      <ListValueEditor
        label={capabilityLabel(capability)}
        values={values}
        options={options.map((option) => ({
          value: option,
          label: optionLabel(option),
        }))}
        labelFor={optionLabel}
        dirty={dirty}
        onChange={(next) => void onStage(next, true)}
        onClose={() => setEditing(false)}
      />
    );
  }

  return (
    <span className="configured-view__enum-editor">
      <select
        ref={editorRef}
        className={`configured-view__cell-editor configured-view__cell-editor--enum${
          dirty ? " is-dirty" : ""
        }`}
        aria-label={`Edit ${capabilityLabel(capability)}`}
        value={values[0] ?? ""}
        onChange={(event) => {
          void onStage(event.currentTarget.value);
          setEditing(false);
        }}
        onKeyDown={(event) => {
          if (event.key === "Escape") setEditing(false);
        }}
      >
        <option value="">Unset</option>
        {unknown.map((option) => (
          <option key={option} value={option}>
            {option}
          </option>
        ))}
        {options.map((option) => (
          <option key={option} value={option}>
            {optionLabel(option)}
          </option>
        ))}
      </select>
      {values.length > 0 ? (
        <button
          type="button"
          className="configured-view__cell-clear"
          aria-label={`Clear ${capabilityLabel(capability)}`}
          onClick={() => void onStage("")}
        >
          ×
        </button>
      ) : null}
    </span>
  );
}

export function BooleanEditableCell({
  capability,
  checked,
  value,
  dirty,
  onStage,
}: {
  capability: ViewFieldCapability | undefined;
  checked: boolean;
  value: JsonValue | undefined;
  dirty: boolean;
  onStage: (value: CellEditValue) => Promise<void> | undefined;
}) {
  const [localChecked, setLocalChecked] = useState(checked);
  const [pendingChecked, setPendingChecked] = useState<boolean | null>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const present = value !== undefined && value !== null && value !== "";

  useEffect(() => {
    if (pendingChecked === null || checked === pendingChecked) {
      setLocalChecked(checked);
      setPendingChecked(null);
    }
  }, [checked, pendingChecked]);

  useEffect(() => {
    if (inputRef.current) inputRef.current.indeterminate = !present;
  }, [present]);

  return (
    <span className={`configured-view__boolean-editor${dirty ? " is-dirty" : ""}`}>
      <input
        ref={inputRef}
        className={`configured-view__checkbox-editor${dirty ? " is-dirty" : ""}`}
        type="checkbox"
        aria-label={`Edit ${capabilityLabel(capability)}`}
        checked={localChecked}
        onChange={(event) => {
          const next = event.target.checked;
          setLocalChecked(next);
          setPendingChecked(next);
          void Promise.resolve(onStage(next ? "true" : "false")).catch(() => {
            setPendingChecked(null);
            setLocalChecked(checked);
          });
        }}
      />
      {present ? (
        <button
          type="button"
          className="configured-view__cell-clear"
          aria-label={`Clear ${capabilityLabel(capability)}`}
          onClick={() => {
            setLocalChecked(false);
            setPendingChecked(false);
            void Promise.resolve(onStage("")).catch(() => {
              setPendingChecked(null);
              setLocalChecked(checked);
            });
          }}
        >
          ×
        </button>
      ) : null}
    </span>
  );
}

export function ScalarEditableCell({
  row,
  field,
  columnIndex,
  capability,
  edit,
  value,
  list,
  dirty,
  vaultKey,
  onStage,
  onOpenRow,
}: {
  row: ViewTableRow;
  field: string;
  columnIndex: number;
  capability: ViewFieldCapability;
  edit: NonNullable<ViewFieldCapability["edit"]>;
  value: JsonValue | undefined;
  list: boolean;
  dirty: boolean;
  vaultKey?: string | null;
  onStage: (
    value: string | string[],
    list?: boolean,
    expected?: OntologyEditOp["expected"],
  ) => Promise<void> | undefined;
  onOpenRow?: (row: ViewTableRow, mode?: OpenMode) => void;
}) {
  const displayValue = stringifyCell(firstScalar(value));
  const [editing, setEditing] = useState(false);
  const editorRef = useRef<HTMLSpanElement>(null);
  const triggerRef = useReturnFocusOnClose<HTMLButtonElement>(editing);
  const previewTarget = publicWorkspaceRef(row.ref);

  const previewTrigger = useNotePreviewTrigger<HTMLButtonElement>({
    target: previewTarget,
    from: row.ref.notePath || row.path,
    open: (target, mode) =>
      onOpenRow?.(
        target === previewTarget ? row : relationTargetRow(target),
        mode === "beside" ? "beside" : "activate",
      ),
  });

  const { ref: previewRef, ...previewTriggerProps } = previewTrigger.triggerProps;

  const gestures = rowTitleGestures((mode) => {
    previewTrigger.close();
    onOpenRow?.(row, mode);
  });

  const operationTarget = fieldOperationTarget(editPathForRow(row), row.ref.nodeId, edit.field);
  const node = workspaceFieldNodeForTableCell(row, capability, edit, value, list);
  useEffect(() => {
    if (!editing) return;
    editorRef.current?.querySelector<HTMLElement>("input, textarea, select, button")?.focus();
  }, [editing]);

  const stageSharedValue = (
    next: OntologyEditFieldValue,
    expected?: OntologyEditOp["expected"],
  ) => {
    const nextValue = next.kind === "list" ? next.items : next.kind === "scalar" ? next.scalar : "";
    const nextList = next.kind === "list" || list;
    const result = onStage(nextValue, nextList, expected);
    setEditing(false);

    return result;
  };

  if (editing) {
    return (
      <span
        ref={editorRef}
        onKeyDown={(event) => {
          if (event.key === "Escape") setEditing(false);
        }}
        // A changed value stages on blur (which closes the editor) or stays
        // open with its error; an unchanged one would otherwise stay open.
        onBlur={(event) => {
          const next = event.relatedTarget;
          const input = event.target;

          if (next instanceof Node && event.currentTarget.contains(next)) return;

          if (
            (input instanceof HTMLInputElement || input instanceof HTMLTextAreaElement) &&
            input.value === displayValue
          ) {
            setEditing(false);
          }
        }}
      >
        <FieldEditor
          node={node}
          editing
          dirty={dirty}
          vaultKey={vaultKey}
          operationTarget={operationTarget}
          expected={fieldExpectedFromNode(node)}
          onStage={stageSharedValue}
        />
      </span>
    );
  }

  return (
    <>
      <button
        {...(onOpenRow ? previewTriggerProps : {})}
        ref={(element) => {
          triggerRef.current = element;

          if (onOpenRow) previewRef.current = element;
        }}
        type="button"
        className={`configured-view__scalar-cell${dirty ? " is-dirty" : ""}`}
        aria-label={
          onOpenRow
            ? `Open ${displayValue || row.title || "Untitled"}`
            : `Edit ${capabilityLabel(capability)}`
        }
        // The label names the action; the value is read after it.
        aria-description={onOpenRow ? undefined : displayValue || "Empty"}
        title={onOpenRow ? `F2 to edit ${capabilityLabel(capability)}` : undefined}
        // A title's double-click falls through to its row, which opens it.
        data-row-title={onOpenRow ? true : undefined}
        onDoubleClick={() => {
          if (onOpenRow) return;
          previewTrigger.close();
          setEditing(true);
        }}
        onClick={(event) => (onOpenRow ? gestures.onClick(event) : setEditing(true))}
        onKeyDown={(event) => {
          if (onOpenRow) previewTriggerProps.onKeyDown?.(event);

          if (event.defaultPrevented) return;

          // The open affordance keeps Enter for opening; F2 is the edit key there.
          if (event.key === "F2" || (event.key === "Enter" && !onOpenRow)) {
            event.preventDefault();
            previewTrigger.close();
            setEditing(true);
          } else if (onOpenRow) gestures.onKeyDown(event);
        }}
      >
        {renderReadOnlyCell(row, field, columnIndex, displayValue, capability)}
      </button>
      {onOpenRow ? previewTrigger.preview : null}
    </>
  );
}
