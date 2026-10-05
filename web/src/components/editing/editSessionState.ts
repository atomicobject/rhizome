import {
  decodeJson,
  isCallable,
  isFiniteNumber,
  isJsonObject,
  isString,
  isStringArray,
  type JsonValue,
} from "../../api/parse";
import type {
  OntologyEditOp,
  OntologyEditSessionResponse,
  OntologyEditSessionSnapshot,
} from "../../api/types";

export { clearPersistedEditorDrafts } from "./draftStorage";

/** A missing optional outcome succeeds only when the returned session is clean. */
export function editCommitSucceeded(session: OntologyEditSessionResponse) {
  return (
    session.outcome === "committed" ||
    session.outcome === "unchanged" ||
    (!session.outcome && !session.hasUncommittedChanges && session.status !== "conflicted")
  );
}

const STORAGE_KEY_PREFIX = "rhizome:ontology-edit-session:";

const TAB_ID_KEY = "rhizome:ontology-edit-session:tab-id";

function storageKey(vaultKey: string | null): string | null {
  if (typeof window === "undefined") {
    return null;
  }

  if (!vaultKey) return null;
  let tabID = "";

  try {
    tabID = window.sessionStorage.getItem(TAB_ID_KEY) || "";
  } catch {
    return null;
  }

  if (!tabID) {
    tabID = isCallable(globalThis.crypto?.randomUUID) ? crypto.randomUUID() : `tab-${Date.now()}`;

    try {
      window.sessionStorage.setItem(TAB_ID_KEY, tabID);
    } catch {
      return null;
    }
  }

  return `${STORAGE_KEY_PREFIX}${encodeURIComponent(vaultKey)}:${tabID}`;
}

export function toSnapshot(
  session: OntologyEditSessionResponse | StoredEditSnapshot | null,
): OntologyEditSessionSnapshot | undefined {
  if (!session?.sessionId) return undefined;

  return {
    version: 3,
    revision: session.revision || 0,
    sessionId: session.sessionId,
    ops: session.ops || [],
    baseFingerprints: session.baseFingerprints || {},
    baseDocuments: session.baseDocuments || baseDocumentsFromOps(session.ops || []),
  };
}

export function baseDocumentsFromOps(ops: OntologyEditOp[]) {
  const documents = new Map<string, { notePath: string; fingerprint: string; content: string }>();

  for (const op of ops) {
    const fingerprint = op.expected?.sourceHash;
    const content = op.expected?.sourceContent;
    const notePath = op.path.split("#", 1)[0] || op.path;

    if (!fingerprint || content === undefined || documents.has(notePath)) continue;
    documents.set(notePath, { notePath, fingerprint, content });
  }

  return [...documents.values()];
}

export function mergeBaseDocuments(
  existing: OntologyEditSessionResponse["baseDocuments"] = [],
  ops: OntologyEditOp[],
) {
  const merged = new Map((existing || []).map((document) => [document.notePath, document]));

  for (const document of baseDocumentsFromOps(ops)) {
    if (!merged.has(document.notePath)) merged.set(document.notePath, document);
  }

  return [...merged.values()];
}

export function operationKey(op: OntologyEditOp): string {
  if (op.id) return op.id;

  return [op.kind, op.path, op.nodeId || "", op.field || op.collection || ""].join(":");
}

/** A newer replacement starts from the field value this submitted boundary saved. */
export function rebaseCommittedFieldWitnesses(ops: OntologyEditOp[], submitted: OntologyEditOp[]) {
  // ponytail: scan the small saved batch; index targets if large batches make this costly.
  return ops.map((op) => {
    const saved = submitted.find(
      (base) =>
        base.kind === op.kind &&
        base.path === op.path &&
        base.nodeId === op.nodeId &&
        base.structuralFingerprint === op.structuralFingerprint &&
        base.field === op.field,
    );

    if (!saved || !op.expected?.field || (op.kind !== "setField" && op.kind !== "setLinkField"))
      return op;

    const field =
      saved.fieldValue ??
      (saved.kind === "setLinkField" || saved.values !== undefined
        ? { kind: "list" as const, items: saved.values ?? [] }
        : { kind: "scalar" as const, scalar: saved.value ?? "" });

    return { ...op, expected: { ...op.expected, field } };
  });
}

/** Retarget only strong field refs whose survival a successful commit verified. */
export function remapCommittedEditOps(
  ops: OntologyEditOp[],
  lineage: NonNullable<OntologyEditSessionResponse["refLineage"]>,
) {
  return ops.map((op) => {
    if ((op.kind !== "setField" && op.kind !== "setLinkField") || !op.structuralFingerprint)
      return op;
    let mapped = op;

    for (const { original, preview } of lineage) {
      const originalPath = original.fragment
        ? `${original.notePath}#${original.fragment}`
        : original.notePath;

      if (
        mapped.path !== originalPath ||
        (mapped.nodeId ?? "") !== (original.nodeId ?? "") ||
        mapped.structuralFingerprint !== original.structuralFingerprint ||
        !preview.structuralFingerprint ||
        preview.notePath !== original.notePath ||
        preview.kind !== original.kind
      )
        continue;

      if (mapped === op) mapped = { ...op };
      // Keep the local revision key stable even when an authored kit op omitted its ID.
      mapped.id ||= operationKey(op);
      mapped.path = preview.fragment ? `${preview.notePath}#${preview.fragment}` : preview.notePath;
      mapped.nodeId = preview.nodeId;
      mapped.structuralFingerprint = preview.structuralFingerprint;
    }

    return mapped;
  });
}

export function compactOperations(current: OntologyEditOp[], incoming: OntologyEditOp[]) {
  const next = new Map(current.map((op) => [operationKey(op), op]));

  for (const op of incoming) next.set(operationKey(op), op);

  return [...next.values()];
}

export function consolidateSourceOperations(
  current: OntologyEditOp[],
  incoming: OntologyEditOp[],
  baseDocuments: OntologyEditSessionResponse["baseDocuments"],
) {
  let nextCurrent = current;

  const normalized = incoming.map((op) => {
    if (op.kind !== "setSource") return keepReplacedTarget(nextCurrent, op);
    const notePath = op.path.split("#", 1)[0] || op.path;
    nextCurrent = nextCurrent.filter(
      (candidate) => (candidate.path.split("#", 1)[0] || candidate.path) !== notePath,
    );
    const base = baseDocuments?.find((document) => document.notePath === notePath);

    if (!base) return op;

    return {
      ...op,
      previousMarkdown: base.content,
      expected: { ...op.expected, sourceHash: base.fingerprint, sourceContent: base.content },
    };
  });

  return { current: nextCurrent, incoming: normalized };
}

/**
 * A second edit to the same field is read from staged state, where the node
 * carries its preview identity. The session addresses nodes by their committed
 * identity and rejects a reused op id with another target, so the replacement
 * keeps the target and witness of the op it replaces, as the server does.
 */
function keepReplacedTarget(current: OntologyEditOp[], op: OntologyEditOp): OntologyEditOp {
  const replaced = current.find((candidate) => operationKey(candidate) === operationKey(op));

  if (!replaced) return op;

  return {
    ...op,
    path: replaced.path,
    nodeId: replaced.nodeId,
    structuralFingerprint: replaced.structuralFingerprint,
    expected: replaced.expected ?? op.expected,
  };
}

export function cloneOps(ops: OntologyEditOp[]): OntologyEditOp[] {
  return ops.map((op) => ({
    ...op,
    values: op.values ? [...op.values] : undefined,
    orderedFragments: op.orderedFragments ? [...op.orderedFragments] : undefined,
  }));
}

export function newEditRequestID(prefix: string) {
  const suffix = isCallable(globalThis.crypto?.randomUUID)
    ? crypto.randomUUID()
    : `${Date.now()}-${Math.random().toString(16).slice(2)}`;

  return `${prefix}-${suffix}`;
}

export function newEditSessionID() {
  return isCallable(globalThis.crypto?.randomUUID) ? crypto.randomUUID() : `edit-${Date.now()}`;
}

const OP_STRING_FIELDS = [
  "nodeId",
  "structuralFingerprint",
  "field",
  "collection",
  "value",
  "markdown",
  "previousMarkdown",
  "heading",
  "body",
  "blockId",
  "oldTarget",
  "newTarget",
  "property",
  "level",
] as const;

const OP_NUMBER_FIELDS = ["rangeStart", "rangeEnd"] as const;

const OP_STRING_ARRAY_FIELDS = ["values", "orderedFragments"] as const;

function isEditOp(v: JsonValue): v is OntologyEditOp {
  if (!isJsonObject(v) || !isString(v.kind) || !isString(v.path)) return false;

  return (
    OP_STRING_FIELDS.every((key) => v[key] === undefined || isString(v[key])) &&
    OP_NUMBER_FIELDS.every((key) => v[key] === undefined || isFiniteNumber(v[key])) &&
    OP_STRING_ARRAY_FIELDS.every((key) => v[key] === undefined || isStringArray(v[key]))
  );
}

function isFingerprintMap(v: JsonValue): v is Record<string, string> {
  return isJsonObject(v) && Object.values(v).every(isString);
}

function isBaseDocument(v: JsonValue): boolean {
  return isJsonObject(v) && isString(v.notePath) && isString(v.fingerprint) && isString(v.content);
}

export type PendingCommitSubmission = {
  requestId: string;
  expectedRevision: number;
  snapshot: OntologyEditSessionSnapshot;
  boundaryOps: OntologyEditOp[];
  boundaryRevisions: Record<string, number>;
};

function isNumberMap(v: JsonValue): v is Record<string, number> {
  return isJsonObject(v) && Object.values(v).every(isFiniteNumber);
}

function isPendingCommitSubmission(v: unknown): v is PendingCommitSubmission {
  return (
    isJsonObject(v) &&
    isString(v.requestId) &&
    isFiniteNumber(v.expectedRevision) &&
    isStoredSnapshot(v.snapshot) &&
    Array.isArray(v.boundaryOps) &&
    v.boundaryOps.every(isEditOp) &&
    isNumberMap(v.boundaryRevisions)
  );
}

/** Snapshot we previously wrote to localStorage; re-validated because the tab may be stale. */
export type StoredEditSnapshot = OntologyEditSessionSnapshot & {
  pendingCommitRequestId?: string;
  pendingCommit?: PendingCommitSubmission;
  localRevisions?: Record<string, number>;
};

function isStoredSnapshot(v: unknown): v is StoredEditSnapshot {
  return (
    isJsonObject(v) &&
    isString(v.sessionId) &&
    (v.version === undefined || (isFiniteNumber(v.version) && v.version <= 3)) &&
    (v.revision === undefined || isFiniteNumber(v.revision)) &&
    (v.pendingCommitRequestId === undefined || isString(v.pendingCommitRequestId)) &&
    (v.pendingCommit === undefined || isPendingCommitSubmission(v.pendingCommit)) &&
    (v.localRevisions === undefined || isNumberMap(v.localRevisions)) &&
    (v.ops === undefined || (Array.isArray(v.ops) && v.ops.every(isEditOp))) &&
    (v.baseFingerprints === undefined || isFingerprintMap(v.baseFingerprints)) &&
    (v.baseDocuments === undefined ||
      (Array.isArray(v.baseDocuments) && v.baseDocuments.every(isBaseDocument)))
  );
}

export function readStoredSnapshot(vaultKey: string | null): StoredEditSnapshot | null {
  if (typeof window === "undefined") return null;

  try {
    const key = storageKey(vaultKey);

    if (!key) return null;
    const parsed = decodeJson(window.localStorage.getItem(key), isStoredSnapshot);

    if (!parsed?.sessionId) return null;

    return {
      version: parsed.version,
      revision: parsed.revision,
      sessionId: parsed.sessionId,
      ops: parsed.ops || [],
      baseFingerprints: parsed.baseFingerprints || {},
      baseDocuments: parsed.baseDocuments || [],
      pendingCommitRequestId: parsed.pendingCommitRequestId,
      pendingCommit: parsed.pendingCommit,
      localRevisions: parsed.localRevisions || {},
    };
  } catch {
    return null;
  }
}

export function persistSnapshot(
  vaultKey: string | null,
  session: OntologyEditSessionResponse | null,
  pendingCommit?: PendingCommitSubmission,
  localRevisions: Record<string, number> = {},
): string | null {
  if (typeof window === "undefined") return null;

  try {
    const key = storageKey(vaultKey);

    if (!key) {
      return "Browser storage unavailable; edits remain active for this tab.";
    }

    if (!session?.sessionId || !session.hasUncommittedChanges) {
      window.localStorage.removeItem(key);

      return null;
    }

    window.localStorage.setItem(
      key,
      JSON.stringify({
        version: 3,
        revision: session.revision || 0,
        sessionId: session.sessionId,
        ops: session.ops || [],
        baseFingerprints: session.baseFingerprints || {},
        baseDocuments: session.baseDocuments || baseDocumentsFromOps(session.ops || []),
        pendingCommit,
        localRevisions,
      }),
    );

    return null;
  } catch {
    return "Browser storage unavailable; edits remain active for this tab.";
  }
}
