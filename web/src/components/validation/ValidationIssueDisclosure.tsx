import type { ValidationCheckSnapshot, ValidationSnapshot } from "../../api/types";
import { ValidationCheck } from "../StatusMark";
import { humanizeName } from "../../lib/labels";

export function ValidationIssueDisclosure({ snapshot }: { snapshot?: ValidationSnapshot }) {
  if (!snapshot) return null;
  const regionId = `validation-check-details-${snapshot.generation}`;

  return (
    <details className="validation-disclosure">
      <summary aria-controls={regionId}>Check details</summary>
      <div id={regionId} className="validation-disclosure__body">
        <dl className="validation-disclosure__facts">
          <div>
            <dt>Scope</dt>
            <dd>{snapshot.scope}</dd>
          </div>
          <div>
            <dt>Checks</dt>
            <dd>{snapshot.selectedChecks.length}</dd>
          </div>
          <div>
            <dt>Duration</dt>
            <dd>{formatDuration(snapshot.durationMs)}</dd>
          </div>
          <div>
            <dt>Files</dt>
            <dd>{snapshot.affectedFileCount}</dd>
          </div>
        </dl>
        <ValidationCheckList checks={snapshot.checks} />
      </div>
    </details>
  );
}

/** Each check's outcome, issue count, and summary or error. */
export function ValidationCheckList({
  checks,
  className = "validation-disclosure__checks",
}: {
  checks: ValidationCheckSnapshot[];
  className?: string;
}) {
  return (
    <ul className={className}>
      {checks.map((check) => (
        <li key={check.check}>
          <span
            className={`validation-disclosure__outcome validation-disclosure__outcome--${check.outcome}`}
          >
            {humanizeName(check.outcome)}
          </span>
          <strong>{humanizeName(check.check)}</strong>
          <span className="num">
            {check.issueCount} {check.issueCount === 1 ? "issue" : "issues"}
          </span>
          {check.error && <span className="validation-disclosure__error">{check.error}</span>}
          {!check.error && check.summary && <span>{sentence(check.summary)}</span>}
        </li>
      ))}
    </ul>
  );
}

/**
 * A clean run's checks: each row leads with the check's name, then a compact
 * success mark and what it found. The strip above already says "No issues".
 */
export function CleanCheckList({ checks }: { checks: ValidationCheckSnapshot[] }) {
  return (
    <ul className="validation-checks__list">
      {checks.map((check) => (
        <li key={check.check}>
          <strong>{humanizeName(check.check)}</strong>
          <span className="validation-checks__result">
            <ValidationCheck
              label={
                check.summary
                  ? sentence(
                      check.summary.replace("identifier nodes checked", "identifiers checked"),
                    )
                  : "Passed"
              }
            />
          </span>
        </li>
      ))}
    </ul>
  );
}

/** Check summaries arrive lowercase ("no broken links"); they read as sentences here. */
function sentence(text: string) {
  return text.charAt(0).toUpperCase() + text.slice(1);
}

export function formatDuration(milliseconds: number) {
  if (milliseconds < 1000) return `${milliseconds}ms`;

  return `${(milliseconds / 1000).toFixed(milliseconds < 10_000 ? 1 : 0)}s`;
}
