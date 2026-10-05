import { useState } from "react";

import { ApiError } from "../../api/publicClient";
import { isJsonObject, isStringArray } from "../../api/parse";
import type { ValidationRepairApplyResponse, ValidationRepairReview } from "../../api/types";
import { UnifiedDiff } from "../UnifiedDiff";

const FILE_KIND_LABELS = { write: "Edit", rename: "Move", delete: "Delete" } as const;

type Props = {
  review: ValidationRepairReview;
  requestedActionCount: number;
  applying: boolean;
  restoring?: boolean;
  onRetryRecovery?: () => void;
  error: Error | null;
  result: ValidationRepairApplyResponse | null;
  onApply: (confirmations: NonNullable<ValidationRepairReview["requiredConfirmations"]>) => void;
  onClose: () => void;
  onReviewDrafts?: () => void;
};

export function ValidationRepairReviewPanel({
  review,
  requestedActionCount,
  applying,
  restoring = false,
  onRetryRecovery,
  error,
  result,
  onApply,
  onClose,
  onReviewDrafts,
}: Props) {
  const required = review.requiredConfirmations ?? [];
  const [confirmed, setConfirmed] = useState<Set<string>>(new Set());

  const confirmationKey = (item: (typeof required)[number]) =>
    `${item.actionId}:${item.candidatePath ?? ""}`;

  const allConfirmed = required.every((item) => confirmed.has(confirmationKey(item)));
  const addedActionCount = Math.max(0, review.actionIds.length - requestedActionCount);
  const overlap = overlappingPaths(error);
  const applied = result?.review.state === "applied";
  const execution = result?.execution;

  return (
    <section className="repair-review" aria-label="Repair review">
      <header className="repair-review__header">
        <div>
          <p className="problems-workspace__meta">repair review</p>
          <h3>
            {review.actionIds.length} {review.actionIds.length === 1 ? "fix" : "fixes"} across{" "}
            {review.affectedPaths.length} {review.affectedPaths.length === 1 ? "file" : "files"}
          </h3>
        </div>
        <button
          className="repair-review__button"
          type="button"
          onClick={onClose}
          disabled={applying || restoring}
        >
          Return to issues
        </button>
      </header>

      {applied && (
        <div className="repair-review__success" role="status">
          Saved {execution?.appliedWrites ?? review.affectedPaths.length} of{" "}
          {execution?.plannedWrites ?? review.affectedPaths.length} planned file changes. Postcheck
          found {execution?.remainingFindings ?? result.result.issueCount} remaining issues.
        </div>
      )}
      {addedActionCount > 0 && (
        <div className="repair-review__notice" role="note">
          This repair is part of a connected transaction. The review includes {addedActionCount}{" "}
          additional {addedActionCount === 1 ? "fix" : "fixes"} so the files stay consistent.
        </div>
      )}
      <p className="repair-review__meta">
        {review.transactionIds.length} connected{" "}
        {review.transactionIds.length === 1 ? "transaction" : "transactions"}
      </p>
      {review.state === "stale" && (
        <div className="repair-review__notice" role="status">
          {review.staleReason || "This fix review is stale. Stage the fixes again."}
        </div>
      )}

      <div className="repair-review__files">
        {review.preview.map((file) => (
          <details key={`${file.kind}:${file.path}`} open={!applied && review.preview.length <= 3}>
            <summary>
              <code>{file.path}</code>
              <span className="repair-review__kind">
                {FILE_KIND_LABELS[file.kind] ?? file.kind}
              </span>
            </summary>
            {file.originalPath && file.originalPath !== file.path && (
              <p className="repair-review__move">Moves from {file.originalPath}</p>
            )}
            {file.diff ? (
              <UnifiedDiff diff={file.diff} />
            ) : (
              <p className="repair-review__move">
                No textual diff is available for this operation.
              </p>
            )}
          </details>
        ))}
      </div>

      {required.length > 0 && (
        <fieldset className="repair-review__confirmations">
          <legend>Confirm candidate choices</legend>
          {required.map((item) => {
            const key = confirmationKey(item);

            return (
              <label key={key}>
                <input
                  type="checkbox"
                  checked={confirmed.has(key)}
                  onChange={(event) =>
                    setConfirmed((current) => {
                      const next = new Set(current);

                      if (event.target.checked) next.add(key);
                      else next.delete(key);

                      return next;
                    })
                  }
                />
                <span>
                  {item.question || "Confirm this repair"}
                  {item.candidatePath && (
                    <small>
                      Candidate <code>{item.candidatePath}</code>
                    </small>
                  )}
                  {(item.affectedPaths?.length ?? 0) > 0 && (
                    <small>Affects {item.affectedPaths?.join(" · ")}</small>
                  )}
                </span>
              </label>
            );
          })}
        </fieldset>
      )}

      {Boolean(error) && (
        <div className="problems-workspace__notice problems-workspace__notice--error" role="alert">
          {errorMessage(error)} Your review and issue selection remain available.
          {onRetryRecovery && (
            <button
              className="repair-review__button"
              type="button"
              onClick={onRetryRecovery}
              disabled={restoring}
            >
              Retry loading review
            </button>
          )}
          {overlap.length > 0 && onReviewDrafts && (
            <button className="repair-review__button" type="button" onClick={onReviewDrafts}>
              Review overlapping drafts ({overlap.length})
            </button>
          )}
        </div>
      )}
      {(execution?.transactions?.length ?? 0) > 0 && (
        <div className="repair-review__transactions">
          <h4>Transaction results</h4>
          {execution?.transactions?.map((transaction) => (
            <details
              key={transaction.transactionId}
              open={transaction.status === "failed"}
              data-status={transaction.status}
            >
              <summary>
                {transaction.transactionId} · {transaction.status}
              </summary>
              {transaction.reason && <p>{transaction.reason}</p>}
              {(transaction.affectedPaths?.length ?? 0) > 0 && (
                <p>{transaction.affectedPaths?.join(", ")}</p>
              )}
            </details>
          ))}
          {execution?.replanCommand && (
            <button
              className="repair-review__button"
              type="button"
              onClick={() => void navigator.clipboard.writeText(execution.replanCommand || "")}
            >
              Copy replan command
            </button>
          )}
        </div>
      )}
      {!applied && (
        <div className="repair-review__actions">
          <span>
            {required.length > 0 && !allConfirmed
              ? "Confirm each candidate to enable saving."
              : "No files change until you save."}
          </span>
          <button
            className="repair-review__button repair-review__button--primary"
            type="button"
            disabled={applying || restoring || !allConfirmed || review.state !== "pending"}
            onClick={() => onApply(required)}
          >
            {applying ? "Saving…" : "Save fixes"}
          </button>
        </div>
      )}
    </section>
  );
}

export function errorMessage(error: Error | null) {
  return error?.message ?? "The repair review could not be completed.";
}

function overlappingPaths(error: Error | null): string[] {
  if (!(error instanceof ApiError) || !isJsonObject(error.details)) return [];

  return isStringArray(error.details.overlappingPaths) ? error.details.overlappingPaths : [];
}
