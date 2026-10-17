package schema

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSelectorsUseOnlySuppliedSubjectFacts(t *testing.T) {
	compiled := mustLoadSDL(t, `type Selected @node(matches: ["find:Provider AND tag:provider AND owner:People/Drew"]) { title: String }`)
	subject := &Subject{
		Path: "notes/current.md", Title: "Provider supplied title", Tags: []string{"provider/current"},
		Frontmatter: map[string]any{"owner": "[[People/Drew|Drew]]"},
	}
	require.True(t, compiled.MatchDoc("Selected", subject))
	subject.Tags = nil
	subject.Frontmatter["tags"] = []string{"provider/current"}
	require.False(t, compiled.MatchDoc("Selected", subject), "provider-supplied empty tags remain authoritative")
	subject.Tags = []string{"provider/current"}
	subject.Title = "Different title"
	require.False(t, compiled.MatchDoc("Selected", subject))
}
