package ontology

import (
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/stretchr/testify/require"
)

func TestFlagsForAssessment(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		assessment *NoteAssessment
		want       semdb.OntologyAssessmentFlags
	}{
		{name: "nil"},
		{name: "clean", assessment: &NoteAssessment{NotePath: "a.md"}},
		{
			name:       "note issue",
			assessment: &NoteAssessment{Issues: []ValidationIssue{{Code: "x"}}},
			want:       semdb.OntologyAssessmentFlags{HasIssues: true},
		},
		{
			name:       "field issue",
			assessment: &NoteAssessment{Fields: []FieldAssessment{{Issues: []ValidationIssue{{Code: "x"}}}}},
			want:       semdb.OntologyAssessmentFlags{HasIssues: true},
		},
		{
			name:       "relation issue",
			assessment: &NoteAssessment{Relations: []RelationAssessment{{Issues: []ValidationIssue{{Code: "x"}}}}},
			want:       semdb.OntologyAssessmentFlags{HasIssues: true},
		},
		{
			name:       "field without issues",
			assessment: &NoteAssessment{Fields: []FieldAssessment{{Name: "summary"}}},
		},
		{
			name:       "one candidate type",
			assessment: &NoteAssessment{CandidateTypes: []string{"Decision"}},
		},
		{
			name:       "two candidate types",
			assessment: &NoteAssessment{CandidateTypes: []string{"Decision", "Project"}},
			want:       semdb.OntologyAssessmentFlags{TypeAmbiguous: true},
		},
		{
			name: "ambiguous and issues",
			assessment: &NoteAssessment{
				CandidateTypes: []string{"Decision", "Project"},
				Issues:         []ValidationIssue{{Code: "x"}},
			},
			want: semdb.OntologyAssessmentFlags{HasIssues: true, TypeAmbiguous: true},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, FlagsForAssessment(tc.assessment))
			require.Equal(t, tc.want.HasIssues, AssessmentHasIssues(tc.assessment))
		})
	}
}

func TestSetAssessmentRowKeepsFlagsInSyncWithJSON(t *testing.T) {
	t.Parallel()
	row := semdb.OntologyNoteAssessmentRow{NotePath: "a.md", HasIssues: true, TypeAmbiguous: true}

	setAssessmentRow(&row, NoteAssessment{NotePath: "a.md"})
	require.False(t, row.HasIssues)
	require.False(t, row.TypeAmbiguous)

	setAssessmentRow(&row, NoteAssessment{
		NotePath:       "a.md",
		CandidateTypes: []string{"Decision", "Project"},
		Fields:         []FieldAssessment{{Issues: []ValidationIssue{{Code: "missing"}}}},
	})
	require.True(t, row.HasIssues)
	require.True(t, row.TypeAmbiguous)

	decoded, err := AssessmentFromJSON(row.AssessmentJSON)
	require.NoError(t, err)
	require.Equal(t, semdb.OntologyAssessmentFlags{HasIssues: true, TypeAmbiguous: true}, FlagsForAssessment(decoded))
}
