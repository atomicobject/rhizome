import type { JsonObject } from "./parse";

export type PublicGraphQLError = {
  message: string;
  path?: string[];
  extensions?: JsonObject;
};

export type PublicNodeRef = {
  ref: string;
  notePath?: string | null;
  path?: string | null;
  fragment?: string | null;
  kind: string;
  nodeId?: string | null;
  structural?: string | null;
  typeName?: string | null;
};

export type PublicGraphQLResult<TData> = {
  data?: TData;
  errors?: PublicGraphQLError[];
};

type PublicNodeLocator = {
  ref?: PublicNodeRef | null;
  kind?: string | null;
  sourceLocator: string;
  status: string;
  wikilink?: string | null;
  markdown?: string | null;
  exists: boolean;
  requiresFix: boolean;
  linkTarget?: {
    ref?: PublicNodeRef | null;
    markdown?: string | null;
    wikilink?: string | null;
    displayLabel?: string | null;
    exists: boolean;
    requiresFix: boolean;
    blockId?: string | null;
  } | null;
  diagnostics?: Array<{
    code: string;
    notePath?: string | null;
    ref?: PublicNodeRef | null;
    blockId?: string | null;
    message: string;
  }> | null;
  fixActions?: Array<{
    ref: PublicNodeRef;
    blockId: string;
  }> | null;
};

export type PublicLocalGraphNode = {
  id: string;
  ref?: PublicNodeRef | null;
  nodeKind: string;
  title: string;
  typeName?: string | null;
  path?: string | null;
  notePath?: string | null;
  nodeId?: string | null;
  parentId?: string | null;
  sourceLocator?: string | null;
};

export type PublicLocalGraphEdge = {
  source: string;
  target: string;
  kind: string;
  relation?: string | null;
  relationLabel?: string | null;
  provenance?: string | null;
  structural: boolean;
  weight?: number | null;
};

type PublicLocalGraph = {
  nodes: PublicLocalGraphNode[];
  edges: PublicLocalGraphEdge[];
  truncated: boolean;
};

export type PublicNode = {
  ref: PublicNodeRef;
  nodeId: string;
  nodeKind: string;
  path?: string | null;
  title: string;
  resolvedType?: string | null;
  locator: PublicNodeLocator;
  localGraph?: PublicLocalGraph | null;
  content?: string;
  format?: string;
  sourceRepresentation?: string;
  evidenceRepresentation?: string;
  sourceCapabilities?: string[];
  frontmatter?: JsonObject | null;
  tags?: string[];
  notePath?: string;
  level?: string;
  language?: string | null;
  symbol?: string | null;
  fqn?: string | null;
  signature?: string | null;
  docComment?: string | null;
  score?: number | null;
  workspace?: PublicNodeWorkspaceProjection | null;
};

type PublicNodeStatus = {
  dirty: boolean;
  validation: { issueCount: number };
  freshness: { state?: string | null };
  session: { state?: string | null };
  hasWarnings: boolean;
};

type PublicNodeRange = { start: number; end: number };

export type PublicWorkspaceFieldState = {
  name: string;
  kind: string;
  sourceKind?: string | null;
  present: boolean;
  status: PublicNodeStatus;
  range: PublicNodeRange;
  valueRanges: PublicNodeRange[] | null;
  values: string[] | null;
  links?: Array<{
    value: string;
    resolved: boolean;
    title?: string | null;
    ref?: PublicNodeRef | null;
  }> | null;
  sectionNodes: PublicNodeRef[] | null;
  capability?: {
    ownerRef: PublicNodeRef;
    ownerType: string;
    typeName: string;
    valueKind: string;
    list: boolean;
    required: boolean;
    enumValues: string[];
    enumOptions?: Array<{ value: string; label: string; tone?: string | null }> | null;
    targetType?: string | null;
    sourceKind?: string | null;
    valueOrigin: string;
    identifier: boolean;
    preferredIdentifier: boolean;
    displayImportance: "KEY" | "NORMAL" | "DETAIL";
    writeOperation?: string | null;
    readOnlyReason?: string | null;
  };
  issues?: PublicAssessmentIssue[];
};

type PublicWorkspaceCollectionState = {
  name: string;
  kind: string;
  status: PublicNodeStatus;
  range: PublicNodeRange;
  items: Array<{ ref: PublicNodeRef; range: PublicNodeRange }>;
  orderFingerprint?: string | null;
};

type PublicBodyBlock = {
  kind: "narrative" | "inline_field" | "child_section" | "collection";
  range: PublicNodeRange;
  markdown?: string | null;
  fieldName?: string | null;
  rawKey?: string | null;
  childRef?: PublicNodeRef | null;
  childRefs: PublicNodeRef[];
  sectionDisplay?: string | null;
};

export type PublicWorkspaceBodyProjection = {
  ref: PublicNodeRef;
  title: string;
  resolvedType?: string | null;
  locator: string;
  level?: string | null;
  blockId?: string | null;
  parentRef?: PublicNodeRef | null;
  markdown?: string | null;
  binding?: {
    typeName?: string | null;
    fieldName?: string | null;
    fieldPath?: string | null;
    fieldList?: boolean | null;
    sectionDisplay?: string | null;
    properties?: Record<string, string> | null;
    identifierField?: string | null;
    previewTemplate?: string | null;
    collapsed?: boolean | null;
  } | null;
  fields: PublicWorkspaceFieldState[];
  collections: PublicWorkspaceCollectionState[];
  blocks: PublicBodyBlock[];
};

type PublicAssessmentIssue = {
  code?: string | null;
  notePath?: string | null;
  typeName?: string | null;
  fieldName?: string | null;
  nodeRef?: string | null;
  nodeId?: string | null;
  line?: number | null;
  message: string;
};

export type PublicNodeWorkspaceProjection = {
  parentRef?: PublicNodeRef | null;
  parentTitle?: string | null;
  sourceLinks?: Array<{
    target: string;
    authoredTarget?: string | null;
    text?: string | null;
    kind: string;
    anchor?: string | null;
    embed: boolean;
    targetKind?: string | null;
    resolved: boolean;
    resolvedRef?: PublicNodeRef | null;
    title?: string | null;
    preview?: string | null;
  }> | null;
  fields: PublicWorkspaceFieldState[];
  collections: PublicWorkspaceCollectionState[];
  bodies: PublicWorkspaceBodyProjection[];
  capabilities: {
    canEdit: boolean;
    canEditFields: boolean;
    canEditCollections: boolean;
    canNavigateChildren: boolean;
    canSubscribe: boolean;
  };
  status: PublicNodeStatus;
  version: string;
  sourceRevision?: {
    notePath: string;
    contentFingerprint: string;
    content: string;
  };
  assessment?: {
    notePath: string;
    declaredType?: string | null;
    resolvedType?: string | null;
    candidateTypes: string[];
    issues: PublicAssessmentIssue[];
    fields: Array<{
      name: string;
      description?: string | null;
      kind: string;
      typeName: string;
      required: boolean;
      list: boolean;
      source?: string | null;
      sourceKind?: string | null;
      present: boolean;
      values: string[];
      validValues: string[];
      issues: PublicAssessmentIssue[];
    }>;
    relations: Array<{
      name: string;
      description?: string | null;
      kind: string;
      typeName: string;
      required: boolean;
      list: boolean;
      source?: string | null;
      sourceKind?: string | null;
      direction?: string | null;
      present: boolean;
      values: string[];
      targets: Array<{
        path: string;
        typeName?: string | null;
        provenance?: string | null;
        structural: boolean;
      }>;
      issues: PublicAssessmentIssue[];
    }>;
  } | null;
  structure: Array<{
    ref: PublicNodeRef;
    parentRef?: PublicNodeRef | null;
    title: string;
    level?: string | null;
    content?: string | null;
  }>;
  relationGroups: Array<{
    key: string;
    label: string;
    ownerTitle?: string | null;
    navigation?: boolean;
    items: Array<{
      ref: PublicNodeRef;
      title: string;
      targetTitle?: string | null;
      resolvedType?: string | null;
      relationName?: string | null;
      provenance?: string | null;
      structural: boolean;
      current?: boolean;
    }>;
  }>;
  loaded: {
    rendered: boolean;
    assessment: boolean;
    structure: boolean;
    relations: boolean;
  };
};

export type PublicNodeDetailData = {
  node?: PublicNode | null;
};

export type PublicLocalGraphData = {
  node?: PublicNode | null;
};
