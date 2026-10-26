import { useQuery, useQueryClient } from "@tanstack/react-query";
import { type CSSProperties, useCallback, useEffect, useMemo, useRef, useState } from "react";

import { getStatus } from "../api/client";
import { queryKeys } from "../api/queryKeys";
import type {
  OntologyEditOp,
  NodeRef,
  StructuralNode,
  ValidationScope,
  ViewCatalogEntry,
} from "../api/types";
import { DiagnosticRegion } from "../diagnostics/DiagnosticRegion";
import { vaultLabel } from "../lib/labels";
import { HomeTab } from "./HomeTab";
import { EDITOR_DRAFTS_CHANGED_EVENT, hasPersistedEditorDrafts } from "./editing/draftStorage";
import { flushEditors } from "./editing/editorFlush";
import { useFocusRegions } from "./focusManagement";
import { clearPersistedEditorDrafts, editCommitSucceeded } from "./editing/editSessionState";
import { NotesLeftRail } from "./NotesLeftRail";
import { sameNoteTabContext, type NoteTabContext } from "./noteTabContext";
import { notifyLocationChange, useLocationSnapshot } from "./locationStore";
import { NoteTab } from "./NoteTab";
import { NoteTabStrip } from "./NoteTabStrip";
import { SearchWorkspace } from "./SearchWorkspace";
import { ViewTab } from "./ViewTab";
import { NotesRightRail } from "./NotesRightRail";
import { PanelResizeHandle } from "./PanelResizeHandle";
import { RecordRailProvider, useRecordRailState } from "./recordRail";
import { inDesktop } from "./desktopHost";
import { readSingleKey, typingIn } from "./KeyboardShortcuts";
import { PANE_RANGE, useShellPanes } from "./shellPanes";
import { isSidebarShortcut, PanelIcon, WorkbenchSidebar } from "./WorkbenchSidebar";
import { useWorkbenchSidebar } from "./sidebarState";
import {
  buildNotesLocation,
  normalizeNotePath,
  type NotesLocation,
  parseNotesLocation,
  PSEUDO_TYPE_ISSUES,
  PSEUDO_TYPE_MODIFIED,
  selectionForType,
  selectionType,
} from "./notesRoute";
import { OntologyEditSessionPanel } from "./OntologyEditSessionPanel";
import { type CollectionTab, type OpenMode, useNoteTabs } from "./useNoteTabs";
import { useFinderNoteRequests } from "./FinderQuickOpen";
import {
  useOntologySummaryQuery,
  useValidationQuery,
  useViewCatalogQuery,
} from "./useNotesQueries";
import { useOntologyEditSession } from "./useOntologyEditSession";
import type { OpenNodeOptions, ViewContext } from "../views/context";
import { useValidationScopeSummaries, validationScopeKey } from "./useValidationScopeSummaries";
import { validationHealth } from "./validation/validationPresentation";

function navigate(url: string, replace = false) {
  const current = `${window.location.pathname}${window.location.search}${window.location.hash}`;

  if (url === current) return;
  window.history[replace ? "replaceState" : "pushState"]({}, "", url);
  notifyLocationChange();
}

const COLLECTION_SELECTIONS = {
  issues: { kind: "issues" },
  modified: { kind: "modified" },
} as const satisfies Record<CollectionTab["collection"], NotesLocation["selection"]>;

function collectionLocation(tab: CollectionTab): NotesLocation {
  return {
    selection: COLLECTION_SELECTIONS[tab.collection],
    note: null,
    query: null,
    fragment: null,
    view: null,
    issueScope: tab.collection === "issues" ? tab.issueScope : null,
    issueKey: tab.collection === "issues" ? tab.issueKey : null,
  };
}

// F6 moves in document order: sidebar, tab row, the active panel, right pane.
const FOCUS_REGIONS =
  ".notes-left-rail, .notes-shell__tabs-row, .notes-shell__panel.is-active, .notes-right-rail";

// All notes in its Briefing, whose Recent changes section is the sidebar's Recently changed.
const RECENT_PRESENTATION = "workspace.briefing";

type ShellStyle = CSSProperties & { "--pane-width": string };

/** Longer than a typical double-click interval. */
const RECORD_PANE_DELAY_MS = 400;

/** `value`, but it turns true only after staying true for `delay` ms; it turns false at once. */
function useSettledTrue(value: boolean, delay: number): boolean {
  const [settled, setSettled] = useState(value);

  useEffect(() => {
    if (!value) {
      setSettled(false);

      return;
    }

    const timer = window.setTimeout(() => setSettled(true), delay);

    return () => window.clearTimeout(timer);
  }, [value, delay]);

  return value && settled;
}

export function NotesShell({ active = true }: { active?: boolean }) {
  const queryClient = useQueryClient();
  const shellRef = useRef<HTMLElement>(null);
  useFocusRegions(shellRef, FOCUS_REGIONS, active);
  const browserLocation = useLocationSnapshot();
  const [retainedBrowserLocation, setRetainedBrowserLocation] = useState(browserLocation);

  useEffect(() => {
    if (!active) return;
    setRetainedBrowserLocation((current) =>
      current.pathname === browserLocation.pathname &&
      current.search === browserLocation.search &&
      current.hash === browserLocation.hash
        ? current
        : browserLocation,
    );
  }, [active, browserLocation.hash, browserLocation.pathname, browserLocation.search]);

  const notesLocation = active ? browserLocation : retainedBrowserLocation;

  const location = useMemo(
    () => parseNotesLocation(notesLocation.pathname, notesLocation.search, notesLocation.hash),
    [notesLocation.hash, notesLocation.pathname, notesLocation.search],
  );

  const summaryQuery = useOntologySummaryQuery(active);
  const summary = summaryQuery.data ?? null;

  // AppShell is the status fetch owner. This disabled observer still follows
  // the shared cache so vault-scoped tab hydration updates without a second request.
  const statusQuery = useQuery({
    queryKey: queryKeys.status(),
    queryFn: getStatus,
    enabled: false,
  });

  const selectedType = useMemo(
    () => selectionType(location.selection, summaryQuery.data),
    [location.selection, summaryQuery.data],
  );

  const editSession = useOntologyEditSession();

  const tabs = useNoteTabs(
    statusQuery.data?.vaultName ?? null,
    editSession.vaultKey === null || (editSession.busy && editSession.session === null)
      ? null
      : Boolean(editSession.session?.hasUncommittedChanges),
  );

  const [hasLocalDrafts, setHasLocalDrafts] = useState(() =>
    hasPersistedEditorDrafts(editSession.vaultKey),
  );

  useEffect(() => {
    const update = () => setHasLocalDrafts(hasPersistedEditorDrafts(editSession.vaultKey));
    update();
    window.addEventListener(EDITOR_DRAFTS_CHANGED_EVENT, update);

    return () => window.removeEventListener(EDITOR_DRAFTS_CHANGED_EVENT, update);
  }, [editSession.vaultKey]);
  const viewCatalogQuery = useViewCatalogQuery(active);
  const [contexts, setContexts] = useState<Map<string, NoteTabContext>>(new Map());
  const panes = useShellPanes();
  const recordRail = useRecordRailState();
  const [desktop] = useState(inDesktop);

  const { touchedPaths } = editSession.summary;

  useEffect(() => {
    if (location.selection.kind !== "type" || !summary) return;

    if (selectedType) return;
    navigate(buildNotesLocation({ selection: { kind: "all" } }), true);
  }, [location.selection, selectedType, summary]);
  useEffect(() => {
    for (const tab of tabs.tabs) {
      if (tab.kind === "note") tabs.markDirty(tab.path, touchedPaths.has(tab.path));
    }
  }, [tabs.markDirty, tabs.tabs, touchedPaths]);
  useEffect(() => {
    if (!editSession.editing || !editSession.session?.hasUncommittedChanges) return;
    const onBeforeUnload = (event: BeforeUnloadEvent) => event.preventDefault();
    window.addEventListener("beforeunload", onBeforeUnload);

    return () => window.removeEventListener("beforeunload", onBeforeUnload);
  }, [editSession.editing, editSession.session?.hasUncommittedChanges]);
  const lastRevalidated = useRef<string | null>(null);
  useEffect(() => {
    const session = editSession.session;

    if (!session?.sessionId || (session.status !== "rebased" && session.status !== "conflicted"))
      return;
    const marker = `${session.sessionId}:${session.updatedAt}:${session.status}`;

    if (lastRevalidated.current === marker) return;
    lastRevalidated.current = marker;
    void summaryQuery.refetch();
  }, [editSession.session, summaryQuery]);

  const selectCollection = useCallback(
    (typeName: string) => {
      if (typeName === PSEUDO_TYPE_ISSUES) {
        tabs.openCollection("issues");

        return;
      }

      if (typeName === PSEUDO_TYPE_MODIFIED) {
        tabs.openCollection("modified");

        return;
      }

      navigate(buildNotesLocation({ selection: selectionForType(typeName) }));
    },
    [tabs.openCollection],
  );

  const selectIssues = useCallback((issueScope?: ValidationScope) => {
    navigate(
      buildNotesLocation({
        selection: { kind: "issues" },
        issueScope: issueScope ?? { kind: "global" },
      }),
    );
  }, []);

  const selectIssue = useCallback(
    (issueKey: string | null) => {
      if (
        tabs.activeId !== "collection:issues" ||
        location.selection.kind !== "issues" ||
        (location.issueKey ?? null) === issueKey
      )
        return;
      navigate(
        buildNotesLocation({
          selection: location.selection,
          issueScope: location.issueScope,
          issueKey,
        }),
        true,
      );
    },
    [location.issueKey, location.issueScope, location.selection, tabs.activeId],
  );

  const selectGroup = useCallback(
    (group: string) => navigate(buildNotesLocation({ selection: { kind: "group", group } })),
    [],
  );

  const openConfiguredView = useCallback(
    (view: ViewCatalogEntry) => tabs.openView(view),
    [tabs.openView],
  );

  // Open panes and staged reads refresh themselves when the session changes.
  // Callbacks depend on the controller's stable actions, not the whole controller,
  // so retained tabs keep equal props while unrelated session state changes.
  const { startEditing, stageOps: stageSessionOps, replaceOps: replaceSessionOps } = editSession;

  const stageOps = useCallback(
    async (ops: OntologyEditOp[]) => {
      startEditing();
      await stageSessionOps(ops);
    },
    [stageSessionOps, startEditing],
  );

  const replaceOps = useCallback(
    async (ops: OntologyEditOp[]) => {
      await replaceSessionOps(ops);
    },
    [replaceSessionOps],
  );

  const registerTabContext = useCallback((tabId: string, context: NoteTabContext | null) => {
    setContexts((current) => {
      if (sameNoteTabContext(current.get(tabId), context)) return current;
      const next = new Map(current);

      if (context) next.set(tabId, context);
      else next.delete(tabId);

      return next;
    });
  }, []);

  const openTab = useCallback(
    (target: string, mode: OpenMode = "activate") => tabs.open(target, { mode }),
    [tabs.open],
  );

  useFinderNoteRequests(openTab);

  const openNode = useCallback(
    (ref: NodeRef, options: OpenNodeOptions) => {
      const target = `${ref.notePath}${ref.fragment ? `#${ref.fragment.replace(/^#/, "")}` : ""}`;
      tabs.open(target, {
        mode: options.beside ? "beside" : "activate",
        nodeRef: ref,
        presentation: options.view,
        nodeId: ref.nodeId,
        nodeKind: ref.kind,
        structural: ref.structuralFingerprint,
      });
    },
    [tabs.open],
  );

  const openView = useCallback(
    (id: string, context: ViewContext) => {
      if (context.kind === "node") {
        openNode(context.ref, { view: id });

        return;
      }

      if (context.kind === "type" || context.kind === "interface") {
        navigate(
          buildNotesLocation({
            selection: selectionForType(context.kind === "type" ? context.type : context.interface),
            presentation: id,
          }),
        );

        return;
      }

      if (context.kind === "group" || context.kind === "workspace") {
        navigate(
          buildNotesLocation({
            selection:
              context.kind === "group" ? { kind: "group", group: context.group } : { kind: "all" },
            presentation: id,
          }),
        );

        return;
      }

      const view = viewCatalogQuery.data?.views.find((candidate) => candidate.id === id);

      if (view) tabs.openView(view);
    },
    [openNode, tabs.openView, viewCatalogQuery.data],
  );

  const openSearch = useCallback(
    ({ folder, query }: { folder: string; query?: string }) =>
      tabs.openSearch(query ?? "", { folder }),
    [tabs.openSearch],
  );

  const selectPresentation = useCallback(
    (id: string | null) =>
      navigate(
        buildNotesLocation({ selection: tabs.homeLocation.selection, presentation: id }),
        true,
      ),
    [tabs.homeLocation.selection],
  );

  const activeContext = tabs.activeTab.kind === "note" ? contexts.get(tabs.activeId) || null : null;
  const activeNotePath = tabs.activeTab.kind === "note" ? tabs.activeTab.path : null;
  const validationQuery = useValidationQuery(active);

  const activeValidationScope = useMemo(
    () => (activeNotePath ? [{ kind: "note" as const, key: activeNotePath }] : []),
    [activeNotePath],
  );

  const validationSummaries = useValidationScopeSummaries(
    validationQuery.data?.snapshot?.generation ?? null,
    activeValidationScope,
  );

  const activeValidationIssueCount = activeNotePath
    ? validationSummaries.summaries.get(validationScopeKey({ kind: "note", key: activeNotePath }))
        ?.issueCount
    : undefined;

  const activeValidationHealth = validationHealth(validationQuery.data ?? null);
  const searchActive = tabs.activeTab.kind === "search";
  const noteSurfacesActive = active && !searchActive;

  const openFromContext = useCallback(
    (path: string, target: "current" | "stack" | "beside" = "stack") =>
      openTab(path, target === "beside" ? "beside" : "activate"),
    [openTab],
  );

  const openNodeFromContext = useCallback(
    (node: StructuralNode) => activeContext?.openNode?.(node),
    [activeContext],
  );

  const refreshSidebar = useCallback(async () => {
    await summaryQuery.refetch();

    if (selectedType && selectedType !== PSEUDO_TYPE_MODIFIED) {
      await queryClient.invalidateQueries({ queryKey: queryKeys.ontology.type(selectedType) });
    }
  }, [queryClient, selectedType, summaryQuery]);

  const commit = useCallback(async () => {
    if (document.activeElement instanceof HTMLElement) document.activeElement.blur();

    try {
      await flushEditors();
    } catch {
      return;
    }

    const committed = await editSession.commit();

    if (!committed) return;

    if (!editCommitSucceeded(committed)) return;

    const hasNewerWork =
      committed.hasUncommittedChanges || hasPersistedEditorDrafts(editSession.vaultKey);

    if (!hasNewerWork) {
      editSession.stopEditing();
      window.requestAnimationFrame(() => {
        document.querySelector<HTMLButtonElement>("button.ontology-edit-toggle")?.focus();
      });
    }

    await refreshSidebar();
    await queryClient.invalidateQueries({ queryKey: queryKeys.views.all() });
  }, [editSession, queryClient, refreshSidebar]);

  const discard = useCallback(async () => {
    const { opCount, touchedPaths } = editSession.summary;

    // Discard drops every staged edit in every note, with no undo.
    if (
      opCount > 0 &&
      !window.confirm(
        `Discard ${opCount} staged change${opCount === 1 ? "" : "s"} to ${touchedPaths.size || 1} note${
          touchedPaths.size === 1 ? "" : "s"
        }? This cannot be undone.`,
      )
    )
      return;

    clearPersistedEditorDrafts(editSession.vaultKey);
    window.dispatchEvent(new Event("rhizome:discard-editor-drafts"));
    await editSession.discard();
    editSession.stopEditing();
    window.requestAnimationFrame(() => {
      document.querySelector<HTMLButtonElement>("button.ontology-edit-toggle")?.focus();
    });

    await refreshSidebar();
    await queryClient.invalidateQueries({ queryKey: queryKeys.views.all() });
  }, [editSession, queryClient, refreshSidebar]);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      const command = event.metaKey || event.ctrlKey;

      // ⌃⌘S toggles the sidebar; it is not a save.
      if (!command || event.isComposing || isSidebarShortcut(event)) return;

      if (
        event.key.toLowerCase() === "s" &&
        (editSession.session?.hasUncommittedChanges || hasLocalDrafts)
      ) {
        event.preventDefault();

        if (!editSession.busy && editSession.session?.status !== "conflicted") void commit();

        return;
      }

      if (event.shiftKey && event.key === "Enter" && editSession.editing) {
        event.preventDefault();
        selectCollection(PSEUDO_TYPE_MODIFIED);

        return;
      }

      if (event.shiftKey && event.key.toLowerCase() === "e" && !editSession.editing) {
        event.preventDefault();
        startEditing();
      }
    };

    window.addEventListener("keydown", onKeyDown);

    return () => window.removeEventListener("keydown", onKeyDown);
  }, [
    commit,
    editSession.busy,
    editSession.editing,
    editSession.session,
    hasLocalDrafts,
    selectCollection,
    startEditing,
  ]);

  const closeTab = useCallback(
    (id: string) => {
      const tab = tabs.tabs.find((candidate) => candidate.id === id);

      if (
        tab?.kind === "note" &&
        tab.dirty &&
        !window.confirm(
          `${tab.title || tab.path} has unsaved changes. Close it? They stay staged in Changes.`,
        )
      )
        return;
      tabs.close(id);
    },
    [tabs],
  );

  // The desktop app's File > Close Tab (Cmd+W) closes the active note tab,
  // only while Notes is the route on screen; it stays mounted under others.
  // Browsers reserve Cmd+W, so a page cannot offer it there.
  useEffect(() => {
    if (!active) return;

    const onCloseTab = () => {
      if (tabs.activeTab.kind !== "home") closeTab(tabs.activeId);
    };

    window.addEventListener("rhizome:close-tab", onCloseTab);

    return () => window.removeEventListener("rhizome:close-tab", onCloseTab);
  }, [active, closeTab, tabs.activeId, tabs.activeTab.kind]);

  // Inactive collection tabs keep one location object per tab model, so their
  // memoized panels see equal props until that tab changes.
  const inactiveCollectionLocations = useMemo(() => {
    const locations = new Map<string, NotesLocation>();

    for (const tab of tabs.tabs) {
      if (tab.kind === "collection") locations.set(tab.id, collectionLocation(tab));
    }

    return locations;
  }, [tabs.tabs]);

  const locationForCollection = (tab: CollectionTab): NotesLocation =>
    tabs.activeId === tab.id
      ? location
      : (inactiveCollectionLocations.get(tab.id) ?? collectionLocation(tab));

  const homeSelection = tabs.homeLocation.selection;
  const homePresentation = tabs.homeLocation.presentation;

  const homeLocation = useMemo<NotesLocation>(
    () => ({
      selection: homeSelection,
      note: null,
      query: null,
      fragment: null,
      view: null,
      presentation: homePresentation,
    }),
    [homePresentation, homeSelection],
  );

  const recentActive =
    tabs.activeTab.kind === "home" &&
    homeSelection.kind === "all" &&
    homePresentation === RECENT_PRESENTATION;

  const openRecent = useCallback(
    () =>
      navigate(
        buildNotesLocation({ selection: { kind: "all" }, presentation: RECENT_PRESENTATION }),
      ),
    [],
  );

  // One right pane: the active note's context, or the selected record of the
  // active collection view. With neither, it stays closed. A first selection
  // opens it a beat later, so the second click of a double-click still lands
  // on the row instead of a table the opening pane just narrowed.
  const record = useSettledTrue(
    recordRail.showing && recordRail.selected && tabs.activeTab.kind !== "note",
    RECORD_PANE_DELAY_MS,
  );

  const paneHasContent = !searchActive && (tabs.activeTab.kind === "note" || record);
  const paneOpen = paneHasContent && !panes.paneCollapsed;
  const sidebarHidden = useWorkbenchSidebar().collapsed;
  const { togglePane: togglePaneState } = panes;

  const paneToggleRef = useRef<HTMLButtonElement>(null);

  // Closing the pane from a control inside it would drop focus to the page;
  // hand it to the pane toggle in the tab row instead.
  const togglePane = useCallback(() => {
    const fromInside = document
      .getElementById("notes-right-pane")
      ?.contains(document.activeElement);

    togglePaneState();

    if (fromInside) requestAnimationFrame(() => paneToggleRef.current?.focus());
  }, [togglePaneState]);

  useEffect(() => {
    if (!active || !paneHasContent) return;

    const onKeyDown = (event: KeyboardEvent) => {
      if (
        event.key !== "]" ||
        event.metaKey ||
        event.ctrlKey ||
        event.altKey ||
        event.defaultPrevented ||
        typingIn(event.target) ||
        !readSingleKey()
      )
        return;
      event.preventDefault();
      togglePane();
    };

    window.addEventListener("keydown", onKeyDown);

    return () => window.removeEventListener("keydown", onKeyDown);
  }, [active, paneHasContent, togglePane]);

  const shellStyle: ShellStyle = {
    "--pane-width": `${panes.paneWidth}px`,
  };

  const homeSelectedType = useMemo(
    () => selectionType(homeSelection, summaryQuery.data),
    [homeSelection, summaryQuery.data],
  );

  const viewEditSession = useMemo(
    () => ({
      session: editSession.session,
      readLifecycle: editSession.readLifecycle,
      vaultKey: editSession.vaultKey,
      busy: editSession.busy,
      unacknowledged: editSession.unacknowledged,
    }),
    [
      editSession.busy,
      editSession.readLifecycle,
      editSession.session,
      editSession.unacknowledged,
      editSession.vaultKey,
    ],
  );

  const homeEditSession = useMemo(
    () => ({ ...viewEditSession, replaceOps }),
    [replaceOps, viewEditSession],
  );

  const renderHomeTab = (
    id: string,
    selection: NotesLocation["selection"],
    tabSelectedType: string | null,
    tabLocation: NotesLocation,
  ) => (
    <HomeTab
      active={noteSurfacesActive && tabs.activeId === id}
      summary={summary}
      selection={selection}
      selectedType={tabSelectedType}
      location={tabLocation}
      editSession={homeEditSession}
      onOpenNote={openTab}
      onOpenNode={openNode}
      onOpenView={openView}
      onOpenSearch={openSearch}
      onPresentation={selectPresentation}
      onSelectCollection={selectCollection}
      onOpenIssues={selectIssues}
      onSelectIssue={selectIssue}
      onStageOps={stageOps}
    />
  );

  const vaultName = statusQuery.data?.vaultName;

  return (
    <main
      className="workspace workspace--ontology workbench notes-shell"
      aria-label="Notes workspace"
      ref={shellRef}
      style={shellStyle}
    >
      <h1 className="sr-only">Notes</h1>
      <WorkbenchSidebar
        title={desktop || !vaultName ? null : vaultLabel(vaultName)}
        titleHint={statusQuery.data?.vaultPath}
      >
        <DiagnosticRegion name="sidebar">
          <NotesLeftRail
            active={active && !sidebarHidden}
            summary={summary}
            selectedType={selectedType}
            // A note or view tab is not a collection, so no sidebar row is current.
            activeCollection={
              tabs.activeTab.kind === "home" || tabs.activeTab.kind === "collection"
                ? selectedType
                : null
            }
            activeViewID={tabs.activeTab.kind === "view" ? tabs.activeTab.viewId : null}
            editSession={editSession.session}
            touchedPaths={touchedPaths}
            onSelectCollection={selectCollection}
            onSelectGroup={selectGroup}
            activeGroup={
              tabs.activeTab.kind === "home" && homeSelection.kind === "group"
                ? homeSelection.group
                : null
            }
            recentActive={recentActive}
            onOpenRecent={openRecent}
            onOpenConfiguredView={openConfiguredView}
          />
        </DiagnosticRegion>
      </WorkbenchSidebar>
      <div className="workbench__panel notes-shell__frame">
        <div className="notes-shell__tabs-row">
          <NoteTabStrip
            tabs={tabs.tabs}
            activeId={tabs.activeId}
            onActivate={tabs.activate}
            onClose={closeTab}
          />
          {!searchActive ? (
            <div className="notes-shell__session">
              <OntologyEditSessionPanel
                session={editSession.session}
                editing={editSession.editing}
                busy={editSession.busy}
                saving={editSession.saving}
                error={editSession.error}
                warnings={editSession.warnings}
                notice={editSession.notice}
                hasLocalDrafts={hasLocalDrafts}
                onStartEditing={startEditing}
                onReview={() => selectCollection(PSEUDO_TYPE_MODIFIED)}
                onCommit={() => void commit()}
                onDiscard={() => void discard()}
              />
            </div>
          ) : null}
          {searchActive ? null : (
            <button
              ref={paneToggleRef}
              type="button"
              className="notes-shell__pane-toggle"
              aria-label={paneOpen ? "Hide right pane" : "Show right pane"}
              aria-expanded={paneOpen}
              aria-keyshortcuts="]"
              title={paneOpen ? "Hide right pane (])" : "Show right pane (])"}
              disabled={!paneHasContent}
              onClick={togglePane}
            >
              <PanelIcon side="right" />
            </button>
          )}
        </div>
        <div className={`notes-shell__body${searchActive ? " notes-shell__body--search" : ""}`}>
          {tabs.tabs.flatMap((tab) =>
            tab.kind === "search"
              ? [
                  <section
                    key={tab.id}
                    id={`panel-${tab.id}`}
                    role="tabpanel"
                    aria-labelledby={`tab-${tab.id}`}
                    className={`notes-shell__panel${tabs.activeId === tab.id ? " is-active" : ""}`}
                    inert={tabs.activeId === tab.id ? undefined : true}
                  >
                    <SearchWorkspace
                      tab={tab}
                      active={active && tabs.activeId === tab.id}
                      restoreReady={tabs.hydrated}
                      types={summary?.types ?? []}
                      editSession={editSession.session}
                      onRefineSearch={tabs.refineSearch}
                      onOpenNote={openTab}
                      onScrollPosition={tabs.setSearchScroll}
                    />
                  </section>,
                ]
              : [],
          )}
          <RecordRailProvider value={recordRail.rail}>
            <div
              className="notes-shell__note-surfaces"
              hidden={searchActive}
              inert={searchActive ? true : undefined}
            >
              <section
                id="panel-home"
                role="tabpanel"
                aria-labelledby="tab-home"
                className={`notes-shell__panel${tabs.activeId === "home" ? " is-active" : ""}`}
                inert={tabs.activeId === "home" ? undefined : true}
              >
                {renderHomeTab("home", homeSelection, homeSelectedType, homeLocation)}
              </section>
              {tabs.tabs.flatMap((tab) =>
                tab.kind === "collection"
                  ? [
                      <section
                        key={tab.id}
                        id={`panel-${tab.id}`}
                        role="tabpanel"
                        aria-labelledby={`tab-${tab.id}`}
                        className={`notes-shell__panel${tabs.activeId === tab.id ? " is-active" : ""}`}
                        inert={tabs.activeId === tab.id ? undefined : true}
                      >
                        {renderHomeTab(
                          tab.id,
                          COLLECTION_SELECTIONS[tab.collection],
                          tab.collection === "issues" ? PSEUDO_TYPE_ISSUES : PSEUDO_TYPE_MODIFIED,
                          locationForCollection(tab),
                        )}
                      </section>,
                    ]
                  : [],
              )}
              {tabs.tabs.flatMap((tab) =>
                tab.kind === "view"
                  ? [
                      <section
                        key={tab.id}
                        id={`panel-${tab.id}`}
                        role="tabpanel"
                        aria-labelledby={`tab-${tab.id}`}
                        className={`notes-shell__panel${tabs.activeId === tab.id ? " is-active" : ""}`}
                        inert={tabs.activeId === tab.id ? undefined : true}
                      >
                        <ViewTab
                          tab={tab}
                          active={noteSurfacesActive && tabs.activeId === tab.id}
                          editSession={viewEditSession}
                          onOpenNote={openTab}
                          onOpenNode={openNode}
                          onOpenView={openView}
                          onOpenSearch={openSearch}
                          onStageOps={stageOps}
                          onOpenIssues={selectIssues}
                          onSelectCollection={selectCollection}
                          onTitle={tabs.setTitle}
                        />
                      </section>,
                    ]
                  : [],
              )}
              {tabs.tabs.flatMap((tab) =>
                tab.kind === "note"
                  ? [
                      <section
                        key={tab.id}
                        id={`panel-${tab.id}`}
                        role="tabpanel"
                        aria-labelledby={`tab-${tab.id}`}
                        className={`notes-shell__panel${tabs.activeId === tab.id ? " is-active" : ""}`}
                        inert={tabs.activeId === tab.id ? undefined : true}
                      >
                        <DiagnosticRegion name="note-view">
                          <NoteTab
                            tab={tab}
                            views={viewCatalogQuery.data}
                            onPresentation={tabs.setPresentation}
                            onOpenNode={openNode}
                            onOpenView={openView}
                            onOpenSearch={openSearch}
                            active={noteSurfacesActive && tabs.activeId === tab.id}
                            anchor={
                              tabs.activeId === tab.id &&
                              location.note !== null &&
                              normalizeNotePath(location.note) === tab.path
                                ? location.fragment
                                : null
                            }
                            editSession={editSession.session}
                            readLifecycle={editSession.readLifecycle}
                            savedWorkspaces={editSession.savedWorkspaces}
                            editing={editSession.editing}
                            onOpen={openTab}
                            onTitle={tabs.setTitle}
                            onFocusedTarget={tabs.setFocusedTarget}
                            registerContext={registerTabContext}
                            onStageOps={stageOps}
                            vaultKey={editSession.vaultKey}
                            validationIssueCount={
                              tab.path === activeNotePath ? activeValidationIssueCount : undefined
                            }
                            validationHealth={activeValidationHealth}
                            onSelectIssues={selectIssues}
                            onSelectCollection={selectCollection}
                          />
                        </DiagnosticRegion>
                      </section>,
                    ]
                  : [],
              )}
              <NotesRightRail
                active={noteSurfacesActive}
                context={activeContext}
                editSession={editSession.session}
                open={paneOpen}
                overlay={panes.paneOverlay}
                onClose={togglePane}
                onOpen={openFromContext}
                onOpenNode={openNodeFromContext}
                validationIssueCount={activeValidationIssueCount}
                validationHealth={activeValidationHealth}
                record={record}
                onRecordSlot={recordRail.setSlot}
                resizeHandle={
                  panes.paneOverlay ? null : (
                    <PanelResizeHandle
                      label="Resize right pane"
                      range={PANE_RANGE}
                      value={panes.paneWidth}
                      edge="start"
                      cssVar="--pane-width"
                      target={shellRef}
                      onResize={panes.setPaneWidth}
                    />
                  )
                }
              />
            </div>
          </RecordRailProvider>
        </div>
      </div>
    </main>
  );
}
