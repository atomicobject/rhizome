import type { SetupDecision } from "./api";

export type FileChoices = Record<string, "later" | "take" | "keep">;

export function UpgradeDecisions({
  decisions,
  choices,
  onChange,
}: {
  decisions: SetupDecision[];
  choices: FileChoices;
  onChange: (choices: FileChoices) => void;
}) {
  return (
    <div className="upgrade-group">
      <div className="upgrade-decisions-heading">
        <h2>Files to review</h2>
        <button
          onClick={() => onChange(Object.fromEntries(decisions.map(({ path }) => [path, "take"])))}
        >
          Use Rhizome's version for all
        </button>
      </div>
      {decisions.map((decision) => (
        <div className="upgrade-decision" key={decision.path}>
          <label>
            <span className="path">{decision.path}</span>
            <select
              aria-label={`Choice for ${decision.path}`}
              value={choices[decision.path] ?? "later"}
              onChange={(event) => {
                const value = event.target.value;
                if (value === "later" || value === "take" || value === "keep") {
                  onChange({ ...choices, [decision.path]: value });
                }
              }}
            >
              <option value="later">Decide later</option>
              <option value="take">Use Rhizome's version</option>
              <option value="keep">Keep mine</option>
            </select>
          </label>
          {decision.from && <p className="path">From {decision.from}</p>}
          {decision.kind === "remove" && <p>Rhizome's version removes this file.</p>}
          {!!decision.copies?.length && (
            <details>
              <summary>Copies ({decision.copies.length})</summary>
              <ul className="file-list">
                {decision.copies.map((path) => (
                  <li key={path}>{path}</li>
                ))}
              </ul>
            </details>
          )}
          {decision.diff && (
            <details>
              <summary>Show diff</summary>
              <pre className="upgrade-diff">{decision.diff}</pre>
            </details>
          )}
        </div>
      ))}
    </div>
  );
}
