import type { GraphEdge } from "../api/types";
import { CODE_COLOR, type GraphViewNode, UNTYPED_COLOR } from "./graphViewShared";
import {
  filePath,
  folderWeight,
  nodeTotals,
  sumTotals,
  totalsLabel,
  type Folder,
  type FolderTree,
  type FolderTotals,
} from "./folderTree";

export const MINOR_FOLDER_LINK = "folder-minor";

export const CODE_FILL_KEY = "__code__";

export const UNTYPED_FILL_KEY = "__untyped__";

// Worker B owns the corresponding additive GraphViewNode fields.
export type FolderViewNode = GraphViewNode & {
  x?: number;
  y?: number;
  labelDetail?: string;
  aggregate?: boolean;
};

export type FolderChild = {
  node: FolderViewNode;
  totals: FolderTotals;
  members: string[];
  folder?: Folder;
};

export type FolderGraph = {
  nodes: FolderViewNode[];
  edges: GraphEdge[];
  truncated: boolean;
  children: Map<string, FolderChild[]>;
};

export type FolderExpansion = {
  open: ReadonlySet<string>;
  explicit: ReadonlySet<string>;
  showAll: ReadonlySet<string>;
};

function fill(totals: FolderTotals, color: (type: string) => string | undefined) {
  const [best] = [...totals.types].sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]));

  if (best && best[1] >= totals.files && best[1] >= totals.untyped)
    return { fillKey: best[0], fill: color(best[0]) ?? UNTYPED_COLOR };

  return totals.files >= totals.untyped
    ? { fillKey: CODE_FILL_KEY, fill: CODE_COLOR }
    : { fillKey: UNTYPED_FILL_KEY, fill: UNTYPED_COLOR };
}

function folderChild(folder: Folder, color: (type: string) => string | undefined): FolderChild {
  return {
    folder,
    totals: folder,
    members: folder.members,
    node: {
      id: `folder:${folder.path}`,
      kind: "module",
      path: folder.path,
      label: folder.path.split("/").pop() || "Vault",
      labelDetail: totalsLabel(folder),
      childCount: folder.notes + folder.files,
      ...fill(folder, color),
    },
  };
}

export function visibleFolderChildren(
  tree: FolderTree,
  folder: Folder,
  state: FolderExpansion,
  color: (type: string) => string | undefined,
): FolderChild[] {
  const score = (child: FolderChild) =>
    folderWeight(child.totals) +
    child.members.reduce((sum, id) => sum + (tree.linkCounts.get(id)?.noteCode ?? 0), 0);

  const folders = folder.folders
    .map((f) => folderChild(f, color))
    .sort((a, b) => score(b) - score(a) || a.node.id.localeCompare(b.node.id));

  const files: FolderChild[] = folder.direct.map((node) => ({
    node: {
      ...node,
      label: filePath(node).split("/").pop() || node.label,
      labelDetail: totalsLabel(nodeTotals(node)),
    },
    totals: nodeTotals(node),
    members: [node.id],
  }));

  files.sort((a, b) => {
    const x = tree.linkCounts.get(a.node.id),
      y = tree.linkCounts.get(b.node.id);

    return (
      (y?.noteCode ?? 0) - (x?.noteCode ?? 0) ||
      (y?.other ?? 0) - (x?.other ?? 0) ||
      a.node.id.localeCompare(b.node.id)
    );
  });

  const showFiles =
    folder.path === "" || state.explicit.has(folder.path) || (!folders.length && files.length <= 8);

  const budget = showFiles && state.explicit.has(folder.path) ? 14 : 10;
  const fileSlots = showFiles ? Math.min(files.length, folders.length ? 8 : budget - 1) : 0;

  const candidates: FolderChild[] =
    folders.length + files.length <= budget
      ? [...folders, ...(showFiles ? files : [])]
      : [...folders.slice(0, budget - fileSlots - 1), ...files.slice(0, fileSlots)];

  const allCount = folders.length + files.length;

  const kept = state.showAll.has(folder.path)
    ? [...folders, ...files]
    : folder.path === ""
      ? [...folders]
      : candidates.slice(0, allCount > budget ? budget - 1 : budget);

  const keptIds = new Set(kept.map((c) => c.node.id));
  const hidden = [...folders, ...files].filter((c) => !keptIds.has(c.node.id));

  if (hidden.length) {
    const totals = sumTotals(hidden.map((c) => c.totals));
    const hiddenFolders = hidden.filter((c) => c.folder).length;

    const hiddenNotes = hidden.filter((c) => !c.folder && c.node.kind === "note").length;
    const hiddenFiles = hidden.length - hiddenFolders - hiddenNotes;
    const noun = (n: number, word: string) => `${n} ${word}${n === 1 ? "" : "s"}`;

    const label = [
      hiddenFolders ? noun(hiddenFolders, "folder") : "",
      hiddenNotes ? noun(hiddenNotes, "note") : "",
      hiddenFiles ? noun(hiddenFiles, "file") : "",
    ]
      .filter(Boolean)
      .join(" · ");

    kept.push({
      node: {
        id: `more:${folder.path}`,
        path: folder.path,
        kind: "module",
        label: `+${label}`,
        labelDetail: hiddenFolders ? `total ${totalsLabel(totals)}` : undefined,
        aggregate: true,
        childCount: totals.notes + totals.files,
        ...fill(totals, color),
      },
      totals,
      members: hidden.flatMap((c) => c.members),
    });
  }

  return kept;
}

export function visibleFolderGraph(
  tree: FolderTree,
  state: FolderExpansion,
  color: (type: string) => string | undefined,
  focusedPath?: string,
): FolderGraph {
  const root = folderChild(tree.root, color);
  const nodes = [root.node];
  const edges: GraphEdge[] = [];
  const children = new Map<string, FolderChild[]>();
  const representatives = new Map<string, string>();

  const visit = (child: FolderChild) => {
    for (const id of child.members) representatives.set(id, child.node.id);

    if (!child.folder || !state.open.has(child.folder.path)) return;
    const list = visibleFolderChildren(tree, child.folder, state, color);
    children.set(child.folder.path, list);

    for (const next of list) {
      nodes.push(next.node);
      edges.push({
        source: child.node.id,
        target: next.node.id,
        kind: "folder-containment",
        weight: next.members.length,
        relationLabel: totalsLabel(next.totals),
      });
      visit(next);
    }
  };

  visit(root);
  const aggregated = new Map<string, GraphEdge>();

  const endpoint = (id: string) => {
    const file = tree.files.get(id);

    if (!file) return representatives.get(id);
    const path = filePath(file);

    if (focusedPath && path.startsWith(`${focusedPath}/`)) return representatives.get(id);
    const top = path.includes("/") ? `folder:${path.split("/")[0]}` : "more:";

    return nodes.some((node) => node.id === top) ? top : representatives.get(id);
  };

  for (const link of tree.links) {
    let a = endpoint(link.source),
      b = endpoint(link.target);

    if (a === b) {
      a = representatives.get(link.source);
      b = representatives.get(link.target);
    }

    if (!a || !b || a === b) continue;
    const [source, target] = [a, b].sort();

    for (const [kind, count] of [
      ["folder-crosslink", link.noteCode],
      [MINOR_FOLDER_LINK, link.other],
    ] as const) {
      if (!count) continue;
      const key = JSON.stringify([source, target, kind]);
      const edge = aggregated.get(key) ?? { source, target, kind, weight: 0 };
      edge.weight = (edge.weight ?? 0) + count;
      edge.relationLabel = `${edge.weight.toLocaleString("en-US")} ${kind === "folder-crosslink" ? "note–code links" : "other links"}`;
      aggregated.set(key, edge);
    }
  }

  if (focusedPath) {
    const local = new Set((children.get(focusedPath) ?? []).map((child) => child.node.id));
    const bundles = new Map<string, GraphEdge>();

    for (const edge of aggregated.values()) {
      if (edge.kind !== "folder-crosslink") continue;

      const child = local.has(edge.source)
        ? edge.source
        : local.has(edge.target)
          ? edge.target
          : undefined;

      const destination = child === edge.source ? edge.target : edge.source;

      if (!child || local.has(destination) || destination === `folder:${focusedPath}`) continue;
      edge.kind = "folder-child-crosslink";
      const node = nodes.find((node) => node.id === child);
      const target = nodes.find((node) => node.id === destination);

      if (node && target)
        node.labelDetail = `${node.labelDetail || ""}  → ${target.label} · ${edge.weight}`.trim();

      const bundle = bundles.get(destination) ?? {
        source: `folder:${focusedPath}`,
        target: destination,
        kind: "folder-crosslink",
        weight: 0,
      };

      bundle.weight = (bundle.weight ?? 0) + (edge.weight ?? 1);
      bundle.relationLabel = `${bundle.weight} note–code links`;
      bundles.set(destination, bundle);
    }

    for (const [destination, edge] of bundles) {
      aggregated.set(`bundle:${destination}`, edge);
      const opened = nodes.find((node) => node.id === `folder:${focusedPath}`);
      const target = nodes.find((node) => node.id === destination);

      if (opened && target)
        opened.labelDetail =
          `${opened.labelDetail || ""}  → ${target.label} · ${edge.weight}`.trim();
    }
  }

  const overflows = nodes.filter((node) => node.aggregate);

  if (overflows.length > 1)
    for (const node of overflows) node.label += ` in ${node.path?.split("/").pop() || "Vault"}`;

  const names = new Map(nodes.map((node) => [node.id, node.label.toLowerCase()]));

  for (const node of nodes) {
    if (node.aggregate) continue;

    const duplicates = nodes.filter(
      (other) => !other.aggregate && names.get(other.id) === names.get(node.id),
    );

    if (duplicates.length > 1 && node.path) {
      const parent = node.path.split("/").slice(0, -1).join("/");
      node.labelDetail = `${parent}/  ${node.labelDetail || ""}`;
    }
  }

  return { nodes, edges: [...edges, ...aggregated.values()], truncated: tree.truncated, children };
}
