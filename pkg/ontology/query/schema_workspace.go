package query

import (
	"strings"
)

func workspaceProjectionTypesSDL() string {
	return strings.TrimSpace(`
"Composable source-preserving metadata used by node workspace clients."
type NodeWorkspaceProjection {
  "Schema-aware semantic parent of the focused node, skipping structural wrappers."
  parentRef: NodeRef
  "Display title of parentRef, read from the index."
  parentTitle: String
  fields: [NodeFieldState!]!
  collections: [NodeCollectionState!]!
  "Source-preserving structural nodes, capped at 200 per workspace selection."
  bodies(first: Int = 200): [NodeBodyProjection!]!
  "Source-ordered authored links and embeds, capped at 100 per workspace selection."
  sourceLinks(first: Int = 100): [NodeWorkspaceSourceLink!]!
  capabilities: NodeCapabilities!
  status: NodeStatus!
  version: String!
  "Full source revision captured by the same projection as the editable values."
  sourceRevision: NodeSourceRevision!
  assessment: NodeAssessment
  structure: [NodeStructure!]!
  relationGroups: [NodeRelationGroup!]!
  loaded: NodeLoadedDomains!
}

"Source-preserving structural nodes for reconstructing a workspace body graph."
type NodeBodyProjection {
  ref: NodeRef!
  title: String!
  resolvedType: String
  locator: String!
  parentRef: NodeRef
  markdown: String!
  level: String
  blockId: String
  fragment: String
  binding: NodeBodyBinding
  fields: [NodeFieldState!]!
  collections: [NodeCollectionState!]!
  blocks: [NodeBodyBlock!]!
}

"One authored internal link or embed in source order."
type NodeWorkspaceSourceLink {
  target: String!
  authoredTarget: String!
  text: String
  kind: String!
  anchor: String
  embed: Boolean!
  targetKind: String!
  resolved: Boolean!
  resolvedRef: NodeRef
  title: String
  preview: String
}

"Schema-derived placement and display metadata for one structural body node."
type NodeBodyBinding {
  typeName: String
  fieldName: String
  fieldPath: String
  fieldList: Boolean!
  sectionDisplay: String
  properties: JSON
  identifierField: String
  previewTemplate: String
  collapsed: Boolean!
}

"One source-ordered block in a structural node body."
type NodeBodyBlock {
  kind: String!
  range: NodeRange!
  markdown: String
  fieldName: String
  rawKey: String
  childRef: NodeRef
  childRefs: [NodeRef!]!
  sectionDisplay: String
}

type NodeRange {
  start: Int!
  end: Int!
}

type NodeFieldState {
  name: String!
  kind: String!
  sourceKind: String
  present: Boolean!
  status: NodeStatus!
  range: NodeRange!
  valueRanges: [NodeRange!]!
  values: [String!]!
  "One entry per value of a relation field, in values order, with its resolved target. Empty for other fields."
  links: [NodeFieldLink!]!
  sectionNodes: [NodeRef!]!
  capability: NodeFieldCapability!
  issues: [NodeAssessmentIssue!]!
}

"One authored relation-field value. Unresolved or ambiguous values keep ref and title null."
type NodeFieldLink {
  value: String!
  resolved: Boolean!
  ref: NodeRef
  title: String
}

"Authoritative schema and authoring metadata for one field on its owning node."
type NodeFieldCapability {
  ownerRef: NodeRef!
  ownerType: String!
  typeName: String!
  valueKind: String!
  list: Boolean!
  required: Boolean!
  enumValues: [String!]!
  "Display label and tone for each enum value, in enumValues order."
  enumOptions: [NodeFieldEnumOption!]!
  targetType: String
  sourceKind: String
  valueOrigin: String!
  identifier: Boolean!
  preferredIdentifier: Boolean!
  displayImportance: FieldDisplayImportance!
  writeOperation: String
  readOnlyReason: String
}

"One admitted enum value. label is the @view(label:) or the raw value; stage is its declared or inferred lifecycle stage."
type NodeFieldEnumOption {
  value: String!
  label: String!
  tone: String
  stage: String
}

type NodeSourceRevision {
  notePath: String!
  contentFingerprint: String!
  content: String!
}

type NodeCollectionState {
  name: String!
  kind: String!
  status: NodeStatus!
  range: NodeRange!
  items: [NodeCollectionItem!]!
  orderFingerprint: String
}

type NodeCollectionItem {
  ref: NodeRef!
  range: NodeRange!
}

type NodeCapabilities {
  canEdit: Boolean!
  canEditFields: Boolean!
  canEditCollections: Boolean!
  canNavigateChildren: Boolean!
  canSubscribe: Boolean!
}

type NodeStatus {
  dirty: Boolean!
  validation: NodeValidationStatus!
  freshness: NodeFreshnessStatus!
  session: NodeSessionStatus!
  hasWarnings: Boolean!
}

type NodeValidationStatus { issueCount: Int! }
type NodeFreshnessStatus { state: String }
type NodeSessionStatus { state: String }

type NodeAssessment {
  notePath: String!
  declaredType: String
  resolvedType: String
  candidateTypes: [String!]!
  issues: [NodeAssessmentIssue!]!
  fields: [NodeFieldAssessment!]!
  relations: [NodeRelationAssessment!]!
}

type NodeAssessmentIssue {
  code: String
  notePath: String
  typeName: String
  fieldName: String
  nodeRef: String
  nodeId: String
  line: Int
  message: String!
}

type NodeFieldAssessment {
  name: String!
  description: String
  kind: String!
  typeName: String!
  required: Boolean!
  list: Boolean!
  source: String
  sourceKind: String
  present: Boolean!
  values: [String!]!
  validValues: [String!]!
  issues: [NodeAssessmentIssue!]!
}

type NodeRelationAssessment {
  name: String!
  description: String
  kind: String!
  typeName: String!
  required: Boolean!
  list: Boolean!
  source: String
  sourceKind: String
  direction: String
  present: Boolean!
  values: [String!]!
  targets: [NodeRelationTarget!]!
  issues: [NodeAssessmentIssue!]!
}

type NodeRelationTarget {
  path: String!
  typeName: String
  provenance: String
  structural: Boolean!
}

type NodeStructure {
  ref: NodeRef!
  parentRef: NodeRef
  title: String!
  level: String
  content: String
}

type NodeRelationGroup {
  key: String!
  label: String!
  ownerTitle: String
  navigation: Boolean!
  items: [NodeRelationItem!]!
}

type NodeRelationItem {
  ref: NodeRef!
  title: String!
  targetTitle: String
  resolvedType: String
  relationName: String
  provenance: String
  direction: TraversalDirection
  structural: Boolean!
  current: Boolean!
}

type NodeLoadedDomains {
  rendered: Boolean!
  assessment: Boolean!
  structure: Boolean!
  relations: Boolean!
}
`) + "\n\n"
}
