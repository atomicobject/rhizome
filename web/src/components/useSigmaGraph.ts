import { createNodeImageProgram } from "@sigma/node-image";
import Graph from "graphology";
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import Sigma from "sigma";
import { drawDiscNodeLabel, type NodeHoverDrawingFunction } from "sigma/rendering";
import type { GraphEdge, GraphNode } from "../api/types";
import {
  computeEdgeStyle,
  computeNodeStyle,
  type GraphColors,
  graphNodeIdentityAttrs,
  lodFromRatio,
  mixColors,
  type ReducerState,
  type SigmaEdgeAttrs,
  type SigmaNodeAttrs,
} from "../lib/graphStyle";
import { resolveCssColor, subscribeColorScheme } from "../lib/themeColors";
import {
  CAMERA,
  communityColor,
  edgeKindLabel,
  type GraphScope,
  type GraphViewNode,
  type HoverInfo,
  MIXED_EDGE_LABEL,
  NODE_SIZE,
  nodeColor,
  nodeIcon,
} from "./graphViewShared";
import { type LayoutInput, type LayoutPositions, runLayout } from "./graphLayout";

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

type ThemeColors = GraphColors & { label: string; edgeLabel: string; surface: string };

/** Read the renderer colors from the current theme's tokens. */
function readThemeColors(): ThemeColors {
  const border = resolveCssColor("var(--border)");
  const strong = resolveCssColor("var(--border-strong)");
  const faint = resolveCssColor("var(--text-faint)");

  return {
    neutral: resolveCssColor("var(--type-8)"),
    edgeFaint: border,
    edgeLight: mixColors(border, strong, 0.35),
    edgeStrong: mixColors(strong, faint, 0.35),
    label: resolveCssColor("var(--text)"),
    edgeLabel: faint,
    surface: resolveCssColor("var(--surface)"),
  };
}

function rendererColorSettings(colors: ThemeColors) {
  return {
    defaultEdgeColor: colors.edgeFaint,
    labelColor: { color: colors.label },
    edgeLabelColor: { color: colors.edgeLabel },
  };
}

// WHY: Sigma's default hover box is hard-coded white, which hides the light
// label text of the dark theme. This draws the same shape on the surface token.
function themedNodeHover(
  colors: () => ThemeColors,
): NodeHoverDrawingFunction<SigmaNodeAttrs, SigmaEdgeAttrs> {
  return (context, data, settings) => {
    const size = settings.labelSize;
    const padding = 2;
    context.font = `${settings.labelWeight} ${size}px ${settings.labelFont}`;
    context.fillStyle = colors().surface;
    context.shadowBlur = 8;
    context.shadowColor = "rgba(0, 0, 0, 0.35)";

    const radius = Math.max(data.size, size / 2) + padding;
    context.beginPath();

    if (data.label) {
      const boxWidth = Math.round(context.measureText(data.label).width + 5);
      const boxHeight = Math.round(size + 2 * padding);
      const angle = Math.asin(boxHeight / 2 / radius);
      const dx = Math.sqrt(Math.abs(radius ** 2 - (boxHeight / 2) ** 2));
      context.moveTo(data.x + dx, data.y + boxHeight / 2);
      context.lineTo(data.x + radius + boxWidth, data.y + boxHeight / 2);
      context.lineTo(data.x + radius + boxWidth, data.y - boxHeight / 2);
      context.lineTo(data.x + dx, data.y - boxHeight / 2);
      context.arc(data.x, data.y, radius, angle, -angle);
    } else {
      context.arc(data.x, data.y, data.size + padding, 0, Math.PI * 2);
    }

    context.closePath();
    context.fill();
    context.shadowBlur = 0;
    drawDiscNodeLabel(context, data, settings);
  };
}

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
        color: communityColors.get(id) || "var(--type-8)",
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

  // One renderer per mounted container. Data refreshes swap its graph (below)
  // instead of recreating it, so WebGL state and the camera survive a refresh.
  const empty = nodes.length === 0;

  useEffect(() => {
    const container = containerRef.current;

    if (!container || needsIndex || empty) return;
    container.style.cursor = "grab";

    // WHY: allowInvalidContainer keeps Sigma from throwing when its container
    // has zero width at mount time — this can happen on the first commit
    // before flex layout has resolved cross-axis sizing. Sigma resizes itself
    // once layout settles, so the only effect is suppressing a fatal error
    // that would otherwise tear down the React tree without an error boundary.
    const sigma: SigmaRenderer = new Sigma<SigmaNodeAttrs, SigmaEdgeAttrs>(new Graph(), container, {
      renderEdgeLabels: stateRef.current.showEdgeLabels,
      labelDensity: 0.07,
      // Wider cells keep long labels from overlapping in dense clusters.
      labelGridCellSize: 140,
      labelRenderedSizeThreshold: 10,
      // Canvas text cannot read CSS variables; this is --font-sans.
      labelFont: "Barlow, -apple-system, system-ui, sans-serif",
      edgeLabelFont: "Barlow, -apple-system, system-ui, sans-serif",
      defaultNodeType: "circle",
      ...rendererColorSettings(colorsRef.current ?? readThemeColors()),
      defaultDrawNodeHover: themedNodeHover(() => colorsRef.current ?? readThemeColors()),
      minCameraRatio: CAMERA.minRatio,
      maxCameraRatio: CAMERA.maxRatio,
      zIndex: true,
      enableEdgeEvents: true,
      edgeLabelSize: 9,
      edgeLabelWeight: "500",
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
      const graph = sigma.getGraph();
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
    let hoverFrameID: number | null = null;
    let lastPointer: { x: number; y: number } | null = null;

    // Compare with the shared hover state, which a graph swap clears.
    const syncHoverNode = (nextNode: string | null) => {
      if (stateRef.current.hoverNodeID === nextNode) return;

      if (!nextNode) {
        container.style.cursor = "grab";

        if (mouseCanvas) mouseCanvas.style.cursor = "grab";
        stateRef.current.hoverNodeID = null;
        stateRef.current.hoverCommunity = null;
        setHover(null);
        scheduleRefresh();

        return;
      }

      const attrs = sigma.getGraph().getNodeAttributes(nextNode);
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
        connectionCount: connectionCountsRef.current.get(nextNode) || 0,
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
      const attrs = sigma.getGraph().getNodeAttributes(node);
      const original = event.original;

      const beside =
        original instanceof MouseEvent && (original.metaKey || original.ctrlKey) ? true : false;

      onNodeClickRef.current(graphNodeFromAttrs(node, attrs), { beside });
    });

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
  }, [empty, needsIndex, scheduleRefresh]);

  // Builds the graph for the current data and installs it on the renderer once
  // laid out. Large layouts run in a worker; until one finishes, the renderer
  // keeps showing the previous graph.
  useEffect(() => {
    const sigma = sigmaRef.current;

    if (!sigma || needsIndex || empty) return;

    const graph: SigmaGraph = new Graph();

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

      const colorValue = nodeColor(n, communityColors, typeColors);
      const baseColor = resolveCssColor(colorValue);
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
        problemCount: problemCountsRef.current.get(n.id) ?? 0,
        baseSize,
        colorValue,
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
        color: stateRef.current.colors.edgeFaint,
        baseColor: stateRef.current.colors.edgeFaint,
        kind: e.kind,
        label: e.label || edgeKindLabel(e.kind),
      });
    });

    const shouldRunLayout =
      graph.order > 1 && (scope.mode !== "global" || !hasCompleteGlobalLayout);

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

      // Counts can arrive while a worker lays the graph out; install the latest.
      graph.forEachNode((id) => {
        graph.setNodeAttribute(id, "problemCount", problemCountsRef.current.get(id) ?? 0);
      });

      setHover(null);
      stateRef.current.hoverCommunity = null;
      stateRef.current.hoverNodeID = null;
      graphRef.current = graph;
      sigma.setGraph(graph);
      scheduleRefresh();
    };

    if (!shouldRunLayout) {
      install();

      return;
    }

    const isWarmGlobal = scope.mode === "global" && globalLayoutRef.current.size > 0;

    const iterations = isWarmGlobal ? 28 : graph.order > 400 ? 50 : graph.order > 200 ? 80 : 120;

    const layoutNodes: LayoutInput["nodes"] = [];
    graph.forEachNode((id, attrs) => layoutNodes.push({ id, x: attrs.x, y: attrs.y }));
    const layoutEdges: LayoutInput["edges"] = [];
    graph.forEachEdge((_edge, attrs, source, target) =>
      layoutEdges.push({ source, target, weight: attrs.weight ?? 1 }),
    );

    return runLayout({ nodes: layoutNodes, edges: layoutEdges, iterations }, install);
  }, [communityColors, edges, empty, needsIndex, nodes, scheduleRefresh, scope.mode, typeColors]);

  // A theme switch re-reads the token colors and repaints in place: no new
  // renderer, no layout run, and the camera stays where it was.
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
    // A different subject starts from the default view; a refresh of the same
    // subject keeps the reader's camera.
    sigmaRef.current?.getCamera().setState({ x: 0.5, y: 0.5, ratio: 1, angle: 0 });
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
