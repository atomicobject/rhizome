package ontology

import "strings"

// FixSuggestion describes an auto-fix for a single validation issue.
type FixSuggestion struct {
	// IssueCode is the validation issue code this fix resolves.
	IssueCode string
	// Issue preserves the complete originating identity. Consumers derive their
	// own stable public issue key from this value instead of joining suggestions
	// by a lossy code/path tuple.
	Issue ValidationIssue
	// IssueKey uniquely identifies the concrete issue instance this fix resolves.
	IssueKey string
	// NotePath is the note the original issue was reported on.
	NotePath string
	// Ops is the ordered list of edit operations that resolve the issue.
	Ops []FixOp
}

// FixOp is a self-contained edit operation. It mirrors OntologyEditOp in the
// web layer but lives in the ontology package to avoid import cycles.
type FixOp struct {
	Kind       string   // "setField" | "setLinkField" | "ensureBlockID"
	Path       string   // note path to edit
	NodeID     string   // target embedded node id
	Structural string   // target structural fingerprint
	Field      string   // field name
	Value      string   // for setField
	Values     []string // for setLinkField
	BlockID    string   // for ensureBlockID
}

// SuggestFixes examines one note's assessment issues and returns auto-fix
// suggestions for mechanically resolvable problems. assessments provides
// neighbor context (e.g. the target note's current relation values for
// inverse_mismatch fixes).
func SuggestFixes(assessment *NoteAssessment, schema *Schema, assessments map[string]*NoteAssessment) []FixSuggestion {
	if assessment == nil || schema == nil {
		return nil
	}
	var fixes []FixSuggestion

	for _, issue := range assessment.Issues {
		if fix := suggestFixForIssue(issue, issue.FieldName, assessment, schema, assessments); fix != nil {
			fixes = append(fixes, *fix)
		}
	}
	for _, field := range assessment.Fields {
		for _, issue := range field.Issues {
			if fix := suggestFixForIssue(issue, field.Name, assessment, schema, assessments); fix != nil {
				fixes = append(fixes, *fix)
			}
		}
	}
	for _, relation := range assessment.Relations {
		for _, issue := range relation.Issues {
			if fix := suggestFixForIssue(issue, relation.Name, assessment, schema, assessments); fix != nil {
				fixes = append(fixes, *fix)
			}
		}
	}
	return fixes
}

// WHY: missing_embedded_block_id is no longer a default-emitted issue per the
// SPEC-0023 usage-driven block-id lifecycle, so SuggestFixes does not have a
// branch for it. Insertion is driven through EnsureLinkTargetApply
// (pkg/ontology/node_link.go) at explicit caller request.
// Coderefs: [[linkable-embedded-node-identifiers#^spec-0023-us2]]
func suggestFixForIssue(issue ValidationIssue, fallbackField string, assessment *NoteAssessment, schema *Schema, assessments map[string]*NoteAssessment) *FixSuggestion {
	issue = validationIssueWithFallbackField(issue, fallbackField)
	switch issue.Code {
	case "inverse_mismatch":
		return suggestInverseMismatchFix(issue, fallbackField, assessment, schema, assessments)
	case "declared_type_mismatch":
		return suggestDeclaredTypeMismatchFix(issue, fallbackField, assessment)
	default:
		return nil
	}
}

func validationIssueWithFallbackField(issue ValidationIssue, fallbackField string) ValidationIssue {
	if strings.TrimSpace(issue.FieldName) == "" {
		issue.FieldName = strings.TrimSpace(fallbackField)
	}
	return issue
}

// ValidationIssueKey identifies one concrete validation issue instance.
func ValidationIssueKey(issue ValidationIssue, fallbackField string) string {
	fieldName := strings.TrimSpace(issue.FieldName)
	if fieldName == "" {
		fieldName = strings.TrimSpace(fallbackField)
	}
	parts := []string{
		strings.TrimSpace(issue.Code),
		fieldName,
		strings.TrimSpace(issue.NotePath),
		strings.TrimSpace(issue.LinkTarget),
		strings.TrimSpace(issue.FixTarget),
		strings.TrimSpace(issue.FixFieldName),
	}
	return strings.Join(parts, "|")
}

// suggestInverseMismatchFix generates a setLinkField op on the target note
// that adds the source note to the inverse field's value list.
func suggestInverseMismatchFix(issue ValidationIssue, fallbackField string, assessment *NoteAssessment, schema *Schema, assessments map[string]*NoteAssessment) *FixSuggestion {
	if issue.FixTarget == "" || issue.FixFieldName == "" {
		return nil
	}

	// Build the complete value list: existing targets on the inverse field + the source note.
	existingPaths, ok := currentLinkTargetPaths(assessments, issue.FixTarget, issue.FixFieldName)
	if !ok {
		return nil
	}
	// Don't add a duplicate.
	srcPath := issue.NotePath
	for _, p := range existingPaths {
		if p == srcPath {
			return nil
		}
	}
	values := make([]string, 0, len(existingPaths)+1)
	for _, p := range existingPaths {
		values = append(values, wikilink(p))
	}
	values = append(values, wikilink(srcPath))

	return &FixSuggestion{
		IssueCode: issue.Code,
		Issue:     issue,
		IssueKey:  ValidationIssueKey(issue, fallbackField),
		NotePath:  issue.NotePath,
		Ops: []FixOp{{
			Kind:   "setLinkField",
			Path:   issue.FixTarget,
			Field:  issue.FixFieldName,
			Values: values,
		}},
	}
}

// suggestDeclaredTypeMismatchFix generates a setField op to correct the
// frontmatter type when exactly one candidate type exists.
func suggestDeclaredTypeMismatchFix(issue ValidationIssue, fallbackField string, assessment *NoteAssessment) *FixSuggestion {
	if assessment == nil || len(assessment.CandidateTypes) != 1 {
		return nil
	}
	return &FixSuggestion{
		IssueCode: issue.Code,
		Issue:     issue,
		IssueKey:  ValidationIssueKey(issue, fallbackField),
		NotePath:  issue.NotePath,
		Ops: []FixOp{{
			Kind:  "setField",
			Path:  issue.NotePath,
			Field: "type",
			Value: assessment.CandidateTypes[0],
		}},
	}
}

// currentLinkTargetPaths reads the current link targets for a relation field
// on a note, returning raw note paths.
func currentLinkTargetPaths(assessments map[string]*NoteAssessment, notePath string, fieldName string) ([]string, bool) {
	if assessments == nil {
		return nil, false
	}
	target := assessments[notePath]
	if target == nil {
		return nil, false
	}
	relation, ok := target.Relation(fieldName)
	if !ok {
		return nil, false
	}
	if len(relation.Targets) == 0 {
		return nil, true
	}
	out := make([]string, 0, len(relation.Targets))
	for _, t := range relation.Targets {
		out = append(out, t.Path)
	}
	return out, true
}

func wikilink(path string) string {
	return "[[" + path + "]]"
}
