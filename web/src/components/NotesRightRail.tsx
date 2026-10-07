import { type KeyboardEvent, useCallback, useEffect, useMemo, useRef, useState } from "react";

import type {
  NodeWorkspace,
  NodeWorkspaceGraph,
  OntologyEditSessionResponse,
  RenderedSection,
  StructuralNode,
  ValidationHealth,
} from "../api/types";
import type { NoteTabContext } from "./noteTabContext";
import { NotesSidebarInfo } from "./NotesSidebarInfo";
import { OntologyLocalGraph } from "./OntologyLocalGraph";
import { useFocusedWorkspaceGraphQuery } from "./useNotesQueries";

type RailTab = "info" | "outline" | "graph";

type OpenTarget = "current" | "stack" | "beside";

const RAIL_COLLAPSED_KEY = "rhizome:notes:right-rail:collapsed:v1";

const RAIL_TAB_KEY = "rhizome:notes:right-rail:tab:v1";

const RAIL_TABS: Array<{ id: RailTab; label: string }> = [
  { id: "info", label: "Info" },
  { id: "outline", label: "Outline" },
  { id: "graph", label: "Graph" },
];

function isRailTab(value: string | null): value is RailTab {
  return RAIL_TABS.some((tab) => tab.id === value);
}

function readCollapsed(): boolean {
  if (typeof window === "undefined") return false;

  try {
    return window.sessionStorage.getItem(RAIL_COLLAPSED_KEY) === "true";
  } catch {
    return false;
  }
}

export function useNotesRightRailState() {
  const [collapsed, setCollapsed] = useState(readCollapsed);
  useEffect(() => {
    writeSessionValue(RAIL_COLLAPSED_KEY, String(collapsed));
  }, [collapsed]);
  const toggle = () => setCollapsed((value) => !value);

  return { collapsed, toggle };
}

function readTab(): RailTab {
  if (typeof window === "undefined") return "info";

  try {
    const stored = window.sessionStorage.getItem(RAIL_TAB_KEY);

    return isRailTab(stored) ? stored : "info";
  } catch {
    return "info";
  }
}

function writeSessionValue(key: string, value: string): void {
  if (typeof window === "undefined") return;

  try {
    window.sessionStorage.setItem(key, value);
  } catch {
    // Session storage is optional in private browsing and embedded contexts.
  }
}

function graphHasContent(graph: NodeWorkspaceGraph | null | undefined): boolean {
  return Boolean(
    graph?.views?.localGraph &&
    ((graph.views.localGraph.nodeIds?.length || 0) > 0 ||
      (graph.nodes?.length || 0) > 0 ||
      (graph.edges?.length || 0) > 0),
  );
}

function graphWorkspace(
  workspace: NodeWorkspace,
  graph: NodeWorkspaceGraph | null | undefined,
): NodeWorkspace | null {
  if (!graphHasContent(graph)) return null;

  return {
    ...workspace,
    focusedNodeId: graph?.focusedNodeId,
    nodes: graph?.nodes || [],
    edges: graph?.edges || [],
    views: graph?.views,
  };
}

function containsSection(section: RenderedSection, id: string | null): boolean {
  return (
    id !== null &&
    (section.id === id || (section.children || []).some((child) => containsSection(child, id)))
  );
}

function flattenOutline(sections: RenderedSection[]): RenderedSection[] {
  return sections.flatMap((section) => [section, ...flattenOutline(section.children || [])]);
}

function scrollParent(element: HTMLElement): HTMLElement | null {
  for (let current = element.parentElement; current; current = current.parentElement) {
    const overflow = getComputedStyle(current).overflowY;

    if (overflow === "auto" || overflow === "scroll") return current;
  }

  return null;
}

const USER_SCROLL_EVENTS = ["wheel", "touchstart", "keydown", "pointerdown"] as const;

/**
 * Tracks the section at the top of the note's scroll viewport. A clicked
 * section stays current until the user scrolls, so a short section near the
 * end of the note, which never reaches the top, still shows as current.
 */
function useCurrentSection(
  outline: NoteTabContext["outline"],
): [string | null, (section: RenderedSection) => void] {
  const [current, setCurrent] = useState<string | null>(null);
  const pinned = useRef(false);

  useEffect(() => {
    const { target } = outline;

    if (!target) return;
    const sections = flattenOutline(outline.sections);
    let frame = 0;

    const update = () => {
      frame = 0;

      if (pinned.current) return;
      const targets = sections.map((section) => ({ id: section.id, element: target(section) }));
      const first = targets.find((entry) => entry.element)?.element;
      const scroller = first ? scrollParent(first) : null;

      if (!scroller) return;
      const top = scroller.getBoundingClientRect().top + 32;
      let next: string | null = null;

      for (const { id, element } of targets) {
        // A section inside a collapsed disclosure is not on screen; the
        // disclosure's own heading is, so only ancestors count.
        if (!element || element.parentElement?.closest("details:not([open])")) continue;

        if (element.getBoundingClientRect().top <= top) next = id;
      }

      setCurrent(next);
    };

    const schedule = () => {
      if (!frame) frame = requestAnimationFrame(update);
    };

    // Section targets register as the note renders, so start on the next
    // frame and follow any scroll (capture sees non-bubbling element scrolls).
    const release = () => {
      pinned.current = false;
    };

    schedule();
    document.addEventListener("scroll", schedule, { capture: true, passive: true });
    // Opening or closing a section moves everything after it; `toggle` does not bubble.
    document.addEventListener("toggle", schedule, { capture: true });
    USER_SCROLL_EVENTS.forEach((type) =>
      document.addEventListener(type, release, { capture: true, passive: true }),
    );

    return () => {
      document.removeEventListener("scroll", schedule, { capture: true });
      document.removeEventListener("toggle", schedule, { capture: true });
      USER_SCROLL_EVENTS.forEach((type) =>
        document.removeEventListener(type, release, { capture: true }),
      );
      cancelAnimationFrame(frame);
    };
  }, [outline]);

  const navigate = useCallback(
    (section: RenderedSection) => {
      outline.navigate(section);
      pinned.current = true;
      setCurrent(section.id);
    },
    [outline],
  );

  return [current, navigate];
}

function OutlineNode({
  section,
  depth,
  current,
  onNavigate,
}: {
  section: RenderedSection;
  depth: number;
  current: string | null;
  onNavigate: (section: RenderedSection) => void;
}) {
  const children = section.children || [];
  const hasChildren = children.length > 0;
  const [open, setOpen] = useState(depth === 0);
  // A collapsed parent stands in for its current descendant.
  const isCurrent = section.id === current || (!open && containsSection(section, current));

  return (
    <li
      className={`ontology-outline__node ontology-outline__node--${String(section.level).toLowerCase()}`}
    >
      <div className="ontology-outline__row">
        {hasChildren ? (
          <button
            type="button"
            className={`ontology-outline__toggle ${open ? "is-open" : ""}`}
            onClick={() => setOpen((value) => !value)}
            aria-label={`${open ? "Collapse" : "Expand"} ${section.title}`}
            aria-expanded={open}
          >
            <span aria-hidden="true">▸</span>
          </button>
        ) : (
          <span className="ontology-outline__toggle-placeholder" aria-hidden="true" />
        )}
        <button
          type="button"
          className="ontology-outline__title"
          aria-current={isCurrent ? "location" : undefined}
          onClick={() => onNavigate(section)}
          title={section.title}
        >
          {section.title}
        </button>
      </div>
      {hasChildren && open && (
        <ul className="ontology-outline__children">
          {children.map((child, index) => (
            <OutlineNode
              key={child.id || `${section.id}-${index}`}
              section={child}
              depth={depth + 1}
              current={current}
              onNavigate={onNavigate}
            />
          ))}
        </ul>
      )}
    </li>
  );
}

function OutlinePanel({ context }: { context: NoteTabContext }) {
  const sections = context.outline.sections;
  const [current, navigate] = useCurrentSection(context.outline);

  return (
    <section className="notes-right-rail__outline" aria-label="Note outline">
      {sections.length === 0 ? (
        <p className="ontology-muted">No headings in this note.</p>
      ) : (
        <ul className="ontology-outline">
          {sections.map((section, index) => (
            <OutlineNode
              key={section.id || `root-${index}`}
              section={section}
              depth={0}
              current={current}
              onNavigate={navigate}
            />
          ))}
        </ul>
      )}
    </section>
  );
}

function GraphPanel({
  workspace,
  graph,
  graphLoading,
  graphError,
  onRetry,
  onOpen,
  onOpenNode,
}: {
  workspace: NodeWorkspace;
  graph: NodeWorkspaceGraph | null | undefined;
  graphLoading: boolean;
  graphError: boolean;
  onRetry: () => void;
  onOpen: (path: string, target?: OpenTarget) => void;
  onOpenNode: (node: StructuralNode) => void;
  validationIssueCount?: number;
  validationHealth?: ValidationHealth;
}) {
  const localWorkspace = useMemo(() => graphWorkspace(workspace, graph), [graph, workspace]);

  if (localWorkspace) {
    return (
      <div className="notes-right-rail__graph">
        <OntologyLocalGraph workspace={localWorkspace} onOpen={onOpen} onOpenNode={onOpenNode} />
      </div>
    );
  }

  return (
    <section className="notes-right-rail__graph notes-right-rail__graph-status" aria-label="Graph">
      {graphLoading ? (
        "Loading local graph…"
      ) : graphError ? (
        <>
          <span>Unable to load local graph.</span>
          <button type="button" onClick={onRetry}>
            Retry
          </button>
        </>
      ) : (
        "No local graph yet."
      )}
    </section>
  );
}

export type NotesRightRailProps = {
  active?: boolean;
  context: NoteTabContext | null;
  editSession: OntologyEditSessionResponse | null;
  collapsed: boolean;
  onToggleCollapsed: () => void;
  onOpen: (path: string, target?: OpenTarget) => void;
  onOpenNode: (node: StructuralNode) => void;
  validationIssueCount?: number;
  validationHealth?: ValidationHealth;
  /** A collection view has a selected record to show; the rail renders it into its slot. */
  record?: boolean;
  onRecordSlot?: (element: HTMLElement | null) => void;
};

export function NotesRightRail({
  active = true,
  context,
  editSession,
  collapsed,
  onToggleCollapsed,
  onOpen,
  onOpenNode,
  validationIssueCount,
  validationHealth,
  record = false,
  onRecordSlot,
}: NotesRightRailProps) {
  const [activeTab, setActiveTab] = useState<RailTab>(readTab);

  const graphQuery = useFocusedWorkspaceGraphQuery(
    context?.workspace.node.ref ?? null,
    editSession,
    context?.workspace.version ?? "",
    active && activeTab === "graph" && !collapsed,
  );

  // A note tab's context wins; otherwise the active collection view's record.
  const showRecord = record && !context;
  const isCollapsed = collapsed || (!context && !showRecord);

  useEffect(() => {
    writeSessionValue(RAIL_TAB_KEY, activeTab);
  }, [activeTab]);

  const selectTab = (tab: RailTab) => {
    if (!context) return;
    setActiveTab(tab);
  };

  const handleTabKeyDown = (event: KeyboardEvent<HTMLButtonElement>) => {
    const index = RAIL_TABS.findIndex((tab) => tab.id === activeTab);
    let nextIndex: number | null = null;

    if (event.key === "ArrowRight" || event.key === "ArrowDown") {
      nextIndex = (index + 1) % RAIL_TABS.length;
    } else if (event.key === "ArrowLeft" || event.key === "ArrowUp") {
      nextIndex = (index - 1 + RAIL_TABS.length) % RAIL_TABS.length;
    } else if (event.key === "Home") {
      nextIndex = 0;
    } else if (event.key === "End") {
      nextIndex = RAIL_TABS.length - 1;
    }

    if (nextIndex === null) return;
    event.preventDefault();
    const next = RAIL_TABS[nextIndex];
    setActiveTab(next.id);
    document.getElementById(`notes-right-rail-tab-${next.id}`)?.focus();
  };

  return (
    <aside
      className={`notes-right-rail${isCollapsed ? " is-collapsed" : ""}${context || showRecord ? "" : " is-disabled"}`}
      aria-label="Note context"
    >
      {!isCollapsed && showRecord && (
        <div className="notes-right-rail__record">
          <div className="notes-right-rail__record-bar">
            <span>Record</span>
            <button
              type="button"
              className="notes-right-rail__toggle"
              aria-label="Collapse note context"
              aria-expanded="true"
              onClick={onToggleCollapsed}
            >
              <span aria-hidden="true">›</span>
            </button>
          </div>
          <div className="notes-right-rail__content" ref={onRecordSlot} />
        </div>
      )}
      {!isCollapsed && context && (
        <div
          className="notes-right-rail__tabs"
          role="tablist"
          aria-label="Note context"
          aria-orientation="horizontal"
        >
          {RAIL_TABS.map((tab) => (
            <button
              key={tab.id}
              id={`notes-right-rail-tab-${tab.id}`}
              type="button"
              role="tab"
              className="notes-right-rail__tab"
              aria-selected={activeTab === tab.id}
              aria-controls={`notes-right-rail-panel-${tab.id}`}
              tabIndex={activeTab === tab.id ? 0 : -1}
              onClick={() => selectTab(tab.id)}
              onKeyDown={handleTabKeyDown}
            >
              {tab.label}
            </button>
          ))}
        </div>
      )}
      {!isCollapsed && context && (
        <div className="notes-right-rail__content">
          <div
            id="notes-right-rail-panel-info"
            role="tabpanel"
            aria-labelledby="notes-right-rail-tab-info"
            hidden={activeTab !== "info"}
          >
            {activeTab === "info" ? (
              <NotesSidebarInfo
                workspace={context.workspace}
                onOpen={onOpen}
                validationIssueCount={validationIssueCount}
                validationHealth={validationHealth}
              />
            ) : null}
          </div>
          <div
            id="notes-right-rail-panel-outline"
            role="tabpanel"
            aria-labelledby="notes-right-rail-tab-outline"
            hidden={activeTab !== "outline"}
          >
            {activeTab === "outline" ? <OutlinePanel context={context} /> : null}
          </div>
          <div
            id="notes-right-rail-panel-graph"
            role="tabpanel"
            aria-labelledby="notes-right-rail-tab-graph"
            hidden={activeTab !== "graph"}
          >
            {activeTab === "graph" ? (
              <GraphPanel
                workspace={context.workspace}
                graph={graphQuery.data}
                graphLoading={graphQuery.isPending}
                graphError={graphQuery.isError}
                onRetry={() => void graphQuery.refetch()}
                onOpen={onOpen}
                onOpenNode={onOpenNode}
              />
            ) : null}
          </div>
        </div>
      )}
      {isCollapsed ? (
        <div className="notes-right-rail__collapsed-strip">
          <button
            type="button"
            className="notes-right-rail__toggle"
            aria-label="Expand note context"
            aria-expanded="false"
            disabled={!context && !showRecord}
            onClick={onToggleCollapsed}
          >
            <span aria-hidden="true">‹</span>
          </button>
          <span aria-hidden="true">Context</span>
        </div>
      ) : null}
    </aside>
  );
}
