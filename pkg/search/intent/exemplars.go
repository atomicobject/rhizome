package intent

import "github.com/atomicobject/rhizome/pkg/search"

// DefaultExemplars defines short query examples per intent for embedding-based detection.
// Keep these concise and representative; they are embedded once per process.
func DefaultExemplars() map[search.Intent][]string {
	return map[search.Intent][]string{
		search.IntentSearch: {
			"find search pipeline code",
			"where is the cache service used",
			"how does semantic_query work",
			"show me ranking logic",
		},
		search.IntentDocsForCode: {
			"docs for pkg/search/service.go",
			"what are the invariants for Service.Search",
			"documentation for this module",
			"read the design notes for planner",
		},
		search.IntentCodeForDocs: {
			"code behind this doc",
			"where is this design implemented",
			"find code referenced by these notes",
			"show implementation for the docs",
		},
		search.IntentRelatedToSeed: {
			"related to this file",
			"show neighbors of this module",
			"what else touches this directory",
			"find connected notes for this code",
		},
		search.IntentConsolidation: {
			"merge duplicate docs for this topic",
			"consolidate overlapping notes",
			"what should be merged here",
		},
		search.IntentOverview: {
			"overview of this module",
			"architecture of the search pipeline",
			"high level summary of this package",
			"explain the design at a glance",
		},
		search.IntentFindUsages: {
			"find usages of this symbol",
			"where is this function called",
			"who references this type",
			"show call sites for this method",
		},
		search.IntentCallers: {
			"callers of this function",
			"who calls this method",
			"find incoming calls",
		},
		search.IntentCallees: {
			"callees of this function",
			"what does this method call",
			"outgoing calls for this symbol",
		},
		search.IntentGoToDef: {
			"go to definition for this symbol",
			"definition of this function",
			"where is this type defined",
		},
		search.IntentExplainSymbol: {
			"explain this function",
			"what does this method do",
			"explain this class",
		},
		search.IntentTestsForCode: {
			"tests for this file",
			"where are the tests for this function",
			"find test coverage for this module",
		},
		search.IntentRefactorImpact: {
			"refactor impact for this module",
			"what would change if I rename this",
		},
		search.IntentImplementers: {
			"implementers of this interface",
			"classes implementing this type",
		},
		search.IntentOverrides: {
			"overrides of this method",
			"where is this method overridden",
		},
		search.IntentImports: {
			"who imports this package",
			"find importers of this module",
		},
		search.IntentDataFlow: {
			"data flow for this symbol",
			"trace data flow for this function",
		},
		search.IntentSecurityAudit: {
			"security audit for this module",
			"find security-sensitive call paths",
		},
	}
}
