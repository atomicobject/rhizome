import { isCallable, isJsonObject, isString, isStringArray } from "../../api/parse";
import type { OntologyEditFieldValue } from "../../api/types";

const EDITOR_DRAFT_KEY_PREFIX = "rhizome:edit-draft:";

const EDITOR_TAB_ID_KEY = "rhizome:ontology-edit-session:tab-id";

export const EDITOR_DRAFTS_CHANGED_EVENT = "rhizome:editor-drafts-changed";

export const EDITOR_DRAFT_CLEARED_EVENT = "rhizome:editor-draft-cleared";

/** The server refused an edit; the editor for `detail.operationTarget` resets to the saved value. */
export const EDITOR_REJECTED_EVENT = "rhizome:editor-rejected";

export type OntologyEditExpected = {
  field?: OntologyEditFieldValue;
  sourceHash?: string;
  sourceContent?: string;
};

export type PersistedEditorDraft = {
  version: 1;
  value: string;
  expected?: OntologyEditExpected;
  /** Bare pre-versioned values can be displayed, but have no safe witness. */
  legacy?: boolean;
};

/**
 * Drafts belong to the vault and browser tab that created them. Keeping the
 * owner in the key prevents a discard in one vault or tab from deleting work
 * that belongs to another.
 */
export function editorDraftStorageKey(
  vaultKey: string | null,
  operationTarget: string,
): string | null {
  const owner = editorDraftOwner(vaultKey);

  if (!owner || !operationTarget) return null;

  return `${EDITOR_DRAFT_KEY_PREFIX}${encodeURIComponent(owner.vaultKey)}:${encodeURIComponent(owner.tabID)}:${encodeURIComponent(operationTarget)}`;
}

export function readPersistedEditorDraft(
  vaultKey: string | null,
  operationTarget: string,
): string | null {
  return readPersistedEditorDraftRecord(vaultKey, operationTarget)?.value ?? null;
}

export function readPersistedEditorDraftRecord(
  vaultKey: string | null,
  operationTarget: string,
): PersistedEditorDraft | null {
  const key = editorDraftStorageKey(vaultKey, operationTarget);

  if (!key) return null;

  try {
    const raw = window.localStorage.getItem(key);

    if (raw === null) return null;

    try {
      const parsed: unknown = JSON.parse(raw);

      if (isPersistedEditorDraft(parsed)) return parsed;
    } catch {
      // Legacy drafts were stored as raw strings. Keep them recoverable while
      // withholding a witness until the user explicitly edits them again.
    }

    return { version: 1, value: raw, legacy: true };
  } catch {
    return null;
  }
}

export function writePersistedEditorDraft(
  vaultKey: string | null,
  operationTarget: string,
  value: string,
  expected?: OntologyEditExpected,
): void {
  const key = editorDraftStorageKey(vaultKey, operationTarget);

  if (!key) return;

  try {
    const previous = readPersistedEditorDraftRecord(vaultKey, operationTarget);
    const preservedExpected = previous?.legacy ? undefined : previous?.expected;
    window.localStorage.setItem(
      key,
      JSON.stringify({
        version: 1,
        value,
        expected: preservedExpected || expected,
      }),
    );
    window.dispatchEvent(new Event(EDITOR_DRAFTS_CHANGED_EVENT));
  } catch {
    // The edit session warning covers browsers that deny local storage.
  }
}

function isPersistedEditorDraft(value: unknown): value is PersistedEditorDraft {
  if (!isJsonObject(value) || value.version !== 1 || !isString(value.value)) return false;

  if (value.expected === undefined) return true;

  if (!isJsonObject(value.expected)) return false;

  return (
    (value.expected.field === undefined || isExpectedFieldValue(value.expected.field)) &&
    (value.expected.sourceHash === undefined || isString(value.expected.sourceHash)) &&
    (value.expected.sourceContent === undefined || isString(value.expected.sourceContent))
  );
}

function isExpectedFieldValue(value: unknown): value is OntologyEditFieldValue {
  if (!isJsonObject(value) || !isString(value.kind)) return false;

  if (value.kind === "unset") return true;

  if (value.kind === "scalar") return isString(value.scalar);

  return value.kind === "list" && isStringArray(value.items);
}

export function clearPersistedEditorDraft(vaultKey: string | null, operationTarget: string): void {
  window.dispatchEvent(
    new CustomEvent(EDITOR_DRAFT_CLEARED_EVENT, { detail: { vaultKey, operationTarget } }),
  );
  const key = editorDraftStorageKey(vaultKey, operationTarget);

  if (!key) return;

  try {
    window.localStorage.removeItem(key);
    window.dispatchEvent(new Event(EDITOR_DRAFTS_CHANGED_EVENT));
  } catch {
    // Ignore cleanup when storage is unavailable.
  }
}

export function clearPersistedEditorDrafts(vaultKey: string | null): void {
  const owner = editorDraftOwner(vaultKey);

  if (!owner) return;
  const prefix = `${EDITOR_DRAFT_KEY_PREFIX}${encodeURIComponent(owner.vaultKey)}:${encodeURIComponent(owner.tabID)}:`;

  try {
    const keys = Array.from({ length: window.localStorage.length }, (_, index) =>
      window.localStorage.key(index),
    ).filter(
      (key): key is string =>
        Boolean(key?.startsWith(prefix)) || isLegacyEditorDraftStorageKey(key),
    );

    for (const key of keys) window.localStorage.removeItem(key);
    window.dispatchEvent(new Event(EDITOR_DRAFTS_CHANGED_EVENT));
  } catch {
    // Active editor listeners still clear their own draft when storage is partial.
  }
}

export function hasPersistedEditorDrafts(vaultKey: string | null): boolean {
  const owner = editorDraftOwner(vaultKey);

  if (!owner) return false;
  const prefix = `${EDITOR_DRAFT_KEY_PREFIX}${encodeURIComponent(owner.vaultKey)}:${encodeURIComponent(owner.tabID)}:`;

  try {
    return Array.from({ length: window.localStorage.length }, (_, index) =>
      window.localStorage.key(index),
    ).some((key) => Boolean(key?.startsWith(prefix)) || isLegacyEditorDraftStorageKey(key));
  } catch {
    return false;
  }
}

function isLegacyEditorDraftStorageKey(key: string | null): boolean {
  if (!key?.startsWith(EDITOR_DRAFT_KEY_PREFIX)) return false;
  const suffix = key.slice(EDITOR_DRAFT_KEY_PREFIX.length);

  return suffix.split(":").length < 3;
}

function editorDraftOwner(vaultKey: string | null): { vaultKey: string; tabID: string } | null {
  if (typeof window === "undefined" || !vaultKey) return null;

  try {
    let tabID = window.sessionStorage.getItem(EDITOR_TAB_ID_KEY) || "";

    if (!tabID) {
      tabID = isCallable(globalThis.crypto?.randomUUID)
        ? globalThis.crypto.randomUUID()
        : `tab-${Date.now()}`;
      window.sessionStorage.setItem(EDITOR_TAB_ID_KEY, tabID);
    }

    return { vaultKey, tabID };
  } catch {
    return null;
  }
}
