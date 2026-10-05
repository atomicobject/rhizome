import { useState } from "react";

import { changeSummary } from "../staging/stagedState";
import type { OntologyEditSessionResponse } from "../api/types";

export function OntologyEditSessionPanel({
  session,
  editing,
  busy,
  saving,
  error,
  warnings,
  notice,
  hasLocalDrafts = false,
  onStartEditing,
  onReview,
  onCommit,
  onDiscard,
}: {
  session: OntologyEditSessionResponse | null;
  editing: boolean;
  busy?: boolean;
  saving?: boolean;
  error?: string | null;
  warnings?: string[];
  notice?: string | null;
  hasLocalDrafts?: boolean;
  onStartEditing: () => void;
  onReview: () => void;
  onCommit: () => void;
  onDiscard: () => void;
}) {
  const { nodeCount, touchedPaths, opCount } = changeSummary(session);
  const noteCount = touchedPaths.size || nodeCount;
  const conflicts = session?.conflicts?.length || 0;
  const conflicted = session?.status === "conflicted";
  const hasStagedChanges = Boolean(session?.hasUncommittedChanges);
  const hasChanges = hasStagedChanges || hasLocalDrafts;

  // One state at a time, most urgent first. Only errors and conflicts use the
  // accent; staged work is gold; editing with nothing staged stays neutral.
  const [lifecycle, tone] = error
    ? ["Error", "error"]
    : conflicted
      ? ["Conflict", "error"]
      : saving
        ? ["Saving…", "dirty"]
        : busy
          ? ["Staging…", hasChanges ? "dirty" : "idle"]
          : session?.status === "rebased"
            ? ["Rebased", "dirty"]
            : hasStagedChanges
              ? ["Ready to save", "dirty"]
              : hasLocalDrafts
                ? ["Draft", "dirty"]
                : ["Editing", "idle"];

  const messageKey = JSON.stringify([
    error,
    session?.conflicts?.map((conflict) => conflict.message),
    warnings,
  ]);

  // Dismissing hides these messages until they change; the label keeps the state.
  const [dismissedKey, setDismissedKey] = useState("");

  const hasMessages =
    (Boolean(error) || conflicts > 0 || Boolean(warnings?.length)) && messageKey !== dismissedKey;

  if (!editing) {
    return (
      <div className="ontology-edit-toggle-group">
        {notice ? (
          <span className="ontology-edit-notice" role="status" aria-live="polite">
            {notice}
          </span>
        ) : null}
        <button
          type="button"
          className="ontology-edit-toggle"
          onClick={onStartEditing}
          title="Start editing session (Ctrl/Command+Shift+E)"
        >
          <svg
            viewBox="0 0 16 16"
            fill="none"
            aria-hidden="true"
            className="ontology-edit-toggle__icon"
          >
            <path
              d="M11.5 1.5l3 3L5 14H2v-3L11.5 1.5z"
              stroke="currentColor"
              strokeWidth="1.4"
              strokeLinejoin="round"
            />
          </svg>
          Edit
        </button>
      </div>
    );
  }

  return (
    <div className={`ontology-edit-bar is-${tone}`}>
      <div className="ontology-edit-bar__label" role="status" aria-live="polite">
        <span className="ontology-edit-bar__dot" aria-hidden="true" />
        {lifecycle}
      </div>
      {hasStagedChanges && (
        <div className="ontology-edit-bar__stats">
          <span>
            {noteCount} note{noteCount === 1 ? "" : "s"}
          </span>
          <span className="ontology-edit-bar__sep">&middot;</span>
          <span>
            {opCount} change{opCount === 1 ? "" : "s"}
          </span>
          {conflicts > 0 && (
            <>
              <span className="ontology-edit-bar__sep">&middot;</span>
              <span className="ontology-edit-bar__conflicts">
                {conflicts} conflict{conflicts === 1 ? "" : "s"}
              </span>
            </>
          )}
        </div>
      )}
      <div className="ontology-edit-bar__actions">
        <button
          type="button"
          className="ontology-edit-bar__btn"
          onClick={onReview}
          disabled={!hasStagedChanges}
          title="Open the Modified home (Ctrl/Command+Shift+Enter)"
        >
          Review
        </button>
        <button
          type="button"
          className="ontology-edit-bar__btn ontology-edit-bar__btn--save"
          onMouseDown={(event) => event.preventDefault()}
          onClick={onCommit}
          disabled={!hasChanges || busy || conflicted}
          title={
            conflicted
              ? "Resolve conflicts in Review before saving"
              : "Save changes (Ctrl/Command+S)"
          }
        >
          Save
        </button>
        <button
          type="button"
          className="ontology-edit-bar__btn ontology-edit-bar__btn--discard"
          onMouseDown={(event) => event.preventDefault()}
          onClick={onDiscard}
          disabled={busy}
        >
          {hasChanges ? "Discard" : "Done"}
        </button>
      </div>
      {hasMessages && (
        // Floats below the bar: the tab row has a fixed height, so wrapping
        // messages inside it clipped them and pushed Save off screen.
        <div className="ontology-edit-bar__messages">
          <button
            type="button"
            className="ontology-edit-bar__dismiss"
            aria-label="Dismiss messages"
            onClick={() => setDismissedKey(messageKey)}
          >
            ×
          </button>
          {error && (
            <div className="ontology-edit-bar__error" role="alert">
              {error}
            </div>
          )}
          {conflicts > 0 && (
            <div className="ontology-edit-bar__conflict-details" role="alert">
              {session?.conflicts?.map((conflict) => (
                <div
                  key={`${conflict.notePath}:${conflict.nodeRef || ""}:${conflict.field || ""}:${conflict.kind}`}
                >
                  {conflict.message}
                </div>
              ))}
              <div className="ontology-edit-bar__hint">
                Open Review to keep your edit or the version on disk.
              </div>
            </div>
          )}
          {warnings && warnings.length > 0 && (
            <div className="ontology-edit-bar__warnings" role="status" aria-live="polite">
              {warnings.map((warning) => (
                <div key={warning}>{warning}</div>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  );
}
