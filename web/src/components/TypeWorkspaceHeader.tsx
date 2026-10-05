import type { ReactNode } from "react";
import type { ValidationHealth } from "../api/types";
import { ValidationIssueBadge } from "./validation/ValidationIssueBadge";

function buildOntologyTypePath(typeName: string): string {
  return `/ontology/type/${encodeURIComponent(typeName)}`;
}

export type TypeWorkspaceHeaderProps = {
  /** Resolved type or interface name. */
  typeName: string;
  /** Display label (falls back to typeName if empty). */
  label: string;
  /** "Type", "Interface", or other kind text shown beside the title. */
  eyebrow: string;
  description?: string | null;
  /** Null while the counts are unknown (loading or failed); renders a dash, never a zero. */
  totalNotes: number | null;
  issueCount: number | undefined;
  validationHealth?: ValidationHealth;
  meanRelations: number | null;
  viewSelector?: ReactNode;
  onOpenIssues?: () => void;
  /**
   * False for display groups, whose views report their own counts, and for
   * every type or interface view but Overview: the Briefing, Table, Board, and
   * Cards report their own. The collection issues badge stays either way.
   */
  showStats?: boolean;
};

export function TypeWorkspaceHeader({
  typeName,
  label,
  eyebrow,
  description,
  totalNotes,
  issueCount,
  validationHealth = "never_checked",
  meanRelations,
  viewSelector,
  onOpenIssues,
  showStats = true,
}: TypeWorkspaceHeaderProps) {
  return (
    <header className="ontology-home__hero type-workspace-header">
      <div className="ontology-home__hero-title">
        <div className="ontology-home__hero-heading">
          <h2 title={label || typeName}>{label || typeName || "Untitled type"}</h2>
          {typeName ? (
            <span className="ontology-home__hero-eyebrow">
              {eyebrow}{" "}
              <a
                href={buildOntologyTypePath(typeName)}
                className="ontology-home__hero-link"
                aria-label={`View ontology type ${typeName}`}
                title="View ontology type"
              >
                {typeName}
              </a>
            </span>
          ) : eyebrow ? (
            <span className="ontology-home__hero-eyebrow">{eyebrow}</span>
          ) : null}
        </div>
        {description ? <p title={description}>{description}</p> : null}
      </div>
      <div className="ontology-home__hero-stats">
        {showStats && (
          <div className="ontology-home__stat">
            <b>{totalNotes ?? "—"}</b>
            <span>notes</span>
          </div>
        )}
        {(issueCount ?? 0) > 0 && (
          <div className="ontology-home__stat">
            <ValidationIssueBadge
              count={issueCount}
              health={validationHealth}
              label={`Validation issues in ${label}`}
              onClick={onOpenIssues}
            />
            <span>issues</span>
          </div>
        )}
        {showStats && (
          <div className="ontology-home__stat" title="Average relations per note">
            <b>{meanRelations ?? "—"}</b>
            <span>avg links</span>
          </div>
        )}
        {viewSelector}
      </div>
    </header>
  );
}
