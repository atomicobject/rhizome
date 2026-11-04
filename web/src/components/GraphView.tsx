import { ChevronDown, Maximize, Minus, Plus, TriangleAlert } from "lucide-react";
import { createPortal } from "react-dom";
import { useMemo, useState } from "react";
import { humanizeName } from "../lib/labels";
import { publicTypeName } from "../lib/typeNames";
import type { GraphEdge, GraphNode } from "../api/types";
import {
  CODE_COLOR,
  FOLDER_COLOR,
  type GraphScope,
  type GraphViewNode,
  kindLabel,
  truncatePath,
  UNTYPED_COLOR,
} from "./graphViewShared";
import { type GraphDisplay, type GraphNodeClickOptions, useSigmaGraph } from "./useSigmaGraph";

const UNTYPED_NODE_TYPE = "__untyped__";

/** Legend keys for nodes that are not notes; their colors match `nodeColor` under a type-color map. */
const CODE_NODE_TYPE = "__code__";

const FOLDER_NODE_TYPE = "__module__";

const KIND_ENTRIES = new Map([
  [UNTYPED_NODE_TYPE, { label: "Untyped notes", color: UNTYPED_COLOR }],
  [CODE_NODE_TYPE, { label: "Code files", color: CODE_COLOR }],
  [FOLDER_NODE_TYPE, { label: "Folders", color: FOLDER_COLOR }],
]);

/** Legend groups, in order: what the colors of notes mean, then code, then folders. */
const LEGEND_GROUPS = [
  { key: "typed", title: "Notes by type" },
  { key: "code", title: "Code" },
  { key: "folder", title: "Folders" },
] as const;

function legendGroup(name: string) {
  if (name === CODE_NODE_TYPE) return "code";

  if (name === FOLDER_NODE_TYPE) return "folder";

  return "typed";
}

const EMPTY_PROBLEM_COUNTS: ReadonlyMap<string, number> = new Map();

type GraphTypeEntry = {
  name: string;
  label: string;
  count: number;
  color: string;
  selected: boolean;
};

function graphTypeName(node: GraphNode): string {
  if (node.kind === "code") return CODE_NODE_TYPE;

  if (node.kind === "module") return FOLDER_NODE_TYPE;

  return publicTypeName(node.resolvedType) || UNTYPED_NODE_TYPE;
}

export function GraphView({
  nodes,
  edges,
  truncated,
  needsIndex,
  scope,
  onResetScope,
  onNodeClick,
  searchActive,
  searchPaths,
  searchModules,
  searchOnlyMatches,
  highlightTypes,
  showTypeLegend = false,
  showFind = true,
  typeColors,
  typeLabels,
  zoomKey,
  problemCounts = EMPTY_PROBLEM_COUNTS,
  display,
  onDisplayChange,
  displaySlot,
  legendNodes,
  folderCaption,
  edgeCaption,
}: {
  /** Nodes to draw; a summarized view may give each its own `fill`. */
  nodes: GraphViewNode[];
  edges: GraphEdge[];
  truncated?: boolean;
  needsIndex?: boolean;
  scope: GraphScope;
  onResetScope: () => void;
  onNodeClick: (node: GraphNode, options: GraphNodeClickOptions) => void;
  searchActive: boolean;
  searchPaths: Set<string>;
  searchModules: Set<string>;
  searchOnlyMatches: boolean;
  highlightTypes?: ReadonlySet<string>;
  showTypeLegend?: boolean;
  /** The Find box; a page with its own search turns it off so there is one search control. */
  showFind?: boolean;
  typeColors?: Map<string, string>;
  /** Display labels for type names in the legend; raw names are the fallback. */
  typeLabels?: ReadonlyMap<string, string>;
  zoomKey: number;
  problemCounts?: ReadonlyMap<string, number>;
  /** Remembered display toggles; without them the toggles last as long as the graph. */
  display?: GraphDisplay;
  onDisplayChange?: (patch: Partial<GraphDisplay>) => void;
  /** Where the Display menu renders, such as the page header; without one it floats on the graph. */
  displaySlot?: HTMLElement | null;
  /** The nodes the legend counts when the drawn nodes summarize them (Explorer's folder view). */
  legendNodes?: GraphNode[];
  /** A note on what folder colors mean, when folders summarize their contents. */
  folderCaption?: string;
  /** What a drawn line means, keyed beside the folders it joins. */
  edgeCaption?: string;
}) {
  const [localShowProblems, setLocalShowProblems] = useState(false);
  const showProblems = display?.showProblems ?? localShowProblems;

  const {
    activeCommunity,
    clusters,
    containerRef,
    graphExpanded,
    graphFilter,
    graphMatches,
    graphQuery,
    graphQueryValue,
    graphReady,
    handleFitToView,
    handleZoomIn,
    handleZoomOut,
    hover,
    scaleByAuthority,
    setActiveCommunity,
    setGraphExpanded,
    setGraphFilter,
    setGraphQuery,
    setScaleByAuthority: setLocalScaleByAuthority,
    setShowEdgeLabels: setLocalShowEdgeLabels,
    showEdgeLabels,
  } = useSigmaGraph({
    nodes,
    edges,
    needsIndex,
    onNodeClick,
    scope,
    searchActive,
    searchModules,
    searchOnlyMatches,
    searchPaths,
    highlightTypes,
    typeColors,
    zoomKey,
    problemCounts,
    showProblems,
    display,
  });

  const remembered = display && onDisplayChange;

  const setScaleByAuthority = (scaleByAuthority: boolean) =>
    remembered ? onDisplayChange({ scaleByAuthority }) : setLocalScaleByAuthority(scaleByAuthority);

  const setShowEdgeLabels = (showEdgeLabels: boolean) =>
    remembered ? onDisplayChange({ showEdgeLabels }) : setLocalShowEdgeLabels(showEdgeLabels);

  const setShowProblems = (showProblems: boolean) =>
    remembered ? onDisplayChange({ showProblems }) : setLocalShowProblems(showProblems);

  const showEmpty = !needsIndex && nodes.length === 0;

  const typeEntries = useMemo<GraphTypeEntry[]>(() => {
    const counts = new Map<string, number>();

    const count = (node: GraphNode) => {
      const name = graphTypeName(node);
      counts.set(name, (counts.get(name) || 0) + 1);
    };

    (legendNodes ?? nodes).forEach((node) => node.kind !== "module" && count(node));
    nodes.forEach((node) => node.kind === "module" && count(node));

    return (
      Array.from(counts.entries())
        .map(([name, count]) => {
          const kind = KIND_ENTRIES.get(name);
          const color = kind?.color ?? (typeColors?.get(name) || "var(--text-faint)");

          return {
            name,
            label: kind?.label ?? (typeLabels?.get(name) || humanizeName(name)),
            count,
            color,
            selected: Boolean(highlightTypes?.size && highlightTypes.has(name)),
          };
        })
        // Types alphabetically, then untyped notes, code, and folders.
        .sort((left, right) => {
          const rank = (entry: GraphTypeEntry) =>
            [UNTYPED_NODE_TYPE, CODE_NODE_TYPE, FOLDER_NODE_TYPE].indexOf(entry.name);

          return rank(left) - rank(right) || left.label.localeCompare(right.label);
        })
    );
  }, [highlightTypes, legendNodes, nodes, typeColors, typeLabels]);

  const highlightedNodeCount = useMemo(() => {
    if (!highlightTypes?.size) return nodes.length;

    return nodes.filter((node) => {
      const typeName = publicTypeName(node.resolvedType);

      return Boolean(typeName && highlightTypes.has(typeName));
    }).length;
  }, [highlightTypes, nodes]);

  const shownCount = (legendNodes ?? nodes).length;

  const nodeCaption = highlightTypes?.size
    ? `${highlightedNodeCount.toLocaleString("en-US")} of ${shownCount.toLocaleString("en-US")} nodes highlighted`
    : `${shownCount.toLocaleString("en-US")} nodes`;

  const problemNodeCount = [...problemCounts.values()].filter((count) => count > 0).length;

  const displayMenu = (
    <div className="graph-display">
      <button
        type="button"
        className="graph-display__button"
        aria-expanded={graphExpanded}
        onClick={() => setGraphExpanded((prev) => !prev)}
      >
        Display
        <ChevronDown size={14} strokeWidth={1.75} aria-hidden="true" />
      </button>
      {graphExpanded && (
        <div className="graph-display__panel">
          <div className="graph-display__section">
            <label className="graph-toggle">
              <input
                type="checkbox"
                checked={scaleByAuthority}
                onChange={(e) => setScaleByAuthority(e.target.checked)}
              />
              Size by authority
            </label>
            <label className="graph-toggle">
              <input
                type="checkbox"
                checked={showEdgeLabels}
                onChange={(e) => setShowEdgeLabels(e.target.checked)}
              />
              Edge labels
            </label>
            {problemNodeCount > 0 && (
              <label className="graph-toggle">
                <input
                  type="checkbox"
                  checked={showProblems}
                  onChange={(event) => setShowProblems(event.target.checked)}
                />
                Problems ({problemNodeCount})
              </label>
            )}
            {showFind && (
              <label className="graph-toggle">
                <input
                  type="checkbox"
                  checked={graphFilter}
                  disabled={!graphQueryValue}
                  onChange={(e) => setGraphFilter(e.target.checked)}
                />
                Only Find matches
              </label>
            )}
          </div>
          {clusters.length > 0 && (
            <div className="graph-display__section">
              <div className="graph-tool-title">Clusters</div>
              <div className="graph-clusters">
                {clusters.slice(0, 6).map((cluster) => (
                  <button
                    key={cluster.id}
                    type="button"
                    className={`cluster-item ${activeCommunity === cluster.id ? "cluster-item--active" : ""}`}
                    onClick={() =>
                      setActiveCommunity((prev) => (prev === cluster.id ? null : cluster.id))
                    }
                  >
                    <span className="cluster-swatch" style={{ background: cluster.color }} />
                    <span className="cluster-label">{cluster.label}</span>
                    <span className="cluster-count num">{cluster.size}</span>
                  </button>
                ))}
                {clusters.length > 6 && (
                  <div className="cluster-more">+{clusters.length - 6} more</div>
                )}
              </div>
              {activeCommunity && (
                <button
                  type="button"
                  className="cluster-clear"
                  onClick={() => setActiveCommunity(null)}
                >
                  Clear focus
                </button>
              )}
            </div>
          )}
          {scope.mode !== "global" && (
            <div className="graph-display__section">
              <button type="button" className="graph-reset" onClick={onResetScope}>
                Back to the whole vault
              </button>
            </div>
          )}
        </div>
      )}
    </div>
  );

  return (
    <div
      className="graph-container"
      data-graph-ready={graphReady ? "true" : undefined}
      data-truncated={truncated ? "true" : undefined}
    >
      {truncated && (
        <div className="graph-banner" role="status">
          <TriangleAlert size={16} strokeWidth={1.75} aria-hidden="true" />
          <span>
            Part of the graph is hidden for performance. Pick a folder or file to see all of its
            links.
          </span>
        </div>
      )}
      <div className="graph-stage">
        <div
          ref={containerRef}
          className="graph-surface"
          role="img"
          aria-label={`Graph of ${countLabel(nodes.length, "node")} and ${countLabel(edges.length, "link")}. Use Find to search nodes; the navigation lists the same notes.`}
        />

        <div className="graph-zoom">
          <button
            type="button"
            className="zoom-btn"
            onClick={handleZoomIn}
            title="Zoom in"
            aria-label="Zoom in"
          >
            <Plus size={16} strokeWidth={1.75} aria-hidden="true" />
          </button>
          <button
            type="button"
            className="zoom-btn"
            onClick={handleZoomOut}
            title="Zoom out"
            aria-label="Zoom out"
          >
            <Minus size={16} strokeWidth={1.75} aria-hidden="true" />
          </button>
          <button
            type="button"
            className="zoom-btn zoom-btn--fit"
            onClick={handleFitToView}
            title="Fit to view"
            aria-label="Fit to view"
          >
            <Maximize size={14} strokeWidth={1.75} aria-hidden="true" />
          </button>
        </div>

        <div className="graph-tools">
          {showFind && (
            <div className="graph-find">
              <input
                className="graph-search"
                aria-label="Search graph nodes"
                value={graphQuery}
                onChange={(e) => setGraphQuery(e.target.value)}
                placeholder="Find in graph"
              />
              {graphQueryValue && (
                <span className="graph-search-meta" role="status">
                  {graphMatches.size} {graphMatches.size === 1 ? "match" : "matches"}
                </span>
              )}
            </div>
          )}
          {displaySlot === undefined && displayMenu}
        </div>

        {!graphReady && (
          <div className="graph-notice" role="status">
            <div className="graph-notice-title">Laying out graph</div>
            <div className="graph-notice-body">Arranging {countLabel(nodes.length, "node")}…</div>
          </div>
        )}
        {needsIndex && (
          <div className="graph-notice">
            <div className="graph-notice-title">Graph unavailable</div>
            <div className="graph-notice-body">
              Run <code>rzm index</code> to build the graph.
            </div>
          </div>
        )}
        {showEmpty && (
          <div className="graph-notice">
            <div className="graph-notice-title">No graph data yet</div>
            <div className="graph-notice-body">
              Pick a file or folder, or rebuild the index if you expected connections here.
            </div>
          </div>
        )}

        {hover && (
          <div className="graph-hover">
            <div className="graph-hover-label">{hover.label}</div>
            <div className="graph-hover-meta">
              <span className="graph-hover-kind">
                {publicTypeName(hover.resolvedType) || kindLabel(hover.kind)}
              </span>
              {hover.nodeId && (
                <span className="graph-hover-id" title={hover.nodeId}>
                  {hover.nodeId}
                </span>
              )}
              {hover.connectionCount > 0 && (
                <span className="graph-hover-connections">
                  {hover.connectionCount} connection
                  {hover.connectionCount !== 1 ? "s" : ""}
                </span>
              )}
              {(hover.problemCount ?? 0) > 0 && (
                <span className="graph-hover-problems">
                  {hover.problemCount} {hover.problemCount === 1 ? "problem" : "problems"}
                </span>
              )}
            </div>
            {hover.path && <div className="graph-hover-path">{truncatePath(hover.path)}</div>}
          </div>
        )}
      </div>
      {showTypeLegend && (
        <aside className="graph-type-legend" aria-label="Graph type legend">
          <div className="graph-type-legend__caption">{nodeCaption}</div>
          {LEGEND_GROUPS.map((group) => {
            const entries = typeEntries.filter((entry) => legendGroup(entry.name) === group.key);

            if (entries.length === 0) return null;

            const item = (entry: GraphTypeEntry) => (
              <li
                key={entry.name}
                className={`graph-type-legend__item ${entry.selected ? "is-highlighted" : ""}`}
                data-type={entry.name}
              >
                <span
                  className="graph-type-legend__swatch"
                  style={group.key === "folder" ? undefined : { backgroundColor: entry.color }}
                  aria-hidden="true"
                />
                <span className="graph-type-legend__label">{entry.label}</span>
                <span className="graph-type-legend__count num">
                  {entry.count.toLocaleString("en-US")}
                </span>
              </li>
            );

            return (
              <section key={group.key} className="graph-type-legend__group" data-group={group.key}>
                <h3 className="graph-type-legend__title">{group.title}</h3>
                <ul className="graph-type-legend__list">
                  {entries.map(item)}
                  {group.key === "folder" && folderCaption && (
                    <li className="graph-type-legend__note">{folderCaption}</li>
                  )}
                  {group.key === "folder" && edgeCaption && (
                    <li className="graph-type-legend__item graph-type-legend__links">
                      <span className="graph-type-legend__line" aria-hidden="true" />
                      <span className="graph-type-legend__label">{edgeCaption}</span>
                    </li>
                  )}
                </ul>
              </section>
            );
          })}
        </aside>
      )}
      {displaySlot && createPortal(displayMenu, displaySlot)}
    </div>
  );
}

function countLabel(count: number, noun: string) {
  return `${count} ${count === 1 ? noun : `${noun}s`}`;
}
