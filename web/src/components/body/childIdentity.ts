import { publicTypeName } from "../../lib/typeNames";
import type {
  NodeWorkspaceGraph,
  StructuralNode,
  WorkspaceContentNode,
  WorkspaceFieldNode,
  WorkspaceNode,
} from "../../api/types";
import { isContentNode } from "../../api/types";

/**
 * Identity-bearing facts for rendering a compact row / link / badge for a
 * child workspace node. Pulled from the child's uniform `data` payload plus
 * its field nodes (id, status, summary).
 */
export type ChildIdentity = {
  title: string;
  typeName: string;
  id: string;
  status: string;
  summary: string;
};

/**
 * Resolve identity facts for a workspace child node.
 *
 * Collection rows need identifier/summary/status at a glance, but workspace
 * field nodes are only emitted for the focused node — descendants rely on
 * the typed-section binding's `properties` map (populated server-side from
 * the child's inline `key:: value` spans). We read both paths so either
 * source satisfies the row, and give the schema-selected identifier field
 * precedence over the generic `id` fallback.
 */
export function extractChildIdentity(
  child: WorkspaceNode,
  workspace: NodeWorkspaceGraph,
): ChildIdentity {
  const base = titleAndType(child);

  const identifierField = isContentNode(child)
    ? child.data.binding?.identifierField?.toLowerCase()
    : undefined;

  let preferredID = "";
  let genericID = "";
  let status = "";
  let summary = "";

  // Primary source: field nodes parented on this child (focused-node case).
  const fieldNodes = (workspace.nodes || []).filter(
    (node): node is WorkspaceFieldNode => node.kind === "field" && node.parentId === child.id,
  );

  for (const fieldNode of fieldNodes) {
    const name = (fieldNode.field.name || "").toLowerCase();
    const value = (fieldNode.field.values || [])[0];

    if (!value) continue;

    if (name === identifierField) preferredID = preferredID || value;
    else if (name === "id") genericID = genericID || value;
    else if (name === "summary") summary = summary || value;
    else if (name.endsWith("status")) status = status || value;
  }

  // Fallback: typed-section binding properties (descendants of a focused
  // parent, where field nodes aren't emitted).
  if (isContentNode(child)) {
    const props = child.data.binding?.properties || {};

    for (const [rawKey, value] of Object.entries(props)) {
      if (!value) continue;
      const name = rawKey.toLowerCase();

      if (name === identifierField) preferredID = preferredID || value;
      else if (name === "id") genericID = genericID || value;
      else if (name === "summary") summary = summary || value;
      else if (name.endsWith("status")) status = status || value;
    }
  }

  return { ...base, id: preferredID || genericID, status, summary };
}

/**
 * Adapt a workspace content node back into the StructuralNode shape that
 * the pane stack uses for opening embedded/section panes.
 */
export function structuralNodeFromWorkspace(child: WorkspaceNode): StructuralNode | null {
  if (!isContentNode(child)) return null;
  const data = child.data;
  const fragment = data.fragment;

  return {
    nodeId: child.ref.nodeId || `${child.notePath}${fragment ? `#${fragment}` : ""}`,
    fragment,
    title: data.title || "",
    typeName: data.binding?.typeName || data.resolvedType || "",
    locator: data.locator || defaultLocatorFor(child),
    notePath: child.notePath,
    level: data.level,
  };
}

function titleAndType(child: WorkspaceNode) {
  if (!isContentNode(child)) return { title: "", typeName: "" };
  const data = child.data;

  return {
    title: data.title || "",
    typeName: publicTypeName(data.binding?.typeName || data.resolvedType),
  };
}

function defaultLocatorFor(child: WorkspaceContentNode): string {
  switch (child.kind) {
    case "note":
      return "FILE";
    case "embedded":
      return "EMBEDDED";
    case "section":
      return "SECTION";
  }
}
