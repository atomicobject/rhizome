package query

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/stretchr/testify/require"
)

func TestExecute_SourceLinksKeepCatalogNoteRootsAsFileLocators(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type ReferenceDoc @node(paths: ["docs/*.md"]) { summary: String @field }
`, map[string]string{
		"docs/index.md": "# Index\n\n[[guide]]\n",
		"docs/guide.md": "# Guide\n\n## Constraints\n\nDetails.\n",
	})
	prepared, errs := Prepare(env.execSchema, `{ node(ref: "docs/index.md") {
  workspace { sourceLinks(first: 10) { resolvedRef { ref kind structural } } }
} }`)
	require.Empty(t, errs)
	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	links := result.Data["node"].(map[string]any)["workspace"].(map[string]any)["sourceLinks"].([]any)
	require.Len(t, links, 1)
	ref := links[0].(map[string]any)["resolvedRef"].(map[string]any)
	require.NotEmpty(t, ref["structural"], "exercise a catalog root carrying its structural identity")
	require.Equal(t, "NOTE", ref["kind"])
	require.Equal(t, "docs/guide.md", ref["ref"])
}

func TestExecute_NodeRootStructuralLocatorRequiresMatchingDocumentIdentity(t *testing.T) {
	const path = "docs/guide.md"
	const content = "# Guide\n\n## Constraints\n\nDetails.\n"
	env := newCustomQueryTestEnv(t, `type ReferenceDoc @node(paths: ["docs/*.md"]) { summary: String @field }`, map[string]string{path: content})
	projection, err := ontology.ProjectNode(context.Background(), env.deps(nil).VaultDef, env.deps(nil).NoteReader, env.schema, ontology.NodeRef{NotePath: path, Kind: ontology.NodeKindNote})
	require.NoError(t, err)
	require.NotEmpty(t, projection.Ref.Structural)
	for _, tt := range []struct {
		name        string
		fingerprint string
		valid       bool
	}{
		{name: "matching root", fingerprint: projection.Ref.Structural, valid: true},
		{name: "unknown identity", fingerprint: "missing-root", valid: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			prepared, errs := PrepareWithVariables(env.execSchema, `query Root($ref: String!) { node(ref: $ref) { title nodeKind ref { ref kind } locator { status ref { ref } } } }`, map[string]any{"ref": path + "#struct:" + tt.fingerprint})
			require.Empty(t, errs)
			result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
			if !tt.valid {
				require.NotEmpty(t, result.Errors)
				return
			}
			require.Empty(t, result.Errors)
			node := result.Data["node"].(map[string]any)
			require.Equal(t, "Guide", node["title"])
			require.Equal(t, "NOTE", node["nodeKind"])
			require.Equal(t, path, node["ref"].(map[string]any)["ref"])
			locator := node["locator"].(map[string]any)
			require.Equal(t, "linkable", locator["status"])
			require.Equal(t, path, locator["ref"].(map[string]any)["ref"])
		})
	}
}
