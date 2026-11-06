package query

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/stretchr/testify/require"
)

func TestUntypedNodesExposeConcreteGraphQLNames(t *testing.T) {
	const content = "# Plain\n\n## Heading\n\nUnclassified content.\n"
	env := newCustomQueryTestEnv(t, `type Project @node(paths: ["projects/*.md"]) { name: String }`, map[string]string{
		"notes/plain.md": content,
	})
	sections := ontology.ParseSections("notes/plain.md", content)
	require.Len(t, sections, 1)
	require.Len(t, sections[0].Children, 1)
	ref := "notes/plain.md#" + queryFragmentFromSectionID("notes/plain.md", sections[0].Children[0].ID)
	prepared, errs := PrepareWithVariables(env.execSchema, `query($section: String!) {
      note(path: "notes/plain.md") { __typename resolvedType ... on UntypedNote { title } }
      section: node(ref: $section) { __typename resolvedType ... on UntypedSection { title } }
    }`, map[string]any{"section": ref})
	require.Empty(t, errs)
	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	require.NotNil(t, result.Data["note"])
	require.NotNil(t, result.Data["section"])
	note := result.Data["note"].(map[string]any)
	section := result.Data["section"].(map[string]any)
	require.Equal(t, "UntypedNote", note["__typename"])
	require.Equal(t, "", note["resolvedType"], "ontology classification remains unchanged")
	require.Contains(t, note, "title", "concrete fragments must match the returned type")
	require.Equal(t, "UntypedSection", section["__typename"])
	require.Equal(t, "Section", section["resolvedType"])
	require.Contains(t, section, "title")
}
