import { useEffect, useMemo, useState, type Dispatch, type SetStateAction } from "react";
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

export { fileFocusGraph } from "./fileFocusGraph";

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

  const drawn = useMemo(
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
            )
          : graph,
    [folders, graph, selection, sourceGraph, file, revealed, path],
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
