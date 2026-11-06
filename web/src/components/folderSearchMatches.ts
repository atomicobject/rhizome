import type { GraphResponse } from "../api/types";
import type { FolderGraph } from "./folderGraph";

/** Highlight the visible representatives without expanding or moving the folder graph. */
export function folderSearchMatches(
  folders: FolderGraph | null,
  graph: GraphResponse,
  paths: Set<string>,
) {
  const pathsByID = new Map(
    graph.nodes
      .filter((node) => paths.has(node.notePath || node.path || ""))
      .map((node) => [node.id, node.notePath || node.path || node.id]),
  );

  const matches = new Map<string, number>();

  for (const children of folders?.children.values() ?? []) {
    for (const child of children) {
      const count = new Set(
        child.members.flatMap((id) => {
          const path = pathsByID.get(id);

          return path ? [path] : [];
        }),
      ).size;

      if (count) matches.set(child.node.id, count);
    }
  }

  return matches;
}
