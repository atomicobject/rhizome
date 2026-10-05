package actions

import (
	"reflect"
	"testing"

	"github.com/atomicobject/rhizome/mocks"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestTags(t *testing.T) {
	tests := []struct {
		name          string
		notes         map[string]string // notePath -> content
		expected      []TagSummary
		expectedError bool
		filterNotes   []string
		projected     []notemeta.NoteSourceSnapshot
	}{
		{
			name: "hierarchical tags example from user",
			notes: map[string]string{
				"note1.md":  "---\ntags: [a]\n---\nContent",
				"note2.md":  "#a/b",
				"note3.md":  "#a/b",
				"note4.md":  "#a/b\n#a/c",
				"note5.md":  "---\ntags: [a/c/c]\n---\nContent",
				"note6.md":  "#a/c/d",
				"note7.md":  "#a/c/d",
				"note8.md":  "#b",
				"note9.md":  "#b",
				"note10.md": "#b",
				"note11.md": "#b",
				"note12.md": "#b",
			},
			expected: []TagSummary{
				{Name: "a", IndividualCount: 1, AggregateCount: 7},
				{Name: "a/c", IndividualCount: 1, AggregateCount: 4},
				{Name: "a/c/d", IndividualCount: 2, AggregateCount: 2},
				{Name: "a/c/c", IndividualCount: 1, AggregateCount: 1},
				{Name: "a/b", IndividualCount: 3, AggregateCount: 3},
				{Name: "b", IndividualCount: 5, AggregateCount: 5},
			},
		},
		{
			name: "mixed frontmatter and hashtags",
			notes: map[string]string{
				"note1.md": "---\ntags: [project, work/meeting]\n---\nContent here",
				"note2.md": "#project/planning\nSome content",
				"note3.md": "---\ntags: work\n---\n\n#personal",
			},
			expected: []TagSummary{
				{Name: "project", IndividualCount: 1, AggregateCount: 2},
				{Name: "project/planning", IndividualCount: 1, AggregateCount: 1},
				{Name: "work", IndividualCount: 1, AggregateCount: 2},
				{Name: "work/meeting", IndividualCount: 1, AggregateCount: 1},
				{Name: "personal", IndividualCount: 1, AggregateCount: 1},
			},
		},
		{
			name: "case insensitive tags",
			notes: map[string]string{
				"note1.md": "#Project",
				"note2.md": "---\ntags: [PROJECT]\n---\nContent",
				"note3.md": "#project/Planning",
			},
			expected: []TagSummary{
				{Name: "project", IndividualCount: 2, AggregateCount: 3},
				{Name: "project/planning", IndividualCount: 1, AggregateCount: 1},
			},
		},
		{
			name:     "no tags",
			notes:    map[string]string{"note1.md": "Just content, no tags"},
			expected: []TagSummary{},
		},
		{
			name: "subset of notes",
			notes: map[string]string{
				"note1.md": "#a",
				"note2.md": "#a\n#b",
			},
			expected: []TagSummary{
				{Name: "a", IndividualCount: 1, AggregateCount: 1},
				{Name: "b", IndividualCount: 1, AggregateCount: 1},
			},
			filterNotes: []string{"note2.md"},
		},
		{
			name:      "projected tag normalization and validity",
			projected: []notemeta.NoteSourceSnapshot{{Path: paths.NormalizeNotePath("Note.md"), Tags: []string{"  Project  ", "project2024", "career-pathing", "career_pathing", "", "---", "123", "person person"}}},
			expected: []TagSummary{
				{Name: "career-pathing", IndividualCount: 1, AggregateCount: 1},
				{Name: "career_pathing", IndividualCount: 1, AggregateCount: 1},
				{Name: "project", IndividualCount: 1, AggregateCount: 1},
				{Name: "project2024", IndividualCount: 1, AggregateCount: 1},
			},
		},
		{
			name: "invalid tags filtered out",
			notes: map[string]string{
				"note1.md": "---\ntags: [\"1\", \"327\", \"person person\", \"valid-tag\"]\n---\nContent with #2 and #project tags.",
			},
			expected: []TagSummary{
				{Name: "project", IndividualCount: 1, AggregateCount: 1},
				{Name: "valid-tag", IndividualCount: 1, AggregateCount: 1},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock managers
			vaultManager := &mocks.VaultManager{}

			// Set up mock vault definition
			vaultDef := obsidian.VaultDefinition{Name: "mock", Path: "/mock/vault"}
			vaultManager.On("Definition").Return(vaultDef, nil)

			// Run the function
			var opts []TagsOptions
			if len(tt.filterNotes) > 0 {
				opts = append(opts, TagsOptions{Notes: tt.filterNotes})
			}
			reader := factReader{facts: NewNoteFacts(tt.projected)}
			if tt.projected == nil {
				reader = newFactReader(nil, tt.notes)
			}
			result, err := Tags(vaultManager, reader, opts...)

			// Check error expectation
			if tt.expectedError && err == nil {
				t.Errorf("expected error but got none")
				return
			}
			if !tt.expectedError && err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			// Compare results
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("Tags() result mismatch\nExpected: %+v\nActual:   %+v", tt.expected, result)

				// Print detailed comparison for debugging
				t.Logf("Expected %d tags, got %d", len(tt.expected), len(result))
				for i, expected := range tt.expected {
					if i < len(result) {
						actual := result[i]
						if expected != actual {
							t.Logf("  Tag %d: expected %+v, got %+v", i, expected, actual)
						}
					} else {
						t.Logf("  Tag %d: expected %+v, got <missing>", i, expected)
					}
				}
				for i := len(tt.expected); i < len(result); i++ {
					t.Logf("  Tag %d: expected <missing>, got %+v", i, result[i])
				}
			}

			// Verify mock expectations
			vaultManager.AssertExpectations(t)
		})
	}
}

func TestTagsMixedCaseMarkdownAndUnpublishedHTML(t *testing.T) {
	vault := &mocks.VaultManager{}
	vault.On("Definition").Return(obsidian.VaultDefinition{Name: "mock", Path: "/mock/vault"}, nil)

	note := newFactReader(nil, map[string]string{
		"Notes/Decision.MD": "#markdown",
		"Notes/Excluded.md": "#excluded",
	})

	result, err := Tags(vault, note, TagsOptions{Notes: []string{"Notes/Decision.MD", "Notes/Reference.html"}})
	require.NoError(t, err)
	require.Equal(t, []TagSummary{
		{Name: "markdown", IndividualCount: 1, AggregateCount: 1},
	}, result)
}
