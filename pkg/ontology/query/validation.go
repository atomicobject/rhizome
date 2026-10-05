package query

import (
	"context"
	"fmt"
	"strings"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
	validationcatalog "github.com/atomicobject/rhizome/pkg/validate/catalog"
	"github.com/vektah/gqlparser/v2/ast"
)

func (e *executor) resolveValidationSelectionSet(ctx context.Context, field *ast.Field, path []string) map[string]any {
	args := e.fieldArgs(field)
	state := e.validationState(ctx)
	selectionReady := validationSelectionReady(state)
	checkFilter := strings.TrimSpace(stringValue(args["check"]))
	selectedCheck := ""
	selectionValid := true
	if checkFilter != "" {
		canonical, ok := validationcatalog.Canonical(checkFilter)
		if !ok {
			selectionValid = false
			e.addValidationSelectionError(path, "unknown validation check", checkFilter, "unknown_validation_check")
			state.SelectedChecks = []string{}
			state.Checks = []validationCheckResult{}
			state.IssueCount = 0
			state.ErrorCount = 0
			state.OK = false
		} else {
			selectedCheck = canonical
			filtered := make([]validationCheckResult, 0, 1)
			for _, check := range state.Checks {
				checkCanonical, known := validationcatalog.Canonical(check.Name)
				if known && checkCanonical == canonical {
					filtered = append(filtered, check)
				}
			}
			selected := selectedValidationChecks(state.SelectedChecks, canonical)
			if len(filtered) == 0 || len(selected) == 0 {
				selectionValid = false
				e.addValidationSelectionError(path, "validation check is unavailable", checkFilter, "validation_check_unavailable")
			}
			state.SelectedChecks = selected
			state.Checks = filtered
			state.IssueCount, state.ErrorCount = validationCounts(state.Checks)
			state.OK = selectionReady && len(filtered) > 0 && validationCheckResultsSuccessful(filtered) && state.ErrorCount == 0 && state.IssueCount == 0
		}
	}
	scope, scopeOK := validationScopeFromArgs(args)
	state.Scope = scope
	if !scopeOK {
		e.addErrorWithExtensions(path, "validation scope kind and key are invalid", map[string]any{"code": "validation_scope_invalid"})
		state.OK = false
		state.IssueCount = 0
		state.AffectedFileCount = 0
		state.AffectedNoteCount = 0
		state.RepairActionCount = 0
	} else if selectionValid && state.PublishedGeneration > 0 {
		store, _ := e.deps.Store.(validationResultStore)
		summaries, err := store.GetValidationScopeSummaries(ctx, semdb.ValidationScopeSummaryRequest{
			Generation: state.PublishedGeneration, Scopes: []semdb.ValidationScope{scope},
			Filter: semdb.ValidationDiagnosticFilter{Check: selectedCheck, InterfaceImplementors: ontology.InterfaceImplementors(e.schema)},
		})
		if err != nil {
			e.addErrorWithExtensions(path, err.Error(), map[string]any{"code": "validation_scope_unavailable"})
			state.OK = false
		} else if len(summaries.Summaries) == 1 {
			summary := summaries.Summaries[0]
			state.IssueCount = summary.IssueCount
			state.AffectedFileCount = summary.AffectedFileCount
			state.AffectedNoteCount = summary.AffectedNoteCount
			state.RepairActionCount = summary.RepairActionCount
			state.OK = selectionReady && validationCheckResultsSuccessful(state.Checks) && summary.IssueCount == 0
		}
	}
	firstIssues, validLimit := validationIssueLimit(args["firstIssues"], semdb.ValidationDiagnosticDefaultPageSize)
	if !validLimit {
		e.addErrorWithExtensions(path, "validation issue limit must be between 1 and 200", map[string]any{"code": "validation_page_limit_invalid"})
	}
	out := make(map[string]any)
	invalidNestedLimitReported := false
	for _, selection := range field.SelectionSet {
		current, ok := selection.(*ast.Field)
		if !ok {
			continue
		}
		key := responseKey(current)
		switch current.Name {
		case "status":
			out[key] = state.Status
		case "generation":
			out[key] = int(state.Generation)
		case "publishedGeneration":
			out[key] = int(state.PublishedGeneration)
		case "completion":
			out[key] = emptyNil(state.Completion)
		case "staleReason":
			out[key] = emptyNil(state.StaleReason)
		case "error":
			if state.Error == "" {
				out[key] = nil
			} else {
				out[key] = state.Error
			}
		case "ok":
			out[key] = state.OK
		case "issueCount":
			out[key] = state.IssueCount
		case "errorCount":
			out[key] = state.ErrorCount
		case "affectedFileCount":
			out[key] = state.AffectedFileCount
		case "affectedNoteCount":
			out[key] = state.AffectedNoteCount
		case "repairActionCount":
			out[key] = state.RepairActionCount
		case "scope":
			out[key] = e.resolveValidationScopeSelectionSet(state.Scope, current.SelectionSet, append(path, key))
		case "selectedChecks":
			out[key] = stringsSliceAny(state.SelectedChecks)
		case "checks":
			checks := make([]any, 0, len(state.Checks))
			for _, check := range state.Checks {
				if scope.Kind != semdb.ValidationScopeGlobal && scopeOK {
					check.IssueCount = e.validationCheckScopeCount(ctx, state.PublishedGeneration, check.Name, scope, append(path, key))
					check.OK = check.Outcome == semdb.ValidationCheckOutcomeCompleted && check.IssueCount == 0
				}
				checks = append(checks, e.resolveValidationCheckSelectionSet(ctx, state.PublishedGeneration, scope, check, firstIssues, validLimit, &invalidNestedLimitReported, current.SelectionSet, append(path, key)))
			}
			out[key] = checks
		default:
			e.addError(append(path, key), fmt.Sprintf("field %q does not exist on ValidationState", current.Name))
		}
	}
	return out
}

func validationSelectionReady(state validationStateResult) bool {
	return state.Status == semdb.ValidationStatusOK &&
		state.Completion == semdb.ValidationCompletionComplete &&
		strings.TrimSpace(state.StaleReason) == "" &&
		strings.TrimSpace(state.Error) == ""
}

func validationCheckResultsSuccessful(checks []validationCheckResult) bool {
	for _, check := range checks {
		if check.Outcome != semdb.ValidationCheckOutcomeCompleted && check.Outcome != semdb.ValidationCheckOutcomeNotApplicable {
			return false
		}
	}
	return true
}

func validationScopeFromArgs(args map[string]any) (semdb.ValidationScope, bool) {
	kind := strings.TrimSpace(stringValue(args["scopeKind"]))
	key := strings.TrimSpace(stringValue(args["scopeKey"]))
	if kind == "" {
		kind = semdb.ValidationScopeGlobal
	}
	scope := semdb.ValidationScope{Kind: kind, Key: key}
	switch kind {
	case semdb.ValidationScopeGlobal:
		return scope, key == ""
	case semdb.ValidationScopeFile, semdb.ValidationScopeNote, semdb.ValidationScopeNode, semdb.ValidationScopeType, semdb.ValidationScopeInterface:
		return scope, key != ""
	default:
		return scope, false
	}
}

func (e *executor) validationCheckScopeCount(ctx context.Context, generation int64, check string, scope semdb.ValidationScope, path []string) int {
	store, ok := e.deps.Store.(validationResultStore)
	if !ok || generation <= 0 {
		return 0
	}
	page, err := store.GetValidationDiagnosticsPage(ctx, semdb.ValidationDiagnosticPageRequest{
		Generation: generation, Limit: 1,
		Filter: semdb.ValidationDiagnosticFilter{Check: check, ScopeKind: scope.Kind, ScopeKey: scope.Key, InterfaceImplementors: ontology.InterfaceImplementors(e.schema)},
	})
	if err != nil {
		e.addErrorWithExtensions(path, err.Error(), map[string]any{"code": "validation_diagnostics_unavailable"})
		return 0
	}
	return page.Total
}

func (e *executor) resolveValidationScopeSelectionSet(scope semdb.ValidationScope, set ast.SelectionSet, path []string) map[string]any {
	out := make(map[string]any)
	for _, selection := range set {
		field, ok := selection.(*ast.Field)
		if !ok {
			continue
		}
		key := responseKey(field)
		switch field.Name {
		case "kind":
			out[key] = scope.Kind
		case "key":
			out[key] = emptyNil(scope.Key)
		default:
			e.addError(append(path, key), fmt.Sprintf("field %q does not exist on ValidationScope", field.Name))
		}
	}
	return out
}

func selectedValidationChecks(selected []string, canonical string) []string {
	result := make([]string, 0, 1)
	for _, check := range selected {
		resolved, ok := validationcatalog.Canonical(check)
		if ok && resolved == canonical {
			result = append(result, check)
		}
	}
	return result
}

func validationCounts(checks []validationCheckResult) (int, int) {
	issueCount := 0
	errorCount := 0
	for _, check := range checks {
		issueCount += check.IssueCount
		if check.Error != "" {
			errorCount++
		}
	}
	return issueCount, errorCount
}

func (e *executor) addValidationSelectionError(path []string, reason, selector, code string) {
	e.addErrorWithExtensions(path, fmt.Sprintf("%s %q", reason, selector), map[string]any{
		"code":     code,
		"selector": selector,
	})
}
