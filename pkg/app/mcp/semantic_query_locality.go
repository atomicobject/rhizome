package mcp

import "github.com/atomicobject/rhizome/pkg/search"

func buildSemanticQueryIntent(intent search.Intent) semanticQueryIntent {
	out := semanticQueryIntent{}
	if intent == search.IntentOverview || intent == search.IntentSubsystemOverview {
		out.Overview = true
	}
	if intent == search.IntentDocsForCode || intent == search.IntentOverview || intent == search.IntentSubsystemOverview || intent == search.IntentExplainSymbol {
		out.PreferDocs = true
	}
	if intent == search.IntentCodeForDocs ||
		intent == search.IntentGoToDef ||
		intent == search.IntentFindUsages ||
		intent == search.IntentCallers ||
		intent == search.IntentCallees ||
		intent == search.IntentTestsForCode ||
		intent == search.IntentExplainSymbol ||
		intent == search.IntentImplementers ||
		intent == search.IntentOverrides ||
		intent == search.IntentImports ||
		intent == search.IntentDataFlow ||
		intent == search.IntentRefactorImpact ||
		intent == search.IntentSecurityAudit {
		out.PreferCode = true
	}
	if intent == search.IntentTestsForCode {
		out.Tests = true
	}
	if isFetchIntent(intent) {
		out.Fetch = true
	}
	return out
}

func recordSemanticSelection(ctx *semanticSelectionContext, group semanticQueryGroup, groupKey, summaryKey string, plan contentPlan, intent semanticQueryIntent, docs docMatcher) {
	if ctx == nil {
		return
	}
	if summaryKey != "" && plan != planStub {
		ctx.selectedSummaries = append(ctx.selectedSummaries, summaryKey)
	}
	if docs.IsDoc(group.match.Path, group.match.Type) {
		if plan != planStub {
			ctx.selectedGroups[groupKey]++
		}
		return
	}
	if group.match.Type == "code" && !isTestPath(group.match.Path) {
		ctx.selectedCode++
	}
}
