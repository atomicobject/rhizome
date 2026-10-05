import type { ValidateEnvelope, ValidationHealth, ValidationSnapshot } from "../../api/types";

export type ValidationTone = "neutral" | "success" | "warning" | "danger";

export type ValidationPresentation = {
  health: ValidationHealth;
  tone: ValidationTone;
  label: string;
  message: string;
  hasPublishedResult: boolean;
};

export function validationHealth(envelope: ValidateEnvelope | null): ValidationHealth {
  if (!envelope) return "never_checked";

  if (envelope.health) return envelope.health;

  if (envelope.status === "running") return "running";

  if (envelope.status === "error") return "failed";

  return envelope.snapshot?.issueCount ? "current_issues" : "never_checked";
}

export function validationPresentation(envelope: ValidateEnvelope | null): ValidationPresentation {
  const health = validationHealth(envelope);
  const snapshot = envelope?.snapshot;
  const issueCount = snapshot?.issueCount ?? 0;
  const hasPublishedResult = Boolean(snapshot);

  switch (health) {
    case "current_clean":
      return {
        health,
        tone: "success",
        label: "No issues found",
        message: "Validation completed successfully.",
        hasPublishedResult,
      };
    case "current_issues":
      return {
        health,
        tone: "danger",
        label: `${issueCount} ${issueCount === 1 ? "issue" : "issues"}`,
        message: "",
        hasPublishedResult,
      };
    case "running":
      return {
        health,
        tone: "neutral",
        label: "Checking vault",
        message: hasPublishedResult
          ? "Showing the last published result while checks run."
          : "Results will appear when the first check finishes.",
        hasPublishedResult,
      };
    case "stale":
      return {
        health,
        tone: "warning",
        label: "Results are out of date",
        message: snapshot?.staleReason || "The vault changed after this result was published.",
        hasPublishedResult,
      };
    case "incomplete":
      return {
        health,
        tone: "warning",
        label: "Validation incomplete",
        message:
          "Some selected checks did not complete, so this result cannot establish a clean vault.",
        hasPublishedResult,
      };
    case "failed":
      return {
        health,
        tone: "danger",
        label: hasPublishedResult ? "Refresh failed" : "Validation unavailable",
        message: envelope?.error || "Validation did not complete.",
        hasPublishedResult,
      };
    default:
      return {
        health,
        tone: "neutral",
        label: "Not checked yet",
        message: "Validation has not published a result for this vault.",
        hasPublishedResult,
      };
  }
}

export function snapshotTime(
  snapshot: ValidationSnapshot | undefined,
  computedAt?: string,
): Date | null {
  if (snapshot?.finishedAt)
    return new Date(snapshot.finishedAt < 1e12 ? snapshot.finishedAt * 1000 : snapshot.finishedAt);

  if (!computedAt) return null;
  const date = new Date(computedAt);

  return Number.isNaN(date.getTime()) ? null : date;
}

export function formatCheckedAt(date: Date | null, now = Date.now()): string {
  if (!date) return "time unavailable";
  const seconds = Math.max(0, Math.round((now - date.getTime()) / 1000));

  if (seconds < 60) return "just now";
  const minutes = Math.round(seconds / 60);

  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.round(minutes / 60);

  if (hours < 48) return `${hours}h ago`;

  return date.toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
  });
}
