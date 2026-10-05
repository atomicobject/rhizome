import type {
  NodeFieldCapability,
  OntologyEditFieldValue,
  OntologyEditOp,
  WorkspaceFieldNode,
} from "../api/types";

/**
 * Shared helpers for splitting a workspace's field nodes between the identity
 * strip (well-known identity-bearing fields) and the property panel (all
 * other typed fields). Lives outside the React components so both consumers
 * agree on the partition without prop-drilling the schema heuristics.
 */

export type IdentityFields = {
  identifier: WorkspaceFieldNode | null;
  status: WorkspaceFieldNode | null;
  version: WorkspaceFieldNode | null;
  lastUpdated: WorkspaceFieldNode | null;
  /** Fields not consumed by the identity strip. */
  others: WorkspaceFieldNode[];
};

/**
 * True when the field's schema type is a date-like scalar. The `Date` and
 * `DateTime` scalars are how the spec-driven starter declares dates; other
 * vaults may add their own, in which case the typeName falls back to the
 * inline-text widget. We intentionally do NOT inspect raw values to avoid
 * mis-detecting a string that "looks like" a date.
 */
function isDateOnlyField(field: WorkspaceFieldNode): boolean {
  return field.field.capability?.valueKind === "date" || field.field.typeName === "Date";
}

export function isDateTimeField(field: WorkspaceFieldNode): boolean {
  return field.field.capability?.valueKind === "datetime" || field.field.typeName === "DateTime";
}

export function isDateField(field: WorkspaceFieldNode): boolean {
  return isDateOnlyField(field) || isDateTimeField(field);
}

/**
 * True when the field's schema type names an ontology enum (i.e. backend
 * supplied `enumValues`). Empty enumValues means "not an enum" rather than
 * "no constraints" — the schema would have populated them.
 */
export function isEnumField(field: WorkspaceFieldNode): boolean {
  return field.field.capability?.valueKind === "enum" || (field.field.enumValues?.length ?? 0) > 0;
}

/**
 * Partition a workspace's field nodes into the identity slots + everything
 * else. Heuristics, not contracts:
 *
 * - identifier: field literally named `id`. The starter ontology keys all
 *   spec-likes off `@identifier(preferred:true)` on `id`, so this matches in
 *   practice. If a vault uses a different convention we'd surface it via the
 *   property panel instead.
 * - status: enum field whose name ends in "status" (case-insensitive).
 * - version: field literally named `version`.
 * - lastUpdated: first date-typed field whose name suggests last-updated
 *   semantics (`lastUpdated`, `updatedAt`, `date`, `last-updated`).
 *
 * Everything else lands in `others` for the property panel.
 */
export function pickIdentityFields(fields: WorkspaceFieldNode[]): IdentityFields {
  let identifier: WorkspaceFieldNode | null = null;
  let status: WorkspaceFieldNode | null = null;
  let version: WorkspaceFieldNode | null = null;
  let lastUpdated: WorkspaceFieldNode | null = null;
  const others: WorkspaceFieldNode[] = [];

  const statusFallback = fields.find((field) => {
    const name = (field.field.name || "").toLowerCase();

    return isEnumField(field) && name.endsWith("status");
  });

  const statusCandidate = statusFallback
    ? fields.find((field) => {
        const name = (field.field.name || "").toLowerCase();

        return (
          isEnumField(field) &&
          name.endsWith("status") &&
          effectiveFieldCapability(field).displayImportance === "KEY"
        );
      }) || statusFallback
    : null;

  for (const field of fields) {
    const name = (field.field.name || "").toLowerCase();

    if (!identifier && (field.field.capability?.preferredIdentifier || name === "id")) {
      identifier = field;
      continue;
    }

    if (!status && field === statusCandidate) {
      status = field;
      continue;
    }

    if (!version && name === "version") {
      version = field;
      continue;
    }

    if (
      !lastUpdated &&
      isDateField(field) &&
      (name === "lastupdated" || name === "updatedat" || name === "date")
    ) {
      lastUpdated = field;
      continue;
    }

    others.push(field);
  }

  return { identifier, status, version, lastUpdated, others };
}

function fieldValue(values: string[], list: boolean, present: boolean): OntologyEditFieldValue {
  if (!present) return { kind: "unset" };

  if (list) return { kind: "list", items: values };

  return { kind: "scalar", scalar: values[0] ?? "" };
}

function stableFieldOperationID(path: string, nodeId: string | undefined, fieldName: string) {
  return `field:${encodeURIComponent(path)}:${encodeURIComponent(nodeId || "note")}:${encodeURIComponent(fieldName)}`;
}

export function buildFieldEditOp(
  notePath: string,
  nodeId: string | undefined,
  node: WorkspaceFieldNode,
  desired: OntologyEditFieldValue,
  sourceFingerprint?: string,
  sourceContent?: string,
  expectedOverride?: OntologyEditOp["expected"],
): OntologyEditOp {
  const capability = node.field.capability;
  const list = capability?.list ?? node.field.valueKind === "list";
  const relation = capability?.valueKind === "relation";
  const currentValues = node.field.values || [];
  // Link edits use list-shaped witnesses even when schema cardinality is one;
  // the edit transport and replay layer represent every relation as targets.
  const expected = fieldValue(currentValues, list || relation, node.field.present);

  const values =
    desired.kind === "list" ? desired.items : desired.kind === "scalar" ? [desired.scalar] : [];

  return {
    id: stableFieldOperationID(notePath, nodeId, node.field.name),
    kind: capability?.writeOperation || (relation ? "setLinkField" : "setField"),
    path: notePath,
    nodeId,
    field: node.field.name,
    value: !list ? (values[0] ?? "") : undefined,
    values: list || relation ? values : undefined,
    fieldValue: desired,
    expected: expectedOverride ?? {
      field: expected,
      sourceHash: sourceFingerprint,
      sourceContent,
    },
  };
}

export function effectiveFieldCapability(
  node: WorkspaceFieldNode,
): NodeFieldCapability & { displayImportance: "KEY" | "NORMAL" | "DETAIL" } {
  const capability = node.field.capability;

  if (capability) {
    return { ...capability, displayImportance: capability.displayImportance || "NORMAL" };
  }

  return {
    ownerRef: node.ref,
    ownerType: node.ref.typeName || "",
    typeName: node.field.typeName || "String",
    valueKind: isEnumField(node)
      ? "enum"
      : isDateTimeField(node)
        ? "datetime"
        : isDateOnlyField(node)
          ? "date"
          : "text",
    list: node.field.valueKind === "list",
    required: false,
    enumValues: node.field.enumValues || [],
    valueOrigin: "authored",
    identifier: node.field.name.toLowerCase() === "id",
    preferredIdentifier: node.field.name.toLowerCase() === "id",
    displayImportance: "NORMAL",
    writeOperation: "setField",
  };
}
