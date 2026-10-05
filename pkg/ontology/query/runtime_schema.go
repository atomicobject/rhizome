package query

import (
	"fmt"
	"strings"
)

func runtimeRootFieldsSDL() []string {
	return []string{
		"ontology: OntologyRuntime!",
		"code: CodeRuntime!",
	}
}

func builtinRuntimeRoots() map[string]RuntimeRootKind {
	return map[string]RuntimeRootKind{
		"ontology": RuntimeRootOntology,
		"code":     RuntimeRootCode,
	}
}

func runtimeTypesSDL() string {
	return strings.TrimSpace(`
type RuntimeWarning {
  code: String
  message: String
  path: String
}

enum IdentifierStrategy { SEQUENTIAL DATETIME }

type IdentifierFormat {
  strategy: IdentifierStrategy!
  prefix: String
  separator: String
  pad: Int
}

type OntologyField {
  name: String!
  type: String!
  kind: String
  required: Boolean!
  list: Boolean!
  source: String
  description: String
  identifierFormat: IdentifierFormat
  capability: OntologyFieldCapability!
}

type OntologyFieldCapability {
  valueKind: String
  filterOps: [String!]!
  indexedFilterOps: [String!]!
  residualFilterOps: [String!]!
  sortable: Boolean!
  groupable: Boolean!
  targetType: String
  enumName: String
}

type OntologyType {
  name: String!
  role: String!
  description: String
  paths: [String!]!
  interfaces: [String!]!
  fields: [OntologyField!]!
}

type OntologyAuthoringGuide {
  type: String
  markdown: String!
  available: Boolean!
  warnings: [RuntimeWarning!]!
}

type OntologyNextID {
  type: String
  identifierField: String
  strategy: IdentifierStrategy
  source: String
  prefix: String
  separator: String
  pad: Int
  next: String
  ids: [String!]!
  count: Int
  last: String
  currentMax: Int
  currentMaxValue: String
  currentMaxOwner: String
  ownersScanned: Int
  sharedWith: [String!]!
  ownersMatched: [String!]!
  ownersSkipped: [String!]!
  notes: [String!]!
  paths: [String!]!
  allocations: [OntologyIDAllocation!]!
  available: Boolean!
  errorCode: String
  error: String
  warnings: [RuntimeWarning!]!
}

type OntologyIDAllocation {
  path: String!
  id: String!
  base: String
  disambiguator: Int
}

type OntologyCurrentUser {
  configured: Boolean!
  ref: String
  found: Boolean!
  resolvedRef: String
  path: String
  title: String
  typeName: String
  errorCode: String
  error: String
  warnings: [RuntimeWarning!]!
}

type OntologyQueryPlan {
  type: String!
  root: String!
  execution: String!
  projection: String!
  first: Int!
  pushedFilters: [String!]!
  residualFilters: [String!]!
  pushedSort: [String!]!
  residualSort: [String!]!
  warnings: [RuntimeWarning!]!
}

type OntologyRuntime {
  schemaHash: String!
  type(name: String!): OntologyType
  types(first: Int = 50): [OntologyType!]!
  authoringGuide(type: String!): OntologyAuthoringGuide!
  nextId(type: String!, count: Int = 1, paths: [String!] = []): OntologyNextID!
  currentUser: OntologyCurrentUser!
  queryPlan(type: String!, first: Int = 20, filters: [FieldFilterInput!], sort: [SortInput!], select: [String!]): OntologyQueryPlan!
}

type RuntimePath {
  path: String!
  title: String
  kind: String
  reason: String
  snippet: String
  content: String
  line: Int
  truncated: Boolean!
}

type CodeContextPack {
  inputPath: String
  normalizedPath: String
  available: Boolean!
  warnings: [RuntimeWarning!]!
  docs: [RuntimePath!]!
  notes: [RuntimePath!]!
  code: [RuntimePath!]!
  tests: [RuntimePath!]!
}

type CodeRuntime {
  docsForCode(path: String!, first: Int = 20): CodeContextPack!
  codeForNote(path: String!, first: Int = 20): CodeContextPack!
  testsForCode(path: String!, first: Int = 20): CodeContextPack!
}

`) + "\n\n"
}

func runtimeTypeNames() map[string]struct{} {
	return map[string]struct{}{
		"RuntimeWarning":          {},
		"NodeKind":                {},
		"FieldFilterOperator":     {},
		"SortDirection":           {},
		"TraversalDirection":      {},
		"GraphProfile":            {},
		"NodeRef":                 {},
		"NodeRefResolution":       {},
		"NodeLinkTarget":          {},
		"NodeLocatorDiagnostic":   {},
		"NodeLinkFixAction":       {},
		"NodeLocator":             {},
		"Node":                    {},
		"Note":                    {},
		"NoteNode":                {},
		"Section":                 {},
		"UntypedSection":          {},
		"CodeFile":                {},
		"CodeSymbol":              {},
		"NodeNeighborhood":        {},
		"NodeRelationEdge":        {},
		"LocalGraph":              {},
		"LocalGraphNode":          {},
		"LocalGraphEdge":          {},
		"PageInfo":                {},
		"NodeBatchResult":         {},
		"NodeBatchItem":           {},
		"NoteConnection":          {},
		"NodeSearchResult":        {},
		"ValidationState":         {},
		"ValidationCheck":         {},
		"ValidationIssue":         {},
		"NodeWorkspaceProjection": {},
		"NodeBodyProjection":      {},
		"NodeWorkspaceSourceLink": {},
		"NodeBodyBinding":         {},
		"NodeBodyBlock":           {},
		"NodeRange":               {},
		"NodeFieldState":          {},
		"NodeFieldLink":           {},
		"NodeFieldCapability":     {},
		"FieldDisplayImportance":  {},
		"NodeSourceRevision":      {},
		"NodeCollectionState":     {},
		"NodeCollectionItem":      {},
		"NodeCapabilities":        {},
		"NodeStatus":              {},
		"NodeValidationStatus":    {},
		"NodeFreshnessStatus":     {},
		"NodeSessionStatus":       {},
		"NodeAssessment":          {},
		"NodeAssessmentIssue":     {},
		"NodeFieldAssessment":     {},
		"NodeRelationAssessment":  {},
		"NodeRelationTarget":      {},
		"NodeStructure":           {},
		"NodeRelationGroup":       {},
		"NodeRelationItem":        {},
		"NodeLoadedDomains":       {},
		"IdentifierFormat":        {},
		"IdentifierStrategy":      {},
		"OntologyField":           {},
		"OntologyFieldCapability": {},
		"OntologyType":            {},
		"OntologyAuthoringGuide":  {},
		"OntologyNextID":          {},
		"OntologyIDAllocation":    {},
		"OntologyCurrentUser":     {},
		"OntologyRuntime":         {},
		"RuntimePath":             {},
		"CodeContextPack":         {},
		"CodeRuntime":             {},
	}
}

func validateRuntimeRootCollision(root string, owner string, used map[string]string) error {
	if prev, ok := used[root]; ok {
		return fmt.Errorf("generated query root %q for type %s conflicts with %s", root, owner, prev)
	}
	return nil
}
