import { useState } from "react";

import { getPublicValidationDiagnostics } from "../../api/publicClient";
import type { OntologyEditOp } from "../../api/types";
import { diagnosticsParams, type DiagnosticFilters } from "../useValidationDiagnostics";

/** The same frontmatter write a validation repair stages for one property. */
export function setTypeOp(path: string, type: string): OntologyEditOp {
  return {
    id: `frontmatter:${encodeURIComponent(path)}:type`,
    kind: "setFrontmatter",
    path,
    property: "type",
    value: type,
  };
}

/** Every note behind the findings `filters` selects, across all pages. */
export async function findingNotePaths(generation: number, filters: DiagnosticFilters) {
  const paths = new Set<string>();
  let cursor: string | undefined;

  do {
    const page = await getPublicValidationDiagnostics({
      ...diagnosticsParams(generation, filters, 200),
      cursor,
    });

    for (const diagnostic of page.diagnostics) {
      const path = diagnostic.primaryPath || diagnostic.affectedNotePaths?.[0];

      if (path) paths.add(path);
    }

    cursor = page.nextCursor;
  } while (cursor);

  return [...paths];
}

type Outcome = { staging: string } | { staged: string; count: number } | { error: string } | null;

/**
 * Resolves an ambiguous-type case: one choice per candidate type stages a
 * `type` edit for every note in the case into the edit session for review.
 */
export function TypeChoice({
  generation,
  filters,
  types,
  noteCount,
  onStageOps,
  onReviewChanges,
}: {
  generation: number;
  /** The findings of one ambiguous-type variant. */
  filters: DiagnosticFilters;
  types: string[];
  noteCount: number;
  onStageOps: (ops: OntologyEditOp[]) => Promise<void>;
  onReviewChanges?: () => void;
}) {
  const [outcome, setOutcome] = useState<Outcome>(null);
  const busy = outcome !== null && "staging" in outcome;

  const choose = async (type: string) => {
    setOutcome({ staging: type });

    try {
      const paths = await findingNotePaths(generation, filters);
      await onStageOps(paths.map((path) => setTypeOp(path, type)));
      setOutcome({ staged: type, count: paths.length });
    } catch (cause) {
      setOutcome({ error: cause instanceof Error ? cause.message : String(cause) });
    }
  };

  return (
    <div className="problems-resolve" role="group" aria-label="Choose a type">
      {types.map((type) => (
        <button key={type} type="button" disabled={busy} onClick={() => void choose(type)}>
          {outcome && "staging" in outcome && outcome.staging === type
            ? "Staging…"
            : `Set type: ${type} on ${noteCount} ${noteCount === 1 ? "note" : "notes"}`}
        </button>
      ))}
      {outcome && "staged" in outcome && (
        <p className="problems-resolve__status" role="status">
          Staged <code>type: {outcome.staged}</code> on {outcome.count}{" "}
          {outcome.count === 1 ? "note" : "notes"}.
          {onReviewChanges && (
            <button type="button" onClick={onReviewChanges}>
              Review changes
            </button>
          )}
        </p>
      )}
      {outcome && "error" in outcome && (
        <p className="problems-resolve__status is-error" role="alert">
          Could not stage the type change. {outcome.error}
        </p>
      )}
    </div>
  );
}
