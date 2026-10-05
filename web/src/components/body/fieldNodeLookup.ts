import type { WorkspaceFieldNode } from "../../api/types";
import type { BodyRenderContext } from "./registry";

export function findFieldNodeForBodyNode(
  nodes: BodyRenderContext["workspace"]["nodes"],
  name: string | undefined,
  ownerNodeID: string,
): WorkspaceFieldNode | undefined {
  if (!nodes || !name || !ownerNodeID) return undefined;

  for (const candidate of nodes) {
    if (
      candidate.kind === "field" &&
      candidate.field.name === name &&
      candidate.parentId === ownerNodeID
    ) {
      return candidate;
    }
  }

  return undefined;
}
