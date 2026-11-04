import type { ValidateEnvelope } from "../../api/types";
import {
  CleanCheckList,
  formatDuration,
  ValidationIssueDisclosure,
} from "./ValidationIssueDisclosure";
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
  const snapshot = envelope?.snapshot;
  // A clean result lists its checks in place: there is nothing else to show.
  const clean = presentation.health === "current_clean" && Boolean(snapshot?.checks.length);
  const checkCount = snapshot?.checks.length ?? 0;

  const strip = (
    <div className={`validation-status-strip validation-status-strip--${presentation.tone}`}>
      <div className="validation-status-strip__summary" role="status" aria-live="polite">
        <span className="validation-status-strip__mark" aria-hidden="true">
          {mark(presentation.health)}
        </span>
        <strong>{presentation.label}</strong>
        {clean && snapshot && (
          <>
            <span aria-hidden="true">·</span>
            <span className="num">
              {checkCount} {checkCount === 1 ? "check" : "checks"} in{" "}
              {formatDuration(snapshot.durationMs)}
            </span>
          </>
        )}
        {presentation.hasPublishedResult && (
          <>
            <span aria-hidden="true">·</span>
            <span>Checked {checkedAt}</span>
          </>
        )}
        {busy && <span className="validation-status-strip__activity">Checking…</span>}
      </div>
      {/* Details belong to the result they explain, beside "Checked …". */}
      {clean ? null : <ValidationIssueDisclosure snapshot={snapshot} />}
      {/* A clean result says it all in its label; other states explain themselves. */}
      {presentation.message && presentation.health !== "current_clean" && (
        <p className="validation-status-strip__message">{presentation.message}</p>
      )}
      {onRefresh && (
        <button
          type="button"
          className="button validation-status-strip__refresh"
          onClick={onRefresh}
          disabled={refreshing}
        >
          {refreshing ? "Refreshing validation…" : "Refresh validation"}
        </button>
      )}
      {refreshError && <p role="alert">{refreshError.message}</p>}
    </div>
  );

  if (!clean || !snapshot) return strip;

  return (
    <>
      {strip}
      <section className="validation-checks" aria-label="Checks that ran">
        <CleanCheckList checks={snapshot.checks} />
      </section>
    </>
  );
}

function mark(health: ReturnType<typeof validationPresentation>["health"]) {
  if (health === "current_clean") return "✓";

  if (health === "current_issues" || health === "failed") return "!";

  if (health === "stale" || health === "incomplete") return "△";

  return "○";
}
