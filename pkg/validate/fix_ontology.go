package validate

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
)

func ontologyValidationIssue(issue ontology.ValidationIssue) Issue {
	result := Issue{
		Code: issue.Code, Path: issue.NotePath, Type: issue.TypeName,
		Field: issue.FieldName, Message: issue.Message, Line: issue.Line,
		Target: issue.LinkTarget, Variant: newIssueVariant(issue.VariantKey, issue.VariantLabel),
	}
	if nodeID := strings.TrimSpace(issue.NodeID); nodeID != "" {
		result.AffectedNodeIDs = []string{nodeID}
		if issue.Line > 0 {
			result.Location = &IssueLocation{
				Unit: LocationUnitLine, Start: issue.Line, End: issue.Line,
				NodeID: nodeID, Field: issue.FieldName,
			}
		}
	}
	identity := OntologyIssueData{
		NodeRef: issue.NodeRef, NodeID: issue.NodeID, Structural: issue.Structural,
		LinkKind: issue.LinkKind, FixTarget: issue.FixTarget, FixFieldName: issue.FixFieldName,
		CandidateTypes: issue.CandidateTypes,
	}
	if !reflect.ValueOf(identity).IsZero() {
		result.Data = mustMarshal(identity)
	}
	return result
}

// enrichOntologyRepairActions runs only after stable issue keys have been
// attached. Every new action is accepted through an exact stable-key lookup;
// there is no code/path/count fallback.
func enrichOntologyRepairActions(ctx context.Context, runCtx RunContext, checks []CheckResult, runtime *ontology.Runtime) error {
	if runtime == nil || runtime.Schema == nil || runtime.Store == nil {
		return nil
	}
	for index := range checks {
		if checks[index].Name != CheckOntology || strings.TrimSpace(checks[index].Error) != "" {
			continue
		}
		suggestions, err := ontologyFixSuggestions(ctx, runtime)
		if err != nil {
			return err
		}
		if err := attachOntologySuggestionActions(&checks[index], suggestions); err != nil {
			return err
		}
		if err := attachOntologyStructuralActions(&checks[index], runtime); err != nil {
			return err
		}
		sort.SliceStable(checks[index].Fixes, func(i, j int) bool {
			return checks[index].Fixes[i].ID < checks[index].Fixes[j].ID
		})
	}
	_ = runCtx
	return nil
}

func ontologyFixSuggestions(ctx context.Context, runtime *ontology.Runtime) ([]ontology.FixSuggestion, error) {
	typeNames := make([]string, 0, len(runtime.Schema.Types))
	for typeName, noteType := range runtime.Schema.Types {
		if noteType != nil && noteType.Role == ontology.TypeRoleNote {
			typeNames = append(typeNames, typeName)
		}
	}
	sort.Strings(typeNames)
	allPaths := make([]string, 0)
	for _, typeName := range typeNames {
		paths, err := runtime.Store.OntologyPathsByType(ctx, typeName, 0)
		if err != nil {
			return nil, err
		}
		allPaths = append(allPaths, paths...)
	}
	for _, issue := range runtime.Issues {
		allPaths = append(allPaths, issue.NotePath, issue.FixTarget)
	}
	allPaths = nonEmptyUnique(allPaths)
	assessmentRows, err := runtime.Store.OntologyAssessmentsByPaths(ctx, allPaths)
	if err != nil {
		return nil, err
	}
	assessments := make(map[string]*ontology.NoteAssessment, len(assessmentRows))
	for _, notePath := range allPaths {
		row, ok := assessmentRows[notePath]
		if !ok {
			continue
		}
		assessment, err := ontology.AssessmentFromJSON(row.AssessmentJSON)
		if err != nil {
			return nil, err
		}
		if assessment != nil {
			assessments[notePath] = assessment
		}
	}
	var suggestions []ontology.FixSuggestion
	for _, notePath := range allPaths {
		if assessment := assessments[notePath]; assessment != nil {
			suggestions = append(suggestions, ontology.SuggestFixes(assessment, runtime.Schema, assessments)...)
		}
	}
	return suggestions, nil
}

func attachOntologySuggestionActions(check *CheckResult, suggestions []ontology.FixSuggestion) error {
	known := stableIssueKeySet(check)
	seen := make(map[string]struct{})
	for _, suggestion := range suggestions {
		issueKey, err := StableIssueKey(CheckOntology, ontologyValidationIssue(suggestion.Issue))
		if err != nil {
			return err
		}
		if _, ok := known[issueKey]; !ok {
			continue
		}
		if _, duplicate := seen[issueKey]; duplicate {
			continue
		}
		edits, paths, kind := ontologySuggestionEdits(suggestion)
		if len(edits) == 0 {
			continue
		}
		seen[issueKey] = struct{}{}
		check.Fixes = append(check.Fixes, FixAction{
			ID:    "ontology-suggestion:" + strings.TrimPrefix(issueKey, "issue:v1:"),
			Check: CheckOntology, IssueCode: suggestion.IssueCode, Kind: kind,
			Safety: FixSafetySafe, Title: fmt.Sprintf("Repair %s on %s", suggestion.IssueCode, suggestion.NotePath),
			Summary:       "apply the schema-derived ontology suggestion",
			InstanceCount: 1, IssueKeys: []string{issueKey}, AffectedPaths: paths, Edits: edits,
		})
	}
	return nil
}

func ontologySuggestionEdits(suggestion ontology.FixSuggestion) ([]FixEdit, []string, string) {
	var edits []FixEdit
	var paths []string
	actionKind := ""
	for _, op := range suggestion.Ops {
		edit := FixEdit{NotePath: op.Path, Property: op.Field, Value: op.Value, Values: append([]string(nil), op.Values...), NodeID: op.NodeID, Structural: op.Structural, BlockID: op.BlockID}
		switch op.Kind {
		case "setField":
			if strings.EqualFold(op.Field, "type") && op.NodeID == "" && op.Structural == "" {
				edit.Kind = FixKindSetFrontmatter
			} else {
				edit.Kind = FixKindOntologySetScalar
			}
		case "setLinkField":
			edit.Kind = FixKindOntologySetLink
		default:
			continue
		}
		if actionKind == "" {
			actionKind = edit.Kind
		}
		edits = append(edits, edit)
		paths = append(paths, op.Path)
	}
	return edits, nonEmptyUnique(paths), actionKind
}

func attachOntologyStructuralActions(check *CheckResult, runtime *ontology.Runtime) error {
	known := stableIssueKeySet(check)
	seen := make(map[string]struct{})
	for _, issue := range runtime.Issues {
		if issue.Code != "missing_required_field" && issue.Code != "missing_required_section" {
			continue
		}
		issueKey, err := StableIssueKey(CheckOntology, ontologyValidationIssue(issue))
		if err != nil {
			return err
		}
		if _, ok := known[issueKey]; !ok {
			continue
		}
		if _, duplicate := seen[issueKey]; duplicate {
			continue
		}
		field, ok := ontologyFieldAtPath(runtime.Schema, issue.TypeName, issue.FieldName)
		if !ok {
			continue
		}
		var action FixAction
		switch issue.Code {
		case "missing_required_field":
			if strings.Contains(issue.FieldName, ".") || field.SourceKind != ontology.FieldSourceFrontmatter || !field.Required || !field.List {
				continue
			}
			property := strings.TrimSpace(field.Source)
			if property == "" {
				property = field.Name
			}
			action = FixAction{
				Kind: FixKindSetFrontmatter, Title: fmt.Sprintf("Initialize %s on %s", issue.FieldName, issue.NotePath),
				Summary: fmt.Sprintf("set missing required list field %s to []", issue.FieldName),
				Edits:   []FixEdit{{Kind: FixKindSetFrontmatter, NotePath: issue.NotePath, Property: property, Values: []string{}}},
			}
		case "missing_required_section":
			if field.Kind != ontology.FieldKindSection || !field.SectionRequired || strings.TrimSpace(field.SectionHeading) == "" {
				continue
			}
			action = FixAction{
				Kind: FixKindOntologyAddSection, Title: fmt.Sprintf("Add missing %s section to %s", issue.FieldName, issue.NotePath),
				Summary: fmt.Sprintf("insert schema-declared %s heading %q at its ordered parent position", field.SectionLevel, field.SectionHeading),
				Edits:   []FixEdit{{Kind: FixKindOntologyAddSection, NotePath: issue.NotePath, Property: issue.FieldName}},
			}
		}
		seen[issueKey] = struct{}{}
		action.ID = "ontology-structural:" + strings.TrimPrefix(issueKey, "issue:v1:")
		action.Check = CheckOntology
		action.IssueCode = issue.Code
		action.Safety = FixSafetySafe
		action.InstanceCount = 1
		action.IssueKeys = []string{issueKey}
		action.AffectedPaths = []string{issue.NotePath}
		check.Fixes = append(check.Fixes, action)
	}
	return nil
}

func stableIssueKeySet(check *CheckResult) map[string]struct{} {
	known := make(map[string]struct{})
	if check == nil {
		return known
	}
	issues := check.fullIssues
	if len(issues) == 0 {
		issues = check.Issues
	}
	for _, issue := range issues {
		if strings.TrimSpace(issue.Key) != "" {
			known[issue.Key] = struct{}{}
		}
	}
	return known
}

func ontologyFieldAtPath(schema *ontology.Schema, typeName, fieldPath string) (*ontology.Field, bool) {
	if schema == nil {
		return nil, false
	}
	noteType := schema.Types[strings.TrimSpace(typeName)]
	parts := strings.Split(strings.TrimSpace(fieldPath), ".")
	for index, part := range parts {
		if noteType == nil {
			return nil, false
		}
		field := noteType.ByName[strings.TrimSpace(part)]
		if field == nil {
			return nil, false
		}
		if index == len(parts)-1 {
			return field, true
		}
		noteType = schema.Types[field.TypeName]
	}
	return nil, false
}

// IdentifierCandidateFromPath infers an identifier value from a note's filename.
func IdentifierCandidateFromPath(notePath string) string {
	base := strings.TrimSuffix(filepath.Base(strings.TrimSpace(notePath)), filepath.Ext(strings.TrimSpace(notePath)))
	base = strings.TrimSpace(base)
	if base == "" {
		return ""
	}
	var b strings.Builder
	lastDash := false
	for _, r := range base {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - 32)
			lastDash = false
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return ""
	}
	return out
}
