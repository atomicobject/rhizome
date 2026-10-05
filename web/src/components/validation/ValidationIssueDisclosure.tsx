import type { ValidationSnapshot } from "../../api/types";

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
        <ul className="validation-disclosure__checks">
          {snapshot.checks.map((check) => (
            <li key={check.check}>
              <span
                className={`validation-disclosure__outcome validation-disclosure__outcome--${check.outcome}`}
              >
                {check.outcome.replace("_", " ")}
              </span>
              <strong>{check.check.replaceAll("_", " ")}</strong>
              <span>
                {check.issueCount} {check.issueCount === 1 ? "issue" : "issues"}
              </span>
              {check.error && <span className="validation-disclosure__error">{check.error}</span>}
              {!check.error && check.summary && <span>{check.summary}</span>}
            </li>
          ))}
        </ul>
      </div>
    </details>
  );
}

function formatDuration(milliseconds: number) {
  if (milliseconds < 1000) return `${milliseconds}ms`;

  return `${(milliseconds / 1000).toFixed(milliseconds < 10_000 ? 1 : 0)}s`;
}
