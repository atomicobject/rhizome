import type {
  NodeWorkspace,
  OntologyEditFieldValue,
  OntologyEditOp,
  ValidationHealth,
  WorkspaceFieldNode,
} from "../api/types";
import { FieldEditor, fieldExpectedFromNode, fieldOperationTarget } from "./editing/FieldEditor";
import { buildFieldEditOp, type IdentityFields } from "./ontologyFieldPicking";
import { ValidationIssueBadge } from "./validation/ValidationIssueBadge";
import { displayTitle } from "../lib/labels";

type Props = {
  workspace: NodeWorkspace;
  /** Result of pickIdentityFields applied to the same workspace. */
  identity: IdentityFields;
  editing: boolean;
  /** Field names that have unsaved changes in the active session. */
  changedFieldNames: Set<string>;
  /** Render the issue chip when positive. */
  issueCount?: number;
  validationHealth?: ValidationHealth;
  /** Render the dirty chip when true. */
  dirty: boolean;
  /** Stage one or more ops against the active session; the result is not read here. */
  onStageOps?: (ops: OntologyEditOp[]) => Promise<void> | void;
  vaultKey?: string | null;
  onOpenIssues?: () => void;
};

/**
 * Top strip of the structural view. It condenses the identity-bearing facts
 * — type badge, preferred identifier, status pill, version, last-updated —
 * into a single row above the property panel. Clicking any widget in edit
 * mode stages a setField op via onStageOps; all ops go through the same
 * builder so the identity strip and the property panel stay interchangeable.
 */
export function OntologyIdentityStrip({
  workspace,
  identity,
  editing,
  changedFieldNames,
  issueCount,
  validationHealth,
  dirty,
  onStageOps,
  vaultKey = null,
  onOpenIssues,
}: Props) {
  const title = displayTitle(workspace.content.title || workspace.node.title);

  const path = workspace.node.ref.fragment
    ? `${workspace.node.ref.notePath}#${workspace.node.ref.fragment}`
    : workspace.node.notePath || workspace.content.path;

  const nodeId = workspace.node.ref.nodeId;

  const stageFieldOp = (
    node: WorkspaceFieldNode,
    value: OntologyEditFieldValue,
    expected?: OntologyEditOp["expected"],
  ) => {
    if (!onStageOps) return;

    return onStageOps([
      buildFieldEditOp(
        path,
        nodeId,
        node,
        value,
        workspace.sourceRevision?.contentFingerprint,
        workspace.sourceRevision?.content,
        expected,
      ),
    ]);
  };

  return (
    <header className="ontology-identity">
      <h2 className="ontology-identity__title">{title || "Untitled"}</h2>
      <div className="ontology-identity__row">
        {identity.identifier && (
          <FieldSlot
            node={identity.identifier}
            editing={editing}
            changedFieldNames={changedFieldNames}
            vaultKey={vaultKey}
            operationTarget={fieldOperationTarget(path, nodeId, identity.identifier.field.name)}
            sourceHash={workspace.sourceRevision?.contentFingerprint}
            sourceContent={workspace.sourceRevision?.content}
            onStage={(values, expected) => stageFieldOp(identity.identifier!, values, expected)}
          />
        )}
        {identity.version && (
          <FieldSlot
            node={identity.version}
            editing={editing}
            changedFieldNames={changedFieldNames}
            vaultKey={vaultKey}
            operationTarget={fieldOperationTarget(path, nodeId, identity.version.field.name)}
            sourceHash={workspace.sourceRevision?.contentFingerprint}
            sourceContent={workspace.sourceRevision?.content}
            onStage={(values, expected) => stageFieldOp(identity.version!, values, expected)}
            prefix="v"
          />
        )}
        {identity.status && (
          <FieldSlot
            node={identity.status}
            editing={editing}
            changedFieldNames={changedFieldNames}
            vaultKey={vaultKey}
            operationTarget={fieldOperationTarget(path, nodeId, identity.status.field.name)}
            sourceHash={workspace.sourceRevision?.contentFingerprint}
            sourceContent={workspace.sourceRevision?.content}
            onStage={(values, expected) => stageFieldOp(identity.status!, values, expected)}
          />
        )}
        {identity.lastUpdated && (
          <FieldSlot
            node={identity.lastUpdated}
            editing={editing}
            changedFieldNames={changedFieldNames}
            vaultKey={vaultKey}
            operationTarget={fieldOperationTarget(path, nodeId, identity.lastUpdated.field.name)}
            sourceHash={workspace.sourceRevision?.contentFingerprint}
            sourceContent={workspace.sourceRevision?.content}
            onStage={(values, expected) => stageFieldOp(identity.lastUpdated!, values, expected)}
            labelPrefix="Updated"
          />
        )}
        <span className="ontology-identity__flex" />
        <ValidationIssueBadge
          count={issueCount}
          health={validationHealth}
          showUnit
          label={
            issueCount == null
              ? `Validation status for ${title || "this note"}`
              : `Open ${issueCount} ${issueCount === 1 ? "issue" : "issues"} for this note`
          }
          onClick={onOpenIssues}
        />
        {dirty && (
          <span className="ontology-identity__chip ontology-identity__chip--dirty">Staged</span>
        )}
      </div>
    </header>
  );
}

/**
 * Render one identity-slot field with the right widget. A "slot" is just a
 * container that picks between enum / date / text based on the node's schema
 * metadata; the surrounding layout stays uniform so slots can be reordered
 * cheaply.
 */
function FieldSlot({
  node,
  editing,
  changedFieldNames,
  vaultKey,
  operationTarget,
  sourceHash,
  sourceContent,
  onStage,
  prefix = "",
  labelPrefix,
}: {
  node: WorkspaceFieldNode;
  editing: boolean;
  changedFieldNames: Set<string>;
  vaultKey: string | null;
  operationTarget: string;
  sourceHash?: string;
  sourceContent?: string;
  onStage: (
    value: OntologyEditFieldValue,
    expected?: OntologyEditOp["expected"],
  ) => Promise<void> | void;
  /** Static prefix rendered inline with the value ("v1.2"). */
  prefix?: string;
  /** Label shown as a faint prefix tag ("Updated · 2026-04-12"). */
  labelPrefix?: string;
}) {
  const { name } = node.field;
  const values = node.field.values || [];
  const value = values[0] || "";
  const dirty = changedFieldNames.has(name);

  return (
    <span className={`ontology-identity__slot ontology-identity__slot--${name}`} data-field={name}>
      {labelPrefix && <span className="ontology-identity__slot-label">{labelPrefix}</span>}
      {prefix && !editing && value && (
        <span className="ontology-identity__slot-prefix">{prefix}</span>
      )}
      <FieldEditor
        node={node}
        editing={editing}
        dirty={dirty}
        vaultKey={vaultKey}
        operationTarget={operationTarget}
        expected={fieldExpectedFromNode(node, sourceHash, sourceContent)}
        onStage={onStage}
      />
    </span>
  );
}
