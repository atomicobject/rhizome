import { publicTypeName } from "../lib/typeNames";
import { useMemo, useState } from "react";
import type { GraphEdge, GraphNode } from "../api/types";
import { type GraphScope, kindLabel, truncatePath } from "./graphViewShared";
import { type GraphDisplay, type GraphNodeClickOptions, useSigmaGraph } from "./useSigmaGraph";

const UNTYPED_NODE_TYPE = "__untyped__";

const EMPTY_PROBLEM_COUNTS: ReadonlyMap<string, number> = new Map();

type GraphTypeEntry = {
  name: string;
  label: string;
  count: number;
  color: string;
  selected: boolean;
};

function graphTypeName(node: GraphNode): string {
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
  typeColors,
  typeLabels,
  zoomKey,
  problemCounts = EMPTY_PROBLEM_COUNTS,
  display,
  onDisplayChange,
}: {
  nodes: GraphNode[];
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
  typeColors?: Map<string, string>;
  /** Display labels for type names in the legend; raw names are the fallback. */
  typeLabels?: ReadonlyMap<string, string>;
  zoomKey: number;
  problemCounts?: ReadonlyMap<string, number>;
  /** Remembered display toggles; without them the toggles last as long as the graph. */
  display?: GraphDisplay;
  onDisplayChange?: (patch: Partial<GraphDisplay>) => void;
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
    nodes.forEach((node) => {
      const name = graphTypeName(node);
      counts.set(name, (counts.get(name) || 0) + 1);
    });

    return Array.from(counts.entries())
      .map(([name, count]) => ({
        name,
        label: name === UNTYPED_NODE_TYPE ? "Untyped" : typeLabels?.get(name) || name,
        count,
        color: typeColors?.get(name) || "var(--text-faint)",
        selected: Boolean(highlightTypes?.size && highlightTypes.has(name)),
      }))
      .sort((left, right) => {
        if (left.name === UNTYPED_NODE_TYPE) return 1;

        if (right.name === UNTYPED_NODE_TYPE) return -1;

        return left.label.localeCompare(right.label);
      });
  }, [highlightTypes, nodes, typeColors, typeLabels]);

  const highlightedNodeCount = useMemo(() => {
    if (!highlightTypes?.size) return nodes.length;

    return nodes.filter((node) => {
      const typeName = publicTypeName(node.resolvedType);

      return Boolean(typeName && highlightTypes.has(typeName));
    }).length;
  }, [highlightTypes, nodes]);

  const nodeCaption = highlightTypes?.size
    ? `${highlightedNodeCount} of ${nodes.length} nodes highlighted`
    : `${nodes.length} nodes`;

  const problemNodeCount = [...problemCounts.values()].filter((count) => count > 0).length;

  return (
    <div className="graph-container">
      <div ref={containerRef} className="graph-surface" />

      <div className="graph-zoom">
        <button
          type="button"
          className="zoom-btn"
          onClick={handleZoomIn}
          title="Zoom in"
          aria-label="Zoom in"
        >
          +
        </button>
        <button
          type="button"
          className="zoom-btn"
          onClick={handleZoomOut}
          title="Zoom out"
          aria-label="Zoom out"
        >
          -
        </button>
        <button
          type="button"
          className="zoom-btn zoom-btn--fit"
          onClick={handleFitToView}
          title="Fit to view"
          aria-label="Fit to view"
        >
          ⊙
        </button>
      </div>

      <div className="graph-tools">
        <div className="graph-tool">
          <div className="graph-tool-header">
            <div className="graph-tool-title">Find</div>
            <button
              type="button"
              className="graph-expand"
              onClick={() => setGraphExpanded((prev) => !prev)}
            >
              {graphExpanded ? "Less" : "More"}
            </button>
          </div>
          <input
            className="graph-search"
            value={graphQuery}
            onChange={(e) => setGraphQuery(e.target.value)}
            placeholder="Search nodes"
          />
          {graphQueryValue && (
            <div className="graph-search-meta" role="status">
              {graphMatches.size} {graphMatches.size === 1 ? "match" : "matches"}
            </div>
          )}
          {graphExpanded && (
            <label className="graph-toggle">
              <input
                type="checkbox"
                checked={graphFilter}
                disabled={!graphQueryValue}
                onChange={(e) => setGraphFilter(e.target.checked)}
              />
              Filter
            </label>
          )}
          {graphExpanded && (
            <div className="graph-scope">
              {scope.mode === "global" && "Global graph"}
              {scope.mode === "module" && `Folder: ${scope.label}`}
              {scope.mode === "local" && `File: ${scope.label}`}
            </div>
          )}
          {graphExpanded && scope.mode !== "global" && (
            <button type="button" className="graph-reset" onClick={onResetScope}>
              Back to global
            </button>
          )}
        </div>

        {graphExpanded && (
          <div className="graph-tool">
            <div className="graph-tool-title">Clusters</div>
            {clusters.length > 0 ? (
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
                    <span className="cluster-count">{cluster.size}</span>
                  </button>
                ))}
                {clusters.length > 6 && (
                  <div className="cluster-more">+{clusters.length - 6} more</div>
                )}
              </div>
            ) : (
              <div className="cluster-empty">No clusters yet</div>
            )}
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

        {graphExpanded && (
          <div className="graph-tool">
            <div className="graph-tool-title">Display</div>
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
          </div>
        )}
      </div>

      {showTypeLegend && (
        <aside className="graph-type-legend" aria-label="Graph type legend">
          <div className="graph-type-legend__header">
            <span className="graph-type-legend__title">Types</span>
            <span className="graph-type-legend__caption">{nodeCaption}</span>
          </div>
          <ul className="graph-type-legend__list">
            {typeEntries.map((entry) => (
              <li
                key={entry.name}
                className={`graph-type-legend__item ${entry.selected ? "is-highlighted" : ""}`}
                data-type={entry.name}
              >
                <span
                  className="graph-type-legend__swatch"
                  style={{ backgroundColor: entry.color }}
                  aria-hidden="true"
                />
                <span className="graph-type-legend__label">{entry.label}</span>
                <span className="graph-type-legend__count">{entry.count}</span>
              </li>
            ))}
          </ul>
        </aside>
      )}

      {truncated && <div className="graph-banner">Graph truncated for performance.</div>}
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
  );
}
