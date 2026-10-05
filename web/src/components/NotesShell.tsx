import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { getStatus } from "../api/client";
import { queryKeys } from "../api/queryKeys";
import type {
  OntologyEditOp,
  NodeRef,
  StructuralNode,
  ValidationScope,
  ViewCatalogEntry,
} from "../api/types";
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
import { NotesRightRail, useNotesRightRailState } from "./NotesRightRail";
import { RecordRailProvider, useRecordRailState } from "./recordRail";
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

// F6 order: tab row, left rail, the active panel, context rail.
const FOCUS_REGIONS =
  ".notes-shell__tabs-row, .notes-left-rail, .notes-shell__panel.is-active, .notes-right-rail";

export function NotesShell({ active = true }: { active?: boolean }) {
  const queryClient = useQueryClient();
  const shellRef = useRef<HTMLDivElement>(null);
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
  const contextRail = useNotesRightRailState();
  const recordRail = useRecordRailState();
  const [homeCollectionView, setHomeCollectionView] = useState(false);

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
  const stageOps = useCallback(
    async (ops: OntologyEditOp[]) => {
      editSession.startEditing();
      await editSession.stageOps(ops);
    },
    [editSession],
  );

  const replaceOps = useCallback(
    async (ops: OntologyEditOp[]) => {
      await editSession.replaceOps(ops);
    },
    [editSession],
  );

  const startEditing = editSession.startEditing;

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

      if (context.kind === "group") {
        navigate(
          buildNotesLocation({
            selection: { kind: "group", group: context.group },
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

      if (!command || event.isComposing) return;

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

  const locationForCollection = (tab: CollectionTab): NotesLocation => {
    if (tabs.activeId === tab.id) return location;

    return {
      selection: { kind: tab.collection },
      note: null,
      query: null,
      fragment: null,
      view: null,
      issueScope: tab.collection === "issues" ? tab.issueScope : null,
      issueKey: tab.collection === "issues" ? tab.issueKey : null,
    };
  };

  const homeSelection = tabs.homeLocation.selection;

  const homeLocation: NotesLocation = {
    selection: homeSelection,
    note: null,
    query: null,
    fragment: null,
    view: null,
    presentation: tabs.homeLocation.presentation,
  };

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
      editSession={{
        session: editSession.session,
        replaceOps,
        readLifecycle: editSession.readLifecycle,
        vaultKey: editSession.vaultKey,
        busy: editSession.busy,
        unacknowledged: editSession.unacknowledged,
      }}
      onOpenNote={openTab}
      onOpenNode={openNode}
      onOpenView={openView}
      onPresentation={selectPresentation}
      onSelectCollection={selectCollection}
      onOpenIssues={selectIssues}
      onSelectIssue={selectIssue}
      onStageOps={stageOps}
      onCollectionViewChange={id === "home" ? setHomeCollectionView : undefined}
    />
  );

  return (
    <div className="workspace workspace--ontology notes-shell" ref={shellRef}>
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
                    types={summary?.types ?? []}
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
            <NotesLeftRail
              active={noteSurfacesActive}
              summary={summary}
              selectedType={selectedType}
              activeCollection={tabs.activeTab.kind === "view" ? null : selectedType}
              activeViewID={tabs.activeTab.kind === "view" ? tabs.activeTab.viewId : null}
              activeTabID={tabs.activeId}
              collectionView={tabs.activeId === "home" && homeCollectionView}
              editSession={editSession.session}
              touchedPaths={touchedPaths}
              findTabByPath={tabs.findByPath}
              onSelectCollection={selectCollection}
              onSelectGroup={selectGroup}
              activeGroup={
                tabs.activeTab.kind === "home" && homeSelection.kind === "group"
                  ? homeSelection.group
                  : null
              }
              onOpenConfiguredView={openConfiguredView}
              onOpenNote={openTab}
            />
            <section
              id="panel-home"
              role="tabpanel"
              aria-labelledby="tab-home"
              className={`notes-shell__panel${tabs.activeId === "home" ? " is-active" : ""}`}
              inert={tabs.activeId === "home" ? undefined : true}
            >
              {renderHomeTab(
                "home",
                homeSelection,
                selectionType(homeSelection, summaryQuery.data),
                homeLocation,
              )}
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
                        { kind: tab.collection },
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
                        editSession={{
                          session: editSession.session,
                          readLifecycle: editSession.readLifecycle,
                          vaultKey: editSession.vaultKey,
                          busy: editSession.busy,
                          unacknowledged: editSession.unacknowledged,
                        }}
                        onOpenNote={openTab}
                        onOpenNode={openNode}
                        onOpenView={openView}
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
                      <NoteTab
                        tab={tab}
                        views={viewCatalogQuery.data}
                        onPresentation={tabs.setPresentation}
                        onOpenNode={openNode}
                        onOpenView={openView}
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
                        contextCollapsed={contextRail.collapsed}
                        onToggleContext={contextRail.toggle}
                        onStageOps={stageOps}
                        vaultKey={editSession.vaultKey}
                        validationIssueCount={
                          tab.path === activeNotePath ? activeValidationIssueCount : undefined
                        }
                        validationHealth={activeValidationHealth}
                        onOpenIssues={() => selectIssues({ kind: "note", key: tab.path })}
                        onSelectIssues={selectIssues}
                        onSelectCollection={selectCollection}
                      />
                    </section>,
                  ]
                : [],
            )}
            <NotesRightRail
              active={noteSurfacesActive}
              context={activeContext}
              editSession={editSession.session}
              collapsed={contextRail.collapsed}
              onToggleCollapsed={contextRail.toggle}
              onOpen={openFromContext}
              onOpenNode={openNodeFromContext}
              validationIssueCount={activeValidationIssueCount}
              validationHealth={activeValidationHealth}
              record={recordRail.showing && tabs.activeTab.kind !== "note"}
              onRecordSlot={recordRail.setSlot}
            />
          </div>
        </RecordRailProvider>
      </div>
    </div>
  );
}
