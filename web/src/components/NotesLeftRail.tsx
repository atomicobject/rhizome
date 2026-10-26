import { buildRailGroups } from "./noteRailGroups";
import { memo, type ReactNode, useMemo } from "react";

import type {
  OntologyEditSessionResponse,
  OntologySummaryResponse,
  ViewCatalogEntry,
} from "../api/types";
import { PSEUDO_TYPE_ALL, PSEUDO_TYPE_ISSUES, PSEUDO_TYPE_MODIFIED } from "./notesRoute";
import { RailIcon, type RailIconName, RailSectionHeader, TypeTree, useRail } from "./RailTree";
import { summaryCountsKnown, useValidationQuery, useViewCatalogQuery } from "./useNotesQueries";
import { ValidationIssueBadge } from "./validation/ValidationIssueBadge";
import { validationHealth } from "./validation/validationPresentation";
import { groupStandaloneViews } from "./viewCatalog";

const EXPANDED_KEY = "rhizome:notes:left-rail:expanded:v1";

export type NotesLeftRailProps = {
  active?: boolean;
  /** The vault this browser tab serves; Rhizome Desktop names it in its toolbar instead. */
  summary: OntologySummaryResponse | null;
  selectedType: string | null;
  activeCollection: string | null;
  activeViewID: string | null;
  editSession: OntologyEditSessionResponse | null;
  touchedPaths: Set<string>;
  activeGroup?: string | null;
  /** Recently changed is showing (All notes in its Briefing). */
  recentActive?: boolean;
  onSelectGroup?: (group: string) => void;
  onSelectCollection: (typeName: string) => void;
  onOpenRecent?: () => void;
  onOpenConfiguredView: (view: ViewCatalogEntry) => void;
};

export const NotesLeftRail = memo(function NotesLeftRail({
  active = true,
  summary,
  selectedType,
  activeCollection,
  activeViewID,
  editSession,
  touchedPaths,
  onSelectCollection,
  activeGroup,
  recentActive = false,
  onSelectGroup,
  onOpenRecent,
  onOpenConfiguredView,
}: NotesLeftRailProps) {
  const catalogQuery = useViewCatalogQuery(active);
  const validationQuery = useValidationQuery(active);
  const catalog = catalogQuery.data?.views ?? [];
  const standaloneGroups = useMemo(() => groupStandaloneViews(catalog), [catalog]);
  const railGroups = useMemo(() => buildRailGroups(summary), [summary]);
  const { railProps, ...expansion } = useRail(EXPANDED_KEY, railGroups, selectedType);
  const { isExpanded, toggle } = expansion;

  const totalNotes = summary && summaryCountsKnown(summary) ? summary.totalNotes : null;

  const issueCount = validationQuery.data?.snapshot?.issueCount;
  const health = validationHealth(validationQuery.data ?? null);

  const typeGroupNames = new Set(railGroups.map((group) => group.name));
  const viewsByGroup = new Map(standaloneGroups.map((group) => [group.group, group.views]));

  // Views whose group matches a type group render inside it, so a group is named once.
  const standaloneViews = standaloneGroups
    .filter((group) => !typeGroupNames.has(group.group))
    .flatMap((group) => group.views);

  const viewsOpen = isExpanded("views", true);

  const renderView = (view: ViewCatalogEntry) => (
    <div className="ontology-type-row-wrap" key={view.id}>
      <button
        type="button"
        data-rail-row
        className={`ontology-view-row${activeViewID === view.id ? " is-selected" : ""}`}
        aria-current={activeViewID === view.id ? "page" : undefined}
        onClick={() => onOpenConfiguredView(view)}
      >
        <RailIcon name="view" />
        <span className="ontology-view-row__label">{view.name}</span>
      </button>
    </div>
  );

  const quickRow = (
    current: boolean,
    icon: RailIconName,
    text: string,
    accessibleName: string,
    onClick: () => void,
    trailing?: ReactNode,
    tone = "",
  ) => (
    <div className="ontology-type-row-wrap">
      <button
        type="button"
        data-rail-row
        className={`ontology-type-row ontology-type-row--pseudo${tone}${current ? " is-selected" : ""}`}
        aria-label={accessibleName}
        aria-current={current ? "page" : undefined}
        onClick={onClick}
      >
        <RailIcon name={icon} />
        <span className="ontology-type-row__label">{text}</span>
        {trailing}
      </button>
    </div>
  );

  return (
    <aside className="notes-left-rail" aria-label="Notes navigation" {...railProps}>
      <section className="notes-left-rail__collections" role="region" aria-label="Collections">
        <div className="ontology-type-list">
          {quickRow(
            activeCollection === PSEUDO_TYPE_ALL && !recentActive,
            "home",
            "All notes",
            totalNotes === null ? "All notes" : `All notes (${totalNotes})`,
            () => onSelectCollection(PSEUDO_TYPE_ALL),
            totalNotes !== null && <span className="ontology-type-row__count">{totalNotes}</span>,
          )}
          {onOpenRecent &&
            quickRow(recentActive, "clock", "Recently changed", "Recently changed", onOpenRecent)}
          {quickRow(
            activeCollection === PSEUDO_TYPE_ISSUES,
            "alert",
            "Problems",
            issueCount ? `Problems ${issueCount}` : "Problems",
            () => onSelectCollection(PSEUDO_TYPE_ISSUES),
            issueCount == null ? (
              <ValidationIssueBadge count={issueCount} health={health} label="Validation status" />
            ) : (
              <span className={`ontology-type-row__count${issueCount > 0 ? " is-risk" : ""}`}>
                {issueCount}
              </span>
            ),
            " ontology-type-row--issues",
          )}
          {editSession?.hasUncommittedChanges &&
            quickRow(
              activeCollection === PSEUDO_TYPE_MODIFIED,
              "edit",
              "Changes",
              `Changes ${touchedPaths.size}`,
              () => onSelectCollection(PSEUDO_TYPE_MODIFIED),
              <span className="ontology-type-row__count">{touchedPaths.size}</span>,
              " ontology-type-row--modified",
            )}
          {standaloneViews.length > 0 && (
            <nav
              className={`ontology-type-group ontology-type-group--presentation${viewsOpen ? " is-expanded" : " is-collapsed"}`}
              aria-label="Views"
            >
              <RailSectionHeader open={viewsOpen} onToggle={() => toggle("views", viewsOpen)}>
                Views
              </RailSectionHeader>
              {viewsOpen && standaloneViews.map(renderView)}
            </nav>
          )}
          <TypeTree
            groups={railGroups}
            expansion={expansion}
            active={active}
            selected={activeCollection}
            showCounts={totalNotes !== null}
            onSelect={onSelectCollection}
            onSelectGroup={(group) => onSelectGroup?.(group)}
            activeGroup={activeGroup}
            groupContent={(group) => {
              const groupViews = viewsByGroup.get(group);

              return groupViews ? (
                <nav className="notes-left-rail__nested-views" aria-label={`${group} views`}>
                  {groupViews.map(renderView)}
                </nav>
              ) : null;
            }}
          />
        </div>
      </section>
    </aside>
  );
});
