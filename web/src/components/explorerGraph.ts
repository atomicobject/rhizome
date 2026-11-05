import { useEffect, useMemo, type Dispatch, type SetStateAction } from "react";
import type { FolderGraph } from "./folderGraph";
import type { GraphResponse } from "../api/types";
import type { SigmaNodeAttrs } from "../lib/graphStyle";
import type { GraphViewNode } from "./graphViewShared";

export type ExplorerNodeAttrs = SigmaNodeAttrs & {
  selected?: boolean;
  branch?: boolean;
  muted?: boolean;
};

export function onBranch(path: string, branch: string) {
  return !path || path === branch || path.startsWith(`${branch}/`) || branch.startsWith(`${path}/`);
}

export function focusFolderNodes(nodes: GraphViewNode[], path: string): GraphViewNode[] {
  return nodes.map((node) => ({
    ...node,
    center: node.id === `folder:${path}`,
    labelDetail:
      node.id === "folder:" && path
        ? undefined
        : !path ||
            node.id === `folder:${path}` ||
            (node.aggregate && node.path === path) ||
            (node.path || "").split("/").slice(0, -1).join("/") === path
          ? node.labelDetail
          : undefined,
  }));
}

const middle = (label: string) =>
  label.length > 28 ? `${label.slice(0, 14)}…${label.slice(-13)}` : label;

/** Deterministic relation columns leave every label beside its own node. */
export function fileFocusGraph(graph: GraphResponse, path: string) {
  const focused = graph.nodes.find((node) => node.path === path || node.notePath === path);
  const rest = graph.nodes.filter((node) => node !== focused);
  const notes = rest.filter((node) => node.kind === "note" || node.kind === "embedded");

  const code = rest
    .filter((node) => !notes.includes(node))
    .sort(
      (a, b) =>
        Number((a.path || a.label).includes("test")) -
          Number((b.path || b.label).includes("test")) || a.label.localeCompare(b.label),
    );

  const nodes: GraphViewNode[] = [];

  for (const [group, x] of [
    [notes, -140],
    [code, 140],
  ] as const) {
    group.forEach((node, index) =>
      nodes.push({
        ...node,
        label: middle(
          node.kind === "code"
            ? (node.path || node.label).split("/").pop() || node.label
            : node.label,
        ),
        x,
        y: ((group.length - 1) / 2 - index) * (group === notes ? 180 : 60),
      }),
    );
  }

  if (focused)
    nodes.push({
      ...focused,
      center: true,
      label: path.split("/").pop() || focused.label,
      x: -140,
      y: Math.max(360, code.length * 30 + 120),
    });

  return {
    ...graph,
    nodes,
    edges: graph.edges.map((edge) => ({
      ...edge,
      kind: [edge.source, edge.target].some((id) => notes.some((node) => node.id === id))
        ? "explorer-documents"
        : [edge.source, edge.target].some((id) =>
              code.some((node) => node.id === id && (node.path || node.label).includes("test")),
            )
          ? "explorer-tests"
          : "explorer-code",
    })),
  };
}

export function useRevealRail(
  initial: Set<string>,
  atRoot: boolean,
  path: string,
  setExpanded: Dispatch<SetStateAction<Set<string>>>,
) {
  useEffect(() => {
    setExpanded(
      atRoot
        ? new Set([...initial].filter(Boolean))
        : new Set(
            path
              .split("/")
              .slice(0, -1)
              .map((_, index, parts) => parts.slice(0, index + 1).join("/")),
          ),
    );
  }, [initial, atRoot, path, setExpanded]);
  useEffect(() => {
    if (atRoot) return;

    const reveal = () =>
      document
        .querySelector('.explorer-rail .tree-row[aria-current="page"]')
        ?.scrollIntoView?.({ block: "nearest" });

    const observer = new MutationObserver(reveal);
    observer.observe(document.body, { childList: true, subtree: true });
    reveal();

    return () => observer.disconnect();
  }, [atRoot, path]);
}

export function useExplorerGraph(
  folders: FolderGraph | null,
  graph: GraphResponse,
  selection: { kind: "root" } | { kind: "module" | "file"; path: string },
) {
  return useMemo(
    () =>
      folders
        ? {
            ...folders,
            nodes: focusFolderNodes(
              folders.nodes,
              selection.kind === "module" ? selection.path : "",
            ),
          }
        : selection.kind === "file"
          ? fileFocusGraph(graph, selection.path)
          : graph,
    [folders, graph, selection],
  );
}

export function drawTestEdges(
  sigma: import("sigma").default<ExplorerNodeAttrs, import("../lib/graphStyle").SigmaEdgeAttrs>,
  context: CanvasRenderingContext2D,
  color: string,
) {
  context.strokeStyle = color;
  context.lineWidth = 1.5;
  context.setLineDash([4, 3]);
  sigma.getGraph().forEachEdge((_id, attrs, source, target) => {
    if (attrs.kind !== "explorer-tests") return;
    const a = sigma.getNodeDisplayData(source);
    const b = sigma.getNodeDisplayData(target);

    if (!a || !b || a.hidden || b.hidden) return;
    const start = sigma.framedGraphToViewport(a);
    const end = sigma.framedGraphToViewport(b);
    context.beginPath();
    context.moveTo(start.x, start.y);
    context.lineTo(end.x, end.y);
    context.stroke();
  });
  context.setLineDash([]);
}
