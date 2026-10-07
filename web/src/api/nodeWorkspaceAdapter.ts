import type {
  PublicLocalGraphEdge,
  PublicLocalGraphNode,
  PublicNode,
  PublicNodeDetailData,
  PublicNodeRef,
  PublicWorkspaceBodyProjection,
  PublicWorkspaceFieldState,
} from "./publicGraphQLTypes";
import type {
  NodeCapabilities,
  NodeContent,
  NodeDescriptor,
  NodeFieldCapability,
  NodeFieldLink,
  NodeLocator,
  NodeRef,
  NodeStatus,
  NodeWorkspace,
  RenderedFile,
  RenderedSection,
  StructuralNode,
  WorkspaceContentNode,
  WorkspaceEdge,
  WorkspaceNode,
} from "./types";

const NODE_LOCATOR_STATUSES = [
  "linkable",
  "requires_fix",
  "unsupported",
  "unresolved",
] as const satisfies readonly NodeLocator["status"][];

function nodeLocatorStatus(value: string): NodeLocator["status"] {
  const known = NODE_LOCATOR_STATUSES.find((status) => status === value);

  return known || "unresolved";
}

function workspaceNodeKind(nodeKind: string): WorkspaceContentNode["kind"] {
  switch (nodeKind) {
    case "SECTION":
      return "section";
    case "EMBEDDED":
      return "embedded";
    default:
      return "note";
  }
}

function workspaceRelationKind(nodeKind: string): string {
  switch (nodeKind) {
    case "SECTION":
      return "section";
    case "EMBEDDED":
      return "embedded";
    case "CODE_FILE":
    case "CODE_SYMBOL":
    case "MODULE":
      return "code";
    default:
      return "note";
  }
}

function emptyCapabilities(): NodeCapabilities {
  return {
    canEdit: false,
    canEditFields: false,
    canEditCollections: false,
    canNavigateChildren: true,
    canSubscribe: true,
  };
}

function emptyStatus(): NodeStatus {
  return {
    dirty: false,
    validation: { issueCount: 0 },
    freshness: {},
    session: {},
    hasWarnings: false,
  };
}

function statusFromPublic(status?: {
  dirty: boolean;
  validation: { issueCount: number };
  freshness: { state?: string | null };
  session: { state?: string | null };
  hasWarnings: boolean;
}): NodeStatus {
  if (!status) return emptyStatus();

  return {
    dirty: status.dirty,
    validation: status.validation,
    freshness: { state: status.freshness.state || undefined },
    session: { state: status.session.state || undefined },
    hasWarnings: status.hasWarnings,
  };
}

function fieldLinksFromPublic(
  links: PublicWorkspaceFieldState["links"],
): NodeFieldLink[] | undefined {
  return links?.map((link) => ({
    value: link.value,
    ref: link.ref ? nodeRefFromPublicRef(link.ref) : undefined,
    title: link.title || undefined,
  }));
}

function nodeRefFromPublicRef(ref: PublicNodeRef): NodeRef {
  return {
    notePath: ref.notePath || ref.path || ref.ref,
    fragment: ref.fragment || undefined,
    nodeId: ref.nodeId || undefined,
    typeName: ref.typeName || undefined,
    kind: ref.kind,
    structuralFingerprint: ref.structural || undefined,
  };
}

function nodeLocatorFromPublicNode(node: PublicNode): NodeLocator {
  const locator = node.locator;
  const ref = nodeRefFromPublicRef(locator.ref || node.ref);

  const linkTarget = locator.linkTarget
    ? {
        ref: nodeRefFromPublicRef(locator.linkTarget.ref || locator.ref || node.ref),
        markdown: locator.linkTarget.markdown || undefined,
        wikilink: locator.linkTarget.wikilink || undefined,
        displayLabel: locator.linkTarget.displayLabel || undefined,
        exists: locator.linkTarget.exists,
        requiresFix: locator.linkTarget.requiresFix,
        blockId: locator.linkTarget.blockId || undefined,
      }
    : undefined;

  return {
    ref,
    kind: locator.kind || ref.kind,
    sourceLocator: locator.sourceLocator,
    status: nodeLocatorStatus(locator.status),
    linkTarget,
    diagnostics: locator.diagnostics?.map((diagnostic) => ({
      code: diagnostic.code,
      notePath: diagnostic.notePath || undefined,
      ref: diagnostic.ref ? nodeRefFromPublicRef(diagnostic.ref) : undefined,
      blockId: diagnostic.blockId || undefined,
      message: diagnostic.message,
    })),
    fixActions: locator.fixActions?.map((action) => ({
      ref: nodeRefFromPublicRef(action.ref),
      blockId: action.blockId,
    })),
  };
}

function descriptorFromPublicNode(node: PublicNode): NodeDescriptor {
  const ref = nodeRefFromPublicRef(node.ref);

  return {
    ref,
    resolvedType: node.resolvedType || undefined,
    notePath: ref.notePath,
    title: node.title,
    locator: node.locator.sourceLocator || node.ref.ref,
    nodeLocator: nodeLocatorFromPublicNode(node),
    parentRef: node.workspace?.parentRef
      ? nodeRefFromPublicRef(node.workspace.parentRef)
      : undefined,
    parentTitle: node.workspace?.parentTitle || undefined,
  };
}

function contentFromPublicNode(node: PublicNode): NodeContent {
  const rendered = renderedFromPublicNode(node);
  const structural = structuralFromPublicNode(node);

  return {
    path: node.path || node.ref.notePath || node.ref.ref,
    title: node.title,
    resolvedType: node.resolvedType || undefined,
    markdown: node.content || "",
    format: node.format || undefined,
    sourceRepresentation: node.sourceRepresentation || undefined,
    evidenceRepresentation: node.evidenceRepresentation || undefined,
    sourceCapabilities: node.sourceCapabilities || [],
    rendered,
    assessment: assessmentFromPublicWorkspace(node.workspace?.assessment),
    structural,
  };
}

type PublicAssessment = NonNullable<NonNullable<PublicNode["workspace"]>["assessment"]>;

type PublicAssessmentIssue = PublicAssessment["issues"][number];

type ValidationIssue = NonNullable<NonNullable<NodeContent["assessment"]>["issues"]>[number];

/** GraphQL sends `null` for absent optional strings; the REST schema expects them absent. */
function issueFromPublicAssessment(issue: PublicAssessmentIssue): ValidationIssue {
  return { ...issue, code: issue.code || undefined };
}

function assessmentFromPublicWorkspace(
  assessment: NonNullable<PublicNode["workspace"]>["assessment"],
): NodeContent["assessment"] {
  if (!assessment) return undefined;

  return {
    ...assessment,
    declaredType: assessment.declaredType || undefined,
    resolvedType: assessment.resolvedType || undefined,
    issues: assessment.issues.map(issueFromPublicAssessment),
    fields: assessment.fields.map((field) => ({
      ...field,
      description: field.description || undefined,
      source: field.source || undefined,
      issues: field.issues.map(issueFromPublicAssessment),
    })),
    relations: assessment.relations.map((relation) => ({
      ...relation,
      description: relation.description || undefined,
      source: relation.source || undefined,
      direction: relation.direction || undefined,
      targets: relation.targets.map((target) => ({
        ...target,
        typeName: target.typeName || undefined,
        provenance: target.provenance || undefined,
      })),
      issues: relation.issues.map(issueFromPublicAssessment),
    })),
  };
}

const NOTE_BACKED_KINDS = new Set(["NOTE", "SECTION", "EMBEDDED"]);

function renderedFromPublicNode(node: PublicNode): RenderedFile | undefined {
  if (!NOTE_BACKED_KINDS.has(node.nodeKind)) return undefined;
  const { links, embeds } = renderedLinksAndEmbedsFromPublicWorkspace(node);

  return {
    path: node.path || node.ref.notePath || node.ref.ref,
    title: node.title,
    resolvedType: node.resolvedType || undefined,
    content: node.content || "",
    rendered: node.content || "",
    frontmatter: node.frontmatter || undefined,
    links,
    embeds,
    sections: renderedSectionsFromPublicNode(node),
  };
}

function renderedLinksAndEmbedsFromPublicWorkspace(node: PublicNode) {
  const sourceLinks = node.workspace?.sourceLinks || [];
  const links: NonNullable<RenderedFile["links"]> = [];
  const embeds: NonNullable<RenderedFile["embeds"]> = [];
  const embedTargets = new Set<string>();

  for (const sourceLink of sourceLinks) {
    const target = sourceLink.resolvedRef?.ref || sourceLink.target;

    if (!target) continue;
    links.push({
      target,
      // Legacy LinkRef.text is the authored-target lookup key; sourceLinks.text is display text.
      text: sourceLink.authoredTarget || sourceLink.text || sourceLink.title || target,
      kind: sourceLink.kind,
      anchor: sourceLink.anchor || undefined,
    });

    if (sourceLink.embed) {
      if (embedTargets.has(target)) continue;
      embedTargets.add(target);
      embeds.push({
        target,
        title: sourceLink.title || sourceLink.text || sourceLink.authoredTarget || target,
        kind: sourceLink.targetKind || "unknown",
        preview: sourceLink.preview || undefined,
        resolved: sourceLink.resolved,
      });
    }
  }

  return { links, embeds };
}

function renderedSectionsFromPublicNode(node: PublicNode): RenderedSection[] {
  const structure = node.workspace?.structure || [];
  const byRef = new Map<string, RenderedSection>();
  const roots: RenderedSection[] = [];
  structure.forEach((item) => {
    byRef.set(item.ref.ref, {
      id: item.ref.nodeId || item.ref.ref,
      title: item.title,
      level: item.level || "H2",
      content: item.content || "",
      notePath: item.ref.notePath || node.ref.notePath || "",
      locator: item.ref.kind,
      children: [],
    });
  });
  structure.forEach((item) => {
    const current = byRef.get(item.ref.ref);
    const parent = item.parentRef?.ref ? byRef.get(item.parentRef.ref) : undefined;

    if (current && parent) {
      parent.children?.push(current);
    } else if (current) {
      roots.push(current);
    }
  });

  return roots;
}

function structuralFromPublicNode(node: PublicNode): NodeContent["structural"] {
  const structure = node.workspace?.structure || [];

  if (structure.length === 0) return undefined;
  const byRef = new Map<string, StructuralNode>();
  const roots: StructuralNode[] = [];
  structure.forEach((item) => {
    byRef.set(item.ref.ref, {
      nodeId: item.ref.nodeId || item.ref.ref,
      fragment: item.ref.fragment || undefined,
      title: item.title,
      typeName: item.ref.typeName || undefined,
      locator: item.ref.kind,
      notePath: item.ref.notePath || node.ref.notePath || "",
      parentNodeId: item.parentRef?.nodeId || undefined,
      structuralFingerprint: item.ref.structural || undefined,
      level: item.level || undefined,
      content: item.content || undefined,
      children: [],
    });
  });
  structure.forEach((item) => {
    const current = byRef.get(item.ref.ref);
    const parent = item.parentRef?.ref ? byRef.get(item.parentRef.ref) : undefined;

    if (current && parent) {
      parent.children = [...(parent.children || []), current];
    } else if (current) {
      roots.push(current);
    }
  });

  if (node.nodeKind !== "NOTE") {
    const selected = structure.find(
      (item) =>
        item.ref.ref === node.ref.ref || (item.ref.nodeId && item.ref.nodeId === node.nodeId),
    );

    const selectedRoot = selected ? byRef.get(selected.ref.ref) : undefined;

    if (!selectedRoot) return undefined;
    selectedRoot.parentNodeId = undefined;

    return {
      root: selectedRoot,
      defaultView: node.resolvedType ? "structural" : "markdown",
    };
  }

  const root: StructuralNode = {
    nodeId: node.nodeId,
    title: node.title,
    typeName: node.resolvedType || undefined,
    locator: node.ref.kind,
    notePath: node.ref.notePath || node.path || "",
    content: node.content || undefined,
    children: roots,
  };

  return { root, defaultView: node.resolvedType ? "structural" : "markdown" };
}

function workspaceNodeFromGraphNode(
  graphNode: PublicLocalGraphNode,
  fallback: PublicNode,
): WorkspaceNode {
  const ref = nodeRefFromPublicRef(graphNode.ref || fallback.ref);
  const sourceLocator = graphNode.sourceLocator || graphNode.ref?.ref || fallback.ref.ref;

  return {
    id: graphNode.id,
    kind: workspaceNodeKind(graphNode.nodeKind),
    ref,
    notePath: graphNode.notePath || ref.notePath || graphNode.path || "",
    parentId: graphNode.parentId || undefined,
    status: emptyStatus(),
    capabilities: emptyCapabilities(),
    data: {
      title: graphNode.title,
      resolvedType: graphNode.typeName || undefined,
      locator: sourceLocator,
    },
  };
}

function workspaceEdgeFromGraphEdge(edge: PublicLocalGraphEdge): WorkspaceEdge {
  return {
    fromId: edge.source,
    toId: edge.target,
    kind: workspaceEdgeKind(edge.kind),
    relationLabel: edge.relationLabel || edge.relation || undefined,
    relationKey: edge.relation || undefined,
  };
}

function workspaceEdgeKind(kind: string): WorkspaceEdge["kind"] {
  if (kind === "ontology") return "links_to";

  return kind;
}

function focusedNodeId(node: PublicNode): string {
  if (node.localGraph?.nodes?.length) {
    const refKey = node.ref.ref;

    const matched = node.localGraph.nodes.find((candidate) => {
      return candidate.ref?.ref === refKey;
    });

    if (matched) return matched.id;

    if (node.nodeKind === "NOTE") {
      const noteMatched = node.localGraph.nodes.find((candidate) => {
        return candidate.path === node.path;
      });

      if (noteMatched) return noteMatched.id;
    }

    const nodeIDMatched = node.localGraph.nodes.find((candidate) => {
      return candidate.nodeId === node.nodeId;
    });

    if (nodeIDMatched) return nodeIDMatched.id;
  }

  return node.nodeId || node.ref.ref;
}

function canonicalWorkspaceNodeID(ref: PublicNodeRef): string {
  return ["node", ref.notePath || ref.path || ref.ref, ref.fragment || "", ref.nodeId || ""].join(
    "|",
  );
}

function bodyBlockFromPublic(block: PublicWorkspaceBodyProjection["blocks"][number]) {
  return {
    kind: block.kind,
    range: block.range,
    markdown: block.markdown || undefined,
    fieldName: block.fieldName || undefined,
    rawKey: block.rawKey || undefined,
    childRef: block.childRef ? nodeRefFromPublicRef(block.childRef) : undefined,
    childRefs: (block.childRefs || []).map(nodeRefFromPublicRef),
    sectionDisplay: block.sectionDisplay || undefined,
  };
}

function contentNodeFromPublicBody(body: PublicWorkspaceBodyProjection): WorkspaceNode {
  const ref = nodeRefFromPublicRef(body.ref);

  return {
    id: canonicalWorkspaceNodeID(body.ref),
    kind: workspaceNodeKind(body.ref.kind),
    ref,
    notePath: ref.notePath,
    parentId: body.parentRef ? canonicalWorkspaceNodeID(body.parentRef) : undefined,
    status: emptyStatus(),
    capabilities: emptyCapabilities(),
    body: body.blocks.map(bodyBlockFromPublic),
    data: {
      title: body.title,
      resolvedType: body.resolvedType || undefined,
      markdown: body.markdown || undefined,
      locator: body.locator,
      level: body.level || undefined,
      blockId: body.blockId || undefined,
      fragment: ref.fragment,
      binding: body.binding
        ? {
            typeName: body.binding.typeName || undefined,
            fieldName: body.binding.fieldName || undefined,
            fieldPath: body.binding.fieldPath || undefined,
            fieldList: body.binding.fieldList || undefined,
            sectionDisplay: body.binding.sectionDisplay || undefined,
            properties: body.binding.properties || undefined,
            identifierField: body.binding.identifierField || undefined,
            previewTemplate: body.binding.previewTemplate || undefined,
            collapsed: body.binding.collapsed || undefined,
          }
        : undefined,
    },
  };
}

function fieldCapabilityFromPublic(
  capability: PublicWorkspaceBodyProjection["fields"][number]["capability"],
  ownerRef: PublicNodeRef,
): NodeFieldCapability {
  if (!capability) {
    return {
      ownerRef: nodeRefFromPublicRef(ownerRef),
      ownerType: ownerRef.typeName || "",
      typeName: "",
      valueKind: "text",
      list: false,
      required: false,
      enumValues: [],
      valueOrigin: "authored",
      identifier: false,
      preferredIdentifier: false,
      displayImportance: "NORMAL" as const,
      readOnlyReason: "Field editing metadata is unavailable.",
    };
  }

  return {
    ...capability,
    displayImportance: capability.displayImportance || "NORMAL",
    enumOptions: (capability.enumOptions || []).map(({ value, label, tone }) => ({
      value,
      label,
      tone: tone || undefined,
    })),
    ownerRef: nodeRefFromPublicRef(capability.ownerRef),
    targetType: capability.targetType || undefined,
    sourceKind: capability.sourceKind || undefined,
    writeOperation: capability.writeOperation || undefined,
    readOnlyReason: capability.readOnlyReason || undefined,
  };
}

function appendChild(
  nodes: Map<string, WorkspaceNode>,
  parentID: string | undefined,
  childID: string,
) {
  if (!parentID) return;
  const parent = nodes.get(parentID);

  if (!parent || parent.kind === "field" || parent.kind === "collection") {
    return;
  }

  parent.childIds = [...new Set([...(parent.childIds || []), childID])];
}

function canonicalWorkspaceNodes(node: PublicNode): {
  nodes: WorkspaceNode[];
  edges: WorkspaceEdge[];
  focusedID: string;
} | null {
  const bodies = node.workspace?.bodies || [];

  if (bodies.length === 0) return null;

  const nodes = new Map<string, WorkspaceNode>();
  const edges: WorkspaceEdge[] = [];

  for (const body of bodies) {
    const content = contentNodeFromPublicBody(body);
    nodes.set(content.id, content);
  }

  for (const body of bodies) {
    const ownerID = canonicalWorkspaceNodeID(body.ref);
    const owner = nodes.get(ownerID);

    if (!owner) continue;
    appendChild(nodes, owner.parentId, ownerID);

    if (owner.parentId) {
      edges.push({ kind: "contains", fromId: owner.parentId, toId: ownerID });
    }

    for (const field of body.fields) {
      const capability = fieldCapabilityFromPublic(field.capability, body.ref);
      const id = `field|${ownerID}|${field.name}`;
      nodes.set(id, {
        id,
        kind: "field",
        ref: owner.ref,
        notePath: owner.notePath,
        parentId: ownerID,
        status: statusFromPublic(field.status),
        capabilities: emptyCapabilities(),
        field: {
          name: field.name,
          valueKind: capability.valueKind,
          typeName: capability.typeName,
          enumValues: capability.enumValues,
          present: field.present,
          values: field.values || [],
          links: fieldLinksFromPublic(field.links),
          range: field.range,
          valueRanges: field.valueRanges || [],
          sectionRefs: (field.sectionNodes || []).map(nodeRefFromPublicRef),
          capability,
          issues: (field.issues || []).map(issueFromPublicAssessment),
        },
      });
      appendChild(nodes, ownerID, id);
      edges.push({
        kind: "binds_field",
        fromId: ownerID,
        toId: id,
        fieldName: field.name,
      });
    }

    for (const collection of body.collections) {
      const id = `collection|${ownerID}|${collection.name}`;

      const collectionNode: WorkspaceNode = {
        id,
        kind: "collection",
        ref: owner.ref,
        notePath: owner.notePath,
        parentId: ownerID,
        status: statusFromPublic(collection.status),
        capabilities: emptyCapabilities(),
        collection: {
          name: collection.name,
          itemRefs: collection.items.map((item) => ({
            ref: nodeRefFromPublicRef(item.ref),
            range: item.range,
          })),
          orderFingerprint: collection.orderFingerprint || undefined,
          range: collection.range,
        },
      };

      nodes.set(id, collectionNode);
      appendChild(nodes, ownerID, id);
      edges.push({
        kind: "binds_field",
        fromId: ownerID,
        toId: id,
        collectionName: collection.name,
      });

      for (const item of collection.items) {
        const itemID = canonicalWorkspaceNodeID(item.ref);

        if (!nodes.has(itemID)) continue;
        collectionNode.childIds = [...new Set([...(collectionNode.childIds || []), itemID])];
        edges.push({
          kind: "collection_item",
          fromId: id,
          toId: itemID,
          collectionName: collection.name,
        });
      }
    }
  }

  const focusedBody = bodies.find((body) => body.ref.ref === node.ref.ref) || bodies[0];

  return {
    nodes: [...nodes.values()],
    edges,
    focusedID: canonicalWorkspaceNodeID(focusedBody.ref),
  };
}

function mergeLocalGraphNodes(
  canonicalNodes: WorkspaceNode[],
  graphNodes: PublicLocalGraphNode[],
  node: PublicNode,
): WorkspaceNode[] {
  const byID = new Map(canonicalNodes.map((item) => [item.id, item]));

  for (const graphNode of graphNodes) {
    if (!byID.has(graphNode.id)) {
      byID.set(graphNode.id, workspaceNodeFromGraphNode(graphNode, node));
    }
  }

  return [...byID.values()];
}

function relationDirection(
  direction: string | null | undefined,
): "outgoing" | "incoming" | undefined {
  if (direction === "INBOUND") return "incoming";

  return direction === "OUTBOUND" ? "outgoing" : undefined;
}

export function nodeWorkspaceFromPublicGraphQL(
  data: PublicNodeDetailData,
  requestedRef: string,
): NodeWorkspace {
  if (!data.node) {
    throw new Error(`Public GraphQL node did not resolve: ${requestedRef}`);
  }

  const node = data.node;
  const graphNodes = node.localGraph?.nodes || [];
  const graphEdges = node.localGraph?.edges || [];
  const projection = node.workspace;
  const canonical = canonicalWorkspaceNodes(node);
  const nodeLocator = nodeLocatorFromPublicNode(node);
  const graphFocused = focusedNodeId(node);
  const focused = canonical?.focusedID || graphFocused;

  const fallbackNodes =
    graphNodes.length > 0
      ? graphNodes.map((graphNode) => workspaceNodeFromGraphNode(graphNode, node))
      : [
          {
            id: focused,
            kind: workspaceNodeKind(node.nodeKind),
            ref: nodeRefFromPublicRef(node.ref),
            notePath: node.ref.notePath || node.path || "",
            status: emptyStatus(),
            capabilities: emptyCapabilities(),
            data: {
              title: node.title,
              resolvedType: node.resolvedType || undefined,
              locator: node.locator.sourceLocator || node.ref.ref,
            },
          },
        ];

  const nodes = canonical ? mergeLocalGraphNodes(canonical.nodes, graphNodes, node) : fallbackNodes;

  const localNodeIDs = graphNodes.length
    ? graphNodes.map((item) => item.id)
    : canonical
      ? [canonical.focusedID]
      : nodes.map((item) => item.id);

  const localGraphCenterIDs = graphNodes.length
    ? graphNodes.some((graphNode) => graphNode.id === graphFocused)
      ? [graphFocused]
      : []
    : [focused];

  return {
    requestedRef,
    focusedNodeId: focused,
    node: descriptorFromPublicNode(node),
    content: contentFromPublicNode(node),
    nodes,
    edges: [...(canonical?.edges || []), ...graphEdges.map(workspaceEdgeFromGraphEdge)],
    views: {
      localGraph: {
        centerNodeIds: localGraphCenterIDs,
        nodeIds: localNodeIDs,
      },
    },
    fields: projection?.fields?.map((field) => ({
      ...field,
      sourceKind: field.sourceKind || undefined,
      status: statusFromPublic(field.status),
      values: field.values || [],
      links: fieldLinksFromPublic(field.links),
      valueRanges: field.valueRanges || [],
      sectionNodes: (field.sectionNodes || []).map(nodeRefFromPublicRef),
      capability: fieldCapabilityFromPublic(field.capability, node.ref),
      issues: (field.issues || []).map(issueFromPublicAssessment),
    })),
    collections: projection?.collections?.map((collection) => ({
      ...collection,
      status: statusFromPublic(collection.status),
      items: collection.items.map((item) => ({
        ref: nodeRefFromPublicRef(item.ref),
        range: item.range,
      })),
      orderFingerprint: collection.orderFingerprint || undefined,
    })),
    relations: projection?.relationGroups.map((group) => ({
      key: group.key,
      label: group.label,
      ownerTitle: group.ownerTitle || undefined,
      navigation: Boolean(group.navigation),
      items: group.items.map((item) => ({
        path: item.ref.notePath || item.ref.path || item.ref.ref,
        title: item.title,
        targetTitle: item.targetTitle || undefined,
        anchor: item.ref.fragment || undefined,
        kind: workspaceRelationKind(item.ref.kind),
        resolvedType: item.resolvedType || undefined,
        relationName: item.relationName || undefined,
        provenance: item.provenance || undefined,
        direction: relationDirection(item.direction),
        structural: item.structural,
        current: Boolean(item.current),
      })),
    })),
    loaded: projection?.loaded || {
      rendered: Boolean(node.content),
      assessment: false,
      structure: false,
      relations: false,
    },
    capabilities: projection?.capabilities || emptyCapabilities(),
    status: statusFromPublic(projection?.status),
    version: projection?.version || "",
    sourceRevision: projection?.sourceRevision,
    nodeLocator,
    linkTarget: nodeLocator.linkTarget,
  };
}
