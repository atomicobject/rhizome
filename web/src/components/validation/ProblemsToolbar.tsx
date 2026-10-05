import type { ValidateEnvelope, ValidationDiagnostic } from "../../api/types";
import type { DiagnosticFilters } from "../useValidationDiagnostics";
import { checkLabel, issueLabel } from "./issueLabels";

export type GroupMode = "kind" | "file";

/** Grouping, server-side filters, and the global "Stage N safe fixes" control. */
export function ProblemsToolbar({
  groupMode,
  onGroupMode,
  filters,
  onFilters,
  textFilter,
  onTextFilter,
  checks,
  issueCodes,
  onClear,
  safeFixCount,
  staging,
  onStageSafeFixes,
}: {
  groupMode: GroupMode;
  onGroupMode: (mode: GroupMode) => void;
  filters: DiagnosticFilters;
  onFilters: (update: (value: DiagnosticFilters) => DiagnosticFilters) => void;
  textFilter: string;
  onTextFilter: (text: string) => void;
  checks: string[];
  issueCodes: string[];
  /** Present when filters hide some issues. */
  onClear?: () => void;
  /** Browser-reviewable safe repairs; 0 hides the control. */
  safeFixCount: number;
  staging: boolean;
  onStageSafeFixes: () => void;
}) {
  return (
    <div className="problems-toolbar" role="group" aria-label="Problem filters">
      <div
        className="problems-toolbar__segmented segmented"
        role="group"
        aria-label="Group issues by"
      >
        {(["kind", "file"] as const).map((mode) => (
          <button
            key={mode}
            type="button"
            aria-pressed={groupMode === mode}
            onClick={() => onGroupMode(mode)}
          >
            {mode === "kind" ? "By kind" : "By file"}
          </button>
        ))}
      </div>
      <select
        aria-label="Check"
        value={filters.check}
        onChange={(event) => onFilters((value) => ({ ...value, check: event.target.value }))}
      >
        <option value="">All checks</option>
        {checks.map((check) => (
          <option key={check} value={check}>
            {checkLabel(check)}
          </option>
        ))}
      </select>
      <select
        aria-label="Issue"
        value={filters.code}
        onChange={(event) => onFilters((value) => ({ ...value, code: event.target.value }))}
      >
        <option value="">All kinds</option>
        {issueCodes.map((code) => (
          <option key={code} value={code}>
            {issueLabel(code)}
          </option>
        ))}
      </select>
      <select
        aria-label="Repair"
        value={filters.repairAvailability}
        onChange={(event) =>
          onFilters((value) => ({
            ...value,
            repairAvailability: parseRepairAvailability(event.target.value),
          }))
        }
      >
        <option value="any">Any repair</option>
        <option value="applicable">Fixable here</option>
        <option value="inapplicable">Not fixable here</option>
      </select>
      <label className="problems-toolbar__search">
        <span className="sr-only">Search issues</span>
        <input
          type="search"
          value={textFilter}
          onChange={(event) => onTextFilter(event.target.value)}
          placeholder="Search issues…"
        />
      </label>
      {onClear && (
        <button type="button" className="problems-toolbar__clear" onClick={onClear}>
          Clear filters
        </button>
      )}
      {safeFixCount > 0 && (
        <button
          type="button"
          className="problems-toolbar__safe"
          disabled={staging}
          title="Review every safe repair in this validation result"
          onClick={onStageSafeFixes}
        >
          Stage {safeFixCount} safe {safeFixCount === 1 ? "fix" : "fixes"}
        </button>
      )}
    </div>
  );
}

export function availableChecks(
  envelope: ValidateEnvelope | null,
  diagnostics: ValidationDiagnostic[],
) {
  return [
    ...new Set([
      ...(envelope?.snapshot?.checks ?? [])
        .filter((check) => check.outcome !== "not_applicable")
        .map((check) => check.check),
      ...diagnostics.map((item) => item.check),
    ]),
  ].sort();
}

function parseRepairAvailability(value: string): DiagnosticFilters["repairAvailability"] {
  if (value === "applicable" || value === "inapplicable") return value;

  return "any";
}
