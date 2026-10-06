import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { EditReadLifecycle } from "../staging/stagedQuery";

import { publicWorkspaceRef } from "../api/client";
import type {
  NodeRef,
  NodeWorkspace,
  OntologyEditOp,
  OntologyEditSessionResponse,
  StructuralNode,
  ValidationHealth,
  ValidationScope,
  ViewCatalog,
  ViewTarget,
} from "../api/types";
import type { NoteTabContext } from "./noteTabContext";
import { publicTypeName } from "../lib/typeNames";
import { canonicalNodeRefKey, nodeRefMatches } from "./nodeRef";
import { noteTabID } from "./noteTabIdentity";
import { NodePresentationHost } from "./NodePresentationHost";
import { ViewSelector } from "../views/ViewSelector";
import { useViewSelection } from "../views/useViewSelection";
import { type OpenNodeOptions, type ViewContext } from "../views/context";
import type { ViewServices } from "../views/ViewHost";
import { flushEditors } from "./editing/editorFlush";
import { OntologyNotePane } from "./OntologyNotePane";
import { buildNoteWebHref } from "../lib/content";
import { normalizeNotePath, splitNoteTarget } from "./notesRoute";
import type { NoteTab as NoteTabModel, OpenMode } from "./useNoteTabs";
import { targetRefForNode, usePaneStack } from "./usePaneStack";
import { parseIssueTarget } from "./validation/issueNavigation";

type OpenTarget = "current" | "stack" | "beside";

export function NoteTab({
  tab,
  active,
  anchor,
  editSession,
  readLifecycle,
  savedWorkspaces,
  editing,
  onOpen,
  onTitle,
  onFocusedTarget,
  registerContext,
  contextCollapsed = false,
  onToggleContext,
  onStageOps,
  vaultKey = null,
  validationIssueCount,
  validationHealth,
  onOpenIssues,
  onSelectIssues,
  onSelectCollection,
  views,
  onPresentation,
  onOpenNode: openCanonicalNode,
  onOpenView,
  onOpenSearch,
}: {
  tab: NoteTabModel;
  active: boolean;
  anchor: string | null;
  editSession: OntologyEditSessionResponse | null;
  readLifecycle?: EditReadLifecycle;
  /** Workspaces the last save returned; see usePaneStack. */
  savedWorkspaces?: NodeWorkspace[];
  editing: boolean;
  onOpen: (target: string, mode: OpenMode) => void;
  onTitle: (tabId: string, title: string) => void;
  onFocusedTarget?: (tabId: string, target: string, requestedTarget: string) => void;
  registerContext: (tabId: string, context: NoteTabContext | null) => void;
  contextCollapsed?: boolean;
  onToggleContext?: () => void;
  onStageOps: (ops: OntologyEditOp[]) => Promise<void>;
  vaultKey?: string | null;
  validationIssueCount?: number;
  validationHealth?: ValidationHealth;
  onOpenIssues?: () => void;
  /** Opens issues for a scope a presentation names; without one, the note's own issues. */
  onSelectIssues?: (scope: ValidationScope) => void;
  onSelectCollection?: (typeName: string) => void;
  onOpenNode?: (ref: NodeRef, options: OpenNodeOptions) => void;
  onOpenView?: (id: string, context: ViewContext) => void;
  onOpenSearch?: ViewServices["onOpenSearch"];
  views?: ViewCatalog;
  onPresentation?: (tabId: string, presentation: string | null) => void;
}) {
  const { paneStack, openRootPane, focusOrPushNodeFromIndex, closePaneAt, loadPane } = usePaneStack(
    editSession,
    savedWorkspaces,
  );

  const openedRef = useRef(false);
  const lastRequestedRef = useRef<string | null>(null);
  const fragment = active ? anchor : (tab.fragment ?? null);
  const issueTarget = useMemo(() => parseIssueTarget(fragment), [fragment]);

  const targetFragment = issueTarget?.nodeId
    ? `node:${issueTarget.nodeId}`
    : issueTarget
      ? null
      : fragment;

  const requestedTarget = `${tab.path}${tab.query ? `?${tab.query}` : ""}${targetFragment ? `#${targetFragment}` : ""}`;

  const requestedNodeRef = useMemo<NodeRef | null>(() => {
    if (issueTarget || (!tab.nodeRef && !tab.nodeId && !tab.nodeKind && !tab.structural))
      return null;

    return {
      ...tab.nodeRef,
      notePath: tab.path,
      kind: tab.nodeKind || tab.nodeRef?.kind || "NOTE",
      fragment: targetFragment || undefined,
      nodeId: tab.nodeId || tab.nodeRef?.nodeId || undefined,
      structuralFingerprint: tab.structural || tab.nodeRef?.structuralFingerprint || undefined,
    };
  }, [
    issueTarget,
    tab.nodeId,
    tab.nodeKind,
    tab.nodeRef,
    tab.path,
    tab.structural,
    targetFragment,
  ]);

  const requestedIdentity = requestedNodeRef
    ? `${requestedTarget}|${canonicalNodeRefKey(requestedNodeRef)}`
    : requestedTarget;

  const [issueFallback, setIssueFallback] = useState(false);
  const rootWorkspace = paneStack[0]?.workspace;

  useEffect(() => {
    if (!active || openedRef.current) return;
    openedRef.current = true;
    void openRootPane(tab.path);
  }, [active, openRootPane, tab.path]);

  useEffect(() => setIssueFallback(false), [issueTarget]);

  useEffect(() => {
    if (!paneStack[0]?.workspace || lastRequestedRef.current === requestedIdentity) return;
    lastRequestedRef.current = requestedIdentity;

    if (
      (!fragment && (!requestedNodeRef || requestedNodeRef.kind === "NOTE")) ||
      rootWorkspace?.content.format === "html"
    ) {
      if (paneStack.length > 1) closePaneAt(1);

      return;
    }

    const existing = paneStack.findIndex((pane) =>
      requestedNodeRef
        ? nodeRefMatches(requestedNodeRef, pane.resolvedRef)
        : pane.requestedRef === requestedTarget ||
          (pane.resolvedRef && publicWorkspaceRef(pane.resolvedRef) === requestedTarget),
    );

    if (existing >= 0) {
      if (existing < paneStack.length - 1) closePaneAt(existing + 1);

      return;
    }

    focusOrPushNodeFromIndex(paneStack.length - 1, requestedNodeRef || requestedTarget);
  }, [
    closePaneAt,
    focusOrPushNodeFromIndex,
    fragment,
    paneStack,
    requestedTarget,
    requestedIdentity,
    requestedNodeRef,
    rootWorkspace?.content.format,
  ]);

  const rootTitle =
    rootWorkspace?.node.ref.kind === "NOTE" ? rootWorkspace.content?.title : undefined;

  const currentIndex = Math.max(0, paneStack.length - 1);
  const current = paneStack[currentIndex];
  const workspace = current?.workspace;
  const subjectKind = tab.nodeKind || tab.nodeRef?.kind;

  const tabTitle =
    subjectKind && subjectKind !== "NOTE"
      ? workspace?.node.ref.kind !== "NOTE"
        ? workspace?.content.title
        : undefined
      : rootTitle;

  useEffect(() => {
    if (tabTitle) onTitle(tab.id, tabTitle);
  }, [onTitle, tabTitle, tab.id]);

  const resolvedType = publicTypeName(workspace?.node.resolvedType);

  const nodeContext = useMemo<ViewContext | null>(
    () => (workspace ? { kind: "node", type: resolvedType, ref: workspace.node.ref } : null),
    [resolvedType, workspace],
  );

  // Selection is remembered per concrete node, so it keeps the internal fallback
  // type the public context hides; the server requires a nonempty type.
  const selectionContext = useMemo<ViewContext | undefined>(() => {
    const kind = workspace?.node.ref.kind.toUpperCase();

    const type =
      workspace?.node.ref.typeName?.trim() ||
      workspace?.node.resolvedType?.trim() ||
      (kind === "NOTE" ? "_FallbackNote" : kind === "SECTION" ? "_FallbackSection" : undefined);

    return workspace && type ? { kind: "node", type, ref: workspace.node.ref } : undefined;
  }, [workspace]);

  const target = useMemo<ViewTarget>(() => {
    const configured = views?.targets?.find(
      (entry) => entry.kind === "node" && entry.name === resolvedType,
    );

    const markdown = !workspace?.content.format || workspace.content.format === "markdown";

    return {
      kind: "node",
      name: resolvedType,
      defaultChoiceId: configured?.defaultChoiceId || "builtin:read",
      choices: (
        configured?.choices ?? [
          { id: "builtin:read", name: "Structure", renderer: "read" },
          { id: "builtin:source", name: "Markdown", renderer: "source" },
        ]
      ).map((choice) =>
        choice.renderer === "read"
          ? { ...choice, name: markdown ? "Structure" : "Preview" }
          : choice.renderer === "source"
            ? { ...choice, name: markdown ? "Markdown" : "Source" }
            : choice,
      ),
    };
  }, [resolvedType, views, workspace?.content.format]);

  const sourceIssue =
    issueTarget?.unit && !issueTarget.nodeId && !issueTarget.field && !issueTarget.relation;

  const [issuePresentation, setIssuePresentation] = useState<{
    target: string;
    id: string | null;
  } | null>(null);

  const selection = useViewSelection({
    target,
    vaultKey,
    subject: workspace ? noteTabID(workspace.node.ref.notePath, workspace.node.ref) : tab.path,
    context: selectionContext,
    explicit: issueTarget
      ? issuePresentation?.target === requestedTarget
        ? issuePresentation.id
        : sourceIssue
          ? "builtin:source"
          : "builtin:read"
      : tab.presentation,
    onSelect: (id) => onPresentation?.(tab.id, id),
  });

  const choice = selection.choice;
  // Content loads regardless; only the presentation waits for a remembered choice.
  const awaitingChoice = selection.loading && !issueTarget && !tab.presentation;

  const viewMode =
    choice?.renderer === "source" ? "source" : choice?.renderer === "custom" ? "custom" : "read";

  const definition = views?.views.find((view) => view.id === choice?.viewId) ?? null;
  const [presentationError, setPresentationError] = useState<string | null>(null);

  const changePresentation = async (id: string) => {
    try {
      await flushEditors();
      setPresentationError(null);

      if (issueTarget) setIssuePresentation({ target: requestedTarget, id });

      selection.select(id);
    } catch (error) {
      setPresentationError(
        error instanceof Error ? error.message : "Unable to stage editor changes",
      );
    }
  };

  useEffect(() => {
    if (!issueTarget || !current?.error || paneStack.length <= 1) return;
    setIssueFallback(true);
    closePaneAt(1);
  }, [closePaneAt, current?.error, issueTarget, paneStack.length]);

  useEffect(() => {
    if (!current?.resolvedRef || current.loading || current.error) return;
    const resolvedTarget = publicWorkspaceRef(current.resolvedRef);

    // A render that starts a new drill still holds the previous workspace.
    if (current.requestedRef !== requestedTarget && resolvedTarget !== requestedTarget) return;
    onFocusedTarget?.(tab.id, resolvedTarget, requestedTarget);
  }, [current, onFocusedTarget, requestedTarget, tab.id]);

  const openPath = useCallback(
    (target: string, mode: OpenTarget = "stack") => {
      const parsed = splitNoteTarget(target);
      const path = normalizeNotePath(parsed.path || tab.path);
      const normalized = `${path}${parsed.query ? `?${parsed.query}` : ""}${parsed.fragment ? `#${parsed.fragment}` : ""}`;
      onOpen(normalized, mode === "beside" ? "beside" : "activate");
    },
    [onOpen, tab.path],
  );

  const openNode = useCallback(
    (node: StructuralNode) => onOpen(publicWorkspaceRef(targetRefForNode(node)), "activate"),
    [onOpen],
  );

  const handleContextChange = useCallback(
    (context: NoteTabContext | null) => registerContext(tab.id, context),
    [registerContext, tab.id],
  );

  if (!current)
    return (
      <div className="ontology-empty" role="status">
        Loading note…
      </div>
    );

  return (
    <div className="notes-note-tab">
      <OntologyNotePane
        loading={current.loading}
        error={current.error}
        onRetry={() =>
          void loadPane(current.id, current.resolvedRef || current.requestedRef || tab.path)
        }
        workspace={current.workspace}
        editSession={editSession}
        editing={editing}
        initialAnchor={current.anchor}
        documentQuery={tab.query}
        applicationHref={buildNoteWebHref(requestedTarget)}
        viewMode={viewMode}
        presentationControls={
          <>
            <ViewSelector
              target={target}
              selectedId={choice?.id ?? null}
              onSelect={(id) => void changePresentation(id)}
              status={selection}
            />
            {presentationError && <span role="alert">{presentationError}</span>}
          </>
        }
        renderPresentation={
          awaitingChoice
            ? () => <p role="status">Loading view…</p>
            : choice && nodeContext
              ? (body) => (
                  <NodePresentationHost
                    choice={choice}
                    context={nodeContext}
                    view={{ id: choice.viewId ?? choice.id, name: choice.name }}
                    definition={definition}
                    active={active}
                    body={body}
                    services={{
                      session: editSession,
                      readLifecycle,
                      vaultKey,
                      onStageOps,
                      onOpenNote: (path, mode) => onOpen(path, mode ?? "activate"),
                      onOpenNode:
                        openCanonicalNode ??
                        ((ref, options) =>
                          onOpen(publicWorkspaceRef(ref), options?.beside ? "beside" : "activate")),
                      onOpenView,
                      onOpenIssues: (scope) =>
                        scope && onSelectIssues ? onSelectIssues(scope) : onOpenIssues?.(),
                      onSelectCollection,
                      onOpenSearch,
                    }}
                  />
                )
              : undefined
        }
        contextCollapsed={contextCollapsed}
        onToggleContext={onToggleContext}
        breadcrumbs={
          paneStack.length > 1 ? (
            <nav className="notes-crumbs" aria-label="Node path">
              {paneStack.slice(0, -1).map((pane) => (
                <button
                  key={pane.id}
                  type="button"
                  onClick={() =>
                    onOpen(
                      pane.resolvedRef
                        ? publicWorkspaceRef(pane.resolvedRef)
                        : pane.requestedRef || tab.path,
                      "activate",
                    )
                  }
                >
                  {pane.workspace?.content?.title || pane.anchor || pane.path || "Note"}
                </button>
              ))}
              <span aria-hidden="true">›</span>
              <span>{current.workspace?.content?.title || current.anchor || "Node"}</span>
            </nav>
          ) : null
        }
        onContextChange={handleContextChange}
        onOpen={openPath}
        onOpenNode={openNode}
        onStageOps={onStageOps}
        vaultKey={vaultKey}
        issueTarget={issueTarget}
        issueFallback={issueFallback}
        validationIssueCount={validationIssueCount}
        validationHealth={validationHealth}
        onOpenIssues={onOpenIssues}
      />
    </div>
  );
}
