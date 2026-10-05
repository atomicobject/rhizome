package validate

import (
	"context"
	"fmt"
	"sort"
	"strings"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
)

type identifierAliasMirrorMissing struct {
	Path, Type, FieldName, Source, Value string
}

func runAliasesWithRuntime(ctx context.Context, runCtx RunContext, runtime *ontology.Runtime, result CheckResult) CheckResult {
	type identifierField struct {
		FieldName      string
		Source         string
		AuthoredSource string
		Preferred      bool
		Required       bool
	}
	identifierFieldsByType := make(map[string][]identifierField)
	identifierSourceSet := make(map[string]struct{})
	for typeName, nt := range runtime.Schema.Types {
		if nt == nil {
			continue
		}
		for _, f := range nt.Fields {
			if f == nil || !f.IsIdentifier {
				continue
			}
			if f.SourceKind != ontology.FieldSourceFrontmatter {
				continue
			}
			source := strings.ToLower(strings.TrimSpace(f.Source))
			if source == "" {
				source = strings.ToLower(f.Name)
			}
			identifierFieldsByType[typeName] = append(identifierFieldsByType[typeName], identifierField{
				FieldName:      f.Name,
				Source:         source,
				AuthoredSource: strings.TrimSpace(firstNonEmpty(f.Source, f.Name)),
				Preferred:      f.IsPreferredIdentifier,
				Required:       f.Required,
			})
			identifierSourceSet[source] = struct{}{}
		}
	}
	if len(identifierFieldsByType) == 0 {
		result.Skipped = true
		result.Summary = "no @identifier fields"
		return result
	}

	typedPaths := make(map[string]string)
	for typeName := range identifierFieldsByType {
		paths, err := runtime.Store.OntologyPathsByType(ctx, typeName, 0)
		if err != nil {
			result.OK = false
			result.Error = err.Error()
			return result
		}
		for _, p := range paths {
			typedPaths[p] = typeName
		}
	}
	if len(typedPaths) == 0 {
		result.Summary = "no typed notes to check"
		return result
	}

	sourceList := make([]string, 0, len(identifierSourceSet))
	for s := range identifierSourceSet {
		sourceList = append(sourceList, s)
	}
	sort.Strings(sourceList)
	pathList := make([]string, 0, len(typedPaths))
	for p := range typedPaths {
		pathList = append(pathList, p)
	}
	sort.Strings(pathList)

	identifierRows, err := runtime.Store.CurrentNotePropertyValues(ctx, pathList, sourceList, semdb.NotePropertySourceFrontmatter)
	if err != nil {
		result.OK = false
		result.Error = err.Error()
		return result
	}
	aliasRows, err := runtime.Store.CurrentNotePropertyValues(ctx, pathList, []string{"aliases"}, semdb.NotePropertySourceFrontmatter)
	if err != nil {
		result.OK = false
		result.Error = err.Error()
		return result
	}

	identifierValuesByNote := make(map[string]map[string]string, len(typedPaths))
	for _, row := range identifierRows {
		value := strings.TrimSpace(row.ValueText)
		if value == "" {
			continue
		}
		if identifierValuesByNote[row.NotePath] == nil {
			identifierValuesByNote[row.NotePath] = make(map[string]string)
		}
		identifierValuesByNote[row.NotePath][row.PropertyName] = value
	}

	aliasesByNote := make(map[string]map[string]struct{}, len(typedPaths))
	for _, row := range aliasRows {
		value := strings.TrimSpace(row.ValueText)
		if value == "" {
			continue
		}
		if aliasesByNote[row.NotePath] == nil {
			aliasesByNote[row.NotePath] = make(map[string]struct{})
		}
		aliasesByNote[row.NotePath][value] = struct{}{}
	}

	var missing []identifierAliasMirrorMissing

	for _, notePath := range pathList {
		typeName := typedPaths[notePath]
		fields := identifierFieldsByType[typeName]
		values := identifierValuesByNote[notePath]
		aliases := aliasesByNote[notePath]
		for _, field := range fields {
			value, ok := values[field.Source]
			if !ok || value == "" {
				continue
			}
			if _, present := aliases[value]; !present {
				missing = append(missing, identifierAliasMirrorMissing{
					Path: notePath, Type: typeName, FieldName: field.FieldName,
					Source: field.Source, Value: value,
				})
			}
		}
	}

	issues := make([]Issue, 0, len(missing))
	for _, m := range missing {
		issue, fix, keyErr := identifierAliasMirrorOutput(m)
		if keyErr != nil {
			result.OK = false
			result.Error = fmt.Sprintf("identify alias-mirror issue: %v", keyErr)
			result.Issues = nil
			result.Fixes = nil
			return result
		}
		issues = append(issues, issue)
		result.Fixes = append(result.Fixes, fix)
	}

	reservedIdentifiers := make(map[string]struct{})
	for _, values := range identifierValuesByNote {
		for _, value := range values {
			reservedIdentifiers[ontology.NormalizeIdentifierSemanticValue(value)] = struct{}{}
		}
	}

	assessmentRows, err := runtime.Store.OntologyAssessmentsByPaths(ctx, pathList)
	if err != nil {
		result.OK = false
		result.Error = err.Error()
		return result
	}
	for _, notePath := range pathList {
		typeName := typedPaths[notePath]
		fields := identifierFieldsByType[typeName]
		row, ok := assessmentRows[notePath]
		if !ok {
			continue
		}
		assessment, err := ontology.AssessmentFromJSON(row.AssessmentJSON)
		if err != nil {
			result.OK = false
			result.Error = err.Error()
			return result
		}
		if assessment == nil {
			continue
		}
		aliases := aliasesByNote[notePath]
		aliasList := make([]string, 0, len(aliases))
		for alias := range aliases {
			aliasList = append(aliasList, alias)
		}
		sort.Strings(aliasList)
		for _, field := range fields {
			fieldAssessment, ok := assessment.Field(field.FieldName)
			if !ok || !assessmentHasIssue(fieldAssessment.Issues, "missing_required_field") {
				continue
			}
			var sourceIssue ontology.ValidationIssue
			for _, candidateIssue := range fieldAssessment.Issues {
				if candidateIssue.Code == "missing_required_field" {
					sourceIssue = candidateIssue
					break
				}
			}
			if sourceIssue.NotePath == "" {
				sourceIssue.NotePath = notePath
			}
			if sourceIssue.TypeName == "" {
				sourceIssue.TypeName = typeName
			}
			if sourceIssue.FieldName == "" {
				sourceIssue.FieldName = field.FieldName
			}
			issue := ontologyValidationIssue(sourceIssue)
			issueKey, keyErr := StableIssueKey(CheckIdentifiers, issue)
			if keyErr != nil {
				result.OK = false
				result.Error = keyErr.Error()
				return result
			}
			issue.Key = issueKey
			issues = append(issues, issue)
			if len(aliasList) == 1 {
				identifierKey := ontology.NormalizeIdentifierSemanticValue(aliasList[0])
				if _, reserved := reservedIdentifiers[identifierKey]; reserved {
					continue
				}
				result.Fixes = append(result.Fixes, FixAction{
					ID:            "identifier-from-alias:" + notePath + ":" + field.FieldName,
					Check:         CheckAliases,
					IssueCode:     "missing_required_field",
					Kind:          FixKindSetFrontmatter,
					Safety:        FixSafetySafe,
					Title:         fmt.Sprintf("Set %s from aliases on %s", field.FieldName, notePath),
					Summary:       fmt.Sprintf("use existing alias %q as %s", aliasList[0], field.FieldName),
					InstanceCount: 1,
					IssueKeys:     []string{issueKey},
					AffectedPaths: []string{notePath},
					Edits: []FixEdit{{
						Kind:     FixKindSetFrontmatter,
						NotePath: notePath,
						Property: field.AuthoredSource,
						Value:    aliasList[0],
					}},
				})
				reservedIdentifiers[identifierKey] = struct{}{}
				continue
			}
			candidate := IdentifierCandidateFromPath(notePath)
			if field.Required && candidate != "" && len(aliasList) == 0 {
				identifierKey := ontology.NormalizeIdentifierSemanticValue(candidate)
				if _, reserved := reservedIdentifiers[identifierKey]; reserved {
					continue
				}
				result.Fixes = append(result.Fixes, FixAction{
					ID:            "identifier-from-path:" + notePath + ":" + field.FieldName,
					Check:         CheckAliases,
					IssueCode:     "missing_required_field",
					Kind:          FixKindSetFrontmatter,
					Safety:        FixSafetyConfirm,
					Title:         fmt.Sprintf("Generate %s for %s", field.FieldName, notePath),
					Summary:       fmt.Sprintf("set %s to %q inferred from the note path", field.FieldName, candidate),
					Question:      fmt.Sprintf("Set %s on %s to %q?", field.FieldName, notePath, candidate),
					InstanceCount: 1,
					IssueKeys:     []string{issueKey},
					AffectedPaths: []string{notePath},
					Edits: []FixEdit{{
						Kind:     FixKindSetFrontmatter,
						NotePath: notePath,
						Property: field.AuthoredSource,
						Value:    candidate,
					}},
				})
				reservedIdentifiers[identifierKey] = struct{}{}
			}
		}
	}

	result.IssueCount = len(issues)
	if result.IssueCount == 0 {
		result.Summary = fmt.Sprintf("%d typed notes checked", len(typedPaths))
		return result
	}
	result.Issues = issues
	result.Summary = fmt.Sprintf("%d alias issues", result.IssueCount)
	return result
}

func identifierAliasMirrorOutput(m identifierAliasMirrorMissing) (Issue, FixAction, error) {
	issue := Issue{
		Code:    "identifier_not_in_aliases",
		Path:    m.Path,
		Field:   m.FieldName,
		Target:  m.Value,
		Source:  m.Source,
		Message: fmt.Sprintf("%s note %q declares %s=%q but aliases: list does not contain %q", m.Type, m.Path, m.FieldName, m.Value, m.Value),
		Variant: newIssueVariant(ontology.TypeFieldVariant(m.Type, m.FieldName)),
	}
	issueKey, err := StableIssueKey(CheckIdentifiers, issue)
	if err != nil {
		return Issue{}, FixAction{}, err
	}
	issue.Key = issueKey
	return issue, FixAction{
		ID:            "alias-mirror:" + m.Path + ":" + m.FieldName + ":" + m.Value,
		Check:         CheckAliases,
		IssueCode:     "identifier_not_in_aliases",
		Kind:          FixKindAppendAlias,
		Safety:        FixSafetySafe,
		Title:         fmt.Sprintf("Mirror %s into aliases for %s", m.FieldName, m.Path),
		Summary:       fmt.Sprintf("append %q to aliases:", m.Value),
		InstanceCount: 1,
		IssueKeys:     []string{issueKey},
		AffectedPaths: []string{m.Path},
		Edits: []FixEdit{{
			Kind:     FixKindAppendAlias,
			NotePath: m.Path,
			Value:    m.Value,
		}},
	}, nil
}

func assessmentHasIssue(issues []ontology.ValidationIssue, code string) bool {
	for _, issue := range issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
