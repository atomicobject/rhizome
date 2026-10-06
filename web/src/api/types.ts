import type { components, operations } from "./generated";
import type { JsonValue } from "./parse";

export type TreeEntry = components["schemas"]["TreeEntry"];

export type TreeResponse = components["schemas"]["TreeResponse"];

export type GraphNode = components["schemas"]["GraphNode"];

export type GraphEdge = components["schemas"]["GraphEdge"];

export type GraphResponse = components["schemas"]["GraphResponse"];

export type FileView = components["schemas"]["FileView"];

export type RenderedFile = components["schemas"]["RenderedFile"];

export type RenderedSection = components["schemas"]["RenderedSection"];

type NoteAssessment = components["schemas"]["NoteAssessment"];

export type TypeDoc = components["schemas"]["TypeDoc"];

export type StructuralNode = components["schemas"]["StructuralNodeResponse"] & {
  fragment?: string;
  structuralFingerprint?: string;
};

type GeneratedStructuralView = components["schemas"]["StructuralViewResponse"];

type GeneratedStructuralTab = NonNullable<GeneratedStructuralView["tabs"]>[number];

export type StructuralView = Omit<GeneratedStructuralView, "root" | "tabs"> & {
  root: StructuralNode;
  tabs?: Array<Omit<GeneratedStructuralTab, "nodes"> & { nodes?: StructuralNode[] }>;
};

export type NoteWorkspaceGroup = components["schemas"]["NoteWorkspaceGroup"];

export type NoteWorkspaceLink = components["schemas"]["NoteWorkspaceLink"];

export type NoteSearchResponse = components["schemas"]["NoteSearchResponse"];

export type NoteSearchMatch = components["schemas"]["NoteSearchMatch"];

export type NodePreviewFieldValue = components["schemas"]["NodePreviewValue"];

export type NodePreviewField = components["schemas"]["NodePreviewField"];

export type NodePreview = components["schemas"]["NodePreview"];

export type StatusResponse = components["schemas"]["StatusResponse"];

export type ViewTarget = components["schemas"]["ViewTarget"];

export type ViewCatalog = components["schemas"]["ViewCatalog"];

export type ViewChoice = components["schemas"]["ViewChoice"];

export type ViewCatalogEntry = components["schemas"]["ViewCatalogEntry"];

export type EditSessionReadRequest = components["schemas"]["EditSessionReadRequest"];

export type ViewExecuteRequest = components["schemas"]["ViewExecuteRequest"];

export type ViewExecuteReadRequest = ViewExecuteRequest;

export type ViewExecuteResponse = components["schemas"]["ViewExecuteResponse"];

export type ViewExecutionStats = components["schemas"]["ViewExecutionStats"];

export type TypeProfile = components["schemas"]["TypeProfile"];

export type ViewBoardLane = components["schemas"]["ViewBoardLane"];

export type ViewSaveRequest = components["schemas"]["ViewSaveRequest"];

export type ViewSaveResponse = components["schemas"]["ViewSaveResponse"];

export type ViewTableRow = components["schemas"]["ViewTableRow"];

export type ViewTableColumn = components["schemas"]["ViewTableColumn"];

export type ViewFieldCapability = components["schemas"]["ViewFieldCapability"];

export type ViewFieldCandidatesResponse = components["schemas"]["ViewFieldCandidatesResponse"];

export type SuggestResponse = components["schemas"]["SuggestResponse"];

export type SearchResponse = components["schemas"]["SearchResponse"];

export type SearchMatch = components["schemas"]["SearchMatch"];

/** Fields added by the workspace search adapter as the server evolves. */
export type WorkspaceSearchMatch = SearchMatch;

export type WorkspaceSearchResponse = Omit<SearchResponse, "matches"> & {
  matches?: WorkspaceSearchMatch[];
};

export type OntologySummaryResponse = components["schemas"]["OntologySummaryResponse"];

export type OntologyTypeSummary = components["schemas"]["OntologyTypeSummary"];

export type OntologyInterfaceSummary = components["schemas"]["OntologyInterfaceSummary"];

export type OntologyTypeResponse = components["schemas"]["OntologyTypeResponse"];

export type OntologyAtlasResponse = components["schemas"]["OntologyAtlasResponse"] & {
  sections?: TypeDoc[];
};

export type OntologyAtlasTypeEntry = components["schemas"]["OntologyAtlasTypeEntry"];

export type TypeFieldDoc = components["schemas"]["TypeFieldDoc"];

export type EnumDoc = components["schemas"]["EnumDoc"];

export type EnumValueDoc = components["schemas"]["EnumValueDoc"];

export type OntologyNoteListItem = components["schemas"]["OntologyNoteListItem"];

export type OntologyInspectResponse = components["schemas"]["OntologyInspectResponse"];

export type OntologyQuerySchemaResponse = components["schemas"]["OntologyQuerySchemaResponse"];

export type OntologyQueryRequest = components["schemas"]["OntologyQueryRequest"];

export type OntologyQueryReadRequest = OntologyQueryRequest;

export type OntologyQueryResult = components["schemas"]["OntologyQueryResult"];

type OntologyEditSessionStatus = "clean" | "dirty" | "stale" | "rebased" | "conflicted";

export type OntologyEditOp = {
  id?: string;
  kind: string;
  path: string;
  nodeId?: string;
  structuralFingerprint?: string;
  field?: string;
  collection?: string;
  value?: string;
  values?: string[];
  fieldValue?: OntologyEditFieldValue;
  expected?: {
    field?: OntologyEditFieldValue;
    sourceHash?: string;
    sourceContent?: string;
  };
  markdown?: string;
  previousMarkdown?: string;
  rangeStart?: number;
  rangeEnd?: number;
  heading?: string;
  body?: string;
  blockId?: string;
  orderedFragments?: string[];
  oldTarget?: string;
  newTarget?: string;
  property?: string;
  level?: string;
};

export type OntologyEditFieldValue =
  | { kind: "unset" }
  | { kind: "scalar"; scalar: string }
  | { kind: "list"; items: string[] };

type OntologyConflict = {
  kind: string;
  operationId?: string;
  notePath: string;
  nodeRef?: string;
  field?: string;
  message: string;
  currentValues?: string[];
  currentValueKind?: OntologyEditFieldValue["kind"];
};

type OntologyCollectionChange = {
  path: string;
  ref?: NodeRef;
  collection: string;
  orderedFragments?: string[];
};

type OntologyCommitFilePlan = {
  notePath: string;
  baseFingerprint: string;
  currentFingerprint: string;
  updatedFingerprint: string;
  diff?: string;
  rebased?: boolean;
  hasMaterialChange: boolean;
  updatedContentPreview?: string;
};

type OntologyCommitPlan = {
  files: OntologyCommitFilePlan[];
};

export type OntologyEditSessionSnapshot = {
  version?: number;
  revision?: number;
  sessionId?: string;
  ops?: OntologyEditOp[];
  baseFingerprints?: Record<string, string>;
  baseDocuments?: Array<{ notePath: string; fingerprint: string; content: string }>;
};

export type OntologyEditSessionResponse = {
  sessionId: string;
  revision?: number;
  status: OntologyEditSessionStatus;
  outcome?: "committed" | "unchanged" | "conflicted" | "failed";
  ops?: OntologyEditOp[];
  touchedPaths?: string[];
  touchedNodes?: string[];
  touchedNodeRefs?: NodeRef[];
  refLineage?: Array<{ original: NodeRef; preview: NodeRef }>;
  changedFieldsByNode?: Record<string, string[]>;
  changedFieldsByNodeRef?: Record<string, string[]>;
  collectionChanges?: OntologyCollectionChange[];
  baseFingerprints?: Record<string, string>;
  baseDocuments?: Array<{ notePath: string; fingerprint: string; content: string }>;
  stalePaths?: string[];
  rebased?: boolean;
  conflicts?: OntologyConflict[];
  hasUncommittedChanges: boolean;
  plan?: OntologyCommitPlan;
  warnings?: string[];
  createdAt: string;
  updatedAt: string;
  workspaces?: NodeWorkspace[];
};

export type OntologyNoteNode = {
  path: string;
  title: string;
  tags?: string[];
};

export type NodeKind = components["schemas"]["NodeRef"]["kind"];

export type NodeRef = components["schemas"]["NodeRef"];

type NodeRange = {
  start: number;
  end: number;
};

type NodeInlineFieldSpan = {
  key: string;
  value: string;
  lineRange: NodeRange;
  keyRange: NodeRange;
  valueRange: NodeRange;
  wholeRange: NodeRange;
  propertyKey?: string;
};

export type NodeCapabilities = {
  canEdit: boolean;
  canEditFields: boolean;
  canEditCollections: boolean;
  canNavigateChildren: boolean;
  canSubscribe: boolean;
};

export type NodeStatus = {
  dirty: boolean;
  validation: { issueCount: number };
  freshness: { state?: string };
  session: { state?: string };
  hasWarnings: boolean;
};

export type NodeFieldState = {
  id?: string;
  name: string;
  kind: string;
  sourceKind?: string;
  present: boolean;
  status: NodeStatus;
  range: NodeRange;
  valueRanges?: NodeRange[];
  values?: string[];
  links?: NodeFieldLink[];
  inlineSpans?: NodeInlineFieldSpan[];
  sectionNodes?: NodeRef[];
  capability?: NodeFieldCapability;
  issues?: Array<{ code?: string; message?: string; fieldName?: string; nodeRef?: string }>;
};

/** A relation-field value with its resolved target; unresolved values carry no ref or title. */
export type NodeFieldLink = { value: string; ref?: NodeRef; title?: string };

export type NodeFieldEnumOption = { value: string; label: string; tone?: string };

export type NodeFieldCapability = {
  ownerRef: NodeRef;
  ownerType: string;
  typeName: string;
  valueKind: string;
  list: boolean;
  required: boolean;
  enumValues: string[];
  /** Label and tone per enum value, in `enumValues` order. */
  enumOptions?: NodeFieldEnumOption[];
  targetType?: string;
  sourceKind?: string;
  valueOrigin: string;
  identifier: boolean;
  preferredIdentifier: boolean;
  displayImportance: "KEY" | "NORMAL" | "DETAIL";
  writeOperation?: string;
  readOnlyReason?: string;
};

type NodeCollectionItemState = {
  ref: NodeRef;
  range: NodeRange;
};

export type NodeCollectionState = {
  name: string;
  kind: string;
  status: NodeStatus;
  range: NodeRange;
  items?: NodeCollectionItemState[];
  orderFingerprint?: string;
};

export type NodeLocator = components["schemas"]["NodeLocator"];

export type NodeDescriptor = {
  ref: NodeRef;
  resolvedType?: string;
  notePath: string;
  title?: string;
  locator: string;
  nodeLocator?: NodeLocator;
  parentRef?: NodeRef;
  parentTitle?: string;
};

export type NodeContent = {
  path: string;
  title: string;
  resolvedType?: string;
  markdown?: string;
  format?: string;
  sourceRepresentation?: string;
  evidenceRepresentation?: string;
  sourceCapabilities?: string[];
  rendered?: RenderedFile | null;
  assessment?: NoteAssessment;
  typeDoc?: TypeDoc;
  structural?: StructuralView | null;
};

type NodeWorkspaceLoaded = {
  rendered: boolean;
  assessment: boolean;
  structure: boolean;
  relations: boolean;
};

type WorkspaceNodeKind = string;

export type NodeBodyBlockKind = "narrative" | "inline_field" | "child_section" | "collection";

export type NodeBodyBlock = {
  kind: NodeBodyBlockKind;
  range: NodeRange;
  markdown?: string;
  fieldName?: string;
  rawKey?: string;
  childRef?: NodeRef;
  childRefs?: NodeRef[];
  sectionDisplay?: string;
};

type WorkspaceNodeBase = {
  id: string;
  kind: WorkspaceNodeKind;
  ref: NodeRef;
  notePath: string;
  parentId?: string;
  childIds?: string[];
  status: NodeStatus;
  capabilities: NodeCapabilities;
  version?: string;
  body?: NodeBodyBlock[];
};

type WorkspaceSectionBinding = {
  typeName?: string;
  fieldName?: string;
  fieldPath?: string;
  fieldList?: boolean;
  sectionDisplay?: string;
  properties?: Record<string, string>;
  identifierField?: string;
  previewTemplate?: string;
  collapsed?: boolean;
};

type WorkspaceNodeData = {
  title?: string;
  resolvedType?: string;
  markdown?: string;
  locator?: string;
  level?: string;
  blockId?: string;
  fragment?: string;
  binding?: WorkspaceSectionBinding;
};

type WorkspaceFieldNodeData = {
  name: string;
  valueKind: string;
  typeName?: string;
  enumValues?: string[];
  present: boolean;
  values?: string[];
  links?: NodeFieldLink[];
  range: NodeRange;
  valueRanges?: NodeRange[];
  inlineSpans?: NodeInlineFieldSpan[];
  sectionRefs?: NodeRef[];
  capability?: NodeFieldCapability;
  issues?: NodeFieldState["issues"];
};

type WorkspaceCollectionNodeData = {
  name: string;
  itemRefs?: NodeCollectionItemState[];
  orderFingerprint?: string;
  range: NodeRange;
};

/**
 * Content node — notes, sections, and embedded nodes share one shape. The
 * `kind` label stays for informational purposes (and for routing edit ops
 * that care about block-ID semantics), but rendering should read uniformly
 * from `data` regardless of kind.
 */
export type WorkspaceContentNode = WorkspaceNodeBase & {
  kind: "note" | "section" | "embedded";
  data: WorkspaceNodeData;
  field?: never;
  collection?: never;
};

export type WorkspaceFieldNode = WorkspaceNodeBase & {
  kind: "field";
  field: WorkspaceFieldNodeData;
  data?: never;
  collection?: never;
};

export type WorkspaceCollectionNode = WorkspaceNodeBase & {
  kind: "collection";
  collection: WorkspaceCollectionNodeData;
  data?: never;
  field?: never;
};

export type WorkspaceNode = WorkspaceContentNode | WorkspaceFieldNode | WorkspaceCollectionNode;

/** Narrow helper: true when the node carries the uniform content payload. */
export function isContentNode(node: WorkspaceNode): node is WorkspaceContentNode {
  return node.kind === "note" || node.kind === "section" || node.kind === "embedded";
}

export type WorkspaceEdge = {
  kind: string;
  fromId: string;
  toId: string;
  fieldName?: string;
  collectionName?: string;
  relationKey?: string;
  relationLabel?: string;
  scopeNodeId?: string;
  index?: number;
};

type WorkspaceRenderedOutlineView = {
  rootIds?: string[];
};

export type WorkspaceStructuralTabView = {
  key: string;
  label: string;
  fieldName?: string;
  count: number;
  nodeIds?: string[];
};

type WorkspaceStructuralOutlineView = {
  rootId?: string;
  defaultView?: string;
  tabs?: WorkspaceStructuralTabView[];
};

export type WorkspaceRelationGroupsView = {
  scopeNodeId?: string;
  groups?: NoteWorkspaceGroup[];
};

type WorkspaceLocalGraphView = {
  centerNodeIds?: string[];
  nodeIds?: string[];
  emptyReason?: string;
};

type WorkspaceViews = {
  renderedOutline?: WorkspaceRenderedOutlineView;
  structuralOutline?: WorkspaceStructuralOutlineView;
  relationGroups?: WorkspaceRelationGroupsView[];
  localGraph?: WorkspaceLocalGraphView;
};

export type NodeWorkspace = {
  requestedRef: string;
  focusedNodeId?: string;
  node: NodeDescriptor;
  content: NodeContent;
  nodes?: WorkspaceNode[];
  edges?: WorkspaceEdge[];
  views?: WorkspaceViews;
  fields?: NodeFieldState[];
  collections?: NodeCollectionState[];
  relations?: NoteWorkspaceGroup[];
  sectionRelationGroups?: Record<string, NoteWorkspaceGroup[]>;
  loaded: NodeWorkspaceLoaded;
  capabilities: NodeCapabilities;
  status: NodeStatus;
  version: string;
  sourceRevision?: {
    notePath: string;
    contentFingerprint: string;
    content: string;
  };
  nodeLocator?: NodeLocator;
  linkTarget?: components["schemas"]["NodeLinkTarget"];
  linkFixOps?: OntologyEditOp[];
};

export type NodeWorkspaceGraph = Pick<NodeWorkspace, "focusedNodeId" | "nodes" | "edges" | "views">;

export type NodeEvent = {
  id: string;
  kind: string;
  ref: NodeRef;
  /** Fresh identity when `ref` has gone stale, e.g. a renumbered `#item-N`. */
  canonicalRef?: NodeRef;
  version?: string;
  cause?: string;
  changed?: string[];
};

type ModifiedNotesTotals = {
  notes: number;
  ops: number;
  setField?: number;
  setLinkField?: number;
  setNarrative?: number;
  addEmbedded?: number;
  delete?: number;
  reorder?: number;
};

export type ModifiedNoteOpView = {
  id?: string;
  kind: string;
  nodeRef: NodeRef;
  /** Title of the committed node an op inside a note targets. */
  nodeTitle?: string;
  field?: string;
  previousValue?: string;
  value?: string;
  previousValues?: string[];
  values?: string[];
  previousMarkdown?: string;
  markdown?: string;
  rangeStart?: number;
  rangeEnd?: number;
  heading?: string;
  body?: string;
  blockId?: string;
  collection?: string;
  orderedFragments?: string[];
  previousFragments?: string[];
  oldTarget?: string;
  newTarget?: string;
  property?: string;
  level?: string;
};

export type ModifiedNoteEntry = {
  path: string;
  title?: string;
  resolvedType?: string;
  diff?: string;
  baseFingerprint?: string;
  currentFingerprint?: string;
  updatedFingerprint?: string;
  rebased?: boolean;
  hasMaterialChange: boolean;
  ops?: ModifiedNoteOpView[];
};

export type ModifiedNotesResponse = {
  sessionId: string;
  totals: ModifiedNotesTotals;
  notes: ModifiedNoteEntry[];
  conflicts?: OntologyConflict[];
  rebased?: boolean;
  stalePaths?: string[];
  updatedAt: string;
};

// --- Validation snapshot types ---

export type ValidationHealth = components["schemas"]["ValidationHealth"];

export type ValidationSnapshot = components["schemas"]["ValidationSnapshot"];

export type ValidationCheckSnapshot = components["schemas"]["ValidationCheckSnapshot"];

export type ValidationActionSnapshot = components["schemas"]["ValidationActionSnapshot"];

export type ValidationDiagnosticPage = components["schemas"]["ValidationDiagnosticPage"];

export type ValidationDiagnostic = components["schemas"]["ValidationDiagnostic"];

export type ValidationDiagnosticsParams =
  operations["getPublicValidationDiagnostics"]["parameters"]["query"];

export type ValidationScope = components["schemas"]["ValidationScope"];

export type ValidationScopeSummary = components["schemas"]["ValidationScopeSummary"];

export type ValidationScopeSummaryRequest = components["schemas"]["ValidationScopeSummaryRequest"];

export type ValidationScopeSummaryResponse =
  components["schemas"]["ValidationScopeSummaryResponse"];

export type ValidationDiagnosticFilter = components["schemas"]["ValidationDiagnosticFilter"];

export type ValidationIssueVariant = components["schemas"]["ValidationIssueVariant"];

export type ValidationIssueGroup = components["schemas"]["ValidationIssueGroup"];

export type ValidationIssueGroupRequest = components["schemas"]["ValidationIssueGroupRequest"];

export type ValidationIssueGroupResponse = components["schemas"]["ValidationIssueGroupResponse"];

export type ValidationRepairReviewCreateRequest =
  components["schemas"]["ValidationRepairReviewCreateRequest"];

export type ValidationRepairReviewApplyRequest =
  components["schemas"]["ValidationRepairReviewApplyRequest"];

export type ValidationRepairReview = components["schemas"]["RepairReview"];

export type ValidationRepairApplyResponse = components["schemas"]["ValidationRepairApplyResponse"];

export type ValidateEnvelope = components["schemas"]["ValidateEnvelope"];

export type AgentHarnessKind = "codex" | "claude";

export type AgentPermissionMode = "approval-required" | "auto-accept-edits" | "full-access";

export type AgentHarnessSettings = {
  model?: string;
  effort?: string;
  permissionMode: AgentPermissionMode;
};

export type AgentSettings = {
  harness?: AgentHarnessKind;
  harnesses: Record<AgentHarnessKind, AgentHarnessSettings>;
};

export type AgentHarnessStatus = {
  installed: boolean;
  version?: string;
  loggedIn: boolean;
  account?: string;
  models?: AgentModelOption[];
  efforts?: string[];
  capabilities: AgentHarnessCapabilities;
  lastError?: string;
  loginHint?: string;
};

export type AgentModelOption = {
  id: string;
  displayName: string;
  efforts?: string[];
  default?: boolean;
};

export type AgentHarnessCapabilities = {
  supportsAllowedTools: boolean;
  supportsAllowForSession: boolean;
  permissionModes: AgentPermissionMode[];
};

export type AgentSettingsResponse = {
  settings: AgentSettings;
  resolved: {
    harness?: AgentHarnessKind;
    reason: "configured" | "first available" | "none available";
  };
  harnesses: { kind: AgentHarnessKind; status: AgentHarnessStatus }[];
};

export type AgentSession = {
  id: string;
  title?: string;
  harness?: AgentHarnessKind;
  harnessSessionId?: string;
  model?: string;
  readOnly?: boolean;
  turnRunning?: boolean;
  createdAt: string;
  updatedAt: string;
  archived?: boolean;
};

export type AgentMessage = {
  id: number;
  sessionId: string;
  role: "user" | "assistant" | string;
  content: string;
  createdAt: string;
};

export type AgentEvent = {
  id: number;
  sessionId: string;
  type: string;
  role?: string;
  content?: string;
  toolName?: string;
  result?: {
    turnId?: string;
    itemId?: string;
    requestId?: string;
    name?: string;
    command?: string;
    cwd?: string;
    paths?: string[];
    status?: string;
    phase?: string;
    reason?: string;
    allowForSession?: boolean;
    input?: JsonValue;
    output?: JsonValue;
    truncated?: boolean;
    exitCode?: number;
    diff?: string;
    usage?: {
      inputTokens?: number;
      cachedInputTokens?: number;
      outputTokens?: number;
      reasoningOutputTokens?: number;
      totalTokens?: number;
    };
  };
  error?: string;
  createdAt: string;
};

export type AgentSessionResponse = {
  session: AgentSession;
  messages?: AgentMessage[];
  events?: AgentEvent[];
};

export type AgentSessionsResponse = {
  sessions?: AgentSession[];
};

export type AgentSendMessageResponse = {
  session: AgentSession;
  events?: AgentEvent[];
};

export type AgentApprovalDecision = "allow" | "deny" | "allow-for-session";
