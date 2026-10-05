package query

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/vektah/gqlparser/v2/ast"
)

type validationStateResult struct {
	Status              string
	Generation          int64
	PublishedGeneration int64
	Completion          string
	StaleReason         string
	Error               string
	OK                  bool
	IssueCount          int
	ErrorCount          int
	AffectedFileCount   int
	AffectedNoteCount   int
	RepairActionCount   int
	Scope               semdb.ValidationScope
	SelectedChecks      []string
	Checks              []validationCheckResult
}

type validationCheckResult struct {
	Name       string            `json:"name"`
	Outcome    string            `json:"outcome"`
	OK         bool              `json:"ok"`
	Skipped    bool              `json:"skipped,omitempty"`
	IssueCount int               `json:"issueCount"`
	Summary    string            `json:"summary,omitempty"`
	Error      string            `json:"error,omitempty"`
	Issues     []validationIssue `json:"issues,omitempty"`
}

type validationIssue struct {
	IssueKey           string
	Check              string
	Code               string          `json:"code,omitempty"`
	Path               string          `json:"path,omitempty"`
	Field              string          `json:"field,omitempty"`
	Source             string          `json:"source,omitempty"`
	Target             string          `json:"target,omitempty"`
	Message            string          `json:"message,omitempty"`
	Line               int             `json:"line,omitempty"`
	Data               json.RawMessage `json:"data,omitempty"`
	AffectedPaths      []string
	AffectedNotePaths  []string
	AffectedNodeIDs    []string
	AffectedTypes      []string
	AffectedInterfaces []string
	ActionIDs          []string
}

func (e *executor) validationState(ctx context.Context) validationStateResult {
	store, ok := e.deps.Store.(validationResultStore)
	if !ok || store == nil {
		return emptyValidationState(semdb.ValidationStatusNeverRan, 0, "")
	}
	read, err := store.GetValidationStateSnapshot(ctx)
	if err != nil {
		return emptyValidationState(semdb.ValidationStatusError, 0, err.Error())
	}
	state := read.State
	if !read.HasSnapshot {
		return emptyValidationState(state.Status, state.Generation, state.Error)
	}
	snapshot := read.Snapshot
	checks := make([]validationCheckResult, 0, len(snapshot.Checks))
	for _, check := range snapshot.Checks {
		checks = append(checks, validationCheckResult{
			Name: check.Check, Outcome: check.Outcome,
			OK:         check.Outcome == semdb.ValidationCheckOutcomeCompleted && check.IssueCount == 0,
			Skipped:    check.Outcome == semdb.ValidationCheckOutcomeSkipped,
			IssueCount: check.IssueCount, Summary: check.Summary, Error: check.Error,
		})
	}
	return validationStateResult{
		Status: state.Status, Generation: state.Generation, PublishedGeneration: snapshot.Generation,
		Completion: snapshot.Completion, StaleReason: snapshot.StaleReason, Error: state.Error,
		OK: state.Status == semdb.ValidationStatusOK && snapshot.Completion == semdb.ValidationCompletionComplete &&
			strings.TrimSpace(snapshot.StaleReason) == "" && snapshot.IssueCount == 0 && snapshot.ErrorCount == 0 && validationChecksSuccessful(snapshot.Checks),
		IssueCount: snapshot.IssueCount, ErrorCount: snapshot.ErrorCount,
		AffectedFileCount: snapshot.AffectedFileCount, AffectedNoteCount: snapshot.AffectedNoteCount,
		RepairActionCount: snapshot.RepairActionCount, Scope: semdb.ValidationScope{Kind: semdb.ValidationScopeGlobal},
		SelectedChecks: append([]string(nil), snapshot.SelectedChecks...), Checks: checks,
	}
}

func validationChecksSuccessful(checks []semdb.ValidationCheckSnapshot) bool {
	for _, check := range checks {
		if check.Outcome != semdb.ValidationCheckOutcomeCompleted && check.Outcome != semdb.ValidationCheckOutcomeNotApplicable {
			return false
		}
	}
	return true
}

func emptyValidationState(status string, generation int64, message string) validationStateResult {
	if strings.TrimSpace(status) == "" {
		status = semdb.ValidationStatusNeverRan
	}
	return validationStateResult{
		Status:         status,
		Generation:     generation,
		Error:          message,
		OK:             false,
		SelectedChecks: []string{},
		Checks:         []validationCheckResult{},
	}
}

func (e *executor) resolveValidationCheckSelectionSet(ctx context.Context, generation int64, scope semdb.ValidationScope, check validationCheckResult, firstIssues int, validLimit bool, invalidNestedLimitReported *bool, set ast.SelectionSet, path []string) map[string]any {
	out := make(map[string]any)
	for _, selection := range set {
		field, ok := selection.(*ast.Field)
		if !ok {
			continue
		}
		key := responseKey(field)
		switch field.Name {
		case "name":
			out[key] = check.Name
		case "ok":
			out[key] = check.OK
		case "skipped":
			out[key] = check.Skipped
		case "outcome":
			out[key] = check.Outcome
		case "issueCount":
			out[key] = check.IssueCount
		case "summary":
			out[key] = emptyNil(check.Summary)
		case "error":
			out[key] = emptyNil(check.Error)
		case "issues":
			issues := check.Issues
			issuesFirst := firstIssues
			if fieldHasArg(field, "first") {
				var nestedValid bool
				issuesFirst, nestedValid = validationIssueLimit(e.fieldArgs(field)["first"], semdb.ValidationDiagnosticDefaultPageSize)
				if !nestedValid {
					if invalidNestedLimitReported != nil && !*invalidNestedLimitReported {
						e.addErrorWithExtensions(append(path, key), "validation issue limit must be between 1 and 200", map[string]any{"code": "validation_page_limit_invalid"})
						*invalidNestedLimitReported = true
					}
					issuesFirst = 0
				}
			}
			if validLimit && issuesFirst > 0 && generation > 0 {
				if store, ok := e.deps.Store.(validationResultStore); ok {
					page, err := store.GetValidationDiagnosticsPage(ctx, semdb.ValidationDiagnosticPageRequest{
						Generation: generation, Limit: issuesFirst,
						Filter: semdb.ValidationDiagnosticFilter{
							Check: check.Name, ScopeKind: validationPageScopeKind(scope), ScopeKey: scope.Key,
							InterfaceImplementors: ontology.InterfaceImplementors(e.schema),
						},
					})
					if err != nil {
						e.addErrorWithExtensions(append(path, key), err.Error(), map[string]any{"code": "validation_diagnostics_unavailable"})
					} else {
						issues = validationIssuesFromDiagnostics(page.Diagnostics)
					}
				}
			}
			rows := make([]any, 0, len(issues))
			for _, issue := range issues {
				rows = append(rows, e.resolveValidationIssueSelectionSet(issue, field.SelectionSet, append(path, key)))
			}
			out[key] = rows
		default:
			e.addError(append(path, key), fmt.Sprintf("field %q does not exist on ValidationCheck", field.Name))
		}
	}
	return out
}

func validationPageScopeKind(scope semdb.ValidationScope) string {
	if scope.Kind == semdb.ValidationScopeGlobal {
		return ""
	}
	return scope.Kind
}

func validationIssueLimit(value any, fallback int) (int, bool) {
	limit := intValue(value, fallback)
	return limit, limit >= 1 && limit <= semdb.ValidationDiagnosticMaxPageSize
}

func validationIssuesFromDiagnostics(diagnostics []semdb.ValidationDiagnostic) []validationIssue {
	issues := make([]validationIssue, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		issue := validationIssue{
			IssueKey: diagnostic.IssueKey, Check: diagnostic.Check, Code: diagnostic.Code,
			Path: diagnostic.PrimaryPath, Field: diagnostic.Field, Source: diagnostic.Source,
			Target: diagnostic.Target, Message: diagnostic.Message, Data: diagnostic.Evidence,
			AffectedPaths: diagnostic.AffectedPaths, AffectedNotePaths: diagnostic.AffectedNotePaths,
			AffectedNodeIDs: diagnostic.AffectedNodeIDs, AffectedTypes: diagnostic.AffectedTypes,
			AffectedInterfaces: diagnostic.AffectedInterfaces, ActionIDs: diagnostic.ActionIDs,
		}
		if diagnostic.Location != nil && diagnostic.Location.Unit == semdb.ValidationLocationUnitLine {
			issue.Line = diagnostic.Location.Start
		}
		issues = append(issues, issue)
	}
	return issues
}

func (e *executor) resolveValidationIssueSelectionSet(issue validationIssue, set ast.SelectionSet, path []string) map[string]any {
	out := make(map[string]any)
	for _, selection := range set {
		field, ok := selection.(*ast.Field)
		if !ok {
			continue
		}
		key := responseKey(field)
		switch field.Name {
		case "issueKey":
			out[key] = issue.IssueKey
		case "check":
			out[key] = issue.Check
		case "code":
			out[key] = emptyNil(issue.Code)
		case "path":
			out[key] = emptyNil(issue.Path)
		case "field":
			out[key] = emptyNil(issue.Field)
		case "source":
			out[key] = emptyNil(issue.Source)
		case "target":
			out[key] = emptyNil(issue.Target)
		case "message":
			out[key] = emptyNil(issue.Message)
		case "line":
			out[key] = issue.Line
		case "data":
			var data any
			if len(issue.Data) > 0 {
				_ = json.Unmarshal(issue.Data, &data)
			}
			out[key] = data
		case "affectedPaths":
			out[key] = stringsSliceAny(issue.AffectedPaths)
		case "affectedNotePaths":
			out[key] = stringsSliceAny(issue.AffectedNotePaths)
		case "affectedNodeIds":
			out[key] = stringsSliceAny(issue.AffectedNodeIDs)
		case "affectedTypes":
			out[key] = stringsSliceAny(issue.AffectedTypes)
		case "affectedInterfaces":
			out[key] = stringsSliceAny(issue.AffectedInterfaces)
		case "actionIds":
			out[key] = stringsSliceAny(issue.ActionIDs)
		default:
			e.addError(append(path, key), fmt.Sprintf("field %q does not exist on ValidationIssue", field.Name))
		}
	}
	return out
}
