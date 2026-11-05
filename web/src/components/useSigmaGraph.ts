import { graphClusters } from "./graphClusters";
import {
  mountSigmaRenderer,
  readThemeColors,
  rendererColorSettings,
  type ThemeColors,
} from "./sigmaRenderer";
import type Graph from "graphology";
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import type Sigma from "sigma";
import type { GraphEdge, GraphNode } from "../api/types";
import { type ReducerState, type SigmaEdgeAttrs, type SigmaNodeAttrs } from "../lib/graphStyle";
import { resolveCssColor, subscribeColorScheme } from "../lib/themeColors";
import {
  CAMERA,
  communityColor,
  type GraphScope,
  type GraphViewNode,
  type HoverInfo,
} from "./graphViewShared";
import { type LayoutPositions } from "./graphLayout";

import { buildSigmaGraph, layoutSigmaGraph } from "./sigmaGraphData";
import { fitFixedGraph, zoomGraph } from "./sigmaCamera";

type SigmaRenderer = Sigma<SigmaNodeAttrs, SigmaEdgeAttrs>;

type SigmaGraph = Graph<SigmaNodeAttrs, SigmaEdgeAttrs>;

const EMPTY_HIGHLIGHT_TYPES: ReadonlySet<string> = new Set();

const EMPTY_PROBLEM_COUNTS: ReadonlyMap<string, number> = new Map();

type GraphHoverInfo = HoverInfo & {
  nodeId?: string;
  resolvedType?: string;
  problemCount?: number;
};

/** The graph's display toggles, which Overview remembers per view instance (SPEC-0114). */
export type GraphDisplay = {
  scaleByAuthority: boolean;
  showEdgeLabels: boolean;
  showProblems: boolean;
};

export type GraphNodeClickOptions = {
  beside: boolean;
};

export function useSigmaGraph({
  nodes,
  edges,
  needsIndex,
  scope,
  onNodeClick,
  searchActive,
  searchPaths,
  searchModules,
  searchOnlyMatches,
  highlightTypes = EMPTY_HIGHLIGHT_TYPES,
  typeColors,
  zoomKey,
  problemCounts = EMPTY_PROBLEM_COUNTS,
  showProblems = false,
  display,
  layoutExtent,
  explorer = false,
}: {
  layoutExtent?: number;
  explorer?: boolean;
  nodes: GraphViewNode[];
  edges: GraphEdge[];
  needsIndex?: boolean;
  scope: GraphScope;
  onNodeClick: (node: GraphNode, options: GraphNodeClickOptions) => void;
  searchActive: boolean;
  searchPaths: Set<string>;
  searchModules: Set<string>;
  searchOnlyMatches: boolean;
  highlightTypes?: ReadonlySet<string>;
  typeColors?: Map<string, string>;
  zoomKey: number;
  problemCounts?: ReadonlyMap<string, number>;
  showProblems?: boolean;
  /** Display toggles a caller remembers; without them the toggles last as long as the graph. */
  display?: Pick<GraphDisplay, "scaleByAuthority" | "showEdgeLabels">;
}) {
  const fixed =
    nodes.length > 0 && nodes.every((n) => Number.isFinite(n.x) && Number.isFinite(n.y));

  const fixedRef = useRef(fixed);
  fixedRef.current = fixed;
  const didFit = useRef(false);
  const containerRef = useRef<HTMLDivElement | null>(null);
  const sigmaRef = useRef<SigmaRenderer | null>(null);
  const graphRef = useRef<SigmaGraph | null>(null);
  const onNodeClickRef = useRef(onNodeClick);
  const problemCountsRef = useRef(problemCounts);
  problemCountsRef.current = problemCounts;
  const globalLayoutRef = useRef<Map<string, { x: number; y: number }>>(new Map());

  const colorsRef = useRef<ThemeColors | null>(null);
  colorsRef.current ??= readThemeColors();

  const stateRef = useRef<ReducerState>({
    graphMatches: new Set(),
    semanticMatches: new Set(),
    highlightTypes: EMPTY_HIGHLIGHT_TYPES,
    graphFilter: false,
    searchActive: false,
    searchOnlyMatches: false,
    activeCommunity: null,
    scaleByAuthority: true,
    showEdgeLabels: true,
    hoverCommunity: null,
    hoverNodeID: null,
    cameraRatio: 1,
    maxScore: 0,
    showProblems: false,
    colors: colorsRef.current,
  });

  const refreshRef = useRef<number | null>(null);

  const [hover, setHover] = useState<GraphHoverInfo | null>(null);
  // The data the renderer last drew; the graph is ready when it matches the current data.
  const [drawn, setDrawn] = useState<readonly GraphNode[] | null>(null);
  const [graphQuery, setGraphQuery] = useState("");
  const [graphFilter, setGraphFilter] = useState(false);
  const [graphExpanded, setGraphExpanded] = useState(false);
  const [localScaleByAuthority, setScaleByAuthority] = useState(true);
  const [activeCommunity, setActiveCommunity] = useState<string | null>(null);
  const [localShowEdgeLabels, setShowEdgeLabels] = useState(true);
  const scaleByAuthority = display?.scaleByAuthority ?? localScaleByAuthority;
  const showEdgeLabels = display?.showEdgeLabels ?? localShowEdgeLabels;

  useLayoutEffect(() => {
    onNodeClickRef.current = onNodeClick;
  }, [onNodeClick]);

  const graphQueryValue = graphQuery.trim().toLowerCase();

  const graphMatches = useMemo(() => {
    if (!graphQueryValue) return new Set<string>();
    const matches = new Set<string>();
    nodes.forEach((node) => {
      const label = node.label.toLowerCase();
      const path = (node.path || "").toLowerCase();

      if (label.includes(graphQueryValue) || path.includes(graphQueryValue)) {
        matches.add(node.id);
      }
    });

    return matches;
  }, [graphQueryValue, nodes]);

  const semanticMatches = useMemo(() => {
    if (!searchActive) return new Set<string>();
    const matches = new Set<string>();
    nodes.forEach((node) => {
      const path = node.path || "";

      const matched =
        (node.kind !== "module" && path && searchPaths.has(path)) ||
        (node.kind === "module" &&
          (searchModules.has(path) || searchModules.has(node.label || "")));

      if (matched) matches.add(node.id);
    });

    return matches;
  }, [nodes, searchActive, searchModules, searchPaths]);

  const communityColors = useMemo(() => {
    const colors = new Map<string, string>();
    nodes.forEach((node) => {
      if (node.community && !colors.has(node.community)) {
        colors.set(node.community, communityColor(node.community));
      }
    });

    return colors;
  }, [nodes]);

  const clusters = useMemo(() => graphClusters(nodes, communityColors), [nodes, communityColors]);

  const maxScore = useMemo(() => {
    let max = 0;
    nodes.forEach((node) => {
      const value = node.score ?? node.authority ?? 0;
      max = Math.max(max, value);
    });

    return max;
  }, [nodes]);

  const connectionCounts = useMemo(() => {
    const counts = new Map<string, number>();
    edges.forEach((e) => {
      counts.set(e.source, (counts.get(e.source) || 0) + 1);
      counts.set(e.target, (counts.get(e.target) || 0) + 1);
    });

    return counts;
  }, [edges]);

  const connectionCountsRef = useRef(connectionCounts);
  connectionCountsRef.current = connectionCounts;

  const scheduleRefresh = useCallback(() => {
    if (!sigmaRef.current || refreshRef.current !== null) return;
    refreshRef.current = window.requestAnimationFrame(() => {
      refreshRef.current = null;
      sigmaRef.current?.refresh();
    });
  }, []);

  useEffect(() => {
    stateRef.current = {
      ...stateRef.current,
      graphMatches,
      semanticMatches,
      highlightTypes,
      graphFilter,
      searchActive,
      searchOnlyMatches,
      activeCommunity,
      scaleByAuthority,
      showEdgeLabels,
      maxScore,
      showProblems,
    };

    // Reducers refresh highlights without rebuilding the graph or moving the camera.
    if (sigmaRef.current) {
      sigmaRef.current.setSetting("renderEdgeLabels", showEdgeLabels);
      scheduleRefresh();
    }
  }, [
    activeCommunity,
    graphFilter,
    graphMatches,
    highlightTypes,
    maxScore,
    scaleByAuthority,
    scheduleRefresh,
    searchActive,
    searchOnlyMatches,
    semanticMatches,
    showEdgeLabels,
    showProblems,
  ]);

  // Swap graphs on one renderer to preserve the camera and WebGL state.
  const empty = nodes.length === 0;

  useEffect(() => {
    const container = containerRef.current;

    if (!container || needsIndex || empty) return;

    const cleanup = mountSigmaRenderer({
      container,
      sigmaRef,
      graphRef,
      stateRef,
      colorsRef,
      onNodeClickRef,
      connectionCountsRef,
      setHover,
      scheduleRefresh,
    });

    const sigma = sigmaRef.current;
    const reducer = sigma?.getSetting("edgeReducer");

    if (sigma && reducer)
      sigma.setSetting("edgeReducer", (id, attrs) => {
        const result = reducer(id, attrs);

        if (!fixedRef.current || attrs.kind !== "folder-crosslink" || stateRef.current.hoverNodeID)
          return result;

        return { ...result, size: 0.6, color: stateRef.current.colors.edgeFaint };
      });

    return cleanup;
  }, [empty, needsIndex, scheduleRefresh]);

  // Keep the previous graph visible until this layout is ready.
  useEffect(() => {
    const sigma = sigmaRef.current;

    if (!sigma || needsIndex || empty) return;

    const { graph, hasCompleteGlobalLayout } = buildSigmaGraph({
      nodes,
      edges,
      scope,
      connectionCounts,
      globalLayout: globalLayoutRef.current,
      communityColors,
      typeColors,
      problemCounts: problemCountsRef.current,
      edgeFaint: stateRef.current.colors.edgeFaint,
      fixed,
      explorer,
    });

    const shouldRunLayout =
      !fixed && graph.order > 1 && (scope.mode !== "global" || !hasCompleteGlobalLayout);

    const install = (positions?: LayoutPositions) => {
      if (positions) {
        graph.forEachNode((id) => {
          const position = positions.get(id);

          if (position) graph.mergeNodeAttributes(id, position);
        });
      }

      if (scope.mode === "global") {
        const nextLayout = new Map<string, { x: number; y: number }>();
        graph.forEachNode((id, attrs) => {
          nextLayout.set(id, { x: attrs.x, y: attrs.y });
        });
        globalLayoutRef.current = nextLayout;
      }

      graph.forEachNode((id) => {
        graph.setNodeAttribute(id, "problemCount", problemCountsRef.current.get(id) ?? 0);
      });

      setHover(null);
      stateRef.current.hoverCommunity = null;
      stateRef.current.hoverNodeID = null;
      stateRef.current.hoverNeighbors = undefined;
      graphRef.current = graph;
      sigma.setCustomBBox(
        fixed && layoutExtent !== undefined
          ? { x: [-layoutExtent, layoutExtent], y: [-layoutExtent, layoutExtent] }
          : null,
      );
      sigma.setSettings({
        labelSize: 14,
        labelWeight: explorer || fixed ? "500" : "normal",
        edgeLabelSize: explorer || fixed ? 13 : 9,
      });
      sigma.setGraph(graph);

      if (fixed) {
        sigma.refresh();
        fitFixedGraph(sigma, !didFit.current);
        didFit.current = true;
      }

      sigma.once("afterRender", () => setDrawn(nodes));
      scheduleRefresh();
    };

    if (!shouldRunLayout) {
      install();

      return;
    }

    return layoutSigmaGraph(graph, nodes, scope, globalLayoutRef.current, install);
  }, [
    fixed,
    explorer,
    layoutExtent,
    communityColors,
    connectionCounts,
    edges,
    empty,
    needsIndex,
    nodes,
    scheduleRefresh,
    scope.mode,
    typeColors,
  ]);

  // Repaint theme colors without moving nodes or the camera.
  useEffect(
    () =>
      subscribeColorScheme(() => {
        const colors = readThemeColors();
        colorsRef.current = colors;
        stateRef.current = { ...stateRef.current, colors };
        const graph = graphRef.current;

        graph?.forEachNode((nodeID, attrs) => {
          if (attrs.colorValue) {
            graph.setNodeAttribute(nodeID, "baseColor", resolveCssColor(attrs.colorValue));
          }
        });
        sigmaRef.current?.setSettings(rendererColorSettings(colors));
        scheduleRefresh();
      }),
    [scheduleRefresh],
  );

  useEffect(() => {
    const graph = graphRef.current;

    if (!graph) return;
    graph.forEachNode((nodeID) => {
      graph.setNodeAttribute(nodeID, "problemCount", problemCounts.get(nodeID) ?? 0);
    });
    scheduleRefresh();
  }, [problemCounts, scheduleRefresh]);

  useEffect(() => {
    void zoomKey;

    const handle = window.setTimeout(() => {
      const sigma = sigmaRef.current;

      if (!sigma) return;
      sigma.resize();
      sigma.refresh();
      const camera = sigma.getCamera();
      const state = camera.getState();

      if (fixedRef.current) {
        fitFixedGraph(sigma, false);

        return;
      }

      camera.animate({ x: 0.5, y: 0.5, angle: 0, ratio: state.ratio }, { duration: 350 });
    }, 120);

    return () => window.clearTimeout(handle);
  }, [zoomKey]);

  useEffect(() => {
    if (!graphQueryValue) setGraphFilter(false);
  }, [graphQueryValue]);

  useEffect(() => {
    const scopeKey = `${scope.mode}:${scope.label ?? ""}`;
    void scopeKey;
    setHover(null);
    setGraphQuery("");
    setGraphFilter(false);
    setActiveCommunity(null);
    stateRef.current.hoverCommunity = null;
    stateRef.current.hoverNodeID = null;
    stateRef.current.hoverNeighbors = undefined;

    if (fixedRef.current) return;
    sigmaRef.current?.getCamera().setState({ x: 0.5, y: 0.5, ratio: 1, angle: 0 });
  }, [scope.label, scope.mode]);

  const handleZoomIn = useCallback(() => zoomGraph(sigmaRef.current, CAMERA.zoomStep), []);
  const handleZoomOut = useCallback(() => zoomGraph(sigmaRef.current, 1 / CAMERA.zoomStep), []);

  const handleFitToView = useCallback(() => {
    const sigma = sigmaRef.current;

    if (!sigma) return;

    if (fixedRef.current) {
      fitFixedGraph(sigma, false);

      return;
    }

    const camera = sigma.getCamera();
    camera.animate({ x: 0.5, y: 0.5, ratio: 1, angle: 0 }, { duration: 300 });
  }, []);

  return {
    activeCommunity,
    clusters,
    containerRef,
    graphExpanded,
    graphFilter,
    graphMatches,
    graphQuery,
    graphQueryValue,
    // Nothing to draw counts as settled, so a caller waiting on readiness never hangs.
    graphReady: empty || Boolean(needsIndex) || drawn === nodes,
    handleFitToView,
    handleZoomIn,
    handleZoomOut,
    hover,
    scaleByAuthority,
    setActiveCommunity,
    setGraphExpanded,
    setGraphFilter,
    setGraphQuery,
    setScaleByAuthority,
    setShowEdgeLabels,
    showEdgeLabels,
  };
}
