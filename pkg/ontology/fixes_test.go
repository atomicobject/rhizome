package ontology

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSuggestFixes_NilInputs(t *testing.T) {
	assert.Nil(t, SuggestFixes(nil, nil, nil))
	assert.Nil(t, SuggestFixes(&NoteAssessment{}, nil, nil))
}

func TestSuggestFixes_InverseMismatch(t *testing.T) {
	schema := &Schema{
		Types: map[string]*NoteType{
			"Spec": {
				Name: "Spec",
				Fields: []*Field{{
					Name:     "implements",
					Kind:     FieldKindLink,
					TypeName: "Feature",
					Inverse:  "specs",
				}},
			},
			"Feature": {
				Name: "Feature",
				Fields: []*Field{{
					Name:     "specs",
					Kind:     FieldKindLink,
					TypeName: "Spec",
					List:     true,
					Inverse:  "implements",
				}},
			},
		},
	}

	targetAssessment := &NoteAssessment{
		NotePath:     "features/login.md",
		ResolvedType: "Feature",
		Relations: []RelationAssessment{{
			Name: "specs",
			Targets: []RelationTarget{
				{Path: "specs/existing.md", TypeName: "Spec"},
			},
		}},
	}

	sourceAssessment := &NoteAssessment{
		NotePath:     "specs/auth.md",
		ResolvedType: "Spec",
		Issues: []ValidationIssue{{
			Code:         "inverse_mismatch",
			NotePath:     "specs/auth.md",
			TypeName:     "Spec",
			FieldName:    "implements",
			Message:      "field implements expects inverse specs on features/login.md",
			FixTarget:    "features/login.md",
			FixFieldName: "specs",
		}},
	}

	allAssessments := map[string]*NoteAssessment{
		"specs/auth.md":     sourceAssessment,
		"features/login.md": targetAssessment,
	}

	fixes := SuggestFixes(sourceAssessment, schema, allAssessments)
	require.Len(t, fixes, 1)
	fix := fixes[0]
	assert.Equal(t, "inverse_mismatch", fix.IssueCode)
	assert.Equal(t, "specs/auth.md", fix.NotePath)
	require.Len(t, fix.Ops, 1)

	op := fix.Ops[0]
	assert.Equal(t, "setLinkField", op.Kind)
	assert.Equal(t, "features/login.md", op.Path)
	assert.Equal(t, "specs", op.Field)
	// Should contain existing target + the new source note.
	assert.Equal(t, []string{"[[specs/existing.md]]", "[[specs/auth.md]]"}, op.Values)
}

func TestSuggestFixes_InverseMismatch_NoDuplicate(t *testing.T) {
	sourceAssessment := &NoteAssessment{
		NotePath:     "specs/auth.md",
		ResolvedType: "Spec",
		Issues: []ValidationIssue{{
			Code:         "inverse_mismatch",
			NotePath:     "specs/auth.md",
			FieldName:    "implements",
			FixTarget:    "features/login.md",
			FixFieldName: "specs",
		}},
	}
	targetAssessment := &NoteAssessment{
		NotePath: "features/login.md",
		Relations: []RelationAssessment{{
			Name: "specs",
			Targets: []RelationTarget{
				{Path: "specs/auth.md"}, // Already linked
			},
		}},
	}

	allAssessments := map[string]*NoteAssessment{
		"specs/auth.md":     sourceAssessment,
		"features/login.md": targetAssessment,
	}

	fixes := SuggestFixes(sourceAssessment, &Schema{}, allAssessments)
	assert.Empty(t, fixes, "should not suggest fix when link already exists")
}

func TestSuggestFixes_InverseMismatch_NoTarget(t *testing.T) {
	assessment := &NoteAssessment{
		NotePath: "specs/auth.md",
		Issues: []ValidationIssue{{
			Code:         "inverse_mismatch",
			NotePath:     "specs/auth.md",
			FixTarget:    "features/missing.md",
			FixFieldName: "specs",
		}},
	}
	// Target note not in assessments map — do not suggest a destructive
	// replacement when we cannot see the current inverse field values.
	fixes := SuggestFixes(assessment, &Schema{}, map[string]*NoteAssessment{
		"specs/auth.md": assessment,
	})
	assert.Empty(t, fixes)
}

func TestSuggestFixes_InverseMismatch_MissingFixFields(t *testing.T) {
	assessment := &NoteAssessment{
		NotePath: "specs/auth.md",
		Issues: []ValidationIssue{{
			Code:     "inverse_mismatch",
			NotePath: "specs/auth.md",
			// FixTarget and FixFieldName missing
		}},
	}
	fixes := SuggestFixes(assessment, &Schema{}, nil)
	assert.Empty(t, fixes)
}

func TestSuggestFixes_DeclaredTypeMismatch_SingleCandidate(t *testing.T) {
	assessment := &NoteAssessment{
		NotePath:       "notes/meeting.md",
		DeclaredType:   "WrongType",
		CandidateTypes: []string{"MeetingNote"},
		Issues: []ValidationIssue{{
			Code:     "declared_type_mismatch",
			NotePath: "notes/meeting.md",
			TypeName: "WrongType",
			Message:  `note "notes/meeting.md" declares WrongType but selectors match MeetingNote`,
		}},
	}

	fixes := SuggestFixes(assessment, &Schema{}, nil)
	require.Len(t, fixes, 1)
	fix := fixes[0]
	assert.Equal(t, "declared_type_mismatch", fix.IssueCode)
	assert.Equal(t, assessment.Issues[0], fix.Issue,
		"repair consumers need the complete originating identity for exact issue-key binding")
	require.Len(t, fix.Ops, 1)

	op := fix.Ops[0]
	assert.Equal(t, "setField", op.Kind)
	assert.Equal(t, "notes/meeting.md", op.Path)
	assert.Equal(t, "type", op.Field)
	assert.Equal(t, "MeetingNote", op.Value)
}

func TestSuggestFixes_PreservesFallbackFieldInFullIssueIdentity(t *testing.T) {
	issue := ValidationIssue{
		Code:         "inverse_mismatch",
		NotePath:     "specs/auth.md",
		TypeName:     "Spec",
		NodeRef:      "specs/auth.md#^story-1",
		NodeID:       "story-1",
		Structural:   "structure:v1",
		FixTarget:    "features/login.md",
		FixFieldName: "specs",
	}
	source := &NoteAssessment{
		NotePath: "specs/auth.md",
		Relations: []RelationAssessment{{
			Name:   "implements",
			Issues: []ValidationIssue{issue},
		}},
	}
	target := &NoteAssessment{
		NotePath:  "features/login.md",
		Relations: []RelationAssessment{{Name: "specs"}},
	}

	fixes := SuggestFixes(source, &Schema{}, map[string]*NoteAssessment{
		source.NotePath: source,
		target.NotePath: target,
	})
	require.Len(t, fixes, 1)
	want := issue
	want.FieldName = "implements"
	assert.Equal(t, want, fixes[0].Issue)
}

func TestSuggestFixes_DeclaredTypeMismatch_MultipleCandidates(t *testing.T) {
	assessment := &NoteAssessment{
		NotePath:       "notes/meeting.md",
		DeclaredType:   "WrongType",
		CandidateTypes: []string{"MeetingNote", "CallNote"},
		Issues: []ValidationIssue{{
			Code:     "declared_type_mismatch",
			NotePath: "notes/meeting.md",
		}},
	}

	fixes := SuggestFixes(assessment, &Schema{}, nil)
	assert.Empty(t, fixes, "should not suggest fix when multiple candidates exist")
}

func TestSuggestFixes_DeclaredTypeMismatch_NoCandidates(t *testing.T) {
	assessment := &NoteAssessment{
		NotePath:       "notes/meeting.md",
		DeclaredType:   "WrongType",
		CandidateTypes: nil,
		Issues: []ValidationIssue{{
			Code:     "declared_type_mismatch",
			NotePath: "notes/meeting.md",
		}},
	}

	fixes := SuggestFixes(assessment, &Schema{}, nil)
	assert.Empty(t, fixes, "should not suggest fix when no candidates exist")
}

func TestSuggestFixes_UnfixableIssue(t *testing.T) {
	assessment := &NoteAssessment{
		NotePath: "notes/ambiguous.md",
		Issues: []ValidationIssue{{
			Code:     "type_ambiguous",
			NotePath: "notes/ambiguous.md",
			Message:  "matches multiple types",
		}},
	}

	fixes := SuggestFixes(assessment, &Schema{}, nil)
	assert.Empty(t, fixes)
}

func TestSuggestFixes_FieldIssues(t *testing.T) {
	// declared_type_mismatch on the note-level issues, plus a field-level issue
	// that is not fixable. Only the note-level fix should be suggested.
	assessment := &NoteAssessment{
		NotePath:       "notes/test.md",
		DeclaredType:   "Wrong",
		CandidateTypes: []string{"Right"},
		Issues: []ValidationIssue{{
			Code:     "declared_type_mismatch",
			NotePath: "notes/test.md",
		}},
		Fields: []FieldAssessment{{
			Name: "status",
			Issues: []ValidationIssue{{
				Code:     "field_type_mismatch",
				NotePath: "notes/test.md",
			}},
		}},
	}

	fixes := SuggestFixes(assessment, &Schema{}, nil)
	require.Len(t, fixes, 1)
	assert.Equal(t, "declared_type_mismatch", fixes[0].IssueCode)
}
