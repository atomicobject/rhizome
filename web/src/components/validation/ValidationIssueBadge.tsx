import type { ValidationHealth } from "../../api/types";

export function ValidationIssueBadge({
  count,
  health = "current_issues",
  label,
  onClick,
  presenceOnly = false,
  showUnit = false,
}: {
  count: number | null | undefined;
  health?: ValidationHealth;
  label: string;
  onClick?: () => void;
  presenceOnly?: boolean;
  /** Say what is counted ("1 issue") where no row or stat label does. */
  showUnit?: boolean;
}) {
  if (count === 0) return null;

  if (count == null && health !== "running" && health !== "stale" && health !== "failed")
    return null;

  const content =
    presenceOnly && count ? (
      <span aria-hidden="true">•</span>
    ) : count && showUnit ? (
      `${count} ${count === 1 ? "issue" : "issues"}`
    ) : (
      (count ?? statusMark(health))
    );

  const className = `validation-issue-badge validation-issue-badge--${health}${presenceOnly ? " validation-issue-badge--presence" : ""}`;

  if (!onClick)
    return (
      <span className={className} role="img" aria-label={label}>
        {content}
      </span>
    );

  return (
    <button type="button" className={className} aria-label={label} onClick={onClick}>
      {content}
    </button>
  );
}

function statusMark(health: ValidationHealth) {
  if (health === "running") return "…";

  if (health === "stale") return "old";

  return "?";
}
