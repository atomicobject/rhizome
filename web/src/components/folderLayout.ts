import { folderWeight, initialOpenFolders, type FolderTree } from "./folderTree";
import { visibleFolderGraph, type FolderChild, type FolderGraph } from "./folderGraph";

const extents = new WeakMap<FolderTree, number>();

/** A stable camera frame sized for the overview, rather than every possible expansion. */
export function folderLayoutExtent(tree: FolderTree) {
  const cached = extents.get(tree);

  if (cached !== undefined) return cached;
  const open = initialOpenFolders(tree);

  const overview = positionFolderGraph(
    tree,
    visibleFolderGraph(
      tree,
      {
        open,
        explicit: new Set(),
        showAll: new Set(),
      },
      () => undefined,
    ),
  );

  const extent = Math.max(640, ...overview.map((n) => Math.hypot(n.x ?? 0, n.y ?? 0))) * 2;
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

/** Child fans depend only on their parent's anchor and child list. */
function positionFolderGraph(tree: FolderTree, graph: FolderGraph) {
  const positions = new Map<string, { x: number; y: number }>([["folder:", { x: 0, y: 0 }]]);

  const visit = (path: string, x: number, y: number, direction: number) => {
    const children = orderedChildren(tree, path, graph.children.get(path) ?? []);
    const n = children.length;

    if (!n) return;
    const root = path === "";
    const width = path && !path.includes("/") ? 0.9 : Math.max(0.9, Math.min(2.4, n * 0.22));
    const floor = root ? Math.max(1, Math.sqrt(folderWeight(tree.root)) / 5) : 1;
    const weights = children.map((c) => Math.max(floor, Math.sqrt(folderWeight(c.totals))));
    const total = weights.reduce((a, b) => a + b, 0);

    const radius = root
      ? Math.max(320, 90 / ((Math.min(...weights) / total) * Math.PI * 2))
      : Math.max(160, (90 * (n - 1)) / width);

    let start = -Math.PI;
    children.forEach((child, index) => {
      const wedge = (weights[index] / total) * Math.PI * 2;

      const angle = root
        ? start + wedge / 2
        : direction + (n === 1 ? 0 : (index / (n - 1) - 0.5) * width);

      const point = { x: x + Math.cos(angle) * radius, y: y + Math.sin(angle) * radius };
      positions.set(child.node.id, point);

      if (child.folder) visit(child.folder.path, point.x, point.y, angle);
      start += wedge;
    });
  };

  visit("", 0, 0, 0);

  return graph.nodes.map((node) => ({ ...node, ...positions.get(node.id) }));
}

export function layoutFolderGraph(
  tree: FolderTree,
  graph: FolderGraph,
): FolderGraph & { layoutExtent: number } {
  return {
    ...graph,
    nodes: positionFolderGraph(tree, graph),
    layoutExtent: folderLayoutExtent(tree),
  };
}
