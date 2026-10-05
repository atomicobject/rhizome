import type { GraphNode, NodeRef } from "../api/types";

export type SigmaNodeAttrs = {
  label: string;
  kind: string;
  center?: boolean;
  path?: string;
  notePath?: string;
  nodeId?: string;
  sourceLocator?: string;
  nodeRef?: NodeRef;
  module?: string;
  lang?: string;
  community?: string;
  resolvedType?: string;
  authority?: number;
  hub?: number;
  pageRank?: number;
  score?: number;
  problemCount?: number;
  baseSize: number;
  baseColor: string;
  size: number;
  color: string;
  type: string;
  image?: string;
  x: number;
  y: number;
};

export function graphNodeIdentityAttrs(node: GraphNode) {
  return {
    path: node.path,
    notePath: node.notePath,
    nodeId: node.nodeId,
    sourceLocator: node.sourceLocator,
    nodeRef: node.nodeRef,
  };
}

export type SigmaEdgeAttrs = {
  size: number;
  weight: number;
  color: string;
  baseColor: string;
  kind: string;
  label: string;
};

export type ReducerState = {
  graphMatches: Set<string>;
  semanticMatches: Set<string>;
  highlightTypes: ReadonlySet<string>;
  graphFilter: boolean;
  searchActive: boolean;
  searchOnlyMatches: boolean;
  activeCommunity: string | null;
  scaleByAuthority: boolean;
  showEdgeLabels: boolean;
  hoverCommunity: string | null;
  hoverNodeID: string | null;
  cameraRatio: number;
  maxScore: number;
  showProblems?: boolean;
};

const CAMERA = {
  minRatio: 0.05,
  maxRatio: 4,
  zoomStep: 0.8,
};

const LOD = {
  showLabels: 0.18,
  showIcons: 0.28,
  highSalience: 0.72,
};

const NODE_ALPHA = {
  low: { normal: 0.9, zoomed: 0.58 },
  high: { normal: 1.0, zoomed: 0.92 },
  focus: { normal: 1.0, zoomed: 0.95 },
  deemph: { normal: 0.75, zoomed: 0.48 },
  deemphHover: { normal: 0.65, zoomed: 0.42 },
};

const TYPE_HIGHLIGHT_ALPHA = {
  node: 0.24,
  edge: 0.28,
};

const EDGE_COLORS = {
  faint: "#e8e5e0",
  light: "#dedad4",
  normal: "#d0ccc6",
  strong: "#b8b3ac",
};

const NODE_SIZE = {
  base: { normal: 6, module: 9 },
  min: { normal: 2.8, module: 4.5 },
  authorityBonus: 12,
  iconMinScreen: { normal: 8.5, zoomed: 12.5 },
};

const DESAT = {
  normal: { normal: 0.35, zoomed: 0.65 },
  hover: { normal: 0.55, zoomed: 0.8 },
};

function clamp(value: number, min: number, max: number) {
  return Math.min(max, Math.max(min, value));
}

function lerp(a: number, b: number, t: number) {
  return a + (b - a) * t;
}

export function lodFromRatio(ratio: number) {
  if (ratio <= 1) {
    return 0;
  }

  const clamped = clamp(ratio, 1, CAMERA.maxRatio);

  return clamp(Math.log(clamped) / Math.log(CAMERA.maxRatio), 0, 1);
}

function parseHexColor(color: string) {
  if (!color) {
    return null;
  }

  const hex = color.startsWith("#") ? color.slice(1) : color;

  if (hex.length === 3) {
    const r = parseInt(hex[0] + hex[0], 16);
    const g = parseInt(hex[1] + hex[1], 16);
    const b = parseInt(hex[2] + hex[2], 16);

    if (Number.isNaN(r) || Number.isNaN(g) || Number.isNaN(b)) {
      return null;
    }

    return { r, g, b };
  }

  if (hex.length === 6) {
    const r = parseInt(hex.slice(0, 2), 16);
    const g = parseInt(hex.slice(2, 4), 16);
    const b = parseInt(hex.slice(4, 6), 16);

    if (Number.isNaN(r) || Number.isNaN(g) || Number.isNaN(b)) {
      return null;
    }

    return { r, g, b };
  }

  return null;
}

function rgbToHex(r: number, g: number, b: number) {
  const toHex = (value: number) => clamp(Math.round(value), 0, 255).toString(16).padStart(2, "0");

  return `#${toHex(r)}${toHex(g)}${toHex(b)}`;
}

function mixColors(colorA: string, colorB: string, t: number) {
  const a = parseHexColor(colorA);
  const b = parseHexColor(colorB);

  if (!a || !b) {
    return colorA;
  }

  const tt = clamp(t, 0, 1);

  return rgbToHex(lerp(a.r, b.r, tt), lerp(a.g, b.g, tt), lerp(a.b, b.b, tt));
}

function colorWithAlpha(color: string, alpha: number) {
  if (!color || color.startsWith("rgba")) {
    return color;
  }

  if (color.startsWith("rgb(")) {
    const parts = color
      .replace("rgb(", "")
      .replace(")", "")
      .split(",")
      .map((part) => part.trim());

    if (parts.length >= 3) {
      return `rgba(${parts[0]}, ${parts[1]}, ${parts[2]}, ${alpha})`;
    }
  }

  const hex = color.startsWith("#") ? color.slice(1) : color;

  if (hex.length === 3) {
    const r = parseInt(hex[0] + hex[0], 16);
    const g = parseInt(hex[1] + hex[1], 16);
    const b = parseInt(hex[2] + hex[2], 16);

    return `rgba(${r}, ${g}, ${b}, ${alpha})`;
  }

  if (hex.length === 6) {
    const r = parseInt(hex.slice(0, 2), 16);
    const g = parseInt(hex.slice(2, 4), 16);
    const b = parseInt(hex.slice(4, 6), 16);

    return `rgba(${r}, ${g}, ${b}, ${alpha})`;
  }

  return color;
}

// Sigma blends with ONE, ONE_MINUS_SRC_ALPHA, so translucent colors must carry
// their alpha in RGB as well as in the alpha channel.
function colorWithPremultipliedAlpha(color: string, alpha: number) {
  const parsed = parseHexColor(color);

  if (!parsed) {
    return colorWithAlpha(color, alpha);
  }

  return `rgba(${Math.round(parsed.r * alpha)}, ${Math.round(parsed.g * alpha)}, ${Math.round(parsed.b * alpha)}, ${alpha})`;
}

function isTypeHighlighted(resolvedType: string | undefined, highlightTypes: ReadonlySet<string>) {
  const typeName = resolvedType?.trim();

  return Boolean(typeName && highlightTypes.has(typeName));
}

export type NodeReducerInput = {
  nodeId: string;
  attrs: SigmaNodeAttrs;
  state: ReducerState;
  lod: number;
};

export type NodeReducerOutput = {
  hidden: boolean;
  type: string;
  image?: string;
  size: number;
  color: string;
  label: string;
};

export function computeNodeStyle(input: NodeReducerInput): NodeReducerOutput {
  const { nodeId, attrs, state, lod } = input;
  const neutral = "#94a3b8";
  const hasTypeHighlighting = state.highlightTypes.size > 0;

  const typeMatch =
    !hasTypeHighlighting || isTypeHighlighted(attrs.resolvedType, state.highlightTypes);

  const scoreValue = attrs.score ?? attrs.authority ?? 0;
  const importance = state.maxScore > 0 ? clamp(scoreValue / state.maxScore, 0, 1) : 0;
  const salience = attrs.kind === "module" ? 0.9 : clamp(0.2 + 0.8 * importance ** 0.6, 0, 1);

  let size = attrs.baseSize;

  if (state.scaleByAuthority && attrs.kind !== "module") {
    if (scoreValue > 0 && state.maxScore > 0) {
      size = attrs.baseSize + (scoreValue / state.maxScore) * NODE_SIZE.authorityBonus;
    }
  }

  const sizeScaleLow = lerp(1, 0.6, lod);
  const sizeScaleHigh = lerp(1, 0.9, lod);
  const sizeScale = lerp(sizeScaleLow, sizeScaleHigh, salience);
  const minSize = attrs.kind === "module" ? NODE_SIZE.min.module : NODE_SIZE.min.normal;
  size = Math.max(minSize, size * sizeScale);

  const hasGraphMatches = state.graphMatches.size > 0;
  const semanticMatch = state.semanticMatches.has(nodeId);
  const graphMatch = hasGraphMatches && state.graphMatches.has(nodeId);
  const clusterMatch = state.activeCommunity ? attrs.community === state.activeCommunity : true;
  const isHoverNode = state.hoverNodeID === nodeId;

  let allowed = true;

  if (state.searchActive && state.searchOnlyMatches) {
    allowed = allowed && semanticMatch;
  }

  if (state.graphFilter && hasGraphMatches) {
    allowed = allowed && graphMatch;
  }

  if (state.activeCommunity) {
    allowed = allowed && clusterMatch;
  }

  const filterActive =
    (state.searchActive && state.searchOnlyMatches) ||
    (state.graphFilter && hasGraphMatches) ||
    state.activeCommunity !== null;

  if (filterActive && !allowed) {
    return { hidden: true, type: "circle", size, color: neutral, label: "" };
  }

  const hoverCommunity =
    !state.activeCommunity && !state.graphFilter && !state.searchActive
      ? state.hoverCommunity
      : null;

  const inHoverCommunity = hoverCommunity && attrs.community === hoverCommunity;

  const hasHighlighting =
    (state.searchActive && state.semanticMatches.size > 0) ||
    hasGraphMatches ||
    state.activeCommunity !== null;

  const isPersistentFocus =
    Boolean(attrs.center) ||
    semanticMatch ||
    graphMatch ||
    Boolean(state.activeCommunity && clusterMatch);

  const isFocus = isPersistentFocus || isHoverNode;

  const baseColor = attrs.baseColor || neutral;
  const desat = lod * (1 - salience) * 0.55;
  let mixedColor = mixColors(baseColor, neutral, desat);
  const alphaLow = lerp(NODE_ALPHA.low.normal, NODE_ALPHA.low.zoomed, lod);
  const alphaHigh = lerp(NODE_ALPHA.high.normal, NODE_ALPHA.high.zoomed, lod);
  let alpha = lerp(alphaLow, alphaHigh, salience);

  if (isFocus || inHoverCommunity) {
    mixedColor = baseColor;
    alpha = Math.max(alpha, lerp(NODE_ALPHA.focus.normal, NODE_ALPHA.focus.zoomed, lod));
  }

  let color = colorWithAlpha(mixedColor, alpha);
  let label = attrs.label;

  if (state.showProblems && attrs.problemCount) {
    label = `! ${attrs.problemCount} · ${label}`;
  }

  if (hoverCommunity && !isHoverNode && !inHoverCommunity) {
    const desatAmount = lerp(DESAT.hover.normal, DESAT.hover.zoomed, lod);
    const deemphColor = mixColors(baseColor, neutral, desatAmount);
    const deemphAlpha = lerp(NODE_ALPHA.deemphHover.normal, NODE_ALPHA.deemphHover.zoomed, lod);
    color = colorWithAlpha(deemphColor, Math.min(alpha, deemphAlpha));
    label = "";
  } else if (hasHighlighting && !isFocus) {
    const desatAmount = lerp(DESAT.normal.normal, DESAT.normal.zoomed, lod);
    const deemphColor = mixColors(baseColor, neutral, desatAmount);
    const deemphAlpha = lerp(NODE_ALPHA.deemph.normal, NODE_ALPHA.deemph.zoomed, lod);
    color = colorWithAlpha(deemphColor, Math.min(alpha, deemphAlpha));
    label = "";
  } else {
    const showLabel = isFocus || isHoverNode || lod < LOD.showLabels;

    if (!showLabel) {
      label = "";
    }
  }

  if (hasTypeHighlighting && !typeMatch && !isFocus && !inHoverCommunity) {
    color = colorWithPremultipliedAlpha(neutral, TYPE_HIGHLIGHT_ALPHA.node);
    label = "";
  }

  const dimmedByType = hasTypeHighlighting && !typeMatch && !isFocus && !inHoverCommunity;

  const cameraSizeRatio = Math.sqrt(Math.max(state.cameraRatio, 0.0001));
  const screenSize = size / cameraSizeRatio;

  const iconMinScreenSize = lerp(
    NODE_SIZE.iconMinScreen.normal,
    NODE_SIZE.iconMinScreen.zoomed,
    lod,
  );

  const useImage =
    !dimmedByType &&
    (isPersistentFocus ||
      ((attrs.kind === "note" || attrs.kind === "code") &&
        (lod < LOD.showIcons || salience > LOD.highSalience) &&
        screenSize >= iconMinScreenSize) ||
      (attrs.kind === "module" && screenSize >= iconMinScreenSize + 2));

  return {
    hidden: false,
    type: useImage ? "image" : "circle",
    image: useImage ? attrs.image : undefined,
    size,
    color,
    label,
  };
}

export type EdgeReducerInput = {
  source: string;
  target: string;
  attrs: SigmaEdgeAttrs;
  sourceAttrs: SigmaNodeAttrs;
  targetAttrs: SigmaNodeAttrs;
  state: ReducerState;
  lod: number;
};

export type EdgeReducerOutput = {
  hidden: boolean;
  color: string;
  size: number;
  label: string;
};

export function computeEdgeStyle(input: EdgeReducerInput): EdgeReducerOutput {
  const { source, target, attrs, sourceAttrs, targetAttrs, state, lod } = input;
  const hasTypeHighlighting = state.highlightTypes.size > 0;

  const sourceTypeMatch =
    !hasTypeHighlighting || isTypeHighlighted(sourceAttrs.resolvedType, state.highlightTypes);

  const targetTypeMatch =
    !hasTypeHighlighting || isTypeHighlighted(targetAttrs.resolvedType, state.highlightTypes);

  const hasGraphMatches = state.graphMatches.size > 0;

  const filterActive =
    (state.searchActive && state.searchOnlyMatches) ||
    (state.graphFilter && hasGraphMatches) ||
    state.activeCommunity !== null;

  const sSemantic = state.semanticMatches.has(source);
  const tSemantic = state.semanticMatches.has(target);
  const sGraph = hasGraphMatches && state.graphMatches.has(source);
  const tGraph = hasGraphMatches && state.graphMatches.has(target);
  const sCluster = state.activeCommunity ? sourceAttrs.community === state.activeCommunity : true;
  const tCluster = state.activeCommunity ? targetAttrs.community === state.activeCommunity : true;

  if (filterActive) {
    let sAllowed = true;
    let tAllowed = true;

    if (state.searchActive && state.searchOnlyMatches) {
      sAllowed = sAllowed && sSemantic;
      tAllowed = tAllowed && tSemantic;
    }

    if (state.graphFilter && hasGraphMatches) {
      sAllowed = sAllowed && sGraph;
      tAllowed = tAllowed && tGraph;
    }

    if (state.activeCommunity) {
      sAllowed = sAllowed && sCluster;
      tAllowed = tAllowed && tCluster;
    }

    if (!sAllowed || !tAllowed) {
      return { hidden: true, color: EDGE_COLORS.faint, size: 0.5, label: "" };
    }
  }

  const isHoverEdge =
    state.hoverNodeID !== null && (source === state.hoverNodeID || target === state.hoverNodeID);

  const isFocus =
    sSemantic ||
    tSemantic ||
    sGraph ||
    tGraph ||
    (state.activeCommunity && (sCluster || tCluster)) ||
    isHoverEdge;

  const hasHighlighting =
    (state.searchActive && state.semanticMatches.size > 0) ||
    hasGraphMatches ||
    state.activeCommunity !== null;

  const sizeScale = lerp(1.15, 0.82, lod);
  const size = Math.max(0.35, attrs.size * sizeScale);
  const labelVisible = state.showEdgeLabels && (lod < 0.2 || isFocus);
  let label = labelVisible ? attrs.label : "";

  let color: string;

  if (isFocus || isHoverEdge) {
    color = EDGE_COLORS.strong;
  } else if (hasHighlighting) {
    color = EDGE_COLORS.faint;
  } else {
    color = lod > 0.3 ? EDGE_COLORS.faint : EDGE_COLORS.light;
  }

  if (hasTypeHighlighting && (!sourceTypeMatch || !targetTypeMatch) && !isFocus) {
    color = colorWithPremultipliedAlpha(EDGE_COLORS.faint, TYPE_HIGHLIGHT_ALPHA.edge);
    label = "";
  }

  return {
    hidden: false,
    color,
    size,
    label,
  };
}
