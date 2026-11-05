import { folderWeight, type Folder, type FolderTree } from "./folderTree";
import type { FolderChild, FolderGraph } from "./folderGraph";

const extents = new WeakMap<FolderTree, number>();

/** Fully expanded child lists give the narrowest wedges and bound every budgeted layout. */
export function folderLayoutExtent(tree: FolderTree) {
  const cached = extents.get(tree);

  if (cached !== undefined) return cached;
  let extent = 1000;

  const visit = (folder: Folder, span: number, radius: number) => {
    const weights = [
      ...folder.folders.map((child) => Math.max(1, Math.sqrt(folderWeight(child)))),
      ...folder.direct.map((node) => (node.kind === "note" ? Math.sqrt(3) : 1)),
    ];

    if (!weights.length) return;
    const total = weights.reduce((a, b) => a + b, 0);

    const nextRadius = Math.max(
      radius + 140,
      weights.length > 1 ? 90 / ((Math.min(...weights) / total) * span) : 0,
    );

    extent = Math.max(extent, nextRadius + 300);
    folder.folders.forEach((child, index) =>
      visit(child, (span * weights[index]) / total, nextRadius),
    );
  };

  visit(tree.root, Math.PI * 2, 0);
  extents.set(tree, extent);

  return extent;
}

function orderedChildren(tree: FolderTree, path: string, children: FolderChild[]) {
  const remaining = [...children].sort(
    (a, b) => folderWeight(b.totals) - folderWeight(a.totals) || a.node.id.localeCompare(b.node.id),
  );

  const result: FolderChild[] = [];
  const affinity = tree.affinity.get(path);

  while (remaining.length) {
    const last = result.at(-1);

    if (last)
      remaining.sort(
        (a, b) =>
          (affinity?.get(JSON.stringify([last.node.id, b.node.id].sort())) ?? 0) -
            (affinity?.get(JSON.stringify([last.node.id, a.node.id].sort())) ?? 0) ||
          a.node.id.localeCompare(b.node.id),
      );
    const next = remaining.shift();

    if (next) result.push(next);
  }

  return result;
}

/** Each parent's wedge depends only on its own children, never their open state. */
export function layoutFolderGraph(
  tree: FolderTree,
  graph: FolderGraph,
): FolderGraph & { layoutExtent: number } {
  const positions = new Map<string, { x: number; y: number }>([["folder:", { x: 0, y: 0 }]]);

  const visit = (path: string, start: number, span: number, radius: number) => {
    const children = orderedChildren(tree, path, graph.children.get(path) ?? []);

    if (!children.length) return;
    const weights = children.map((c) => Math.max(1, Math.sqrt(folderWeight(c.totals))));
    const total = weights.reduce((a, b) => a + b, 0);
    const minSpan = (Math.min(...weights) / total) * span;
    const nextRadius = Math.max(radius + 140, children.length > 1 ? 90 / minSpan : 0);
    let angle = start;
    children.forEach((child, index) => {
      const wedge = (weights[index] / total) * span;
      const middle = angle + wedge / 2;
      positions.set(child.node.id, {
        x: Math.cos(middle) * nextRadius,
        y: Math.sin(middle) * nextRadius,
      });

      if (child.folder) visit(child.folder.path, angle, wedge, nextRadius);
      angle += wedge;
    });
  };

  visit("", -Math.PI, Math.PI * 2, 0);

  return {
    ...graph,
    nodes: graph.nodes.map((node) => ({ ...node, ...positions.get(node.id) })),
    layoutExtent: folderLayoutExtent(tree),
  };
}
