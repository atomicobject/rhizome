import { createNodeImageProgram } from "@sigma/node-image";
import Graph from "graphology";
import Sigma from "sigma";
import { drawDiscNodeLabel, type NodeHoverDrawingFunction } from "sigma/rendering";
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
  type SigmaNodeAttrs,
} from "../lib/graphStyle";
import { resolveCssColor } from "../lib/themeColors";
import { CAMERA, type HoverInfo } from "./graphViewShared";
import type { GraphNodeClickOptions } from "./useSigmaGraph";

type SigmaRenderer = Sigma<SigmaNodeAttrs, SigmaEdgeAttrs>;

type SigmaGraph = Graph<SigmaNodeAttrs, SigmaEdgeAttrs>;

type GraphHoverInfo = HoverInfo & { nodeId?: string; resolvedType?: string; problemCount?: number };

// WHY: the exported `NodeImageProgram` is typed for untyped graph attributes;
// building it through the factory binds it to this graph's attribute types.
const NodeImageProgram = createNodeImageProgram<SigmaNodeAttrs, SigmaEdgeAttrs>();

// WHY: Sigma 3.0 keeps `getNodeAtPosition` private, and hover picking while the
// pointer drags the camera has no public equivalent. Read the member through
// element access (the sanctioned escape hatch for a private member, which the
// published declaration leaves untyped), check that it is callable, and fall
// back to enterNode/leaveNode events if a future Sigma drops it.
type NodePicker = (position: { x: number; y: number }) => string | null;

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
    edgeLabel: faint,
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
    childCount: attrs.childCount,
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
    // The plotted bounds keep clear of the canvas edge and the zoom controls.
    stagePadding: 44,
    // Canvas text cannot read CSS variables; this is --font-sans.
    labelFont: "Inter, -apple-system, system-ui, sans-serif",
    edgeLabelFont: "Inter, -apple-system, system-ui, sans-serif",
    defaultNodeType: "circle",
    ...rendererColorSettings(colorsRef.current ?? readThemeColors()),
    defaultDrawNodeHover: themedNodeHover(() => colorsRef.current ?? readThemeColors()),
    defaultDrawNodeLabel: labels.draw,
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
    context.strokeStyle = (colorsRef.current ?? readThemeColors()).label;
    context.lineWidth = 1.5;
    sigma.getGraph().forEachNode((id, attrs) => {
      const node = sigma.getNodeDisplayData(id);

      if (!node || node.hidden) return;
      const point = sigma.framedGraphToViewport(node);
      context.strokeStyle = attrs.aggregate
        ? attrs.baseColor
        : (colorsRef.current ?? readThemeColors()).label;

      context.beginPath();
      context.arc(point.x, point.y, sigma.scaleSize(node.size), 0, 2 * Math.PI);

      if (attrs.aggregate) {
        context.fillStyle = (colorsRef.current ?? readThemeColors()).canvas;
        context.fill();
      }

      context.stroke();
    });
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
      size: result.size,
      color: result.color,
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
      forceLabel: Boolean(result.label && attrs.kind === "folder-crosslink"),
    };
  });

  const pickCandidate = sigma["getNodeAtPosition"];

  const picker: NodePicker | null = isNodePicker(pickCandidate) ? pickCandidate.bind(sigma) : null;

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
    container.style.cursor = "pointer";

    if (mouseCanvas) mouseCanvas.style.cursor = "pointer";
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
}
