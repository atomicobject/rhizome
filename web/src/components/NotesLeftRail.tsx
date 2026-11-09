import { useQuery } from "@tanstack/react-query";
import { buildRailGroups } from "./noteRailGroups";
import { memo, type ReactNode, useMemo } from "react";

import { getStatus } from "../api/client";
import { queryKeys } from "../api/queryKeys";
import type {
  OntologyEditSessionResponse,
  OntologySummaryResponse,
  ViewCatalogEntry,
  ViewTarget,
} from "../api/types";
import type { ViewContext } from "../views/context";
import { FolderPanel } from "./FolderPanel";
import { useLocationSnapshot } from "./locationStore";
import { panelIdentity, SidebarPanelSlot } from "./SidebarPanelSlot";
import {
  parseNotesLocation,
  PSEUDO_TYPE_ALL,
  PSEUDO_TYPE_ISSUES,
  PSEUDO_TYPE_MODIFIED,
} from "./notesRoute";
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

/**
 * The active target's resolved panel and its context (SPEC-0136 R6). The
 * shell resolves `owner` before passing it, so the rail sees view, folder, or
 * none.
 */
export type ActivePanel = { panel: ViewTarget["panel"]; context: ViewContext | null };

const NO_PANEL: ActivePanel = { panel: { kind: "none" }, context: null };

// ponytail: stand-in until NotesShell passes `activePanel` and `vaultKey`
// (lane P6): Folder for the note in the URL, keyed by the status vault path.
function useLocationPanel(enabled: boolean) {
  const location = useLocationSnapshot();
  const status = useQuery({ queryKey: queryKeys.status(), queryFn: getStatus, enabled: false });

  const note = enabled
    ? parseNotesLocation(location.pathname, location.search, location.hash).note
    : null;

  return {
    activePanel: (note
      ? {
          panel: { kind: "folder" },
          context: { kind: "node", type: "", ref: { notePath: note, kind: "NOTE" } },
        }
      : NO_PANEL) satisfies ActivePanel,
    vaultKey: status.data?.vaultPath || null,
  };
}

const notePathOf = (context: ViewContext | null) =>
  context?.kind === "node" ? context.ref.notePath : null;

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
  /** The panel the active target resolves to; omitted, the URL's note gets Folder. */
  activePanel?: ActivePanel | null;
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
  vaultKey: vaultKeyProp,
  activePanel: activePanelProp,
}: NotesLeftRailProps) {
  const fallback = useLocationPanel(activePanelProp === undefined);

  const activePanel =
    activePanelProp === undefined ? fallback.activePanel : (activePanelProp ?? NO_PANEL);

  const vaultKey = vaultKeyProp === undefined ? fallback.vaultKey : vaultKeyProp;
  const panelNote = notePathOf(activePanel.context);
  // Custom panel frames arrive with lane P6's message bridge; until then only
  // Folder has content to host.
  const folder = activePanel.panel.kind === "folder" && panelNote !== null;

  const catalogQuery = useViewCatalogQuery(active);
  const validationQuery = useValidationQuery(active);
  const catalog = catalogQuery.data?.views ?? [];
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
      <SidebarPanelSlot
        vaultKey={vaultKey}
        panel={
          folder
            ? {
                identity: panelIdentity(activePanel.panel) ?? "",
                title: "Folder",
                minimisedByDefault: true,
              }
            : null
        }
        navigation={navigation}
      >
        {folder && panelNote ? <FolderPanel notePath={panelNote} active={active} /> : null}
      </SidebarPanelSlot>
    </aside>
  );
});
