import type { FolderOverview, GraphResponse } from "../api/types";
import { buildFolderTree as buildOverviewTree, filePath } from "../components/folderTree";

// Reference projection retained only in tests to compare the server projection
// against the original browser's ownership, ordering, and pair-weight rules.
export function folderOverview(graph: GraphResponse): FolderOverview {
  const files = new Map(
    graph.nodes.filter((n) => ["note", "code", "module"].includes(n.kind)).map((n) => [n.id, n]),
  );

  const notesByPath = new Map(
    graph.nodes.filter((n) => n.kind === "note").map((n) => [filePath(n), n.id]),
  );

  const owners = new Map(
    graph.nodes.map((n) => [
      n.id,
      n.kind === "embedded" ? notesByPath.get(filePath(n)) : files.has(n.id) ? n.id : undefined,
    ]),
  );

  const pairs = new Map<string, FolderOverview["links"][number]>();

  for (const edge of graph.edges) {
    if (edge.kind === "embeds") continue;

    const a = owners.get(edge.source),
      b = owners.get(edge.target);

    if (!a || !b || a === b) continue;
    const [source, target] = [a, b].sort();
    const key = JSON.stringify([source, target]);
    const pair = pairs.get(key) ?? { source, target, noteCode: 0, other: 0 };
    const category = edge.kind === "mentions" || edge.kind === "coderef" ? "noteCode" : "other";
    // A file pair counts once in each category, even when embedded nodes repeat it.
    pair[category] = Math.max(pair[category], edge.weight ?? 1);
    pairs.set(key, pair);
  }

  const links = [...pairs.values()];

  const types = new Map<string, number>();

  for (const n of graph.nodes)
    if (!files.has(n.id))
      types.set(n.resolvedType || "", (types.get(n.resolvedType || "") || 0) + 1);

  return {
    files: [...files.values()],
    links,
    nestedTypes: [...types]
      .sort(([a], [b]) => a.localeCompare(b))
      .map(([resolvedType, count]) => ({ resolvedType, count })),
    truncated: graph.truncated ?? false,
    needsIndex: graph.needsIndex,
  };
}

export const buildFolderTree = (graph: GraphResponse) => buildOverviewTree(folderOverview(graph));
