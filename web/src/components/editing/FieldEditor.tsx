import { Fragment, type ReactNode, useCallback, useEffect, useId, useRef, useState } from "react";

import type { OntologyEditFieldValue, WorkspaceFieldNode } from "../../api/types";
import { effectiveFieldCapability } from "../ontologyFieldPicking";
import { DateWidget } from "../widgets/DateWidget";
import { EnumPillWidget } from "../widgets/EnumPillWidget";
import { InlineTextWidget } from "../widgets/InlineTextWidget";
import {
  clearPersistedEditorDraft,
  EDITOR_DRAFT_CLEARED_EVENT,
  EDITOR_REJECTED_EVENT,
  readPersistedEditorDraftRecord,
  type OntologyEditExpected,
  writePersistedEditorDraft,
} from "./draftStorage";
import { BooleanEditor, ListEditor, NumberEditor } from "./FieldValueEditors";
import { RelationEditor, displayValues, fieldValueFromScalar } from "./RelationEditor";
import { FieldLinkValues, fieldLinksForValues } from "../FieldLinkValues";

type Props = {
  node: WorkspaceFieldNode;
  editing: boolean;
  dirty?: boolean;
  onStage: (value: OntologyEditFieldValue, expected?: OntologyEditExpected) => Promise<void> | void;
  vaultKey?: string | null;
  operationTarget?: string;
  expected?: OntologyEditExpected;
};

export function FieldEditor({
  node,
  editing,
  dirty = false,
  onStage,
  vaultKey = null,
  operationTarget = fieldOperationTarget(node.notePath, node.ref.nodeId, node.field.name),
  expected,
}: Props) {
  const capability = node.field.capability ? effectiveFieldCapability(node) : null;
  const values = node.field.values || [];
  const value = values[0] || "";
  const label = node.field.name;
  const [requiredError, setRequiredError] = useState("");
  const fieldEditorID = useId();
  const errorID = `field-editor-error-${fieldEditorID}`;
  const changedID = `field-editor-changed-${fieldEditorID}`;

  const [persistedAtMount] = useState(() =>
    readPersistedEditorDraftRecord(vaultKey, operationTarget),
  );

  const capturedExpectedRef = useRef(persistedAtMount?.expected);
  // Remounts the control when the server refuses its edit, so it drops the
  // refused value and shows the saved one again.
  const [rejections, setRejections] = useState(0);

  useEffect(() => {
    const reset = (event: Event) => {
      // SAFETY: the edit session is the sole dispatcher and always sends this detail shape.
      const detail = (event as CustomEvent<{ operationTarget: string }>).detail;

      if (detail?.operationTarget !== operationTarget) return;
      capturedExpectedRef.current = undefined;
      setRequiredError("");
      setRejections((count) => count + 1);
    };

    window.addEventListener(EDITOR_REJECTED_EVENT, reset);

    return () => window.removeEventListener(EDITOR_REJECTED_EVENT, reset);
  }, [operationTarget]);

  const captureExpected = useCallback(
    (
      next: OntologyEditFieldValue,
      preserveWhenOriginal = false,
    ): OntologyEditExpected | undefined => {
      const persisted = readPersistedEditorDraftRecord(vaultKey, operationTarget);

      if (persisted?.expected) capturedExpectedRef.current = persisted.expected;

      const original =
        capturedExpectedRef.current?.field || expected?.field || fieldValueFromNode(node);

      if (sameFieldValue(next, original)) {
        if (preserveWhenOriginal) {
          if (!capturedExpectedRef.current) {
            capturedExpectedRef.current = { ...expected, field: original };
          }

          return capturedExpectedRef.current;
        }

        capturedExpectedRef.current = undefined;
        clearPersistedEditorDraft(vaultKey, operationTarget);

        return undefined;
      }

      if (!capturedExpectedRef.current) {
        capturedExpectedRef.current = {
          ...expected,
          field: expected?.field || fieldValueFromNode(node),
        };
      }

      return capturedExpectedRef.current;
    },
    [expected, node, operationTarget, vaultKey],
  );

  useEffect(() => {
    const discard = () => {
      capturedExpectedRef.current = undefined;
    };

    window.addEventListener("rhizome:discard-editor-drafts", discard);

    return () => window.removeEventListener("rhizome:discard-editor-drafts", discard);
  }, []);
  useEffect(() => {
    const clearCapturedWitness = (event: Event) => {
      // SAFETY: draftStorage is the sole dispatcher and always sends this detail shape.
      const detail = (event as CustomEvent<{ vaultKey: string | null; operationTarget: string }>)
        .detail;

      if (detail?.vaultKey === vaultKey && detail.operationTarget === operationTarget) {
        capturedExpectedRef.current = undefined;
      }
    };

    window.addEventListener(EDITOR_DRAFT_CLEARED_EVENT, clearCapturedWitness);

    return () => window.removeEventListener(EDITOR_DRAFT_CLEARED_EVENT, clearCapturedWitness);
  }, [operationTarget, vaultKey]);

  const stage = useCallback(
    (next: OntologyEditFieldValue): boolean => {
      if (capability?.required && isEmptyFieldValue(next)) {
        setRequiredError(`${label} is required.`);

        return false;
      }

      setRequiredError("");
      const persisted = readPersistedEditorDraftRecord(vaultKey, operationTarget);

      if (persisted?.legacy && !capturedExpectedRef.current) return false;

      const stagedExpected = persisted?.legacy
        ? capturedExpectedRef.current
        : persisted?.expected || capturedExpectedRef.current || expected;

      const draftBeforeStage = readPersistedEditorDraftRecord(vaultKey, operationTarget);

      const clearTransferredDraft = () => {
        const current = readPersistedEditorDraftRecord(vaultKey, operationTarget);

        if (
          draftBeforeStage &&
          current?.value === draftBeforeStage.value &&
          JSON.stringify(current.expected) === JSON.stringify(draftBeforeStage.expected)
        ) {
          capturedExpectedRef.current = undefined;
          clearPersistedEditorDraft(vaultKey, operationTarget);
        } else if (current?.expected) {
          capturedExpectedRef.current = current.expected;
        } else if (!current) {
          capturedExpectedRef.current = undefined;
        }
      };

      try {
        const result = stagedExpected ? onStage(next, stagedExpected) : onStage(next);

        if (result instanceof Promise) void result.then(clearTransferredDraft).catch(() => {});
        else clearTransferredDraft();
      } catch {
        return false;
      }

      // FieldEditor clears the raw draft only after its parent accepts the
      // transfer, so leaf controls must not clear it eagerly.
      return false;
    },
    [capability?.required, expected, label, onStage, operationTarget, vaultKey],
  );

  if (!capability) {
    return (
      <span className="field-editor field-editor--readonly">
        <span>{readOnlyValues(node)}</span>
        {editing ? (
          <small className="field-editor__reason">Schema edit metadata is unavailable.</small>
        ) : null}
      </span>
    );
  }

  if (!capability.writeOperation || capability.readOnlyReason) {
    return (
      <span className="field-editor field-editor--readonly">
        <span>{readOnlyValues(node)}</span>
        {editing && capability.readOnlyReason ? (
          <small className="field-editor__reason">{capability.readOnlyReason}</small>
        ) : null}
      </span>
    );
  }

  let control: ReactNode;

  if (capability.list) {
    control =
      capability.valueKind === "relation" ? (
        <RelationEditor
          values={values}
          links={node.field.links}
          targetType={capability.targetType}
          list
          editing={editing}
          dirty={dirty}
          vaultKey={vaultKey}
          operationTarget={operationTarget}
          required={capability.required}
          ariaLabel={label}
          onDraft={captureExpected}
          onStage={stage}
        />
      ) : (
        <ListEditor
          values={values}
          options={capability.valueKind === "enum" ? capability.enumValues : undefined}
          editing={editing}
          dirty={dirty}
          required={capability.required}
          vaultKey={vaultKey}
          operationTarget={operationTarget}
          ariaLabel={label}
          onDraft={captureExpected}
          onStage={stage}
        />
      );
  } else {
    switch (capability.valueKind) {
      case "enum":
        control = (
          <EnumPillWidget
            value={value}
            options={capability.enumValues}
            optionLabels={capability.enumOptions}
            editing={editing}
            dirty={dirty}
            clearable={!capability.required}
            onStage={(next) => {
              const nextValue = fieldValueFromScalar(next);
              const nextExpected = captureExpected(nextValue, true);
              writePersistedEditorDraft(vaultKey, operationTarget, next, nextExpected);

              if (stage(fieldValueFromScalar(next)))
                clearPersistedEditorDraft(vaultKey, operationTarget);
            }}
            ariaLabel={label}
          />
        );
        break;
      case "date":
      case "datetime":
        control = (
          <DateWidget
            value={value}
            kind={capability.valueKind}
            editing={editing}
            dirty={dirty}
            required={capability.required}
            vaultKey={vaultKey}
            operationTarget={operationTarget}
            onDraft={captureExpected}
            onStage={(next) =>
              stage(next === "" ? { kind: "unset" } : { kind: "scalar", scalar: next })
            }
            ariaLabel={label}
          />
        );
        break;
      case "boolean":
      case "bool":
        control = (
          <BooleanEditor
            value={value}
            editing={editing}
            dirty={dirty}
            required={capability.required}
            vaultKey={vaultKey}
            operationTarget={operationTarget}
            ariaLabel={label}
            onDraft={captureExpected}
            onStage={stage}
          />
        );
        break;
      case "relation":
        control = (
          <RelationEditor
            values={values}
            links={node.field.links}
            targetType={capability.targetType}
            list={false}
            editing={editing}
            dirty={dirty}
            vaultKey={vaultKey}
            operationTarget={operationTarget}
            required={capability.required}
            ariaLabel={label}
            onDraft={captureExpected}
            onStage={stage}
          />
        );
        break;
      case "int":
      case "float":
      case "number":
        control = (
          <NumberEditor
            value={value}
            kind={capability.valueKind}
            editing={editing}
            dirty={dirty}
            required={capability.required}
            vaultKey={vaultKey}
            operationTarget={operationTarget}
            ariaLabel={label}
            onDraft={captureExpected}
            onStage={stage}
          />
        );
        break;
      default:
        control = (
          <InlineTextWidget
            value={value}
            editing={editing}
            dirty={dirty}
            present={node.field.present}
            vaultKey={vaultKey}
            operationTarget={operationTarget}
            placeholder="Empty"
            onStage={(next) => {
              if (stage(fieldValueFromScalar(next)))
                clearPersistedEditorDraft(vaultKey, operationTarget);
            }}
            ariaLabel={label}
            onDraft={captureExpected}
          />
        );
    }
  }

  return (
    <span
      className={`field-editor__validation${dirty ? " is-dirty" : ""}`}
      role={requiredError || dirty ? "group" : undefined}
      aria-describedby={
        [dirty ? changedID : "", requiredError ? errorID : ""].filter(Boolean).join(" ") ||
        undefined
      }
    >
      <Fragment key={rejections}>{control}</Fragment>
      {dirty ? (
        <small id={changedID} className="field-editor__changed" aria-label={`${label} changed`}>
          Changed
        </small>
      ) : null}
      {requiredError ? (
        <small id={errorID} className="field-editor__reason" role="alert">
          {requiredError}
        </small>
      ) : null}
    </span>
  );
}

/** Read-only values; relation values show their targets' titles. */
function readOnlyValues(node: WorkspaceFieldNode): ReactNode {
  const values = node.field.values || [];

  if (node.field.valueKind !== "relation" || values.length === 0) return displayValues(values);

  return <FieldLinkValues links={fieldLinksForValues(values, node.field.links)} />;
}

function fieldValueFromNode(node: WorkspaceFieldNode): OntologyEditFieldValue {
  const values = node.field.values || [];
  const capability = node.field.capability;

  if (!node.field.present) return { kind: "unset" };

  if (capability?.list || capability?.valueKind === "relation" || node.field.valueKind === "list") {
    return { kind: "list", items: values };
  }

  return { kind: "scalar", scalar: values[0] || "" };
}

export function fieldExpectedFromNode(
  node: WorkspaceFieldNode,
  sourceHash?: string,
  sourceContent?: string,
): OntologyEditExpected {
  return { field: fieldValueFromNode(node), sourceHash, sourceContent };
}

function sameFieldValue(left: OntologyEditFieldValue, right: OntologyEditFieldValue): boolean {
  if (left.kind !== right.kind) return false;

  if (left.kind === "unset" || right.kind === "unset") return true;

  if (left.kind === "scalar" && right.kind === "scalar") return left.scalar === right.scalar;

  return (
    left.kind === "list" &&
    right.kind === "list" &&
    left.items.length === right.items.length &&
    left.items.every((item, index) => item === right.items[index])
  );
}

/** Matches the stable identifier used by buildFieldEditOp. */
export function fieldOperationTarget(
  path: string,
  nodeId: string | undefined,
  fieldName: string,
): string {
  return `field:${encodeURIComponent(path)}:${encodeURIComponent(nodeId || "note")}:${encodeURIComponent(fieldName)}`;
}

function isEmptyFieldValue(value: OntologyEditFieldValue): boolean {
  if (value.kind === "unset") return true;

  if (value.kind === "scalar") return value.scalar.trim() === "";

  return value.items.every((item) => item.trim() === "");
}
