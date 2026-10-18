package validate

import (
	"encoding/json"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestStableIssueKeyIgnoresJSONObjectOrdering(t *testing.T) {
	left := Issue{
		Code:   "duplicate_preferred_identifier",
		Path:   "docs/specs/one.md",
		Target: "SPEC-0001",
		Data:   json.RawMessage(`{"type":"Spec","identifier":"SPEC-0001"}`),
	}
	right := left
	right.Data = json.RawMessage(`{ "identifier": "SPEC-0001", "type": "Spec" }`)

	leftKey, err := StableIssueKey(CheckIdentifiers, left)
	require.NoError(t, err)
	rightKey, err := StableIssueKey(CheckIdentifiers, right)
	require.NoError(t, err)

	assert.Equal(t, leftKey, rightKey)
	assert.Contains(t, leftKey, "issue:v1:")
}

func TestStableIssueKeyExcludesPresentationAndNormalizesPaths(t *testing.T) {
	left := Issue{
		Code:    "broken_note_link",
		Path:    "docs\\specs\\one.md",
		Source:  "./docs/specs/source.md",
		Target:  "missing",
		Line:    10,
		Message: "first rendering",
	}
	right := left
	right.Path = "docs/specs/one.md"
	right.Source = "docs/specs/source.md"
	right.Line = 99
	right.Message = "different rendering"

	leftKey, err := StableIssueKey(CheckBrokenLinks, left)
	require.NoError(t, err)
	rightKey, err := StableIssueKey(CheckBrokenLinks, right)
	require.NoError(t, err)
	assert.Equal(t, leftKey, rightKey)
}

func TestStableActionKeyExcludesPresentationFields(t *testing.T) {
	left := FixAction{
		Check:         CheckBrokenLinks,
		IssueCode:     "broken_note_link",
		Kind:          FixKindRewriteLinkGroup,
		Safety:        FixSafetySafe,
		Title:         "first title",
		Summary:       "first summary",
		Question:      "first question",
		InstanceCount: 1,
		AffectedPaths: []string{"docs\\source.md"},
		Edits: []FixEdit{{
			Kind:      FixKindRewriteLinkGroup,
			NotePath:  "docs/source.md",
			OldTarget: "old",
			NewTarget: "new",
		}},
	}
	right := left
	right.Title = "different title"
	right.Summary = "different summary"
	right.Question = "different question"
	right.InstanceCount = 99

	leftKey, err := StableActionKey(left)
	require.NoError(t, err)
	rightKey, err := StableActionKey(right)
	require.NoError(t, err)
	assert.Equal(t, leftKey, rightKey)
	assert.Contains(t, leftKey, "action:v1:")
}

func TestSourceHashUsesRawBytes(t *testing.T) {
	lf := SourceHash([]byte("# Note\nbody\n"))
	crlf := SourceHash([]byte("# Note\r\nbody\r\n"))

	assert.NotEqual(t, lf, crlf)
	assert.Equal(t, lf, SourceHash([]byte("# Note\nbody\n")))
}
