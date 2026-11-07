import { themedNodeHover } from "./sigmaRendererDisclosure";
import { drawDisclosure } from "./sigmaRendererDisclosure";
import { drawFolderLinks } from "./sigmaFolderLinks";
import { drawTestEdges } from "./explorerGraph";
import type { ExplorerNodeAttrs as SigmaNodeAttrs } from "./explorerGraph";
import { createNodeImageProgram } from "@sigma/node-image";
import Graph from "graphology";
import Sigma from "sigma";
import { drawStraightEdgeLabel } from "sigma/rendering";
import type { RefObject } from "react";
import type { GraphNode } from "../api/types";
import { createLabelPainter } from "./sigmaLabels";
import {
  computeEdgeStyle,
  computeNodeStyle,
  lodFromRatio,
  mixColors,
  type GraphColors,
  type ReducerState,
  type SigmaEdgeAttrs,
} from "../lib/graphStyle";
import { resolveCssColor } from "../lib/themeColors";
import { CAMERA, type HoverInfo } from "./graphViewShared";
import type { GraphNodeClickOptions } from "./useSigmaGraph";

type SigmaRenderer = Sigma<SigmaNodeAttrs, SigmaEdgeAttrs>;

type SigmaGraph = Graph<SigmaNodeAttrs, SigmaEdgeAttrs>;

type GraphHoverInfo = HoverInfo & { nodeId?: string; resolvedType?: string; problemCount?: number };

const NodeImageProgram = createNodeImageProgram<SigmaNodeAttrs, SigmaEdgeAttrs>();

// WHY: Sigma 3.0 keeps `getNodeAtPosition` private, and hover picking while the
// pointer drags the camera has no public equivalent. Read the member through
// element access (the sanctioned escape hatch for a private member, which the
// published declaration leaves untyped), check that it is callable, and fall
// back to enterNode/leaveNode events if a future Sigma drops it.
type NodePicker = (position: { x: number; y: number }) => string | null;

/** How far outside a node's disc, in CSS pixels, the pointer still picks it. */
const HIT_SLOP = 8;

export type ThemeColors = GraphColors & {
  label: string;
  edgeLabel: string;
  surface: string;
  /** The graph canvas behind node labels. */
  canvas: string;
};

/** Read the renderer colors from the current theme's tokens. */
export function readThemeColors(): ThemeColors {
  const border = resolveCssColor("var(--border)");
  const strong = resolveCssColor("var(--border-strong)");
  const faint = resolveCssColor("var(--text-faint)");

  return {
    neutral: resolveCssColor("var(--type-8)"),
    edgeFaint: border,
    edgeLight: mixColors(border, strong, 0.35),
    edgeStrong: mixColors(strong, faint, 0.35),
    label: resolveCssColor("var(--text)"),
    edgeLabel: resolveCssColor("var(--text-muted)"),
    surface: resolveCssColor("var(--surface)"),
    canvas: resolveCssColor("var(--surface-alt)"),
  };
}

export function rendererColorSettings(colors: ThemeColors) {
  return {
    defaultEdgeColor: colors.edgeFaint,
    labelColor: { color: colors.label },
    edgeLabelColor: { color: colors.edgeLabel },
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
    childCount: attrs.childCount,
    collapsed: attrs.collapsed,
    hub: attrs.hub,
    authority: attrs.authority,
    pageRank: attrs.pageRank,
    score: attrs.score,
  };
}

function isNodePicker(value: unknown): value is NodePicker {
  return typeof value === "function";
}

export function mountSigmaRenderer({
  container,
  sigmaRef,
  graphRef,
  stateRef,
  colorsRef,
  onNodeClickRef,
  connectionCountsRef,
  setHover,
  scheduleRefresh,
}: {
  container: HTMLDivElement;
  sigmaRef: RefObject<SigmaRenderer | null>;
  graphRef: RefObject<SigmaGraph | null>;
  stateRef: RefObject<ReducerState>;
  colorsRef: RefObject<ThemeColors | null>;
  onNodeClickRef: RefObject<(node: GraphNode, options: GraphNodeClickOptions) => void>;
  connectionCountsRef: RefObject<Map<string, number>>;
  setHover: (hover: GraphHoverInfo | null) => void;
  scheduleRefresh: () => void;
}) {
  container.style.cursor = "grab";

  const labels = createLabelPainter(
    () => sigmaRef.current,
    () => colorsRef.current ?? readThemeColors(),
  );

  // allowInvalidContainer keeps Sigma from throwing when its container
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
    // The plotted bounds keep clear of the canvas edge and the zoom controls.
    stagePadding: 44,
    // Canvas text cannot read CSS variables; this is --font-sans.
    labelFont: "Inter, -apple-system, system-ui, sans-serif",
    edgeLabelFont: "Inter, -apple-system, system-ui, sans-serif",
    defaultNodeType: "circle",
    ...rendererColorSettings(colorsRef.current ?? readThemeColors()),
    defaultDrawNodeHover: themedNodeHover(() => colorsRef.current ?? readThemeColors()),
    defaultDrawNodeLabel: labels.draw,
    defaultDrawEdgeLabel: (context, data, source, target, settings) => {
      if (data.kind !== "folder-crosslink")
        drawStraightEdgeLabel(context, data, source, target, settings);
    },
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
  sigma.on("beforeRender", labels.beginFrame);
  const outlines = sigma.createCanvas("outlines", { beforeLayer: "labels" });
  sigma.on("afterRender", () => {
    const context = outlines.getContext("2d");

    if (!context) return;
    const { width, height } = sigma.getDimensions();
    const ratio = window.devicePixelRatio || 1;
    outlines.width = width * ratio;
    outlines.height = height * ratio;
    outlines.style.width = `${width}px`;
    outlines.style.height = `${height}px`;
    context.save();
    context.scale(outlines.width / width, outlines.height / height);
    context.clearRect(0, 0, width, height);
    drawFolderLinks(sigma, context, stateRef.current);
    drawTestEdges(sigma, context, resolveCssColor("var(--explorer-code)"));
    context.strokeStyle = (colorsRef.current ?? readThemeColors()).label;
    context.lineWidth = 1.5;
    sigma.getGraph().forEachNode((id, attrs) => {
      const node = sigma.getNodeDisplayData(id);

      if (!node || node.hidden) return;

      if (id.startsWith("file-more:")) {
        drawDisclosure(sigma, context, id);

        return;
      }

      const point = sigma.framedGraphToViewport(node);
      const palette = colorsRef.current ?? readThemeColors();
      context.globalAlpha = 1;
      context.strokeStyle = attrs.muted
        ? node.color
        : attrs.aggregate
          ? attrs.baseColor
          : attrs.explorerLabels && (attrs.kind === "note" || attrs.kind === "embedded")
            ? attrs.baseColor
            : (colorsRef.current ?? readThemeColors()).label;

      context.beginPath();
      const r = sigma.scaleSize(node.size);

      if (attrs.explorerLabels && attrs.kind === "module") {
        context.roundRect(point.x - r, point.y - r, r * 2, r * 2, Math.min(4, r / 3));
        context.fillStyle = node.color;
        context.fill();
      } else context.arc(point.x, point.y, r, 0, 2 * Math.PI);

      if (attrs.aggregate) {
        context.fillStyle = (colorsRef.current ?? readThemeColors()).canvas;
        context.fill();
      }

      context.setLineDash(attrs.aggregate ? [3, 3] : []);
      context.stroke();
      context.setLineDash([]);

      if (attrs.selected || attrs.searchMatchCount) {
        context.beginPath();
        context.strokeStyle = palette.label;
        context.lineWidth = 2;

        if (attrs.kind === "module")
          context.roundRect(point.x - r - 4, point.y - r - 4, 2 * r + 8, 2 * r + 8, 5);
        else context.arc(point.x, point.y, r + 4, 0, 2 * Math.PI);
        context.stroke();
        context.lineWidth = 1.5;
      }
    });
    context.globalAlpha = 1;
    context.restore();
  });
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
      size:
        attrs.explorerLabels && attrs.fixed && attrs.kind !== "module"
          ? nodeId.startsWith("file-more:")
            ? 12
            : attrs.selected
              ? 7
              : attrs.baseSize
          : result.size,
      color: nodeId.startsWith("file-more:")
        ? "#00000000"
        : attrs.muted
          ? mixColors(result.color, (colorsRef.current ?? readThemeColors()).canvas, 0.6)
          : result.color,
      label: result.label,
      forceLabel: Boolean(attrs.landmark && result.label),
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
      forceLabel: false,
    };
  });

  const pickCandidate = sigma["getNodeAtPosition"];

  const exactPicker: NodePicker | null = isNodePicker(pickCandidate)
    ? pickCandidate.bind(sigma)
    : null;

  // Explorer draws nodes at 2-3px, too small to hover or click by their pixels
  // alone. A miss falls back to the node whose label is under the pointer, then
  // to the nearest node within HIT_SLOP of its edge.
  // ponytail: a linear scan per animation frame; index the points if graphs grow far past NODE_AVOID_ORDER.
  const picker: NodePicker | null = exactPicker
    ? (position) => {
        const exact = exactPicker(position);

        if (exact) return exact;
        const label = labels.labelAt(position.x, position.y);

        if (label) return label;
        let nearest: string | null = null;
        let gap: number = HIT_SLOP;

        sigma.getGraph().forEachNode((id) => {
          const data = sigma.getNodeDisplayData(id);

          if (!data || data.hidden) return;
          const point = sigma.framedGraphToViewport(data);

          const distance =
            Math.hypot(point.x - position.x, point.y - position.y) - sigma.scaleSize(data.size);

          if (distance <= gap) {
            nearest = id;
            gap = distance;
          }
        });

        return nearest;
      }
    : null;

  const mouseCaptor = sigma.getMouseCaptor();
  const mouseCanvas = sigma.getCanvases().mouse;

  const onWheel = (event: WheelEvent) => {
    // Trackpad pinches arrive as Ctrl+wheel and zoom; any other scroll pans.
    // Handle both before Sigma's own wheel listener zooms on every scroll.
    event.stopImmediatePropagation();
    event.preventDefault();
    const bounds = container.getBoundingClientRect();
    const scale = event.deltaMode === 1 ? 16 : event.deltaMode === 2 ? bounds.height : 1;

    if (!event.ctrlKey) {
      const { width, height } = sigma.getDimensions();

      const center = sigma.viewportToFramedGraph({
        x: width / 2 + event.deltaX * scale,
        y: height / 2 + event.deltaY * scale,
      });

      camera.setState({ x: center.x, y: center.y });

      return;
    }

    const delta = event.deltaY * scale;

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
      stateRef.current.hoverNeighbors = undefined;
      stateRef.current.hoverCommunity = null;
      setHover(null);
      scheduleRefresh();

      return;
    }

    const attrs = sigma.getGraph().getNodeAttributes(nextNode);
    container.style.cursor = attrs.collapsed ? "default" : "pointer";

    if (mouseCanvas) mouseCanvas.style.cursor = attrs.collapsed ? "default" : "pointer";
    stateRef.current.hoverNodeID = nextNode;
    stateRef.current.hoverNeighbors = new Set(sigma.getGraph().neighbors(nextNode));
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

  // With a picker, pointer moves clear hover, so Sigma's pixel-exact leaveNode
  // cannot drop a hover the wider target still holds.
  if (!picker) sigma.on("leaveNode", () => syncHoverNode(null));

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

  const clickNode = (node: string, original: MouseEvent | TouchEvent) => {
    const attrs = sigma.getGraph().getNodeAttributes(node);

    const beside =
      original instanceof MouseEvent && (original.metaKey || original.ctrlKey) ? true : false;

    onNodeClickRef.current(graphNodeFromAttrs(node, attrs), { beside });
  };

  sigma.on("clickNode", ({ node, event }) => clickNode(node, event.original));

  // A click near a node, or on its label, opens it like a click on the node.
  const clickNearbyNode = (event: { x: number; y: number; original: MouseEvent | TouchEvent }) => {
    const node = picker?.({ x: event.x, y: event.y });

    if (node) clickNode(node, event.original);
  };

  sigma.on("clickStage", ({ event }) => clickNearbyNode(event));
  sigma.on("clickEdge", ({ event }) => clickNearbyNode(event));

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
}
