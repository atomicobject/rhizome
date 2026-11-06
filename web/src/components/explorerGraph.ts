import type Graph from "graphology";
import { resolveCssColor } from "../lib/themeColors";
import { useEffect, useMemo, useRef, useState, type Dispatch, type SetStateAction } from "react";
import { useQuery } from "@tanstack/react-query";
import { getFileView } from "../api/client";
import { queryKeys } from "../api/queryKeys";
import { fileFocusGraph, mergeFileNotes } from "./fileFocusGraph";
import type { GraphNode } from "../api/types";
import type { FolderGraph } from "./folderGraph";
import type { GraphResponse } from "../api/types";
import type { SigmaNodeAttrs } from "../lib/graphStyle";
import type { GraphViewNode } from "./graphViewShared";

export type ExplorerNodeAttrs = SigmaNodeAttrs & {
  collapsed?: boolean;
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
  }));
}

export { fileFocusGraph } from "./fileFocusGraph";

export function useRevealRail(
  initial: Set<string>,
  atRoot: boolean,
  path: string,
  setExpanded: Dispatch<SetStateAction<Set<string>>>,
) {
  const seeded = useRef(false);
  useEffect(() => {
    if (seeded.current || ![...initial].some(Boolean)) return;
    seeded.current = true;
    setExpanded((previous) => new Set([...previous, ...[...initial].filter(Boolean)]));
  }, [initial, setExpanded]);
  useEffect(() => {
    if (atRoot) return;

    const ancestors = path
      .split("/")
      .slice(0, -1)
      .map((_, index, parts) => parts.slice(0, index + 1).join("/"));

    setExpanded((previous) => new Set([...previous, ...ancestors]));
  }, [atRoot, path, setExpanded]);
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
  searchPaths: ReadonlySet<string> = new Set(),
) {
  const path = selection.kind === "file" ? selection.path : "";

  const { data: file } = useQuery({
    queryKey: queryKeys.files.detail(path),
    queryFn: ({ signal }) => getFileView(path, { signal }),
    enabled: Boolean(path),
  });

  const [revealed, setRevealed] = useState<{ path: string; groups: Set<string> }>({
    path: "",
    groups: new Set(),
  });

  const sourceGraph = useMemo(() => mergeFileNotes(graph, file), [graph, file]);

  const drawn = useMemo<Omit<GraphResponse, "nodes"> & { nodes: GraphViewNode[] }>(
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
          ? fileFocusGraph(
              sourceGraph,
              selection.path,
              file,
              revealed.path === path ? revealed.groups : new Set(),
              searchPaths,
            )
          : graph,
    [folders, graph, selection, sourceGraph, file, revealed, path, searchPaths],
  );

  const reveal = (node: GraphNode) => {
    if (!node.id.startsWith("file-more:")) return false;
    setRevealed((previous) => ({
      path,
      groups: new Set([
        ...(previous.path === path ? previous.groups : []),
        node.id.slice("file-more:".length),
      ]),
    }));

    return true;
  };

  return { ...drawn, sourceGraph, reveal };
}

export function drawTestEdges(
  sigma: {
    getGraph: () => Graph;
    getNodeDisplayData: (
      id: string,
    ) => { x: number; y: number; hidden?: boolean } | null | undefined;
    framedGraphToViewport: (point: { x: number; y: number }) => { x: number; y: number };
  },
  context: Pick<
    CanvasRenderingContext2D,
    "strokeStyle" | "lineWidth" | "setLineDash" | "beginPath" | "moveTo" | "lineTo" | "stroke"
  >,
  color: string,
) {
  const graph = sigma.getGraph();

  const point = (id: string) => {
    const node = sigma.getNodeDisplayData(id);

    return node && !node.hidden ? sigma.framedGraphToViewport(node) : null;
  };

  const line = (x1: number, y1: number, x2: number, y2: number) => {
    context.moveTo(x1, y1);
    context.lineTo(x2, y2);
  };

  graph.forEachNode((id) => {
    if (!id.startsWith("file-group:")) return;
    const heading = point(id);

    if (!heading) return;
    const items: { x: number; y: number }[] = [];
    const focused: { x: number; y: number }[] = [];
    let kind = "";
    graph.forEachEdge(id, (_edge, attrs, source, target) => {
      kind = attrs.kind;

      const other = source === id ? target : source;
      const position = point(other);

      if (!position) return;

      if (other.startsWith("file-more:")) return;

      if (graph.getNodeAttribute(other, "selected")) focused.push(position);
      else items.push(position);
    });
    context.strokeStyle =
      kind === "explorer-documents"
        ? resolveCssColor("var(--explorer-link-strong)")
        : kind === "explorer-code"
          ? resolveCssColor("var(--explorer-code)")
          : kind === "explorer-tests"
            ? resolveCssColor("var(--type-8)")
            : color;
    context.lineWidth = 1.5;
    context.setLineDash(kind === "explorer-tests" ? [4, 3] : []);
    context.beginPath();

    for (const focus of focused) line(focus.x, focus.y, heading.x, heading.y);
    const columns = new Map<number, { x: number; y: number }[]>();

    for (const item of items) {
      const x = Math.round(item.x);
      const column = columns.get(x) ?? [];
      column.push(item);
      columns.set(x, column);
    }

    for (const [x, column] of columns) {
      const trunkX = x + (x < heading.x ? 12 : -12);
      line(heading.x, heading.y, trunkX, heading.y);
      line(trunkX, heading.y, trunkX, Math.max(...column.map((item) => item.y)));

      for (const item of column) line(trunkX, item.y, item.x, item.y);
    }

    context.stroke();
  });
  context.setLineDash([]);
}
