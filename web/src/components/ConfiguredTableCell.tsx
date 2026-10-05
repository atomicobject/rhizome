import type {
  OntologyEditOp,
  OntologyEditSessionResponse,
  ViewFieldCapability,
  ViewTableRow,
} from "../api/types";
import type { OpenMode } from "./useNoteTabs";
import {
  BooleanEditableCell,
  EnumEditableCell,
  ScalarEditableCell,
} from "./ConfiguredTableCellEditors";
import { NodeEditableCell } from "./ConfiguredTableNodeEditor";
import {
  hasResolvedRelations,
  renderReadOnlyCell,
  withCandidateRelations,
} from "./ConfiguredTableCellReadOnly";
import {
  booleanCellValue,
  cellHasChangedField,
  editOpForCell,
  listEditForCell,
  rawFieldValue,
  scalarEditKind,
  stagedTargetForRow,
} from "./ConfiguredTableCellHelpers";
import { changedFields as stagedChangedFields } from "../staging/stagedState";

type ConfiguredTableCellContentProps = {
  viewID?: string;
  vaultKey?: string | null;
  row: ViewTableRow;
  field: string;
  columnIndex: number;
  capability: ViewFieldCapability | undefined;
  editSession?: OntologyEditSessionResponse | null;
  onStageOps?: (ops: OntologyEditOp[]) => Promise<void> | undefined;
  onOpenRow?: (row: ViewTableRow, mode?: OpenMode) => void;
  onOpenRelation?: (row: ViewTableRow, mode?: OpenMode) => void;
};

export function ConfiguredTableCellContent({
  viewID,
  vaultKey,
  row,
  field,
  columnIndex,
  capability,
  editSession,
  onStageOps,
  onOpenRow,
  onOpenRelation,
}: ConfiguredTableCellContentProps) {
  const edit = capability?.edit;
  const changedFields = stagedChangedFields(editSession, stagedTargetForRow(row));
  const dirty = cellHasChangedField(field, capability, changedFields);

  if (!edit || !onStageOps) {
    return renderReadOnlyCell(
      row,
      field,
      columnIndex,
      undefined,
      capability,
      onOpenRow,
      onOpenRelation,
    );
  }

  const current = rawFieldValue(row, field);
  const fieldIsList = listEditForCell(current, capability, edit);

  const stage = (
    value: string | string[],
    list = fieldIsList,
    expected?: OntologyEditOp["expected"],
  ) => {
    const op = editOpForCell(row, field, edit.field, edit.operation, value, list, expected);

    if (op) return onStageOps([op]);

    return undefined;
  };

  if (edit.kind === "enum" && edit.options?.length) {
    return (
      <EnumEditableCell
        capability={capability}
        options={edit.options}
        value={current}
        list={fieldIsList}
        dirty={dirty}
        onStage={stage}
      />
    );
  }

  if (edit.kind === "boolean") {
    const checked = booleanCellValue(current);

    return (
      <BooleanEditableCell
        capability={capability}
        checked={checked}
        value={current}
        dirty={dirty}
        onStage={stage}
      />
    );
  }

  if (edit.kind === "node") {
    return (
      <NodeEditableCell
        viewID={viewID}
        field={field}
        capability={capability}
        edit={edit}
        value={current}
        list={fieldIsList}
        dirty={dirty}
        editSession={editSession}
        resolved={hasResolvedRelations(row, field, capability)}
        display={(candidates) =>
          renderReadOnlyCell(
            withCandidateRelations(row, field, capability, candidates),
            field,
            columnIndex,
            undefined,
            // A node edit is a relation even when the column reports no value kind.
            capability ? { ...capability, valueKind: "relation" } : capability,
            undefined,
            onOpenRelation,
          )
        }
        onStage={stage}
      />
    );
  }

  if (scalarEditKind(edit.kind)) {
    return (
      <ScalarEditableCell
        row={row}
        field={field}
        columnIndex={columnIndex}
        capability={capability}
        edit={edit}
        value={current}
        list={fieldIsList}
        dirty={dirty}
        vaultKey={vaultKey}
        onStage={stage}
        onOpenRow={onOpenRow}
      />
    );
  }

  return renderReadOnlyCell(
    row,
    field,
    columnIndex,
    undefined,
    capability,
    onOpenRow,
    onOpenRelation,
  );
}

export function configuredTableRowKey(row: ViewTableRow) {
  const fragment = row.ref.fragment ? `#${row.ref.fragment}` : "";

  return `${row.ref.notePath}${fragment}:${row.ref.nodeId ?? ""}`;
}

export function configuredTableRowHasChanges(
  row: ViewTableRow,
  editSession: OntologyEditSessionResponse | null | undefined,
) {
  return stagedChangedFields(editSession, stagedTargetForRow(row)).size > 0;
}
