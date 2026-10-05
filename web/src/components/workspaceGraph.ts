import type {
  NodeCollectionState,
  NodeFieldState,
  NodeWorkspace,
  NoteWorkspaceGroup,
  RenderedFile,
  RenderedSection,
  StructuralNode,
  StructuralView,
  WorkspaceCollectionNode,
  WorkspaceFieldNode,
  WorkspaceNode,
  WorkspaceRelationGroupsView,
  WorkspaceStructuralTabView,
} from "../api/types";
import { isContentNode } from "../api/types";

function nodeMap(workspace: NodeWorkspace | null): Map<string, WorkspaceNode> {
  return new Map((workspace?.nodes || []).map((node) => [node.id, node]));
}

function canonicalScopeNodeID(
  workspace: NodeWorkspace | null,
  scopeNodeId?: string | null,
): string | null {
  if (!scopeNodeId) return null;

  if ((workspace?.nodes || []).some((node) => node.id === scopeNodeId)) {
    return scopeNodeId;
  }

  const match = (workspace?.nodes || []).find((node) => {
    if (node.kind === "field" || node.kind === "collection") return false;

    if (node.ref.nodeId && node.ref.nodeId === scopeNodeId) return true;

    if (!node.ref.nodeId && node.kind === "note" && node.ref.notePath === scopeNodeId) {
      return true;
    }

    return false;
  });

  return match?.id || null;
}

function renderedSectionFromNode(
  id: string,
  nodes: Map<string, WorkspaceNode>,
): RenderedSection | null {
  const node = nodes.get(id);

  if (!node || !isContentNode(node)) return null;

  if (node.kind === "note") return null;
  const data = node.data;
  const binding = data.binding;

  return {
    id: node.ref.nodeId || node.ref.fragment || node.id,
    title: data.title || "",
    level: data.level || "H2",
    content: data.markdown || "",
    notePath: node.notePath,
    parentId: node.parentId,
    locator: data.locator,
    blockId: data.blockId,
    typeName: binding?.typeName,
    fieldName: binding?.fieldName,
    fieldPath: binding?.fieldPath,
    fieldList: binding?.fieldList,
    sectionDisplay: binding?.sectionDisplay,
    properties: binding?.properties,
    identifierField: binding?.identifierField,
    previewTemplate: binding?.previewTemplate,
    collapsed: binding?.collapsed,
    children: (node.childIds || []).flatMap((childID: string) => {
      const child = renderedSectionFromNode(childID, nodes);

      return child ? [child] : [];
    }),
  };
}

function structuralNodeFromWorkspaceNode(
  id: string,
  nodes: Map<string, WorkspaceNode>,
): StructuralNode | null {
  const node = nodes.get(id);

  if (!node || !isContentNode(node)) return null;
  const data = node.data;
  const binding = data.binding;
  const hasChildren = (node.childIds || []).length > 0;

  const locator =
    data.locator ||
    (node.kind === "note" ? "FILE" : node.kind === "embedded" ? "EMBEDDED" : "SECTION");

  const content =
    node.kind === "note" && locator === "FILE" && hasChildren ? "" : data.markdown || "";

  return {
    nodeId: node.ref.nodeId || node.ref.notePath,
    fragment: data.fragment || node.ref.fragment,
    title: data.title || "",
    typeName: binding?.typeName || data.resolvedType,
    locator,
    notePath: node.notePath,
    parentNodeId: node.parentId,
    fieldName: binding?.fieldName,
    fieldPath: binding?.fieldPath,
    fieldList: binding?.fieldList,
    sectionDisplay: binding?.sectionDisplay,
    level: data.level,
    content,
    properties: binding?.properties,
    identifierField: binding?.identifierField,
    previewTemplate: binding?.previewTemplate,
    preview: binding?.previewTemplate,
    collapsed: binding?.collapsed,
    children: (node.childIds || []).flatMap((childID: string) => {
      const child = structuralNodeFromWorkspaceNode(childID, nodes);

      return child ? [child] : [];
    }),
  };
}

export function selectWorkspaceFields(workspace: NodeWorkspace | null): NodeFieldState[] {
  const graphNodes = (workspace?.nodes || []).filter(
    (node): node is WorkspaceFieldNode => node.kind === "field",
  );

  if (graphNodes.length === 0) {
    return workspace?.fields || [];
  }

  return graphNodes.map((node) => ({
    id: node.id,
    name: node.field.name,
    kind: "FIELD",
    present: node.field.present,
    status: node.status,
    range: node.field.range,
    values: node.field.values || [],
    valueRanges: node.field.valueRanges || [],
    inlineSpans: node.field.inlineSpans || [],
    sectionNodes: node.field.sectionRefs || [],
  }));
}

export function selectWorkspaceCollections(workspace: NodeWorkspace | null): NodeCollectionState[] {
  const graphNodes = (workspace?.nodes || []).filter(
    (node): node is WorkspaceCollectionNode => node.kind === "collection",
  );

  if (graphNodes.length === 0) {
    return workspace?.collections || [];
  }

  return graphNodes.map((node) => ({
    name: node.collection.name,
    kind: "COLLECTION",
    status: node.status,
    range: node.collection.range,
    items: node.collection.itemRefs || [],
    orderFingerprint: node.collection.orderFingerprint,
  }));
}

export function selectWorkspaceRenderedSections(
  workspace: NodeWorkspace | null,
  rendered: RenderedFile | null,
): RenderedSection[] {
  const rootIDs = workspace?.views?.renderedOutline?.rootIds || [];

  if (!workspace?.nodes?.length || rootIDs.length === 0) {
    return rendered?.sections || [];
  }

  const nodes = nodeMap(workspace);

  return rootIDs.flatMap((id: string) => {
    const section = renderedSectionFromNode(id, nodes);

    return section ? [section] : [];
  });
}

export function selectWorkspaceStructuralView(
  workspace: NodeWorkspace | null,
): StructuralView | null {
  if (workspace?.content?.structural) {
    return workspace.content.structural;
  }

  const outline = workspace?.views?.structuralOutline;

  if (!workspace?.nodes?.length || !outline?.rootId) {
    return workspace?.content?.structural || null;
  }

  const nodes = nodeMap(workspace);
  const root = structuralNodeFromWorkspaceNode(outline.rootId, nodes);

  if (!root) {
    return workspace?.content?.structural || null;
  }

  return {
    root,
    defaultView: outline.defaultView,
    tabs: (outline.tabs || []).map((tab: WorkspaceStructuralTabView) => ({
      key: tab.key,
      label: tab.label,
      fieldName: tab.fieldName,
      count: tab.count,
      nodes: (tab.nodeIds || []).flatMap((id: string) => {
        const node = structuralNodeFromWorkspaceNode(id, nodes);

        return node ? [node] : [];
      }),
    })),
  };
}

export function selectWorkspaceRelationGroups(
  workspace: NodeWorkspace | null,
  scopeNodeId?: string | null,
): NoteWorkspaceGroup[] {
  const groupsByScope = workspace?.views?.relationGroups || [];
  const canonicalScopeID = canonicalScopeNodeID(workspace, scopeNodeId);

  if (groupsByScope.length === 0) {
    if (scopeNodeId) {
      return workspace?.sectionRelationGroups?.[scopeNodeId] || workspace?.relations || [];
    }

    return workspace?.relations || [];
  }

  const scoped = groupsByScope.find(
    (entry: WorkspaceRelationGroupsView) => entry.scopeNodeId === (canonicalScopeID || ""),
  );

  if (scoped) return scoped.groups || [];

  const root = groupsByScope.find(
    (entry: WorkspaceRelationGroupsView) =>
      !entry.scopeNodeId || entry.scopeNodeId === workspace?.focusedNodeId,
  );

  return root?.groups || workspace?.relations || [];
}

// Re-exported for legacy callers — content nodes are now the canonical shape
// for anything that previously branched on note/section/embedded.
