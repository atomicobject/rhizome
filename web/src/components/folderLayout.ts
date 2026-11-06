import { folderWeight, initialOpenFolders, type FolderTree } from "./folderTree";
import {
  visibleFolderChildren,
  visibleFolderGraph,
  type FolderChild,
  type FolderGraph,
} from "./folderGraph";

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
        revealed: new Map(),
      },
      () => undefined,
    ),
  );

  const extent = Math.max(640, ...overview.map((n) => Math.hypot(n.x ?? 0, n.y ?? 0))) * 2;
  extents.set(tree, extent);

  return extent;
}

function orderedChildren(tree: FolderTree, path: string, children: FolderChild[]) {
  if (path)
    return [...children].sort(
      (a, b) =>
        Number(Boolean(a.node.aggregate)) - Number(Boolean(b.node.aggregate)) ||
        a.node.label.localeCompare(b.node.label),
    );

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
    // Root anchors always use the overview; revealed files fan out from its overflow.
    const children = orderedChildren(
      tree,
      path,
      path === ""
        ? visibleFolderChildren(
            tree,
            tree.root,
            {
              open: new Set([""]),
              explicit: new Set(),
              revealed: new Map(),
            },
            () => undefined,
          )
        : (graph.children.get(path) ?? []),
    );

    const n = children.length;

    if (!n) return;
    const root = path === "";
    const width = Math.max(0.9, Math.min(1.6, n * 0.22));

    const dominant = root
      ? children.filter((c) => c.folder && initialOpenFolders(tree).has(c.folder.path))
      : [];

    const others = children.filter((c) => !dominant.includes(c));
    const radius = root ? 600 : Math.max(220, (150 * (n - 1)) / width);

    let start = -Math.PI;
    children.forEach((child, index) => {
      const major = dominant.indexOf(child);
      const minor = others.indexOf(child);
      const wedge = (Math.PI * 2) / Math.max(1, n);

      const angle = root
        ? dominant.length === 2
          ? major >= 0
            ? Math.PI - major * Math.PI
            : (minor < others.length / 2 ? -Math.PI / 2 : Math.PI / 2) +
              ((minor % Math.ceil(others.length / 2)) /
                Math.max(1, Math.ceil(others.length / 2) - 1) -
                0.5) *
                2.4
          : start + wedge / 2
        : direction + (n === 1 ? 0 : (index / (n - 1) - 0.5) * width);

      const distance = root && dominant.length === 2 && major < 0 ? 800 : radius;
      const local = angle - direction;
      const depth = path.split("/").length;
      const axial = Math.cos(local) * distance * (depth > 2 ? 3.5 : depth > 1 ? 1.3 : 0.65);
      const lateral = Math.sin(local) * distance * 1.4;

      const point = root
        ? {
            x: x + Math.cos(angle) * distance * (major < 0 && dominant.length === 2 ? 0.65 : 1),
            y: y + Math.sin(angle) * distance,
          }
        : {
            x: x + Math.cos(direction) * axial - Math.sin(direction) * lateral,
            y: y + Math.sin(direction) * axial + Math.cos(direction) * lateral,
          };

      positions.set(child.node.id, point);

      if (child.folder) visit(child.folder.path, point.x, point.y, root ? angle : direction);
      start += wedge;
    });
  };

  visit("", 0, 0, 0);

  const overflow = positions.get("more:");

  if (overflow) {
    const files = (graph.children.get("") ?? []).filter((child) => !positions.has(child.node.id));
    files.forEach((child, index) => {
      const angle = index * Math.PI * (3 - Math.sqrt(5));
      const radius = 65 * Math.sqrt(index + 1);
      positions.set(child.node.id, {
        x: overflow.x + Math.cos(angle) * radius,
        y: overflow.y + Math.sin(angle) * radius,
      });
    });
  }

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
