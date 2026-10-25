import type { ReactNode } from "react";
import type { ValidationHealth } from "../api/types";
import { ViewHeader } from "./ConfiguredViewHeader";
import { ValidationIssueBadge } from "./validation/ValidationIssueBadge";

function buildOntologyTypePath(typeName: string): string {
  return `/ontology/type/${encodeURIComponent(typeName)}`;
}

export type TypeWorkspaceHeaderProps = {
  /** Resolved type or interface name. */
  typeName: string;
  /** Display label (falls back to typeName if empty). */
  label: string;
  /** "Type", "Interface", or other kind text; it links to the ontology type. */
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
   * False for All notes and display groups, and for every type or interface
   * view but Overview: a collection view fills the count slot with its own
   * row count. The collection issues badge stays either way.
   */
  showStats?: boolean;
  /** Slots a collection view below fills with its count and its Find, Filter, and View. */
  countRef?: (element: HTMLElement | null) => void;
  toolsRef?: (element: HTMLElement | null) => void;
};

/** The type, interface, group, or All notes header: SPEC-0126's shared view header. */
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
  countRef,
  toolsRef,
}: TypeWorkspaceHeaderProps) {
  return (
    <ViewHeader
      className="type-workspace-header"
      title={label || typeName || "Untitled type"}
      titleHint={label || typeName}
      count={showStats ? (totalNotes ?? "—") : undefined}
      countRef={countRef}
      description={description}
      facts={
        <>
          {(issueCount ?? 0) > 0 && (
            <span className="view-header__fact">
              <ValidationIssueBadge
                count={issueCount}
                health={validationHealth}
                label={`Validation issues in ${label}`}
                onClick={onOpenIssues}
              />{" "}
              <span>validation {issueCount === 1 ? "issue" : "issues"}</span>
            </span>
          )}
          {showStats && (
            <span className="view-header__fact" title="Average relations per note">
              <span>{meanRelations ?? "—"}</span> avg links
            </span>
          )}
          {typeName ? (
            <a
              href={buildOntologyTypePath(typeName)}
              className="view-header__fact view-header__schema"
              aria-label={`View ontology type ${typeName}`}
              title={`View ontology type ${typeName}`}
            >
              {eyebrow}
            </a>
          ) : eyebrow ? (
            <span className="view-header__fact">{eyebrow}</span>
          ) : null}
        </>
      }
      toolsRef={toolsRef}
      tabs={viewSelector}
    />
  );
}
