import type { ValidateEnvelope } from "../../api/types";
import { ValidationIssueDisclosure } from "./ValidationIssueDisclosure";
import { formatCheckedAt, snapshotTime, validationPresentation } from "./validationPresentation";

export function ValidationStatusStrip({
  envelope,
  busy = false,
  onRefresh,
  refreshing = false,
  refreshError,
}: {
  envelope: ValidateEnvelope | null;
  busy?: boolean;
  onRefresh?: () => void;
  refreshing?: boolean;
  refreshError?: Error | null;
}) {
  const presentation = validationPresentation(envelope);
  const checkedAt = formatCheckedAt(snapshotTime(envelope?.snapshot, envelope?.computedAt));

  return (
    <div className={`validation-status-strip validation-status-strip--${presentation.tone}`}>
      <div className="validation-status-strip__summary" role="status" aria-live="polite">
        <span className="validation-status-strip__mark" aria-hidden="true">
          {mark(presentation.health)}
        </span>
        <strong>{presentation.label}</strong>
        {presentation.hasPublishedResult && (
          <>
            <span aria-hidden="true">·</span>
            <span>Checked {checkedAt}</span>
          </>
        )}
        {busy && <span className="validation-status-strip__activity">Checking…</span>}
      </div>
      {presentation.message && (presentation.health !== "current_clean" || envelope?.snapshot) && (
        <p className="validation-status-strip__message">{presentation.message}</p>
      )}
      {onRefresh && (
        <button
          type="button"
          className="problems-workspace__review validation-status-strip__refresh"
          onClick={onRefresh}
          disabled={refreshing}
        >
          {refreshing ? "Refreshing validation…" : "Refresh validation"}
        </button>
      )}
      {refreshError && <p role="alert">{refreshError.message}</p>}
      <ValidationIssueDisclosure snapshot={envelope?.snapshot} />
    </div>
  );
}

function mark(health: ReturnType<typeof validationPresentation>["health"]) {
  if (health === "current_clean") return "✓";

  if (health === "current_issues" || health === "failed") return "!";

  if (health === "stale" || health === "incomplete") return "△";

  return "○";
}
