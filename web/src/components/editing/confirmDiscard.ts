import { hasPersistedEditorDrafts } from "./draftStorage";

export function confirmDiscard(
  { opCount, touchedPaths }: { opCount: number; touchedPaths: Set<string> },
  vaultKey: string | null,
) {
  if (opCount === 0 && !hasPersistedEditorDrafts(vaultKey)) return true;

  return window.confirm(
    opCount > 0
      ? `Discard ${opCount} staged change${opCount === 1 ? "" : "s"} to ${touchedPaths.size || 1} note${touchedPaths.size === 1 ? "" : "s"}? This cannot be undone.`
      : "Discard local drafts? This cannot be undone.",
  );
}
