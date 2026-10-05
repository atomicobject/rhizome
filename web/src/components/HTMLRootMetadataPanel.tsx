import type { ReactNode } from "react";
import type {
  NodeCapabilities,
  NodeFieldCapability,
  NodeRef,
  NodeStatus,
  NodeWorkspace,
  OntologyEditFieldValue,
  OntologyEditOp,
  RenderedFile,
  WorkspaceFieldNode,
} from "../api/types";
import { FieldEditor, fieldExpectedFromNode, fieldOperationTarget } from "./editing/FieldEditor";
import { buildFieldEditOp } from "./ontologyFieldPicking";

type HTMLRootFieldName = "title" | "type" | "tags" | "aliases";

type HTMLRootMetadata = {
  title: { value: string; present: boolean };
  type: { value: string; present: boolean };
  tags: { values: string[]; present: boolean };
  aliases: { values: string[]; present: boolean };
};

type HTMLRootSourceRevision = NonNullable<NodeWorkspace["sourceRevision"]>;

/* oxlint-disable anti-slop/no-runtime-typeof, anti-slop/no-unsafe-dictionary-type, anti-slop/no-unknown-parameters -- This function parses the generated JSON object at the API boundary. */
function parseHTMLRootMetadata(frontmatter: RenderedFile["frontmatter"]): HTMLRootMetadata {
  const value = frontmatter || {};

  const scalar = (key: "title" | "type") => ({
    value: typeof value[key] === "string" ? value[key] : "",
    present: typeof value[key] === "string",
  });

  const list = (key: "tags" | "aliases") => {
    const candidate = value[key];

    return {
      values: Array.isArray(candidate)
        ? candidate.filter((item): item is string => typeof item === "string")
        : [],
      // An empty authored list is different from an absent metadata field.
      present: Array.isArray(candidate),
    };
  };

  return {
    title: scalar("title"),
    type: scalar("type"),
    tags: list("tags"),
    aliases: list("aliases"),
  };
}
/* oxlint-enable anti-slop/no-runtime-typeof, anti-slop/no-unsafe-dictionary-type, anti-slop/no-unknown-parameters */

function emptyNodeStatus(): NodeStatus {
  return {
    dirty: false,
    validation: { issueCount: 0 },
    freshness: {},
    session: {},
    hasWarnings: false,
  };
}

function htmlRootNodeCapabilities(): NodeCapabilities {
  return {
    canEdit: true,
    canEditFields: true,
    canEditCollections: false,
    canNavigateChildren: false,
    canSubscribe: false,
  };
}

function htmlRootFieldCapability(ref: NodeRef, list: boolean): NodeFieldCapability {
  return {
    ownerRef: ref,
    ownerType: "HTMLRoot",
    typeName: "String",
    valueKind: "text",
    list,
    required: false,
    enumValues: [],
    sourceKind: "root_metadata",
    valueOrigin: "authored",
    identifier: false,
    preferredIdentifier: false,
    displayImportance: "NORMAL",
    writeOperation: list ? "setRootMetadataList" : "setField",
  };
}

function htmlRootFieldNode(
  path: string,
  name: HTMLRootFieldName,
  values: string[],
  present: boolean,
): WorkspaceFieldNode {
  const ref: NodeRef = { notePath: path, kind: "NOTE" };
  const list = name === "tags" || name === "aliases";

  return {
    id: `field|html-root|${name}`,
    kind: "field",
    ref,
    notePath: path,
    status: emptyNodeStatus(),
    capabilities: htmlRootNodeCapabilities(),
    field: {
      name,
      valueKind: list ? "list" : "scalar",
      typeName: "String",
      present,
      values,
      range: { start: 0, end: 0 },
      capability: htmlRootFieldCapability(ref, list),
    },
  };
}

function stageHTMLRootField(
  path: string,
  node: WorkspaceFieldNode,
  value: OntologyEditFieldValue,
  sourceRevision: HTMLRootSourceRevision | undefined,
  expected?: OntologyEditOp["expected"],
  onStageOps?: (ops: OntologyEditOp[]) => Promise<void> | undefined,
) {
  if (!onStageOps) return;

  return onStageOps([
    buildFieldEditOp(
      path,
      undefined,
      node,
      value,
      sourceRevision?.contentFingerprint,
      sourceRevision?.content,
      expected,
    ),
  ]);
}

export function HTMLRootMetadataPanel({
  path,
  fallbackTitle,
  frontmatter,
  existingFields,
  editing,
  dirtyFields,
  vaultKey = null,
  sourceRevision,
  onStageOps,
  children,
}: {
  children?: ReactNode;
  path: string;
  fallbackTitle: string;
  frontmatter: RenderedFile["frontmatter"];
  existingFields: Set<string>;
  editing: boolean;
  dirtyFields: Set<string>;
  vaultKey?: string | null;
  sourceRevision?: HTMLRootSourceRevision;
  onStageOps?: (ops: OntologyEditOp[]) => Promise<void> | undefined;
}) {
  const metadata = parseHTMLRootMetadata(frontmatter);

  const fields: Array<{
    key: HTMLRootFieldName;
    values: string[];
    present: boolean;
  }> = [];

  if (!existingFields.has("title")) {
    fields.push({
      key: "title",
      // Keep the derived filename title visible while retaining an unset
      // witness for a missing authored title below.
      values: metadata.title.present
        ? [metadata.title.value]
        : fallbackTitle
          ? [fallbackTitle]
          : [],
      present: metadata.title.present || fallbackTitle !== "",
    });
  }

  fields.push({
    key: "type",
    values: metadata.type.present ? [metadata.type.value] : [],
    present: metadata.type.present,
  });

  if (!existingFields.has("tags")) {
    fields.push({
      key: "tags",
      values: metadata.tags.values,
      present: metadata.tags.present,
    });
  }

  fields.push({
    key: "aliases",
    values: metadata.aliases.values,
    present: metadata.aliases.present,
  });

  return (
    <details key={path} className="ontology-properties ontology-properties--html-root">
      <summary className="ontology-properties__header ontology-properties__html-summary">
        <span className="ontology-properties__label">HTML metadata</span>
      </summary>
      {children}
      <dl className="ontology-properties__grid">
        {fields.map((field) => {
          const node = htmlRootFieldNode(path, field.key, field.values, field.present);

          const expected = fieldExpectedFromNode(
            node,
            sourceRevision?.contentFingerprint,
            sourceRevision?.content,
          );

          // The fallback title is derived from the file name and is not an
          // authored witness. A changed title therefore inserts metadata.
          if (field.key === "title" && !metadata.title.present) {
            expected.field = { kind: "unset" };
          }

          const dirty = dirtyFields.has(field.key);
          const operationTarget = fieldOperationTarget(path, undefined, field.key);

          return (
            <div
              key={field.key}
              data-field={field.key}
              className={`ontology-properties__row${dirty ? " is-dirty" : ""}${field.present ? "" : " is-missing"}`}
            >
              <dt className="ontology-properties__key">{field.key}</dt>
              <dd className="ontology-properties__value">
                <FieldEditor
                  node={node}
                  editing={editing}
                  dirty={dirty}
                  vaultKey={vaultKey}
                  operationTarget={operationTarget}
                  expected={expected}
                  onStage={(value, stagedExpected) =>
                    stageHTMLRootField(
                      path,
                      node,
                      value,
                      sourceRevision,
                      stagedExpected,
                      onStageOps,
                    )
                  }
                />
              </dd>
            </div>
          );
        })}
      </dl>
    </details>
  );
}
