import type { ValidationRepairController } from "../useValidationRepair";
import { errorMessage } from "./ValidationRepairReviewPanel";

/** Connected-transaction consent, staging failures, and stale-validation notices. */
export function RepairNotices({
  repair,
  onIncludeConnected,
  onReviewChanges,
}: {
  repair: ValidationRepairController;
  onIncludeConnected: (actionIds: string[]) => void;
  onReviewChanges?: () => void;
}) {
  return (
    <>
      {repair.expansion && (
        <div className="problems-workspace__notice" role="note">
          These fixes belong to a connected transaction with {repair.expansion.actionIds.length}{" "}
          fixes. Include all connected fixes to review the complete changes.
          <button
            type="button"
            onClick={() => repair.expansion && onIncludeConnected(repair.expansion.actionIds)}
          >
            Include connected fixes and review
          </button>
          <button type="button" onClick={repair.dismissExpansion}>
            Cancel
          </button>
        </div>
      )}
      {Boolean(repair.error) && (
        <div className="problems-workspace__notice problems-workspace__notice--error" role="alert">
          {errorMessage(repair.error)} Your selection is still available.
          {onReviewChanges && (
            <button type="button" onClick={onReviewChanges}>
              Review drafts
            </button>
          )}
        </div>
      )}
      {repair.changed && (
        <div className="problems-workspace__notice" role="status">
          Validation changed. Select the current fixes and create a new review before saving.
        </div>
      )}
    </>
  );
}

/** The selected-repairs bar, or a note when the browser cannot review repairs. */
export function RepairActionBar({
  selectedCount,
  hasActions,
  canReview,
  staging,
  onClear,
  onStage,
}: {
  selectedCount: number;
  hasActions: boolean;
  canReview: boolean;
  staging: boolean;
  onClear: () => void;
  onStage: () => void;
}) {
  if (selectedCount === 0) {
    return hasActions && !canReview ? (
      <div className="problems-workspace__repair-note" role="note">
        Automatic repairs are visible in issue details. Browser review is unavailable; use{" "}
        <code>rzm validate fix</code> to preview changes.
      </div>
    ) : null;
  }

  return (
    <div className="problems-action-bar" role="region" aria-label="Selected repairs">
      <span>
        <strong>{selectedCount}</strong> {selectedCount === 1 ? "repair" : "repairs"} selected
      </span>
      <button type="button" className="problems-action-bar__clear" onClick={onClear}>
        Clear
      </button>
      {canReview ? (
        <button
          type="button"
          className="problems-action-bar__stage"
          disabled={staging}
          onClick={onStage}
        >
          {staging ? "Staging…" : `Stage ${selectedCount} ${selectedCount === 1 ? "fix" : "fixes"}`}
        </button>
      ) : (
        <span>
          Browser repair review is unavailable. Use <code>rzm validate fix</code> to preview these
          repairs.
        </span>
      )}
    </div>
  );
}
