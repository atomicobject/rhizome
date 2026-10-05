import { createNodeImageProgram } from "@sigma/node-image";
import Graph from "graphology";
import forceAtlas2 from "graphology-layout-forceatlas2";
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import Sigma from "sigma";
import type { GraphEdge, GraphNode } from "../api/types";
import {
  computeEdgeStyle,
  computeNodeStyle,
  graphNodeIdentityAttrs,
  lodFromRatio,
  type ReducerState,
  type SigmaEdgeAttrs,
  type SigmaNodeAttrs,
} from "../lib/graphStyle";
import {
  CAMERA,
  communityColor,
  EDGE_COLORS,
  edgeKindLabel,
  type GraphScope,
  type GraphViewNode,
  type HoverInfo,
  MIXED_EDGE_LABEL,
  NODE_SIZE,
  nodeColor,
  nodeIcon,
} from "./graphViewShared";

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

// WHY: the exported `NodeImageProgram` is typed for untyped graph attributes;
// building it through the factory binds it to this graph's attribute types.
const NodeImageProgram = createNodeImageProgram<SigmaNodeAttrs, SigmaEdgeAttrs>();

// WHY: Sigma 3.0 keeps `getNodeAtPosition` private, and hover picking while the
// pointer drags the camera has no public equivalent. Read the member through
// element access (the sanctioned escape hatch for a private member, which the
// published declaration leaves untyped), check that it is callable, and fall
// back to enterNode/leaveNode events if a future Sigma drops it.
type NodePicker = (position: { x: number; y: number }) => string | null;

/** Rebuild the API graph node a click refers to from its renderer attributes. */
function graphNodeFromAttrs(id: string, attrs: SigmaNodeAttrs): GraphNode {
  return {
    id,
    label: attrs.label ?? id,
    kind: attrs.kind,
    lang: attrs.lang,
    path: attrs.path,
    notePath: attrs.notePath,
    nodeId: attrs.nodeId,
    nodeRef: attrs.nodeRef,
    sourceLocator: attrs.sourceLocator,
    module: attrs.module,
    community: attrs.community,
    resolvedType: attrs.resolvedType,
    hub: attrs.hub,
    authority: attrs.authority,
    pageRank: attrs.pageRank,
    score: attrs.score,
  };
}

function isNodePicker(value: unknown): value is NodePicker {
  return typeof value === "function";
}

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
}: {
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
  const containerRef = useRef<HTMLDivElement | null>(null);
  const sigmaRef = useRef<SigmaRenderer | null>(null);
  const graphRef = useRef<SigmaGraph | null>(null);
  const onNodeClickRef = useRef(onNodeClick);
  const globalLayoutRef = useRef<Map<string, { x: number; y: number }>>(new Map());

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
  });

  const refreshRef = useRef<number | null>(null);

  const [hover, setHover] = useState<GraphHoverInfo | null>(null);
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

  const clusters = useMemo(() => {
    const byCommunity = new Map<string, { size: number; topLabel: string; topAuthority: number }>();
    nodes.forEach((node) => {
      if (!node.community) return;

      const current = byCommunity.get(node.community) || {
        size: 0,
        topLabel: "",
        topAuthority: -1,
      };

      current.size += 1;

      if (node.kind === "note" && (node.authority ?? -1) > current.topAuthority) {
        current.topAuthority = node.authority ?? -1;
        current.topLabel = node.label;
      }

      byCommunity.set(node.community, current);
    });

    return Array.from(byCommunity.entries())
      .map(([id, data]) => ({
        id,
        size: data.size,
        label: data.topLabel || `Cluster ${id}`,
        color: communityColors.get(id) || "#64748b",
      }))
      .sort((a, b) => b.size - a.size);
  }, [communityColors, nodes]);

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

    // WHY: type highlighting belongs in Sigma reducers so changing it refreshes
    // existing node and edge attributes without rebuilding the graph, rerunning
    // layout, or changing the camera state.
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

  useEffect(() => {
    const container = containerRef.current;

    if (!container || needsIndex || nodes.length === 0) return;
    container.style.cursor = "grab";

    setHover(null);
    stateRef.current.hoverCommunity = null;
    stateRef.current.hoverNodeID = null;

    if (sigmaRef.current) {
      sigmaRef.current.kill();
      sigmaRef.current = null;
    }

    graphRef.current = null;

    const graph: SigmaGraph = new Graph();
    graphRef.current = graph;

    const total = Math.max(1, nodes.length);
    const radius = Math.max(80, Math.sqrt(total) * 28);

    const hasCompleteGlobalLayout =
      scope.mode === "global" &&
      nodes.length > 0 &&
      nodes.every((node) => globalLayoutRef.current.has(node.id));

    nodes.forEach((n, index) => {
      const cached = scope.mode === "global" ? globalLayoutRef.current.get(n.id) : undefined;
      const angle = (index / total) * Math.PI * 2;
      const jitter = (Math.random() - 0.5) * 12;
      const x = cached ? cached.x : Math.cos(angle) * radius + jitter;
      const y = cached ? cached.y : Math.sin(angle) * radius + jitter;

      const baseSize =
        (n.kind === "module" ? NODE_SIZE.base.module : NODE_SIZE.base.normal) + (n.center ? 2 : 0);

      const baseColor = nodeColor(n, communityColors, typeColors);
      graph.addNode(n.id, {
        label: n.label,
        kind: n.kind,
        center: Boolean(n.center),
        ...graphNodeIdentityAttrs(n),
        module: n.module,
        lang: n.lang,
        community: n.community,
        resolvedType: n.resolvedType,
        authority: n.authority,
        hub: n.hub,
        pageRank: n.pageRank,
        score: n.score,
        problemCount: 0,
        baseSize,
        baseColor,
        size: baseSize,
        color: baseColor,
        type: "circle",
        image: nodeIcon(n.kind),
        x,
        y,
      });
    });

    const edgeAgg = new Map<
      string,
      {
        source: string;
        target: string;
        weight: number;
        kind: string;
        label?: string;
      }
    >();

    edges.forEach((e) => {
      if (!graph.hasNode(e.source) || !graph.hasNode(e.target)) return;
      const a = e.source <= e.target ? e.source : e.target;
      const b = e.source <= e.target ? e.target : e.source;
      const key = `${a}|${b}`;
      const current = edgeAgg.get(key);

      if (!current) {
        edgeAgg.set(key, {
          source: a,
          target: b,
          weight: e.weight ?? 1,
          kind: e.kind,
          label: e.relationLabel || edgeKindLabel(e.kind),
        });
      } else {
        current.weight += e.weight ?? 1;

        if (current.kind !== e.kind) {
          current.kind = "mixed";
          current.label = MIXED_EDGE_LABEL;
        }
      }
    });

    Array.from(edgeAgg.values()).forEach((e, index) => {
      graph.addEdgeWithKey(`edge-${index}`, e.source, e.target, {
        size: Math.max(0.6, Math.min(4, Math.sqrt(e.weight))),
        weight: e.weight,
        color: EDGE_COLORS.faint,
        baseColor: EDGE_COLORS.faint,
        kind: e.kind,
        label: e.label || edgeKindLabel(e.kind),
      });
    });

    if (graph.order > 1) {
      const shouldRunLayout = scope.mode !== "global" || !hasCompleteGlobalLayout;

      if (shouldRunLayout) {
        const isWarmGlobal = scope.mode === "global" && globalLayoutRef.current.size > 0;

        const iterations = isWarmGlobal
          ? 28
          : graph.order > 400
            ? 50
            : graph.order > 200
              ? 80
              : 120;

        const settings = forceAtlas2.inferSettings(graph);
        settings.edgeWeightInfluence = 1;
        forceAtlas2.assign(graph, { iterations, settings });
      }
    }

    if (scope.mode === "global") {
      const nextLayout = new Map<string, { x: number; y: number }>();
      graph.forEachNode((id, attrs) => {
        nextLayout.set(id, { x: attrs.x, y: attrs.y });
      });
      globalLayoutRef.current = nextLayout;
    }

    // WHY: allowInvalidContainer keeps Sigma from throwing when its container
    // has zero width at mount time — this can happen on the first commit
    // before flex layout has resolved cross-axis sizing. Sigma resizes itself
    // once layout settles, so the only effect is suppressing a fatal error
    // that would otherwise tear down the React tree without an error boundary.
    const sigma: SigmaRenderer = new Sigma<SigmaNodeAttrs, SigmaEdgeAttrs>(graph, container, {
      renderEdgeLabels: stateRef.current.showEdgeLabels,
      labelDensity: 0.07,
      labelGridCellSize: 90,
      labelRenderedSizeThreshold: 10,
      defaultNodeType: "circle",
      defaultEdgeColor: EDGE_COLORS.faint,
      minCameraRatio: CAMERA.minRatio,
      maxCameraRatio: CAMERA.maxRatio,
      zIndex: true,
      enableEdgeEvents: true,
      edgeLabelSize: 9,
      edgeLabelWeight: "500",
      edgeLabelColor: { color: "#9a9488" },
      nodeProgramClasses: { image: NodeImageProgram },
      nodeHoverProgramClasses: { image: NodeImageProgram },
      allowInvalidContainer: true,
    });

    sigmaRef.current = sigma;
    const camera = sigma.getCamera();
    stateRef.current.cameraRatio = camera.getState().ratio;

    sigma.setSetting("nodeReducer", (nodeId, attrs) => {
      const lod = lodFromRatio(stateRef.current.cameraRatio);

      const result = computeNodeStyle({
        nodeId,
        attrs,
        state: stateRef.current,
        lod,
      });

      return {
        ...attrs,
        hidden: result.hidden,
        type: result.type,
        image: result.image,
        size: result.size,
        color: result.color,
        label: result.label,
      };
    });

    sigma.setSetting("edgeReducer", (edgeId, attrs) => {
      const source = graph.source(edgeId);
      const target = graph.target(edgeId);
      const sourceAttrs = graph.getNodeAttributes(source);
      const targetAttrs = graph.getNodeAttributes(target);
      const lod = lodFromRatio(stateRef.current.cameraRatio);

      const result = computeEdgeStyle({
        source,
        target,
        attrs,
        sourceAttrs,
        targetAttrs,
        state: stateRef.current,
        lod,
      });

      return {
        ...attrs,
        hidden: result.hidden,
        color: result.color,
        size: result.size,
        label: result.label,
      };
    });

    const pickCandidate = sigma["getNodeAtPosition"];

    const picker: NodePicker | null = isNodePicker(pickCandidate)
      ? pickCandidate.bind(sigma)
      : null;

    const mouseCaptor = sigma.getMouseCaptor();
    const mouseCanvas = sigma.getCanvases().mouse;

    const onWheel = (event: WheelEvent) => {
      // Trackpad pinches arrive as Ctrl+wheel. Leave ordinary scrolling native,
      // before Sigma's wheel listener can consume it or move the camera.
      event.stopImmediatePropagation();

      if (!event.ctrlKey) return;
      event.preventDefault();
      const bounds = container.getBoundingClientRect();

      const delta =
        event.deltaY * (event.deltaMode === 1 ? 16 : event.deltaMode === 2 ? bounds.height : 1);

      const ratio = camera.getBoundedRatio(camera.getState().ratio * Math.exp(delta * 0.01));
      camera.setState(
        sigma.getViewportZoomedState(
          { x: event.clientX - bounds.left, y: event.clientY - bounds.top },
          ratio,
        ),
      );
    };

    mouseCanvas?.addEventListener("wheel", onWheel, { capture: true, passive: false });
    let hoverNodeID: string | null = null;
    let hoverFrameID: number | null = null;
    let lastPointer: { x: number; y: number } | null = null;

    const syncHoverNode = (nextNode: string | null) => {
      if (hoverNodeID === nextNode) return;
      hoverNodeID = nextNode;

      if (!nextNode) {
        container.style.cursor = "grab";

        if (mouseCanvas) mouseCanvas.style.cursor = "grab";
        stateRef.current.hoverNodeID = null;
        stateRef.current.hoverCommunity = null;
        setHover(null);
        scheduleRefresh();

        return;
      }

      const attrs = graph.getNodeAttributes(nextNode);
      container.style.cursor = "pointer";

      if (mouseCanvas) mouseCanvas.style.cursor = "pointer";
      stateRef.current.hoverNodeID = nextNode;
      stateRef.current.hoverCommunity =
        attrs.kind === "code" && attrs.community ? attrs.community : null;
      setHover({
        label: attrs.label ?? nextNode,
        kind: attrs.kind,
        path: attrs.path,
        nodeId: attrs.nodeId || nextNode,
        resolvedType: attrs.resolvedType,
        connectionCount: connectionCounts.get(nextNode) || 0,
        problemCount: attrs.problemCount,
      });
      scheduleRefresh();
    };

    sigma.on("enterNode", ({ node }) => syncHoverNode(node));
    sigma.on("leaveNode", () => syncHoverNode(null));

    const clearHoverFrame = () => {
      if (hoverFrameID === null) return;
      window.cancelAnimationFrame(hoverFrameID);
      hoverFrameID = null;
    };

    const flushHoverFromPointer = () => {
      hoverFrameID = null;

      if (!lastPointer || !picker) return;
      const hovered = picker({ x: lastPointer.x, y: lastPointer.y });
      syncHoverNode(hovered ?? null);
    };

    const queueHoverFromPointer = (x: number, y: number) => {
      lastPointer = { x, y };

      if (hoverFrameID !== null) return;
      hoverFrameID = window.requestAnimationFrame(flushHoverFromPointer);
    };

    const onMouseMoveBody = ({ x, y }: { x: number; y: number }) => {
      if (!picker) return;
      const { width, height } = sigma.getDimensions();
      const inside = x >= 0 && y >= 0 && x <= width && y <= height;

      if (!inside) {
        lastPointer = null;
        clearHoverFrame();
        syncHoverNode(null);

        return;
      }

      queueHoverFromPointer(x, y);
    };

    if (picker) {
      mouseCaptor.on("mousemovebody", onMouseMoveBody);
    }

    const onCameraUpdated = () => {
      const nextRatio = camera.getState().ratio;

      if (stateRef.current.cameraRatio !== nextRatio) {
        stateRef.current.cameraRatio = nextRatio;
        scheduleRefresh();
      }

      if (picker && lastPointer) queueHoverFromPointer(lastPointer.x, lastPointer.y);
    };

    camera.on("updated", onCameraUpdated);

    sigma.on("clickNode", ({ node, event }) => {
      const attrs = graph.getNodeAttributes(node);
      const original = event.original;

      const beside =
        original instanceof MouseEvent && (original.metaKey || original.ctrlKey) ? true : false;

      onNodeClickRef.current(graphNodeFromAttrs(node, attrs), { beside });
    });

    scheduleRefresh();

    return () => {
      camera.off("updated", onCameraUpdated);
      mouseCanvas?.removeEventListener("wheel", onWheel, true);

      if (picker) mouseCaptor.removeListener("mousemovebody", onMouseMoveBody);
      clearHoverFrame();
      container.style.cursor = "grab";

      if (mouseCanvas) mouseCanvas.style.cursor = "grab";
      sigma.kill();
      sigmaRef.current = null;
      graphRef.current = null;
    };
  }, [
    communityColors,
    connectionCounts,
    edges,
    needsIndex,
    nodes,
    scheduleRefresh,
    scope.mode,
    typeColors,
  ]);

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
  }, [scope.label, scope.mode]);

  const handleZoomIn = useCallback(() => {
    const sigma = sigmaRef.current;

    if (!sigma) return;
    const camera = sigma.getCamera();
    const ratio = Math.max(CAMERA.minRatio, camera.getState().ratio * CAMERA.zoomStep);
    camera.animate({ ratio }, { duration: 200 });
  }, []);

  const handleZoomOut = useCallback(() => {
    const sigma = sigmaRef.current;

    if (!sigma) return;
    const camera = sigma.getCamera();
    const ratio = Math.min(CAMERA.maxRatio, camera.getState().ratio / CAMERA.zoomStep);
    camera.animate({ ratio }, { duration: 200 });
  }, []);

  const handleFitToView = useCallback(() => {
    const sigma = sigmaRef.current;

    if (!sigma) return;
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
