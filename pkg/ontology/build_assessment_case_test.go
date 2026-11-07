package ontology

import (
	"os"
	"path/filepath"
	"testing"

	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/stretchr/testify/require"
)

func TestDeclaredTypeMatchesSelectedTypeIgnoringCase(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome", "ontology"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "ontology", "schema.graphql"),
		[]byte(`type Assertion @node(paths: ["assertions/*.md"]) { headline: String }`+"\n"), 0o644))
	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)

	assess := func(path, declared string) *NoteAssessment {
		return resolveNoteAssessment(&noteDoc{Path: path, TypeName: declared}, schema)
	}
	lower := assess("assertions/a.md", "assertion")
	require.Equal(t, "Assertion", lower.ResolvedType)
	require.Equal(t, "assertion", lower.DeclaredType, "the authored spelling is kept")
	require.Empty(t, lower.Issues)

	typo := assess("assertions/a.md", "assertoin")
	require.Len(t, typo.Issues, 1)
	require.Equal(t, "unknown_declared_type", typo.Issues[0].Code)

	elsewhere := assess("notes/a.md", "assertion")
	require.Len(t, elsewhere.Issues, 1, "case folding needs a selector match, not just a similar name")
	require.Equal(t, "unknown_declared_type", elsewhere.Issues[0].Code)
}
