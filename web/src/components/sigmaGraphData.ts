import { onBranch } from "./explorerGraph";
import type { ExplorerNodeAttrs as SigmaNodeAttrs } from "./explorerGraph";
import { type LayoutInput, type LayoutPositions, runLayout } from "./graphLayout";
import Graph from "graphology";
import type { GraphEdge } from "../api/types";
import { graphNodeIdentityAttrs, type SigmaEdgeAttrs } from "../lib/graphStyle";
import { resolveCssColor } from "../lib/themeColors";
import {
  CODE_COLOR,
  NODE_SIZE,
  MIXED_EDGE_LABEL,
  edgeKindLabel,
  nodeColor,
  nodeIcon,
  type GraphScope,
  type GraphViewNode,
} from "./graphViewShared";

const LANDMARKS = 20;

function nodeBaseSize(node: GraphViewNode) {
  const center = node.center ? 2 : 0;

  if (node.kind !== "module") return NODE_SIZE.base.normal + center;

  if (!node.childCount) return NODE_SIZE.base.module + center;

  return Math.min(28, 4 + Math.sqrt(node.childCount) * 0.55) + center;
}

export function buildSigmaGraph({
  nodes,
  edges,
  scope,
  connectionCounts,
  globalLayout,
  communityColors,
  typeColors,
  problemCounts,
  edgeFaint,
  fixed,
  explorer,
}: {
  nodes: GraphViewNode[];
  edges: GraphEdge[];
  scope: GraphScope;
  connectionCounts: Map<string, number>;
  globalLayout: Map<string, { x: number; y: number }>;
  communityColors: Map<string, string>;
  typeColors?: Map<string, string>;
  problemCounts: ReadonlyMap<string, number>;
  edgeFaint: string;
  fixed: boolean;
  explorer: boolean;
}) {
  const graph: Graph<SigmaNodeAttrs, SigmaEdgeAttrs> = new Graph({ multi: true });

  // A folder that summarizes its contents ranks by what it holds, and every
  // one is named; anything else ranks by its links.
  const rank = (node: GraphViewNode) =>
    node.kind === "module" && node.childCount
      ? node.childCount
      : (connectionCounts.get(node.id) ?? 0);

  const landmarks = new Set(
    nodes
      .filter((node) => node.kind !== "code")
      .sort((a, b) => rank(b) - rank(a) || a.id.localeCompare(b.id))
      .filter((node, index) => rank(node) > 0 && (index < LANDMARKS || Boolean(node.fill)))
      .map((node) => node.id),
  );

  const total = Math.max(1, nodes.length);
  const radius = Math.max(80, Math.sqrt(total) * 28);

  const hasCompleteGlobalLayout =
    scope.mode === "global" && nodes.length > 0 && nodes.every((node) => globalLayout.has(node.id));

  const selected = explorer ? nodes.find((node) => node.center) : undefined;
  const branch = selected?.kind === "module" ? selected.path : undefined;
  nodes.forEach((n, index) => {
    const cached = scope.mode === "global" ? globalLayout.get(n.id) : undefined;

    const angle =
      explorer && scope.mode === "local"
        ? (n.kind === "note" ? Math.PI / 2 : -Math.PI / 2) + (index / total) * Math.PI
        : (index / total) * Math.PI * 2;

    const jitter = (Math.random() - 0.5) * 12;

    const x =
      fixed && n.x !== undefined ? n.x : cached ? cached.x : Math.cos(angle) * radius + jitter;

    const y =
      fixed && n.y !== undefined ? n.y : cached ? cached.y : Math.sin(angle) * radius + jitter;

    const center = Boolean(
      n.center ||
      (explorer &&
        scope.mode === "local" &&
        (n.path === scope.label || n.notePath === scope.label)),
    );

    const baseSize = fixed
      ? Math.min(12, nodeBaseSize({ ...n, center }) / 2)
      : nodeBaseSize({ ...n, center });

    const colorValue =
      (explorer && n.kind === "code" ? CODE_COLOR : n.fill) ??
      nodeColor(n, communityColors, typeColors);

    const baseColor = resolveCssColor(colorValue);
    graph.addNode(n.id, {
      label:
        explorer && scope.mode === "local" && n.x === undefined
          ? (n.path || n.label).split("/").pop() || n.label
          : n.label,
      labelDetail: n.labelDetail,
      selected: explorer && center,
      branch:
        Boolean(branch) &&
        (branch === "" || (n.path || "").startsWith(`${branch}/`) || n.path === branch),
      muted: branch ? !onBranch(n.path || "", branch) : false,
      aggregate: n.aggregate,
      fixed,
      priority:
        !explorer && !fixed
          ? 0
          : center
            ? 1e9
            : n.kind === "module"
              ? 1e8
              : (scope.mode === "local" && n.kind === "note" ? 1e6 : 0) +
                (connectionCounts.get(n.id) ?? 0),
      kind: n.kind,
      center,
      explorerLabels: explorer || fixed,
      ...graphNodeIdentityAttrs(n),
      module: n.module,
      lang: n.lang,
      community: n.community,
      resolvedType: n.resolvedType,
      childCount: n.childCount,
      authority: n.authority,
      hub: n.hub,
      pageRank: n.pageRank,
      score: n.score,
      problemCount: problemCounts.get(n.id) ?? 0,
      landmark: explorer || fixed ? true : landmarks.has(n.id),
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
    const key = `${a}|${b}|${e.kind.startsWith("folder-") || e.kind.startsWith("explorer-") ? e.kind : "links"}`;
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
      color: edgeFaint,
      baseColor: edgeFaint,
      kind: e.kind,
      label: e.label || edgeKindLabel(e.kind),
    });
  });

  return { graph, hasCompleteGlobalLayout };
}

export function layoutSigmaGraph(
  graph: Graph<SigmaNodeAttrs, SigmaEdgeAttrs>,
  nodes: GraphViewNode[],
  scope: GraphScope,
  globalLayout: Map<string, { x: number; y: number }>,
  install: (positions: LayoutPositions) => void,
) {
  const isWarmGlobal = scope.mode === "global" && globalLayout.size > 0;

  // A small graph settles fully for a few hundred passes at no visible cost.
  const iterations = isWarmGlobal
    ? 28
    : graph.order > 400
      ? 50
      : graph.order > 200
        ? 80
        : graph.order > 80
          ? 120
          : 600;

  const layoutNodes: LayoutInput["nodes"] = [];
  graph.forEachNode((id, attrs) =>
    layoutNodes.push({ id, x: attrs.x, y: attrs.y, size: attrs.baseSize }),
  );
  const layoutEdges: LayoutInput["edges"] = [];
  // Hidden minor folder links do not pull their folders together.
  graph.forEachEdge(
    (_edge, attrs, source, target) =>
      attrs.kind !== "folder-minor" &&
      layoutEdges.push({ source, target, weight: attrs.weight ?? 1 }),
  );

  const radial = nodes.length > 0 && nodes.every((node) => node.kind === "module" && node.fill);

  return runLayout({ nodes: layoutNodes, edges: layoutEdges, iterations, radial }, install);
}
