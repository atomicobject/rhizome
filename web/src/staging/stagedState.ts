/**
 * What the edit session stages for a node, answered in one place.
 *
 * The session carries two kinds of evidence: server-reported maps
 * (`changedFieldsByNodeRef`, `changedFieldsByNode`, `touched*`) and the local
 * `ops`, which include edits the server has not acknowledged yet. Screens must
 * not read those fields directly; they describe the node they show as a
 * `StagedTarget` and ask these functions, so every surface matches nodes the
 * same way and marks an edit as soon as it is made.
 */
import type { JsonValue } from "../api/parse";
import type { NodeRef, OntologyEditOp, OntologyEditSessionResponse } from "../api/types";
import { remapCommittedEditOps } from "../components/editing/editSessionState";
import { canonicalNodeRefKey } from "../components/nodeRef";

type Session = OntologyEditSessionResponse | null | undefined;

/**
 * A node as a screen read it: its ref plus any locators it was read at. Pass
 * a bare note path only for a note, since edits key note fields by it.
 */
export type StagedTarget = {
  ref?: NodeRef | null;
  paths?: ReadonlyArray<string | null | undefined>;
};

/** Field names the session changes on this node, including unacknowledged edits. */
export function changedFields(session: Session, target: StagedTarget): Set<string> {
  const changed = new Set<string>();

  for (const key of refKeys(target)) {
    session?.changedFieldsByNodeRef?.[key]?.forEach((field) => changed.add(field));
  }

  for (const key of pathKeys(target)) {
    session?.changedFieldsByNode?.[key]?.forEach((field) => changed.add(field));
  }

  for (const op of session?.ops || []) {
    if (op.field && opTargets(op, target, session?.refLineage ?? [])) changed.add(op.field);
  }

  return changed;
}

/** Whether the session touches this node at all, including source-only edits. */
export function isTouched(session: Session, target: StagedTarget): boolean {
  if (!session) return false;
  const keys = new Set(refKeys(target));

  if (session.touchedNodeRefs?.some((ref) => keys.has(canonicalNodeRefKey(ref)))) return true;
  const touchedPaths = new Set([...(session.touchedNodes || []), ...(session.touchedPaths || [])]);

  if (pathKeys(target).some((path) => touchedPaths.has(path))) return true;

  return (session.ops || []).some((op) => opTargets(op, target, session?.refLineage ?? []));
}

/**
 * Collections (repeated sections, lists) the session changes on this node. A
 * note also owns collection changes addressed to anything inside it.
 */
export function changedCollections(session: Session, target: StagedTarget): Set<string> {
  const changed = new Set<string>();
  const keys = new Set(refKeys(target));
  const paths = pathKeys(target);

  const notePath =
    target.ref?.notePath || splitNodePath(target.paths?.find(Boolean) || "").notePath;

  for (const change of session?.collectionChanges || []) {
    const matches =
      (change.ref && keys.has(canonicalNodeRefKey(change.ref))) ||
      (change.path && paths.includes(change.path)) ||
      (target.ref?.kind === "NOTE" &&
        notePath &&
        (change.ref?.notePath === notePath ||
          splitNodePath(change.path || "").notePath === notePath));

    if (matches && change.collection) changed.add(change.collection);
  }

  return changed;
}

/** The latest value staged for a field on this node, or null when none is staged. */
export function stagedFieldValue(
  session: Session,
  target: StagedTarget,
  field: string,
): { value: JsonValue | undefined } | null {
  let staged: { value: JsonValue | undefined } | null = null;

  for (const op of session?.ops || []) {
    if (
      (op.kind !== "setField" && op.kind !== "setLinkField") ||
      op.field !== field ||
      !opTargets(op, target, session?.refLineage ?? [])
    )
      continue;
    staged = { value: stagedOpValue(op) };
  }

  return staged;
}

/**
 * Session-wide counts for badges and summaries. They include edits made
 * locally that the server has not acknowledged yet, whose server-reported
 * maps are still empty or stale.
 */
export function changeSummary(session: Session) {
  const fieldMap = session?.changedFieldsByNodeRef || session?.changedFieldsByNode || {};
  const serverFieldCount = Object.values(fieldMap).reduce((sum, fields) => sum + fields.length, 0);
  const touchedPaths = new Set(session?.touchedPaths ?? []);
  const localNodes = new Set<string>();
  const localFields = new Set<string>();

  for (const op of session?.ops || []) {
    const path = (op.path || "").trim();
    const notePath = splitNodePath(path).notePath;

    if (notePath) touchedPaths.add(notePath);
    const node = op.nodeId || path;

    if (node) localNodes.add(node);

    if (node && op.field) localFields.add(`${node}\u0000${op.field}`);
  }

  // Server and local identities differ, so take the larger count rather than a
  // union that would count an acknowledged edit twice.
  return {
    nodeCount: Math.max(session?.touchedNodes?.length || 0, localNodes.size),
    fieldCount: Math.max(serverFieldCount, localFields.size),
    touchedPaths,
    opCount: session?.ops?.length ?? 0,
    sourceFileCount: new Set(
      (session?.ops || []).filter((op) => op.kind === "setSource").map((op) => op.path),
    ).size,
  };
}

/**
 * A session state that needs attention beyond "has staged edits", which the
 * staged markers already show: a conflict, a rebase, or an out-of-date read.
 */
export function sessionStateLabel(status: string | undefined): string | null {
  switch (status) {
    case "conflicted":
      return "Conflict";
    case "rebased":
      return "Rebased";
    case "stale":
      return "Out of date";
    default:
      return null;
  }
}

/** The locator an edit operation uses for a node: `note.md` or `note.md#fragment`. */
export function editPathForRef(ref: NodeRef | null | undefined, fallbackPath = "") {
  if (ref?.fragment && ref.notePath) return `${ref.notePath}#${ref.fragment}`;

  return ref?.notePath || fallbackPath;
}

function stagedOpValue(op: OntologyEditOp): JsonValue | undefined {
  switch (op.fieldValue?.kind) {
    case "scalar":
      return op.fieldValue.scalar;
    case "list":
      return op.fieldValue.items ?? [];
    case "unset":
      return undefined;
    default:
      return op.values ?? op.value;
  }
}

// Server maps may key a node with or without its structural fingerprint and kind.
function refKeys({ ref }: StagedTarget) {
  if (!ref) return [];
  const keys = new Set([canonicalNodeRefKey(ref)]);

  if (ref.structuralFingerprint)
    keys.add(canonicalNodeRefKey({ ...ref, structuralFingerprint: "" }));

  if (ref.kind) keys.add(canonicalNodeRefKey({ ...ref, kind: "" }));

  return [...keys].filter(Boolean);
}

function pathKeys(target: StagedTarget) {
  const paths = (target.paths ?? []).filter((path): path is string => Boolean(path));
  const keys = new Set(paths);
  const editPath = editPathForRef(target.ref, paths[0]);

  if (editPath) keys.add(editPath);

  return [...keys];
}

function opTargets(
  op: OntologyEditOp,
  target: StagedTarget,
  lineage: NonNullable<OntologyEditSessionResponse["refLineage"]>,
) {
  op = remapCommittedEditOps([op], lineage)[0];
  let ref = target.ref;

  for (const { original, preview } of lineage) {
    if (ref?.structuralFingerprint && canonicalNodeRefKey(ref) === canonicalNodeRefKey(original)) {
      ref = preview;
      target = { ref, paths: [editPathForRef(ref)] };
    }
  }

  const opPath = (op.path || "").trim();

  if (opPath && pathKeys(target).includes(opPath)) return true;
  const { notePath, fragment } = splitNodePath(opPath);
  const targetNotePath = ref?.notePath || target.paths?.find(Boolean) || "";

  if (notePath && targetNotePath && notePath !== targetNotePath) return false;

  if (op.nodeId && ref?.nodeId && op.nodeId === ref.nodeId) return true;

  if (
    op.structuralFingerprint &&
    ref?.structuralFingerprint &&
    op.structuralFingerprint === ref.structuralFingerprint
  ) {
    return true;
  }

  return Boolean(fragment && ref?.fragment && fragment === ref.fragment);
}

function splitNodePath(path: string) {
  const index = path.indexOf("#");

  if (index < 0) return { notePath: path, fragment: "" };

  return { notePath: path.slice(0, index), fragment: path.slice(index + 1) };
}
