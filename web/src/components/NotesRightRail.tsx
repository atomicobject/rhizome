import {
  type KeyboardEvent,
  type ReactNode,
  lazy,
  Suspense,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";

import type {
  NodeWorkspace,
  NodeWorkspaceGraph,
  OntologyEditSessionResponse,
  RenderedSection,
  StructuralNode,
  ValidationHealth,
} from "../api/types";
import { tabKeyTarget } from "./focusManagement";
import type { NoteTabContext } from "./noteTabContext";
import { NotesSidebarInfo } from "./NotesSidebarInfo";
import { useFocusedWorkspaceGraphQuery } from "./useNotesQueries";

// Sigma, graphology, and ForceAtlas2 load with the Graph tab, not the Notes entry.
const OntologyLocalGraph = lazy(() =>
  import("./OntologyLocalGraph").then((module) => ({ default: module.OntologyLocalGraph })),
);

type RailTab = "info" | "outline" | "graph";

type OpenTarget = "current" | "stack" | "beside";

const RAIL_TAB_KEY = "rhizome:notes:right-rail:tab:v1";

const RAIL_TABS: Array<{ id: RailTab; label: string }> = [
  { id: "info", label: "Info" },
  { id: "outline", label: "Outline" },
  { id: "graph", label: "Graph" },
];

function isRailTab(value: string | null): value is RailTab {
  return RAIL_TABS.some((tab) => tab.id === value);
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
        <Suspense
          fallback={
            <p className="notes-right-rail__graph-status" role="status">
              Loading local graph…
            </p>
          }
        >
          <OntologyLocalGraph workspace={localWorkspace} onOpen={onOpen} onOpenNode={onOpenNode} />
        </Suspense>
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
  /** The pane is open: something to show, and the user has not closed it. */
  open: boolean;
  /** Below 1200px the pane covers the content instead of narrowing it. */
  overlay?: boolean;
  onClose: () => void;
  onOpen: (path: string, target?: OpenTarget) => void;
  onOpenNode: (node: StructuralNode) => void;
  validationIssueCount?: number;
  validationHealth?: ValidationHealth;
  /** A collection view has a selected record; the pane renders it into its slot. */
  record?: boolean;
  onRecordSlot?: (element: HTMLElement | null) => void;
  /** The drag handle on the pane's inner edge. */
  resizeHandle?: ReactNode;
};

function CloseIcon() {
  return (
    <svg
      viewBox="0 0 16 16"
      aria-hidden="true"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      strokeLinecap="round"
    >
      <path d="M4 4l8 8M12 4l-8 8" />
    </svg>
  );
}

/**
 * The Notes workspace's one right pane (SPEC-0126 US6): Info, Outline, and
 * Graph for the active note tab, or the record a collection view selected.
 * It renders nothing while closed; NotesShell opens it only when there is
 * something to show.
 */
export function NotesRightRail({
  active = true,
  context,
  editSession,
  open,
  overlay = false,
  onClose,
  onOpen,
  onOpenNode,
  validationIssueCount,
  validationHealth,
  record = false,
  onRecordSlot,
  resizeHandle,
}: NotesRightRailProps) {
  const [activeTab, setActiveTab] = useState<RailTab>(readTab);

  const graphQuery = useFocusedWorkspaceGraphQuery(
    context?.workspace.node.ref ?? null,
    editSession,
    context?.workspace.version ?? "",
    active && activeTab === "graph" && open && Boolean(context),
  );

  // A note tab's context wins; otherwise the active collection view's record.
  const showRecord = record && !context;

  useEffect(() => {
    writeSessionValue(RAIL_TAB_KEY, activeTab);
  }, [activeTab]);

  if (!open) return null;

  const handleTabKeyDown = (event: KeyboardEvent<HTMLButtonElement>) => {
    const index = RAIL_TABS.findIndex((tab) => tab.id === activeTab);
    const nextIndex = tabKeyTarget(event.key, index, RAIL_TABS.length);

    if (nextIndex === null) return;
    event.preventDefault();
    const next = RAIL_TABS[nextIndex];
    setActiveTab(next.id);
    document.getElementById(`notes-right-rail-tab-${next.id}`)?.focus();
  };

  const close = (
    <button
      type="button"
      className="notes-right-rail__close"
      aria-label="Hide right pane"
      aria-keyshortcuts="]"
      title="Hide right pane (])"
      onClick={onClose}
    >
      <CloseIcon />
    </button>
  );

  return (
    <aside
      id="notes-right-pane"
      className={`notes-right-rail${overlay ? " is-overlay" : ""}`}
      aria-label={context ? "Note context" : "Record"}
      onKeyDown={(event) => {
        // An overlay covers the content, so Escape gives the content back.
        if (overlay && event.key === "Escape" && !event.defaultPrevented) {
          event.preventDefault();
          onClose();
        }
      }}
    >
      {resizeHandle}
      {showRecord ? (
        <>
          <div className="notes-right-rail__header">
            <h2 className="notes-right-rail__title">Record</h2>
            {close}
          </div>
          <div className="notes-right-rail__content" ref={onRecordSlot} />
        </>
      ) : context ? (
        <>
          <div className="notes-right-rail__header">
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
                  onClick={() => setActiveTab(tab.id)}
                  onKeyDown={handleTabKeyDown}
                >
                  {tab.label}
                </button>
              ))}
            </div>
            {close}
          </div>
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
        </>
      ) : (
        // A note tab whose context is still loading keeps the pane's place.
        <div className="notes-right-rail__header">
          <span className="notes-right-rail__title" />
          {close}
        </div>
      )}
    </aside>
  );
}
