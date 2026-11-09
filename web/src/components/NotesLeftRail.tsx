import { buildRailGroups } from "./noteRailGroups";
import { memo, type ReactNode, useMemo, useState } from "react";

import type {
  OntologyEditSessionResponse,
  OntologySummaryResponse,
  ViewCatalogEntry,
} from "../api/types";
import type { ActivePanel } from "./activePanel";
import type { ExperienceFrames } from "./experienceFrames";
import { FolderPanel } from "./FolderPanel";
import { panelIdentity, type SidebarPanel, SidebarPanelSlot } from "./SidebarPanelSlot";
import { type PanelServices, SidebarPanelFrames, SidebarPanelView } from "./SidebarPanelView";
import { PSEUDO_TYPE_ALL, PSEUDO_TYPE_ISSUES, PSEUDO_TYPE_MODIFIED } from "./notesRoute";
import {
  RailIcon,
  type RailIconName,
  RailSectionHeader,
  TypeTree,
  useRail,
  withoutEmptyViewDuplicates,
} from "./RailTree";
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
  /** Scopes the panel's remembered size and state; from the shell. */
  vaultKey?: string | null;
  /** The active tab's sidebar panel and the context it is invoked with (SPEC-0136 R6); null while unknown. */
  activePanel?: ActivePanel | null;
  /** The window's experience frames, which a custom panel's frame joins; without them only Folder shows. */
  frames?: ExperienceFrames;
  /** What a custom panel's page may ask the workspace to do. */
  panelServices?: PanelServices;
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
  vaultKey = null,
  activePanel,
  frames,
  panelServices,
}: NotesLeftRailProps) {
  const catalogQuery = useViewCatalogQuery(active);
  const validationQuery = useValidationQuery(active);
  const catalog = catalogQuery.data?.views ?? [];

  // While the next subject's panel is unknown (a note still loading), the
  // last one stays, so the slot and its panel do not remount between subjects.
  const [held, setHeld] = useState(activePanel ?? null);

  if (activePanel && activePanel !== held) setHeld(activePanel);
  const shown = activePanel === null ? held : activePanel;
  const panel = shown?.panel;
  const context = shown?.context;

  const folderNote =
    panel?.kind === "folder" && context?.kind === "node" ? context.ref.notePath : null;

  const panelView =
    panel?.kind === "view" ? catalog.find((view) => view.id === panel.viewId) : undefined;

  const hostsView = panelView !== undefined && frames !== undefined && panelServices !== undefined;

  const slotPanel: SidebarPanel | null =
    folderNote !== null && panel
      ? { identity: panelIdentity(panel) ?? "", title: "Folder", minimisedByDefault: true }
      : hostsView && panel
        ? { identity: panelIdentity(panel) ?? "", title: panelView.name }
        : null;

  const standaloneGroups = useMemo(() => groupStandaloneViews(catalog), [catalog]);

  const railGroups = useMemo(
    () =>
      withoutEmptyViewDuplicates(
        buildRailGroups(summary),
        new Map(
          standaloneGroups.map((group) => [
            group.group,
            new Set(group.views.map((view) => view.name)),
          ]),
        ),
      ),
    [summary, standaloneGroups],
  );

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

  const navigation = (
    <section
      className="notes-left-rail__collections"
      role="region"
      aria-label="Collections"
      {...railProps}
    >
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
          ) : issueCount > 0 ? (
            <span className="ontology-type-row__count is-risk">{issueCount}</span>
          ) : null,
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

            // A group holding saved views and types names each set, since a
            // view is often named after the type it shows.
            return groupViews ? (
              <>
                <nav className="notes-left-rail__nested-views" aria-label={`${group} views`}>
                  <span className="notes-left-rail__subhead" aria-hidden="true">
                    Views
                  </span>
                  {groupViews.map(renderView)}
                </nav>
                <span className="notes-left-rail__subhead" aria-hidden="true">
                  Types
                </span>
              </>
            ) : null;
          }}
        />
      </div>
    </section>
  );

  return (
    <aside className="notes-left-rail" aria-label="Notes navigation">
      <SidebarPanelSlot vaultKey={vaultKey} panel={slotPanel} navigation={navigation}>
        {folderNote !== null ? (
          <FolderPanel
            notePath={folderNote}
            active={active}
            onOpenNote={panelServices && ((path) => panelServices.onOpenNote(path, "activate"))}
          />
        ) : hostsView && panel?.kind === "view" && context ? (
          <SidebarPanelView
            frames={frames}
            view={panelView}
            experienceKey={panel.experienceKey}
            context={context}
            services={panelServices}
            active={active}
          />
        ) : null}
      </SidebarPanelSlot>
      {frames ? <SidebarPanelFrames frames={frames} /> : null}
    </aside>
  );
});
